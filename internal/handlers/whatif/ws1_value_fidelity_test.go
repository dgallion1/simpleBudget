package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
)

// WS1 — "untouched inputs never rewrite or block the saved plan" (SPEC.md
// Decision D1). These tests guard the two fix classes used across every
// /whatif stored-value input:
//
//  1. A visible <input type=range> that a browser silently snaps to its
//     step grid on load must carry NO name — the exact saved value lives in
//     a same-name sibling <input type=hidden> instead (the W2 Part B
//     mechanism, reused here rather than invented a second time).
//  2. A visible <input type=number> whose stored value can be off a coarse
//     step grid (or lossily rounded by the template) uses step="any" and
//     renders the value via formatExact (render.go), never a %.Nf that
//     throws away precision.
//
// Render tests here assert the MARKUP itself (the only thing that can catch
// "the fix was reverted in the template"); handler round-trip tests assert
// the SERVER behavior (a form posting the exact untouched value must save
// it unchanged). Together they are the permanent regression guard the
// browser-driven oracle (.swarm/tier3/WS1) cannot leave behind, since the
// oracle's own fixture/harness lives outside this repo's test suite.

// extractInputTag returns the substring of html covering the first <input
// ...> tag whose text contains marker (e.g. an id="..." attribute) — enough
// to assert on that one element's other attributes (name, step, value)
// without being thrown off by unrelated tags elsewhere on the page.
func extractInputTag(t *testing.T, html, marker string) string {
	t.Helper()
	idx := strings.Index(html, marker)
	if idx == -1 {
		t.Fatalf("marker %q not found in rendered output:\n%s", marker, truncate(html, 2000))
	}
	start := strings.LastIndex(html[:idx], "<input")
	if start == -1 {
		t.Fatalf("no <input before marker %q", marker)
	}
	endRel := strings.Index(html[start:], ">")
	if endRel == -1 {
		t.Fatalf("no closing '>' after marker %q", marker)
	}
	return html[start : start+endRel+1]
}

// ── Mutation (a): a healthcare cost RANGE must never carry name= ───────────

