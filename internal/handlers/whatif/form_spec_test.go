package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
)

func formReq(form url.Values) *http.Request {
	req := httptest.NewRequest("POST", "/whatif/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()
	return req
}

func TestApplyFieldSpec_FloatHappyPath(t *testing.T) {
	spec := fieldSpec{Name: "x", Kind: fieldFloat, ParseLabel: "x"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"x": {"12.5"}})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" || !included {
		t.Fatalf("included=%v msg=%q", included, msg)
	}
	if updates["x"] != 12.5 {
		t.Errorf("expected 12.5, got %v", updates["x"])
	}
}

func TestApplyFieldSpec_FloatExplicitZeroIsIncluded(t *testing.T) {
	spec := fieldSpec{Name: "x", Kind: fieldFloat, ParseLabel: "x"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"x": {"0"}})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" || !included {
		t.Fatalf("included=%v msg=%q", included, msg)
	}
	if v, ok := updates["x"].(float64); !ok || v != 0 {
		t.Errorf("expected explicit 0, got %v", updates["x"])
	}
}

func TestApplyFieldSpec_FloatAbsentSkipped(t *testing.T) {
	spec := fieldSpec{Name: "x", Kind: fieldFloat, ParseLabel: "x"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("msg=%q", msg)
	}
	if included {
		t.Errorf("expected not included")
	}
	if _, ok := updates["x"]; ok {
		t.Errorf("expected updates[x] absent")
	}
}

func TestApplyFieldSpec_FloatEmptyStringSkipped(t *testing.T) {
	spec := fieldSpec{Name: "x", Kind: fieldFloat, ParseLabel: "x"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"x": {""}})
	included, _ := applyFieldSpec(r, spec, updates)
	if included {
		t.Errorf("empty string should not include")
	}
}

func TestApplyFieldSpec_FloatParseError(t *testing.T) {
	spec := fieldSpec{Name: "x", Kind: fieldFloat, ParseLabel: "test value"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"x": {"abc"}})
	_, msg := applyFieldSpec(r, spec, updates)
	if !strings.Contains(msg, "Invalid test value:") {
		t.Errorf("expected parse message containing label, got %q", msg)
	}
}

func TestApplyFieldSpec_FloatBoundsViolation(t *testing.T) {
	spec := fieldSpec{
		Name: "pct", Kind: fieldFloat, ParseLabel: "pct",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "pct out of range",
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"pct": {"150"}})
	_, msg := applyFieldSpec(r, spec, updates)
	if msg != "pct out of range" {
		t.Errorf("expected bounds message, got %q", msg)
	}
}

func TestApplyFieldSpec_FloatBoundsAtEdges(t *testing.T) {
	spec := fieldSpec{
		Name: "pct", Kind: fieldFloat, ParseLabel: "pct",
		HasBounds: true, Min: 0, Max: 100,
		BoundsMsg: "out of range",
	}
	for _, v := range []string{"0", "100"} {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"pct": {v}})
		if _, msg := applyFieldSpec(r, spec, updates); msg != "" {
			t.Errorf("value %s should be in range, got %q", v, msg)
		}
	}
}

func TestApplyFieldSpec_IntBoundsViolation(t *testing.T) {
	spec := fieldSpec{
		Name: "age", Kind: fieldInt, ParseLabel: "age",
		HasBounds: true, Min: 18, Max: 120,
		BoundsMsg: "Age must be between 18 and 120",
	}
	for _, v := range []string{"17", "121"} {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"age": {v}})
		_, msg := applyFieldSpec(r, spec, updates)
		if msg != "Age must be between 18 and 120" {
			t.Errorf("value %s: expected bounds message, got %q", v, msg)
		}
	}
}

func TestApplyFieldSpec_IntParseError(t *testing.T) {
	spec := fieldSpec{Name: "n", Kind: fieldInt, ParseLabel: "n"}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"n": {"3.14"}})
	_, msg := applyFieldSpec(r, spec, updates)
	if !strings.Contains(msg, "Invalid n:") {
		t.Errorf("expected parse message, got %q", msg)
	}
}

func TestApplyFieldSpec_EnumValid(t *testing.T) {
	spec := fieldSpec{
		Name: "ref", Kind: fieldEnum,
		EnumVals:       []string{"a", "b", "c"},
		EnumInvalidMsg: "bad ref",
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"ref": {"b"}})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" || !included {
		t.Fatalf("included=%v msg=%q", included, msg)
	}
	if updates["ref"] != "b" {
		t.Errorf("expected b, got %v", updates["ref"])
	}
}

