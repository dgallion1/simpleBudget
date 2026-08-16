package admin

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// shutdownDeps returns Deps wired with a RECORDING shutdown func. Never wire
// the real os.Exit here: it would kill the test binary.
func shutdownDeps(t *testing.T) (Deps, *atomic.Int32) {
	t.Helper()
	deps, _ := newLiveDeps(t)
	var calls atomic.Int32
	deps.Shutdown = func() { calls.Add(1) }
	deps.Confirm = confirm.NewRegistry(time.Minute)
	return deps, &calls
}

// assertNoShutdown fails if the shutdown func fires at all. The tool schedules
// the real exit with time.AfterFunc, so a broken guard does not call it
// synchronously -- it calls it shutdownExitDelay later. Asserting immediately
// would pass against exactly the regression this test exists to catch, so wait
// past the delay first.
func assertNoShutdown(t *testing.T, calls *atomic.Int32, what string) {
	t.Helper()
	time.Sleep(shutdownExitDelay + 150*time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("shutdown func called %d times %s, want 0", got, what)
	}
}

// The first call is the whole guard: if it shuts down, the two-step protocol
// is decorative.
func TestShutdownFirstCallDoesNotShutDown(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	if out.Confirmed {
		t.Error("confirmed = true on the first call")
	}
	if out.ConfirmToken == "" {
		t.Error("no confirm_token returned, so the operation can never be confirmed")
	}
	if out.WhatWouldHappen == "" {
		t.Error("no what_would_happen returned; the user has nothing to agree to")
	}
	assertNoShutdown(t, calls, "on the preview call")
}

func TestShutdownSecondCallWithTheTokenShutsDown(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	second := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))
	if !second.Confirmed {
		t.Error("confirmed = false after redeeming a valid token")
	}
	// The exit is deferred so the response lands first; wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times after confirmation, want 1", got)
	}
}

func TestShutdownRefusesABadToken(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "shutdown_server", map[string]any{"confirm_token": "not-a-real-token"}))
	if msg == "" {
		t.Error("no error message explaining the refusal")
	}
	assertNoShutdown(t, calls, "with a bad token")
}

func TestShutdownRefusesAReplayedToken(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	call(t, cs, "shutdown_server", map[string]any{"confirm_token": first.ConfirmToken})
	res := call(t, cs, "shutdown_server", map[string]any{"confirm_token": first.ConfirmToken})
	if !res.IsError {
		t.Fatal("a replayed token was accepted")
	}
	// Wait past the replay attempt's own would-be AfterFunc delay, not just
	// until the legitimate redeem's call lands: polling-until-first-increment
	// breaks as soon as the legitimate exit fires, which can read the count
	// before a wrongly-accepted replay's own delayed call would have (same
	// shape as the bug fixed in 582aebd).
	time.Sleep(shutdownExitDelay + 150*time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times, want exactly 1 (the replay must not shut down a second time)", got)
	}
}

// A nil Shutdown must fail this one call with a named error, matching how
// every other admin dependency behaves. It must NOT panic.
func TestShutdownWithoutAShutdownFuncReportsIt(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Confirm = confirm.NewRegistry(time.Minute)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "shutdown_server", map[string]any{}))
	if !strings.Contains(msg, "shutdown path") {
		t.Errorf("error message = %q, want it to name the missing shutdown path", msg)
	}
}

// A nil Confirm must fail this one call with a named error too -- the guard
// itself is unusable without a confirmation registry, matching the nil
// Shutdown case above. It must NOT panic.
func TestShutdownWithoutAConfirmRegistryReportsIt(t *testing.T) {
	deps, _ := shutdownDeps(t)
	deps.Confirm = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "shutdown_server", map[string]any{}))
	if !strings.Contains(msg, "confirmation registry") {
		t.Errorf("error message = %q, want it to name the missing confirmation registry", msg)
	}
}

// The same consent path restore_backup uses. Two guarded tools with different
// consent semantics would be worse than either choice made consistently: a
// reader could not tell which one asks.
func TestShutdownDoesNotRunWhenTheUserRefuses(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connectAsking(t, deps, &mcp.ElicitResult{Action: "decline"}, "stops answering")

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))

	if out.Confirmed {
		t.Error("confirmed = true after the user refused")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}
	assertNoShutdown(t, calls, "after the user refused")
}

func TestShutdownRunsWhenTheUserApproves(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connectAsking(t, deps, approve(), "stops answering")

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("confirmed = false after the user approved (note: %q)", out.Note)
	}
	if out.HumanApproval != "approved" {
		t.Errorf("human_approval = %q, want approved", out.HumanApproval)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times after approval, want 1", got)
	}
}

func TestShutdownOnAClientThatCannotPromptSaysNobodyWasAsked(t *testing.T) {
	deps, _ := shutdownDeps(t)
	cs := connect(t, deps) // no elicitation handler

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))

	if !out.Confirmed {
		t.Fatalf("the shutdown did not run on a client without elicitation (note: %q)", out.Note)
	}
	if out.HumanApproval != "not asked" {
		t.Errorf("human_approval = %q, want \"not asked\"", out.HumanApproval)
	}
	if !strings.Contains(out.Note, "NO HUMAN WAS ASKED") {
		t.Errorf("note = %q, want it to admit nobody was asked", out.Note)
	}
}
