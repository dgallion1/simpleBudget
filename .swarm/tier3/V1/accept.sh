#!/usr/bin/env bash
# Oracle for V1 — undo_resolve must reach a decision filed under a legacy
# pair key when the caller supplies the pair's current key.
# Run with cwd set to the tree under test.
#
# Plants its own end-to-end test in internal/services/mcpsvc/admin and
# removes it afterwards. The test drives the real MCP server over a real
# DataLoader and asserts only on observable consumer output (list_duplicates,
# undo_resolve, the decisions file on disk) — no new API is referenced, so
# any correct implementation shape passes and current master fails.
#
# Ruling 2026-08-16f: the file-on-disk and list_duplicates assertions are the
# existing-consumer checks for the store this task touches.
set -u
PKG=internal/services/mcpsvc/admin
PLANTED="$PKG/zz_oracle_v1_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
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

// oracleLegacyPairKey mirrors dataloader's unexported pairKey: sorted pair,
// sha256, first 8 bytes hex. If dataloader ever changes its key derivation,
// the non-degeneracy fatal below fires rather than the test passing vacuously.
func oracleLegacyPairKey(a, b string) string {
	lo, hi := a, b
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:8])
}

func TestOracleV1UndoReachesLegacyKeyedDecision(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)

	// The pair as the user sees it: current (StableID-derived) key + the
	// two rows' legacy content hashes.
	currentKey, keptHash, suppressedHash := pendingPairKey(t, cs)
	if keptHash == "" || suppressedHash == "" {
		t.Fatal("fixture: pendingPairKey returned an empty hash")
	}

	legacyKey := oracleLegacyPairKey(keptHash, suppressedHash)
	if legacyKey == currentKey {
		t.Fatal("fixture is degenerate: the legacy (hash-derived) key equals the current key, so this test cannot distinguish the two lookups")
	}

	// File a kept_winner decision under the LEGACY key only — the on-disk
	// state of a pair decided before StableID existed.
	doc := struct {
		Decisions map[string]dataloader.DuplicateDecision `json:"decisions"`
	}{Decisions: map[string]dataloader.DuplicateDecision{
		legacyKey: {
			KeptHash:       keptHash,
			SuppressedHash: suppressedHash,
			Outcome:        "kept_winner",
			DecidedAt:      time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		},
	}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal decisions doc: %v", err)
	}
	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	if err := os.WriteFile(decisionsPath, raw, 0o644); err != nil {
		t.Fatalf("write decisions file: %v", err)
	}

	// Existing consumer #1: list_duplicates must surface the legacy-keyed
	// decision as a resolved pair filed under the CURRENT key. This is the
	// count assertion that keeps the later probes non-vacuous (2026-08-16e).
	listed := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if listed.ResolvedCount != 1 {
		t.Fatalf("resolved_count = %d, want 1 (the legacy-keyed decision must resolve the pair)", listed.ResolvedCount)
	}
	if listed.Resolved[0].PairKey != currentKey {
		t.Fatalf("resolved pair_key = %q, want the current key %q", listed.Resolved[0].PairKey, currentKey)
	}
	if listed.UnresolvedCount != 0 {
		t.Fatalf("unresolved_count = %d, want 0 before the undo", listed.UnresolvedCount)
	}

	// THE defect under test: undo by the key every consumer displays.
	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": currentKey}))
	if out.PreviousOutcome != "kept_winner" {
		t.Errorf("previous_outcome = %q, want kept_winner (must come from the aliased entry)", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1 (the pair is back in the queue)", out.UnresolvedRemaining)
	}

	// Existing consumer #2: the store itself. Neither key form may survive.
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

	// Existing consumer #3: the queue as re-listed.
	relisted := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{"include_resolved": true}))
	if relisted.UnresolvedCount != 1 {
		t.Errorf("unresolved_count = %d after undo, want 1", relisted.UnresolvedCount)
	}
	if relisted.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d after undo, want 0", relisted.ResolvedCount)
	}
}

// The refusal contract must survive the fix: a pair key that names NO
// decision under any alias is still refused, not silently "undone".
func TestOracleV1UnknownKeyStillRefused(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)

	res, err := cs.CallTool(t.Context(), callToolParams("undo_resolve", map[string]any{"pair_key": "no-such-pair"}))
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("undo_resolve of an unknown pair_key must be refused, and was not")
	}
}
GO

# The refusal test needs CallToolParams; keep the planted file free of a
# direct mcp import by generating a tiny helper alongside it.
HELPER="$PKG/zz_oracle_v1_helper_test.go"
cleanup() { rm -f "$PLANTED" "$HELPER"; }
trap cleanup EXIT
cat > "$HELPER" <<'GO'
package admin

import "github.com/modelcontextprotocol/go-sdk/mcp"

func callToolParams(name string, args map[string]any) *mcp.CallToolParams {
	return &mcp.CallToolParams{Name: name, Arguments: args}
}
GO

go test -count=1 -run 'TestOracleV1' ./internal/services/mcpsvc/admin/
ck "01-oracle-e2e" 0 "$?"

# Nothing already covered may regress in the two packages this task touches.
go test -count=1 ./internal/services/mcpsvc/admin/ ./internal/services/dataloader/ >/dev/null 2>&1
ck "02-package-suites" 0 "$?"

go build ./... >/dev/null 2>&1; ck "03-build" 0 "$?"
go vet ./...   >/dev/null 2>&1; ck "04-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 )) || exit 1
echo "ORACLE PASS"
