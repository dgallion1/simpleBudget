//go:build financegaps

// Package engine gap suite.
//
// These tests encode claims from FINANCEAPPCONCERNS.md that this engine
// does NOT currently satisfy. They are expected to FAIL. Each failure
// message names the concern, what was measured, and what capability is
// missing — so the suite reads as a live gap ledger rather than noise.
//
// Run them deliberately:
//
//	go test -tags financegaps ./internal/services/retirement/engine/ -run TestFinanceGap -v
//
// The `financegaps` build tag keeps them out of `go test ./...`, so the
// default suite stays green while the gap report stays one command away.
// Delete a test from this file when the gap it documents is closed — and
// move its assertion into finance_concerns_golden_test.go.
package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// TestFinanceGap_TaxableAccountHasNoLots
//
// FINANCEAPPCONCERNS.md §2: "Ingest and store lots, not positions. Every tax
// projection is a function of a selected set of lots, never of a position."
// The document measures a ~10x error from blended position-level gain.
//
// PARTIALLY CLOSED. The seeding half is fixed: the account no longer starts
// with CostBasis == MarketValue, because scenarios can now supply a real
// TaxableCostBasis (see engine/taxable_cost_basis_test.go and the completeness
// warning for scenarios that leave it unset).
//
// What remains is lot *selection*. TaxableAccountState still holds one blended
// basis and Withdraw recognises gain pro-rata, so choosing which lots to sell
// cannot change the tax.
//
// Note this is deliberate, not an oversight. Pro-rata against a blended basis
// is average-cost, which is the correct expected-value treatment for a
// simulated homogeneous reinvesting account. Real lot selection belongs in a
// single-year "what can I realize this year, and from which lots" calculation
// — not inside the monthly projection loop, where
// ExecuteTaxAwarePortfolioMonth's fixed-point iteration copies the account by
// value up to six times a month across every Monte Carlo path. A lot slice
// there would alias its backing array between trial iterations and would need
// a deep copy per trial to be correct.
func TestFinanceGap_TaxableAccountHasNoLots(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	const marketValue = 500000.0

	// The seeding half is closed: a configured basis is honoured.
	s.TaxableCostBasis = models.FloatPtr(250000)
	seeded := NewTaxableAccountState(s, marketValue)
	if seeded.CostBasis != 250000 {
		t.Fatalf("regression: configured cost basis was not honoured (got %.2f, want 250000)",
			seeded.CostBasis)
	}

	// The selection half is open. Two callers selling the same dollar amount
	// realize identical gain no matter which lots they would have chosen,
	// because there is no API to choose.
	a := NewTaxableAccountState(s, marketValue)
	b := NewTaxableAccountState(s, marketValue)
	_, _, gainA := a.Withdraw(34667)
	_, _, gainB := b.Withdraw(34667)

	if gainA != gainB {
		t.Fatalf("unexpected: identical withdrawals produced different gains (%.2f vs %.2f)",
			gainA, gainB)
	}

	// The tax model can now price short-term gain correctly (see
	// short_term_gains_test.go); what is missing is anything that produces it,
	// because holding period is exactly what lots would carry.
	tc := fcCalculator(t)
	shortTermIsPriced := tc.CalculateTaxBreakdown(
		InvestmentIncomeTaxInputs{OrdinaryIncome: 90000, ShortTermCapitalGains: 20000}, 0).TotalTax >
		tc.CalculateTaxBreakdown(
			InvestmentIncomeTaxInputs{OrdinaryIncome: 90000, LongTermCapitalGains: 20000}, 0).TotalTax
	if !shortTermIsPriced {
		t.Fatal("regression: the tax model no longer distinguishes short-term from long-term gain")
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §2, selection half): Withdraw recognises "+
		"%.2f of gain pro-rata against a blended basis.\n"+
		"  Still missing: individual lots (open date, quantity, cost/share) and a\n"+
		"  cost-basis method (FIFO / LIFO / HighCost / LowCost / SpecID /\n"+
		"  TaxLotOptimizer).\n"+
		"  The holding-period half is now half-closed: the tax model prices\n"+
		"  short-term gain as ordinary income, but nothing in the projection ever\n"+
		"  produces any, because average-cost accounting has no notion of when a\n"+
		"  dollar was bought. Lots are what would carry that.\n"+
		"  The document's case realized $1,663 of gain on $34,667 of proceeds by\n"+
		"  picking lots, against ~10x that blended. Reproducing it needs a\n"+
		"  single-year lot-selection tool, not lots in the projection loop.", gainA)
}

