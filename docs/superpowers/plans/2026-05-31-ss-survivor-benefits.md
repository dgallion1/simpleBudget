# Social Security Survivor Benefits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface Social Security survivor-benefit information on the what-if Claiming Comparison card — a per-claim-age survivor benefit (RIB-LIM floored) on the higher earner's table plus a callout quantifying the gain from delaying to 70.

**Architecture:** Pure helper in `analysis/ss.go` computes the survivor record amount for a claim age; `SSAnalysis` wires it onto the higher-PIA worker's option slice and a household summary on `SSComparisonAnalysis`; the SS template renders a "Survivor" column and a callout. Inform-only — `BestAge`/cumulative columns and the projection are untouched. No mortality assumption.

**Tech Stack:** Go 1.26, `html/template`, existing `analysis` + `handlers/whatif` test harnesses.

**Spec:** `docs/superpowers/specs/2026-05-31-ss-survivor-benefits-design.md`

---

## File Structure

- `internal/services/retirement/analysis/ss.go` — add `SurvivorBenefitForClaimAge` helper and survivor wiring in `SSAnalysis`.
- `internal/models/whatif.go` — add survivor fields to `SSClaimingOption` and `SSComparisonAnalysis`.
- `web/templates/components/whatif/social-security.html` — survivor column (gated per higher earner) + callout.
- `internal/services/retirement/analysis/ss_test.go` — helper + wiring unit tests.
- `internal/handlers/whatif/ss_survivor_render_test.go` (new) — render assertions.

---

## Task 1: `SurvivorBenefitForClaimAge` helper

**Files:**
- Modify: `internal/services/retirement/analysis/ss.go`
- Test: `internal/services/retirement/analysis/ss_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/services/retirement/analysis/ss_test.go`:

```go
func TestSurvivorBenefitForClaimAge(t *testing.T) {
	const pia = 2000.0
	const fra = 67

	// Claim at 62 with FRA 67: own benefit reduced 30% → $1400, which is
	// below the RIB-LIM survivor floor of 82.5%·PIA = $1650. Floor applies.
	if got := SurvivorBenefitForClaimAge(pia, fra, 62); !ssWithinTolerance(got, 0.825*pia, 0.01) {
		t.Errorf("age 62: got %.2f, want RIB-LIM floor %.2f", got, 0.825*pia)
	}

	// Claim at 66 (12 months early): reduced 6.667% → $1866.67, above the
	// floor, so the survivor inherits the (reduced) actual benefit.
	want66 := AdjustedSSBenefit(pia, fra, 66)
	if got := SurvivorBenefitForClaimAge(pia, fra, 66); !ssWithinTolerance(got, want66, 0.01) {
		t.Errorf("age 66 (above floor): got %.2f, want adjusted %.2f", got, want66)
	}

	// Claim at FRA: exactly PIA.
	if got := SurvivorBenefitForClaimAge(pia, fra, 67); !ssWithinTolerance(got, pia, 0.01) {
		t.Errorf("FRA: got %.2f, want %.2f", got, pia)
	}

	// Claim at 70: delayed-retirement credits 8%/yr × 3 = 24% → $2480,
	// which the survivor inherits.
	if got := SurvivorBenefitForClaimAge(pia, fra, 70); !ssWithinTolerance(got, pia*1.24, 0.01) {
		t.Errorf("age 70: got %.2f, want %.2f", got, pia*1.24)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/retirement/analysis/ -run TestSurvivorBenefitForClaimAge`
Expected: FAIL — `undefined: SurvivorBenefitForClaimAge` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/services/retirement/analysis/ss.go`, add after `AdjustedSpousalBenefit` (i.e., near the other benefit helpers, before `SpousalTopUp` is fine):

```go
// SurvivorBenefitForClaimAge returns the monthly Social Security survivor
// benefit a worker's record produces if claimed at claimAge, in current
// (claim-date) dollars. Per 20 CFR §404.338, a record claimed before FRA
// floors the survivor benefit at 82.5% of PIA (RIB-LIM); claimed at or
// after FRA the survivor inherits the full benefit, including any
// delayed-retirement credits. The survivor inherits the larger of their
// own benefit and this amount; callers apply it to the higher-PIA worker.
func SurvivorBenefitForClaimAge(pia float64, fra, claimAge int) float64 {
	fra = NormalizedSSFRA(fra)
	adjusted := AdjustedSSBenefit(pia, fra, claimAge)
	if claimAge < fra {
		return math.Max(adjusted, 0.825*pia)
	}
	return adjusted
}
```

(`math` is already imported in `ss.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/retirement/analysis/ -run TestSurvivorBenefitForClaimAge -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/analysis/ss.go internal/services/retirement/analysis/ss_test.go
git commit -m "feat(whatif): add SurvivorBenefitForClaimAge helper with RIB-LIM floor

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Model fields + `SSAnalysis` survivor wiring

