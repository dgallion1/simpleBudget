package confirm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Errors from the out-of-band approval flow. ErrNoSuchApproval covers unknown,
// expired and already-decided alike, for the same reason ErrBadToken does:
// distinguishing them only helps someone poking at IDs.
var (
	ErrNoSuchApproval  = errors.New("that approval request is not open (it may have expired or been answered already)")
	ErrApprovalTimeout = errors.New("nobody answered the approval request in time")
)

// Pending is one approval waiting for a human to answer it in a browser.
type Pending struct {
	ID string
	// Tool, Subject and OpID identify what is being approved. The triple is
	// how a tool re-finds its own request after the round-trip, which is why
	// nothing here is ever looked up by a client-supplied identifier. OpID is
	// the operation identity -- in practice the confirm token, which is
	// already bound to the exact arguments a human is shown -- so two
	// operations that merely share a tool and subject (a different amount, a
	// different verdict) can never collide or replace one another.
	Tool    string
	Subject string
	OpID    string
	Title   string
	Detail  string
	Expires time.Time

	// answered/decision are the human's answer once it exists. The record
	// OUTLIVES the answer on purpose: the client can return from showing the
	// URL before the person has clicked, or long after, so the tool may look
	// its request up either side of the decision. Deleting on Decide would
	// silently drop an approval the user really gave.
	answered bool
	decision Decision
	decided  chan Decision
}

// Approvals holds approval requests that a human answers out of band -- on the
// app's own page in their browser, rather than in whatever UI the model is
// driving.
//
// This is the strongest consent this codebase can obtain: the person reads the
// consequences in the application they already trust and clicks a button
// there. A form elicitation is answered inside the model's client; this is
// not.
type Approvals struct {
	mu  sync.Mutex
	m   map[string]*Pending
	ttl time.Duration
	now func() time.Time // injectable so expiry tests need not sleep
}

// NewApprovals returns a registry whose requests live for ttl.
func NewApprovals(ttl time.Duration) *Approvals {
	return &Approvals{m: make(map[string]*Pending), ttl: ttl, now: time.Now}
}

// Create opens an approval request and returns its ID, which is the
// unguessable part of the URL a human visits to answer it.
//
// tool and subject identify the operation (subject is the archive name for a
// restore, empty where the tool has no argument); opID identifies the exact
// operation -- in practice the confirm token, already bound to the arguments
// a human is shown -- so a different amount, date or verdict on the same
// subject never collides with an unrelated request. Creating a second
// request for the same tool, subject AND opID replaces the first: the
// earlier one is stale by definition (it is a re-file of the identical
// operation), and leaving both would let a human answer the wrong one. A
// second request that differs in opID is a DIFFERENT operation and coexists.
func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("cannot generate an approval id: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked()

	for id, p := range a.m {
		if p.Tool == tool && p.Subject == subject && p.OpID == opID {
			delete(a.m, id)
		}
	}

	p := &Pending{
		ID:      hex.EncodeToString(buf),
		Tool:    tool,
		Subject: subject,
		OpID:    opID,
		Title:   title,
		Detail:  detail,
		Expires: a.now().Add(a.ttl),
		decided: make(chan Decision, 1),
	}
	a.m[p.ID] = p
	return p, nil
}

// Get returns the request with this ID if it is still OPEN, for rendering the
// page a human answers. An already-answered request is not open, so a back
// button cannot offer a second answer.
func (a *Approvals) Get(id string) (*Pending, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked()
	p, ok := a.m[id]
	if !ok || p.answered {
		return nil, false
	}
	return p, true
}

// Find returns the request for tool, subject and opID, answered or not. A
// guarded tool uses this to re-find its own request when the client
// re-invokes it, so the flow never depends on an identifier round-tripped
// through the client -- and it must see requests the human has already
// answered, since that race is the common case when they click quickly.
// opID must match exactly: it is what stops a lookup for one operation
// (e.g. anchor the account at 500.00) from returning the pending record for
// a different one that merely shares the tool and subject (anchor it at
// 5000.00, or the opposite verdict on the same transfer pair).
func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked()
	for _, p := range a.m {
		if p.Tool == tool && p.Subject == subject && p.OpID == opID {
			return p, true
		}
	}
	return nil, false
}

// Decide records a human's answer. It is called from the HTTP handler serving
// the approval page. A request can be decided once; a second attempt reports
// ErrNoSuchApproval, so a double-submitted form cannot flip an answer.
func (a *Approvals) Decide(id string, d Decision) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked()

	p, ok := a.m[id]
	if !ok || p.answered {
		return ErrNoSuchApproval
	}
	p.answered = true
	p.decision = d
	// Buffered and written exactly once, so this never blocks even if the
	// waiting tool call has already given up and gone away.
	p.decided <- d
	return nil
}

// Await blocks until p is decided, p expires, or ctx ends. A timeout is
// reported as Refused with ErrApprovalTimeout: a destructive operation that
// nobody answered must not proceed.
func (a *Approvals) Await(ctx context.Context, p *Pending) (Decision, error) {
	if p == nil {
		return Refused, ErrNoSuchApproval
	}
	// Already answered -- the human beat the round-trip back. Return their
	// answer rather than waiting for a decision that has already happened.
	a.mu.Lock()
	if p.answered {
		d := p.decision
		a.mu.Unlock()
		a.forget(p.ID)
		return d, nil
	}
	a.mu.Unlock()

	timeout := time.Until(p.Expires)
	if timeout <= 0 {
		return Refused, ErrApprovalTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case d := <-p.decided:
		a.forget(p.ID)
		return d, nil
	case <-timer.C:
		a.forget(p.ID)
		return Refused, ErrApprovalTimeout
	case <-ctx.Done():
		a.forget(p.ID)
		return Refused, ctx.Err()
	}
}

// forget drops a request that will never be answered, so an abandoned page
// cannot decide an operation whose caller has already given up.
func (a *Approvals) forget(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.m, id)
}

// sweepLocked drops expired requests. Callers hold a.mu.
func (a *Approvals) sweepLocked() {
	now := a.now()
	for id, p := range a.m {
		if !now.Before(p.Expires) {
			delete(a.m, id)
		}
	}
}
