package models

import "math"

// WhatIfSettings contains all user parameters for retirement planning
type WhatIfSettings struct {
	// Portfolio
	PortfolioValue float64 `json:"portfolio_value"` // Current portfolio value

	// Expenses
	MonthlyLivingExpenses float64 `json:"monthly_living_expenses"` // Base monthly expenses
	MonthlyHealthcare     float64 `json:"monthly_healthcare"`      // Monthly healthcare costs (legacy)
	HealthcareStartYears  int     `json:"healthcare_start_years"`  // Years until healthcare starts (legacy)

	// Multi-person healthcare model
	HealthcarePersons []HealthcarePerson `json:"healthcare_persons,omitempty"`

	// RMD Settings
	CurrentAge         int     `json:"current_age"`          // User's current age
	TaxDeferredPercent float64 `json:"tax_deferred_percent"` // % of portfolio in tax-deferred accounts

	// Rates (as percentages, e.g., 4.0 for 4%)
	InflationRate         float64 `json:"inflation_rate"`          // Annual inflation
	HealthcareInflation   float64 `json:"healthcare_inflation"`    // Healthcare inflation (legacy, for single-person model)
	SpendingDeclineRate   float64 `json:"spending_decline_rate"`   // Annual spending reduction
	InvestmentReturn      float64 `json:"investment_return"`       // Expected portfolio return
	DiscountRate          float64 `json:"discount_rate"`           // For PV calculations

	// Projection
	ProjectionYears         int     `json:"projection_years"`           // Number of years to project
	SteadyStateOverrideYear float64 `json:"steady_state_override_year"` // User-adjustable projection year (0 = auto)

	// Income and Expense Sources
	IncomeSources  []IncomeSource  `json:"income_sources"`
	ExpenseSources []ExpenseSource `json:"expense_sources"`

	// Recently Removed (for restore functionality)
	RemovedIncomeSources  []IncomeSource  `json:"removed_income_sources,omitempty"`
	RemovedExpenseSources []ExpenseSource `json:"removed_expense_sources,omitempty"`
}

// GetTotalHealthcareCost returns total healthcare cost for a given month
// Uses multi-person model if HealthcarePersons is populated, otherwise falls back to legacy single value
func (s *WhatIfSettings) GetTotalHealthcareCost(month int) float64 {
	// Use multi-person model if available
	if len(s.HealthcarePersons) > 0 {
		total := 0.0
		for _, person := range s.HealthcarePersons {
			total += person.GetMonthlyCost(month)
		}
		return total
	}

	// Legacy single-value model
	healthcareStartMonth := s.HealthcareStartYears * 12
	if month < healthcareStartMonth {
		return 0
	}

	yearsActive := (month - healthcareStartMonth) / 12
	if yearsActive < 0 {
		yearsActive = 0
	}

	// Apply healthcare inflation to legacy model
	return s.MonthlyHealthcare * math.Pow(1+s.HealthcareInflation/100, float64(yearsActive))
}

// HasMultiPersonHealthcare returns true if multi-person healthcare model is being used
func (s *WhatIfSettings) HasMultiPersonHealthcare() bool {
	return len(s.HealthcarePersons) > 0
}

// DefaultWhatIfSettings returns sensible defaults for retirement planning
func DefaultWhatIfSettings() *WhatIfSettings {
	return &WhatIfSettings{
		PortfolioValue:        0,
		MonthlyLivingExpenses: 4000,
		MonthlyHealthcare:     500,
		HealthcareStartYears:  0,
		CurrentAge:            65,
		TaxDeferredPercent:    70.0,
		InflationRate:         3.0,
		HealthcareInflation:   6.0,
		SpendingDeclineRate:   1.0,
		InvestmentReturn:      6.0,
		DiscountRate:          5.0,
		ProjectionYears:       30,
		IncomeSources:         []IncomeSource{},
		ExpenseSources:        []ExpenseSource{},
		RemovedIncomeSources:  []IncomeSource{},
		RemovedExpenseSources: []ExpenseSource{},
	}
}

// ProjectionMonth represents a single month in the projection
type ProjectionMonth struct {
	Month              int     `json:"month"`
	Year               float64 `json:"year"`
	PortfolioBalance   float64 `json:"portfolio_balance"`
	TaxDeferredBalance float64 `json:"tax_deferred_balance"` // Tax-deferred portion (401k, IRA)
	TaxableBalance     float64 `json:"taxable_balance"`      // Taxable portion (brokerage)
	GeneralExpenses    float64 `json:"general_expenses"`
	HealthcareExpense  float64 `json:"healthcare_expense"`
	TotalExpenses      float64 `json:"total_expenses"`
	TotalIncome        float64 `json:"total_income"`
	NetWithdrawal      float64 `json:"net_withdrawal"`
	RMDWithdrawal      float64 `json:"rmd_withdrawal"` // Forced RMD withdrawal (age 73+)
	PortfolioGrowth    float64 `json:"portfolio_growth"`
	Depleted           bool    `json:"depleted"`
}

