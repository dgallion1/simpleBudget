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

	// Trends (for sparklines) - monthly values. ExpensesTrend is SIGNED
	// (CB2): an ordinary month is positive (net spend); a REFUND-DOMINANT
	// month -- one whose outflow-typed rows net positive -- is NEGATIVE (a
	// credit), not math.Abs'd positive. SavingsTrend derives as
	// IncomeTrend-ExpensesTrend with no extra handling: for a
	// refund-dominant month this correctly ADDS the net refund to income
	// (savings that month = income + refund), since ExpensesTrend is
	// already negative there.
	IncomeTrend   []float64 `json:"income_trend"`
	ExpensesTrend []float64 `json:"expenses_trend"` // signed net; refund-dominant month is negative (a credit)
	SavingsTrend  []float64 `json:"savings_trend"`  // IncomeTrend-ExpensesTrend; refund-dominant month = income + refund
	TrendLabels   []string  `json:"trend_labels"`   // Month labels

	// Budget tracking (compares living-expense outflows to the active
	// what-if MonthlyLivingExpenses target). "Living" excludes the
	// Health Insurance category, which has its own KPI below — counting
	// premiums in both would double-bill the user. When HasBudgetTarget
	// is false, BudgetTarget is 0 and PerMonthDelta/CumulativeDelta
	// degenerate to ActualMonthly and LivingExpensesTotal respectively
	// — consumers must gate on HasBudgetTarget before treating those
	// fields as variance.
	MonthsInRange       float64   `json:"months_in_range"`       // average-calendar-month count for the date range
	LivingExpensesTotal float64   `json:"living_expenses_total"` // TotalExpenses - healthcare premium total over the range
	ActualMonthly       float64   `json:"actual_monthly"`        // LivingExpensesTotal / MonthsInRange
	BudgetTarget        float64   `json:"budget_target"`         // from what-if MonthlyLivingExpenses (phase-adjusted)
	PerMonthDelta       float64   `json:"per_month_delta"`       // ActualMonthly - BudgetTarget; positive = over
	CumulativeDelta     float64   `json:"cumulative_delta"`      // LivingExpensesTotal - BudgetTarget*MonthsInRange; positive = over
	HasBudgetTarget     bool      `json:"has_budget_target"`     // BudgetTarget > 0
	LivingExpensesTrend []float64 `json:"living_expenses_trend"` // signed net monthly living (non-healthcare) outflows aligned with TrendLabels; refund-dominant month is negative (a credit)

	// Healthcare tracking (compares "Health Insurance" category outflows
	// to the active what-if HealthcarePersons premium total at month 0).
	// Mirrors the BudgetTarget fields above, EXCEPT the divisor/multiplier
	// is coverageMonths (MonthsInRange clipped to
	// [HealthcareCoverageStart, rangeEnd] via metrics.
	// ClippedHealthcareMonths), not MonthsInRange itself -- see HC1. When
	// HasHealthcareTarget is false (HealthcareTarget==0, or coverageMonths
	// is 0 because coverage hasn't started or never existed), the deltas
	// degenerate the same way and HealthcareActual is 0 rather than
	// dividing by zero.
	HealthcareActual          float64   `json:"healthcare_actual"`           // sum of "Health Insurance" outflows / coverageMonths (0 when coverageMonths is 0)
	HealthcareTotal           float64   `json:"healthcare_total"`            // sum of "Health Insurance" outflows over the range
	HealthcareTarget          float64   `json:"healthcare_target"`           // from what-if GetTotalHealthcareCost(0)
	HealthcarePerMonthDelta   float64   `json:"healthcare_per_month_delta"`  // HealthcareActual - HealthcareTarget; positive = over
	HealthcareCumulativeDelta float64   `json:"healthcare_cumulative_delta"` // HealthcareTotal - HealthcareTarget*coverageMonths; positive = over
	HasHealthcareTarget       bool      `json:"has_healthcare_target"`       // HealthcareTarget > 0 AND coverageMonths > 0
	HealthcareTrend           []float64 `json:"healthcare_trend"`            // signed net monthly "Health Insurance" actuals (last 6 months, aligned with TrendLabels); refund-dominant month is negative (a credit)

	// HealthcareCoverageStart/HealthcareHasCoverage are the coverageStart/
	// hasCoverage Calculate received (from metrics.HealthcareCoverageStart
	// over the FULL unfiltered transaction set), carried through so
	// templates can render provenance without doing their own arithmetic.
	// HealthcareCoverageStartInRange is true only when HealthcareHasCoverage
	// is true AND HealthcareCoverageStart falls inside [rangeStart,
	// rangeEnd] as passed to Calculate -- the gate the Monthly Healthcare
	// card's "since <date>" note uses.
	HealthcareCoverageStart        time.Time `json:"healthcare_coverage_start"`
	HealthcareHasCoverage          bool      `json:"healthcare_has_coverage"`
	HealthcareCoverageStartInRange bool      `json:"healthcare_coverage_start_in_range"`

	// Combined plan variance — Living + Healthcare against their summed
	// targets. Drives the "Budget" KPI card so the user sees a single
	// "am I net over my whole plan?" number that nets a category being
	// under against another being over.
	CombinedTarget        float64 `json:"combined_target"`          // BudgetTarget + HealthcareTarget
	CombinedActualMonthly float64 `json:"combined_actual_monthly"`  // ActualMonthly + HealthcareActual
	CombinedPerMonthDelta float64 `json:"combined_per_month_delta"` // CombinedActualMonthly - CombinedTarget; positive = over
	// CombinedCumulativeDelta = LivingCumulativeDelta + HealthcareCumulativeDelta
	// = (LivingExpensesTotal - BudgetTarget*MonthsInRange) +
	// (HealthcareTotal - HealthcareTarget*coverageMonths), where coverageMonths
	// is MonthsInRange clipped to actual healthcare coverage
	// (metrics.ClippedHealthcareMonths) -- NOT MonthsInRange itself. The two
	// terms accrue over different month counts (HC1: healthcare prorated from
	// actual coverage start).
	CombinedCumulativeDelta float64 `json:"combined_cumulative_delta"`
	HasCombinedTarget       bool    `json:"has_combined_target"` // CombinedTarget > 0

	// Cumulative target totals over the date range. Surfaced on the Budget
	// card so the user can read off "Living spent X of Y" and "Health spent
	// X of Y" and see exactly how the headline cumulative variance composes.
	LivingTargetTotal float64 `json:"living_target_total"` // BudgetTarget * MonthsInRange
	// HealthcareTargetTotal = HealthcareTarget * coverageMonths, where
	// coverageMonths is MonthsInRange clipped to actual healthcare coverage
	// (metrics.ClippedHealthcareMonths) -- NOT MonthsInRange itself, unlike
	// LivingTargetTotal above. 0 when coverage never overlaps the range.
	HealthcareTargetTotal float64 `json:"healthcare_target_total"`

	// Running cumulative balance for combined Living + Healthcare against
	// CombinedTarget, walked one point per CALENDAR month (not transaction
	// month — a different basis than TrendLabels/LivingExpensesTrend/
	// HealthcareTrend above) over [rangeStart, rangeEnd] as passed to
	// Calculate. Each point adds that calendar month's pro-rated target
	// accrual (CombinedTarget * the fraction of MonthsInRange the month's
	// in-range days represent) minus that month's actual outflow spend,
	// SIGNED (CB1): spend is the negated net of the month's non-excluded
	// outflow bucket, not its absolute value. An ordinary month nets
	// outflow-negative so this is positive spend, same as before; a
	// REFUND-DOMINANT month — one whose outflow-typed rows net POSITIVE,
	// e.g. a cruise refund exceeding the month's spending — therefore
	// enters the walk as a CREDIT (the balance rises by accrual PLUS the
	// net refund), never charged as spend.
	// Positive = ahead of budget (saved), negative = behind (overspent) —
	// opposite sign convention from CombinedCumulativeDelta, which uses
	// actual−target. Capped to the LAST 6 walked points for display; the
	// running totals those points carry retain any dropped earlier months'
	// carry-in.
	//
	// Precondition: Calculate's caller must pass a TransactionSet already
	// filtered to [rangeStart, rangeEnd] — otherwise per-month spend will
	// not sum to TotalExpenses and the invariant below breaks. All current
	// callers do this (see metrics.Calculate's doc).
	//
	// Invariant: the last element equals -CombinedCumulativeDelta, up to
	// float64 summation noise (not month-rounding slack) — the per-month
	// accruals partition the range's days exactly, and the signed per-month
	// spends (a refund-dominant month contributes a negative spend, i.e. a
	// credit) partition TotalExpenses exactly under the precondition above
	// AND the further precondition that the RANGE as a whole still nets
	// outflow-negative — TotalExpenses is math.Abs of the whole range's net
	// (unchanged by CB1), so a wholly refund-dominant RANGE (the range's own
	// outflows net positive) is out of scope: the invariant is not
	// guaranteed to hold in that case.
	// Populated only when HasCombinedTarget; nil otherwise.
	CombinedCumulativeBalance []float64 `json:"combined_cumulative_balance"`

	// PlanExcludedTotal/PlanExcludedCount (SY4) are DISPLAY-ONLY annotation
	// data: the flagged group's SIGNED NET spend and transaction count --
	// spend the what-if plan sync already excludes from its living-expense
	// average because it models it separately (an ExpenseSource, e.g. a car
	// loan flagged models.MajorExpense.ExcludeFromPlanSync). Ruling
	// SY-2026-08-30d (attempt 3): these fields are NEVER arithmetically
	// subtracted from LivingExpensesTotal/ActualMonthly/CumulativeDelta/
	// LivingExpensesTrend above -- those are computed via SET EXCLUSION
	// (metrics.LivingOutflows) before the ordinary |sum| arithmetic runs, so
	// the exclusion is already reflected in them by construction. This pair
	// exists purely so callers can annotate ("$X modeled elsewhere, N
	// transactions") without recomputing it. PlanExcludedTotal is POSITIVE
	// when the flagged group is a net SPEND and NEGATIVE when it is a net
	// REFUND (refunds exceeding payments) -- the same signed convention
	// whatif/sync.go's ExcludedGroups.Total uses (ruling SY-2026-08-30a),
	// never math.Abs. Zero when planExclusions was nil/empty.
	PlanExcludedTotal float64 `json:"plan_excluded_total"`
	PlanExcludedCount int     `json:"plan_excluded_count"`
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
	Type   string      `json:"type"`           // bar, pie, line, scatter
	X      interface{} `json:"x"`              // x-axis values
	Y      interface{} `json:"y"`              // y-axis values
	Labels []string    `json:"labels"`         // for pie charts
	Values []float64   `json:"values"`         // for pie charts
	Name   string      `json:"name"`           // series name
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