**Files:**
- Modify: `internal/models/whatif.go:1427-1455` (the two structs)
- Modify: `internal/services/retirement/analysis/ss.go` (`SSAnalysis`, before `return result`)
- Test: `internal/services/retirement/analysis/ss_test.go`

- [ ] **Step 1: Add the model fields**

In `internal/models/whatif.go`, add to `SSClaimingOption` (after `CumulativeAt90`):

```go
	SurvivorMonthlyBenefit float64 `json:"survivor_monthly_benefit,omitempty"`
```

And add to `SSComparisonAnalysis` (after `SpouseEarlyClaimGapPct`, before `Portfolio`):

```go
	HasSurvivorAnalysis          bool    `json:"has_survivor_analysis,omitempty"`
	HasSurvivorCallout           bool    `json:"has_survivor_callout,omitempty"`
	SurvivorHigherEarnerIsSpouse bool    `json:"survivor_higher_earner_is_spouse,omitempty"`
	SurvivorSelectedClaimAge     int     `json:"survivor_selected_claim_age,omitempty"`
	SurvivorSelectedAgeLocked    bool    `json:"survivor_selected_age_locked,omitempty"`
	SurvivorBenefitAtSelected    float64 `json:"survivor_benefit_at_selected,omitempty"`
	SurvivorBenefitAt70          float64 `json:"survivor_benefit_at_70,omitempty"`
	SurvivorDelayGainPct         float64 `json:"survivor_delay_gain_pct,omitempty"`
	HasSurvivorDelayUpside       bool    `json:"has_survivor_delay_upside,omitempty"`
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/services/retirement/analysis/ss_test.go`:

```go
func ssSurvivorSettings(primaryFRA, spouseFRA float64) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 60
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
		{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: primaryFRA, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: spouseFRA, SpouseFRA: 67, SpouseClaimAge: 67,
		COLARate: 0.02, COLARateSet: true,
	}
	return s
}

func TestSSAnalysis_Survivor_PrimaryHigher(t *testing.T) {
	res := SSAnalysis(engineInput(t, ssSurvivorSettings(3000, 1500)))
	if res == nil {
		t.Fatal("nil analysis")
	}
	if !res.HasSurvivorAnalysis {
		t.Fatal("expected HasSurvivorAnalysis=true")
	}
	if res.SurvivorHigherEarnerIsSpouse {
		t.Error("primary is higher earner; SurvivorHigherEarnerIsSpouse should be false")
	}
	if len(res.Options) == 0 || res.Options[0].SurvivorMonthlyBenefit == 0 {
		t.Error("expected SurvivorMonthlyBenefit populated on primary Options")
	}
	for _, o := range res.SpouseOptions {
		if o.SurvivorMonthlyBenefit != 0 {
			t.Error("spouse options must not carry survivor benefit when primary is higher")
		}
	}
	if !res.HasSurvivorCallout || !res.HasSurvivorDelayUpside {
		t.Fatalf("expected callout with delay upside; callout=%v upside=%v", res.HasSurvivorCallout, res.HasSurvivorDelayUpside)
	}
	if res.SurvivorSelectedClaimAge != 67 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 67", res.SurvivorSelectedClaimAge)
	}
	wantAt70 := SurvivorBenefitForClaimAge(3000, 67, 70)
	if !ssWithinTolerance(res.SurvivorBenefitAt70, math.Round(wantAt70*100)/100, 0.01) {
		t.Errorf("SurvivorBenefitAt70 = %v, want %v", res.SurvivorBenefitAt70, wantAt70)
	}
	if res.SurvivorDelayGainPct <= 0 {
		t.Errorf("expected positive SurvivorDelayGainPct, got %v", res.SurvivorDelayGainPct)
	}
}

func TestSSAnalysis_Survivor_SpouseHigher(t *testing.T) {
	res := SSAnalysis(engineInput(t, ssSurvivorSettings(1500, 3000)))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis")
	}
	if !res.SurvivorHigherEarnerIsSpouse {
		t.Error("spouse is higher earner; flag should be true")
	}
	if len(res.SpouseOptions) == 0 || res.SpouseOptions[0].SurvivorMonthlyBenefit == 0 {
		t.Error("expected SurvivorMonthlyBenefit populated on SpouseOptions")
	}
	for _, o := range res.Options {
		if o.SurvivorMonthlyBenefit != 0 {
			t.Error("primary options must not carry survivor benefit when spouse is higher")
		}
	}
}

func TestSSAnalysis_Survivor_SingleFiler(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 0
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67}
	res := SSAnalysis(engineInput(t, s))
	if res == nil {
		t.Fatal("nil analysis")
	}
	if res.HasSurvivorAnalysis {
		t.Error("single filer must not have survivor analysis")
	}
}

func TestSSAnalysis_Survivor_AnalysisOnlyClaimAge(t *testing.T) {
	s := ssSurvivorSettings(3000, 1500)
	s.SocialSecurity.ClaimAge = 0 // "Analysis only" — no selected age
	res := SSAnalysis(engineInput(t, s))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis (column still populated)")
	}
	if res.HasSurvivorCallout {
		t.Error("unset claim age must suppress the callout")
	}
	if res.SurvivorSelectedClaimAge != 0 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 0", res.SurvivorSelectedClaimAge)
	}
	if len(res.Options) == 0 || res.Options[0].SurvivorMonthlyBenefit == 0 {
		t.Error("survivor column should still be populated for the higher earner")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestSSAnalysis_Survivor'`