// ProjectionResult contains the complete projection with summary metrics
type ProjectionResult struct {
	Months          []ProjectionMonth `json:"months"`
	LongevityYears  *float64          `json:"longevity_years"`  // nil if portfolio survives
	FinalBalance    float64           `json:"final_balance"`
	DepletionMonth  *int              `json:"depletion_month"`  // nil if no depletion
	Survives        bool              `json:"survives"`
}

// BudgetFitAnalysis shows monthly gap and required rates
type BudgetFitAnalysis struct {
	MonthlyExpenses   float64 `json:"monthly_expenses"`
	MonthlyIncome     float64 `json:"monthly_income"`
	MonthlyRMD        float64 `json:"monthly_rmd"`        // Required Minimum Distribution (age 73+)
	MonthlyGap        float64 `json:"monthly_gap"`        // Expenses - Income - RMD
	AnnualGap         float64 `json:"annual_gap"`
	RequiredRate      float64 `json:"required_rate"`      // Rate needed to cover gap

	// RMD/Gap relationship fields
	GapBeforeRMD float64 `json:"gap_before_rmd"` // Expenses - Income (before RMD applied)
	RMDCoverage  float64 `json:"rmd_coverage"`   // How much of the gap RMD covers
	ExcessRMD    float64 `json:"excess_rmd"`     // RMD beyond what's needed (forced taxable withdrawal)

	// Steady-state analysis (when all income sources are active)
	SteadyStateMonth    int     `json:"steady_state_month"`    // Month when all income is active
	SteadyStateYear     float64 `json:"steady_state_year"`     // Year when all income is active (or override)
	MinSteadyStateYear  float64 `json:"min_steady_state_year"` // Auto-calculated minimum (when all income starts)
	SteadyStateExpenses float64 `json:"steady_state_expenses"` // Expenses at steady state (inflated)
	SteadyStateIncome   float64 `json:"steady_state_income"`   // Income at steady state (with COLA)
	SteadyStateRMD      float64 `json:"steady_state_rmd"`      // RMD at steady state (if applicable)
	SteadyStateGap      float64 `json:"steady_state_gap"`      // Gap at steady state
	SteadyStateRate     float64 `json:"steady_state_rate"`     // Required withdrawal rate at steady state
	HasSteadyState      bool    `json:"has_steady_state"`      // True if steady state differs from current
}

// RMDProjection represents RMD estimates for a specific year
type RMDProjection struct {
	Age              int     `json:"age"`
	Year             int     `json:"year"`              // Years from now
	TaxDeferredBal   float64 `json:"tax_deferred_bal"`  // Estimated balance at start of year
	LifeExpFactor    float64 `json:"life_exp_factor"`   // IRS Uniform Lifetime factor
	RMDAmount        float64 `json:"rmd_amount"`        // Required distribution
	RMDPercent       float64 `json:"rmd_percent"`       // RMD as % of tax-deferred balance
}

// RMDAnalysis contains RMD projections and summary
type RMDAnalysis struct {
	StartsInYears     int              `json:"starts_in_years"`     // Years until RMDs begin
	StartAge          int              `json:"start_age"`           // Age when RMDs begin (73)
	CurrentAge        int              `json:"current_age"`
	TaxDeferredValue  float64          `json:"tax_deferred_value"`  // Current tax-deferred balance
	Projections       []RMDProjection  `json:"projections"`         // Year-by-year projections
	TotalRMDsOver10Yr float64          `json:"total_rmds_10yr"`     // Sum of first 10 years of RMDs
}

// PresentValueAnalysis shows PV of expenses vs income
type PresentValueAnalysis struct {
	PVExpenses      float64 `json:"pv_expenses"`
	PVIncome        float64 `json:"pv_income"`
	PVGap           float64 `json:"pv_gap"`
	CoverageRatio   float64 `json:"coverage_ratio"`   // (Portfolio + PV Income) / PV Expenses
	SurplusDeficit  float64 `json:"surplus_deficit"`  // Portfolio + PV Income - PV Expenses
}

// SustainabilityScore represents a 0-100 score with visual attributes
type SustainabilityScore struct {
	Score       int     `json:"score"`        // 0-100
	Label       string  `json:"label"`        // "Excellent", "Good", "Fair", "Poor", "Critical"
	Color       string  `json:"color"`        // CSS color class
	Description string  `json:"description"`
}