func TestApplyFieldSpec_EnumInvalid(t *testing.T) {
	spec := fieldSpec{
		Name: "ref", Kind: fieldEnum,
		EnumVals:       []string{"a", "b"},
		EnumInvalidMsg: "bad ref",
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"ref": {"z"}})
	_, msg := applyFieldSpec(r, spec, updates)
	if msg != "bad ref" {
		t.Errorf("expected bad ref, got %q", msg)
	}
}

func TestApplyFieldSpec_EnumEmptySkipped(t *testing.T) {
	spec := fieldSpec{
		Name: "ref", Kind: fieldEnum,
		EnumVals: []string{"a", "b"},
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("msg=%q", msg)
	}
	if included {
		t.Errorf("absent enum should be skipped")
	}
}

func TestValidateSettingsCrossFieldInvariants_TaxDeferredPlusRoth(t *testing.T) {
	tests := []struct {
		name    string
		updates map[string]interface{}
		form    url.Values
		want    string
	}{
		{
			name:    "from updates: tax_deferred + roth > 100",
			updates: map[string]interface{}{"tax_deferred_percent": 60.0, "roth_percent": 50.0},
			form:    url.Values{},
			want:    "Tax-deferred + Roth cannot exceed 100%",
		},
		{
			name:    "from form fallback: tax_deferred reads from raw form",
			updates: map[string]interface{}{"roth_percent": 60.0},
			form:    url.Values{"tax_deferred_percent": {"50"}},
			want:    "Tax-deferred + Roth cannot exceed 100%",
		},
		{
			name:    "exactly 100 is allowed",
			updates: map[string]interface{}{"tax_deferred_percent": 60.0, "roth_percent": 40.0},
			form:    url.Values{},
			want:    "",
		},
		{
			name:    "no roth in updates means no check",
			updates: map[string]interface{}{"tax_deferred_percent": 90.0},
			form:    url.Values{"roth_percent": {"50"}},
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateSettingsCrossFieldInvariants(formReq(tc.form), tc.updates)
			if msg != tc.want {
				t.Errorf("expected %q, got %q", tc.want, msg)
			}
		})
	}
}

func TestValidateSettingsCrossFieldInvariants_StockPlusCash(t *testing.T) {
	tests := []struct {
		name    string
		updates map[string]interface{}
		form    url.Values
		want    string
	}{
		{
			name:    "from updates: stock + cash > 100",
			updates: map[string]interface{}{"stock_percent": 70.0, "cash_percent": 40.0},
			form:    url.Values{},
			want:    "Stocks + Cash cannot exceed 100%",
		},
		{
			name:    "default stock=60 used when neither updates nor form supplies it",
			updates: map[string]interface{}{"cash_percent": 41.0},
			form:    url.Values{},
			want:    "Stocks + Cash cannot exceed 100%",
		},
		{
			name:    "form fallback honored when updates missing",
			updates: map[string]interface{}{"cash_percent": 50.0},
			form:    url.Values{"stock_percent": {"40"}},
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateSettingsCrossFieldInvariants(formReq(tc.form), tc.updates)
			if msg != tc.want {
				t.Errorf("expected %q, got %q", tc.want, msg)
			}
		})
	}
}

func TestReadFloatFromUpdatesOrForm(t *testing.T) {
	t.Run("from updates", func(t *testing.T) {
		updates := map[string]interface{}{"k": 42.0}
		v, ok := readFloatFromUpdatesOrForm(formReq(url.Values{}), updates, "k", -1)
		if !ok || v != 42.0 {
			t.Errorf("got %v, %v", v, ok)
		}
	})
	t.Run("from form fallback", func(t *testing.T) {
		updates := map[string]interface{}{}
		v, ok := readFloatFromUpdatesOrForm(formReq(url.Values{"k": {"7.5"}}), updates, "k", -1)
		if !ok || v != 7.5 {
			t.Errorf("got %v, %v", v, ok)
		}
	})
	t.Run("form garbage falls through to fallback", func(t *testing.T) {
		updates := map[string]interface{}{}
		v, ok := readFloatFromUpdatesOrForm(formReq(url.Values{"k": {"nope"}}), updates, "k", 99)
		if ok || v != 99 {
			t.Errorf("got %v, %v", v, ok)
		}
	})
	t.Run("absent returns fallback", func(t *testing.T) {
		updates := map[string]interface{}{}
		v, ok := readFloatFromUpdatesOrForm(formReq(url.Values{}), updates, "k", 17)
		if ok || v != 17 {
			t.Errorf("got %v, %v", v, ok)
		}
	})
	t.Run("non-float in updates falls through to form", func(t *testing.T) {
		updates := map[string]interface{}{"k": "not a float"}
		v, ok := readFloatFromUpdatesOrForm(formReq(url.Values{"k": {"3.5"}}), updates, "k", -1)
		if !ok || v != 3.5 {
			t.Errorf("got %v, %v", v, ok)
		}
	})
}