// TestFinanceGap_ACACreditNotPricedIntoTheProjection
//
// FINANCEAPPCONCERNS.md §5 and §8. The premium tax credit phases out at 400%
// FPL as a cliff, COBRA forfeits it at any income, and advance credits are
// clawed back — uncapped — above the cliff.
//
// PARTIALLY CLOSED. The household facts are modelled (size, credit, advance
// flag, COBRA as its own coverage type), ACA MAGI is derived on its own terms
// counting all Social Security, the poverty guidelines are versioned with the
// open-enrolment lookback, and the cliff is registered and surfaced with its
// proximity.
//
// What remains is that none of it reaches the CASH FLOW. Marketplace cost is
// still a flat monthly figure inflating at a fixed rate, independent of
// income, so a projection that crosses the cliff shows the household losing
// nothing. The plan warns about the cliff; it does not yet pay for it.
func TestFinanceGap_ACACreditNotPricedIntoTheProjection(t *testing.T) {
	// The cliff is genuinely registered now — confirm before complaining.
	tc := fcCalculator(t)
	cfg := &models.ACAConfig{HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600)}
	registered := false
	for _, th := range tc.ThresholdRegistry(ThresholdRegistryOptions{
		CoverageYear: 2025, ACA: cfg, MarketplaceEnrolled: true,
	}) {
		if th.Code == "aca_premium_credit_cliff" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("regression: the ACA cliff is no longer registered")
	}

	// But healthcare cost still ignores income entirely.
	poor := models.NewHealthcarePerson("under the cliff", 60, models.CoverageACA)
	rich := models.NewHealthcarePerson("over the cliff", 60, models.CoverageACA)
	if poor.GetMonthlyCost(0) != rich.GetMonthlyCost(0) {
		t.Fatal("unexpected: marketplace cost already varies with something")
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §8, cash-flow half): marketplace cost is still "+
		"$%.0f/mo for every household at every income.\n"+
		"  The cliff is located, priced and surfaced, but crossing it does not change a\n"+
		"  single projected dollar: the credit is not subtracted from healthcare cost,\n"+
		"  losing it does not add cost back, and advance-credit repayment never appears\n"+
		"  as the lump it is.\n"+
		"  Doing this properly needs the credit to vary with each year's ACA MAGI, which\n"+
		"  needs the benchmark silver plan for the household's rating area and the age of\n"+
		"  every enrollee — local data this planner does not carry. A national benchmark\n"+
		"  would be confidently wrong everywhere.\n"+
		"  Also still absent: the § 36B applicable-percentage table, so the credit cannot\n"+
		"  be derived even given a benchmark premium.", poor.GetMonthlyCost(0))
}

// TestFinanceGap_StateTaxCannotExcludeSocialSecurity
//
// FINANCEAPPCONCERNS.md §3: NY AGI "Excludes Social Security entirely".
//
// CalculateStateTax applies one flat rate to federal taxable income. It takes
// no jurisdiction and no income composition, so two households with identical
// federal taxable income pay identical state tax even when one of them is
// living on Social Security that New York does not tax at all.
func TestFinanceGap_StateTaxCannotExcludeSocialSecurity(t *testing.T) {
	nyRate := 5.5
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &nyRate,
		Age65Count:         1,
	}, 0)

	const targetOrdinary = 60000.0

	// Household A: $60,000 of wages, no Social Security.
	// Household B: wages plus $32,919 gross Social Security, with wages solved
	// so that B's federal ordinary income is also exactly $60,000. Same federal
	// taxable income, different composition.
	wagesB := targetOrdinary
	var taxableSS float64
	for i := 0; i < 200; i++ {
		taxableSS = CalculateTaxableSocialSecurity(
			fcSSGross, wagesB, 0, 0, models.FilingMarriedJoint, false)
		next := targetOrdinary - taxableSS
		if math.Abs(next-wagesB) < 1e-9 {
			break
		}
		wagesB = next
	}
	if math.Abs(wagesB+taxableSS-targetOrdinary) > 0.01 {
		t.Fatalf("fixture did not converge: wages %.2f + taxable SS %.2f != %.2f",
			wagesB, taxableSS, targetOrdinary)
	}

	_, stateA, _, _ := tc.CalculateTaxWithInvestmentIncome(targetOrdinary, 0, 0, 0)
	_, stateB, _, _ := tc.CalculateTaxWithInvestmentIncome(wagesB+taxableSS, 0, 0, 0)

	if math.Abs(stateA-stateB) > 0.01 {
		t.Fatalf("unexpected: state tax already distinguishes the two (%.2f vs %.2f)", stateA, stateB)
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §3): both households pay $%.2f of state tax "+
		"on $%.0f of federal ordinary income, but household B's share is $%.2f of\n"+
		"  wages and $%.2f of taxable Social Security.\n"+
		"  New York excludes Social Security from NY AGI entirely, so B's true NY bill\n"+
		"  is lower by ~$%.2f/yr. The model cannot express this: CalculateStateTax is\n"+
		"  rate x federal-taxable-income, with no jurisdiction and no income types.\n"+
		"  Fix: derive state AGI from typed income sources with its own cited function,\n"+
		"  per §3's \"six named functions\" requirement.",
		stateA, targetOrdinary, wagesB, taxableSS, taxableSS*nyRate/100)
}

