package analysis

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// FailurePoints finds exact thresholds where the portfolio fails by
// running a binary search over each parameter (return, inflation,
// expenses, portfolio value). Returns BaselineSurvives=false (and an
// empty FailurePoints slice) when the baseline projection itself
// fails — in that case there's no meaningful failure threshold to find.
func FailurePoints(eng *engine.Engine, in engine.Input) *models.FailurePointAnalysis {
	baseProjection := eng.Run(in)
	failurePoints := make([]models.FailurePoint, 0)

	// If baseline already fails, we can't find "failure thresholds"
	if !baseProjection.Survives {
		return &models.FailurePointAnalysis{
			FailurePoints:    failurePoints,
			BaselineSurvives: false,
		}
	}

	// Find minimum investment return needed
	if fp := findReturnThreshold(eng, in); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum inflation tolerable
	if fp := findInflationThreshold(eng, in); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum expenses tolerable
	if fp := findExpensesThreshold(eng, in); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find minimum portfolio needed
	if fp := findPortfolioThreshold(eng, in); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	return &models.FailurePointAnalysis{
		FailurePoints:    failurePoints,
		BaselineSurvives: true,
	}
}

// FindReturnThreshold, FindInflationThreshold, FindExpensesThreshold,
// and FindPortfolioThreshold are exported so retirement-package test
// helpers can drive a single threshold search directly.
func FindReturnThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	return findReturnThreshold(eng, in)
}

// FindInflationThreshold forwards to findInflationThreshold.
func FindInflationThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	return findInflationThreshold(eng, in)
}

// FindExpensesThreshold forwards to findExpensesThreshold.
func FindExpensesThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	return findExpensesThreshold(eng, in)
}

// FindPortfolioThreshold forwards to findPortfolioThreshold.
func FindPortfolioThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	return findPortfolioThreshold(eng, in)
}

// findReturnThreshold finds minimum investment return to survive.
// Returns nil when InvestmentReturn==0, the sentinel for allocation-based
// returns: the binary search would override per-account allocation with a
// single flat rate, producing thresholds that aren't comparable to the
// projection's actual blended return.
func findReturnThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	s := in.Prepared.Settings()
	current := s.InvestmentReturn
	if current == 0 {
		return nil
	}

	// Binary search between 0% and current value
	low, high := -5.0, current
	precision := 0.1

	// First check if 0% return survives
	modSettings := *s
	modSettings.IncomeSources = append([]models.IncomeSource{}, s.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, s.ExpenseSources...)
	modSettings.InvestmentReturn = low
	if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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
		if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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

// findInflationThreshold finds maximum inflation before failure.
func findInflationThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	s := in.Prepared.Settings()
	current := s.InflationRate

	// Binary search between current and 15%
	low, high := current, 15.0
	precision := 0.1

	// First check if 15% inflation fails
	modSettings := *s
	modSettings.IncomeSources = append([]models.IncomeSource{}, s.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, s.ExpenseSources...)
	modSettings.InflationRate = high
	if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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
		if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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

// findExpensesThreshold finds maximum monthly expenses before failure.
func findExpensesThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	s := in.Prepared.Settings()
	current := s.MonthlyLivingExpenses
	if current <= 0 {
		return nil
	}

	// Binary search between current and 3x current
	low, high := current, current*3
	precision := 50.0 // $50 precision

	// First check if 3x expenses fails
	modSettings := *s
	modSettings.IncomeSources = append([]models.IncomeSource{}, s.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, s.ExpenseSources...)
	modSettings.MonthlyLivingExpenses = high
	if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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
		if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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

// findPortfolioThreshold finds minimum portfolio needed to survive.
func findPortfolioThreshold(eng *engine.Engine, in engine.Input) *models.FailurePoint {
	s := in.Prepared.Settings()
	current := s.PortfolioValue
	if current <= 0 {
		return nil
	}

	// Binary search between 0 and current
	low, high := 0.0, current
	precision := 1000.0 // $1000 precision

	// First check if $0 survives (e.g., income covers all expenses)
	modSettings := *s
	modSettings.IncomeSources = append([]models.IncomeSource{}, s.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, s.ExpenseSources...)
	modSettings.PortfolioValue = low
	if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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
		if eng.Run(engine.Input{Prepared: perturbAndPrepare(&modSettings), Chain: in.Chain}).Survives {
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
