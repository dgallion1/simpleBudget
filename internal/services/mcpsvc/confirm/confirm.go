// Package confirm issues and redeems single-use confirmation tokens for
// destructive MCP tools.
//
// A guarded tool's first call performs nothing, returns a preview, and mints
// a token; a second call must echo that token to proceed. The token is bound
// to the tool name and to a hash of the arguments, is single-use, and expires.
//
// What this buys, stated plainly: deliberateness, not consent. A model can
// mint and redeem a token inside one turn without a human ever seeing the
// preview. It raises the bar from "a stray tool call does the thing" to "the
// model must decide twice with a preview in between". It is NOT a substitute
// for a human clicking a button.
//
// It lives below the tool subpackages rather than in mcpsvc, following the
// precedent internal/services/mcpsvc/snapshot set: mcpsvc imports its
// subpackages, so a shared type declared there and used in a subpackage's
// Deps would be an import cycle.
package confirm

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrBadToken is returned by Redeem for every rejection -- unknown, expired,
// replayed, wrong tool, wrong arguments. One identity on purpose: the caller
// tells the model to start over, and distinguishing "expired" from "forged"
// only helps a caller trying to work around the guard.
var ErrBadToken = errors.New("confirmation token is not valid for this call; call the tool again without a token to get a fresh preview and token")

type entry struct {
	tool     string
	argsHash string
	expires  time.Time
}

// Registry holds outstanding tokens in memory. A restart drops every token,
// which is a legitimate way to invalidate all of them.
type Registry struct {
	mu  sync.Mutex
	m   map[string]entry
	ttl time.Duration
	now func() time.Time // injectable so expiry tests need not sleep
}

// NewRegistry returns a Registry whose tokens live for ttl.
func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{m: make(map[string]entry), ttl: ttl, now: time.Now}
}

// hashArgs renders args as canonical JSON and hashes it. Marshaling a struct
// is field-order deterministic, and a map's keys are sorted by encoding/json,
// so the same arguments always produce the same hash.
func hashArgs(args any) (string, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("cannot hash confirmation arguments: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Mint issues a token bound to tool and args. The returned expiry is what the
// tool reports to the model so it knows how long it has.
func (r *Registry) Mint(tool string, args any) (string, time.Time, error) {
	h, err := hashArgs(args)
	if err != nil {
		return "", time.Time{}, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("cannot generate a confirmation token: %w", err)
	}
	token := hex.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	expires := r.now().Add(r.ttl)
	r.m[token] = entry{tool: tool, argsHash: h, expires: expires}
	return token, expires, nil
}

// Redeem consumes token for tool/args. A successful redeem deletes the token,
// so a replay is refused. Every failure returns ErrBadToken.
func (r *Registry) Redeem(token, tool string, args any) error {
	h, err := hashArgs(args)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()

	// Find by constant-time compare rather than a map lookup on attacker-
	// supplied input. The threat model here is thin, but a token comparison
	// that is not constant-time is a finding waiting to be filed.
	var found string
	var e entry
	for k, v := range r.m {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 {
			found, e = k, v
			break
		}
	}
	if found == "" {
		return ErrBadToken
	}
	// Consume before validating the rest: a token presented for the wrong
	// tool or arguments is spent, not retryable.
	delete(r.m, found)

	if e.tool != tool || e.argsHash != h || !r.now().Before(e.expires) {
		return ErrBadToken
	}
	return nil
}

// sweepLocked drops expired entries. Callers hold r.mu.
func (r *Registry) sweepLocked() {
	now := r.now()
	for k, v := range r.m {
		if !now.Before(v.expires) {
			delete(r.m, k)
		}
	}
}
