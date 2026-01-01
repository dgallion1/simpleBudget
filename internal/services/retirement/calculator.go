package retirement

import (
	"math"

	"budget2/internal/models"
)

// Calculator performs retirement projections and analysis
type Calculator struct {
	Settings *models.WhatIfSettings
}

// NewCalculator creates a new retirement calculator with the given settings
func NewCalculator(settings *models.WhatIfSettings) *Calculator {
	return &Calculator{Settings: settings}
}

// PresentValue calculates the present value of a future cash flow
// PV = FV / (1 + r)^n
func PresentValue(futureValue, annualRate float64, periods int) float64 {
	if periods <= 0 {
		return futureValue
	}
	if annualRate <= 0 {
		return futureValue
	}
	monthlyRate := annualRate / 100 / 12
	return futureValue / math.Pow(1+monthlyRate, float64(periods))
}

// PresentValueAnnuity calculates the PV of a series of payments
// Handles both regular and growing annuities
func PresentValueAnnuity(payment, discountRate, growthRate float64, startMonth, numPayments int) float64 {
	if numPayments <= 0 || payment == 0 {
		return 0
	}

	monthlyRate := discountRate / 100 / 12
	monthlyGrowth := growthRate / 100 / 12

	var pvAtStart float64

	if monthlyRate <= 0 {
		// No discounting
		if monthlyGrowth <= 0 {
			pvAtStart = payment * float64(numPayments)
		} else {
			// Sum with growth
			total := 0.0
			for m := 0; m < numPayments; m++ {
				total += payment * math.Pow(1+monthlyGrowth, float64(m))
			}
			pvAtStart = total
		}
	} else if math.Abs(monthlyRate-monthlyGrowth) < 1e-10 {
		// Growth equals discount rate
		pvAtStart = payment * float64(numPayments)
	} else if monthlyGrowth > 0 {
		// Growing annuity formula
		growthFactor := (1 + monthlyGrowth) / (1 + monthlyRate)
		pvAtStart = payment * (1 - math.Pow(growthFactor, float64(numPayments))) / (monthlyRate - monthlyGrowth)
	} else {
		// Regular annuity formula
		pvAtStart = payment * (1 - math.Pow(1+monthlyRate, -float64(numPayments))) / monthlyRate
	}

	// Discount back if payments start in the future
	if startMonth > 0 && monthlyRate > 0 {
		return pvAtStart / math.Pow(1+monthlyRate, float64(startMonth))
	}

	return pvAtStart
}

// CalculateTotalIncome returns total income for a specific month
func (c *Calculator) CalculateTotalIncome(month int) float64 {
	total := 0.0
	for _, source := range c.Settings.IncomeSources {
		total += source.GetAdjustedAmount(month)
	}
	return total
}

// CalculateTotalExpenses returns total expenses for a specific month
func (c *Calculator) CalculateTotalExpenses(month int) float64 {
	s := c.Settings
	healthcareStartMonth := s.HealthcareStartYears * 12

	// Calculate living expenses with inflation and spending decline
	livingExpenses := s.MonthlyLivingExpenses
	if month > 0 {
		years := month / 12
		netInflation := (s.InflationRate - s.SpendingDeclineRate) / 100
		livingExpenses = s.MonthlyLivingExpenses * math.Pow(1+netInflation, float64(years))
	}

	// Calculate healthcare expenses
	healthcareExpenses := 0.0
	if month >= healthcareStartMonth {
		healthcareExpenses = s.MonthlyHealthcare
		if month > healthcareStartMonth {
			yearsActive := (month - healthcareStartMonth) / 12
			healthcareExpenses = s.MonthlyHealthcare * math.Pow(1+s.HealthcareInflation/100, float64(yearsActive))
		}
	}

	// Add expense sources
	for _, source := range s.ExpenseSources {
		livingExpenses += source.GetAdjustedAmount(month, s.InflationRate)
	}

	return livingExpenses + healthcareExpenses
}

