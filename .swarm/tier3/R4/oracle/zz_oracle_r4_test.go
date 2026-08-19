package confirm

// Tier-3 acceptance oracle for R4 (approval bound to the exact operation).
// Lead-authored before dispatch; copied into the package by accept.sh and
// removed afterwards. Both blind implementations are judged against THIS
// file, so neither worker may edit it.
//
// Required API (pinned in .swarm/briefs/R4.md):
//
//	func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error)
//	func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool)
//
// opID is the operation identity — at the four call sites it is the confirm
// token, which is already bound to the exact arguments. Create replaces an
// existing pending only when all three of tool, subject and opID match.

import (
	"context"
	"testing"
	"time"
)

// Check 1 — the defect itself. Two operations differing only in their
// arguments must not share or replace one another's approval.
func TestZZOracleR4_SameSubjectDifferentOperationsCoexist(t *testing.T) {
	a := NewApprovals(time.Minute)

	p1, err := a.Create("set_balance_anchor", "chk", "tok-500", "Set anchor to 500?", "amount 500.00")
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	p2, err := a.Create("set_balance_anchor", "chk", "tok-5000", "Set anchor to 5000?", "amount 5000.00")
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	if p1.ID == p2.ID {
		t.Fatal("two different operations were given the same pending ID")
	}

	if _, ok := a.Get(p1.ID); !ok {
		t.Fatal("the first approval was discarded when a second operation on the same subject was filed")
	}
	if _, ok := a.Get(p2.ID); !ok {
		t.Fatal("the second approval is not open")
	}

	f1, ok := a.Find("set_balance_anchor", "chk", "tok-500")
	if !ok || f1.ID != p1.ID {
		t.Fatalf("Find(tok-500) = %v ok=%v, want the first pending", f1, ok)
	}
	f2, ok := a.Find("set_balance_anchor", "chk", "tok-5000")
	if !ok || f2.ID != p2.ID {
		t.Fatalf("Find(tok-5000) = %v ok=%v, want the second pending", f2, ok)
	}
}

// Check 2 — answering one operation must not authorize the other.
func TestZZOracleR4_DecidingOneDoesNotAuthorizeTheOther(t *testing.T) {
	a := NewApprovals(time.Minute)
	p1, _ := a.Create("set_balance_anchor", "chk", "tok-500", "500?", "amount 500.00")
	p2, _ := a.Create("set_balance_anchor", "chk", "tok-5000", "5000?", "amount 5000.00")

	if err := a.Decide(p1.ID, Approved); err != nil {
		t.Fatalf("Decide p1: %v", err)
	}

	if d, err := a.Await(context.Background(), p1); d != Approved || err != nil {
		t.Fatalf("Await p1 = (%v, %v), want (Approved, nil)", d, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	d, err := a.Await(ctx, p2)
	if d == Approved {
		t.Fatal("approving the 500 operation also authorized the 5000 operation")
	}
	if err == nil {
		t.Fatal("Await on an unanswered approval returned without an error")
	}
}

// Check 3 — opposite verdicts on one transfer pair are distinct operations.
func TestZZOracleR4_OppositeVerdictsAreDistinctOperations(t *testing.T) {
	a := NewApprovals(time.Minute)
	confirmP, _ := a.Create("resolve_transfer", "pair-7", "tok-confirm", "Confirm pair-7?", "verdict confirm")
	rejectP, _ := a.Create("resolve_transfer", "pair-7", "tok-reject", "Reject pair-7?", "verdict reject")

	if confirmP.ID == rejectP.ID {
		t.Fatal("confirm and reject on one pair share a pending record")
	}
	if err := a.Decide(rejectP.ID, Approved); err != nil {
		t.Fatalf("Decide reject: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if d, _ := a.Await(ctx, confirmP); d == Approved {
		t.Fatal("approving the reject operation also authorized the confirm operation")
	}
}

// Check 4 — the pre-existing contract still holds: re-filing the SAME
// operation replaces the stale request, so a human cannot answer the wrong one.
func TestZZOracleR4_RefilingTheSameOperationReplacesIt(t *testing.T) {
	a := NewApprovals(time.Minute)
	first, _ := a.Create("restore_backup", "2026-08-01.zip", "tok-a", "Restore?", "detail")
	second, err := a.Create("restore_backup", "2026-08-01.zip", "tok-a", "Restore?", "detail")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("re-filing produced the same ID; a fresh unguessable ID is expected")
	}
	if _, ok := a.Get(first.ID); ok {
		t.Fatal("the stale request for the same operation is still open")
	}
	got, ok := a.Find("restore_backup", "2026-08-01.zip", "tok-a")
	if !ok || got.ID != second.ID {
		t.Fatal("Find did not return the current request for this operation")
	}
}

// Check 5 — a subject-only lookup must not match. This is what makes the
// binding load-bearing rather than cosmetic.
func TestZZOracleR4_WrongOperationIDDoesNotMatch(t *testing.T) {
	a := NewApprovals(time.Minute)
	if _, err := a.Create("set_balance_anchor", "chk", "tok-500", "500?", "detail"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := a.Find("set_balance_anchor", "chk", ""); ok {
		t.Fatal("an empty operation id matched a bound approval")
	}
	if _, ok := a.Find("set_balance_anchor", "chk", "tok-other"); ok {
		t.Fatal("a different operation id matched")
	}
}
