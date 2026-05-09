# Scenario-Completeness Banner Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the two unfixable completeness banners on `feat/scenario-completeness` so a fresh scenario produces zero `SeverityError` findings and FL/TX/WA users can correctly enter `0` for state tax without a permanent warning.

**Architecture:** Two coordinated changes — (1) flip `DefaultTaxConfig` to `FilingSingle` and add a filing-status `<select>` to the rate-assumptions card, and (2) make `TaxConfig.StateIncomeTaxRate` a `*float64` so `nil` means "unset" and any explicit value (incl. 0) means "configured". A new `fieldOptionalFloat` parser kind plumbs the form-spec layer; a new `StateIncomeTaxRateOrZero()` helper isolates engine-layer reads.

**Tech Stack:** Go 1.22, chi router, html/template, plain HTML/JS for the rate-assumptions card.

**Worktree:** All paths are relative to the repo root at `/tmp/budget2-scenario-review` (branch `feat/scenario-completeness`). All `git`, `go test`, and `cd` commands assume that working directory.

**Spec:** `docs/superpowers/specs/2026-05-09-scenario-completeness-fixes-design.md`

---

## File Map

| File | Why touched |
|---|---|
| `internal/models/whatif.go` | `DefaultTaxConfig` default flips to `FilingSingle`; `StateIncomeTaxRate` becomes `*float64`; new `StateIncomeTaxRateOrZero()` helper |
| `internal/services/retirement/settings.go` | `defaultTaxConfigForPersons` logic inverted; new `filing_status` apply step; `state_income_tax_rate` apply step casts `*float64` |
| `internal/services/retirement/engine/tax.go` | `NewTaxCalculator` uses helper instead of direct field read |
| `internal/services/retirement/completeness/checks_state_tax.go` | Nil-check instead of `> 0` check |
| `internal/handlers/whatif/form_spec.go` | New `fieldOptionalFloat` kind; new `filing_status` enum entry; switch `state_income_tax_rate` to optional-float |
| `web/templates/components/whatif/rate-assumptions.html` | New filing-status `<select>`; nullable rate input (blank when nil, placeholder, updated help text) |
| `internal/services/retirement/completeness/check_test.go` | New `TestCheckStateTaxUnset_*` cases; `TestDefaultWhatIfSettings_NoErrorFindings` |
| `internal/handlers/whatif/handlers_test.go` | `TestHandleWhatIfSettings_FilingStatus*` cases |
| `internal/handlers/whatif/completeness_render_test.go`, `internal/services/retirement/calculator_state_tax_test.go`, `tax_test.go`, `settings_state_tax_test.go`, `coverage_gaps_test.go`, `coverage_gaps2_test.go`, `coverage_gaps3_test.go`, `calculator_coverage_test.go`, `analysis/rmd_tax_test.go`, `models_extra_test.go` | Construct `StateIncomeTaxRate` as `&val` (helper `floatPtr` recommended) |

---

## Phase A — Filing-status default

### Task A1: `DefaultTaxConfig` defaults to FilingSingle

**Files:**
- Modify: `internal/models/whatif.go:1156-1162`
- Test: `internal/models/models_extra_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/models/models_extra_test.go`:

```go
func TestDefaultTaxConfig_FilingStatusIsSingle(t *testing.T) {
	cfg := models.DefaultTaxConfig()
	if cfg == nil {
		t.Fatal("DefaultTaxConfig returned nil")
	}
	if cfg.FilingStatus != models.FilingSingle {
		t.Errorf("FilingStatus = %q, want %q", cfg.FilingStatus, models.FilingSingle)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/models/ -run TestDefaultTaxConfig_FilingStatusIsSingle -v
```

Expected: FAIL with `FilingStatus = "married_joint", want "single"`.

- [ ] **Step 3: Flip the default**

In `internal/models/whatif.go` change line ~1159:

```go
func DefaultTaxConfig() *TaxConfig {
	return &TaxConfig{
		FilingStatus:       FilingSingle,
		StateIncomeTaxRate: 0.0, // No state tax by default
	}
}
```

- [ ] **Step 4: Verify test passes**

```bash
go test ./internal/models/ -run TestDefaultTaxConfig_FilingStatusIsSingle -v
```

Expected: PASS.

- [ ] **Step 5: Run full models package**

```bash
go test ./internal/models/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/models/whatif.go internal/models/models_extra_test.go
git commit -m "feat(models): DefaultTaxConfig defaults to FilingSingle

Matches default Persons slice (one Primary). Eliminates the false-positive
mfj_no_spouse_person error on every fresh scenario.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task A2: Invert `defaultTaxConfigForPersons` logic

**Files:**
- Modify: `internal/services/retirement/settings.go:205-224`
- Test: `internal/services/retirement/settings_state_tax_test.go` (append) OR new file `internal/services/retirement/settings_default_tax_config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/settings_state_tax_test.go` (or create a new file in the same package):

```go
func TestDefaultTaxConfigForPersons_NoSpouse_StaysSingle(t *testing.T) {
	persons := []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
	}
	cfg := defaultTaxConfigForPersons(persons)
	if cfg.FilingStatus != models.FilingSingle {
		t.Errorf("FilingStatus = %q, want Single", cfg.FilingStatus)
	}
}