// CalculateSustainabilityScore computes score from withdrawal rate
func CalculateSustainabilityScore(requiredRate float64, survives bool) *SustainabilityScore {
	var score int
	var label, color, description string

	if !survives {
		score = 0
		label = "Critical"
		color = "red"
		description = "Portfolio depletes before projection end"
	} else if requiredRate <= 3 {
		score = 100
		label = "Excellent"
		color = "green"
		description = "Very sustainable withdrawal rate"
	} else if requiredRate <= 4 {
		score = 90
		label = "Good"
		color = "green"
		description = "Sustainable based on 4% rule"
	} else if requiredRate <= 5 {
		score = 75
		label = "Fair"
		color = "yellow"
		description = "Moderate risk, consider reducing expenses"
	} else if requiredRate <= 6 {
		score = 60
		label = "Caution"
		color = "orange"
		description = "Higher risk of depletion"
	} else if requiredRate <= 8 {
		score = 40
		label = "Poor"
		color = "orange"
		description = "High withdrawal rate, adjustments recommended"
	} else {
		score = int(max(0, 100-(requiredRate-3)*15))
		label = "Critical"
		color = "red"
		description = "Unsustainable withdrawal rate"
	}

	return &SustainabilityScore{
		Score:       score,
		Label:       label,
		Color:       color,
		Description: description,
	}
}

// SensitivityScenario defines a parameter variation for testing
type SensitivityScenario struct {
	Name       string  `json:"name"`
	ParamName  string  `json:"param_name"`
	ParamValue float64 `json:"param_value"`
	Change     string  `json:"change"` // e.g., "+2%", "-1%"
}

// SensitivityResult contains the outcome of a scenario test
type SensitivityResult struct {
	Scenario       SensitivityScenario `json:"scenario"`
	LongevityYears *float64            `json:"longevity_years"`
	FinalBalance   float64             `json:"final_balance"`
	Survives       bool                `json:"survives"`
	ScoreChange    int                 `json:"score_change"` // vs baseline
}

// FailurePoint represents the threshold where a parameter causes portfolio failure
type FailurePoint struct {
	ParamName    string  `json:"param_name"`    // e.g., "investment_return"
	ParamLabel   string  `json:"param_label"`   // e.g., "Investment Return"
	CurrentValue float64 `json:"current_value"` // Current setting value
	Threshold    float64 `json:"threshold"`     // Value at which failure occurs
	Direction    string  `json:"direction"`     // "below" or "above"
	Margin       float64 `json:"margin"`        // How much buffer before failure (as %)
	SafetyLevel  string  `json:"safety_level"`  // "safe", "marginal", "critical"
}

// FailurePointAnalysis contains all failure thresholds
type FailurePointAnalysis struct {
	FailurePoints []FailurePoint `json:"failure_points"`
	BaselineSurvives bool        `json:"baseline_survives"` // Does current scenario survive?
}

// MonteCarloResult represents a single simulation run outcome
type MonteCarloResult struct {
	FinalBalance    float64 `json:"final_balance"`
	DepletionYear   float64 `json:"depletion_year"` // 0 if survives
	Survives        bool    `json:"survives"`
	MarketCrashes   int     `json:"market_crashes"`   // Number of crash years
	SpendingShocks  int     `json:"spending_shocks"`  // Number of spending shock events
	HealthShocks    int     `json:"health_shocks"`    // Number of health emergency events
	ProjectionYears int     `json:"projection_years"` // Actual years projected (varies with longevity)

	// Crash timing breakdown
	EarlyCrashes   int `json:"early_crashes"`    // Crashes in years 1-5
	MidCrashes     int `json:"mid_crashes"`      // Crashes in years 6-15
	LateCrashes    int `json:"late_crashes"`     // Crashes in years 16+
	FirstCrashYear int `json:"first_crash_year"` // Year of first crash (0 if none)
}

