package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/transfers"
)

// fakeTransferSource is a TransferSource whose SuspectedTransfers() answer is
// canned rather than computed by the classifier, and which records every call
// so a test can assert on what was (or was not) invoked -- in particular that
// get_suspected_transfers never calls ResolveTransfer (it is read-only).
type fakeTransferSource struct {
	suspected []transfers.Suspected

	suspectedCalled bool
	resolveCalled   bool
	resolveErr      error
}

func (f *fakeTransferSource) SuspectedTransfers() []transfers.Suspected {
	f.suspectedCalled = true
	return f.suspected
}

func (f *fakeTransferSource) ResolveTransfer(pairKey string, v transfers.Verdict) error {
	f.resolveCalled = true
	return f.resolveErr
}

// newFakeDeps builds Deps over a stubbed TransactionSource and a
// fakeTransferSource, so a suspected pair can be handed to the tool directly
// rather than reconstructed through real CSV parsing and classification.
// txErr, when non-nil, makes deps.load() fail -- the fixture for the "load
// failure means the queue is never read" case.
func newFakeDeps(t *testing.T, suspected []transfers.Suspected, txErr error) (Deps, *fakeTransferSource) {
	t.Helper()
	fake := &fakeTransferSource{suspected: suspected}
	deps := Deps{
		Transactions: stubTransactions{ts: models.NewTransactionSet(nil), err: txErr},
		Transfers:    fake,
		Confirm:      confirm.NewRegistry(5 * time.Minute),
	}
	return deps, fake
}

// stubTransactions is a TransactionSource whose LoadData() answer is canned,
// matching the pattern the curate/spend/admin mcpsvc packages already use for
// this same interface.
type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

// twoSuspectedPairs builds two candidate pairs distinguishable by reason,
// legs, and pair_key, so a round trip that mixed them up or dropped a field
// would be caught.
func twoSuspectedPairs() []transfers.Suspected {
	day := func(s string) time.Time {
		d, _ := time.Parse("2006-01-02", s)
		return d
	}
	return []transfers.Suspected{
		{
			PairKey: "pairkey-amount-match",
			Reason:  transfers.ReasonAmountMatch,
			Left: models.Transaction{
				Date: day("2026-08-10"), Description: "MYSTERY OUTFLOW", AccountID: "checking",
				Amount: -777.77, Category: "Uncategorized",
			},
			Right: models.Transaction{
				Date: day("2026-08-10"), Description: "MYSTERY INFLOW", AccountID: "brokerage",
				Amount: 777.77,
			},
		},
		{
			PairKey: "pairkey-ambiguous",
			Reason:  transfers.ReasonAmbiguous,
			Left: models.Transaction{
				Date: day("2026-08-12"), Description: "TIED CANDIDATE A", AccountID: "checking",
				Amount: -300, Category: "Transfer",
			},
			Right: models.Transaction{
				Date: day("2026-08-13"), Description: "TIED CANDIDATE B", AccountID: "savings",
				Amount: 300,
			},
		},
	}
}