Expected: FAIL — assertions fail because `SSAnalysis` does not yet set the survivor fields (`HasSurvivorAnalysis=false`).

- [ ] **Step 4: Implement the wiring**

In `internal/services/retirement/analysis/ss.go`, inside `SSAnalysis`, insert this block immediately before the final `return result` (after the existing `if len(spouseOptions) > 0 { ... }` block):

```go
	// Survivor benefits: the surviving spouse inherits the larger of the
	// two benefits, so the higher-PIA worker's record drives the survivor
	// floor. Inform-only — does not affect BestAge or the cumulative
	// columns. See docs/superpowers/specs/2026-05-31-ss-survivor-benefits-design.md.
	if s.HasSpouse() && primaryPIA > 0 && spousePIA > 0 && len(spouseOptions) > 0 {
		result.HasSurvivorAnalysis = true

		higherPIA := primaryPIA
		higherFRA := fra
		selectedClaimAge := ss.ClaimAge
		higherCurrentAge := s.CurrentAge
		survivorOptions := result.Options
		if spousePIA > primaryPIA {
			result.SurvivorHigherEarnerIsSpouse = true
			higherPIA = spousePIA
			higherFRA = spouseFRA
			selectedClaimAge = ss.SpouseClaimAge
			higherCurrentAge = s.SpouseAge
			if higherCurrentAge == 0 {
				higherCurrentAge = s.CurrentAge
			}
			survivorOptions = result.SpouseOptions
		}

		for i := range survivorOptions {
			benefit := SurvivorBenefitForClaimAge(higherPIA, higherFRA, survivorOptions[i].ClaimAge)
			survivorOptions[i].SurvivorMonthlyBenefit = math.Round(benefit*100) / 100
		}

		if ValidSSClaimAge(selectedClaimAge) {
			result.HasSurvivorCallout = true
			result.SurvivorSelectedClaimAge = selectedClaimAge

			atSelected := SurvivorBenefitForClaimAge(higherPIA, higherFRA, selectedClaimAge)
			at70 := SurvivorBenefitForClaimAge(higherPIA, higherFRA, 70)
			result.SurvivorBenefitAtSelected = math.Round(atSelected*100) / 100
			result.SurvivorBenefitAt70 = math.Round(at70*100) / 100

			result.SurvivorSelectedAgeLocked = selectedClaimAge <= higherCurrentAge
			if !result.SurvivorSelectedAgeLocked && selectedClaimAge < 70 && atSelected > 0 {
				result.HasSurvivorDelayUpside = true
				result.SurvivorDelayGainPct = math.Round((at70-atSelected)/atSelected*1000) / 10
			}
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestSSAnalysis_Survivor|TestSurvivorBenefitForClaimAge' -v`
Expected: PASS (all four wiring tests + the helper test).

