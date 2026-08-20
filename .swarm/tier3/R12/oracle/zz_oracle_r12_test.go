package accounts

// Tier-3 acceptance oracle for R12 (Project's day handling, UTC calendar).
// Lead-authored before dispatch; copied into the package by accept.sh and
// removed afterwards. Both blind implementations are judged against THIS file,
// so neither worker may edit it.
//
// USER DECISION (2026-08-20): the projection speaks in the DATA'S UTC calendar.
// Every day boundary inside Project -- the starting-balance cutoff, the walk
// grid, and each occurrence label -- is a UTC calendar day. A host's local
// timezone must not change any figure Project reports.
//
// Three defects this oracle pins, each found by a checker during R12's two
// failed Tier-2 attempts:
//   1. byDay was keyed by time.Time, which Go compares by struct fields
//      including the *Location pointer, so no time.Now()-derived projection
//      applied ANY recurring occurrence -- on every host, UTC included.
//   2. The advance filter compared instants while the walk read calendar-day
//      labels, so an occurrence strictly after asOf could carry a label the
//      walk never visits and vanish (Asia/Tokyo, yearly: $400 lost).
//   3. Normalising only the grid left BalanceAt's cutoff in asOf's own
//      Location, dropping an already-posted transaction from the starting
//      balance (Asia/Tokyo 20:00: true 700.00 reported as 1000.00).

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func zzR12Acct(anchor float64, threshold float64) models.Account {
	return models.Account{
		ID: "chk", Name: "Checking", Kind: models.AccountKindChecking,
		LowBalanceThreshold: threshold,
		Anchors: []models.BalanceAnchor{{
			Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: anchor,
		}},
	}
}

func zzR12Recurring(freq string, next time.Time, amt float64) models.RecurringPayment {
	return models.RecurringPayment{
		Description: "rent", Amount: amt, Frequency: freq, NextExpected: next,
		Transactions: []models.Transaction{{
			AccountID: "chk", Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: -amt,
		}},
	}
}

var zzR12Zones = []string{"UTC", "America/New_York", "Asia/Tokyo", "Pacific/Midway", "Pacific/Kiritimati"}

// Check 1 — THE HEADLINE INVARIANT. Project's output must depend only on the
// UTC instant of asOf, never on the Location it carries. The same instant
// expressed in five zones must produce five identical results. This subsumes
// all three defects: any Location-sensitive key, label or cutoff breaks it.
func TestZZOracleR12_OutputDependsOnlyOnTheUTCInstant(t *testing.T) {
	base := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	txs := []models.Transaction{{
		AccountID: "chk", Date: time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC), Amount: -300,
	}}
	rec := []models.RecurringPayment{
		zzR12Recurring("monthly", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), 400),
	}

	type result struct {
		crossing string
		min      float64
		topup    float64
		avail    bool
	}
	var first result
	for i, name := range zzR12Zones {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("zone %s unavailable: %v", name, err)
		}
		got, err := Project(zzR12Acct(1000, 500), txs, base.In(loc), rec)
		if err != nil {
			t.Fatalf("%s: Project: %v", name, err)
		}
		cr := ""
		if !got.Crossing.IsZero() {
			cr = got.Crossing.UTC().Format("2006-01-02")
		}
		r := result{cr, got.Minimum, got.SuggestedTopUp, got.Available}
		if i == 0 {
			first = r
			t.Logf("baseline (%s): crossing=%q minimum=%.2f topup=%.2f", name, r.crossing, r.min, r.topup)
			continue
		}
		if r != first {
			t.Fatalf("zone %s changed the answer: got %+v, baseline (UTC) %+v — "+
				"Project's output must depend only on asOf's UTC instant", name, r, first)
		}
	}
}

// Check 2 — the starting balance uses the UTC calendar day. A transaction six
// hours before asOf, on asOf's own UTC day, must be counted. This is the
// defect attempt 2 introduced by normalising the grid but not BalanceAt.
func TestZZOracleR12_StartingBalanceUsesTheUTCDay(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("Asia/Tokyo unavailable: %v", err)
	}
	// 2026-09-01 20:00 JST == 2026-09-01 11:00 UTC
	asOf := time.Date(2026, 9, 1, 20, 0, 0, 0, tokyo)
	txs := []models.Transaction{{
		AccountID: "chk", Date: time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC), Amount: -300,
	}}
	got, err := Project(zzR12Acct(1000, 800), txs, asOf, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.Minimum != 700 {
		t.Fatalf("minimum = %.2f, want 700.00 — a transaction on asOf's own UTC day "+
			"was dropped from the starting balance", got.Minimum)
	}
}

// Check 3 — a recurring occurrence on a UTC day strictly after asOf's UTC day
// is applied. This is the defect the calendarDay key fix exposed.
func TestZZOracleR12_OccurrenceAfterAsOfIsApplied(t *testing.T) {
	for _, name := range zzR12Zones {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("zone %s unavailable: %v", name, err)
		}
		asOf := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC).In(loc)
		rec := []models.RecurringPayment{
			zzR12Recurring("yearly", time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), 400),
		}
		got, err := Project(zzR12Acct(800, 500), nil, asOf, rec)
		if err != nil {
			t.Fatalf("%s: Project: %v", name, err)
		}
		if got.Crossing.IsZero() {
			t.Fatalf("%s: no crossing — the occurrence on the next UTC day was not applied", name)
		}
		if got.Minimum != 400 {
			t.Fatalf("%s: minimum = %.2f, want 400.00", name, got.Minimum)
		}
	}
}

// Check 4 — a time.Now()-derived asOf applies recurring occurrences at all.
// The original defect made this impossible on every host.
func TestZZOracleR12_TimeNowDerivedAsOfAppliesRecurring(t *testing.T) {
	now := time.Now()
	nowUTCDay := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	acct := models.Account{
		ID: "chk", Name: "Checking", Kind: models.AccountKindChecking,
		LowBalanceThreshold: 500,
		Anchors:             []models.BalanceAnchor{{Date: nowUTCDay.AddDate(0, 0, -30), Amount: 1000}},
	}
	// Day 10, not day 5. "monthly" resolves to a fixed 30-day interval and the
	// horizon walk is `for d := 1; d <= 35`, so a first occurrence at +5 puts the
	// SECOND at exactly +35 -- inside the inclusive window -- and the expected
	// minimum would be -200, not 400. That was a bug in this fixture, caught by
	// R12's Tier-3 primary arm, which flagged it rather than making day 35
	// exclusive to go green (which would have broken the pre-existing, accepted
	// TestProject_HorizonIs35Days). At +10 the second occurrence lands at +40,
	// outside the window, so exactly one occurrence applies.
	rec := []models.RecurringPayment{
		zzR12Recurring("monthly", nowUTCDay.AddDate(0, 0, 10), 600),
	}
	got, err := Project(acct, nil, now, rec)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.Crossing.IsZero() {
		t.Fatal("no crossing from a time.Now()-derived asOf — recurring occurrences " +
			"are still being dropped, which is the original R12 defect")
	}
	if got.Minimum != 400 {
		t.Fatalf("minimum = %.2f, want 400.00", got.Minimum)
	}
}
