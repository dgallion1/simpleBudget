package admin

import (
	"strings"
	"testing"
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