// TestFinanceGap_TaxConstantsLackStatutoryYears
//
// FINANCEAPPCONCERNS.md §7 asks for constants keyed by (tax_year,
// jurisdiction, effective_date), each carrying a source and a verified_on
// date, with mid-year effective dates supported, the dependency surfaced to
// the user, and a missing year failing loudly rather than silently reusing
// last year's table.
//
// PARTIALLY CLOSED. The mechanism exists (taxyears.go): records are keyed and
// dated, mid-year effective months resolve, every record carries provenance,
// years before the earliest record are an error, and later years come back
// explicitly marked BasisProjected with the year they were extrapolated from.
// The tax panel states all of it.
//
// What is missing is DATA. Only 2024 is seeded, because it is the only year
// this repository can cite. Every later year is therefore a forecast, so the
// document's own 2026 figures — the new senior deduction, the restored ACA
// cliff — remain unreachable no matter how good the mechanism is.
func TestFinanceGap_TaxConstantsLackStatutoryYears(t *testing.T) {
	tc := fcCalculator(t)

	latest := LatestStatutoryFederalTaxYear()
	if latest != taxBaseYear {
		t.Fatalf("statutory coverage moved to %d; update this gap test", latest)
	}

	// The mechanism is genuinely in place — confirm before complaining.
	resolved, err := tc.ResolveTaxYear(latest+2, 1)
	if err != nil {
		t.Fatalf("resolving a future year should project, not fail: %v", err)
	}
	if !resolved.Projected() {
		t.Fatal("regression: a year with no published figures was not marked projected")
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §7, data half): the constants store holds "+
		"exactly one statutory year (%d).\n"+
		"  Everything after it is extrapolated at an assumed inflation rate, which is\n"+
		"  labelled honestly but is still not law: real indexing rounds in fixed steps\n"+
		"  and several thresholds are never indexed at all.\n"+
		"  Consequences: the 2026 senior deduction ($6,000, phasing out over $150,000)\n"+
		"  does not exist in any table, and no 2025 or 2026 figure can be reproduced\n"+
		"  exactly.\n"+
		"  Also still unversioned: IRMAA keeps its own 2026 table, its own base\n"+
		"  year and its own two inflation series outside TaxYearRecord, which\n"+
		"  carries no IRMAA figures at all; and jurisdiction is nominal, since\n"+
		"  state tax is one flat\n"+
		"  rate with no notion of what a state excludes.\n"+
		"  Fix: append statutory records with their own citations — a data change, not\n"+
		"  a code change — and fold the IRMAA tiers into TaxYearRecord.",
		latest)
}

