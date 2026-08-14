package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestUndoResolveRefusesAPairWithNoDecision(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))
	if !strings.Contains(msg, key) {
		t.Errorf("error does not name the key: %s", msg)
	}
}
