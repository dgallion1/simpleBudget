package insights

import (
	"budget2/internal/models"
	"testing"
	"time"
)

func TestDI5CivilNowAndForecastBounds(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 23, 30, 0, 0, loc)
	ts := models.NewTransactionSet([]models.Transaction{di2Txn("2026-09-07", -70)})
	p := BuildPeriodContext(ts, di2Date("2026-09-01"), di2Date("2026-09-30"), now)
	if p.Today != di2Date("2026-09-07") || p.ReferenceDate != di2Date("2026-09-07") || p.ElapsedDays != 7 || !p.ForecastAvailable || p.ForecastAmount != 300 {
		t.Fatalf("civil now/future selection: %+v", p)
	}
	future := models.NewTransactionSet(append(append([]models.Transaction{}, ts.Transactions...), di2Txn("2026-09-08", -10000)))
	fp := BuildPeriodContext(future, di2Date("2026-09-01"), di2Date("2026-09-30"), now)
	if fp.ForecastAvailable || fp.ForecastReason != "Latest transaction is after today" {
		t.Fatalf("future data borrowed: %+v", fp)
	}
	early := BuildPeriodContext(ts, di2Date("2026-09-01"), di2Date("2026-09-06"), now)
	if early.ForecastAvailable {
		t.Fatal("earlier selection borrowed current records")
	}
	filtered := TransactionsForPeriod(future, time.Date(2026, 9, 7, 23, 59, 0, 0, loc), time.Date(2026, 9, 7, 23, 59, 0, 0, loc))
	if filtered.Len() != 1 || filtered.Transactions[0].Amount != -70 {
		t.Fatalf("civil end-day filter: %+v", filtered)
	}
	t.Log("local Sept7 23:30 remains Sept7; projected 300 through today; future data and earlier selection unavailable; civil day includes UTC date-only row")
}
