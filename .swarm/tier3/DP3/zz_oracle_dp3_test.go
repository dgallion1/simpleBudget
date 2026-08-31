package dataloader

// DP3 oracle — staged into internal/services/dataloader/ by
// .swarm/tier3/DP3/accept.sh for the duration of one run, then removed.
// Asserts the acceptance criteria of the DP3 widening (.swarm/DP-RUN-SPEC.md):
// the six live pending→posted duplicates missed by the DP1 constants are now
// detected, every previously detected pair still binds, and no new false
// positive enters the queue.
//
// The live data directory is resolved from $BUDGET2_DATA_DIR when set (the
// worktree checkout has no data/), else from the repo-root data/ like DP1.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func dp3DataDir(t *testing.T) string {
	if dir := os.Getenv("BUDGET2_DATA_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "..", "data")
}

func dp3Load(t *testing.T) *DataLoader {
	src := dp3DataDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("real data dir not found at %s: %v", src, err)
	}
	tmp := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // cache/settings/uploads not needed by the loader path under test
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.New(tmp)
	if err != nil {
		t.Fatal(err)
	}
	loader := New(tmp, store)
	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData over data copy: %v", err)
	}
	return loader
}

// dp3Expected identifies each of the six previously missed pending→posted
// pairs by amount plus the pending side's date and a description fragment
// (calibrated 2026-08-30 against live data via the widening probe).
var dp3Expected = []struct {
	amount      float64
	pendingDate string
	pendingDesc string
}{
	{-144.00, "2026-04-29", "grammarly"},        // 9-byte raw prefix, 11 squashed
	{-634.40, "2026-05-02", "bjs wholesale"},    // apostrophe + store number
	{-64.50, "2026-05-01", "bjs membership"},    // brand dropped on posted side
	{-59.00, "2026-05-01", "grubhub*fiveguys"},  // aggregator vs merchant name
	{-58.00, "2026-07-12", "grubhub*fiveguys"},  // second Grubhub instance
	{-30.81, "2025-11-16", "amazon mktplace"},   // 4-day settlement lag
}

// TestOracleDP3_RealData: exactly the six calibrated pairs are new and
// unresolved, each split one Pending / one Posted; every recorded decision
// (16 resolved + 1 kept_both as of 2026-08-30) still binds.
func TestOracleDP3_RealData(t *testing.T) {
	loader := dp3Load(t)
	unresolved := loader.UnresolvedDuplicates()
	if len(unresolved) != 6 {
		t.Fatalf("expected exactly 6 unresolved pairs on live data (calibrated 2026-08-30), got %d: %+v", len(unresolved), unresolved)
	}
	for _, want := range dp3Expected {
		found := false
		for _, p := range unresolved {
			for _, side := range []models.Transaction{p.Left, p.Right} {
				if side.Amount == want.amount &&
					side.Date.Format("2006-01-02") == want.pendingDate &&
					strings.Contains(strings.ToLower(side.Description), want.pendingDesc) &&
					strings.Contains(strings.ToLower(side.Status), "pending") {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("expected pair not in queue: %.2f %s %q", want.amount, want.pendingDate, want.pendingDesc)
		}
	}
	for _, p := range unresolved {
		pendings := 0
		for _, side := range []models.Transaction{p.Left, p.Right} {
			if strings.Contains(strings.ToLower(side.Status), "pending") {
				pendings++
			}
		}
		if pendings != 1 {
			t.Errorf("pair %s: expected exactly one Pending side, got %d (%q/%q)",
				p.Key, pendings, p.Left.Status, p.Right.Status)
		}
	}
	if got := len(loader.ResolvedDuplicates()); got != 16 {
		t.Errorf("resolved decisions no longer bind: expected 16, got %d", got)
	}
	if got := len(loader.KeptBothDuplicates()); got != 1 {
		t.Errorf("kept_both decisions no longer bind: expected 1, got %d", got)
	}
}
