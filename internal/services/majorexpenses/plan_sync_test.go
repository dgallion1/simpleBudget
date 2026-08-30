package majorexpenses

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// TestComputePlanSyncExclusions_FirstDefWinsTrap is D-SY-b's core guarantee:
// an unflagged keyword def listed BEFORE a flagged exact-amount def must
// keep claiming a transaction that would otherwise match the flagged def's
// amount. A flagged-only-filtered match pass would get this wrong.
func TestComputePlanSyncExclusions_FirstDefWinsTrap(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "gym", Name: "Gym", Keywords: []string{"GYM"}},
		{ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h-gym", Description: "GYM MEMBERSHIP", Amount: -500, TransactionType: models.Outflow},
		{Hash: "h-car", Description: "CAR LOAN PAYMENT", Amount: -500, TransactionType: models.Outflow},
	})

	got := ComputePlanSyncExclusions(ts, defs, nil)

	if _, wrong := got["h-gym"]; wrong {
		t.Error("gym row matches the earlier unflagged def; it must not be excluded")
	}
	if def, ok := got["h-car"]; !ok || def.ID != "car" {
		t.Errorf("car-loan row: want excluded by def car, got ok=%v def=%+v", ok, def)
	}
}

// A pin to a flagged def must exclude a row its own keywords/amounts would
// miss — the pin is explicit user intent and overrides matching entirely.
func TestComputePlanSyncExclusions_PinToFlaggedDefExcludes(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	// Amount doesn't match the def's exact-amount rule, and there's no
	// keyword at all — only the pin makes this row belong to "car".
	tr := models.Transaction{Hash: "h1", Description: "Miscellaneous check", Amount: -617, TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{tr})

	got := ComputePlanSyncExclusions(ts, defs, map[string]string{"h1": "car"})

	if def, ok := got["h1"]; !ok || def.ID != "car" {
		t.Errorf("pinned row: want excluded by def car, got ok=%v def=%+v", ok, def)
	}
}

// A pin to an UNFLAGGED def must beat a flagged def's keyword/amount match —
// the pin is the user's explicit classification and wins outright.
func TestComputePlanSyncExclusions_PinToUnflaggedDefBeatsFlaggedMatch(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
		{ID: "misc", Name: "Misc"},
	}
	// Amount matches the flagged "car" def exactly, but the row is pinned
	// to the unflagged "misc" def.
	tr := models.Transaction{Hash: "h1", Description: "Weird one-off", Amount: -500, TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{tr})

	got := ComputePlanSyncExclusions(ts, defs, map[string]string{"h1": "misc"})

	if _, wrong := got["h1"]; wrong {
		t.Error("row pinned to an unflagged def must not appear in the exclusion map")
	}
}

// A refund (positive amount, but still typed as an outflow — e.g. a partial
// reversal) inside a flagged group must be excluded like its siblings:
// MatchTransaction compares abs(amount), and ComputePlanSyncExclusions must
// not add its own sign filter on top.
func TestComputePlanSyncExclusions_RefundInFlaggedGroupIsExcluded(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	refund := models.Transaction{Hash: "h-refund", Description: "CAR LOAN PAYMENT", Amount: 500, TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{refund})

	got := ComputePlanSyncExclusions(ts, defs, nil)

	if def, ok := got["h-refund"]; !ok || def.ID != "car" {
		t.Errorf("refund row: want excluded by def car like its siblings, got ok=%v def=%+v", ok, def)
	}
}

// Nil/empty-safe on every input: nil ts, nil defs, nil pins must all produce
// an empty, non-nil map, never a panic.
func TestComputePlanSyncExclusions_NilSafe(t *testing.T) {
	cases := []struct {
		name string
		ts   *models.TransactionSet
		defs []models.MajorExpense
		pins map[string]string
	}{
		{"all nil", nil, nil, nil},
		{"nil ts only", nil, []models.MajorExpense{{ID: "x", ExcludeFromPlanSync: true}}, nil},
		{"nil defs only", models.NewTransactionSet(nil), nil, nil},
		{"empty ts, no flagged defs", models.NewTransactionSet(nil), []models.MajorExpense{{ID: "x"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputePlanSyncExclusions(tc.ts, tc.defs, tc.pins)
			if got == nil {
				t.Fatal("expected a non-nil empty map")
			}
			if len(got) != 0 {
				t.Errorf("expected empty map, got %d entries: %+v", len(got), got)
			}
		})
	}
}

// A def with no flagged entries at all must short-circuit to an empty map
// even when transactions would otherwise match it.
func TestComputePlanSyncExclusions_NoFlaggedDefsReturnsEmpty(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "rent", Name: "Rent", Keywords: []string{"rent"}},
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Hash: "h1", Description: "Rent payment", Amount: -2000, TransactionType: models.Outflow, Date: time.Now()},
	})

	got := ComputePlanSyncExclusions(ts, defs, nil)
	if len(got) != 0 {
		t.Errorf("expected empty map when no def is flagged, got %+v", got)
	}
}