// TestWS1HealthcarePersonRender_MonthlyCostRangeHasNoName kills the mutation
// "restore name= on a healthcare range": with it restored, the visible
// range becomes the field the browser submits again, and an untouched
// off-grid saved cost (e.g. 1655.30, step=50) gets silently snapped to 1650
// and resubmitted on the next unrelated form change.
func TestWS1HealthcarePersonRender_MonthlyCostRangeHasNoName(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	person := models.HealthcarePerson{
		ID:                    "hc-ws1-render",
		Name:                  "Sam",
		CurrentAge:            58,
		CurrentCoverage:       models.CoverageACA,
		CurrentMonthlyCost:    1655.30,
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150,
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}

	out, err := renderer.RenderToString("whatif-healthcare-person", map[string]any{
		"Settings": settings,
		"Person":   person,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	visible := extractInputTag(t, out, `id="healthcare-monthly-cost-hc-ws1-render"`)
	if strings.Contains(visible, " name=") {
		t.Fatalf("visible healthcare cost range must not carry name= (W2/WS1 snap-trap fix reverted); tag: %s", visible)
	}
	if !strings.Contains(visible, `data-quick-adjust-mirror="true"`) {
		t.Fatalf("visible healthcare cost range must be marked data-quick-adjust-mirror; tag: %s", visible)
	}
	if !strings.Contains(visible, `aria-valuetext="$1,655"`) {
		t.Fatalf("visible healthcare cost range must carry the exact aria-valuetext; tag: %s", visible)
	}

	hidden := extractInputTag(t, out, `id="healthcare-monthly-cost-hc-ws1-render-exact"`)
	if !strings.Contains(hidden, `name="current_monthly_cost"`) {
		t.Fatalf("hidden canonical field must carry name=current_monthly_cost; tag: %s", hidden)
	}
	if !strings.Contains(hidden, `value="1655.3"`) {
		t.Fatalf("hidden canonical field must carry the exact saved value (1655.3), not a rounded one; tag: %s", hidden)
	}
	if !strings.Contains(hidden, `type="hidden"`) {
		t.Fatalf("canonical field must be type=hidden so it is never step-sanitized; tag: %s", hidden)
	}

	careTag := extractInputTag(t, out, `id="care-monthly-cost-hc-ws1-render"`)
	if !strings.Contains(careTag, `step="any"`) {
		t.Fatalf(`care monthly cost number field must use step="any" so an off-grid value (e.g. 4321 on a step=50 grid) never blocks the form; tag: %s`, careTag)
	}
}

// ── Mutation (b): the Cost Basis number field must use step="any" ──────────

// TestWS1RateAssumptionsRender_CostBasisStepIsAny kills the mutation
// "restore step=1000 on cost basis": with it restored, a LIVE off-grid
// basis like 276146.86 fails HTML validation and blocks the entire
// 34-field Rate Assumptions form from ever submitting anything.
func TestWS1RateAssumptionsRender_CostBasisStepIsAny(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	basis := 276146.86
	s.TaxableCostBasis = &basis

	out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	tag := extractInputTag(t, out, `id="taxable-cost-basis-input"`)
	if !strings.Contains(tag, `step="any"`) {
		t.Fatalf(`cost basis input must use step="any" (WS1 fix reverted to a coarse step, which blocks the whole 34-field form on an off-grid basis); tag: %s`, tag)
	}
	if strings.Contains(tag, `step="1000"`) {
		t.Fatalf("cost basis input must not use the old step=1000 grid; tag: %s", tag)
	}
	if !strings.Contains(tag, `value="276146.86"`) {
		t.Fatalf("cost basis value attribute must carry the exact stored figure, not a rounded one; tag: %s", tag)
	}
}

// ── Mutation (c): an allocation % number field must render the exact value ─

// TestWS1RateAssumptionsRender_AllocationPercentExactValue kills the
// mutation "restore %.0f on an allocation %": with it restored, the LIVE
// tax_deferred_percent (83.037) renders as "83" and an untouched submit of
// the 34-field form silently rewrites the saved allocation to 83.
func TestWS1RateAssumptionsRender_AllocationPercentExactValue(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.TaxDeferredPercent = 83.037

	out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	tag := extractInputTag(t, out, `id="tax-deferred-percent-input"`)
	if !strings.Contains(tag, `value="83.037"`) {
		t.Fatalf(`tax_deferred_percent value attribute must carry the exact stored figure (83.037), not a %%.0f-rounded "83" (WS1 fix reverted); tag: %s`, tag)
	}
	if !strings.Contains(tag, `step="any"`) {
		t.Fatalf("tax_deferred_percent input must use step=\"any\"; tag: %s", tag)
	}
}

// ── Family: portfolio-settings.html ─────────────────────────────────────

// TestWS1PortfolioSettingsRoundTripsOffGridValues guards the untouched
// portfolio value slider (step=100000) and the two property-tax number
// fields together — exactly the set of stored values the "Portfolio &
// Expenses" card's own form owns.
func TestWS1PortfolioSettingsRoundTripsOffGridValues(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.PortfolioValue = 2437512.34
	settings.MonthlyLivingExpenses = 10737.45
	settings.MonthlyPropertyTax = 666.67
	settings.PropertyTaxInflation = 2.25
	settings.ProjectionYears = 30
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		// What the fixed hidden inputs submit when neither slider was ever
		// dragged: the exact saved values, not step-snapped ones.
		"portfolio_value":         {"2437512.34"},
		"monthly_living_expenses": {"10737.45"},
		"monthly_property_tax":    {"666.67"},
		"property_tax_inflation":  {"2.25"},
		"projection_years":        {"30"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.PortfolioValue != 2437512.34 {
		t.Errorf("PortfolioValue = %v, want 2437512.34 unchanged", loaded.PortfolioValue)
	}
	if loaded.MonthlyPropertyTax != 666.67 {
		t.Errorf("MonthlyPropertyTax = %v, want 666.67 unchanged", loaded.MonthlyPropertyTax)
	}
	if loaded.PropertyTaxInflation != 2.25 {
		t.Errorf("PropertyTaxInflation = %v, want 2.25 unchanged", loaded.PropertyTaxInflation)
	}
}

// ── Family: healthcare-person.html ──────────────────────────────────────

// TestWS1HealthcareRoundTripsOffGridCostSliders guards the four cost/
// inflation range sliders on an ACA person untouched together.
func TestWS1HealthcareRoundTripsOffGridCostSliders(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                    "hc-ws1-rt",
		Name:                  "Sam",
		CurrentAge:            58,
		CurrentCoverage:       models.CoverageACA,
		CurrentMonthlyCost:    1655.30,
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150,
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson() error: %v", err)
	}

	form := url.Values{
		"current_monthly_cost":    {"1655.3"},
		"pre_medicare_inflation":  {"7.25"},
		"medicare_monthly_cost":   {"2150"},
		"post_medicare_inflation": {"4.15"},
	}
	req := chiRequest(http.MethodPut, "/whatif/healthcare/hc-ws1-rt", formBody(form), map[string]string{"id": "hc-ws1-rt"})
	w := httptest.NewRecorder()
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	var got *models.HealthcarePerson
	for i := range loaded.HealthcarePersons {
		if loaded.HealthcarePersons[i].ID == "hc-ws1-rt" {
			got = &loaded.HealthcarePersons[i]
		}
	}
	if got == nil {
		t.Fatalf("healthcare person hc-ws1-rt not found after update")
	}
	if got.CurrentMonthlyCost != 1655.30 {
		t.Errorf("CurrentMonthlyCost = %v, want 1655.3 unchanged", got.CurrentMonthlyCost)
	}
	if got.PreMedicareInflation != 7.25 {
		t.Errorf("PreMedicareInflation = %v, want 7.25 unchanged", got.PreMedicareInflation)
	}
	if got.MedicareMonthlyCost != 2150 {
		t.Errorf("MedicareMonthlyCost = %v, want 2150 unchanged", got.MedicareMonthlyCost)
	}
	if got.PostMedicareInflation != 4.15 {
		t.Errorf("PostMedicareInflation = %v, want 4.15 unchanged", got.PostMedicareInflation)
	}
}

// ── Family: rate-assumptions.html ───────────────────────────────────────

// TestWS1RateAssumptionsRoundTripsOffGridValues guards the number fields
// that block/rewrite the 34-field form: state tax rate, cost basis, ACA
// credit, taxable-account assumptions and the per-account allocation
// splits — untouched together, the way the real form submits them.
func TestWS1RateAssumptionsRoundTripsOffGridValues(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.TaxDeferredPercent = 83.037
	settings.RothPercent = 0.5
	settings.TaxDeferredStockPercent = 70.5
	settings.TaxDeferredCashPercent = 2.25
	settings.RothStockPercent = 60.5
	settings.RothCashPercent = 1.5
	settings.TaxableStockPercent = 99.5
	settings.TaxableCashPercent = 0.5
	settings.TaxableDividendYield = 0.45
	settings.TaxableQualifiedDividendPercent = 95.5
	settings.TaxableCapitalGainsDistributionRate = 1.25
	basis := 276146.86
	settings.TaxableCostBasis = &basis
	settings.TaxConfig = models.DefaultTaxConfig()
	rate := 5.525
	settings.TaxConfig.StateIncomeTaxRate = &rate
	settings.ACA = &models.ACAConfig{HouseholdSize: 2}
	credit := 10850.0
	settings.ACA.AnnualPremiumTaxCredit = &credit
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		// What the fixed number inputs submit when untouched: the exact
		// saved values (formatExact), not %.Nf-rounded ones.
		"tax_deferred_percent":                {"83.037"},
		"roth_percent":                        {"0.5"},
		"tax_deferred_stock_percent":          {"70.5"},
		"tax_deferred_cash_percent":           {"2.25"},
		"roth_stock_percent":                  {"60.5"},
		"roth_cash_percent":                   {"1.5"},
		"taxable_stock_percent":               {"99.5"},
		"taxable_cash_percent":                {"0.5"},
		"taxable_dividend_yield":              {"0.45"},
		"taxable_qualified_dividend_percent":  {"95.5"},
		"taxable_cap_gains_distribution_rate": {"1.25"},
		"taxable_cost_basis":                  {"276146.86"},
		"state_income_tax_rate":               {"5.525"},
		"aca_household_size":                  {"2"},
		"aca_premium_credit":                  {"10850"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.TaxDeferredPercent != 83.037 {
		t.Errorf("TaxDeferredPercent = %v, want 83.037 unchanged", loaded.TaxDeferredPercent)
	}
	if loaded.TaxDeferredStockPercent != 70.5 {
		t.Errorf("TaxDeferredStockPercent = %v, want 70.5 unchanged", loaded.TaxDeferredStockPercent)
	}
	if loaded.TaxableDividendYield != 0.45 {
		t.Errorf("TaxableDividendYield = %v, want 0.45 unchanged", loaded.TaxableDividendYield)
	}
	if loaded.TaxableCostBasis == nil || *loaded.TaxableCostBasis != 276146.86 {
		t.Errorf("TaxableCostBasis = %v, want 276146.86 unchanged", loaded.TaxableCostBasis)
	}
	if loaded.TaxConfig == nil || loaded.TaxConfig.StateIncomeTaxRate == nil || *loaded.TaxConfig.StateIncomeTaxRate != 5.525 {
		t.Errorf("StateIncomeTaxRate = %v, want 5.525 unchanged", loaded.TaxConfig)
	}
	if loaded.ACA == nil || loaded.ACA.AnnualPremiumTaxCredit == nil || *loaded.ACA.AnnualPremiumTaxCredit != 10850 {
		t.Errorf("ACA.AnnualPremiumTaxCredit = %v, want 10850 unchanged", loaded.ACA)
	}
}

// TestWS1RateAssumptionsRoundTripsOffGridRateSliders guards the second
// form on the same template: inflation / spending-decline / investment-
// return sliders.
func TestWS1RateAssumptionsRoundTripsOffGridRateSliders(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.InflationRate = 3.85
	settings.SpendingDeclineRate = 0.3
	settings.InvestmentReturn = 6.25
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		"inflation_rate":        {"3.85"},
		"spending_decline_rate": {"0.3"},
		"investment_return":     {"6.25"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.InflationRate != 3.85 {
		t.Errorf("InflationRate = %v, want 3.85 unchanged", loaded.InflationRate)
	}
	if loaded.SpendingDeclineRate != 0.3 {
		t.Errorf("SpendingDeclineRate = %v, want 0.3 unchanged", loaded.SpendingDeclineRate)
	}
	if loaded.InvestmentReturn != 6.25 {
		t.Errorf("InvestmentReturn = %v, want 6.25 unchanged", loaded.InvestmentReturn)
	}
}

// ── Family: spending-phases.html ────────────────────────────────────────

// TestWS1SpendingPhasesRoundTripsOffGridMultiplier guards the per-phase
// multiplier range (step=0.05), submitted the way the single phases-form
// submits every phase together.
func TestWS1SpendingPhasesRoundTripsOffGridMultiplier(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0, Description: "Active retirement"},
			{Name: "Active", StartAge: 65, Multiplier: 0.93, Description: "Pacing slows"},
		},
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		"enabled":             {"true"},
		"phase_0_name":        {"Go-Go"},
		"phase_0_description": {"Active retirement"},
		"phase_0_multiplier":  {"1"},
		"phase_1_name":        {"Active"},
		"phase_1_description": {"Pacing slows"},
		"phase_1_start_age":   {"65"},
		"phase_1_multiplier":  {"0.93"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.SpendingPhaseConfig == nil || len(loaded.SpendingPhaseConfig.Phases) != 2 {
		t.Fatalf("SpendingPhaseConfig = %+v, want 2 phases", loaded.SpendingPhaseConfig)
	}
	if got := loaded.SpendingPhaseConfig.Phases[1].Multiplier; got != 0.93 {
		t.Errorf("Phases[1].Multiplier = %v, want 0.93 unchanged", got)
	}
}

// ── Family: guardrails.html ──────────────────────────────────────────────

// TestWS1GuardrailsRoundTripsOffGridPercents guards the six guardrail
// percent number fields untouched together.
func TestWS1GuardrailsRoundTripsOffGridPercents(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.Guardrails = &models.GuardrailConfig{
		Enabled:                true,
		MinMonthlySpendingReal: 6000.55,
		FloorDropPct:           5.5,
		FloorCutPct:            10.25,
		CeilingRisePct:         10.5,
		CeilingRaisePct:        2.5,
		MinSpendingPct:         0.5,
		MaxSpendingPct:         120.5,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		"enabled":                   {"true"},
		"min_monthly_spending_real": {"6000.55"},
		"floor_drop_pct":            {"5.5"},
		"floor_cut_pct":             {"10.25"},
		"ceiling_rise_pct":          {"10.5"},
		"ceiling_raise_pct":         {"2.5"},
		"min_spending_pct":          {"0.5"},
		"max_spending_pct":          {"120.5"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	g := loaded.Guardrails
	if g == nil {
		t.Fatalf("Guardrails is nil after update")
	}
	cases := map[string]struct{ got, want float64 }{
		"FloorDropPct":    {g.FloorDropPct, 5.5},
		"FloorCutPct":     {g.FloorCutPct, 10.25},
		"CeilingRisePct":  {g.CeilingRisePct, 10.5},
		"CeilingRaisePct": {g.CeilingRaisePct, 2.5},
		"MinSpendingPct":  {g.MinSpendingPct, 0.5},
		"MaxSpendingPct":  {g.MaxSpendingPct, 120.5},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v unchanged", name, c.got, c.want)
		}
	}
}

// ── Family: roth-conversion.html ────────────────────────────────────────

// TestWS1RothConversionRoundTripsOffGridAmount guards the annual amount
// number field.
func TestWS1RothConversionRoundTripsOffGridAmount(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000.5,
		StartYear:    0,
		EndYear:      0,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"50000.5"},
		"start_year":    {"0"},
		"end_year":      {"0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.RothConversion == nil || loaded.RothConversion.AnnualAmount != 50000.5 {
		t.Errorf("RothConversion.AnnualAmount = %v, want 50000.5 unchanged", loaded.RothConversion)
	}
}

// ── Family: social-security.html ────────────────────────────────────────

// TestWS1SocialSecurityRoundTripsOffGridBenefits guards the FRA benefit,
// spouse FRA benefit, and COLA rate number fields untouched together.
func TestWS1SocialSecurityRoundTripsOffGridBenefits(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       4114.9,
		FRA:              67,
		COLARate:         0.0245,
		COLARateSet:      true,
		SpouseFRABenefit: 1905.55,
		SpouseFRA:        67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	form := url.Values{
		"fra_benefit":        {"4114.9"},
		"fra":                {"67"},
		"cola_rate":          {"2.45"},
		"spouse_fra_benefit": {"1905.55"},
		"spouse_fra":         {"67"},
		"claim_age":          {""},
		"spouse_claim_age":   {""},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	ss := loaded.SocialSecurity
	if ss == nil {
		t.Fatalf("SocialSecurity is nil after update")
	}
	if ss.FRABenefit != 4114.9 {
		t.Errorf("FRABenefit = %v, want 4114.9 unchanged", ss.FRABenefit)
	}
	if ss.SpouseFRABenefit != 1905.55 {
		t.Errorf("SpouseFRABenefit = %v, want 1905.55 unchanged", ss.SpouseFRABenefit)
	}
	if got, want := ss.COLARate, 0.0245; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("COLARate = %v, want ~0.0245 unchanged (within float noise from the *100/÷100 round trip)", got)
	}
}
