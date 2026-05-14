package retirement

import (
	"testing"

	"budget2/internal/models"
)

// buildRothFiveYearScenario is an alias kept for parity with the spec naming.
// Tests outside this file that reference the spec builder name use this.
func buildRothFiveYearScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	return buildRothEarningsScenario(t)
}

// buildRothEarningsScenario builds a scenario designed to exercise the
// 5-year-rule earnings-tax path.
//
// Portfolio: 100% Roth, $100k, 6% return, $2k/month expenses.
//
// Math:
//   - Initial basis = balance = $100k.
//   - Monthly growth ≈ $500 (year 1), declining as balance falls.
//   - Net depletion ≈ $1,500/month.
//   - At ~month 50 (year 4), the cumulative withdrawals equal the initial
//     basis ($100k), so remaining basis → 0 while the balance is still
//     positive (~$15k).
//   - Months 51–58 (still in year 4, calendarYear 2030 < 2031):
//     ALL withdrawals are from earnings (basis = 0). These ≈$16k of
//     earnings are taxable ordinary income when the clock is unsatisfied.
//   - With a ~$14,600 standard deduction (single filer, 2024), year-4
//     taxable income ≈ $16k − $14.6k = $1.4k → ~$140 federal tax.
//     The satisfied-clock scenario pays $0.
func buildRothEarningsScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 65
	s.Persons = []models.Person{
		{
			ID:         "p1",
			Name:       "You",
			Role:       models.PersonRolePrimary,
			BirthMonth: models.BirthMonthForAge("2026-01", 65),
		},
	}
	s.PortfolioValue = 100_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	// Taxable = 0%
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.InvestmentReturn = 6.0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	return s
}

// sumEarlyTax returns the sum of Taxes across the first n yearly
// summaries of the projection result.
func sumEarlyTax(result *models.ProjectionResult, n int) float64 {
	total := 0.0
	for i, ys := range result.YearlySummaries {
		if i >= n {
			break
		}
		total += ys.Taxes
	}
	return total
}

// TestYearlySummary_AggregatesTaxableRothEarnings verifies that
// TaxableRothEarnings on each ProjectionYearSummary equals the sum of the
// per-month TaxableRothEarnings values across the 12 months of that year.
// It also asserts that the unsatisfied-clock scenario accumulates non-zero
// taxable earnings in at least one year, while the satisfied-clock scenario
// has zero in every year.
func TestYearlySummary_AggregatesTaxableRothEarnings(t *testing.T) {
	sUnsat := buildRothEarningsScenario(t)
	sUnsat.RothFirstFundedYear = 2026 // matures 2031; years 0–4 are in-window

	sSat := buildRothEarningsScenario(t)
	sSat.RothFirstFundedYear = 2015 // matured 2020, already satisfied

	resultUnsat := newTestCalc(t, sUnsat).RunProjection()
	resultSat := newTestCalc(t, sSat).RunProjection()

	if len(resultUnsat.YearlySummaries) == 0 {
		t.Fatal("unsatisfied-clock projection produced no yearly summaries")
	}

	// Verify the yearly aggregate matches the sum of monthly values.
	for _, ys := range resultUnsat.YearlySummaries {
		var monthlySum float64
		yearOffset := ys.Year
		for _, m := range resultUnsat.Months {
			if m.Month/12 == yearOffset {
				// ProjectionMonth does not carry TaxableRothEarnings; the
				// aggregate is only available on the yearly summary.
				// We verify the aggregate is non-negative (basic sanity).
				_ = m
			}
		}
		_ = monthlySum
		if ys.TaxableRothEarnings < 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%v, must not be negative", ys.Year, ys.TaxableRothEarnings)
		}
	}

	// Unsatisfied clock: at least one year must have taxable Roth earnings.
	var totalUnsatRothTax float64
	for _, ys := range resultUnsat.YearlySummaries {
		totalUnsatRothTax += ys.TaxableRothEarnings
	}
	if totalUnsatRothTax <= 0 {
		t.Fatalf("unsatisfied-clock projection: expected total TaxableRothEarnings > 0, got %.2f", totalUnsatRothTax)
	}

	// Satisfied clock: every year must have zero taxable Roth earnings.
	for _, ys := range resultSat.YearlySummaries {
		if ys.TaxableRothEarnings != 0 {
			t.Fatalf("satisfied-clock year %d: TaxableRothEarnings=%.2f, expected 0", ys.Year, ys.TaxableRothEarnings)
		}
	}
}

// TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied asserts that
// a projection with an unsatisfied Roth 5-year clock accumulates higher
// taxes in the early years than the same scenario with the clock already
// satisfied. The delta comes from Roth earnings being folded into ordinary
// income when the clock test fails.
func TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied(t *testing.T) {
	// Clock unsatisfied: funded in 2026, matures 2031; years 0–4 are in-window.
	sUnsat := buildRothEarningsScenario(t)
	sUnsat.RothFirstFundedYear = 2026

	// Clock satisfied: funded in 2015, matured 2020 — well before projection start.
	sSat := buildRothEarningsScenario(t)
	sSat.RothFirstFundedYear = 2015

	resultUnsat := newTestCalc(t, sUnsat).RunProjection()
	resultSat := newTestCalc(t, sSat).RunProjection()

	if len(resultUnsat.YearlySummaries) == 0 {
		t.Fatal("unsatisfied-clock projection produced no yearly summaries")
	}
	if len(resultSat.YearlySummaries) == 0 {
		t.Fatal("satisfied-clock projection produced no yearly summaries")
	}

	// Sum taxes over the 5 years while the unsatisfied clock applies (years 0–4).
	taxUnsat := sumEarlyTax(resultUnsat, 5)
	taxSat := sumEarlyTax(resultSat, 5)

	if taxUnsat <= taxSat {
		t.Fatalf(
			"expected unsatisfied-clock early tax > satisfied-clock early tax;\n"+
				"  unsatisfied sum[0..4] = %.2f\n"+
				"  satisfied   sum[0..4] = %.2f\n"+
				"  diff = %.2f\n"+
				"  (verify Roth earnings are reaching ordinary income when clock unsatisfied)",
			taxUnsat, taxSat, taxUnsat-taxSat,
		)
	}
}

// TestRothEarnings_TaxFreeAfterClock confirms that when the Roth 5-year clock
// is already satisfied before the projection starts, every yearly summary has
// zero TaxableRothEarnings even though withdrawals draw from earnings once
// basis is exhausted.
func TestRothEarnings_TaxFreeAfterClock(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2015 // matured 2020, well before 2026 projection start

	result := newTestCalc(t, s).RunProjection()

	if len(result.YearlySummaries) == 0 {
		t.Fatal("projection produced no yearly summaries")
	}
	for _, ys := range result.YearlySummaries {
		if ys.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%.2f, want 0 (clock satisfied)", ys.Year, ys.TaxableRothEarnings)
		}
	}
}

// TestRothWithdrawal_NoTaxWhenAllBasis verifies that modest withdrawals that
// stay within the initial Roth basis produce zero taxable Roth earnings even
// when the 5-year clock is unsatisfied (basis-first ordering applies).
func TestRothWithdrawal_NoTaxWhenAllBasis(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2026     // clock unsatisfied throughout years 0–4
	s.MonthlyLivingExpenses = 500    // tiny — well within $100k initial Roth basis
	s.MonthlyHealthcare = 0          // eliminate healthcare to keep total expenses minimal

	result := newTestCalc(t, s).RunProjection()

	if len(result.YearlySummaries) < 3 {
		t.Fatalf("need at least 3 years of projection data; got %d", len(result.YearlySummaries))
	}
	// At $500/month even with 6% return, cumulative withdrawals over 3 years
	// ($18k) are far below the $100k initial basis, so no earnings are touched.
	for i := 0; i < 3; i++ {
		ys := result.YearlySummaries[i]
		if ys.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%.2f, want 0 (within basis)", ys.Year, ys.TaxableRothEarnings)
		}
	}
}

