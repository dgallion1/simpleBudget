package spend

import (
	"context"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/internal/services/majorexpenses"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recurringInput is get_recurring's parameters. Both are optional.
type recurringInput struct {
	ReferenceDate     string `json:"reference_date,omitempty" jsonschema:"freshness cutoff, YYYY-MM-DD; a payment is reported only if its most recent occurrence is recent enough for its own interval as of this date -- default: the ledger's latest transaction date"`
	SubscriptionsOnly bool   `json:"subscriptions_only,omitempty" jsonschema:"return only rows where is_subscription is true"`
}

// recurringRow is one detected recurring payment in get_recurring's result.
type recurringRow struct {
	Description      string  `json:"description"`
	Amount           float64 `json:"amount"`
	Frequency        string  `json:"frequency"`
	IntervalDays     float64 `json:"interval_days,omitempty"`
	LastDate         string  `json:"last_date"`
	NextExpected     string  `json:"next_expected,omitempty"`
	Occurrences      int     `json:"occurrences"`
	AnnualCost       float64 `json:"annual_cost"`
	IsSubscription   bool    `json:"is_subscription"`
	MajorExpenseName string  `json:"major_expense_name,omitempty"`
}

type recurringOutput struct {
	Count         int            `json:"count"`
	ReferenceDate string         `json:"reference_date"`
	Payments      []recurringRow `json:"payments"`
}

// annotateMajorExpenses fills MajorExpenseName on each payment via
// majorexpenses.AnnotateRecurringPayments, when deps.MajorExpenses is wired
// and its definitions load succeeds. A pins-load failure is tolerated --
// annotation proceeds from definitions alone (nil pins), matching the
// handler's own annotateRecurringWithMajorExpense, which likewise ignores a
// LoadTransactionPins error as long as major-expense definitions loaded
// (internal/handlers/insights/handlers.go). AnnotateRecurringPayments
// accepts a nil pins map. A missing MajorExpenses dependency or a
// definitions-load failure returns payments unannotated instead of failing
// the call -- the label is a convenience, not the answer.
func (d Deps) annotateMajorExpenses(payments []models.RecurringPayment) []models.RecurringPayment {
	if d.MajorExpenses == nil {
		return payments
	}
	defs, err := d.MajorExpenses.LoadMajorExpenses()
	if err != nil {
		return payments
	}
	pins, _ := d.MajorExpenses.LoadTransactionPins()
	return majorexpenses.AnnotateRecurringPayments(payments, defs, pins)
}

// recurringRows converts detected payments to get_recurring's output rows,
// optionally filtering to subscriptions only. IntervalDays is derived from
// NextExpected - LastDate (the same interval DetectRecurringAt used to
// project NextExpected in the first place) rather than re-deriving it from
// Frequency, since "ongoing" payments have no fixed per-frequency interval.
func recurringRows(payments []models.RecurringPayment, subscriptionsOnly bool) []recurringRow {
	rows := make([]recurringRow, 0, len(payments))
	for _, p := range payments {
		isSub := insights.IsSubscription(p)
		if subscriptionsOnly && !isSub {
			continue
		}
		row := recurringRow{
			Description:      p.Description,
			Amount:           round2(p.Amount),
			Frequency:        p.Frequency,
			LastDate:         p.LastDate.Format("2006-01-02"),
			Occurrences:      p.Occurrences,
			AnnualCost:       round2(p.AnnualCost),
			IsSubscription:   isSub,
			MajorExpenseName: p.MajorExpenseName,
		}
		if !p.NextExpected.IsZero() {
			row.NextExpected = p.NextExpected.Format("2006-01-02")
			row.IntervalDays = round2(p.NextExpected.Sub(p.LastDate).Hours() / 24)
		}
		rows = append(rows, row)
	}
	return rows
}

// registerRecurring adds get_recurring to s.
func registerRecurring(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_recurring",
		Description: "Detect recurring payments (subscriptions, bills, and other repeating charges) by " +
			"clustering outflows into merchant groups and looking for consistent amounts at consistent " +
			"intervals -- weekly, biweekly, monthly, quarterly, yearly, or a variable-amount \"ongoing\" " +
			"relationship (e.g. metered/usage-based billing). Detection runs over the WHOLE transaction " +
			"history UP TO reference_date -- this is a historical truncation, not a recent display window: " +
			"transactions after reference_date are excluded from detection entirely, so occurrence counts, " +
			"averages, and annual_cost are all computed only from what happened on or before that date. A " +
			"past reference_date therefore answers \"what did this look like as of that point in the " +
			"ledger,\" not \"what does it look like now, shown as of an earlier date.\" reference_date does " +
			"a SECOND job on top of that truncation: it is also the freshness cutoff a detected series must " +
			"still be current against -- a series is reported only if its most recent occurrence (within the " +
			"truncated history) is recent enough for its own interval as of that date (a monthly charge " +
			"stays \"active\" for a shorter gap than a yearly one), so a series that stopped well before " +
			"reference_date is correctly omitted as no longer active. reference_date defaults to the " +
			"ledger's latest transaction date, matching the Insights page (i.e. by default nothing is " +
			"truncated). Suppressed transactions (rows the user has already marked as a resolved duplicate) " +
			"are excluded before detection, matching every other spend tool. is_subscription is a HEURISTIC " +
			"over the payment's frequency and its merchant description (retail stores and utility/bill " +
			"keywords are excluded), not a fact about the merchant -- treat it as a hint, not ground truth. " +
			"Set subscriptions_only to return only rows flagged that way -- NOTE this filter is applied " +
			"AFTER detection caps results at the 20 highest-annual_cost series, so if 20+ higher-cost bills " +
			"crowd out lower-cost subscriptions, subscriptions_only can return an incomplete list rather " +
			"than every subscription in the ledger. major_expense_name is populated only when the payment " +
			"matches one of the user's declared major expenses (via pin or keyword/amount match) and is " +
			"omitted otherwise. amount and annual_cost are POSITIVE dollar figures (unlike " +
			"search_transactions, which returns signed amounts). frequency is lower-case as stored " +
			"(\"monthly\", \"yearly\", ...), not title-cased.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recurringInput) (res *mcp.CallToolResult, out recurringOutput, err error) {
		defer recoverToError("get_recurring", &err)

		refDate, err := parseWindowDate("reference_date", in.ReferenceDate)
		if err != nil {
			return nil, recurringOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, recurringOutput{}, err
		}
		// Suppressed rows are near-duplicates the user has already resolved;
		// every other spend tool excludes them before analysis, and a
		// recurring series inflated by an unresolved duplicate would be a
		// wrong answer with a plausible shape.
		ts = ts.Active()

		var refInput time.Time
		if refDate != nil {
			refInput = *refDate
		}
		resolvedRef := insights.ReferenceDate(ts, refInput)

		payments := insights.DetectRecurringAt(ts, resolvedRef)
		payments = deps.annotateMajorExpenses(payments)
		rows := recurringRows(payments, in.SubscriptionsOnly)

		return nil, recurringOutput{
			Count:         len(rows),
			ReferenceDate: resolvedRef.Format("2006-01-02"),
			Payments:      rows,
		}, nil
	})
}
