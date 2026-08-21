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
// TaxableAccountState (engine/taxable.go) holds a single MarketValue and a
// single CostBasis, and Withdraw recognises gain pro-rata. There is no lot,
// no open date, no holding period, and no cost-basis method.
func TestFinanceGap_TaxableAccountHasNoLots(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	const marketValue = 500000.0

	account := NewTaxableAccountState(s, marketValue)

	// Defect A: the account is seeded with zero embedded gain. A real
	// brokerage position carries unrealized appreciation from day one; this
	// systematically understates withdrawal tax for the whole horizon.
	if account.CostBasis >= account.MarketValue {
		t.Errorf("GAP (FINANCEAPPCONCERNS.md §2): NewTaxableAccountState seeds "+
			"CostBasis (%.2f) == MarketValue (%.2f), i.e. zero embedded gain.\n"+
			"  Every projection therefore starts with an untaxed-appreciation-free\n"+
			"  taxable account and understates realized gain on withdrawal.\n"+
			"  Fix: accept lots (open date, quantity, cost/share) at ingestion.",
			account.CostBasis, account.MarketValue)
	}

	// Defect B: withdrawal gain is pro-rata against blended basis, so lot
	// selection cannot change the tax. The document's whole §2 is that
	// choosing lots changed realized gain from ~$27,000 to $1,663.
	account.CostBasis = marketValue * 0.5 // pretend a 100% blended gain
	_, _, gainA := account.Withdraw(34667)

	account2 := NewTaxableAccountState(s, marketValue)
	account2.CostBasis = marketValue * 0.5
	_, _, gainB := account2.Withdraw(34667)

	if gainA != gainB {
		t.Fatalf("unexpected: identical withdrawals produced different gains (%.2f vs %.2f)", gainA, gainB)
	}
	t.Errorf("GAP (FINANCEAPPCONCERNS.md §2): Withdraw recognises %.2f of gain "+
		"pro-rata regardless of which lots are sold.\n"+
		"  There is no API by which a caller could sell the low-gain lots, so the\n"+
		"  ~10x spread the document measures is unrepresentable.\n"+
		"  Fix: model lots explicitly and a cost-basis method\n"+
		"  (FIFO / LIFO / HighCost / LowCost / SpecID / TaxLotOptimizer).", gainA)
}

// TestFinanceGap_NoACAPremiumCreditOrCliff
//
// FINANCEAPPCONCERNS.md §5 and §8. The ACA subsidy is a step function at
// 400% FPL where "$1 of extra income can cost ~$8,000 of credit", and
// "COBRA enrollment disqualifies you from premium tax credits at any income".
//
// models.HealthcarePerson carries CoverageType medicare|aca|employer and a
// flat CurrentMonthlyCost. ACA cost is independent of income, so the largest
// discontinuity in the document is invisible to the projection and to the
// bracket-fill search in analysis/tax_optimizer_strategies.go.
func TestFinanceGap_NoACAPremiumCreditOrCliff(t *testing.T) {
	lowIncome := models.NewHealthcarePerson("low", 60, models.CoverageACA)
	highIncome := models.NewHealthcarePerson("high", 60, models.CoverageACA)

	// Two identical 60-year-olds on marketplace coverage. One is far under
	// the 400% FPL cliff, one is far over. Nothing in the model can express
	// the difference — there is no income input at all.
	if lowIncome.CurrentMonthlyCost != highIncome.CurrentMonthlyCost {
		t.Fatalf("unexpected: healthcare cost already varies (%v vs %v)",
			lowIncome.CurrentMonthlyCost, highIncome.CurrentMonthlyCost)
	}

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §5, §8): marketplace cost is a flat "+
		"$%.0f/mo constant, independent of income.\n"+
		"  Missing from the data model: household size and FPL, premium tax credit,\n"+
		"  the 400%%-FPL cliff, the advance-credit flag (repayment is uncapped above\n"+
		"  400%%), and COBRA as a coverage type that disqualifies credits entirely.\n"+
		"  Consequence: the tax optimizer's bracket-fill search can walk across an\n"+
		"  ~$8,000 discontinuity without seeing it.\n"+
		"  Fix: add a cliff registry (§5) and income-dependent premium modelling (§8).",
		lowIncome.CurrentMonthlyCost)
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