// RunProjection runs a full retirement projection
func (c *Calculator) RunProjection() *models.ProjectionResult {
	s := c.Settings
	months := s.ProjectionYears * 12
	projection := make([]models.ProjectionMonth, 0, months)

	balance := s.PortfolioValue
	healthcareStartMonth := s.HealthcareStartYears * 12
	var depletionMonth *int
	var longevityYears *float64

	currentLivingExpenses := s.MonthlyLivingExpenses
	currentHealthcareExpenses := s.MonthlyHealthcare

	for m := 0; m < months; m++ {
		// Annual adjustments at year boundaries
		if m > 0 && m%12 == 0 {
			netInflation := (s.InflationRate - s.SpendingDeclineRate) / 100
			currentLivingExpenses *= (1 + netInflation)
			currentHealthcareExpenses *= (1 + s.HealthcareInflation/100)
		}

		// Calculate expenses
		activeHealthcare := 0.0
		if m >= healthcareStartMonth {
			activeHealthcare = currentHealthcareExpenses
		}
		totalExpenses := currentLivingExpenses + activeHealthcare

		// Add expense sources
		for _, source := range s.ExpenseSources {
			totalExpenses += source.GetAdjustedAmount(m, s.InflationRate)
		}

		// Calculate income
		totalIncome := c.CalculateTotalIncome(m)

		// Monthly cash flow
		neededFromPortfolio := totalExpenses - totalIncome

		// Portfolio growth and withdrawal
		growth := balance * (s.InvestmentReturn / 100 / 12)
		balance = balance + growth - neededFromPortfolio

		depleted := false
		if balance < 0 {
			balance = 0
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		}

		projection = append(projection, models.ProjectionMonth{
			Month:             m,
			Year:              float64(m) / 12,
			PortfolioBalance:  balance,
			GeneralExpenses:   currentLivingExpenses,
			HealthcareExpense: activeHealthcare,
			TotalExpenses:     totalExpenses,
			TotalIncome:       totalIncome,
			NetWithdrawal:     math.Max(0, neededFromPortfolio),
			PortfolioGrowth:   growth,
			Depleted:          depleted,
		})
	}

	return &models.ProjectionResult{
		Months:         projection,
		LongevityYears: longevityYears,
		FinalBalance:   balance,
		DepletionMonth: depletionMonth,
		Survives:       depletionMonth == nil,
	}
}

// CalculateBudgetFit analyzes monthly budget gap
func (c *Calculator) CalculateBudgetFit() *models.BudgetFitAnalysis {
	s := c.Settings

	// Calculate first month expenses and income
	monthlyExpenses := c.CalculateTotalExpenses(0)
	monthlyIncome := c.CalculateTotalIncome(0)
	monthlyGap := monthlyExpenses - monthlyIncome
	annualGap := monthlyGap * 12

	// Calculate required withdrawal rate
	requiredRate := 0.0
	if s.PortfolioValue > 0 && monthlyGap > 0 {
		requiredRate = (annualGap / s.PortfolioValue) * 100
	}

	// Calculate max safe withdrawal based on settings
	maxSafeWithdrawal := s.PortfolioValue * s.MaxWithdrawalRate / 100 / 12

	return &models.BudgetFitAnalysis{
		MonthlyExpenses:   monthlyExpenses,
		MonthlyIncome:     monthlyIncome,
		MonthlyGap:        monthlyGap,
		AnnualGap:         annualGap,
		RequiredRate:      requiredRate,
		MaxSafeWithdrawal: maxSafeWithdrawal,
		CanCoverGap:       monthlyGap <= maxSafeWithdrawal,
	}
}

