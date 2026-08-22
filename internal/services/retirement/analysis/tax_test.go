package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// taxableScenario builds a retired, all-tax-deferred portfolio that draws
// down to generate ordinary-income tax every year (RMDs + withdrawals).
func taxableScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 70
	// prepare.ComputeAges recomputes CurrentAge from BirthMonth, so the age
	// must be set via BirthMonth or the scenario silently runs at 65.
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 8000
	s.InvestmentReturn = 5.0
	s.InflationRate = 3.0
	s.ProjectionYears = 25
	return s
}

func TestBuildTax_NilAndEmpty(t *testing.T) {
	in := engineInput(t, taxableScenario())
	if got := BuildTax(nil, in); got != nil {
		t.Errorf("BuildTax(nil) = %v; want nil", got)
	}
	if got := BuildTax(&models.ProjectionResult{}, in); got != nil {
		t.Errorf("BuildTax(empty months) = %v; want nil", got)
	}
}

func TestBuildTax_PopulatesTotals(t *testing.T) {
	proj, in := runProj(t, taxableScenario())

	tax := BuildTax(proj, in)
	if tax == nil {
		t.Fatal("BuildTax returned nil for a taxable projection")
	}
	if tax.TotalTaxPaid <= 0 {
		t.Fatalf("TotalTaxPaid = %.2f; want > 0", tax.TotalTaxPaid)
	}

	// Total must reconcile with the per-year summaries (and thus with the
	// explainability panel, which sums the same field).
	var wantTotal float64
	for _, ys := range proj.YearlySummaries {
		wantTotal += ys.Taxes
	}
	if d := tax.TotalTaxPaid - wantTotal; math.Abs(d) > 0.01 {
		t.Errorf("TotalTaxPaid = %.2f; want sum(YearlySummaries.Taxes) = %.2f", tax.TotalTaxPaid, wantTotal)
	}

	// Federal + state must equal the total.
	if d := (tax.TotalFederalTaxPaid + tax.TotalStateTaxPaid) - tax.TotalTaxPaid; math.Abs(d) > 0.01 {
		t.Errorf("federal (%.2f) + state (%.2f) = %.2f; want total %.2f",
			tax.TotalFederalTaxPaid, tax.TotalStateTaxPaid, tax.TotalFederalTaxPaid+tax.TotalStateTaxPaid, tax.TotalTaxPaid)
	}

	if len(tax.YearlyTaxSummary) != len(proj.YearlySummaries) {
		t.Errorf("YearlyTaxSummary rows = %d; want %d", len(tax.YearlyTaxSummary), len(proj.YearlySummaries))
	}

	if !(tax.AverageEffectiveRate > 0 && tax.AverageEffectiveRate < 100) {
		t.Errorf("AverageEffectiveRate = %.2f; want a sane percent in (0,100)", tax.AverageEffectiveRate)
	}
}

func TestBuildTax_YearlyRowsReconcile(t *testing.T) {
	proj, in := runProj(t, taxableScenario())
	prepared := in.Prepared.Settings()
	tax := BuildTax(proj, in)
	if tax == nil || len(tax.YearlyTaxSummary) == 0 {
		t.Fatal("expected populated yearly rows")
	}

	wantFirstYear := engine.ParseStartYear(prepared.StartDate)
	wantFirstAge := prepared.GetOlderAge()
	if wantFirstAge != 70 {
		t.Fatalf("prepared older age = %d; scenario helper must yield age 70", wantFirstAge)
	}
	first := tax.YearlyTaxSummary[0]
	if first.Year != wantFirstYear {
		t.Errorf("first row Year = %d; want %d", first.Year, wantFirstYear)
	}
	if first.Age != wantFirstAge {
		t.Errorf("first row Age = %d; want %d", first.Age, wantFirstAge)
	}

	for i, row := range tax.YearlyTaxSummary {
		if d := (row.FederalTax + row.StateTax) - row.TotalTax; math.Abs(d) > 0.01 {
			t.Errorf("row %d: federal+state (%.2f) != total (%.2f)", i, row.FederalTax+row.StateTax, row.TotalTax)
		}
		if row.TotalTax > 0 && row.EffectiveRate <= 0 {
			t.Errorf("row %d: positive tax %.2f but effective rate %.4f", i, row.TotalTax, row.EffectiveRate)
		}
	}
}