func TestDefaultTaxConfigForPersons_WithSpouse_UpgradesToMFJ(t *testing.T) {
	persons := []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
	}
	cfg := defaultTaxConfigForPersons(persons)
	if cfg.FilingStatus != models.FilingMarriedJoint {
		t.Errorf("FilingStatus = %q, want MarriedJoint", cfg.FilingStatus)
	}
}

func TestDefaultTaxConfigForPersons_OtherRoleOnly_StaysSingle(t *testing.T) {
	persons := []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Other", Role: models.PersonRoleOther},
	}
	cfg := defaultTaxConfigForPersons(persons)
	if cfg.FilingStatus != models.FilingSingle {
		t.Errorf("FilingStatus = %q, want Single (PersonRoleOther is not a spouse)", cfg.FilingStatus)
	}
}
```

- [ ] **Step 2: Run tests — first two pass (already correct), third may fail**

```bash
go test ./internal/services/retirement/ -run TestDefaultTaxConfigForPersons -v
```

Expected: After A1 the function still says "if !hasSpouse, set to Single", and `DefaultTaxConfig` now returns Single — so the no-spouse test passes by accident. The `WithSpouse` test FAILS because the function never upgrades from Single to MFJ.

- [ ] **Step 3: Invert the logic**

In `internal/services/retirement/settings.go` replace the body of `defaultTaxConfigForPersons` (lines ~211-224):

```go
func defaultTaxConfigForPersons(persons []models.Person) *models.TaxConfig {
	cfg := models.DefaultTaxConfig() // FilingSingle after A1
	for _, p := range persons {
		if p.Role == models.PersonRoleSpouse {
			cfg.FilingStatus = models.FilingMarriedJoint
			break
		}
	}
	return cfg
}
```

Update the doc comment above the function to read:

```go
// defaultTaxConfigForPersons returns a TaxConfig with filing status
// inferred from the household shape. Single-person scenarios stay at
// the FilingSingle default; scenarios with a spouse Person upgrade to
// married-jointly. Used to initialize TaxConfig for legacy scenarios
// that were saved with TaxConfig == nil.
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/services/retirement/ -run TestDefaultTaxConfigForPersons -v
```

Expected: PASS (all three).

- [ ] **Step 5: Run full retirement package**

```bash
go test ./internal/services/retirement/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_state_tax_test.go
git commit -m "refactor(retirement): invert defaultTaxConfigForPersons after Single-default flip

Previously assumed DefaultTaxConfig returned MFJ and downgraded to Single
when no spouse was present. Now defaults to Single and upgrades to MFJ
when a spouse Person is detected. Preserves correct behaviour for legacy
scenarios that load with TaxConfig == nil and a spouse on record.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task A3: Default scenario produces zero SeverityError findings

**Files:**
- Test: `internal/services/retirement/completeness/check_test.go` (append)

- [ ] **Step 1: Write the test**

Append to `internal/services/retirement/completeness/check_test.go`:

```go
func TestDefaultWhatIfSettings_NoErrorFindings(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	findings := Check(settings)

	for _, f := range findings {
		if f.Severity == SeverityError {
			t.Errorf("DefaultWhatIfSettings should produce no SeverityError findings, got: code=%s title=%q", f.Code, f.Title)
		}
	}
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./internal/services/retirement/completeness/ -run TestDefaultWhatIfSettings_NoErrorFindings -v
```

Expected: PASS — the `mfj_no_spouse_person` Error no longer fires now that A1 set the default to Single. (The `state_tax_unset` Warn may still fire, which is OK — it is `SeverityWarn`, not `SeverityError`.)

- [ ] **Step 3: Commit**

```bash
git add internal/services/retirement/completeness/check_test.go
git commit -m "test(completeness): regression — default settings produce no Error findings

Locks in A1+A2: a fresh DefaultWhatIfSettings() must never emit a
SeverityError finding from completeness.Check.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase B — Filing-status UI + persistence

### Task B1: Add `filing_status` to `settingsFormSpec`

**Files:**
- Modify: `internal/handlers/whatif/form_spec.go` (after line 116, before the closing `}`)
- Test: `internal/handlers/whatif/handlers_test.go` (append)

- [ ] **Step 1: Write the failing roundtrip test**

Append to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIfSettings_FilingStatusRoundTrip(t *testing.T) {
	env := setupTestEnvWithRenderer(t)

	form := url.Values{}
	form.Set("filing_status", "married_joint")
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	got := env.retirementMgr.GetSettings()
	if got.TaxConfig == nil {
		t.Fatal("TaxConfig is nil after POST")
	}
	if got.TaxConfig.FilingStatus != models.FilingMarriedJoint {
		t.Errorf("FilingStatus = %q, want married_joint", got.TaxConfig.FilingStatus)
	}
}
```

