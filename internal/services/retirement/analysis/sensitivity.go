package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// Sensitivity runs sensitivity analysis on key parameters by perturbing
// settings and re-running the projection. Returns a slice of results,
// one per scenario (e.g. higher returns, lower returns, higher
// inflation, etc.).
func Sensitivity(eng *engine.Engine, in engine.Input) []models.SensitivityResult {
	s := in.Prepared.Settings()
	results := make([]models.SensitivityResult, 0)

	// Get baseline score
	baseProjection := eng.Run(in)
	baseBudgetFit := BudgetFit(in, baseProjection)
	baseScore := Score(baseProjection, baseBudgetFit)

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

	for _, scenario := range scenarios {
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
				for i := range modifiedSettings.HealthcarePersons {
					modifiedSettings.HealthcarePersons[i].CurrentMonthlyCost *= 1.5
					modifiedSettings.HealthcarePersons[i].MedicareMonthlyCost *= 1.5
					modifiedSettings.HealthcarePersons[i].ACACostAfterEmployer *= 1.5
				}
			} else {
				modifiedSettings.MonthlyHealthcare = scenario.ParamValue
			}
		}

		// Run projection with modified settings
		modIn := engine.Input{Prepared: perturbAndPrepare(&modifiedSettings), Chain: in.Chain, Hooks: in.Hooks}
		modProjection := eng.Run(modIn)
		modBudgetFit := BudgetFit(modIn, modProjection)
		modScore := Score(modProjection, modBudgetFit)

		results = append(results, models.SensitivityResult{
			Scenario:       scenario,
			LongevityYears: modProjection.LongevityYears,
			FinalBalance:   modProjection.FinalBalance,
			Survives:       modProjection.Survives,
			ScoreChange:    modScore.Score - baseScore.Score,
		})
	}

	return results
}
