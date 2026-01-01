package models

// UserSettings contains all user-configurable settings for the application
type UserSettings struct {
	// Retirement calculator settings
	CurrentSavings            float64         `json:"current_savings"`
	MonthlyExpenses           float64         `json:"monthly_expenses"`
	IncomeSources             []IncomeSource  `json:"income_sources"`
	ExpenseSources            []ExpenseSource `json:"expense_sources"`
	WithdrawalRate            float64         `json:"withdrawal_rate"`              // Max withdrawal rate (e.g., 4.0 for 4%)
	InflationRate             float64         `json:"inflation_rate"`               // Expected inflation (e.g., 3.0 for 3%)
	HealthcareInflationRate   float64         `json:"healthcare_inflation_rate"`    // Healthcare inflation (e.g., 5.0 for 5%)
	MonthlyHealthcareExpenses float64         `json:"monthly_healthcare_expenses"`
	HealthcareStartYears      int             `json:"healthcare_start_years"`       // Years until healthcare expenses start
	SpendingDeclineRate       float64         `json:"spending_decline_rate"`        // Annual spending decline in retirement
	InvestmentReturn          float64         `json:"investment_return"`            // Expected return (e.g., 6.0 for 6%)
	ProjectionYears           int             `json:"years"`                        // Years to project

	// Budget settings
	Budgets             map[string]CategoryBudget `json:"budgets"`
	OverallMonthlyBudget float64                  `json:"overall_monthly_budget"`
	SavingsGoals        []SavingsGoal             `json:"savings_goals"`

	// UI preferences
	DefaultDateRange string   `json:"default_date_range"` // ytd, 3m, 6m, 12m, all
	EnabledFiles     []string `json:"enabled_files"`      // List of enabled CSV files
}

// CategoryBudget defines a budget for a spending category
type CategoryBudget struct {
	Category       string  `json:"category"`
	Limit          float64 `json:"limit"`           // Monthly limit
	AlertThreshold float64 `json:"alert_threshold"` // e.g., 0.8 for 80% warning
}

// SavingsGoal represents a financial goal
type SavingsGoal struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Target   float64  `json:"target"`
	Current  float64  `json:"current"`
	Deadline *string  `json:"deadline,omitempty"` // "2025-12-31" format
	Priority int      `json:"priority"`
}

// DefaultUserSettings returns settings with sensible defaults
func DefaultUserSettings() *UserSettings {
	return &UserSettings{
		CurrentSavings:            500000,
		MonthlyExpenses:           5000,
		IncomeSources:             []IncomeSource{},
		ExpenseSources:            []ExpenseSource{},
		WithdrawalRate:            4.0,
		InflationRate:             3.0,
		HealthcareInflationRate:   5.0,
		MonthlyHealthcareExpenses: 500,
		HealthcareStartYears:      0,
		SpendingDeclineRate:       1.0,
		InvestmentReturn:          6.0,
		ProjectionYears:           30,
		Budgets:                   make(map[string]CategoryBudget),
		OverallMonthlyBudget:      0,
		SavingsGoals:              []SavingsGoal{},
		DefaultDateRange:          "ytd",
		EnabledFiles:              []string{},
	}
}

// FileInfo represents metadata about an uploaded CSV file
type FileInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	Enabled      bool   `json:"enabled"`
	Transactions int    `json:"transactions"`
	MinDate      string `json:"min_date"`
	MaxDate      string `json:"max_date"`
}
