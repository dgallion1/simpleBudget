package insights

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

func di2Date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}
func di2Txn(date string, amount float64) models.Transaction {
	return models.Transaction{Date: di2Date(date), Amount: amount, TransactionType: models.Outflow, Category: "Home"}
}

func TestDI2PreviousCalendarBounds(t *testing.T) {
	for _, tc := range []struct {
		start, end, now, previousStart, previousEnd string
		days, priorDays                             int
		clamped                                     bool
	}{
		{"2026-08-01", "2026-08-31", "2026-09-06", "2026-07-01", "2026-07-31", 31, 31, false},
		{"2026-09-01", "2026-09-06", "2026-09-06", "2026-08-01", "2026-08-06", 6, 6, false},
		{"2026-08-11", "2026-08-20", "2026-09-06", "2026-08-01", "2026-08-10", 10, 10, false},
		{"2024-02-01", "2024-02-29", "2024-03-06", "2024-01-01", "2024-01-31", 29, 31, false},
		{"2026-03-01", "2026-03-31", "2026-03-31", "2026-02-01", "2026-02-28", 31, 28, true},
		{"2024-03-01", "2024-03-30", "2024-03-30", "2024-02-01", "2024-02-29", 30, 29, true},
	} {
		t.Run(tc.start+tc.end, func(t *testing.T) {
			p := BuildPeriodContext(nil, di2Date(tc.start), di2Date(tc.end), di2Date(tc.now))
			if p.PreviousStart.Format("2006-01-02") != tc.previousStart || p.PreviousEnd.Format("2006-01-02") != tc.previousEnd || p.SelectedDays != tc.days || p.PreviousDays != tc.priorDays || p.ComparisonClamped != tc.clamped {
				t.Fatalf("unexpected context: %+v", p)
			}
			if !p.PreviousEnd.Before(p.SelectedStart) {
				t.Fatal("comparison overlaps selected period")
			}
			if p.ReferenceDate.Format("2006-01-02") != tc.end || p.ReferenceMonth.Month() != p.SelectedEnd.Month() || p.DaysInMonth != p.ReferenceMonth.AddDate(0, 1, -1).Day() {
				t.Fatalf("reference month/date mismatch: %+v", p)
			}
		})
	}
}

func TestDI2ObservedZeroPriorIsAvailable(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: di2Date("2026-07-01"), Amount: 100, TransactionType: models.Income},
		di2Txn("2026-08-28", -100),
	})
	p := BuildPeriodContext(ts, di2Date("2026-08-01"), di2Date("2026-08-31"), di2Date("2026-09-06"))
	if !p.HistoryAvailable || p.HistoryReason != "" {
		t.Fatalf("observed zero treated as missing history: %+v", p)
	}
	rows := CategoryTrendsForPeriod(ts, p)
	if len(rows) != 1 || rows[0].PreviousAmount != 0 || rows[0].Change.Kind != "new" {
		t.Fatalf("zero baseline: %+v", rows)
	}
}

func TestDI2CalendarDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 7, 0, 0, 0, 0, loc)
	end := time.Date(2026, 3, 16, 23, 0, 0, 0, loc)
	p := BuildPeriodContext(nil, start, end, time.Date(2026, 4, 1, 0, 0, 0, 0, loc))
	if p.SelectedDays != 10 || p.PreviousStart.Format("2006-01-02") != "2026-02-25" || p.PreviousEnd.Format("2006-01-02") != "2026-03-06" {
		t.Fatalf("DST changed calendar bounds: %+v", p)
	}
}

func TestDI2ForecastEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, start, end, now, latest string
		available, stale              bool
	}{
		{"demo historical", "2026-08-01", "2026-08-28", "2026-09-06", "2026-08-28", false, true},
		{"demo September", "2026-09-01", "2026-09-06", "2026-09-06", "2026-08-28", false, true},
		{"six days", "2026-09-01", "2026-09-06", "2026-09-06", "2026-09-06", false, false},
		{"seven days", "2026-09-01", "2026-09-07", "2026-09-07", "2026-09-07", true, false},
		{"seven days old", "2026-09-01", "2026-09-14", "2026-09-14", "2026-09-07", true, false},
		{"eight days old", "2026-09-01", "2026-09-15", "2026-09-15", "2026-09-07", false, true},
		{"missing month start", "2026-09-02", "2026-09-15", "2026-09-15", "2026-09-15", false, false},
		{"selection ends early", "2026-09-01", "2026-09-14", "2026-09-15", "2026-09-15", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := models.NewTransactionSet([]models.Transaction{di2Txn(tc.latest, -70)})
			p := BuildPeriodContext(ts, di2Date(tc.start), di2Date(tc.end), di2Date(tc.now))
			if p.ForecastAvailable != tc.available || p.Stale != tc.stale {
				t.Fatalf("availability/staleness: %+v", p)
			}
			if !tc.available && (p.ForecastReason == "" || p.ForecastAmount != 0) {
				t.Fatalf("missing unavailability reason: %+v", p)
			}
		})
	}
}

