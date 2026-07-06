package analysis

import (
	"runtime"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// SensitivityWithBaseline runs sensitivity analysis on key parameters by
// perturbing settings and re-running the projection, reusing an
// already-computed baseline projection and budget fit so orchestrators
// that just ran the baseline don't pay for a redundant full projection.
// The scenario projections are independent of one another and run
// concurrently (capped at NumCPU workers); each perturbation builds its
// own PreparedSettings deep copy, and results land in fixed
// scenario-order slots so the output is identical to the sequential form
// regardless of scheduling.
func SensitivityWithBaseline(eng *engine.Engine, in engine.Input, baseProjection *models.ProjectionResult, baseBudgetFit *models.BudgetFitAnalysis) []models.SensitivityResult {
	s := in.Prepared.Settings()
	baseScore := Score(baseBudgetFit.RequiredRate, baseProjection.Survives)

	// Get effective baseline return (either explicit or calculated from per-account allocation)
	// When InvestmentReturn=0, the projection uses per-account asset allocation blended returns
	effectiveReturn := s.InvestmentReturn
	if effectiveReturn == 0 {
		effectiveReturn = s.GetExpectedReturnFromAllocation()
	}

	// Define scenarios
	scenarios := []models.SensitivityScenario{
		{Name: "Higher Returns", ParamName: "investment_return", ParamValue: effectiveReturn + 2, Change: "+2%"},
		{Name: "Lower Returns", ParamName: "investment_return", ParamValue: effectiveReturn - 2, Change: "-2%"},
		{Name: "Higher Inflation", ParamName: "inflation_rate", ParamValue: s.InflationRate + 1, Change: "+1%"},
		{Name: "Lower Inflation", ParamName: "inflation_rate", ParamValue: s.InflationRate - 1, Change: "-1%"},
		{Name: "Higher Spending", ParamName: "monthly_living_expenses", ParamValue: s.MonthlyLivingExpenses * 1.1, Change: "+10%"},
		{Name: "Higher Healthcare", ParamName: "monthly_healthcare", ParamValue: s.MonthlyHealthcare * 1.5, Change: "+50%"},
	}

	results := make([]models.SensitivityResult, len(scenarios))
	parallelIndexed(len(scenarios), runtime.NumCPU(), func(i int) {
		scenario := scenarios[i]

		// Clone settings and apply variation
		modifiedSettings := *s
		modifiedSettings.IncomeSources = append([]models.IncomeSource{}, s.IncomeSources...)
		modifiedSettings.ExpenseSources = append([]models.ExpenseSource{}, s.ExpenseSources...)
		modifiedSettings.HealthcarePersons = append([]models.HealthcarePerson{}, s.HealthcarePersons...)

		switch scenario.ParamName {
		case "investment_return":
			modifiedSettings.InvestmentReturn = scenario.ParamValue
		case "inflation_rate":
			modifiedSettings.InflationRate = scenario.ParamValue
		case "monthly_living_expenses":
			modifiedSettings.MonthlyLivingExpenses = scenario.ParamValue
		case "monthly_healthcare":
			if len(modifiedSettings.HealthcarePersons) > 0 {
				for j := range modifiedSettings.HealthcarePersons {
					modifiedSettings.HealthcarePersons[j].CurrentMonthlyCost *= 1.5
					modifiedSettings.HealthcarePersons[j].MedicareMonthlyCost *= 1.5
					modifiedSettings.HealthcarePersons[j].ACACostAfterEmployer *= 1.5
				}
			} else {
				modifiedSettings.MonthlyHealthcare = scenario.ParamValue
			}
		}

		// Run projection with modified settings
		modIn := engine.Input{Prepared: perturbAndPrepare(&modifiedSettings), Chain: in.Chain, Hooks: in.Hooks}
		modProjection := eng.Run(modIn)
		modBudgetFit := BudgetFit(modIn, modProjection)
		modScore := Score(modBudgetFit.RequiredRate, modProjection.Survives)

		results[i] = models.SensitivityResult{
			Scenario:       scenario,
			LongevityYears: modProjection.LongevityYears,
			FinalBalance:   modProjection.FinalBalance,
			Survives:       modProjection.Survives,
			ScoreChange:    modScore.Score - baseScore.Score,
		}
	})

	return results
}
