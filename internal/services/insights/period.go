package insights

import (
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
)

// calendarDate reads a civil date, not an instant. In particular a CSV date
// parsed at UTC midnight must not turn into yesterday in the app's timezone.
// now's Y-M-D is read in its supplied (normally app-local) location.
func calendarDate(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func calendarDays(start, end time.Time) int {
	return int(calendarDate(end).Sub(calendarDate(start))/(24*time.Hour)) + 1
}
func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// TransactionsForPeriod selects active rows by inclusive civil dates, preserving
// date-only bank records across timezone/DST boundaries. It never mutates input.
func TransactionsForPeriod(ts *models.TransactionSet, start, end time.Time) *models.TransactionSet {
	start, end = calendarDate(start), calendarDate(end)
	out := &models.TransactionSet{}
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return out
	}
	for _, txn := range ts.Active().Transactions {
		day := calendarDate(txn.Date)
		if !day.Before(start) && !day.After(end) {
			out.Transactions = append(out.Transactions, txn)
		}
	}
	return out
}

// HasCivilDateCoverage reports whether active first/last evidence dates enclose
// the inclusive civil-date range. This is not proof of import completeness.
func HasCivilDateCoverage(ts *models.TransactionSet, start, end time.Time) bool {
	active := ts.Active()
	start, end = calendarDate(start), calendarDate(end)
	return active.Len() > 0 && !start.IsZero() && !end.IsZero() && !end.Before(start) &&
		!calendarDate(active.MinDate()).After(start) && !calendarDate(active.MaxDate()).Before(end)
}

// BuildPeriodContext is the shared explicit-clock producer. ReferenceDate is
// min(selected end, today); forecasting requires the selection to cover the
// current month from its first day THROUGH TODAY. A historical subrange cannot
// borrow later records to claim a current forecast. Seven elapsed days includes
// today. Staleness is >7 civil days; latest dates never assert import completeness.
func BuildPeriodContext(allData *models.TransactionSet, start, end, now time.Time) models.PeriodContext {
	p := models.PeriodContext{SelectedStart: calendarDate(start), SelectedEnd: calendarDate(end), Today: calendarDate(now),
		HistoryReason: "Not enough history to compare", ForecastReason: "No transaction data available"}
	active := allData.Active()
	p.HasData = active.Len() > 0
	p.LatestTransaction = calendarDate(active.MaxDate())
	if p.HasData && !p.Today.IsZero() {
		p.DataAgeDays = calendarDays(p.LatestTransaction, p.Today) - 1
		p.Stale = p.DataAgeDays > 7
	}
	if p.SelectedStart.IsZero() || p.SelectedEnd.IsZero() || p.Today.IsZero() || p.SelectedEnd.Before(p.SelectedStart) {
		p.ForecastReason = "A valid selected period and reference date are required"
		return p
	}
	p.Valid = true
	p.SelectedDays = calendarDays(p.SelectedStart, p.SelectedEnd)
	p.ReferenceDate = p.SelectedEnd
	if p.ReferenceDate.After(p.Today) {
		p.ReferenceDate = p.Today
	}
	p.ReferenceMonth = monthStart(p.ReferenceDate)
	currentMonth := monthStart(p.Today)
	p.ElapsedDays = p.ReferenceDate.Day()
	p.DaysInMonth = p.ReferenceMonth.AddDate(0, 1, -1).Day()
	switch {
	case p.SelectedStart.Equal(currentMonth) && p.SelectedEnd.Equal(p.Today):
		p.ComparisonKind = "month_to_date"
		p.PreviousStart = currentMonth.AddDate(0, -1, 0)
		last := currentMonth.AddDate(0, 0, -1)
		p.PreviousEnd = p.PreviousStart.AddDate(0, 0, p.SelectedDays-1)
		if p.PreviousEnd.After(last) {
			p.PreviousEnd = last
			p.ComparisonClamped = true
		}
	case p.SelectedStart.Day() == 1 && p.SelectedEnd.Equal(p.SelectedStart.AddDate(0, 1, -1)) && p.SelectedEnd.Before(currentMonth):
		p.ComparisonKind = "calendar_month"
		p.PreviousStart = p.SelectedStart.AddDate(0, -1, 0)
		p.PreviousEnd = p.SelectedStart.AddDate(0, 0, -1)
	default:
		p.ComparisonKind = "equal_days"
		p.PreviousEnd = p.SelectedStart.AddDate(0, 0, -1)
		p.PreviousStart = p.SelectedStart.AddDate(0, 0, -p.SelectedDays)
	}
	p.PreviousDays = calendarDays(p.PreviousStart, p.PreviousEnd)
	p.HistoryAvailable = HasCivilDateCoverage(active, p.PreviousStart, p.PreviousEnd)
	if p.HistoryAvailable {
		p.HistoryReason = ""
	}
	switch {
	case !p.HasData:
	case p.SelectedStart.After(currentMonth) || p.SelectedEnd.Before(p.Today):
		p.ForecastReason = "Selected range does not include the current month from its start through today"
	case p.LatestTransaction.Before(currentMonth):
		p.ForecastReason = "No current-month transaction data"
	case p.LatestTransaction.After(p.Today):
		p.ForecastReason = "Latest transaction is after today"
	case p.Stale:
		p.ForecastReason = "Latest transaction is more than seven calendar days old"
	case p.ElapsedDays < 7:
		p.ForecastReason = "At least seven elapsed calendar days are needed"
	default:
		spent := metrics.SignedNet(TransactionsForPeriod(active, currentMonth, p.Today).FilterByType(models.Outflow))
		p.ForecastAmount = models.RoundToCents(spent / float64(p.ElapsedDays) * float64(p.DaysInMonth))
		p.ForecastAvailable = true
		p.ForecastReason = ""
	}
	return p
}

// SpendingVelocityForPeriod uses selected/prior inclusive calendar lengths,
// never transaction spans or a baseline containing the selected period.
// Period.HistoryAvailable controls whether the prior pace/change are meaningful.
// The legacy SpendingVelocity entry point remains compatible for existing callers.
func SpendingVelocityForPeriod(allData *models.TransactionSet, p models.PeriodContext) *models.SpendingVelocity {
	v := &models.SpendingVelocity{Period: &p}
	if !p.Valid {
		return v
	}
	v.DailyAverage = models.RoundToCents(metrics.SignedNet(TransactionsForPeriod(allData, p.SelectedStart, p.SelectedEnd).FilterByType(models.Outflow)) / float64(p.SelectedDays))
	if p.HistoryAvailable {
		v.HistoricalDaily = models.RoundToCents(metrics.SignedNet(TransactionsForPeriod(allData, p.PreviousStart, p.PreviousEnd).FilterByType(models.Outflow)) / float64(p.PreviousDays))
		v.BurnRateChange = ChangeDisplay(v.HistoricalDaily, v.DailyAverage).Percent
	}
	if p.ForecastAvailable {
		v.MonthProjection = p.ForecastAmount
		v.DaysRemaining = p.DaysInMonth - p.ElapsedDays
	}
	return v
}