func TestClampPerAccountAllocations(t *testing.T) {
	t.Run("clamps cash when sum exceeds 100", func(t *testing.T) {
		updates := map[string]interface{}{
			"tax_deferred_stock_percent": 70.0,
			"tax_deferred_cash_percent":  40.0,
		}
		clampPerAccountAllocations(updates)
		if updates["tax_deferred_cash_percent"] != 30.0 {
			t.Errorf("expected cash clamped to 30, got %v", updates["tax_deferred_cash_percent"])
		}
	})
	t.Run("never below zero", func(t *testing.T) {
		updates := map[string]interface{}{
			"roth_stock_percent": 120.0,
			"roth_cash_percent":  10.0,
		}
		clampPerAccountAllocations(updates)
		if updates["roth_cash_percent"] != 0.0 {
			t.Errorf("expected cash floored to 0, got %v", updates["roth_cash_percent"])
		}
	})
	t.Run("sum within 100 untouched", func(t *testing.T) {
		updates := map[string]interface{}{
			"taxable_stock_percent": 60.0,
			"taxable_cash_percent":  20.0,
		}
		clampPerAccountAllocations(updates)
		if updates["taxable_cash_percent"] != 20.0 {
			t.Errorf("expected cash unchanged, got %v", updates["taxable_cash_percent"])
		}
	})
	t.Run("missing fields no-op", func(t *testing.T) {
		updates := map[string]interface{}{
			"tax_deferred_stock_percent": 80.0,
		}
		clampPerAccountAllocations(updates)
		if _, ok := updates["tax_deferred_cash_percent"]; ok {
			t.Errorf("clamp should not invent missing cash key")
		}
	})
}

func TestApplyProjectionTiming(t *testing.T) {
	t.Run("empty skipped", func(t *testing.T) {
		updates := map[string]interface{}{}
		if msg := applyProjectionTiming(formReq(url.Values{}), updates); msg != "" {
			t.Errorf("msg=%q", msg)
		}
		if _, ok := updates["projection_timing"]; ok {
			t.Errorf("expected no key on empty input")
		}
	})
	t.Run("invalid value", func(t *testing.T) {
		updates := map[string]interface{}{}
		msg := applyProjectionTiming(formReq(url.Values{"projection_timing": {"middle"}}), updates)
		if msg != "Invalid projection timing" {
			t.Errorf("got %q", msg)
		}
	})
	t.Run("valid value persisted", func(t *testing.T) {
		updates := map[string]interface{}{}
		valid := string(models.ProjectionTimingStartOfMonth)
		if msg := applyProjectionTiming(formReq(url.Values{"projection_timing": {valid}}), updates); msg != "" {
			t.Errorf("unexpected msg=%q", msg)
		}
		if got, ok := updates["projection_timing"].(models.ProjectionTiming); !ok || string(got) != valid {
			t.Errorf("got %v, want %s", updates["projection_timing"], valid)
		}
	})
}

