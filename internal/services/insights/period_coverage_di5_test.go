package insights

import (
	"budget2/internal/models"
	"testing"
	"time"
)

func TestDI5CivilCoverageBoundaries(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("west", -5*3600), time.FixedZone("east", 9*3600)} {
		for _, hour := range []int{0, 12, 23} {
			for firstOffset := -1; firstOffset <= 1; firstOffset++ {
				for lastOffset := -1; lastOffset <= 1; lastOffset++ {
					first := time.Date(2025, 2, 1+firstOffset, hour, 30, 0, 0, zone)
					last := time.Date(2025, 2, 28+lastOffset, hour, 30, 0, 0, zone)
					tx := models.NewTransactionSet([]models.Transaction{{Date: first}, {Date: last}})
					want := firstOffset <= 0 && lastOffset >= 0
					if got := HasCivilDateCoverage(tx, start, end); got != want {
						t.Fatalf("first=%s last=%s got=%v want=%v", first, last, got, want)
					}
					// Range times also carry civil semantics and must not change the result.
					if got := HasCivilDateCoverage(tx, start.Add(12*time.Hour), end.Add(23*time.Hour)); got != want {
						t.Fatal("range time changed civil enclosure")
					}
				}
			}
		}
	}
	exact := models.NewTransactionSet([]models.Transaction{{Date: start}, {Date: end}})
	for _, bounds := range [][2]time.Time{{{}, end}, {start, {}}, {end, start}} {
		if HasCivilDateCoverage(exact, bounds[0], bounds[1]) {
			t.Fatal("invalid range accepted")
		}
	}
	if HasCivilDateCoverage(nil, start, end) || HasCivilDateCoverage(&models.TransactionSet{}, start, end) {
		t.Fatal("empty evidence accepted")
	}
	onlySuppressed := models.NewTransactionSet([]models.Transaction{{Date: start, Suppressed: true}, {Date: end, Suppressed: true}})
	if HasCivilDateCoverage(onlySuppressed, start, end) {
		t.Fatal("suppressed evidence accepted")
	}
	partial := models.NewTransactionSet([]models.Transaction{{Date: start, Suppressed: true}, {Date: start.AddDate(0, 0, 1)}, {Date: end}})
	if HasCivilDateCoverage(partial, start, end) {
		t.Fatal("suppressed first bound supplied coverage")
	}
	if !HasCivilDateCoverage(exact, start, start) {
		t.Fatal("inclusive one-day coverage rejected")
	}
}
