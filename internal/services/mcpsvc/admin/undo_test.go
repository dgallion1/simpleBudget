package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/services/dataloader"
)

// legacyPairKey mirrors dataloader's unexported pairKey: sorted pair,
// sha256, first 8 bytes hex. Kept independent of the package under test so a
// change to key derivation on one side would be caught here rather than the
// test silently tracking the implementation.
func legacyPairKey(a, b string) string {
	lo, hi := a, b
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:8])
}

// TestUndoResolveRestoresThePairToTheQueue also asserts a genuine .bak was
// produced -- not merely that SnapshotPaths is non-empty. Unlike
// resolve_duplicates on a fresh install, undo_resolve always runs AFTER a
// resolve, so duplicate_decisions.json always exists by the time undo_resolve
// runs and Ensure always takes the real-backup branch: accepting either
// branch here would leave that path unproven, the mistake Task 7 made once
// already.
func TestUndoResolveRestoresThePairToTheQueue(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	_ = decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))

	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	before, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatalf("read decisions before undo: %v", err)
	}
	if !strings.Contains(string(before), key) {
		t.Fatalf("test setup: resolved decision missing from file:\n%s", before)
	}

	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))

	if out.PairKey != key {
		t.Errorf("pair_key = %q, want %q", out.PairKey, key)
	}
	if out.PreviousOutcome != "kept_winner" {
		t.Errorf("previous_outcome = %q, want kept_winner", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1 -- the pair should be back in the queue", out.UnresolvedRemaining)
	}

	if len(out.SnapshotPaths) == 0 {
		t.Fatal("snapshot_paths is empty; a resolved decisions file existed and should have been backed up")
	}
	backup, err := os.ReadFile(out.SnapshotPaths[0])
	if err != nil {
		t.Fatalf("read snapshot %s: %v", out.SnapshotPaths[0], err)
	}
	if string(backup) != string(before) {
		t.Errorf("snapshot content does not match the decisions file's content just before the undo:\nsnapshot=%s\nbefore=%s", backup, before)
	}

	after := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if after.UnresolvedCount != 1 {
		t.Errorf("list_duplicates unresolved_count = %d after undo, want 1", after.UnresolvedCount)
	}
}

// TestUndoResolveOfKeptBothOnlyRequeuesNothingWasEverSuppressed covers the
// half of the outcome space TestUndoResolveRestoresThePairToTheQueue does
// not: kept_both never sets a suppressed hash (resolve.go's decision.
// SuppressedHash stays empty for kept_both, see resolve.go:124-146), so
// undoing it can only re-flag the pair for review -- there is no suppressed
// transaction to bring back. This is exactly the claim the tool's
// description now makes per-outcome; without this test that claim would be
// unchecked, same gap Task 7 and this task's Description text both fell
// into once already.
func TestUndoResolveOfKeptBothOnlyRequeuesNothingWasEverSuppressed(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	resolved := decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": key,
		"outcome":  "kept_both",
	}))
	if resolved.SuppressedHash != "" {
		t.Fatalf("test setup: kept_both should never suppress a hash, got %q", resolved.SuppressedHash)
	}

	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))

	if out.PreviousOutcome != "kept_both" {
		t.Errorf("previous_outcome = %q, want kept_both", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1 -- the pair should be back in the queue", out.UnresolvedRemaining)
	}
	if len(out.SnapshotPaths) == 0 {
		t.Error("snapshot_paths is empty; a resolved decisions file existed and should have been backed up")
	}

	after := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if after.UnresolvedCount != 1 {
		t.Errorf("list_duplicates unresolved_count = %d after undoing kept_both, want 1", after.UnresolvedCount)
	}
	// Both transactions must still be counted -- kept_both never suppressed
	// either side, so undoing it must not newly suppress one either.
	restored := after.Unresolved[0]
	if restored.Left.Hash == "" || restored.Right.Hash == "" {
		t.Errorf("re-flagged pair is missing a side: %+v", restored)
	}
}