- [ ] **Step 6: Run the existing SS suite to confirm no regression**

Run: `go test ./internal/services/retirement/analysis/ -run 'TestRunSSAnalysis'`
Expected: PASS — `BestAge`/`SpouseBestAge`/cumulative behavior unchanged.

- [ ] **Step 7: Commit**

```bash
git add internal/models/whatif.go internal/services/retirement/analysis/ss.go internal/services/retirement/analysis/ss_test.go
git commit -m "feat(whatif): wire survivor benefits into SSAnalysis

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Template column + callout

**Files:**
- Modify: `web/templates/components/whatif/social-security.html`
- Test (new): `internal/handlers/whatif/ss_survivor_render_test.go`

- [ ] **Step 1: Write the failing render test**

Create `internal/handlers/whatif/ss_survivor_render_test.go`:

```go
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// ssRenderData builds the data map the "whatif-social-security-results"
// template expects: .Analysis.SocialSecurity and .Settings.
func ssRenderData(ss *models.SSComparisonAnalysis, settings *models.WhatIfSettings) map[string]interface{} {
	return map[string]interface{}{
		"Analysis": map[string]interface{}{"SocialSecurity": ss},
		"Settings": settings,
	}
}

func ssRenderSettings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		SpouseAge: 60,
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3000, FRA: 67, ClaimAge: 67,
			SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 67,
			COLARate: 0.02,
		},
	}
}

func TestSSSurvivor_RendersColumnForPrimaryHigher(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		SpouseOptions: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 1500},
		},
		HasSurvivorAnalysis:       true,
		HasSurvivorCallout:        true,
		SurvivorSelectedClaimAge:  67,
		SurvivorBenefitAtSelected: 3000,
		SurvivorBenefitAt70:       3720,
		SurvivorDelayGainPct:      24,
		HasSurvivorDelayUpside:    true,
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Survivor") {
		t.Errorf("expected a Survivor column/callout; got: %s", out)
	}
	if !strings.Contains(out, "You are the higher earner") {
		t.Errorf("expected higher-earner callout; got: %s", out)
	}
	if !strings.Contains(out, "to 70") {
		t.Errorf("expected delay-to-70 callout copy; got: %s", out)
	}
}

func TestSSSurvivor_RendersColumnForSpouseHigher(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 1500},
		},
		SpouseOptions: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		HasSurvivorAnalysis:          true,
		SurvivorHigherEarnerIsSpouse: true,
		HasSurvivorCallout:           true,
		SurvivorSelectedClaimAge:     67,
		SurvivorBenefitAtSelected:    3000,
		SurvivorBenefitAt70:          3720,
		SurvivorDelayGainPct:         24,
		HasSurvivorDelayUpside:       true,
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Survivor") {
		t.Errorf("expected a Survivor column; got: %s", out)
	}
	if !strings.Contains(out, "Your spouse is the higher earner") {
		t.Errorf("expected spouse-higher callout copy; got: %s", out)
	}
}

func TestSSSurvivor_AnalysisOnlySuppressesCallout(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		SpouseOptions:       []models.SSClaimingOption{{ClaimAge: 67, MonthlyBenefit: 1500}},
		HasSurvivorAnalysis: true,
		HasSurvivorCallout:  false, // unset claim age
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if strings.Contains(out, "higher earner") {
		t.Errorf("callout must be suppressed when HasSurvivorCallout=false; got: %s", out)
	}
	if strings.Contains(out, "age 0") {
		t.Errorf("must not render 'age 0' copy; got: %s", out)
	}
}
```

- [ ] **Step 2: Run the render test to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestSSSurvivor`
Expected: FAIL — the rendered output does not contain "Survivor" / the callout copy yet.

- [ ] **Step 3: Add the callout to the template**

In `web/templates/components/whatif/social-security.html`, immediately after the heading line `<h3 ...>Social Security Claiming Comparison</h3>` (line 121), insert:

