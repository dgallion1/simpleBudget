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

// velocityRow is an explicit selected/prior calendar-day pace summary.
// Unsupported history is omitted, not represented as a measured zero.
type velocityRow struct {
	DailyAverage    float64  `json:"daily_average"`
	HistoricalDaily *float64 `json:"historical_daily,omitempty"`
	BurnRateChange  *float64 `json:"burn_rate_change,omitempty"`
}

type trendsOutput struct {
	Period             models.PeriodContext `json:"period"`
	Start              string               `json:"start"`
	End                string               `json:"end"`
	PreviousStart      string               `json:"previous_start"`
	PreviousEnd        string               `json:"previous_end"`
	CategoryTrends     []categoryTrendRow   `json:"category_trends"`
	MajorExpenseTrends []categoryTrendRow   `json:"major_expense_trends,omitempty"`
	IncomePatterns     []incomePatternRow   `json:"income_patterns"`
	Velocity           velocityRow          `json:"velocity"`
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

// velocityRowFrom keeps explicit history availability from the shared producer.
func velocityRowFrom(v *models.SpendingVelocity) velocityRow {
	row := velocityRow{DailyAverage: round2(v.DailyAverage)}
	if v.Period == nil || v.Period.HistoryAvailable {
		history, change := round2(v.HistoricalDaily), round2(v.BurnRateChange)
		row.HistoricalDaily = &history
		row.BurnRateChange = &change
	}
	return row
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
func (d Deps) majorExpenseTrendRows(ts *models.TransactionSet, period models.PeriodContext) []categoryTrendRow {
	if d.MajorExpenses == nil {
		return nil
	}
	defs, err := d.MajorExpenses.LoadMajorExpenses()
	if err != nil || len(defs) == 0 {
		return nil
	}
	pins, _ := d.MajorExpenses.LoadTransactionPins()
	return categoryTrendRows(insights.MajorExpenseTrendsForPeriod(ts, defs, pins, period))
}

// registerTrends adds get_trends to s.
func registerTrends(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_trends",
		Description: "Category and declared major-expense spending comparisons over inclusive selected dates. " +
			"Default dates are the last full calendar month present in the ledger. Completed calendar months " +
			"compare with the prior calendar month; current month-to-date compares with the prior month through " +
			"the same day, clamped to its final day; other ranges compare with the preceding equal number of " +
			"calendar days. period and previous_start/previous_end expose the actual bounds and any clamp. " +
			"History bounds do not prove import completeness. When period.history_available is false, comparisons " +
			"are unavailable, trend rows are empty, and velocity omits historical_daily and burn_rate_change. " +
			"All comparison amounts are signed net outflow spending: refunds reduce spending. Rows use one " +
			"rounded ChangeCell calculation; negative change means spending fell. Categories and major expenses " +
			"are uncapped, sorted by absolute change then name. Major expenses use pins/keyword matching and " +
			"exclude unmatched transactions; the block is omitted when definitions are unavailable or no rows match. " +
			"Income patterns retain their whole-active-ledger detection and top-10 total-amount cap. " +
			"Velocity divides signed spending by selected/prior calendar lengths, excluding the selected window " +
			"from its prior baseline. Period forecast availability/reason is explicit; a forecast requires current " +
			"month-start through today selection, at least seven elapsed days, and current-month data no more " +
			"than seven calendar days old. It estimates current-month net spending / elapsed days * month days. " +
			"Suppressed transactions are excluded; no live data is modified.",
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

		out = trendsForWindow(deps, ts, *from, *to, time.Now())
		return nil, out, nil
	})
}

// trendsForWindow gives tests and all tool fields one explicitly injected clock.
func trendsForWindow(deps Deps, ts *models.TransactionSet, start, end, now time.Time) trendsOutput {
	period := insights.BuildPeriodContext(ts, start, end, now)
	return trendsOutput{
		Period:             period,
		Start:              period.SelectedStart.Format("2006-01-02"),
		End:                period.SelectedEnd.Format("2006-01-02"),
		PreviousStart:      period.PreviousStart.Format("2006-01-02"),
		PreviousEnd:        period.PreviousEnd.Format("2006-01-02"),
		CategoryTrends:     categoryTrendRows(insights.CategoryTrendsForPeriod(ts, period)),
		MajorExpenseTrends: deps.majorExpenseTrendRows(ts, period),
		IncomePatterns:     incomePatternRows(insights.IncomePatterns(ts.Active())),
		Velocity:           velocityRowFrom(insights.SpendingVelocityForPeriod(ts, period)),
	}
}
