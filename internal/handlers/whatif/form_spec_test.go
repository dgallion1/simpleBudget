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
