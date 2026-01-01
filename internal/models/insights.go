package models

import "time"

// RecurringPayment represents a detected recurring expense or subscription
type RecurringPayment struct {
	Description  string        `json:"description"`
	Amount       float64       `json:"amount"`
	Frequency    string        `json:"frequency"` // "weekly", "monthly", "yearly"
	LastDate     time.Time     `json:"last_date"`
	NextExpected time.Time     `json:"next_expected"`
	AnnualCost   float64       `json:"annual_cost"`
	Occurrences  int           `json:"occurrences"`
	Confidence   float64       `json:"confidence"` // 0.0-1.0
	Transactions []Transaction `json:"transactions,omitempty"`
}

// CategoryTrend represents month-over-month spending changes in a category
type CategoryTrend struct {
	Category       string  `json:"category"`
	CurrentAmount  float64 `json:"current_amount"`
	PreviousAmount float64 `json:"previous_amount"`
	ChangePercent  float64 `json:"change_percent"`
	ChangeAmount   float64 `json:"change_amount"`
	Direction      string  `json:"direction"` // "up", "down", "stable"
}

// IncomePattern represents detected income sources and their regularity
type IncomePattern struct {
	Description string  `json:"description"`
	AvgAmount   float64 `json:"avg_amount"`
	Frequency   string  `json:"frequency"` // "weekly", "biweekly", "monthly", "irregular"
	IsRegular   bool    `json:"is_regular"`
	Occurrences int     `json:"occurrences"`
	TotalAmount float64 `json:"total_amount"`
}

// SpendingVelocity tracks the burn rate and projections
type SpendingVelocity struct {
	DailyAverage    float64 `json:"daily_average"`
	HistoricalDaily float64 `json:"historical_daily"`
	MonthProjection float64 `json:"month_projection"`
	DaysRemaining   int     `json:"days_remaining"`
	BurnRateChange  float64 `json:"burn_rate_change"` // % vs historical
}

// InsightsData contains all insight metrics for the page
type InsightsData struct {
	RecurringPayments  []RecurringPayment `json:"recurring_payments"`
	CategoryTrends     []CategoryTrend    `json:"category_trends"`
	IncomePatterns     []IncomePattern    `json:"income_patterns"`
	Velocity           *SpendingVelocity  `json:"velocity"`
	TotalRecurring     float64            `json:"total_recurring"`      // Annual recurring cost
	MonthlyRecurring   float64            `json:"monthly_recurring"`    // Monthly recurring cost
	RegularIncomeTotal float64            `json:"regular_income_total"` // Total from regular income
}
