package confirm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newApprovals(t *testing.T) *Approvals {
	t.Helper()
	return NewApprovals(time.Minute)
}

func TestApprovalRoundTrip(t *testing.T) {
	a := newApprovals(t)
	p, err := a.Create("restore_backup", "a.zip", "Restore a.zip?", "everything else goes")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == "" {
		t.Fatal("no id, so no URL to send anybody to")
	}

	// The page finds it by id; the tool finds it by what it is about.
	if got, ok := a.Get(p.ID); !ok || got.Title != "Restore a.zip?" {
		t.Fatalf("Get = %+v ok=%v", got, ok)
	}
	if got, ok := a.Find("restore_backup", "a.zip"); !ok || got.ID != p.ID {
		t.Fatalf("Find = %+v ok=%v, want the request just created", got, ok)
	}

	go func() {
		if err := a.Decide(p.ID, Approved); err != nil {
			t.Errorf("Decide: %v", err)
		}
	}()

	d, err := a.Await(context.Background(), p)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if d != Approved {
		t.Errorf("Decision = %v, want Approved", d)
	}
}

func TestApprovalCarriesARefusal(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	if err := a.Decide(p.ID, Refused); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	d, err := a.Await(context.Background(), p)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if d != Refused {
		t.Errorf("Decision = %v, want Refused", d)
	}
}

// One answer per request. A double-submitted form must not be able to flip a
// decision that has already been acted on.
func TestApprovalCanOnlyBeDecidedOnce(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	if err := a.Decide(p.ID, Approved); err != nil {
		t.Fatalf("first Decide: %v", err)
	}
	if err := a.Decide(p.ID, Refused); !errors.Is(err, ErrNoSuchApproval) {
		t.Errorf("second Decide = %v, want ErrNoSuchApproval", err)
	}
	if _, ok := a.Get(p.ID); ok {
		t.Error("a decided request is still open")
	}
}

func TestApprovalUnknownIDIsRefused(t *testing.T) {
	a := newApprovals(t)
	if err := a.Decide("not-a-real-id", Approved); !errors.Is(err, ErrNoSuchApproval) {
		t.Errorf("Decide = %v, want ErrNoSuchApproval", err)
	}
	if _, ok := a.Get("not-a-real-id"); ok {
		t.Error("Get invented a request")
	}
}

// Nobody answered. A destructive operation must not proceed on silence.
func TestApprovalTimesOutAsRefused(t *testing.T) {
	a := NewApprovals(40 * time.Millisecond)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")

	d, err := a.Await(context.Background(), p)
	if !errors.Is(err, ErrApprovalTimeout) {
		t.Fatalf("err = %v, want ErrApprovalTimeout", err)
	}
	if d != Refused {
		t.Errorf("Decision = %v, want Refused -- silence is not consent", d)
	}
	// And the abandoned request is gone, so a late click cannot decide an
	// operation whose caller has given up.
	if _, ok := a.Get(p.ID); ok {
		t.Error("a timed-out request is still open")
	}
}

func TestApprovalHonorsContextCancellation(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d, err := a.Await(ctx, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d != Refused {
		t.Errorf("Decision = %v, want Refused", d)
	}
}

// A superseded request must not linger: two open approvals for the same
// operation means a human can answer the stale one.
func TestCreateReplacesAnEarlierRequestForTheSameThing(t *testing.T) {
	a := newApprovals(t)
	first, _ := a.Create("restore_backup", "a.zip", "t", "d")
	second, _ := a.Create("restore_backup", "a.zip", "t", "d")

	if first.ID == second.ID {
		t.Fatal("the replacement reused the id")
	}
	if _, ok := a.Get(first.ID); ok {
		t.Error("the superseded request is still answerable")
	}
	if got, ok := a.Find("restore_backup", "a.zip"); !ok || got.ID != second.ID {
		t.Errorf("Find returned %+v, want the newest request", got)
	}
}

// Different operations coexist: approving a restore must not approve a
// shutdown, and one archive's approval must not answer another's.
func TestCreateKeepsDistinctOperationsApart(t *testing.T) {
	a := newApprovals(t)
	restoreA, _ := a.Create("restore_backup", "a.zip", "t", "d")
	restoreB, _ := a.Create("restore_backup", "b.zip", "t", "d")
	shutdown, _ := a.Create("shutdown_server", "", "t", "d")

	for _, id := range []string{restoreA.ID, restoreB.ID, shutdown.ID} {
		if _, ok := a.Get(id); !ok {
			t.Errorf("request %s was dropped", id)
		}
	}
	if got, _ := a.Find("restore_backup", "b.zip"); got.ID != restoreB.ID {
		t.Error("Find crossed two archives")
	}
	if got, _ := a.Find("shutdown_server", ""); got.ID != shutdown.ID {
		t.Error("Find crossed two tools")
	}
}

func TestExpiredApprovalsAreSweptAway(t *testing.T) {
	a := NewApprovals(time.Minute)
	now := time.Now()
	a.now = func() time.Time { return now }

	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	now = now.Add(2 * time.Minute)

	if _, ok := a.Get(p.ID); ok {
		t.Error("an expired request is still answerable")
	}
	if err := a.Decide(p.ID, Approved); !errors.Is(err, ErrNoSuchApproval) {
		t.Errorf("Decide on an expired request = %v, want ErrNoSuchApproval", err)
	}
}

// The common race, and the one that matters most: the client returns from
// showing the URL immediately, so the human can click BEFORE the tool comes
// back to wait. Their answer must not be dropped on the floor.
func TestAnAnswerGivenBeforeTheToolWaitsIsNotLost(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")

	// Answered first...
	if err := a.Decide(p.ID, Approved); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	// ...and only then does the tool re-find its request and wait on it.
	found, ok := a.Find("restore_backup", "a.zip")
	if !ok {
		t.Fatal("the tool cannot find its own request after the human answered it")
	}
	d, err := a.Await(context.Background(), found)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if d != Approved {
		t.Errorf("Decision = %v, want Approved -- the human said yes and it was lost", d)
	}
}

// The same race with a refusal: a fast "no" must not decay into a timeout
// that some future caller reads as merely unanswered.
func TestARefusalGivenBeforeTheToolWaitsIsNotLost(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	if err := a.Decide(p.ID, Refused); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	found, _ := a.Find("restore_backup", "a.zip")
	d, err := a.Await(context.Background(), found)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if d != Refused {
		t.Errorf("Decision = %v, want Refused", d)
	}
}

// Once consumed, the request is gone: a second Await must not replay an old
// approval into a new operation.
func TestAnAnswerIsConsumedOnce(t *testing.T) {
	a := newApprovals(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")
	if err := a.Decide(p.ID, Approved); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if _, err := a.Await(context.Background(), p); err != nil {
		t.Fatalf("first Await: %v", err)
	}
	if _, ok := a.Find("restore_backup", "a.zip"); ok {
		t.Error("the consumed request is still findable; a later call could replay it")
	}
}
