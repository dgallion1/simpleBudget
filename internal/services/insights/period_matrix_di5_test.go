package insights

import (
	"budget2/internal/models"
	"testing"
	"time"
)

func TestDI5CalendarMatrix(t *testing.T) {
	for year := 2023; year <= 2027; year++ {
		for month := time.January; month <= time.December; month++ {
			start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 1, -1)
			now := end.AddDate(0, 0, 1)
			p := BuildPeriodContext(nil, start, end, now)
			if !p.PreviousStart.Equal(start.AddDate(0, -1, 0)) || !p.PreviousEnd.Equal(start.AddDate(0, 0, -1)) {
				t.Fatalf("full-month %s: %+v", start, p)
			}
			for days := 1; days <= end.Day(); days++ {
				today := start.AddDate(0, 0, days-1)
				p = BuildPeriodContext(nil, start, today, today)
				priorEnd := start.AddDate(0, -1, days-1)
				if !priorEnd.Before(start) {
					priorEnd = start.AddDate(0, 0, -1)
				}
				if !p.PreviousEnd.Equal(priorEnd) || p.SelectedDays != days {
					t.Fatalf("MTD %s: %+v", today, p)
				}
			}
		}
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []time.Time{time.Date(2026, 3, 7, 0, 0, 0, 0, loc), time.Date(2026, 10, 31, 0, 0, 0, 0, loc)} {
		end := start.AddDate(0, 0, 9)
		ts := models.NewTransactionSet([]models.Transaction{{Date: time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)}, {Date: end.Add(23 * time.Hour)}})
		p := BuildPeriodContext(ts, start, end, end.AddDate(0, 1, 0))
		if p.SelectedDays != 10 || p.PreviousDays != 10 || TransactionsForPeriod(ts, start, end).Len() != 2 {
			t.Fatalf("DST bounds/filter mismatch: %+v", p)
		}
	}
}

func TestDI5ForecastNoBorrowing(t *testing.T) {
	for today := 6; today <= 16; today++ {
		for age := 0; age <= 8; age++ {
			now := time.Date(2026, 9, today, 23, 0, 0, 0, time.FixedZone("west", -5*3600))
			start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			tx := models.NewTransactionSet([]models.Transaction{{Date: calendarDate(now).AddDate(0, 0, -age), Amount: -70, TransactionType: models.Outflow}})
			for offset := -1; offset <= 1; offset++ {
				p := BuildPeriodContext(tx, start, calendarDate(now).AddDate(0, 0, offset), now)
				want := offset >= 0 && today >= 7 && age <= 7 && today-age >= 1
				if p.ForecastAvailable != want || p.Stale != (age > 7) {
					t.Fatalf("today=%d age=%d endOffset=%d want=%v got=%+v", today, age, offset, want, p)
				}
			}
		}
	}
}
