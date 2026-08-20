package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/transfers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedSuspectedPair writes a CSV pair that lands in the suspected review
// queue (equal-amount, cross-account, no transfer pattern). Returns the
// pair_key the queue reports.
//
// It does NOT load through deps: resolve_transfer must trigger its own load
// to see the pair, so deps' suspected queue is left empty here on purpose. A
// throwaway DataLoader over the same directory and store computes the same
// deterministic pair_key from the CSV content, purely so the test knows what
// key to hand the tool.
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

	peek := dataloader.New(dir, deps.Store)
	if _, err := peek.LoadData(); err != nil {
		t.Fatalf("LoadData (peek, not deps): %v", err)
	}
	suspected := peek.SuspectedTransfers()
	if len(suspected) == 0 {
		t.Fatal("no suspected pairs after load; the fixture did not land in the queue")
	}
	return suspected[0].PairKey
}

// readTransferDecision reads the verdict actually PERSISTED to
// transfer_decisions.json for pairKey, independent of the tool's own output.
// resolveTransferOutput.Verdict is built in applyResolveTransfer as a bare
// echo of the verdict string the caller passed in, so asserting against it
// proves nothing about what was written to disk -- a test that only checks
// out.Verdict would still pass even if the write persisted a different
// verdict than the one the approval described. This reads the decision back
// from disk through a throwaway DataLoader, the same way seedSuspectedPair
// peeks the suspected queue.
func readTransferDecision(t *testing.T, deps Deps, dir, pairKey string) transfers.Decision {
	t.Helper()
	peek := dataloader.New(dir, deps.Store)
	decisions, err := peek.LoadTransferDecisions()
	if err != nil {
		t.Fatalf("LoadTransferDecisions: %v", err)
	}
	d, ok := decisions[pairKey]
	if !ok {
		t.Fatalf("no persisted decision for pair_key %q in transfer_decisions.json", pairKey)
	}
	return d
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
	if len(out.SnapshotPaths) == 0 {
		t.Error("no snapshot_paths; a .bak should have been taken for the pre-existing transfer_decisions.json")
	}

	// out.Verdict proves what the tool REPORTED back to the caller. It is
	// built as an echo of the request in applyResolveTransfer, but a bug
	// that reports one verdict while persisting another must still fail
	// here even when the write itself is correct.
	if out.Verdict != "confirm" {
		t.Errorf("reported out.Verdict = %q, want confirm", out.Verdict)
	}

	// The persisted decision proves what actually got WRITTEN to
	// transfer_decisions.json, independent of out.Verdict.
	decision := readTransferDecision(t, deps, dir, key)
	if string(decision.Verdict) != "confirm" {
		t.Errorf("persisted verdict = %q, want confirm", decision.Verdict)
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

// The write path (applyResolveTransfer) must re-validate against a fresh
// load, not just rely on the check that ran when the token was minted:
// the approval round trip means time passes between mint and redeem, and
// the underlying CSVs can change in between so the pair is no longer
// suspected. This test drives applyResolveTransfer directly -- bypassing
// the registered handler entirely -- so the top-of-handler pre-mint check
// at resolve.go:95 cannot be what refuses the write; only the
// re-validation inside applyResolveTransfer (resolve.go:208-220) can.
func TestResolveTransferWritePathRevalidatesBeforeWriting(t *testing.T) {
	deps, dir := newDeps(t)
	decisionsPath := filepath.Join(dir, transferDecisionsFile)
	const before = `{"decisions": {}}`
	writeCSV(t, dir, transferDecisionsFile, before)

	key := seedSuspectedPair(t, deps, dir)
	cs := connect(t, deps)

	// Mint a token for the pair while it is still suspected.
	first := decodeToolResult[resolveTransferOutput](t, call(t, cs, "resolve_transfer", map[string]any{
		"pair_key": key,
		"verdict":  "confirm",
	}))
	if first.ConfirmToken == "" {
		t.Fatal("no confirm_token minted")
	}

	// The underlying data changes so the pair is no longer suspected: drop
	// the checking-side leg so the equal-amount, cross-account pair no
	// longer exists.
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n")

	// Drive the write path directly. This skips the handler's own
	// top-of-function pre-mint check (resolve.go:84-99) entirely, so only
	// applyResolveTransfer's own re-validation before writing can catch a
	// pair that stopped being suspected between mint and redeem.
	_, out, err := applyResolveTransfer(deps, key, transfers.VerdictConfirm,
		resolveTokenArgs{PairKey: key, Verdict: "confirm"}, first.ConfirmToken, confirm.NotAsked)
	if err == nil {
		t.Fatal("applyResolveTransfer did not refuse a pair that is no longer suspected")
	}
	if !strings.Contains(err.Error(), "no longer a suspected transfer") {
		t.Errorf("error = %q, want it to say the pair is no longer suspected", err)
	}
	if out.Confirmed {
		t.Error("confirmed = true for a pair that is no longer suspected")
	}

	after, readErr := os.ReadFile(decisionsPath)
	if readErr != nil {
		t.Fatalf("read %s after the refused write: %v", decisionsPath, readErr)
	}
	if string(after) != before {
		t.Errorf("transfer_decisions.json changed after a refused write: got %q, want unchanged %q", after, before)
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