// B.1: two suspected pairs from a fake TransferSource round-trip with
// correct pair_key, reason, and both legs' date/account_id/signed amount.
func TestGetSuspectedTransfersRoundTripsPairsFromTheQueue(t *testing.T) {
	deps, fake := newFakeDeps(t, twoSuspectedPairs(), nil)
	cs := connect(t, deps)

	out := decodeToolResult[getSuspectedTransfersOutput](t, call(t, cs, "get_suspected_transfers", map[string]any{}))
	if out.Count != 2 {
		t.Fatalf("count = %d, want 2", out.Count)
	}
	if len(out.Pairs) != 2 {
		t.Fatalf("pairs = %d, want 2", len(out.Pairs))
	}

	first := out.Pairs[0]
	if first.PairKey != "pairkey-amount-match" {
		t.Errorf("pairs[0].pair_key = %q, want pairkey-amount-match", first.PairKey)
	}
	if first.Reason != transfers.ReasonAmountMatch {
		t.Errorf("pairs[0].reason = %q, want %q", first.Reason, transfers.ReasonAmountMatch)
	}
	if first.Left.Date != "2026-08-10" || first.Left.AccountID != "checking" || first.Left.Amount != -777.77 {
		t.Errorf("pairs[0].left = %+v, want date=2026-08-10 account_id=checking amount=-777.77", first.Left)
	}
	if first.Left.Description != "MYSTERY OUTFLOW" {
		t.Errorf("pairs[0].left.description = %q, want the transaction's Label()", first.Left.Description)
	}
	if first.Left.Category != "Uncategorized" {
		t.Errorf("pairs[0].left.category = %q, want Uncategorized", first.Left.Category)
	}
	if first.Right.Date != "2026-08-10" || first.Right.AccountID != "brokerage" || first.Right.Amount != 777.77 {
		t.Errorf("pairs[0].right = %+v, want date=2026-08-10 account_id=brokerage amount=777.77", first.Right)
	}
	if first.Right.Category != "" {
		t.Errorf("pairs[0].right.category = %q, want empty (omitempty, no category on this leg)", first.Right.Category)
	}

	second := out.Pairs[1]
	if second.PairKey != "pairkey-ambiguous" {
		t.Errorf("pairs[1].pair_key = %q, want pairkey-ambiguous", second.PairKey)
	}
	if second.Reason != transfers.ReasonAmbiguous {
		t.Errorf("pairs[1].reason = %q, want %q", second.Reason, transfers.ReasonAmbiguous)
	}
	if second.Left.AccountID != "checking" || second.Right.AccountID != "savings" {
		t.Errorf("pairs[1] legs = left:%q right:%q, want checking/savings", second.Left.AccountID, second.Right.AccountID)
	}

	// C: no write call was made anywhere in the call.
	if fake.resolveCalled {
		t.Error("ResolveTransfer was called; get_suspected_transfers must be read-only")
	}
}

// B.2: an empty queue reports count 0 and no error.
func TestGetSuspectedTransfersOnEmptyQueue(t *testing.T) {
	deps, _ := newFakeDeps(t, nil, nil)
	cs := connect(t, deps)

	out := decodeToolResult[getSuspectedTransfersOutput](t, call(t, cs, "get_suspected_transfers", map[string]any{}))
	if out.Count != 0 {
		t.Fatalf("count = %d, want 0 on an empty queue", out.Count)
	}
	if len(out.Pairs) != 0 {
		t.Errorf("pairs = %v, want none", out.Pairs)
	}
}

// B.3: Transfers == nil reports the missing configuration, matching the
// package's nil-dep convention (resolve_transfer's identical check).
func TestGetSuspectedTransfersWithoutTransferSourceReportsIt(t *testing.T) {
	deps, _ := newFakeDeps(t, twoSuspectedPairs(), nil)
	deps.Transfers = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "get_suspected_transfers", map[string]any{}))
	if !strings.Contains(msg, "no transfer source is configured") {
		t.Errorf("error = %q, want it to name the missing transfer source", msg)
	}
}

// B.4: a load failure is the tool's error, and the queue is never read --
// proven by asserting SuspectedTransfers() was never called, not merely that
// the call returned an error.
func TestGetSuspectedTransfersLoadFailureDoesNotReadQueue(t *testing.T) {
	loadErr := errors.New("boom: cannot parse ledger")
	deps, fake := newFakeDeps(t, twoSuspectedPairs(), loadErr)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "get_suspected_transfers", map[string]any{}))
	if !strings.Contains(msg, "boom") {
		t.Errorf("error = %q, want it to surface the load failure", msg)
	}
	if fake.suspectedCalled {
		t.Error("SuspectedTransfers() was called despite the load failing; the queue must not be read")
	}
}

// B.5, and the whole point of this task: a pair_key taken from
// get_suspected_transfers' own output is accepted by resolve_transfer's
// FIRST (preview) call against the same fakes -- get_transfers previously
// could not produce a key resolve_transfer would accept at all.
func TestGetSuspectedTransfersPairKeyIsAcceptedByResolveTransferPreview(t *testing.T) {
	deps, _ := newFakeDeps(t, twoSuspectedPairs(), nil)
	cs := connect(t, deps)

	queue := decodeToolResult[getSuspectedTransfersOutput](t, call(t, cs, "get_suspected_transfers", map[string]any{}))
	if len(queue.Pairs) == 0 {
		t.Fatal("setup: get_suspected_transfers returned no pairs")
	}
	key := queue.Pairs[0].PairKey

	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	if out.Confirmed {
		t.Error("confirmed = true on the first (preview) call")
	}
	if out.ConfirmToken == "" {
		t.Error("resolve_transfer refused a pair_key taken straight from get_suspected_transfers' own output")
	}
	if out.PairKey != key {
		t.Errorf("resolve_transfer echoed pair_key %q, want %q", out.PairKey, key)
	}
}