// F-035: applyRMDTiming tests mirror applyProjectionTiming.
func TestApplyRMDTiming_F035(t *testing.T) {
	t.Run("empty skipped", func(t *testing.T) {
		updates := map[string]interface{}{}
		if msg := applyRMDTiming(formReq(url.Values{}), updates); msg != "" {
			t.Errorf("msg=%q", msg)
		}
		if _, ok := updates["rmd_timing"]; ok {
			t.Errorf("expected no key on empty input")
		}
	})
	t.Run("invalid value", func(t *testing.T) {
		updates := map[string]interface{}{}
		msg := applyRMDTiming(formReq(url.Values{"rmd_timing": {"quarterly"}}), updates)
		if msg != "Invalid RMD timing" {
			t.Errorf("got %q", msg)
		}
	})
	t.Run("valid start_of_year", func(t *testing.T) {
		updates := map[string]interface{}{}
		valid := string(models.RMDTimingStartOfYear)
		if msg := applyRMDTiming(formReq(url.Values{"rmd_timing": {valid}}), updates); msg != "" {
			t.Errorf("unexpected msg=%q", msg)
		}
		if got, ok := updates["rmd_timing"].(models.RMDTiming); !ok || string(got) != valid {
			t.Errorf("got %v, want %s", updates["rmd_timing"], valid)
		}
	})
	t.Run("valid mid_year", func(t *testing.T) {
		updates := map[string]interface{}{}
		if msg := applyRMDTiming(formReq(url.Values{"rmd_timing": {"mid_year"}}), updates); msg != "" {
			t.Errorf("unexpected msg=%q", msg)
		}
		got, ok := updates["rmd_timing"].(models.RMDTiming)
		if !ok || got != models.RMDTimingMidYear {
			t.Errorf("got %v, want mid_year", updates["rmd_timing"])
		}
	})
	t.Run("valid end_of_year", func(t *testing.T) {
		updates := map[string]interface{}{}
		if msg := applyRMDTiming(formReq(url.Values{"rmd_timing": {"end_of_year"}}), updates); msg != "" {
			t.Errorf("unexpected msg=%q", msg)
		}
		got, ok := updates["rmd_timing"].(models.RMDTiming)
		if !ok || got != models.RMDTimingEndOfYear {
			t.Errorf("got %v, want end_of_year", updates["rmd_timing"])
		}
	})
}

func TestApplyFieldSpec_OptionalFloat_EmptyRawIsNil(t *testing.T) {
	form := url.Values{}
	form.Set("rate", "")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	updates := map[string]interface{}{}
	spec := fieldSpec{Name: "rate", Kind: fieldOptionalFloat, ParseLabel: "rate"}
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	if !included {
		t.Fatal("included = false, want true (empty raw still propagates as nil)")
	}
	got, ok := updates["rate"].(*float64)
	if !ok {
		t.Fatalf("updates[rate] type = %T, want *float64", updates["rate"])
	}
	if got != nil {
		t.Errorf("got %v, want nil", *got)
	}
}

func TestApplyFieldSpec_OptionalFloat_ZeroIsConfigured(t *testing.T) {
	form := url.Values{}
	form.Set("rate", "0")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	updates := map[string]interface{}{}
	spec := fieldSpec{Name: "rate", Kind: fieldOptionalFloat, ParseLabel: "rate"}
	_, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	got, ok := updates["rate"].(*float64)
	if !ok || got == nil {
		t.Fatalf("updates[rate] = %v (%T), want non-nil *float64", updates["rate"], updates["rate"])
	}
	if *got != 0 {
		t.Errorf("got %v, want 0", *got)
	}
}

func TestApplyFieldSpec_OptionalFloat_NonzeroIsConfigured(t *testing.T) {
	form := url.Values{}
	form.Set("rate", "9.3")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	updates := map[string]interface{}{}
	spec := fieldSpec{Name: "rate", Kind: fieldOptionalFloat, ParseLabel: "rate"}
	_, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	got, ok := updates["rate"].(*float64)
	if !ok || got == nil {
		t.Fatalf("updates[rate] = %v (%T), want non-nil *float64", updates["rate"], updates["rate"])
	}
	if *got != 9.3 {
		t.Errorf("got %v, want 9.3", *got)
	}
}

func TestApplyFieldSpec_OptionalFloat_BoundsViolation(t *testing.T) {
	form := url.Values{}
	form.Set("rate", "999")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	updates := map[string]interface{}{}
	spec := fieldSpec{Name: "rate", Kind: fieldOptionalFloat, ParseLabel: "rate",
		HasBounds: true, Min: 0, Max: 20, BoundsMsg: "rate must be 0..20"}
	_, msg := applyFieldSpec(r, spec, updates)
	if msg != "rate must be 0..20" {
		t.Errorf("msg = %q, want %q", msg, "rate must be 0..20")
	}
}