(If `setupTestEnvWithRenderer` isn't the right helper in this file, copy the pattern from existing `TestHandleWhatIfSettings_*` tests at the top of `handlers_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/handlers/whatif/ -run TestHandleWhatIfSettings_FilingStatusRoundTrip -v
```

Expected: FAIL — POST is accepted (no enum entry → field ignored), but `FilingStatus` stays at whatever the env's default is, not `married_joint`.

- [ ] **Step 3: Add the form spec entry**

In `internal/handlers/whatif/form_spec.go`, insert before the closing `}` of `settingsFormSpec` (after the `state_income_tax_rate` entry at ~line 116):

```go
	{Name: "filing_status", Kind: fieldEnum,
		EnumVals:       []string{"single", "married_joint", "married_separate", "head_of_household"},
		EnumInvalidMsg: "Invalid filing status"},
```

- [ ] **Step 4: Add the apply step**

In `internal/services/retirement/settings.go`, just after the `state_income_tax_rate` apply block (currently lines 1003-1008), append:

```go
	if v, ok := updates["filing_status"].(string); ok {
		if settings.TaxConfig == nil {
			settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
		}
		settings.TaxConfig.FilingStatus = models.FilingStatus(v)
	}
```

- [ ] **Step 5: Verify test passes**

```bash
go test ./internal/handlers/whatif/ -run TestHandleWhatIfSettings_FilingStatusRoundTrip -v
```

Expected: PASS.

- [ ] **Step 6: Run full handlers package**

```bash
go test ./internal/handlers/whatif/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/whatif/form_spec.go internal/services/retirement/settings.go internal/handlers/whatif/handlers_test.go
git commit -m "feat(whatif): wire filing_status form field through to TaxConfig

Adds a fieldEnum entry to settingsFormSpec and an apply step in
settings.go that mirrors the existing phase_age_reference pattern.
Allocates TaxConfig via defaultTaxConfigForPersons when nil.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B2: Invalid `filing_status` returns 400

**Files:**
- Test: `internal/handlers/whatif/handlers_test.go` (append)

- [ ] **Step 1: Write the test**