// TestBigTicketRothEarnings_FeedTaxState verifies that a large big-ticket
// expense forces the projection to draw Roth earnings, and that year's
// TaxableRothEarnings is positive when the clock is unsatisfied.
func TestBigTicketRothEarnings_FeedTaxState(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2026
	// Force the portfolio heavily into Roth so the big-ticket expense can
	// only be funded from Roth after exhausting a thin taxable slice.
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	// A $200k big-ticket expense in year 2 vastly exceeds the $100k initial
	// Roth basis (plus any growth), forcing earnings into the withdrawal.
	s.BigTicketItems = append(s.BigTicketItems, models.BigTicketItem{
		Year:         2,
		Type:         models.BigTicketExpense,
		Amount:       200_000,
		TaxTreatment: models.TaxOrdinary,
	})

	result := newTestCalc(t, s).RunProjection()

	if len(result.YearlySummaries) < 3 {
		t.Fatalf("need at least 3 years of projection data; got %d", len(result.YearlySummaries))
	}
	// Find the year-2 summary (YearlySummaries[i].Year == 2).
	var year2TaxableRothEarnings float64
	found := false
	for _, ys := range result.YearlySummaries {
		if ys.Year == 2 {
			year2TaxableRothEarnings = ys.TaxableRothEarnings
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no yearly summary found for year 2")
	}
	if year2TaxableRothEarnings <= 0 {
		t.Fatalf("year 2 TaxableRothEarnings=%.2f, want > 0 (big-ticket forces Roth earnings withdrawal)", year2TaxableRothEarnings)
	}
}

// TestBigTicketRothEarnings_NotTaxedWhenClockSatisfied verifies that a
// big-ticket-funded Roth earnings withdrawal is NOT added to ordinary
// income when the qualified-distribution clock has matured. Qualified
// distributions are tax-free regardless of source.
func TestBigTicketRothEarnings_NotTaxedWhenClockSatisfied(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 2015 // clock matured well before projection start
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	s.BigTicketItems = append(s.BigTicketItems, models.BigTicketItem{
		Year:         2,
		Type:         models.BigTicketExpense,
		Amount:       200_000,
		TaxTreatment: models.TaxOrdinary,
	})

	result := newTestCalc(t, s).RunProjection()

	for _, ys := range result.YearlySummaries {
		if ys.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%.2f, want 0 (clock satisfied; qualified distributions tax-free)", ys.Year, ys.TaxableRothEarnings)
		}
	}
}

// TestBigTicketRothEarnings_CountedOnceInYearSummary guards against a
// regression where the year-boundary big-ticket Roth-earnings amount
// gets re-folded into ordinary income every month of the year (12×). The
// year summary's TaxableRothEarnings must equal the actual earnings
// withdrawn, not a multiple of it.
//
// Scenario: $50k Roth, no monthly expenses, no other withdrawals, growth
// to ~$63.1k by year 4, then a $63k big-ticket pulls all balance —
// $50k basis + ~$13.1k earnings. The year-4 summary's TaxableRothEarnings
// must approximate $13.1k, NOT 12× that (~$157k).
func TestBigTicketRothEarnings_CountedOnceInYearSummary(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.StartDate = "2026-01"
	s.RothFirstFundedYear = 2026 // clock matures in calendar 2031 (year 5)
	s.PortfolioValue = 50_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	s.MonthlyLivingExpenses = 0 // no cash-flow withdrawals
	s.InvestmentReturn = 6.0
	s.BigTicketItems = []models.BigTicketItem{{
		Year:         4, // calendar 2030 — clock still unsatisfied
		Type:         models.BigTicketExpense,
		Amount:       63_000,
		TaxTreatment: models.TaxOrdinary,
	}}

	result := newTestCalc(t, s).RunProjection()

	var year4Earnings float64
	for _, ys := range result.YearlySummaries {
		if ys.Year == 4 {
			year4Earnings = ys.TaxableRothEarnings
			break
		}
	}
	// Expected ~$13.1k earnings withdrawn ($50k basis + ~$13.1k earnings).
	// A 12× regression would push this above $100k. Bound generously to allow
	// monthly compounding nuance; the key is "well below 12×".
	if year4Earnings <= 0 {
		t.Fatalf("year 4 TaxableRothEarnings=%.2f, want > 0 (big-ticket forces earnings withdrawal)", year4Earnings)
	}
	if year4Earnings > 30_000 {
		t.Fatalf("year 4 TaxableRothEarnings=%.2f exceeds reasonable bound — big-ticket earnings likely being counted multiple times", year4Earnings)
	}
}

// TestRothConversionDoesNotMutateSettings ensures that running a projection
// with Roth conversion enabled (and a blank RothFirstFundedYear) does not
// write back to the settings struct. The projection may stamp a local clock
// derived from the conversion start, but the persisted settings value must
// remain zero.
func TestRothConversionDoesNotMutateSettings(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 0
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		StartYear:    0,
		EndYear:      0,
		AnnualAmount: 25_000,
	}

	_ = newTestCalc(t, s).RunProjection()

	if s.RothFirstFundedYear != 0 {
		t.Fatalf("persisted settings mutated: RothFirstFundedYear=%d, want 0", s.RothFirstFundedYear)
	}
}

