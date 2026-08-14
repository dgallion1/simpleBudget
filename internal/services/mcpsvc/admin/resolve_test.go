package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/services/dataloader"

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

// TestResolveDuplicatesWritesKeptBoth covers the OTHER success path: unlike
// kept_winner, no test previously ever wrote a kept_both decision.
func TestResolveDuplicatesWritesKeptBoth(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	out := decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": key,
		"outcome":  "kept_both",
	}))

	if out.PairKey != key {
		t.Errorf("pair_key = %q, want %q", out.PairKey, key)
	}
	if out.Outcome != "kept_both" {
		t.Errorf("outcome = %q, want kept_both", out.Outcome)
	}
	if out.SuppressedHash != "" {
		t.Errorf("suppressed_hash = %q, want empty for kept_both", out.SuppressedHash)
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
	if !strings.Contains(string(data), "kept_both") {
		t.Errorf("decisions file does not record the outcome:\n%s", data)
	}

	// A kept_both pair must not be re-flagged as awaiting review.
	after := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if after.UnresolvedCount != 0 {
		t.Errorf("unresolved_count = %d, want 0 after kept_both", after.UnresolvedCount)
	}
}

func TestResolveDuplicatesRejectsIdenticalKeptAndSuppressedHashes(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": kept,
	}))
	if !strings.Contains(msg, kept) {
		t.Errorf("error does not name the duplicated hash: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written when kept_hash == suppressed_hash (stat err = %v)", err)
	}
}

func TestResolveDuplicatesRejectsKeptWinnerMissingHashes(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": key,
		"outcome":  "kept_winner",
	}))
	if !strings.Contains(msg, "kept_hash") || !strings.Contains(msg, "suppressed_hash") {
		t.Errorf("error does not name the missing fields: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written for kept_winner missing both hashes (stat err = %v)", err)
	}
}

// TestResolveDuplicatesSnapshotsAnExistingDecisionsFileFirst is the direct
// fix for the gap the review caught: every prior test runs against a FRESH
// install, where duplicate_decisions.json does not exist yet and Ensure
// always takes the fs.ErrNotExist branch -- so the .bak this tool's own
// description promises was never once produced anywhere in the suite. This
// seeds a prior decision directly through deps.Decisions (bypassing the
// tool, and therefore the Snapshotter's own cache) so a REAL backup must be
// taken when resolve_duplicates runs.
func TestResolveDuplicatesSnapshotsAnExistingDecisionsFileFirst(t *testing.T) {
	deps, dir := newLiveDeps(t)
	if err := deps.Decisions.SaveDuplicateDecision("stale-pair-key", dataloader.DuplicateDecision{
		Outcome: dataloader.DuplicateOutcomeKeptBoth,
	}); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	before, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatalf("read decisions before: %v", err)
	}
	if !strings.Contains(string(before), "stale-pair-key") {
		t.Fatalf("test setup: seeded decision missing from file:\n%s", before)
	}

	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	out := decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))

	if len(out.SnapshotPaths) == 0 {
		t.Fatal("snapshot_paths is empty; a prior decisions file existed and should have been backed up")
	}
	backup, err := os.ReadFile(out.SnapshotPaths[0])
	if err != nil {
		t.Fatalf("read snapshot %s: %v", out.SnapshotPaths[0], err)
	}
	if !strings.Contains(string(backup), "stale-pair-key") {
		t.Errorf("snapshot does not contain the pre-write decision:\n%s", backup)
	}
	if string(backup) != string(before) {
		t.Errorf("snapshot content does not match the decisions file's content just before the write:\nsnapshot=%s\nbefore=%s", backup, before)
	}
}

// TestResolveDuplicatesAbortsWhenAnExistingDecisionsFileCannotBeBackedUp
// covers the OTHER half of ensureDecisionsSnapshot: a non-not-found Ensure
// failure must abort before anything is written, unlike a genuinely missing
// file. The failure is engineered at the SNAPSHOT DESTINATION, not the data
// file -- a regular file is planted where Ensure needs to create the
// snapshot directory, so os.MkdirAll fails with "not a directory" (never
// fs.ErrNotExist) while duplicate_decisions.json itself stays fully
// readable and writable. Mirrors
// curate/pins_test.go's TestPinTransactionsAbortsWhenAnExistingPinsFileCannotBeBackedUp,
// including its reasoning for why the blocker must live at the destination
// rather than the source: a chmod on the source would make the eventual
// SaveDuplicateDecision write fail on its own, so the test would pass
// whether or not the abort branch actually ran.
func TestResolveDuplicatesAbortsWhenAnExistingDecisionsFileCannotBeBackedUp(t *testing.T) {
	deps, dir := newLiveDeps(t)
	if err := deps.Decisions.SaveDuplicateDecision("stale-pair-key", dataloader.DuplicateDecision{
		Outcome: dataloader.DuplicateOutcomeKeptBoth,
	}); err != nil {
		t.Fatalf("seed decisions: %v", err)
	}
	decisionsPath := filepath.Join(dir, "duplicate_decisions.json")
	before, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatalf("read decisions before: %v", err)
	}

	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	// newLiveDeps points this Deps' Snapshotter at filepath.Join(dir,
	// "snapshots"). Plant a plain file there so Ensure's os.MkdirAll of that
	// same path fails with ENOTDIR, without touching
	// duplicate_decisions.json at all. This is the FIRST Ensure call of the
	// session, so there is no prior success cached to short-circuit it.
	snapshotDirPath := filepath.Join(dir, "snapshots")
	if err := os.WriteFile(snapshotDirPath, []byte("blocking the snapshot directory"), 0o644); err != nil {
		t.Fatalf("plant snapshot-dir blocker: %v", err)
	}

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))
	if !strings.Contains(msg, "snapshot") {
		t.Errorf("expected the refusal to mention the failed snapshot, got: %s", msg)
	}

	// The discriminating assertion: the data file's bytes must be
	// BYTE-FOR-BYTE unchanged, not merely "the tool returned an error".
	after, err := os.ReadFile(decisionsPath)
	if err != nil {
		t.Fatalf("read decisions after: %v", err)
	}
	if string(after) != string(before) {
		t.Error("duplicate_decisions.json changed despite the backup failing")
	}

	// The pending pair must still be unresolved -- nothing was written for it.
	list := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if list.UnresolvedCount != 1 {
		t.Errorf("unresolved_count = %d, want 1 (nothing should have been resolved)", list.UnresolvedCount)
	}
}
