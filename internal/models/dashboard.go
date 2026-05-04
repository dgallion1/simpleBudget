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

	// Budget tracking (compares living-expense outflows to the active
	// what-if MonthlyLivingExpenses target). "Living" excludes the
	// Health Insurance category, which has its own KPI below — counting
	// premiums in both would double-bill the user. When HasBudgetTarget
	// is false, BudgetTarget is 0 and PerMonthDelta/CumulativeDelta
	// degenerate to ActualMonthly and LivingExpensesTotal respectively
	// — consumers must gate on HasBudgetTarget before treating those
	// fields as variance.
	MonthsInRange       float64 `json:"months_in_range"`        // average-calendar-month count for the date range
	LivingExpensesTotal float64 `json:"living_expenses_total"`  // TotalExpenses - healthcare premium total over the range
	ActualMonthly       float64 `json:"actual_monthly"`         // LivingExpensesTotal / MonthsInRange
	BudgetTarget        float64 `json:"budget_target"`          // from what-if MonthlyLivingExpenses (phase-adjusted)
	PerMonthDelta       float64 `json:"per_month_delta"`        // ActualMonthly - BudgetTarget; positive = over
	CumulativeDelta     float64 `json:"cumulative_delta"`       // LivingExpensesTotal - BudgetTarget*MonthsInRange; positive = over
	HasBudgetTarget     bool    `json:"has_budget_target"`      // BudgetTarget > 0
	LivingExpensesTrend []float64 `json:"living_expenses_trend"` // monthly living (non-healthcare) outflows aligned with TrendLabels

	// Healthcare tracking (compares "Health Insurance" category outflows
	// to the active what-if HealthcarePersons premium total at month 0).
	// Mirrors the BudgetTarget fields above. When HasHealthcareTarget is
	// false, HealthcareTarget is 0 and the deltas degenerate the same way.
	HealthcareActual          float64   `json:"healthcare_actual"`           // sum of "Health Insurance" outflows / MonthsInRange
	HealthcareTotal           float64   `json:"healthcare_total"`            // sum of "Health Insurance" outflows over the range
	HealthcareTarget          float64   `json:"healthcare_target"`           // from what-if GetTotalHealthcareCost(0)
	HealthcarePerMonthDelta   float64   `json:"healthcare_per_month_delta"`  // HealthcareActual - HealthcareTarget; positive = over
	HealthcareCumulativeDelta float64   `json:"healthcare_cumulative_delta"` // HealthcareTotal - HealthcareTarget*MonthsInRange; positive = over
	HasHealthcareTarget       bool      `json:"has_healthcare_target"`       // HealthcareTarget > 0
	HealthcareTrend           []float64 `json:"healthcare_trend"`            // monthly "Health Insurance" actuals (last 6 months, aligned with TrendLabels)

	// Combined plan variance — Living + Healthcare against their summed
	// targets. Drives the "Budget" KPI card so the user sees a single
	// "am I net over my whole plan?" number that nets a category being
	// under against another being over.
	CombinedTarget          float64 `json:"combined_target"`            // BudgetTarget + HealthcareTarget
	CombinedActualMonthly   float64 `json:"combined_actual_monthly"`    // ActualMonthly + HealthcareActual
	CombinedPerMonthDelta   float64 `json:"combined_per_month_delta"`   // CombinedActualMonthly - CombinedTarget; positive = over
	CombinedCumulativeDelta float64 `json:"combined_cumulative_delta"`  // (LivingExpensesTotal + HealthcareTotal) - CombinedTarget*MonthsInRange
	HasCombinedTarget       bool    `json:"has_combined_target"`        // CombinedTarget > 0

	// Cumulative target totals over the date range (each = monthly
	// target × MonthsInRange). Surfaced on the Budget card so the user
	// can read off "Living spent X of Y" and "Health spent X of Y" and
	// see exactly how the headline cumulative variance composes.
	LivingTargetTotal     float64 `json:"living_target_total"`     // BudgetTarget * MonthsInRange
	HealthcareTargetTotal float64 `json:"healthcare_target_total"` // HealthcareTarget * MonthsInRange
}

// PeriodComparison holds metrics for two periods for comparison
type PeriodComparison struct {
	Current  *DashboardMetrics `json:"current"`
	Previous *DashboardMetrics `json:"previous"`
	HasData  bool              `json:"has_data"`

	// Percentage changes
	IncomeChange      float64 `json:"income_change_pct"`
	ExpensesChange    float64 `json:"expenses_change_pct"`
	SavingsChange     float64 `json:"savings_change_pct"`
	SavingsRateChange float64 `json:"savings_rate_change_pp"` // percentage points

	// Budget tracking deltas (current - previous; signed, in dollars)
	ActualMonthlyChange   float64 `json:"actual_monthly_change"`
	CumulativeDeltaChange float64 `json:"cumulative_delta_change"`
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