func TestApplyFieldSpec_OptionalFloat_AbsentKeyIsNotIncluded(t *testing.T) {
	// Form has no "rate" key at all. Should NOT be added to updates,
	// preserving partial-PATCH semantics.
	form := url.Values{}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}

	updates := map[string]interface{}{}
	spec := fieldSpec{Name: "rate", Kind: fieldOptionalFloat, ParseLabel: "rate"}
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	if included {
		t.Error("included = true, want false (absent key must not propagate)")
	}
	if _, exists := updates["rate"]; exists {
		t.Error("updates contains rate key, want absent")
	}
}

func TestFormSpec_RothFirstFundedYear_Parse(t *testing.T) {
	t.Run("blank parses to zero", func(t *testing.T) {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"roth_first_funded_year": {""}})
		msg := applySettingsFormSpec(r, updates)
		if msg != "" {
			t.Fatalf("unexpected error: %s", msg)
		}
		// Blank should not produce an error; value absent from updates (skipped as zero+empty).
		if v, ok := updates["roth_first_funded_year"]; ok {
			switch x := v.(type) {
			case int:
				if x != 0 {
					t.Fatalf("blank should yield zero, got %v", x)
				}
			case float64:
				if x != 0 {
					t.Fatalf("blank should yield zero, got %v", x)
				}
			default:
				t.Fatalf("unexpected type %T for roth_first_funded_year", v)
			}
		}
	})

	t.Run("valid year parses through", func(t *testing.T) {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"roth_first_funded_year": {"2010"}})
		msg := applySettingsFormSpec(r, updates)
		if msg != "" {
			t.Fatalf("unexpected errors: %s", msg)
		}
		got := updates["roth_first_funded_year"]
		if got == nil {
			t.Fatal("missing parsed value")
		}
		if got.(int) != 2010 {
			t.Fatalf("want 2010, got %v", got)
		}
	})

	t.Run("year before 1998 errors", func(t *testing.T) {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"roth_first_funded_year": {"1997"}})
		msg := applySettingsFormSpec(r, updates)
		if msg == "" {
			t.Fatalf("expected validation error for year < 1998")
		}
	})

	t.Run("far-future year errors", func(t *testing.T) {
		updates := map[string]interface{}{}
		r := formReq(url.Values{"roth_first_funded_year": {"3000"}})
		msg := applySettingsFormSpec(r, updates)
		if msg == "" {
			t.Fatalf("expected validation error for year > current+50")
		}
	})
}

func TestFormSpec_RothFirstFundedYear_AppliedToSettings(t *testing.T) {
	// Build a minimal SettingsManager backed by a temp dir so we can call
	// UpdateSettings and verify the field is wired through.
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"roth_first_funded_year": {"2026"}}
	r := formReq(form)
	updates := map[string]interface{}{}
	if msg := applySettingsFormSpec(r, updates); msg != "" {
		t.Fatalf("parse error: %s", msg)
	}
	s, err := rm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if s.RothFirstFundedYear != 2026 {
		t.Fatalf("RothFirstFundedYear not applied: got %d, want 2026", s.RothFirstFundedYear)
	}
}

// TestFormSpec_RothFirstFundedYear_BlankClearsPersistedValue verifies that
// submitting the form with an empty roth_first_funded_year clears a
// previously-set value (rather than leaving it stale). The UI nudge tells
// users to leave it blank if they don't know it; that promise requires
// blank to actually clear the persisted setting.
func TestFormSpec_RothFirstFundedYear_BlankClearsPersistedValue(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// First save a non-zero value.
	form := url.Values{"roth_first_funded_year": {"2026"}}
	updates := map[string]interface{}{}
	if msg := applySettingsFormSpec(formReq(form), updates); msg != "" {
		t.Fatalf("first parse error: %s", msg)
	}
	s, err := rm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings (set): %v", err)
	}
	if s.RothFirstFundedYear != 2026 {
		t.Fatalf("setup: RothFirstFundedYear=%d, want 2026", s.RothFirstFundedYear)
	}

	// Now submit blank — the persisted value must be cleared to 0.
	form = url.Values{"roth_first_funded_year": {""}}
	updates = map[string]interface{}{}
	if msg := applySettingsFormSpec(formReq(form), updates); msg != "" {
		t.Fatalf("blank parse error: %s", msg)
	}
	if v, ok := updates["roth_first_funded_year"]; !ok {
		t.Fatalf("blank submission did not emit roth_first_funded_year update; updates=%v", updates)
	} else if iv, _ := v.(int); iv != 0 {
		t.Fatalf("blank submission emitted %v, want 0", v)
	}
	s, err = rm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings (clear): %v", err)
	}
	if s.RothFirstFundedYear != 0 {
		t.Fatalf("blank did not clear: RothFirstFundedYear=%d, want 0", s.RothFirstFundedYear)
	}
}

