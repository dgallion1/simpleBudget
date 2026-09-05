package dataloader

// zz_probe_od1_test.go — OD1 live-data binding probe (ND3-style). Copies the
// live data dir into t.TempDir() (never touches live data), runs a full load,
// and FAILS if any recorded duplicate decision does not bind to a detected
// pair, printing why (no matching rows at all vs. rows present but keyed
// differently). Skipped unless OD1_DATA_DIR is set, so CI never depends on
// live data.
//
// Regression context (task OD1, 2026-09-03): the 2026-08-29 CSV rename +
// accounts.json assignment changed every row's StableID account slot from
// "file:<oldname>" to a real account ID. Six decisions keyed by pair keys of
// the old file:-form StableIDs were orphaned — the PR #64 legacy aliasing only
// maps content-hash pair keys to current keys, never old-StableID keys (see
// stable_id.go: the file: slot is "not durable across a file rename"). All six
// had been re-decided by the user under the new identities, so the orphans
// were pruned from data/duplicate_decisions.json (backup alongside). This
// probe keeps recorded == bound from drifting again unnoticed.
//
// Run:
//   OD1_DATA_DIR=/home/darrell/bin/ai/budget2/data go test -count=1 -run TestProbeOD1 -v ./internal/services/dataloader/

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"budget2/internal/services/storage"
)

func od1CopyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // loader reads only root files (*.csv + json sidecars)
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dst, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			t.Fatal(err)
		}
		in.Close()
		out.Close()
	}
}

func TestProbeOD1_DecisionBinding(t *testing.T) {
	src := os.Getenv("OD1_DATA_DIR")
	if src == "" {
		t.Skip("OD1_DATA_DIR not set; live-data probe skipped")
	}
	dir := t.TempDir()
	od1CopyDir(t, src, dir)

	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dl := New(dir, store)
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	t.Logf("loaded %d transactions", len(ts.Transactions))

	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("LoadDuplicateDecisions: %v", err)
	}

	unresolved := dl.UnresolvedDuplicates()
	resolved := dl.ResolvedDuplicates()
	keptBoth := dl.KeptBothDuplicates()
	t.Logf("pairs: unresolved=%d resolved(kept_winner)=%d kept_both=%d",
		len(unresolved), len(resolved), len(keptBoth))
	for _, p := range unresolved {
		t.Logf("  UNRESOLVED %s: %s %s %.2f | %s %s %.2f", p.Key,
			p.Left.Date.Format("2006-01-02"), p.Left.Description, p.Left.Amount,
			p.Right.Date.Format("2006-01-02"), p.Right.Description, p.Right.Amount)
	}

	// Reconstruct the loader's binding sets: a decision entry binds when its
	// map key is a detected pair's current (StableID-derived) key, or that
	// pair's legacy content-hash key (and the current key holds no decision).
	all := append(append(append([]DuplicatePair{}, unresolved...), resolved...), keptBoth...)
	currentKeys := make(map[string]DuplicatePair, len(all))
	legacyKeys := make(map[string]DuplicatePair, len(all))
	for _, p := range all {
		currentKeys[p.Key] = p
		legacyKeys[pairKey(p.Left.Hash, p.Right.Hash)] = p
	}

	// Identity index over every loaded row, both forms, mirroring the loader.
	idx := make(map[string]int, 2*len(ts.Transactions))
	for i, tx := range ts.Transactions {
		idx[tx.Hash] = i
		if tx.StableID != "" {
			idx[tx.StableID] = i
		}
	}

	keys := make([]string, 0, len(decisions))
	for k := range decisions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	bound, unbound := 0, 0
	for _, k := range keys {
		d := decisions[k]
		if p, ok := currentKeys[k]; ok {
			bound++
			t.Logf("BOUND   %s [%s] via current key (pair %s|%.2f)", k, d.Outcome,
				p.Left.Date.Format("2006-01-02"), p.Left.Amount)
			continue
		}
		if p, ok := legacyKeys[k]; ok {
			if _, shadowed := decisions[p.Key]; shadowed {
				unbound++
				t.Errorf("SHADOWED %s [%s]: legacy key of pair %s, which has its own decision", k, d.Outcome, p.Key)
			} else {
				bound++
				t.Logf("BOUND   %s [%s] via legacy key (pair %s)", k, d.Outcome, p.Key)
			}
			continue
		}
		unbound++
		// Diagnose: do the recorded identities still name loaded rows?
		ki, kOK := idx[d.KeptHash]
		si, sOK := idx[d.SuppressedHash]
		switch {
		case d.KeptHash == "" && d.SuppressedHash == "":
			t.Errorf("UNBOUND %s [%s]: kept_both with no hashes; key matches no detected pair (current or legacy)", k, d.Outcome)
		case kOK && sOK:
			cur := pairKey(identityKey(ts.Transactions[ki]), identityKey(ts.Transactions[si]))
			_, redecided := decisions[cur]
			_, stillDetected := currentKeys[cur]
			t.Errorf("UNBOUND %s [%s]: both rows still exist (kept=%s suppressed=%s); current pair key would be %s (re-decided=%v, still-detected=%v)",
				k, d.Outcome, ts.Transactions[ki].StableID, ts.Transactions[si].StableID, cur, redecided, stillDetected)
		default:
			t.Errorf("UNBOUND %s [%s]: no loaded row matches kept_hash=%q (found=%v) suppressed_hash=%q (found=%v)",
				k, d.Outcome, d.KeptHash, kOK, d.SuppressedHash, sOK)
		}
	}
	t.Logf("TOTAL recorded=%d bound=%d unbound=%d", len(decisions), bound, unbound)
	if unbound != 0 {
		t.Errorf("recorded=%d bound=%d: every recorded decision must bind to a detected pair", len(decisions), bound)
	}
}
