package models

import "time"

// DashboardMetrics contains the main KPI metrics for the dashboard
type DashboardMetrics struct {
	TotalIncome      float64   `json:"total_income"`
	TotalExpenses    float64   `json:"total_expenses"`
	NetSavings       float64   `json:"net_savings"`
	SavingsRate      float64   `json:"savings_rate"`
	TransactionCount int       `json:"transaction_count"`
	StartDate        time.Time `json:"start_date"`
	EndDate          time.Time `json:"end_date"`

	// Trends (for sparklines) - monthly values
	IncomeTrend   []float64 `json:"income_trend"`
	ExpensesTrend []float64 `json:"expenses_trend"`
	SavingsTrend  []float64 `json:"savings_trend"`
	TrendLabels   []string  `json:"trend_labels"` // Month labels
}

// PeriodComparison holds metrics for two periods for comparison
type PeriodComparison struct {
	Current    *DashboardMetrics `json:"current"`
	Previous   *DashboardMetrics `json:"previous"`
	HasData    bool              `json:"has_data"`

	// Percentage changes
	IncomeChange      float64 `json:"income_change_pct"`
	ExpensesChange    float64 `json:"expenses_change_pct"`
	SavingsChange     float64 `json:"savings_change_pct"`
	SavingsRateChange float64 `json:"savings_rate_change_pp"` // percentage points
}

// SecondaryMetrics contains additional dashboard metrics
type SecondaryMetrics struct {
	AvgDailySpending    float64 `json:"avg_daily_spending"`
	AvgMonthlySpending  float64 `json:"avg_monthly_spending"`
	AvgMonthlyIncome    float64 `json:"avg_monthly_income"`
	RecurringCosts      float64 `json:"recurring_costs"`
	UnusualTransactions int     `json:"unusual_transactions"`
	LargestExpense      float64 `json:"largest_expense"`
	LargestIncome       float64 `json:"largest_income"`
}

// SpendingAlert represents a notification about spending patterns
type SpendingAlert struct {
	Type         string        `json:"type"`     // unusual_day, budget_exceeded, budget_warning, large_transaction
	Severity     string        `json:"severity"` // error, warning, info, success
	Title        string        `json:"title"`
	Message      string        `json:"message"`
	Detail       string        `json:"detail,omitempty"`
	Date         *time.Time    `json:"date,omitempty"`
	Amount       float64       `json:"amount,omitempty"`
	Transactions []Transaction `json:"transactions,omitempty"` // Transactions that triggered this alert
}

// ChartData represents data for a Plotly chart
type ChartData struct {
	Type   string      `json:"type"`   // bar, pie, line, scatter
	X      interface{} `json:"x"`      // x-axis values
	Y      interface{} `json:"y"`      // y-axis values
	Labels []string    `json:"labels"` // for pie charts
	Values []float64   `json:"values"` // for pie charts
	Name   string      `json:"name"`   // series name
	Mode   string      `json:"mode,omitempty"` // for scatter: lines, markers, lines+markers
}

// ChartResponse wraps chart data with layout options
type ChartResponse struct {
	Data   []ChartData `json:"data"`
	Layout ChartLayout `json:"layout,omitempty"`
}

// ChartLayout defines Plotly layout options
type ChartLayout struct {
	Title      string `json:"title,omitempty"`
	XAxisTitle string `json:"xaxis_title,omitempty"`
	YAxisTitle string `json:"yaxis_title,omitempty"`
	BarMode    string `json:"barmode,omitempty"` // group, stack
	ShowLegend bool   `json:"showlegend,omitempty"`
}

// CategorySummary represents spending in a category
type CategorySummary struct {
	Category   string  `json:"category"`
	Amount     float64 `json:"amount"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// MonthlySummary represents a month's financial summary
type MonthlySummary struct {
	Month       string  `json:"month"`
	Income      float64 `json:"income"`
	Expenses    float64 `json:"expenses"`
	NetSavings  float64 `json:"net_savings"`
	SavingsRate float64 `json:"savings_rate"`
}
