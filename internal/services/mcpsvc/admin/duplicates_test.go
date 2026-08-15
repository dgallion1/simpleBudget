package admin

import (
	"strings"
	"testing"

	"budget2/internal/services/dataloader"
)

func TestListDuplicatesReturnsTheDetectedPair(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))

	if out.UnresolvedCount != 1 {
		t.Fatalf("unresolved_count = %d, want 1; pairs = %+v", out.UnresolvedCount, out.Unresolved)
	}
	p := out.Unresolved[0]
	if p.PairKey == "" {
		t.Error("pair_key is empty; resolve_duplicates cannot be called without it")
	}
	amounts := []float64{p.Left.Amount, p.Right.Amount}
	for _, a := range amounts {
		if a != -250.00 {
			t.Errorf("amount = %v, want -250 (signed as stored, expenses negative)", a)
		}
	}
	if p.Left.Hash == "" || p.Right.Hash == "" {
		t.Error("a side is missing its hash; resolve_duplicates needs both to name a winner")
	}
	if p.Left.Hash == p.Right.Hash {
		t.Error("both sides share a hash; they are not two distinct transactions")
	}
	if out.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d, want 0 before any decision", out.ResolvedCount)
	}
}

// A kept_both decision changes no total, so it is absent from the ledger's
// resolved list by design. It is still a recorded, undoable decision, and if
// list_duplicates does not surface it the pair_key exists in no tool output
// anywhere: undo_resolve refuses an empty key, resolve_duplicates rejects the
// pair as "not awaiting review", and a model that has lost context can never
// name it again. This test walks that exact round trip.
func TestListDuplicatesSurfacesKeptBothSoItsKeyStaysRecoverable(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)

	before := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if before.UnresolvedCount != 1 {
		t.Fatalf("unresolved_count = %d, want 1 before deciding", before.UnresolvedCount)
	}
	key := before.Unresolved[0].PairKey

	call(t, cs, "resolve_duplicates", map[string]any{"pair_key": key, "outcome": "kept_both"})

	after := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{
		"include_resolved": true,
	}))
	if after.UnresolvedCount != 0 {
		t.Errorf("unresolved_count = %d, want 0 after the decision", after.UnresolvedCount)
	}
	if after.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d, want 0: kept_both suppressed nothing, so it is not a kept_winner", after.ResolvedCount)
	}
	if after.KeptBothCount != 1 {
		t.Fatalf("kept_both_count = %d, want 1; the decision is recorded but invisible", after.KeptBothCount)
	}
	if len(after.KeptBoth) != 1 {
		t.Fatalf("kept_both has %d entries, want 1", len(after.KeptBoth))
	}
	if got := after.KeptBoth[0].PairKey; got != key {
		t.Fatalf("kept_both pair_key = %q, want %q", got, key)
	}
	if after.KeptBoth[0].Left.Hash == "" || after.KeptBoth[0].Right.Hash == "" {
		t.Error("a kept_both side is missing its hash; the pair cannot be re-resolved after an undo")
	}

	// The recovered key must actually work, or surfacing it proved nothing.
	undone := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))
	if undone.PreviousOutcome != string(dataloader.DuplicateOutcomeKeptBoth) {
		t.Errorf("previous_outcome = %q, want kept_both", undone.PreviousOutcome)
	}
	if undone.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1: the pair should be back in the queue", undone.UnresolvedRemaining)
	}
}

// kept_both must not leak into the resolved list: the Duplicates page renders
// a resolved pair's Right side as the suppressed one, and a kept_both pair has
// no suppressed side, so counting it there would mislabel a live transaction
// as excluded.
func TestKeptBothIsNotReportedAsAResolvedPair(t *testing.T) {
	deps, _ := newLiveDeps(t)
	loader, ok := deps.Duplicates.(*dataloader.DataLoader)
	if !ok {
		t.Fatalf("Duplicates is %T, want *dataloader.DataLoader", deps.Duplicates)
	}
	cs := connect(t, deps)

	before := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	key := before.Unresolved[0].PairKey
	call(t, cs, "resolve_duplicates", map[string]any{"pair_key": key, "outcome": "kept_both"})

	if got := loader.ResolvedDuplicates(); len(got) != 0 {
		t.Errorf("ResolvedDuplicates() has %d entries, want 0; the Duplicates page would show a suppressed side that does not exist", len(got))
	}
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	for _, tx := range ts.Transactions {
		if tx.Suppressed {
			t.Errorf("transaction %q is suppressed after kept_both; both sides must stay live", tx.Label())
		}
	}
}

// The store in a t.TempDir() is never encrypted, so the locked path cannot be
// reached here without building an encrypted fixture. What this test pins is
// the weaker but still load-bearing half: a load failure surfaces as a tool
// error, NOT as an empty queue. An empty queue would tell the model the user
// has nothing to review, which is the opposite of "we could not look".
func TestListDuplicatesReportsALoadFailureRatherThanAnEmptyQueue(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Transactions = stubTransactions{err: errNoLedger}
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "list_duplicates", map[string]any{}))
	if !strings.Contains(msg, errNoLedger.Error()) {
		t.Errorf("error text %q does not carry the underlying cause %q", msg, errNoLedger)
	}
}