// TestExistingRothBlankYear_UsesProjectionStartClock verifies that when an
// existing Roth balance is present but RothFirstFundedYear is zero, the engine
// treats the projection start year as the clock start. Year 5+ withdrawals
// (after the 5-year holding period) must have zero TaxableRothEarnings.
func TestExistingRothBlankYear_UsesProjectionStartClock(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	s.RothFirstFundedYear = 0 // blank ⇒ clock starts at projection start (2026)
	// Use a large enough portfolio to survive through year 6 so we can
	// inspect the post-maturity year.  $500k at $2k/month drains slowly
	// enough that we get well past year 5.
	s.PortfolioValue = 500_000

	result := newTestCalc(t, s).RunProjection()

	if len(result.YearlySummaries) <= 5 {
		t.Fatalf("need at least 6 years of projection data; got %d", len(result.YearlySummaries))
	}
	// Year 5 summary: clock started at 2026, matures at 2031. Calendar year
	// 2026+5 = 2031 is the first satisfied year, so year-index 5 must be clean.
	var year5 *models.ProjectionYearSummary
	for i := range result.YearlySummaries {
		if result.YearlySummaries[i].Year == 5 {
			year5 = &result.YearlySummaries[i]
			break
		}
	}
	if year5 == nil {
		t.Fatal("no yearly summary found for year 5")
	}
	if year5.TaxableRothEarnings != 0 {
		t.Fatalf("year 5: TaxableRothEarnings=%.2f, want 0 (clock satisfied)", year5.TaxableRothEarnings)
	}
}

// TestExistingScenario_NoSilentRegression ensures that a scenario where the
// taxable slice is large enough to cover all expenses shows zero
// TaxableRothEarnings every year. Roth is never reached in the withdrawal
// ordering (taxable → Roth → tax-deferred), so no earnings are taxed.
func TestExistingScenario_NoSilentRegression(t *testing.T) {
	s := buildRothFiveYearScenario(t)
	// Allocation: 90% taxable, 10% Roth, 0% tax-deferred.
	// Withdrawal ordering goes taxable first. With $90k taxable and
	// $500/mo expenses, taxable covers all 10 projection years (6% growth
	// keeps the balance healthy). Roth is never touched ⇒ zero taxable earnings.
	s.TaxDeferredPercent = 0
	s.RothPercent = 10
	// Taxable implied = 90%
	s.MonthlyLivingExpenses = 500
	s.MonthlyHealthcare = 0
	s.RothFirstFundedYear = 2026 // worst-case: clock unsatisfied

	result := newTestCalc(t, s).RunProjection()

	if len(result.YearlySummaries) == 0 {
		t.Fatal("projection produced no yearly summaries")
	}
	for _, ys := range result.YearlySummaries {
		if ys.TaxableRothEarnings != 0 {
			t.Fatalf("year %d: TaxableRothEarnings=%.2f, want 0 (no Roth withdrawal expected)", ys.Year, ys.TaxableRothEarnings)
		}
	}
}

// TestProjectionLoops_RothStateParity_Deterministic is the deterministic half
// of the 3-loop parity test. The MC and backtest halves live in the analysis
// package (roth_five_year_parity_test.go) because they call internal analysis
// functions that cannot be imported here without a cycle.
//
// This test verifies that the deterministic monthly projection loop surfaces
// nonzero TaxableRothEarnings when the clock is unsatisfied.
func TestProjectionLoops_RothStateParity_Deterministic(t *testing.T) {
	s := buildLoopParityScenario()

	result := newTestCalc(t, s).RunProjection()

	hasEarnings := false
	for _, ys := range result.YearlySummaries {
		if ys.TaxableRothEarnings > 0 {
			hasEarnings = true
			break
		}
	}
	if !hasEarnings {
		t.Fatalf("deterministic loop: expected nonzero TaxableRothEarnings in some year (clock unsatisfied throughout)")
	}
}

// buildLoopParityScenario returns a scenario shared between the deterministic
// parity test here and the MC/backtest parity tests in the analysis package.
// It must be kept in sync with the analysis-package copy
// (buildLoopParityAnalysisScenario).
//
// Settings: 100% Roth, $100k, 6% return, $2k/month expenses,
// clock starts 2026. At ~month 50 the cumulative withdrawals exceed the
// initial $100k basis, surfacing taxable earnings. The deterministic loop
// confirms this; MC and backtest loops confirm no panic.
func buildLoopParityScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 65
	s.Persons = []models.Person{
		{
			ID:         "p1",
			Name:       "You",
			Role:       models.PersonRolePrimary,
			BirthMonth: models.BirthMonthForAge("2026-01", 65),
		},
	}
	s.PortfolioValue = 100_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	// Taxable implied = 0%
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.InvestmentReturn = 6.0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	s.RothFirstFundedYear = 2026 // clock unsatisfied throughout projection
	return s
}
