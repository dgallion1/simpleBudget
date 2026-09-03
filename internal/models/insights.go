package models

import "time"

// RecurringPayment represents a detected recurring expense or subscription
type RecurringPayment struct {
	Description      string        `json:"description"`
	Amount           float64       `json:"amount"`
	Frequency        string        `json:"frequency"` // "weekly", "monthly", "yearly"
	LastDate         time.Time     `json:"last_date"`
	NextExpected     time.Time     `json:"next_expected"`
	AnnualCost       float64       `json:"annual_cost"`
	Occurrences      int           `json:"occurrences"`
	Confidence       float64       `json:"confidence"` // 0.0-1.0
	Transactions     []Transaction `json:"transactions,omitempty"`
	MajorExpenseName string        `json:"major_expense_name,omitempty"` // Filled by AnnotateRecurringPayments
}

// CategoryTrend represents month-over-month spending changes in a category
type CategoryTrend struct {
	Category       string     `json:"category"`
	CurrentAmount  float64    `json:"current_amount"`
	PreviousAmount float64    `json:"previous_amount"`
	ChangePercent  float64    `json:"change_percent"`
	ChangeAmount   float64    `json:"change_amount"`
	Direction      string     `json:"direction"` // "up", "down", "stable"
	Change         ChangeCell `json:"change"`    // single rendering contract for the Change cell -- see ChangeCell
}

// ChangeCell is the single rendering contract for a period-over-period
// Change figure (U5 contract v3, SPEC §2d rule 2). It is the ONE source
// for ChangePercent/ChangeAmount/Direction above -- CategoryTrend's
// producer sets ChangeAmount=Change.Amount, ChangePercent=Change.Percent,
// Direction=Change.Direction FROM this cell (never the reverse), so the
// MCP tool, the arrow/color, and the Change cell text all agree by
// construction. Templates branch on Change.Kind only, never re-derive a
// percent/dollar decision from a raw number. See
// internal/services/insights.ChangeDisplay for how this is computed.
type ChangeCell struct {
	// Kind is one of "new", "none", "dollar", "percent".
	Kind string `json:"kind"`
	// Text is the literal cell text for the non-numeric kinds: "new" when
	// Kind == "new", "—" (em dash, "—") when Kind == "none". Empty for
	// "dollar"/"percent" -- those render Amount/Percent through formatMoney
	// / printf in the template, not from this field.
	Text string `json:"text,omitempty"`
	// Amount is the signed change (current - previous, both already
	// rounded to cents) -- ALWAYS populated, regardless of Kind. Rendered
	// via formatMoney when Kind == "dollar".
	Amount float64 `json:"amount"`
	// Percent is the signed percent derived from the rounded pair --
	// ALWAYS populated, regardless of Kind. Rendered with one decimal when
	// Kind == "percent".
	Percent float64 `json:"percent"`
	// Direction is "up"/"down"/"stable", derived from Percent with the
	// existing +-5 band ("none" rows are always "stable"). This is the
	// SAME value the row's own Direction field carries.
	Direction string `json:"direction"`
}

// Change kind values for ChangeCell.Kind.
const (
	ChangeKindNew     = "new"
	ChangeKindNone    = "none"
	ChangeKindDollar  = "dollar"
	ChangeKindPercent = "percent"
)

// IncomePattern represents detected income sources and their regularity
type IncomePattern struct {
	Description string    `json:"description"`
	AvgAmount   float64   `json:"avg_amount"`
	Frequency   string    `json:"frequency"` // "weekly", "biweekly", "monthly", "irregular"
	IsRegular   bool      `json:"is_regular"`
	Occurrences int       `json:"occurrences"`
	TotalAmount float64   `json:"total_amount"`
	LastDate    time.Time `json:"last_date"` // most recent occurrence; tells ended income from ongoing
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
	RecurringPayments    []RecurringPayment `json:"recurring_payments"`
	Subscriptions        []RecurringPayment `json:"subscriptions"`
	CategoryTrends       []CategoryTrend    `json:"category_trends"`
	IncomePatterns       []IncomePattern    `json:"income_patterns"`
	Velocity             *SpendingVelocity  `json:"velocity"`
	TotalRecurring       float64            `json:"total_recurring"`       // Annual recurring cost
	MonthlyRecurring     float64            `json:"monthly_recurring"`     // Monthly recurring cost
	MonthlySubscriptions float64            `json:"monthly_subscriptions"` // Monthly subscription cost
	RegularIncomeTotal   float64            `json:"regular_income_total"`  // Total from regular income
}
