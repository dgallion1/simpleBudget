package spend

import (
	"context"
	"fmt"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/insights"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// trendsInput is get_trends' parameters. Both are optional; when either is
// omitted the tool falls back to the last full calendar month present in
// the ledger (see lastFullMonth).
type trendsInput struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"inclusive window start date, YYYY-MM-DD; default: the start of the last full calendar month in the ledger"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"inclusive window end date, YYYY-MM-DD; default: the end of the last full calendar month in the ledger"`
}

// categoryTrendRow mirrors models.CategoryTrend -- it is also the shape used
// for major_expense_trends, since MajorExpenseTrends returns the same type
// (major-expense names standing in for categories).
type categoryTrendRow struct {
	Category       string  `json:"category"`
	CurrentAmount  float64 `json:"current_amount"`
	PreviousAmount float64 `json:"previous_amount"`
	ChangePercent  float64 `json:"change_percent"`
	ChangeAmount   float64 `json:"change_amount"`
	Direction      string  `json:"direction"`
}

// incomePatternRow mirrors models.IncomePattern. Description is the
// lower-cased, trimmed transaction description IncomePatterns grouped by --
// it is not necessarily how any single transaction's description reads.
type incomePatternRow struct {
	Description string  `json:"description"`
	AvgAmount   float64 `json:"avg_amount"`
	Frequency   string  `json:"frequency"`
	IsRegular   bool    `json:"is_regular"`
	Occurrences int     `json:"occurrences"`
	TotalAmount float64 `json:"total_amount"`
}

// velocityRow is a window-scoped spending-pace summary derived from
// models.SpendingVelocity. It deliberately omits MonthProjection and
// DaysRemaining: both are computed by insights.SpendingVelocity against the
// real-world CURRENT calendar month (time.Now()), which has no relationship
// to this tool's requested window -- the default window is the last FULL
// past month in the ledger, which by construction is never the current
// month, so a "days remaining this month" / "projected month total" pair
// would silently describe a different month than the one being reported on,
// with no way for a caller to detect it. daily_average and burn_rate_change
// stay, but see DailyAverage's own doc below: it is NOT simply window spend
// over the window's calendar length, and is not as "honest about the
// SELECTED window" as its name suggests.
type velocityRow struct {
	// DailyAverage is insights.SpendingVelocity's DailyAverage, UNCHANGED --
	// see Finding 3 in the Phase 2 review. It is NOT window spend divided by
	// the window's calendar length (end - start); it is window spend
	// divided by the number of days BETWEEN THE EARLIEST AND LATEST
	// TRANSACTION PRESENT IN THE WINDOW (which may be an income row, not
	// just an outflow), a span that is shorter than the requested window
	// whenever the window has gaps -- e.g. a 31-day window whose
	// transactions cluster on the 10th-12th divides by 3, not 31, inflating
	// this figure roughly tenfold. Do not multiply it by the window's
	// length to project a total; use MonthProjection-style math elsewhere
	// if that's needed. When the window has zero outflows, this is 0.
	DailyAverage float64 `json:"daily_average"`
	// HistoricalDaily is the same computation as DailyAverage (see its doc
	// above) but over the WHOLE active ledger (not the selected window) --
	// the baseline burn_rate_change compares the window against. It shares
	// DailyAverage's same span-vs-length caveat over the ledger's own
	// transaction dates. When the selected window has zero outflows,
	// SpendingVelocity returns a fully zeroed struct, so this reads as 0
	// too -- even when the ledger has years of spending history; that zero
	// means "the window was empty," not "there is no history."
	HistoricalDaily float64 `json:"historical_daily"`
	// BurnRateChange is the percent difference between DailyAverage and
	// HistoricalDaily: (window - history) / |history| * 100 when history is
	// non-zero, so the sign always tracks the sign of the change even when
	// HistoricalDaily is negative (a refund-dominant ledger); when history
	// is exactly zero it is +100 / -100 / 0 by the sign of the change
	// (ruling CB8-2026-09-03a). Positive means the selected window is
	// spending faster than the ledger's own history.
	// It inherits DailyAverage's inflation risk, since both inputs share
	// the same span-vs-length caveat.
	BurnRateChange float64 `json:"burn_rate_change"`
}

type trendsOutput struct {
	Start              string             `json:"start"`
	End                string             `json:"end"`
	PreviousStart      string             `json:"previous_start"`
	PreviousEnd        string             `json:"previous_end"`
	CategoryTrends     []categoryTrendRow `json:"category_trends"`
	MajorExpenseTrends []categoryTrendRow `json:"major_expense_trends,omitempty"`
	IncomePatterns     []incomePatternRow `json:"income_patterns"`
	Velocity           velocityRow        `json:"velocity"`
}

// categoryTrendRows converts CategoryTrends'/MajorExpenseTrends' shared
// []models.CategoryTrend into rows, rounding dollar and percent figures.
func categoryTrendRows(trends []models.CategoryTrend) []categoryTrendRow {
	rows := make([]categoryTrendRow, 0, len(trends))
	for _, tr := range trends {
		rows = append(rows, categoryTrendRow{
			Category:       tr.Category,
			CurrentAmount:  round2(tr.CurrentAmount),
			PreviousAmount: round2(tr.PreviousAmount),
			ChangePercent:  round2(tr.ChangePercent),
			ChangeAmount:   round2(tr.ChangeAmount),
			Direction:      tr.Direction,
		})
	}
	return rows
}

// incomePatternRows converts IncomePatterns' []models.IncomePattern into
// rows, rounding dollar figures.
func incomePatternRows(patterns []models.IncomePattern) []incomePatternRow {
	rows := make([]incomePatternRow, 0, len(patterns))
	for _, p := range patterns {
		rows = append(rows, incomePatternRow{
			Description: p.Description,
			AvgAmount:   round2(p.AvgAmount),
			Frequency:   p.Frequency,
			IsRegular:   p.IsRegular,
			Occurrences: p.Occurrences,
			TotalAmount: round2(p.TotalAmount),
		})
	}
	return rows
}

// velocityRowFrom converts SpendingVelocity's *models.SpendingVelocity into
// a row, rounding dollar figures and dropping MonthProjection/DaysRemaining
// (see velocityRow's doc comment for why). v is never nil (SpendingVelocity
// always returns a pointer, possibly to a zero value).
func velocityRowFrom(v *models.SpendingVelocity) velocityRow {
	return velocityRow{
		DailyAverage:    round2(v.DailyAverage),
		HistoricalDaily: round2(v.HistoricalDaily),
		BurnRateChange:  round2(v.BurnRateChange),
	}
}

// daysInMonth returns the number of days in the given calendar month.
func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// lastFullMonth returns the [start, end] bounds of the last full calendar
// month present as of maxDate: maxDate's own month, unless maxDate falls
// short of that month's last day, in which case maxDate's month is still in
// progress in the ledger and the PRECEDING calendar month is used instead.
func lastFullMonth(maxDate time.Time) (start, end time.Time) {
	y, m := maxDate.Year(), maxDate.Month()
	if maxDate.Day() < daysInMonth(y, m) {
		m--
		if m == 0 {
			m = 12
			y--
		}
	}
	start = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(y, m, daysInMonth(y, m), 0, 0, 0, 0, time.UTC)
	return start, end
}

// majorExpenseTrendRows computes major_expense_trends via deps.MajorExpenses,
// returning nil (the block is omitted from output) when the dependency is
// unwired or its definitions fail to load. A pins-load failure is tolerated
// -- trends still compute from definitions alone (nil pins), matching
// get_recurring's annotateMajorExpenses and the handler's own
// annotateRecurringWithMajorExpense, both of which likewise proceed on a
// pins-load failure as long as major-expense definitions loaded.
func (d Deps) majorExpenseTrendRows(ts *models.TransactionSet, start, end time.Time) []categoryTrendRow {
	if d.MajorExpenses == nil {
		return nil
	}
	defs, err := d.MajorExpenses.LoadMajorExpenses()
	if err != nil || len(defs) == 0 {
		return nil
	}
	pins, _ := d.MajorExpenses.LoadTransactionPins()
	return categoryTrendRows(insights.MajorExpenseTrends(ts, defs, pins, start, end))
}

// registerTrends adds get_trends to s.
func registerTrends(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_trends",
		Description: "Category spending trends, income patterns, and spending velocity (burn rate) over an " +
			"optional date window (default: the last full calendar month present in the ledger; start_date and " +
			"end_date, whether given or defaulted, must resolve to start <= end or the call is a tool error). " +
			"category_trends and major_expense_trends compare the selected window's spending against the " +
			"IMMEDIATELY PRECEDING window of equal length (echoed back as previous_start/previous_end) -- " +
			"NOT a long-run average -- so a category can show as \"up\" or \"down\" relative to just the one " +
			"prior period, which may itself have been unusual. Within each row, category_trends' current_amount " +
			"and previous_amount are POSITIVE dollar figures; major_expense_trends' current_amount and " +
			"previous_amount are SIGNED instead -- a refund-dominant major-expense period (refunds outweigh " +
			"purchases) nets NEGATIVE, meaning a credit, not a debt. change_amount (current_amount - " +
			"previous_amount) and change_percent are SIGNED for both -- negative means spending FELL versus " +
			"the prior window, positive means it rose; do not read either as a magnitude. category_trends, " +
			"major_expense_trends, and income_patterns are each SILENTLY CAPPED and will not list every category/expense/income source " +
			"when the household has more than the cap: category_trends and major_expense_trends each keep only " +
			"the 10 rows with the largest |change_amount| (biggest movers, not biggest spenders -- a large, " +
			"stable category can be dropped in favor of a small one that moved a lot), and income_patterns " +
			"keeps only the 10 rows with the largest total_amount. A household with more than 10 categories, " +
			"major expenses, or income sources will have some silently missing from these lists -- there is no " +
			"count/total field here to detect this against, unlike search_transactions' total or " +
			"get_recurring's 20-row cap. major_expense_trends groups outflows by the " +
			"user's declared major expenses (via pin or keyword/amount match) instead of raw category, " +
			"dropping unmatched transactions; it is OMITTED from the response entirely (not present, not an " +
			"empty list) when no major-expense source is configured, its definitions fail to load, OR no " +
			"transaction in either window matched a declared major expense -- omission is not evidence the " +
			"user has none declared. income_patterns detects recurring income sources (paycheck, freelance, " +
			"etc.) over the WHOLE ledger, not just the selected window -- a source needs at least 2 " +
			"occurrences to appear at all, so a single-window slice would chronically miss regular income " +
			"whose next occurrence falls outside it. velocity is a PACE summary, not a forecast: " +
			"daily_average is NOT simply window spend divided by the window's calendar length -- it is spend " +
			"within the selected window divided by the number of days BETWEEN THE WINDOW'S OWN EARLIEST AND " +
			"LATEST TRANSACTION (which may be income rows, not just outflows), so a window whose transactions " +
			"cluster in a few days -- leaving gaps elsewhere in the window -- reports a daily_average inflated " +
			"well above the window's true per-calendar-day pace; do not multiply it by the window's length to " +
			"project a total. historical_daily is computed the same way but over the WHOLE ledger, independent " +
			"of the window, as a baseline -- and burn_rate_change, being the percent difference between the " +
			"two, inherits daily_average's inflation risk. When the selected window has no outflows at all, the " +
			"entire velocity block reads as zero -- including historical_daily -- even if the ledger has years " +
			"of spending history; a zero historical_daily in that case means \"this window was empty,\" not " +
			"\"this household has no spending history.\" There is no month-remaining projection field: " +
			"the tool's default window is the last " +
			"FULL past month, never the current one, so a projection tied to today's calendar would " +
			"silently describe a different month than the one being reported on. Suppressed transactions " +
			"(rows the user has already marked as a resolved duplicate) are excluded before analysis, " +
			"matching every other spend tool. income_patterns amounts are positive as recorded (income is a " +
			"positive amount in the ledger).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in trendsInput) (res *mcp.CallToolResult, out trendsOutput, err error) {
		defer recoverToError("get_trends", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, trendsOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, trendsOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, trendsOutput{}, err
		}
		// Suppressed rows are near-duplicates the user has already resolved;
		// every other spend tool excludes them before analysis.
		ts = ts.Active()

		from, to := start, end
		if from == nil || to == nil {
			// lastFullMonth needs a real MaxDate to anchor on; an empty
			// ledger (after suppression) has none (ts.MaxDate() is the zero
			// time), and defaulting from it would silently emit a
			// nonsensical window (year 0/1) instead of the true answer,
			// which is "there is nothing to default from". Only the
			// defaulting path needs this guard -- two fully explicit dates
			// are a legitimate (if empty) request even against an empty
			// ledger.
			if ts.MaxDate().IsZero() {
				return nil, trendsOutput{}, fmt.Errorf(
					"cannot default the trends window: the ledger has no transactions (after excluding " +
						"suppressed rows); pass start_date and end_date explicitly")
			}
			defaultStart, defaultEnd := lastFullMonth(ts.MaxDate())
			if from == nil {
				from = &defaultStart
			}
			if to == nil {
				to = &defaultEnd
			}
		}
		if to.Before(*from) {
			return nil, trendsOutput{}, fmt.Errorf(
				"end_date %s is before start_date %s", to.Format("2006-01-02"), from.Format("2006-01-02"))
		}

		// Mirrors CategoryTrends'/MajorExpenseTrends' own internal previous-
		// window math exactly, so the echoed previous_start/previous_end
		// truthfully describe what they compared against.
		duration := to.Sub(*from)
		prevStart := from.Add(-duration - 24*time.Hour)
		prevEnd := from.Add(-24 * time.Hour)

		windowed := ts.FilterByDateRange(*from, *to)

		out = trendsOutput{
			Start:              from.Format("2006-01-02"),
			End:                to.Format("2006-01-02"),
			PreviousStart:      prevStart.Format("2006-01-02"),
			PreviousEnd:        prevEnd.Format("2006-01-02"),
			CategoryTrends:     categoryTrendRows(insights.CategoryTrends(ts, *from, *to)),
			MajorExpenseTrends: deps.majorExpenseTrendRows(ts, *from, *to),
			IncomePatterns:     incomePatternRows(insights.IncomePatterns(ts)),
			Velocity:           velocityRowFrom(insights.SpendingVelocity(windowed, ts)),
		}
		return nil, out, nil
	})
}