// SequenceRiskBreakdown provides detailed crash timing analysis
type SequenceRiskBreakdown struct {
	// Survival rates by crash timing
	NoCrashSurvival    float64 `json:"no_crash_survival"`    // Survival rate with no crashes
	EarlyCrashSurvival float64 `json:"early_crash_survival"` // Survival rate when crashes in years 1-5
	MidCrashSurvival   float64 `json:"mid_crash_survival"`   // Survival rate when crashes in years 6-15
	LateCrashSurvival  float64 `json:"late_crash_survival"`  // Survival rate when crashes in years 16+

	// Sample sizes for each category
	NoCrashCount    int `json:"no_crash_count"`
	EarlyCrashCount int `json:"early_crash_count"`
	MidCrashCount   int `json:"mid_crash_count"`
	LateCrashCount  int `json:"late_crash_count"`

	// Impact metrics
	EarlyVsLateImpact float64 `json:"early_vs_late_impact"` // Difference: late survival - early survival
	EarlyVsNoneImpact float64 `json:"early_vs_none_impact"` // Difference: no crash survival - early survival

	// Recovery analysis
	RecoveryRate     float64 `json:"recovery_rate"`      // % of early crash runs that still survived
	AvgRecoveryYears float64 `json:"avg_recovery_years"` // Avg years to recover after early crash

	// Buffer recommendation (years of expenses to hold safe)
	RecommendedBuffer int     `json:"recommended_buffer"`
	BufferRationale   string  `json:"buffer_rationale"`
	BufferAmount      float64 `json:"buffer_amount"`       // Dollar amount of recommended buffer
	AnnualExpenses    float64 `json:"annual_expenses"`     // Annual expenses used for buffer calculation
	AdjustedSpending  float64 `json:"adjusted_spending"`   // Monthly spending if buffer is set aside from portfolio
}

// MonteCarloStats contains aggregated simulation statistics
type MonteCarloStats struct {
	Runs            int     `json:"runs"`             // Number of simulations
	SuccessRate     float64 `json:"success_rate"`     // % of scenarios that survive
	MedianBalance   float64 `json:"median_balance"`   // Median final balance
	MeanBalance     float64 `json:"mean_balance"`     // Average final balance
	Percentile10    float64 `json:"percentile_10"`    // 10th percentile (worst 10%)
	Percentile25    float64 `json:"percentile_25"`    // 25th percentile
	Percentile75    float64 `json:"percentile_75"`    // 75th percentile
	Percentile90    float64 `json:"percentile_90"`    // 90th percentile (best 10%)
	WorstCase       float64 `json:"worst_case"`       // Minimum final balance
	BestCase        float64 `json:"best_case"`        // Maximum final balance
	AvgDepletionYr  float64 `json:"avg_depletion_yr"` // Avg years to depletion (failed runs only)

	// Enhanced simulation stats
	MarketCrashCount   int     `json:"market_crash_count"`   // Runs that experienced crashes
	SpendingShockCount int     `json:"spending_shock_count"` // Runs with spending shocks
	HealthShockCount   int     `json:"health_shock_count"`   // Runs with health emergencies
	AvgCrashesPerRun   float64 `json:"avg_crashes_per_run"`  // Average market crashes per simulation
	AvgShocksPerRun    float64 `json:"avg_shocks_per_run"`   // Average spending shocks per simulation
	SequenceRiskImpact float64 `json:"sequence_risk_impact"` // How much sequence of returns affected outcomes

	// Detailed sequence risk analysis
	SequenceRisk *SequenceRiskBreakdown `json:"sequence_risk"`
}

// MonteCarloDistribution contains bucketed results for visualization
type MonteCarloDistribution struct {
	Buckets []MonteCarloDistBucket `json:"buckets"`
}

// MonteCarloDistBucket represents a histogram bucket
type MonteCarloDistBucket struct {
	Label      string `json:"label"`      // e.g., "$0-100K"
	Count      int    `json:"count"`      // Number of simulations in this bucket
	Percentage float64 `json:"percentage"` // % of total
}

// MonteCarloAnalysis contains complete simulation analysis
type MonteCarloAnalysis struct {
	Stats        *MonteCarloStats        `json:"stats"`
	Distribution *MonteCarloDistribution `json:"distribution"`
}

// WhatIfAnalysis is the complete analysis container returned to templates
type WhatIfAnalysis struct {
	Settings       *WhatIfSettings       `json:"settings"`
	Projection     *ProjectionResult     `json:"projection"`
	BudgetFit      *BudgetFitAnalysis    `json:"budget_fit"`
	PresentValue   *PresentValueAnalysis `json:"present_value"`
	Sustainability *SustainabilityScore  `json:"sustainability"`
	Sensitivity    []SensitivityResult   `json:"sensitivity"`
	FailurePoints  *FailurePointAnalysis `json:"failure_points"`
	MonteCarlo     *MonteCarloAnalysis   `json:"monte_carlo"`
	RMD            *RMDAnalysis          `json:"rmd"`
}

// WhatIfPageData is the data passed to the whatif template
type WhatIfPageData struct {
	Title     string          `json:"title"`
	ActiveTab string          `json:"active_tab"`
	Settings  *WhatIfSettings `json:"settings"`
	Analysis  *WhatIfAnalysis `json:"analysis"`
}
