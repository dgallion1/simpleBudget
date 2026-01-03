package retirement

import (
	"fmt"
	"math"
	"math/rand"
	"time"

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

// RunProjection runs a full retirement projection with RMD integration
func (c *Calculator) RunProjection() *models.ProjectionResult {
	s := c.Settings
	months := s.ProjectionYears * 12
	projection := make([]models.ProjectionMonth, 0, months)

	// Split portfolio into tax-deferred and taxable portions
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	taxableBalance := s.PortfolioValue - taxDeferredBalance

	healthcareStartMonth := s.HealthcareStartYears * 12
	var depletionMonth *int
	var longevityYears *float64

	currentLivingExpenses := s.MonthlyLivingExpenses
	currentHealthcareExpenses := s.MonthlyHealthcare

	// Track annual RMD (calculated once per year, distributed monthly)
	var annualRMD float64
	var monthlyRMD float64

	for m := 0; m < months; m++ {
		currentAge := s.CurrentAge + (m / 12)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 {
				netInflation := (s.InflationRate - s.SpendingDeclineRate) / 100
				currentLivingExpenses *= (1 + netInflation)
				currentHealthcareExpenses *= (1 + s.HealthcareInflation/100)
			}

			// Calculate annual RMD at start of each year (age 73+)
			if currentAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, currentAge)
				monthlyRMD = annualRMD / 12
			} else {
				annualRMD = 0
				monthlyRMD = 0
			}
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

		// Monthly cash flow needed from portfolio
		neededFromPortfolio := totalExpenses - totalIncome

		// Apply investment growth to both portions
		taxDeferredGrowth := taxDeferredBalance * (s.InvestmentReturn / 100 / 12)
		taxableGrowth := taxableBalance * (s.InvestmentReturn / 100 / 12)
		totalGrowth := taxDeferredGrowth + taxableGrowth

		taxDeferredBalance += taxDeferredGrowth
		taxableBalance += taxableGrowth

		// Process withdrawals with RMD priority
		rmdWithdrawal := 0.0
		actualWithdrawal := 0.0

		if neededFromPortfolio > 0 {
			// First, take from RMD (which must be withdrawn anyway)
			if monthlyRMD > 0 {
				rmdUsed := math.Min(monthlyRMD, neededFromPortfolio)
				rmdUsed = math.Min(rmdUsed, taxDeferredBalance) // Can't withdraw more than available
				taxDeferredBalance -= rmdUsed
				neededFromPortfolio -= rmdUsed
				rmdWithdrawal = rmdUsed
				actualWithdrawal += rmdUsed
			}

			// If still need more, withdraw from taxable first (tax-efficient)
			if neededFromPortfolio > 0 && taxableBalance > 0 {
				fromTaxable := math.Min(neededFromPortfolio, taxableBalance)
				taxableBalance -= fromTaxable
				neededFromPortfolio -= fromTaxable
				actualWithdrawal += fromTaxable
			}

			// If still need more, withdraw additional from tax-deferred
			if neededFromPortfolio > 0 && taxDeferredBalance > 0 {
				fromTaxDeferred := math.Min(neededFromPortfolio, taxDeferredBalance)
				taxDeferredBalance -= fromTaxDeferred
				neededFromPortfolio -= fromTaxDeferred
				actualWithdrawal += fromTaxDeferred
			}
		} else {
			// Expenses covered by income, but RMD still must be withdrawn
			// RMD goes to taxable account (reinvested after taxes in practice)
			if monthlyRMD > 0 && taxDeferredBalance > 0 {
				rmdWithdrawal = math.Min(monthlyRMD, taxDeferredBalance)
				taxDeferredBalance -= rmdWithdrawal
				taxableBalance += rmdWithdrawal // RMD moves to taxable
			}
		}

		totalBalance := taxDeferredBalance + taxableBalance
		depleted := false
		if totalBalance <= 0 {
			taxDeferredBalance = 0
			taxableBalance = 0
			totalBalance = 0
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		}

		projection = append(projection, models.ProjectionMonth{
			Month:              m,
			Year:               float64(m) / 12,
			PortfolioBalance:   totalBalance,
			TaxDeferredBalance: taxDeferredBalance,
			TaxableBalance:     taxableBalance,
			GeneralExpenses:    currentLivingExpenses,
			HealthcareExpense:  activeHealthcare,
			TotalExpenses:      totalExpenses,
			TotalIncome:        totalIncome,
			NetWithdrawal:      actualWithdrawal,
			RMDWithdrawal:      rmdWithdrawal,
			PortfolioGrowth:    totalGrowth,
			Depleted:           depleted,
		})
	}

	finalBalance := 0.0
	if len(projection) > 0 {
		finalBalance = projection[len(projection)-1].PortfolioBalance
	}

	return &models.ProjectionResult{
		Months:         projection,
		LongevityYears: longevityYears,
		FinalBalance:   finalBalance,
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

	// Calculate RMD if age 73+ and have tax-deferred balance
	monthlyRMD := 0.0
	if s.CurrentAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
		taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
		annualRMD, _ := CalculateRMD(taxDeferredBalance, s.CurrentAge)
		monthlyRMD = annualRMD / 12
	}

	// Gap = Expenses - Income - RMD (RMD is forced withdrawal that can cover expenses)
	monthlyGap := monthlyExpenses - monthlyIncome - monthlyRMD
	annualGap := monthlyGap * 12

	// Calculate required withdrawal rate (only for positive gap after RMD)
	requiredRate := 0.0
	if s.PortfolioValue > 0 && monthlyGap > 0 {
		requiredRate = (annualGap / s.PortfolioValue) * 100
	}

	return &models.BudgetFitAnalysis{
		MonthlyExpenses: monthlyExpenses,
		MonthlyIncome:   monthlyIncome,
		MonthlyRMD:      monthlyRMD,
		MonthlyGap:      monthlyGap,
		AnnualGap:       annualGap,
		RequiredRate:    requiredRate,
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

// CalculateFailurePoints finds exact thresholds where the portfolio fails
func (c *Calculator) CalculateFailurePoints() *models.FailurePointAnalysis {
	baseProjection := c.RunProjection()
	failurePoints := make([]models.FailurePoint, 0)

	// If baseline already fails, we can't find "failure thresholds"
	if !baseProjection.Survives {
		return &models.FailurePointAnalysis{
			FailurePoints:    failurePoints,
			BaselineSurvives: false,
		}
	}

	// Find minimum investment return needed
	if fp := c.findReturnThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum inflation tolerable
	if fp := c.findInflationThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum expenses tolerable
	if fp := c.findExpensesThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find minimum portfolio needed
	if fp := c.findPortfolioThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	return &models.FailurePointAnalysis{
		FailurePoints:    failurePoints,
		BaselineSurvives: true,
	}
}

// findReturnThreshold finds minimum investment return to survive
func (c *Calculator) findReturnThreshold() *models.FailurePoint {
	current := c.Settings.InvestmentReturn

	// Binary search between 0% and current value
	low, high := -5.0, current
	precision := 0.1

	// First check if 0% return survives
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.InvestmentReturn = low
	modCalc := NewCalculator(&modSettings)
	if modCalc.RunProjection().Survives {
		// Survives even at -5%, no meaningful threshold
		return &models.FailurePoint{
			ParamName:    "investment_return",
			ParamLabel:   "Investment Return",
			CurrentValue: current,
			Threshold:    -5.0,
			Direction:    "below",
			Margin:       current + 5.0,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.InvestmentReturn = mid
		modCalc := NewCalculator(&modSettings)
		if modCalc.RunProjection().Survives {
			high = mid
		} else {
			low = mid
		}
	}

	threshold := math.Round(high*10) / 10
	margin := current - threshold
	safetyLevel := "safe"
	if margin < 1 {
		safetyLevel = "critical"
	} else if margin < 2 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "investment_return",
		ParamLabel:   "Investment Return",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "below",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findInflationThreshold finds maximum inflation before failure
func (c *Calculator) findInflationThreshold() *models.FailurePoint {
	current := c.Settings.InflationRate

	// Binary search between current and 15%
	low, high := current, 15.0
	precision := 0.1

	// First check if 15% inflation fails
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.InflationRate = high
	modCalc := NewCalculator(&modSettings)
	if modCalc.RunProjection().Survives {
		// Survives even at 15%, very robust
		return &models.FailurePoint{
			ParamName:    "inflation_rate",
			ParamLabel:   "Inflation Rate",
			CurrentValue: current,
			Threshold:    15.0,
			Direction:    "above",
			Margin:       15.0 - current,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.InflationRate = mid
		modCalc := NewCalculator(&modSettings)
		if modCalc.RunProjection().Survives {
			low = mid
		} else {
			high = mid
		}
	}

	threshold := math.Round(low*10) / 10
	margin := threshold - current
	safetyLevel := "safe"
	if margin < 1 {
		safetyLevel = "critical"
	} else if margin < 2 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "inflation_rate",
		ParamLabel:   "Inflation Rate",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "above",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findExpensesThreshold finds maximum monthly expenses before failure
func (c *Calculator) findExpensesThreshold() *models.FailurePoint {
	current := c.Settings.MonthlyLivingExpenses
	if current <= 0 {
		return nil
	}

	// Binary search between current and 3x current
	low, high := current, current*3
	precision := 50.0 // $50 precision

	// First check if 3x expenses fails
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.MonthlyLivingExpenses = high
	modCalc := NewCalculator(&modSettings)
	if modCalc.RunProjection().Survives {
		// Survives even at 3x expenses
		margin := ((high / current) - 1) * 100
		return &models.FailurePoint{
			ParamName:    "monthly_expenses",
			ParamLabel:   "Monthly Expenses",
			CurrentValue: current,
			Threshold:    high,
			Direction:    "above",
			Margin:       margin,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.MonthlyLivingExpenses = mid
		modCalc := NewCalculator(&modSettings)
		if modCalc.RunProjection().Survives {
			low = mid
		} else {
			high = mid
		}
	}

	threshold := math.Round(low/50) * 50 // Round to nearest $50
	margin := ((threshold / current) - 1) * 100
	safetyLevel := "safe"
	if margin < 10 {
		safetyLevel = "critical"
	} else if margin < 25 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "monthly_expenses",
		ParamLabel:   "Monthly Expenses",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "above",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findPortfolioThreshold finds minimum portfolio needed to survive
func (c *Calculator) findPortfolioThreshold() *models.FailurePoint {
	current := c.Settings.PortfolioValue
	if current <= 0 {
		return nil
	}

	// Binary search between 0 and current
	low, high := 0.0, current
	precision := 1000.0 // $1000 precision

	// First check if $0 survives (e.g., income covers all expenses)
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.PortfolioValue = low
	modCalc := NewCalculator(&modSettings)
	if modCalc.RunProjection().Survives {
		return &models.FailurePoint{
			ParamName:    "portfolio_value",
			ParamLabel:   "Portfolio Value",
			CurrentValue: current,
			Threshold:    0,
			Direction:    "below",
			Margin:       100, // 100% buffer
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.PortfolioValue = mid
		modCalc := NewCalculator(&modSettings)
		if modCalc.RunProjection().Survives {
			high = mid
		} else {
			low = mid
		}
	}

	threshold := math.Round(high/1000) * 1000 // Round to nearest $1000
	margin := ((current - threshold) / current) * 100
	safetyLevel := "safe"
	if margin < 10 {
		safetyLevel = "critical"
	} else if margin < 25 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "portfolio_value",
		ParamLabel:   "Portfolio Value",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "below",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// RunMonteCarloSimulation runs randomized scenario analysis
func (c *Calculator) RunMonteCarloSimulation(runs int) *models.MonteCarloAnalysis {
	if runs <= 0 {
		runs = 1000
	}

	results := make([]models.MonteCarloResult, runs)
	successCount := 0
	totalDepletionYears := 0.0
	depletionCount := 0

	// Parameters for random variation (using uniform distribution)
	// Return: base +/- 4% range
	// Inflation: base +/- 2% range
	returnBase := c.Settings.InvestmentReturn
	inflationBase := c.Settings.InflationRate

	// Create a new random source seeded with current time for true randomness
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < runs; i++ {
		// Create varied settings
		modSettings := *c.Settings
		modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
		modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)

		// Random variations: uniform distribution centered on base values
		returnVar := (rng.Float64() - 0.5) * 8   // -4 to +4
		inflationVar := (rng.Float64() - 0.5) * 4 // -2 to +2

		modSettings.InvestmentReturn = math.Max(0, returnBase+returnVar)
		modSettings.InflationRate = math.Max(0, inflationBase+inflationVar)

		modCalc := NewCalculator(&modSettings)
		projection := modCalc.RunProjection()

		result := models.MonteCarloResult{
			FinalBalance: projection.FinalBalance,
			Survives:     projection.Survives,
		}

		if projection.LongevityYears != nil {
			result.DepletionYear = *projection.LongevityYears
			totalDepletionYears += *projection.LongevityYears
			depletionCount++
		}

		if projection.Survives {
			successCount++
		}

		results[i] = result
	}

	// Calculate statistics
	balances := make([]float64, runs)
	for i, r := range results {
		balances[i] = r.FinalBalance
	}
	sortFloat64s(balances)

	stats := &models.MonteCarloStats{
		Runs:         runs,
		SuccessRate:  float64(successCount) / float64(runs) * 100,
		MedianBalance: balances[runs/2],
		MeanBalance:   mean(balances),
		Percentile10:  balances[runs/10],
		Percentile25:  balances[runs/4],
		Percentile75:  balances[runs*3/4],
		Percentile90:  balances[runs*9/10],
		WorstCase:     balances[0],
		BestCase:      balances[runs-1],
	}

	if depletionCount > 0 {
		stats.AvgDepletionYr = totalDepletionYears / float64(depletionCount)
	}

	// Create distribution buckets
	distribution := c.createDistributionBuckets(balances)

	return &models.MonteCarloAnalysis{
		Stats:        stats,
		Distribution: distribution,
	}
}

// createDistributionBuckets creates histogram buckets for visualization
func (c *Calculator) createDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	buckets := make([]models.MonteCarloDistBucket, 0)
	total := len(sortedBalances)

	// Define bucket boundaries based on data range
	maxVal := sortedBalances[total-1]

	// Use fixed boundaries with more detail in 0-3M range
	var boundaries []float64
	if maxVal <= 0 {
		boundaries = []float64{0}
	} else if maxVal < 100000 {
		boundaries = []float64{0, 10000, 25000, 50000, 75000, 100000}
	} else if maxVal < 1000000 {
		boundaries = []float64{0, 100000, 250000, 500000, 750000, 1000000}
	} else if maxVal < 3000000 {
		// Fine detail for 0-3M range
		boundaries = []float64{0, 250000, 500000, 1000000, 1500000, 2000000, 2500000, 3000000}
	} else {
		// Fixed boundaries with detail in 0-3M, then larger buckets for higher values
		boundaries = []float64{0, 250000, 500000, 1000000, 2000000, 3000000, 5000000, 10000000}
		// Add boundaries beyond 10M if needed
		if maxVal > 10000000 {
			boundaries = append(boundaries, 20000000)
		}
	}

	// Count items in each bucket
	for i := 0; i < len(boundaries)-1; i++ {
		low := boundaries[i]
		high := boundaries[i+1]
		count := 0
		for _, b := range sortedBalances {
			if b >= low && b < high {
				count++
			}
		}
		if count > 0 || i == 0 { // Always show first bucket even if empty
			buckets = append(buckets, models.MonteCarloDistBucket{
				Label:      formatBucketLabel(low, high),
				Count:      count,
				Percentage: float64(count) / float64(total) * 100,
			})
		}
	}

	// Add final bucket for values at or above last boundary
	lastBoundary := boundaries[len(boundaries)-1]
	count := 0
	for _, b := range sortedBalances {
		if b >= lastBoundary {
			count++
		}
	}
	if count > 0 {
		buckets = append(buckets, models.MonteCarloDistBucket{
			Label:      formatBucketLabel(lastBoundary, -1),
			Count:      count,
			Percentage: float64(count) / float64(total) * 100,
		})
	}

	return &models.MonteCarloDistribution{Buckets: buckets}
}

// formatBucketLabel formats a bucket range for display
func formatBucketLabel(low, high float64) string {
	formatVal := func(v float64) string {
		if v >= 1000000 {
			return fmt.Sprintf("$%.1fM", v/1000000)
		}
		return fmt.Sprintf("$%.0fK", v/1000)
	}

	if high < 0 {
		return formatVal(low) + "+"
	}
	return formatVal(low) + "-" + formatVal(high)
}

// Helper functions
func sortFloat64s(a []float64) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func mean(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range a {
		sum += v
	}
	return sum / float64(len(a))
}

// RunFullAnalysis performs complete what-if analysis
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	projection := c.RunProjection()
	budgetFit := c.CalculateBudgetFit()
	presentValue := c.CalculatePresentValueAnalysis()
	sustainability := c.CalculateSustainabilityScore(projection)
	sensitivity := c.CalculateSensitivity()
	failurePoints := c.CalculateFailurePoints()
	monteCarlo := c.RunMonteCarloSimulation(1000)
	rmd := c.CalculateRMDAnalysis()

	return &models.WhatIfAnalysis{
		Settings:       c.Settings,
		Projection:     projection,
		BudgetFit:      budgetFit,
		PresentValue:   presentValue,
		Sustainability: sustainability,
		Sensitivity:    sensitivity,
		FailurePoints:  failurePoints,
		MonteCarlo:     monteCarlo,
		RMD:            rmd,
	}
}