// CalculatePresentValueAnalysis computes PV of expenses and income
func (c *Calculator) CalculatePresentValueAnalysis() *models.PresentValueAnalysis {
	s := c.Settings
	months := s.ProjectionYears * 12
	discountRate := s.DiscountRate

	// Calculate PV of expenses
	pvExpenses := 0.0

	// Living expenses with inflation - spending decline
	netInflation := s.InflationRate - s.SpendingDeclineRate
	pvExpenses += PresentValueAnnuity(s.MonthlyLivingExpenses, discountRate, netInflation, 0, months)

	// Healthcare expenses (if applicable)
	if s.MonthlyHealthcare > 0 {
		healthcareStartMonth := s.HealthcareStartYears * 12
		healthcareMonths := months - healthcareStartMonth
		if healthcareMonths > 0 {
			pvExpenses += PresentValueAnnuity(s.MonthlyHealthcare, discountRate, s.HealthcareInflation, healthcareStartMonth, healthcareMonths)
		}
	}

	// Add expense sources
	for _, source := range s.ExpenseSources {
		startMonth := source.StartYear * 12
		endMonth := months
		if source.EndYear > 0 {
			endMonth = min(source.EndYear*12, months)
		}
		duration := endMonth - startMonth
		if duration > 0 {
			growthRate := 0.0
			if source.Inflation {
				growthRate = s.InflationRate
			}
			pvExpenses += PresentValueAnnuity(source.Amount, discountRate, growthRate, startMonth, duration)
		}
	}

	// Calculate PV of income sources
	pvIncome := 0.0
	for _, source := range s.IncomeSources {
		endMonth := months
		if source.EndMonth != nil {
			endMonth = min(*source.EndMonth, months)
		}
		duration := endMonth - source.StartMonth
		if duration > 0 {
			pvIncome += PresentValueAnnuity(source.Amount, discountRate, source.COLARate*100, source.StartMonth, duration)
		}
	}

	pvGap := pvExpenses - pvIncome
	coverageRatio := 0.0
	if pvExpenses > 0 {
		coverageRatio = (s.PortfolioValue + pvIncome) / pvExpenses
	}
	surplusDeficit := s.PortfolioValue + pvIncome - pvExpenses

	return &models.PresentValueAnalysis{
		PVExpenses:     pvExpenses,
		PVIncome:       pvIncome,
		PVGap:          pvGap,
		CoverageRatio:  coverageRatio,
		SurplusDeficit: surplusDeficit,
	}
}

// CalculateSustainabilityScore computes the sustainability score
func (c *Calculator) CalculateSustainabilityScore(projection *models.ProjectionResult) *models.SustainabilityScore {
	budgetFit := c.CalculateBudgetFit()
	return models.CalculateSustainabilityScore(budgetFit.RequiredRate, projection.Survives)
}

// CalculateSensitivity runs sensitivity analysis on key parameters
func (c *Calculator) CalculateSensitivity() []models.SensitivityResult {
	results := make([]models.SensitivityResult, 0)

	// Get baseline score
	baseProjection := c.RunProjection()
	baseScore := c.CalculateSustainabilityScore(baseProjection)

	// Define scenarios
	scenarios := []models.SensitivityScenario{
		{Name: "Higher Returns", ParamName: "investment_return", ParamValue: c.Settings.InvestmentReturn + 2, Change: "+2%"},
		{Name: "Lower Returns", ParamName: "investment_return", ParamValue: c.Settings.InvestmentReturn - 2, Change: "-2%"},
		{Name: "Higher Inflation", ParamName: "inflation_rate", ParamValue: c.Settings.InflationRate + 1, Change: "+1%"},
		{Name: "Lower Inflation", ParamName: "inflation_rate", ParamValue: c.Settings.InflationRate - 1, Change: "-1%"},
		{Name: "Higher Spending", ParamName: "monthly_living_expenses", ParamValue: c.Settings.MonthlyLivingExpenses * 1.1, Change: "+10%"},
		{Name: "Higher Healthcare", ParamName: "monthly_healthcare", ParamValue: c.Settings.MonthlyHealthcare * 1.5, Change: "+50%"},
	}

	for _, scenario := range scenarios {
		// Clone settings and apply variation
		modifiedSettings := *c.Settings
		modifiedSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
		modifiedSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)

		switch scenario.ParamName {
		case "investment_return":
			modifiedSettings.InvestmentReturn = scenario.ParamValue
		case "inflation_rate":
			modifiedSettings.InflationRate = scenario.ParamValue
		case "monthly_living_expenses":
			modifiedSettings.MonthlyLivingExpenses = scenario.ParamValue
		case "monthly_healthcare":
			modifiedSettings.MonthlyHealthcare = scenario.ParamValue
		}

		// Run projection with modified settings
		modCalc := NewCalculator(&modifiedSettings)
		modProjection := modCalc.RunProjection()
		modScore := modCalc.CalculateSustainabilityScore(modProjection)

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

// RunFullAnalysis performs complete what-if analysis
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	projection := c.RunProjection()
	budgetFit := c.CalculateBudgetFit()
	presentValue := c.CalculatePresentValueAnalysis()
	sustainability := c.CalculateSustainabilityScore(projection)
	sensitivity := c.CalculateSensitivity()

	return &models.WhatIfAnalysis{
		Settings:       c.Settings,
		Projection:     projection,
		BudgetFit:      budgetFit,
		PresentValue:   presentValue,
		Sustainability: sustainability,
		Sensitivity:    sensitivity,
	}
}