// TestApplyFieldSpec_IntAllowBlankZero_AbsentKeySkipped pins the bug fix for
// AllowBlankZero fields: when the form does NOT carry the key at all (because
// a different partial settings form was submitted), the field must be skipped
// — not silently written as zero.
func TestApplyFieldSpec_IntAllowBlankZero_AbsentKeySkipped(t *testing.T) {
	spec := fieldSpec{
		Name: "roth_first_funded_year", Kind: fieldInt, ParseLabel: "Roth first funded year",
		HasBounds: true, Min: 1998, Max: 9999,
		BoundsMsg:      "Year must be 1998 or later",
		AllowBlankZero: true,
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"some_other_field": {"42"}})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	if included {
		t.Error("included = true, want false (absent key must not propagate)")
	}
	if _, exists := updates["roth_first_funded_year"]; exists {
		t.Errorf("updates contains roth_first_funded_year, want absent; got %v", updates)
	}
}

// TestApplyFieldSpec_IntAllowBlankZero_PresentBlankIncluded is the companion
// regression guard: when the key IS submitted with an empty value (user
// cleared the input), the spec must still emit zero so the persisted value
// is cleared. Verifies the fix preserves the legitimate clear path.
func TestApplyFieldSpec_IntAllowBlankZero_PresentBlankIncluded(t *testing.T) {
	spec := fieldSpec{
		Name: "roth_first_funded_year", Kind: fieldInt, ParseLabel: "Roth first funded year",
		HasBounds: true, Min: 1998, Max: 9999,
		BoundsMsg:      "Year must be 1998 or later",
		AllowBlankZero: true,
	}
	updates := map[string]interface{}{}
	r := formReq(url.Values{"roth_first_funded_year": {""}})
	included, msg := applyFieldSpec(r, spec, updates)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
	if !included {
		t.Fatal("included = false, want true (present-but-blank is an explicit clear)")
	}
	v, ok := updates["roth_first_funded_year"].(int)
	if !ok {
		t.Fatalf("updates[roth_first_funded_year] missing or wrong type: %T %v", updates["roth_first_funded_year"], updates["roth_first_funded_year"])
	}
	if v != 0 {
		t.Errorf("present-blank emitted %d, want 0", v)
	}
}

// TestFormSpec_RothFirstFundedYear_AbsentFromOtherFormPreservesPersisted is
// the end-to-end regression for the silent-clobber bug. A previously-saved
// RothFirstFundedYear must survive a partial /whatif/settings post that
// changes some other field (e.g. the steady-state slider) and therefore
// does not carry the roth_first_funded_year key.
func TestFormSpec_RothFirstFundedYear_AbsentFromOtherFormPreservesPersisted(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Save Roth year via the Roth-bearing form.
	form := url.Values{"roth_first_funded_year": {"2026"}}
	updates := map[string]interface{}{}
	if msg := applySettingsFormSpec(formReq(form), updates); msg != "" {
		t.Fatalf("setup parse error: %s", msg)
	}
	s, err := rm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("setup UpdateSettings: %v", err)
	}
	if s.RothFirstFundedYear != 2026 {
		t.Fatalf("setup failed: RothFirstFundedYear=%d, want 2026", s.RothFirstFundedYear)
	}

	// Simulate a partial settings post from a form that does NOT include
	// roth_first_funded_year (e.g. budget-analysis.html steady-state slider).
	form = url.Values{"steady_state_override_year": {"2055"}}
	updates = map[string]interface{}{}
	if msg := applySettingsFormSpec(formReq(form), updates); msg != "" {
		t.Fatalf("partial parse error: %s", msg)
	}
	if _, included := updates["roth_first_funded_year"]; included {
		t.Errorf("partial form silently included roth_first_funded_year; updates=%v", updates)
	}
	s, err = rm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("partial UpdateSettings: %v", err)
	}
	if s.RothFirstFundedYear != 2026 {
		t.Fatalf("RothFirstFundedYear clobbered by unrelated partial post: got %d, want 2026", s.RothFirstFundedYear)
	}
}