Append to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIfSettings_FilingStatusInvalid(t *testing.T) {
	env := setupTestEnvWithRenderer(t)

	form := url.Values{}
	form.Set("filing_status", "garbage")
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Invalid filing status") {
		t.Errorf("body should contain enum error message, got: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test**

```bash
go test ./internal/handlers/whatif/ -run TestHandleWhatIfSettings_FilingStatusInvalid -v
```

Expected: PASS — the enum parser at `form_spec.go` already returns `EnumInvalidMsg` for unknown values, and the surrounding `applySettingsFormSpec` returns it as a 400.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/whatif/handlers_test.go
git commit -m "test(whatif): regression — invalid filing_status returns 400

Locks in the enum-parser behaviour: filing_status=<garbage> emits
'Invalid filing status' as a 400 response body, matching the existing
phase_age_reference contract.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task B3: Add filing-status `<select>` to the rate-assumptions card

**Files:**
- Modify: `web/templates/components/whatif/rate-assumptions.html` (insert just above the State Income Tax block at line ~154)

- [ ] **Step 1: Insert the select**

In `web/templates/components/whatif/rate-assumptions.html`, find the block starting `<label for="state-income-tax-rate-input"` (around line 154-161) and insert the following block IMMEDIATELY above it (so filing status precedes state tax rate visually):

```html
        <div>
            <label for="filing-status-select" class="block text-sm font-medium text-gray-700 dark:text-gray-200">Filing Status</label>
            <select id="filing-status-select" name="filing_status"
                class="mt-1 block w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
                {{$fs := ""}}{{if .Settings.TaxConfig}}{{$fs = .Settings.TaxConfig.FilingStatus}}{{end}}
                <option value="single" {{if eq $fs "single"}}selected{{end}}>Single</option>
                <option value="married_joint" {{if eq $fs "married_joint"}}selected{{end}}>Married Filing Jointly</option>
                <option value="married_separate" {{if eq $fs "married_separate"}}selected{{end}}>Married Filing Separately</option>
                <option value="head_of_household" {{if eq $fs "head_of_household"}}selected{{end}}>Head of Household</option>
            </select>
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Used for federal brackets, standard deduction, IRMAA thresholds, and Social Security taxation.</p>
        </div>

```

(Mind the trailing blank line so the next `<div>` block keeps its existing indentation.)

- [ ] **Step 2: Confirm the template still renders**

```bash
go test ./internal/templates/... ./internal/handlers/whatif/...
```

Expected: PASS (no test asserts on the new HTML yet, but template parse errors would fail compilation/tests).

- [ ] **Step 3: Smoke verify in the browser**

Start the dev server if not running:

```bash
go run ./cmd/server
```

Open the what-if page, expand the Rate Assumptions card, confirm:
- A "Filing Status" dropdown is visible above "State Income Tax Rate %".
- For a fresh scenario the dropdown shows "Single".
- Changing it to "Married Filing Jointly" triggers the existing `hx-post` and the page re-renders without errors.
- The `mfj_no_spouse_person` banner appears after switching to MFJ on a single-person household, and clears after switching back to Single.

If any of those fail, stop and diagnose before moving on.

- [ ] **Step 4: Commit**

```bash
git add web/templates/components/whatif/rate-assumptions.html
git commit -m "feat(whatif/ui): add filing-status select to Rate Assumptions card

Sits directly above State Income Tax Rate so both TaxConfig fields are
visually adjacent. Submits via the existing hx-post; the completeness
banner re-renders on next analysis.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase C — Nullable state-tax rate

### Task C1: Add `StateIncomeTaxRateOrZero` helper (field still float64)

**Files:**
- Modify: `internal/models/whatif.go` (just below the `TaxConfig` struct definition, ~line 1162)
- Test: `internal/models/models_extra_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/models/models_extra_test.go`:

```go
func TestStateIncomeTaxRateOrZero_NilTaxConfig(t *testing.T) {
	var tc *models.TaxConfig
	if got := tc.StateIncomeTaxRateOrZero(); got != 0 {
		t.Errorf("nil TaxConfig: got %v, want 0", got)
	}
}

func TestStateIncomeTaxRateOrZero_ZeroRate(t *testing.T) {
	tc := &models.TaxConfig{StateIncomeTaxRate: 0}
	if got := tc.StateIncomeTaxRateOrZero(); got != 0 {
		t.Errorf("zero rate: got %v, want 0", got)
	}
}

func TestStateIncomeTaxRateOrZero_NonzeroRate(t *testing.T) {
	tc := &models.TaxConfig{StateIncomeTaxRate: 9.3}
	if got := tc.StateIncomeTaxRateOrZero(); got != 9.3 {
		t.Errorf("non-zero rate: got %v, want 9.3", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/models/ -run TestStateIncomeTaxRateOrZero -v
```

Expected: FAIL with `tc.StateIncomeTaxRateOrZero undefined`.

- [ ] **Step 3: Add the helper (field still `float64`)**

In `internal/models/whatif.go`, immediately after the `DefaultTaxConfig` function (after line ~1162):

```go
// StateIncomeTaxRateOrZero returns the configured state rate, or 0 if
// the TaxConfig pointer or rate is nil/zero. Use this at engine and
// math boundaries; use direct nil checks at completeness/validation
// boundaries where "unset" semantics matter.
func (t *TaxConfig) StateIncomeTaxRateOrZero() float64 {
	if t == nil {
		return 0
	}
	return t.StateIncomeTaxRate
}
```

(After the type change in C3, this body will be revised — but the signature stays the same. That's the whole point of introducing it now.)

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/models/ -run TestStateIncomeTaxRateOrZero -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/whatif.go internal/models/models_extra_test.go
git commit -m "feat(models): add StateIncomeTaxRateOrZero helper

Boundary helper that returns the configured rate or 0 when the TaxConfig
or rate is unset. Lets engine code stay agnostic to the upcoming
*float64 type change.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C2: Migrate engine reader to the helper

**Files:**
- Modify: `internal/services/retirement/engine/tax.go:198`

- [ ] **Step 1: Replace the direct field read**

In `internal/services/retirement/engine/tax.go`, change the `NewTaxCalculator` constructor (around line 198):

```go
		StateRate:          config.StateIncomeTaxRateOrZero(),
```

(was `config.StateIncomeTaxRate`)

- [ ] **Step 2: Run engine tests**

```bash
go test ./internal/services/retirement/...
```

Expected: PASS — semantics unchanged because the helper currently mirrors the field.

- [ ] **Step 3: Commit**

```bash
git add internal/services/retirement/engine/tax.go
git commit -m "refactor(engine): NewTaxCalculator reads state rate via helper

Boundary cleanup ahead of the *float64 type change. No behaviour change
yet.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C3: Add `fieldOptionalFloat` parser kind

**Files:**
- Modify: `internal/handlers/whatif/form_spec.go` (constants block + `applyFieldSpec` switch)
- Test: new file `internal/handlers/whatif/form_spec_optional_float_test.go` (or append to existing `form_spec_test.go` if present)

- [ ] **Step 0: Check for an existing form_spec test file**

```bash
ls internal/handlers/whatif/form_spec*_test.go 2>/dev/null
```

If a test file exists, append the test below to it. Otherwise create `internal/handlers/whatif/form_spec_optional_float_test.go`.

- [ ] **Step 1: Write the failing tests**

Use this content (full file template if creating new):

```go
package whatif

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/handlers/whatif/ -run TestApplyFieldSpec_OptionalFloat -v
```

Expected: FAIL with `undeclared name: fieldOptionalFloat`.

- [ ] **Step 3: Add the new kind constant**

In `internal/handlers/whatif/form_spec.go`, change the `fieldKind` constant block (around lines 14-18):

```go
const (
	fieldFloat fieldKind = iota
	fieldInt
	fieldEnum
	fieldOptionalFloat // empty-but-present raw → nil; numeric raw (incl. "0") → &v
)
```

- [ ] **Step 4: Add the case to `applyFieldSpec`**

In `internal/handlers/whatif/form_spec.go`, inside the `switch spec.Kind` block in `applyFieldSpec` (after the `fieldEnum` case), add:

```go
	case fieldOptionalFloat:
		// Distinguish three input states:
		// - key absent (FormValue returns "" and the form has no key) → don't include
		// - key present and empty                                       → include as nil
		// - key present with parseable numeric (incl. "0")              → include as &v
		if _, present := r.Form[spec.Name]; !present {
			return false, ""
		}
		if raw == "" {
			updates[spec.Name] = (*float64)(nil)
			return true, ""
		}
		v, parseErr := parseFormFloat(r, spec.Name)
		if parseErr != nil {
			return false, fmt.Sprintf("Invalid %s: %s", spec.ParseLabel, parseErr.Error())
		}
		if spec.HasBounds && (v < spec.Min || v > spec.Max) {
			return false, spec.BoundsMsg
		}
		ptr := v
		updates[spec.Name] = &ptr
```

- [ ] **Step 5: Verify tests pass**

```bash
go test ./internal/handlers/whatif/ -run TestApplyFieldSpec_OptionalFloat -v
```

Expected: PASS (all five cases).

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/whatif/form_spec.go internal/handlers/whatif/form_spec_optional_float_test.go
git commit -m "feat(form_spec): add fieldOptionalFloat parser kind

Distinguishes three input states: key absent (don't propagate), key
present and empty (propagate as nil *float64), key present with
numeric value (propagate as &v). Enables nil-as-explicit-state for
optional numeric fields like state_income_tax_rate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C4: Type change — `StateIncomeTaxRate *float64` (atomic)

This is the largest task. It must land as a single commit because the codebase will not compile in between.

**Files (all in one commit):**
- Modify: `internal/models/whatif.go` (struct field, default, helper body)
- Modify: `internal/services/retirement/settings.go` (apply step cast)
- Modify: `internal/handlers/whatif/form_spec.go` (switch entry to fieldOptionalFloat)
- Modify: 8+ test files (construct rate as `&val`)
- Modify: `internal/services/retirement/completeness/checks_state_tax.go` (covered in C5 — keep as `> 0` check until then to keep this commit's scope tight)

- [ ] **Step 1: Add a `floatPtr` test helper**

Create `internal/models/ptr.go`:

```go
package models

// FloatPtr returns a pointer to the given float64 value. Convenience
// for constructing nullable numeric fields in tests and call sites
// where taking the address inline is awkward.
func FloatPtr(v float64) *float64 {
	return &v
}
```

(This belongs in the `models` package because it's about model types and is consumed by tests across packages.)

- [ ] **Step 2: Change the struct field and default**

In `internal/models/whatif.go` change the `TaxConfig` struct (around line 1148-1154):

```go
type TaxConfig struct {
	FilingStatus       FilingStatus `json:"filing_status"`
	StateIncomeTaxRate *float64     `json:"state_income_tax_rate,omitempty"` // nil = unset; *0 = explicit no-tax state; *x = configured rate
	Age65Count         int          `json:"age_65_count"`          // F-001: number of filers 65 or older (0, 1, or 2 for MFJ).
	MFSLivedWithSpouse bool         `json:"mfs_lived_with_spouse"` // F-018: 26 USC § 86(c)(2) sub-case; true = lived with spouse → $0/$0 thresholds.
}
```

Note the `,omitempty` so JSON for unset rate writes `{}` not `{"state_income_tax_rate": null}` (more compatible with future readers). Existing JSON `{"state_income_tax_rate": 0}` still decodes to `*float64` pointing at 0.

Update `DefaultTaxConfig` (around line 1156-1162):

```go
func DefaultTaxConfig() *TaxConfig {
	return &TaxConfig{
		FilingStatus:       FilingSingle,
		StateIncomeTaxRate: nil, // unset; user must explicitly set (incl. 0 for no-tax states)
	}
}
```

Update `StateIncomeTaxRateOrZero` (the helper from C1):

```go
func (t *TaxConfig) StateIncomeTaxRateOrZero() float64 {
	if t == nil || t.StateIncomeTaxRate == nil {
		return 0
	}
	return *t.StateIncomeTaxRate
}
```

- [ ] **Step 3: Update the apply step**

In `internal/services/retirement/settings.go` change the `state_income_tax_rate` apply block (lines 1003-1008):

```go
	if v, ok := updates["state_income_tax_rate"].(*float64); ok {
		if settings.TaxConfig == nil {
			settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
		}
		settings.TaxConfig.StateIncomeTaxRate = v
	}
```

- [ ] **Step 4: Switch the form-spec entry to `fieldOptionalFloat`**

In `internal/handlers/whatif/form_spec.go`, change the `state_income_tax_rate` entry (lines 114-116):

```go
	{Name: "state_income_tax_rate", Kind: fieldOptionalFloat, ParseLabel: "state income tax rate",
		HasBounds: true, Min: 0, Max: 20,
		BoundsMsg: "State income tax rate must be between 0 and 20%"},
```

- [ ] **Step 5: Update test files that construct rates as `float64`**

Use the helper to keep line-noise low. For each file below, change literal `StateIncomeTaxRate: <num>` lines (in struct literals) to `StateIncomeTaxRate: models.FloatPtr(<num>)`. If the file is already in the `models` package, use `FloatPtr` directly.

Files to touch (the grep found 14 references, but only ~8 distinct test files need construction edits):

```bash
grep -rln "StateIncomeTaxRate:" --include="*_test.go" .
```

Expected file list: `internal/models/models_extra_test.go`, `internal/services/retirement/calculator_state_tax_test.go`, `internal/services/retirement/tax_test.go`, `internal/services/retirement/settings_state_tax_test.go`, `internal/services/retirement/coverage_gaps_test.go`, `internal/services/retirement/coverage_gaps2_test.go`, `internal/services/retirement/coverage_gaps3_test.go`, `internal/services/retirement/calculator_coverage_test.go`, `internal/services/retirement/analysis/rmd_tax_test.go`, `internal/handlers/whatif/completeness_render_test.go`, `internal/services/retirement/completeness/check_test.go`.

Pattern (illustrative — adapt to each file's import path and existing alias):

```go
// before
&models.TaxConfig{StateIncomeTaxRate: 5.0}
// after
&models.TaxConfig{StateIncomeTaxRate: models.FloatPtr(5.0)}
```

For zero literals that should remain "configured 0":
```go
// before
&models.TaxConfig{StateIncomeTaxRate: 0}
// after
&models.TaxConfig{StateIncomeTaxRate: models.FloatPtr(0)}
```

For tests that intend to assert "unset" semantics, leave the field zero-valued (i.e. omit it from the literal so it stays `nil`). Existing tests that just want a TaxConfig and don't care about state tax can also drop the field.

- [ ] **Step 6: Update any test that asserts on `StateIncomeTaxRate` directly**

If any test reads `cfg.StateIncomeTaxRate == 5.0`, change to `cfg.StateIncomeTaxRate != nil && *cfg.StateIncomeTaxRate == 5.0` or use the `StateIncomeTaxRateOrZero()` helper.

```bash
grep -rn "\.StateIncomeTaxRate" --include="*_test.go" . | grep -v ":="
```

Update each match.

- [ ] **Step 7: Build and run all tests**

```bash
go build ./... && go test ./...
```

Expected: PASS. If a test fails because it now sees the `state_tax_unset` finding (because rate is nil where it used to be zero), update that test to use `models.FloatPtr(0)` if its intent was "explicit zero" — or leave nil if its intent was "no rate set".

- [ ] **Step 8: Commit (single atomic commit)**

```bash
git add internal/models/whatif.go internal/models/ptr.go internal/services/retirement/settings.go internal/handlers/whatif/form_spec.go internal/services/retirement/calculator_state_tax_test.go internal/services/retirement/tax_test.go internal/services/retirement/settings_state_tax_test.go internal/services/retirement/coverage_gaps_test.go internal/services/retirement/coverage_gaps2_test.go internal/services/retirement/coverage_gaps3_test.go internal/services/retirement/calculator_coverage_test.go internal/services/retirement/analysis/rmd_tax_test.go internal/handlers/whatif/completeness_render_test.go internal/services/retirement/completeness/check_test.go internal/models/models_extra_test.go
git commit -m "refactor(models): TaxConfig.StateIncomeTaxRate becomes *float64

nil = unset (banner fires), *0 = explicit no-tax state (banner clears),
*x = configured rate. Engine reads through StateIncomeTaxRateOrZero so
tax math is unchanged. Form parsing switches to fieldOptionalFloat so
empty input yields nil, '0' yields &0, '9.3' yields &9.3.

Existing JSON scenarios with 'state_income_tax_rate: 0.0' decode to
&0 (configured), keeping legacy users out of the warning banner.

Adds models.FloatPtr helper for ergonomic test construction.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C5: Update `checkStateTaxUnset` to nil-only

**Files:**
- Modify: `internal/services/retirement/completeness/checks_state_tax.go:16-19`
- Test: `internal/services/retirement/completeness/check_test.go` (append)

- [ ] **Step 1: Write failing tests**

Append to `internal/services/retirement/completeness/check_test.go`:

```go
func TestCheckStateTaxUnset_NilFires(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: nil, // explicit unset
		},
	}
	findings := Check(settings)
	if !hasCode(findings, codeStateTaxUnset) {
		t.Error("nil rate must fire state_tax_unset")
	}
}

func TestCheckStateTaxUnset_ZeroPtrSilent(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: models.FloatPtr(0),
		},
	}
	findings := Check(settings)
	if hasCode(findings, codeStateTaxUnset) {
		t.Error("explicit zero (no-tax state) must NOT fire state_tax_unset")
	}
}

func TestCheckStateTaxUnset_NonzeroSilent(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: models.FloatPtr(9.3),
		},
	}
	findings := Check(settings)
	if hasCode(findings, codeStateTaxUnset) {
		t.Error("non-zero rate must NOT fire state_tax_unset")
	}
}

func TestCheckStateTaxUnset_NilTaxConfigFires(t *testing.T) {
	settings := &models.WhatIfSettings{TaxConfig: nil}
	findings := Check(settings)
	if !hasCode(findings, codeStateTaxUnset) {
		t.Error("nil TaxConfig must fire state_tax_unset")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/services/retirement/completeness/ -run TestCheckStateTaxUnset_ -v
```

Expected: `TestCheckStateTaxUnset_ZeroPtrSilent` FAILS (today the check fires for any rate ≤ 0). The other three may pass already, but the failure proves the change is needed.

- [ ] **Step 3: Update the check**

Replace the body of `checkStateTaxUnset` in `internal/services/retirement/completeness/checks_state_tax.go`:

```go
func checkStateTaxUnset(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig != nil && s.TaxConfig.StateIncomeTaxRate != nil {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeStateTaxUnset,
		Title:      "No state income tax configured",
		Detail:     "Projections currently model federal tax only. If you live in a state with income tax, your after-tax balances are overstated. Enter 0 if you live in a no-income-tax state (FL, TX, WA, etc.).",
		FormAnchor: "state-income-tax-rate-input",
		Action:     "Set state tax rate",
	}
}
```

(The `Detail` text now includes the no-tax-state hint so users in those states immediately understand entering 0 is the right action.)

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/services/retirement/completeness/ -run TestCheckStateTaxUnset_ -v
```

Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/completeness/checks_state_tax.go internal/services/retirement/completeness/check_test.go
git commit -m "feat(completeness): state_tax_unset fires only when rate is nil

After the *float64 change, an explicit 0 is a valid 'no-tax state'
configuration and must not produce a permanent banner. nil (truly
unset) and a nil TaxConfig still fire the warning. Detail text now
hints that 0 is correct for FL/TX/WA-class states.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C6: Template — nullable rate input

**Files:**
- Modify: `web/templates/components/whatif/rate-assumptions.html:154-161`

- [ ] **Step 1: Update the input block**

Replace lines 154-161 (the current State Income Tax Rate `<div>`) with:

```html
        <div>
            <label for="state-income-tax-rate-input" class="block text-sm font-medium text-gray-700 dark:text-gray-200">State Income Tax Rate %</label>
            <input type="number" id="state-income-tax-rate-input" name="state_income_tax_rate"
                value="{{if and .Settings.TaxConfig .Settings.TaxConfig.StateIncomeTaxRate}}{{printf "%.2f" (deref .Settings.TaxConfig.StateIncomeTaxRate)}}{{end}}"
                placeholder="e.g. 0 for FL/TX, 9.3 for CA"
                min="0" max="20" step="0.25"
                class="mt-1 block w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm">
            <p class="text-xs text-gray-400 dark:text-gray-500 mt-1">Flat rate applied to ordinary income. Enter <code>0</code> for no-income-tax states (FL, TX, WA, etc.). Leaving the field blank flags this scenario as unconfigured.</p>
        </div>
```

- [ ] **Step 2: Confirm a `deref` template func exists or add one**

```bash
grep -rn "\"deref\"\\|FuncMap" internal/templates/ 2>/dev/null | head -10
```

If `deref` (or equivalent) is already registered, you're done. Otherwise add it to whichever file builds the template `FuncMap`:

```go
"deref": func(p *float64) float64 {
    if p == nil {
        return 0
    }
    return *p
},
```

(Alternative: avoid the helper entirely by using `{{with .Settings.TaxConfig.StateIncomeTaxRate}}{{printf "%.2f" .}}{{end}}` — Go templates will dereference a non-nil pointer automatically inside `with`. Prefer this if no `FuncMap` change is needed:)

```html
            <input type="number" id="state-income-tax-rate-input" name="state_income_tax_rate"
                value="{{if .Settings.TaxConfig}}{{with .Settings.TaxConfig.StateIncomeTaxRate}}{{printf "%.2f" .}}{{end}}{{end}}"
                placeholder="e.g. 0 for FL/TX, 9.3 for CA"
                ...>
```

Use the `with` form unless there's a clear reason not to.

- [ ] **Step 3: Run handler tests (catches template parse errors)**

```bash
go test ./internal/handlers/whatif/... ./internal/templates/...
```

Expected: PASS.

- [ ] **Step 4: Smoke verify in the browser**

```bash
go run ./cmd/server
```

- Fresh scenario: input is blank (placeholder visible), banner says "No state income tax configured".
- Type `0` → banner clears on next render; input shows `0.00`.
- Clear the input again → banner returns.
- Type `9.3` → banner stays clear; tax math reflects 9.3% (verify the projection table changes).
- Reload an existing JSON scenario whose file contains `"state_income_tax_rate": 0.0` → banner is clear, input shows `0.00`.

If any of those misbehave, stop and diagnose.

- [ ] **Step 5: Commit**

```bash
git add web/templates/components/whatif/rate-assumptions.html
git commit -m "feat(whatif/ui): blank state-tax input when nil; explicit-zero hint

Empty input renders blank with placeholder so users see the rate as
unconfigured. Help text spells out that 0 is correct for FL/TX/WA so
users don't mistake the warning for an instruction to enter a non-zero
number.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task C7: JSON migration regression test

**Files:**
- Test: `internal/services/retirement/settings_state_tax_test.go` (append)

- [ ] **Step 1: Write the test**

This test deserializes only the `TaxConfig` portion (avoids any cross-field dependency on the surrounding `WhatIfSettings` struct shape) and then attaches it to a default scenario for the `Check` call. Append to `internal/services/retirement/settings_state_tax_test.go`:

```go
func TestStateIncomeTaxRate_LegacyJSONRoundTrip(t *testing.T) {
	// Legacy persisted TaxConfig with explicit 0 rate. After the *float64
	// change, this must decode to a non-nil pointer (configured) and
	// produce no state_tax_unset finding when fed to Check.
	const legacyTaxConfigJSON = `{"filing_status":"single","state_income_tax_rate":0.0}`

	var cfg models.TaxConfig
	if err := json.Unmarshal([]byte(legacyTaxConfigJSON), &cfg); err != nil {
		t.Fatalf("unmarshal TaxConfig: %v", err)
	}

	if cfg.StateIncomeTaxRate == nil {
		t.Fatal("StateIncomeTaxRate is nil — legacy 0.0 should decode as configured")
	}
	if got := *cfg.StateIncomeTaxRate; got != 0 {
		t.Errorf("rate = %v, want 0", got)
	}

	settings := models.DefaultWhatIfSettings()
	settings.TaxConfig = &cfg

	findings := completeness.Check(settings)
	for _, f := range findings {
		if f.Code == "state_tax_unset" {
			t.Errorf("legacy explicit-zero scenario must not fire state_tax_unset, got: %+v", f)
		}
	}
}
```

Imports needed: `encoding/json`, `testing`, `budget2/internal/models`, `budget2/internal/services/retirement/completeness` (the test is in the `retirement` package, so the completeness import is external — confirm no import cycle; if there is, move the test under `internal/services/retirement/completeness/check_test.go` and use `models.DefaultWhatIfSettings()` directly).

- [ ] **Step 2: Run the test**

```bash
go test ./internal/services/retirement/ -run TestStateIncomeTaxRate_LegacyJSONRoundTrip -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/services/retirement/settings_state_tax_test.go
git commit -m "test(retirement): regression — legacy JSON 0.0 stays configured

Locks in the migration semantics from the design: existing scenarios
that persisted state_income_tax_rate as 0.0 decode to *float64 pointing
at 0 (configured) and never see the new state_tax_unset banner.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Phase D — Verification + GitNexus

### Task D1: Full regression sweep

- [ ] **Step 1: Run the entire suite**

```bash
go test ./...
```

Expected: PASS across every package.

- [ ] **Step 2: Vet + staticcheck (matches the pre-commit hook)**

```bash
go vet ./...
staticcheck ./...
```

Expected: clean.

- [ ] **Step 3: Re-run the manual smoke list**

Re-perform the smoke checks from B3 Step 3 and C6 Step 4 in a single browser session, going through:
- Fresh scenario → no banner.
- Switch to MFJ on single-person → `mfj_no_spouse_person` Error appears with anchor at the filing-status select.
- Add a spouse Person → Error clears.
- Clear the state-tax input → `state_tax_unset` Warn appears.
- Type `0` → Warn clears.
- Reload a saved scenario file with explicit 0 → Warn does not appear.

If any check fails, do not proceed.

---

### Task D2: GitNexus impact + detect-changes

- [ ] **Step 1: Confirm GitNexus index is fresh**

The pre-commit hook refreshes the index after every commit, so the index should be current. If the user explicitly asks for a manual refresh:

```bash
npx gitnexus analyze
```

- [ ] **Step 2: Check final blast radius via the MCP tools**

In a Claude session attached to this repo, run:

```
gitnexus_detect_changes()
```

Expected: Affected symbols include `DefaultTaxConfig`, `defaultTaxConfigForPersons`, `checkStateTaxUnset`, `applyFieldSpec`, `NewTaxCalculator`, plus the new `StateIncomeTaxRateOrZero` and `FloatPtr`. No surprise affected symbols (e.g. nothing under `dashboard/`, `explorer/`, or `backup/`).

```
gitnexus_impact({target: "checkStateTaxUnset", direction: "upstream"})
```

Expected: 1 caller (`Check` in the same package), risk level LOW or MEDIUM. The `Check` function's blast radius is unchanged.

If `gitnexus_detect_changes` reports symbols outside the spec's File Map, stop and investigate before merging.

---

### Task D3: Final sanity commit (no-op or notes)

- [ ] **Step 1: Confirm tree is clean**

```bash
git status --porcelain
```

Expected: empty (every change committed).

- [ ] **Step 2: Final log review**

```bash
git log --oneline da0f874..HEAD
```

Expected: a tight sequence of commits that read as a coherent story:
- `feat(models): DefaultTaxConfig defaults to FilingSingle`
- `refactor(retirement): invert defaultTaxConfigForPersons after Single-default flip`
- `test(completeness): regression — default settings produce no Error findings`
- `feat(whatif): wire filing_status form field through to TaxConfig`
- `test(whatif): regression — invalid filing_status returns 400`
- `feat(whatif/ui): add filing-status select to Rate Assumptions card`
- `feat(models): add StateIncomeTaxRateOrZero helper`
- `refactor(engine): NewTaxCalculator reads state rate via helper`
- `feat(form_spec): add fieldOptionalFloat parser kind`
- `refactor(models): TaxConfig.StateIncomeTaxRate becomes *float64`
- `feat(completeness): state_tax_unset fires only when rate is nil`
- `feat(whatif/ui): blank state-tax input when nil; explicit-zero hint`
- `test(retirement): regression — legacy JSON 0.0 stays configured`

If any commits are out of order or clearly mis-scoped, do NOT rebase — note it for review and let the human decide.

---

## Done

The branch is ready for the existing review pipeline. The two unfixable banners are gone:
- Fresh single-person scenario: clean (no Error, no Warn unrelated to the choices the user has not yet made).
- FL/TX/WA user enters 0: banner clears immediately and stays clear after reload.
- Legacy scenarios with explicit 0 in JSON: banner stays clear without any migration code.