// TestFinanceGap_MoneyIsFloat64
//
// FINANCEAPPCONCERNS.md §9: "Never floats. Integer cents or Decimal. A float
// rounding error at a bracket boundary is silent and can flip a cliff
// determination."
//
// Not hypothetical here. The engine accumulates income month by month
// (ProjectionTaxAccumulator.ApplyMonth) and dividend credits arrive in small
// increments, so a year's taxable income is a sum, never a literal. Two
// legitimate ways of accumulating the same $94,300 straddle the 12%/22%
// bracket edge in opposite directions, and the engine reports a different
// marginal bracket for each.
func TestFinanceGap_MoneyIsFloat64(t *testing.T) {
	brackets := TaxBrackets2024[models.FilingMarriedJoint]
	boundary := brackets[1].MaxIncome // 94,300 — the 12%/22% edge

	// Twelve equal monthly accruals.
	var monthly float64
	for i := 0; i < 12; i++ {
		monthly += boundary / 12
	}

	// The same total reached in ten-cent dividend credits.
	var dimes float64
	for i := 0; i < int(boundary*10); i++ {
		dimes += 0.10
	}

	_, rateExact := calculateFederalTaxOnTaxableIncome(boundary, brackets)
	_, rateMonthly := calculateFederalTaxOnTaxableIncome(monthly, brackets)
	_, rateDimes := calculateFederalTaxOnTaxableIncome(dimes, brackets)

	if rateExact == rateMonthly && rateExact == rateDimes {
		t.Skipf("no bracket flip observed at this boundary; "+
			"exact=%.10f monthly=%.10f dimes=%.10f", boundary, monthly, dimes)
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §9): float64 money flips the bracket "+
		"determination at the 12%%/22%% edge.\n"+
		"  exact literal      %.10f -> marginal %.0f%%\n"+
		"  12 monthly accruals %.10f -> marginal %.0f%%  (err %+.3e)\n"+
		"  dime-sized credits  %.10f -> marginal %.0f%%  (err %+.3e)\n"+
		"  The comparison in calculateFederalTaxOnTaxableIncome is a float compare,\n"+
		"  and the engine builds annual income by summing months, so this is on the\n"+
		"  live path — not a contrived case.\n"+
		"  Fix: integer cents or a Decimal type across models and engine.",
		boundary, rateExact*100,
		monthly, rateMonthly*100, monthly-boundary,
		dimes, rateDimes*100, dimes-boundary)
}

// TestFinanceGap_OptimizerDoesNotProbeCliffs
//
// FINANCEAPPCONCERNS.md §5 asks for three things: an explicit registry of
// discontinuity points, optimizers that evaluate the objective AT and JUST
// BELOW every relevant cliff, and cliff proximity surfaced in the UI.
//
// PARTIALLY CLOSED. The registry exists (ThresholdRegistry, thresholds.go),
// it distinguishes true cliffs from kinks, it keys each entry to the income
// measure it is actually tested against, and proximity is surfaced per
// projection year and in the tax panel.
//
// What remains is the middle requirement. The Roth bracket-fill search in
// analysis/tax_optimizer_strategies.go targets ordinary bracket tops only; it
// never asks the registry where the step costs are, so it can size a
// conversion that lands one dollar over an IRMAA tier and pay for the whole
// tier to gain a few dollars of bracket fill.
func TestFinanceGap_OptimizerDoesNotProbeCliffs(t *testing.T) {
	zero := 0.0
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
		Age65Count:         2,
	}, 0)

	registry := tc.ThresholdRegistry(ThresholdRegistryOptions{
		IRMAAEligibleAdults:  2,
		IRMAAThresholdFactor: 1,
		IRMAASurchargeFactor: 1,
	})

	var cliffs []Threshold
	for _, th := range registry {
		if th.Kind == ThresholdCliff {
			cliffs = append(cliffs, th)
		}
	}
	if len(cliffs) == 0 {
		t.Fatal("regression: the registry no longer enumerates any cliffs")
	}

	// The optimizer's own target list, for contrast: bracket tops, no cliffs.
	t.Errorf("GAP (FINANCEAPPCONCERNS.md §5, optimizer half): the registry knows "+
		"about %d step-cost thresholds (first at $%.0f, costing $%.2f to cross), "+
		"but no optimizer consults it.\n"+
		"  analysis/tax_optimizer_strategies.go sizes Roth conversions against "+
		"ordinary\n"+
		"  bracket tops only. A conversion that fills a bracket and lands one dollar\n"+
		"  over an IRMAA tier buys a few dollars of bracket at the price of the whole\n"+
		"  tier, two years later.\n"+
		"  Fix: evaluate every candidate at and just below each registered cliff, in\n"+
		"  addition to whatever search runs.\n"+
		"  Still entirely absent: the ACA premium-credit cliff at 400%% FPL, because\n"+
		"  premium credits are not modelled — see the ACA gap test.",
		len(cliffs), cliffs[0].Amount, cliffs[0].AnnualCostOfCrossing)
}