func TestBuildTax_StateTaxSplit(t *testing.T) {
	// No state tax configured (default) → all tax is federal.
	projNoState, inNoState := runProj(t, taxableScenario())
	noState := BuildTax(projNoState, inNoState)
	if noState.TotalStateTaxPaid != 0 {
		t.Errorf("no state rate: TotalStateTaxPaid = %.2f; want 0", noState.TotalStateTaxPaid)
	}
	if d := noState.TotalFederalTaxPaid - noState.TotalTaxPaid; math.Abs(d) > 0.01 {
		t.Errorf("no state rate: federal (%.2f) should equal total (%.2f)", noState.TotalFederalTaxPaid, noState.TotalTaxPaid)
	}

	// 5% state rate → positive state tax, lower federal share.
	sState := taxableScenario()
	sState.TaxConfig.StateIncomeTaxRate = models.FloatPtr(5.0)
	projState, inState := runProj(t, sState)
	withState := BuildTax(projState, inState)
	if withState.TotalStateTaxPaid <= 0 {
		t.Errorf("5%% state rate: TotalStateTaxPaid = %.2f; want > 0", withState.TotalStateTaxPaid)
	}
}

// TestBuildTax_MarginalRateFlowsFromEngine is the end-to-end check for the
// §4 fix (FINANCEAPPCONCERNS.md): the marginal rate is measured in the engine
// from each year's own income composition and carried through to the tax
// panel. A plumbing break here would leave every displayed rate at 0 while
// every unit test still passed.
func TestBuildTax_MarginalRateFlowsFromEngine(t *testing.T) {
	proj, in := runProj(t, taxableScenario())

	// The engine must populate the per-year rate on the projection itself.
	populated := 0
	for _, ys := range proj.YearlySummaries {
		if ys.Taxes <= 0 {
			continue // a year with no tax legitimately has no marginal rate
		}
		if ys.MarginalRate <= 0 {
			t.Errorf("year %d: taxes %.2f but MarginalRate = %.4f; "+
				"the engine did not measure a rate for a taxable year",
				ys.Year, ys.Taxes, ys.MarginalRate)
			continue
		}
		if ys.MarginalRate > 60 {
			t.Errorf("year %d: MarginalRate = %.2f%%; implausibly high for this scenario",
				ys.Year, ys.MarginalRate)
		}
		populated++
	}
	if populated == 0 {
		t.Fatal("no taxable year carried a marginal rate; the engine plumbing is broken")
	}

	// BuildTax must carry it through to what the UI renders, unmodified.
	tax := BuildTax(proj, in)
	if tax == nil {
		t.Fatal("BuildTax returned nil for a taxable projection")
	}
	if len(tax.YearlyTaxSummary) != len(proj.YearlySummaries) {
		t.Fatalf("YearlyTaxSummary has %d rows; want %d",
			len(tax.YearlyTaxSummary), len(proj.YearlySummaries))
	}
	for i, row := range tax.YearlyTaxSummary {
		if want := proj.YearlySummaries[i].MarginalRate; row.MarginalRate != want {
			t.Errorf("row %d: MarginalRate = %.4f; want the engine's %.4f",
				i, row.MarginalRate, want)
		}
	}
}

// TestBuildTax_MarginalRateOnGainsFlowsFromEngine is the end-to-end check for
// the "what does realizing one more dollar of gain cost" column. The rate is
// legitimately 0 in a year with 0%-bracket headroom, so this asserts exact
// pass-through plus at least one year where the cost is real.
func TestBuildTax_MarginalRateOnGainsFlowsFromEngine(t *testing.T) {
	proj, in := runProj(t, taxableScenario())

	tax := BuildTax(proj, in)
	if tax == nil {
		t.Fatal("BuildTax returned nil for a taxable projection")
	}
	if len(tax.YearlyTaxSummary) != len(proj.YearlySummaries) {
		t.Fatalf("YearlyTaxSummary has %d rows; want %d",
			len(tax.YearlyTaxSummary), len(proj.YearlySummaries))
	}

	nonZero := 0
	for i, row := range tax.YearlyTaxSummary {
		want := proj.YearlySummaries[i].MarginalRateLongTermGain
		if row.MarginalRateLongTermGain != want {
			t.Errorf("row %d: MarginalRateLongTermGain = %.4f; want the engine's %.4f",
				i, row.MarginalRateLongTermGain, want)
		}
		if want < 0 || want > 45 {
			t.Errorf("row %d: implausible marginal rate on gains: %.4f%%", i, want)
		}
		if want > 0 {
			nonZero++
		}
	}

	if nonZero == 0 {
		t.Error("no year priced realizing a gain above zero; this scenario draws down a " +
			"large tax-deferred balance, so at least some years should sit past the 0% ceiling")
	}
}