```html
    {{if .Analysis.SocialSecurity.HasSurvivorCallout}}
    {{$ss := .Analysis.SocialSecurity}}
    <div class="text-xs bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-2 mb-3 text-gray-700 dark:text-gray-200">
        {{if $ss.SurvivorHigherEarnerIsSpouse}}Your spouse is{{else}}You are{{end}} the higher earner — this benefit becomes the surviving spouse's for life.
        {{if $ss.HasSurvivorDelayUpside}}
        Delaying from age {{$ss.SurvivorSelectedClaimAge}} to 70 raises the survivor benefit from ${{formatNumber $ss.SurvivorBenefitAtSelected}} to ${{formatNumber $ss.SurvivorBenefitAt70}}/mo (+{{printf "%.0f" $ss.SurvivorDelayGainPct}}%).
        {{else}}
        The surviving spouse's benefit is ${{formatNumber $ss.SurvivorBenefitAtSelected}}/mo.
        {{end}}
    </div>
    {{end}}
```

- [ ] **Step 4: Add the survivor column to the PRIMARY table**

In the primary table `<thead>` (after the `Monthly` header, line 129), insert:

```html
                    {{if and .Analysis.SocialSecurity.HasSurvivorAnalysis (not .Analysis.SocialSecurity.SurvivorHigherEarnerIsSpouse)}}
                    <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Survivor</th>
                    {{end}}
```

In the primary table row, after the `Monthly` cell (line 145), insert:

```html
                    {{if and $.Analysis.SocialSecurity.HasSurvivorAnalysis (not $.Analysis.SocialSecurity.SurvivorHigherEarnerIsSpouse)}}
                    <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300">${{formatNumber .SurvivorMonthlyBenefit}}</td>
                    {{end}}
```

- [ ] **Step 5: Add the survivor column to the SPOUSE table**

In the spouse table `<thead>` (after the `Monthly` header, line 184), insert:

```html
                        {{if and .Analysis.SocialSecurity.HasSurvivorAnalysis .Analysis.SocialSecurity.SurvivorHigherEarnerIsSpouse}}
                        <th class="text-right py-2 px-2 text-gray-500 dark:text-gray-400 font-medium">Survivor</th>
                        {{end}}
```

In the spouse table row, after the `Monthly` cell (line 200), insert:

```html
                        {{if and $.Analysis.SocialSecurity.HasSurvivorAnalysis $.Analysis.SocialSecurity.SurvivorHigherEarnerIsSpouse}}
                        <td class="py-2 px-2 text-right text-gray-600 dark:text-gray-300">${{formatNumber .SurvivorMonthlyBenefit}}</td>
                        {{end}}
```

- [ ] **Step 6: Run the render test to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestSSSurvivor -v`
Expected: PASS (both render tests).

- [ ] **Step 7: Commit**

```bash
git add web/templates/components/whatif/social-security.html internal/handlers/whatif/ss_survivor_render_test.go
git commit -m "feat(whatif): render survivor-benefit column and callout on SS card

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Full verification gate

**Files:** none (verification only)

- [ ] **Step 1: Build, vet, test, staticcheck**

Run:
```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./internal/services/retirement/...
```
Expected: all pass; no staticcheck output.

- [ ] **Step 2: Confirm the diff matches intent**

Run: `git diff master --stat`
Expected: only `ss.go`, `whatif.go`, `social-security.html`, `ss_test.go`, `ss_survivor_render_test.go`, and the two design/plan docs.

- [ ] **Step 3: Manual visual check (optional but recommended)**

Use the `run` skill (or `verify`) to launch the app, open the what-if page, enter two SS benefits with different amounts and a claim age below 70 for the higher earner, and confirm the Survivor column and callout appear on the higher earner's table. Toggle which spouse is higher and confirm the column moves.

---

## Self-Review notes (for the implementer)

- The survivor figure attaches to the higher-PIA worker's slice only; the lower earner's table shows no survivor column (by design — the lower earner's claim age does not change the survivor floor).
- `result.Options`/`result.SpouseOptions` share backing arrays with `options`/`spouseOptions`; mutating elements in place is intentional and safe (the slices are local to this call).
- Do not touch `BestAge`, `SpouseBestAge`, or the cumulative columns — the regression check in Task 2 Step 6 guards this.
- Field/method names used here exactly match definitions: `SurvivorBenefitForClaimAge`, `SurvivorMonthlyBenefit`, `HasSurvivorAnalysis`, `HasSurvivorCallout`, `SurvivorHigherEarnerIsSpouse`, `SurvivorSelectedClaimAge`, `SurvivorSelectedAgeLocked`, `SurvivorBenefitAtSelected`, `SurvivorBenefitAt70`, `SurvivorDelayGainPct`, `HasSurvivorDelayUpside`.