// TestFinanceGap_TaxConstantsAreNotVersioned
//
// FINANCEAPPCONCERNS.md §7: "Every constant lives in a data file keyed by
// (tax_year, jurisdiction, effective_date)... Fail loudly on a missing year
// rather than silently falling back to last year's table."
//
// Constants here are Go literals for a single base year (taxBaseYear = 2024),
// projected forward by a uniform inflation factor. Asking for 2026 does not
// return 2026 statutory values and does not fail — it silently returns
// 2024 values scaled by an assumed inflation rate.
func TestFinanceGap_TaxConstantsAreNotVersioned(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingMarriedJoint,
		Age65Count:   1,
	}, 2.5) // 2.5%/yr assumed inflation

	// 2026 is two years from the 2024 base.
	got := tc.GetAdjustedStandardDeduction(2)

	// FINANCEAPPCONCERNS.md §9's household implies a 2026 MFJ deduction of
	// $33,850 (statutory standard + one 65+ additional), before the new
	// $6,000 senior deduction the document lists in §7.
	const want2026Statutory = 33850.0
	const wantSeniorDeduction = 6000.0

	t.Errorf("GAP (FINANCEAPPCONCERNS.md §7): standard deduction for 2026 is "+
		"$%.2f — the 2024 table (%.0f + %.0f) inflated by %.1f%%/yr, not a 2026 "+
		"statutory value ($%.0f).\n"+
		"  Also absent entirely: the new senior deduction ($%.0f, phasing out over\n"+
		"  $150,000 of AGI), which the document's golden cases depend on.\n"+
		"  No constant in this package carries an effective date, a source URL, or a\n"+
		"  verified_on date, and a missing year cannot fail loudly because every year\n"+
		"  is synthesised from one base year.\n"+
		"  Fix: move brackets, deductions, thresholds and IRMAA tiers into a data file\n"+
		"  keyed by (tax_year, jurisdiction, effective_date) with mid-year support.",
		got, StandardDeduction2024[models.FilingMarriedJoint],
		AdditionalStandardDeduction2024Age65[models.FilingMarriedJoint],
		2.5, want2026Statutory, wantSeniorDeduction)
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

// TestFinanceGap_NoCliffRegistry
//
// FINANCEAPPCONCERNS.md §5: "Maintain an explicit registry of discontinuity
// points... Any optimization evaluates the objective at and just below every
// relevant cliff."
//
// IRMAA is already a genuine cliff in this engine (CalculateMonthlyIRMAA
// steps), but nothing enumerates it as a discontinuity, and the tax
// optimizer's bracket-fill search targets ordinary brackets only.
func TestFinanceGap_NoCliffRegistry(t *testing.T) {
	// IRMAA is a step function: probe either side of the first MFJ tier.
	const tier1 = 218000.0
	below := CalculateMonthlyIRMAA(tier1, models.FilingMarriedJoint, 1, 1)
	above := CalculateMonthlyIRMAA(tier1+1, models.FilingMarriedJoint, 1, 1)

	if above == below {
		t.Skipf("first MFJ IRMAA tier is not at %.0f in the bundled table; adjust the probe", tier1)
	}

	annualJump := (above - below) * 12 * 2 // two filers, twelve months
	t.Errorf("GAP (FINANCEAPPCONCERNS.md §5): $1 of extra MAGI at $%.0f costs "+
		"$%.2f/yr of IRMAA for an MFJ couple — a true discontinuity.\n"+
		"  Nothing in this package enumerates it. There is no registry of cliff\n"+
		"  locations for the optimizer to evaluate at and just below, and no UI\n"+
		"  surface for proximity (\"you are $X over the next IRMAA tier\").\n"+
		"  The ACA 400%%-FPL cliff is not modelled at all (see the ACA gap test).\n"+
		"  Fix: build the registry as a function of the household's situation, and\n"+
		"  make every optimizer probe it explicitly.",
		tier1, annualJump)
}