// TestUndoResolveReachesALegacyKeyedDecision covers the defect this task
// fixes: a decision recorded before StableID existed is filed on disk under
// the pair's old content-hash key, while list_duplicates (and every other
// caller) hands back the current StableID-derived key. undo_resolve must
// still reach it. The count assertion on list_duplicates precedes the
// index-0 probe below so it cannot pass vacuously (package convention).
func TestUndoResolveReachesALegacyKeyedDecision(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	currentKey, kept, suppressed := pendingPairKey(t, cs)

	legacyKey := legacyPairKey(kept, suppressed)
	if legacyKey == currentKey {
		t.Fatal("fixture is degenerate: the legacy key equals the current key")
	}

	doc := struct {
		Decisions map[string]dataloader.DuplicateDecision `json:"decisions"`
	}{Decisions: map[string]dataloader.DuplicateDecision{
		legacyKey: {
			KeptHash:       kept,
			SuppressedHash: suppressed,
			Outcome:        dataloader.DuplicateOutcomeKeptWinner,
			DecidedAt:      time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	if err := os.WriteFile(decisionsPath, data, 0o644); err != nil {
		t.Fatalf("write decisions file: %v", err)
	}

	listed := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if listed.ResolvedCount != 1 {
		t.Fatalf("resolved_count = %d, want 1 (the legacy-keyed decision must resolve the pair)", listed.ResolvedCount)
	}
	if listed.Resolved[0].PairKey != currentKey {
		t.Fatalf("resolved pair_key = %q, want the current key %q", listed.Resolved[0].PairKey, currentKey)
	}

	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": currentKey}))
	if out.PreviousOutcome != "kept_winner" {
		t.Errorf("previous_outcome = %q, want kept_winner (must come from the aliased entry)", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1", out.UnresolvedRemaining)
	}

	after, err := os.ReadFile(decisionsPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read decisions after undo: %v", err)
	}
	if strings.Contains(string(after), legacyKey) {
		t.Errorf("legacy key %q survived the undo; it would resurrect the decision on the next load", legacyKey)
	}
	if strings.Contains(string(after), currentKey) {
		t.Errorf("current key %q present after undo; nothing should be filed under it", currentKey)
	}

	relisted := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if relisted.UnresolvedCount != 1 {
		t.Errorf("unresolved_count = %d after undo, want 1", relisted.UnresolvedCount)
	}
	if relisted.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d after undo, want 0", relisted.ResolvedCount)
	}
}

// TestUndoResolveReachesALegacyKeyedKeptBothDecision is the kept_both
// variant of TestUndoResolveReachesALegacyKeyedDecision above: the decision
// is filed ONLY under the legacy key (kept_both never suppresses a hash --
// see TestUndoResolveOfKeptBothOnlyRequeuesNothingWasEverSuppressed -- so
// KeptHash/SuppressedHash are left empty here too), and undo_resolve called
// with the current key must still reach it, report kept_both, and leave
// neither key form on disk afterward.
func TestUndoResolveReachesALegacyKeyedKeptBothDecision(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	currentKey, kept, suppressed := pendingPairKey(t, cs)

	legacyKey := legacyPairKey(kept, suppressed)
	if legacyKey == currentKey {
		t.Fatal("fixture is degenerate: the legacy key equals the current key")
	}

	doc := struct {
		Decisions map[string]dataloader.DuplicateDecision `json:"decisions"`
	}{Decisions: map[string]dataloader.DuplicateDecision{
		legacyKey: {
			Outcome:   dataloader.DuplicateOutcomeKeptBoth,
			DecidedAt: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	if err := os.WriteFile(decisionsPath, data, 0o644); err != nil {
		t.Fatalf("write decisions file: %v", err)
	}

	listed := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if listed.KeptBothCount != 1 {
		t.Fatalf("kept_both_count = %d, want 1 (the legacy-keyed kept_both decision must resolve the pair)", listed.KeptBothCount)
	}
	if listed.KeptBoth[0].PairKey != currentKey {
		t.Fatalf("kept_both pair_key = %q, want the current key %q", listed.KeptBoth[0].PairKey, currentKey)
	}

	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": currentKey}))
	if out.PreviousOutcome != "kept_both" {
		t.Errorf("previous_outcome = %q, want kept_both (must come from the aliased entry)", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1", out.UnresolvedRemaining)
	}

	after, err := os.ReadFile(decisionsPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read decisions after undo: %v", err)
	}
	if strings.Contains(string(after), legacyKey) {
		t.Errorf("legacy key %q survived the undo; it would resurrect the decision on the next load", legacyKey)
	}
	if strings.Contains(string(after), currentKey) {
		t.Errorf("current key %q present after undo; nothing should be filed under it", currentKey)
	}

	relisted := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if relisted.UnresolvedCount != 1 {
		t.Errorf("unresolved_count = %d after undo, want 1", relisted.UnresolvedCount)
	}
	if relisted.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d after undo, want 0", relisted.ResolvedCount)
	}
}

func TestUndoResolveRefusesAPairWithNoDecision(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))
	if !strings.Contains(msg, key) {
		t.Errorf("error does not name the key: %s", msg)
	}
}
