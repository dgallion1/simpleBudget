package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pendingPairKey resolves the one candidate pair newLiveDeps creates.
func pendingPairKey(t *testing.T, cs *mcp.ClientSession) (key, keptHash, suppressedHash string) {
	t.Helper()
	out := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if out.UnresolvedCount != 1 {
		t.Fatalf("unresolved_count = %d, want 1", out.UnresolvedCount)
	}
	p := out.Unresolved[0]
	return p.PairKey, p.Left.Hash, p.Right.Hash
}

func TestResolveDuplicatesWritesTheDecision(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	out := decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))

	if out.PairKey != key {
		t.Errorf("pair_key = %q, want %q", out.PairKey, key)
	}
	if len(out.SnapshotPaths) == 0 && out.Note == "" {
		t.Error("neither a snapshot path nor a note explaining its absence")
	}
	if out.UnresolvedRemaining != 0 {
		t.Errorf("unresolved_remaining = %d, want 0", out.UnresolvedRemaining)
	}

	data, err := os.ReadFile(filepath.Join(dir, "duplicate_decisions.json"))
	if err != nil {
		t.Fatalf("read decisions file: %v", err)
	}
	if !strings.Contains(string(data), key) {
		t.Errorf("decisions file does not mention the pair key:\n%s", data)
	}
	if !strings.Contains(string(data), "kept_winner") {
		t.Errorf("decisions file does not record the outcome:\n%s", data)
	}
}

func TestResolveDuplicatesRejectsAnUnknownPairKey(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": "deadbeefdeadbeef",
		"outcome":  "kept_both",
	}))
	if !strings.Contains(msg, "deadbeefdeadbeef") {
		t.Errorf("error does not name the rejected key: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written for an unknown key (stat err = %v)", err)
	}
}

func TestResolveDuplicatesRejectsAHashNotInThePair(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": "not-in-this-pair",
	}))
	if !strings.Contains(msg, "not-in-this-pair") {
		t.Errorf("error does not name the offending hash: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written for a hash outside the pair (stat err = %v)", err)
	}
}

func TestResolveDuplicatesRejectsAnUnknownOutcome(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": key,
		"outcome":  "kept_neither",
	}))
	if !strings.Contains(msg, "kept_neither") {
		t.Errorf("error does not name the rejected outcome: %s", msg)
	}
}