func TestDI2ForecastSignedNetAndFullHistory(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		di2Txn("2026-08-01", -200), di2Txn("2026-09-03", -100), di2Txn("2026-09-07", 170),
		{Date: di2Date("2026-09-07"), Amount: -999, TransactionType: models.Transfer},
		{Date: di2Date("2026-09-20"), Amount: -999, TransactionType: models.Outflow, Suppressed: true},
	})
	p := BuildPeriodContext(ts, di2Date("2026-09-01"), di2Date("2026-09-07"), di2Date("2026-09-07"))
	if !p.ForecastAvailable || p.ForecastAmount != -300 || p.LatestTransaction != di2Date("2026-09-07") || !p.HistoryAvailable {
		t.Fatalf("wrong signed/active context: %+v", p)
	}
	v := SpendingVelocityForPeriod(ts, p)
	if v.DailyAverage != -10 || v.HistoricalDaily != 28.57 {
		t.Fatalf("pace includes wrong days or current history: %+v", v)
	}
	ts.Transactions[2].Amount = 100
	p = BuildPeriodContext(ts, di2Date("2026-09-01"), di2Date("2026-09-07"), di2Date("2026-09-07"))
	if p.ForecastAmount != 0 || math.Signbit(p.ForecastAmount) {
		t.Fatal("forecast negative zero")
	}
}

func TestDI2NoDataAndUnknownHistory(t *testing.T) {
	for _, ts := range []*models.TransactionSet{nil, models.NewTransactionSet([]models.Transaction{di2Txn("2026-08-28", -100)})} {
		p := BuildPeriodContext(ts, di2Date("2026-08-01"), di2Date("2026-08-31"), di2Date("2026-09-06"))
		if p.HistoryAvailable || p.HistoryReason == "" || p.ForecastAvailable {
			t.Fatalf("invented history/forecast: %+v", p)
		}
		if got := CategoryTrendsForPeriod(ts, p); len(got) != 0 {
			t.Fatal("comparison invented from missing history")
		}
	}
}

func TestDI2ExplicitComparisonsSignedAndRounded(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: di2Date("2026-02-01"), Amount: 1, TransactionType: models.Income},
		{Hash: "prior", Date: di2Date("2026-02-02"), Amount: -30.004, Category: "Home", TransactionType: models.Outflow},
		{Hash: "current", Date: di2Date("2026-03-02"), Amount: -100.004, Category: "Home", TransactionType: models.Outflow},
		{Hash: "refund", Date: di2Date("2026-03-03"), Amount: 150, Category: "Home", TransactionType: models.Outflow},
		{Date: di2Date("2026-03-31"), Amount: 1, TransactionType: models.Income},
	})
	p := BuildPeriodContext(ts, di2Date("2026-03-01"), di2Date("2026-03-31"), di2Date("2026-04-01"))
	defs := []models.MajorExpense{{ID: "home", Name: "Home"}}
	pins := map[string]string{"prior": "home", "current": "home", "refund": "home"}
	for _, rows := range [][]models.CategoryTrend{CategoryTrendsForPeriod(ts, p), MajorExpenseTrendsForPeriod(ts, defs, pins, p)} {
		if len(rows) != 1 {
			t.Fatalf("rows: %+v", rows)
		}
		r := rows[0]
		if r.CurrentAmount != -50 || r.PreviousAmount != 30 || r.Change.Kind != "dollar" || r.Change.Amount != -80 || r.ChangeAmount != r.Change.Amount || r.Direction != "down" {
			t.Fatalf("signed rounded comparison: %+v", r)
		}
	}
}
