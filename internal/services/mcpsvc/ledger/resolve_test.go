package ledger

import (
	"strings"
	"testing"

	"budget2/internal/services/transfers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedSuspectedPair writes a CSV pair that lands in the suspected review
// queue (equal-amount, cross-account, no transfer pattern). Returns the
// pair_key the queue reports.
func seedSuspectedPair(t *testing.T, deps Deps, dir string) string {
	t.Helper()
	seedAccounts(t, deps, twoAccounts())
	// Two cross-account rows of equal amount, no pattern hit -> suspected.
	writeCSV(t, dir, "checking.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-10,MYSTERY OUTFLOW,-777.77,\n")
	writeCSV(t, dir, "schwab.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-10,MYSTERY INFLOW,777.77,\n")
	// Load so the suspected queue is populated.
	if _, err := deps.Transactions.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	suspected := deps.Transfers.SuspectedTransfers()
	if len(suspected) == 0 {
		t.Fatal("no suspected pairs after load; the fixture did not land in the queue")
	}
	return suspected[0].PairKey
}

// The first call MUST NOT write.
func TestResolveTransferFirstCallDoesNotWrite(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	if out.Confirmed {
		t.Error("confirmed = true on the first (preview) call")
	}
	if out.ConfirmToken == "" {
		t.Error("no confirm_token returned")
	}
	if out.WhatWouldHappen == "" {
		t.Error("no what_would_happen returned")
	}

	// The queue still has the pair (nothing resolved).
	suspected := deps.Transfers.SuspectedTransfers()
	if len(suspected) == 0 {
		t.Error("suspected queue is empty after the preview; the preview should not resolve anything")
	}
}

// The second call WITH the token writes the decision.
func TestResolveTransferSecondCallWithTokenWrites(t *testing.T) {
	deps, dir := newDeps(t)
	// Pre-create transfer_decisions.json so a .bak is taken (mirrors the
	// admin resolve_duplicates "first change of a session with prior data"
	// path). Without a prior file, no .bak is taken and snapshot_paths is
	// empty -- which is correct, not a bug.
	writeCSV(t, dir, "transfer_decisions.json", `{"decisions": {}}`)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	}))
	if !out.Confirmed {
		t.Fatal("confirmed = false after redeeming a valid token")
	}
	if out.Verdict != "confirm" {
		t.Errorf("verdict = %q, want confirm", out.Verdict)
	}
	if len(out.SnapshotPaths) == 0 {
		t.Error("no snapshot_paths; a .bak should have been taken for the pre-existing transfer_decisions.json")
	}
}

// A bad token MUST be refused and nothing is written.
func TestResolveTransferRefusesABadToken(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": "not-a-real-token",
	}))
	if msg == "" {
		t.Error("no error message explaining the refusal")
	}

	// The queue still has the pair.
	suspected := deps.Transfers.SuspectedTransfers()
	if len(suspected) == 0 {
		t.Error("suspected queue is empty after a bad-token refusal")
	}
}

// A replayed token MUST be refused.
func TestResolveTransferRefusesAReplayedToken(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	// First redeem succeeds.
	call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	})
	// Replay must fail.
	res := call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	})
	if !res.IsError {
		t.Fatal("a replayed token was accepted")
	}
}

// A token is bound to its verdict: changing verdict is refused.
func TestResolveTransferRefusesATokenForDifferentVerdict(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	res := call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "reject", // different verdict
		"confirm_token": first.ConfirmToken,
	})
	if !res.IsError {
		t.Fatal("a token minted for confirm was accepted for reject")
	}
}

// An unknown pair_key is refused before minting.
func TestResolveTransferRejectsUnknownPairKey(t *testing.T) {
	deps, dir := newDeps(t)
	seedSuspectedPair(t, deps, dir) // populate the queue
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": "not-a-real-key",
		"verdict":  "confirm",
	}))
	if !strings.Contains(msg, "not a suspected transfer") {
		t.Errorf("error = %q, want it to name the unknown pair", msg)
	}
}

// An unknown verdict is refused before minting.
func TestResolveTransferRejectsUnknownVerdict(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "maybe",
	}))
	if !strings.Contains(msg, "not recognized") {
		t.Errorf("error = %q, want it to name the unknown verdict", msg)
	}
	// Ensure the verdict constants are what the tool expects.
	_ = transfers.VerdictConfirm
}

// A nil Confirm registry makes the tool refuse rather than run unguarded.
func TestResolveTransferWithoutConfirmRegistryReportsIt(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	deps.Confirm = nil
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	if !strings.Contains(msg, "confirmation registry") {
		t.Errorf("error = %q, want it to name the missing confirmation registry", msg)
	}
}

// The human-approval rung: a refusal leaves the queue untouched.
func TestResolveTransferDoesNotWriteWhenTheUserRefuses(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connectAsking(t, deps, &mcp.ElicitResult{Action: "decline"}, "CONFIRMED")

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "confirm",
		"confirm_token": first.ConfirmToken,
	}))
	if out.Confirmed {
		t.Error("confirmed = true after the user refused")
	}
	if out.HumanApproval != "refused" {
		t.Errorf("human_approval = %q, want refused", out.HumanApproval)
	}
}

// On a client that cannot prompt, the token alone authorizes the write and
// the answer admits it.
func TestResolveTransferOnAClientThatCannotPromptSaysNobodyWasAsked(t *testing.T) {
	deps, dir := newDeps(t)
	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps) // no elicitation handler

	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "reject",
	}))
	out := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key":      key,
		"verdict":       "reject",
		"confirm_token": first.ConfirmToken,
	}))
	if !out.Confirmed {
		t.Fatalf("the write did not run on a client without elicitation (note: %q)", out.Note)
	}
	if out.HumanApproval != "not asked" {
		t.Errorf("human_approval = %q, want \"not asked\"", out.HumanApproval)
	}
	if !strings.Contains(out.Note, "NO HUMAN WAS ASKED") {
		t.Errorf("note = %q, want it to admit nobody was asked", out.Note)
	}
}
