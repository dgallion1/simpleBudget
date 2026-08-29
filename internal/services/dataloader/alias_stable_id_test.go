package dataloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// aliasFixture loads a one-row fixture and returns the loader plus the
// row's two identity forms. Fails fast if the forms collide -- that would
// make every apply/save assertion below vacuous, since either form would
// resolve the same map entry.
func aliasFixture(t *testing.T) (dir string, loader *DataLoader, hash, stableID string, cleanup func()) {
	t.Helper()
	dir, loader, cleanup = setupTestDir(t, map[string]string{
		"usaa-checking-2025.csv": `Date,Description,Category,Amount
2025-06-01,SQ *CORNER BAKERY,Dining,-18.75`,
	})
	ts, err := loader.LoadData()
	if err != nil {
		cleanup()
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 1 {
		cleanup()
		t.Fatalf("loaded %d rows, want 1", len(ts.Transactions))
	}
	row := ts.Transactions[0]
	if row.Hash == "" || row.StableID == "" {
		cleanup()
		t.Fatalf("fixture row has Hash=%q StableID=%q; both are required", row.Hash, row.StableID)
	}
	if row.Hash == row.StableID {
		cleanup()
		t.Fatal("fixture is degenerate: Hash equals StableID")
	}
	return dir, loader, row.Hash, row.StableID, cleanup
}

func stageAliases(t *testing.T, dir string, m map[string]string) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal aliases: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aliases.json"), data, 0644); err != nil {
		t.Fatalf("stage aliases.json: %v", err)
	}
}

// TestApplyAliases_LegacyHashSidecarStillResolves is the migration
// guarantee: an aliases file written before StableID existed, keyed on the
// content hash, keeps applying with no migration step.
func TestApplyAliases_LegacyHashSidecarStillResolves(t *testing.T) {
	dir, loader, hash, _, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{hash: "Corner Bakery"})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(ts.Transactions))
	}
	if got := ts.Transactions[0].DisplayName; got != "Corner Bakery" {
		t.Errorf("DisplayName = %q, want the legacy-hash-keyed alias", got)
	}
}

// TestApplyAliases_StableIDKeyedSidecarApplies is the defect this task
// fixes: an aliases file already rekeyed to StableID -- the shape that
// survives a description reformat, since the reformat changes Hash but not
// StableID -- must still apply. Before this change applyAliases matched
// Hash only, so this failed.
func TestApplyAliases_StableIDKeyedSidecarApplies(t *testing.T) {
	dir, loader, _, stableID, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{stableID: "Corner Bakery"})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(ts.Transactions))
	}
	if got := ts.Transactions[0].DisplayName; got != "Corner Bakery" {
		t.Errorf("DisplayName = %q, want the StableID-keyed alias", got)
	}
}

// TestSaveAlias_RekeysToStableIDAndDropsLegacyKey covers behavior 2+3: a
// save made with the legacy hash (what the explorer posts) files the entry
// under the StableID and removes the now-redundant legacy key, so a later
// description reformat -- which changes only Hash -- cannot orphan it.
func TestSaveAlias_RekeysToStableIDAndDropsLegacyKey(t *testing.T) {
	dir, loader, hash, stableID, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{hash: "Corner Bakery"})

	if err := loader.SaveAlias(hash, "Corner Bakery Cafe"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("aliases has %d entries, want 1: %+v", len(saved), saved)
	}
	if saved[stableID] != "Corner Bakery Cafe" {
		t.Errorf("save did not file the entry under the StableID: %+v", saved)
	}
	if _, stillLegacy := saved[hash]; stillLegacy {
		t.Errorf("legacy hash key survived the save; a later reformat would then leave both or orphan one: %+v", saved)
	}
}

// TestSaveAlias_RemovalReachesEntryFiledUnderTheOtherForm covers behavior 4:
// an entry already rekeyed to StableID (a prior save, or a hand-rekeyed
// file) must still be removable when the caller hands back the legacy hash
// -- exactly what the explorer does, since it renders Transaction.Hash.
func TestSaveAlias_RemovalReachesEntryFiledUnderTheOtherForm(t *testing.T) {
	dir, loader, hash, stableID, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{stableID: "Corner Bakery"})

	if err := loader.SaveAlias(hash, ""); err != nil {
		t.Fatalf("SaveAlias(remove): %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("aliases after removal = %+v, want empty (removal must reach the StableID-keyed entry)", saved)
	}
}

// TestSaveAlias_BystanderLegacyEntryRekeyedOnUnrelatedSave covers the
// opportunistic-migration half of behavior 3: a save issued for one row
// must also rekey a DIFFERENT row's resolvable legacy-hash entry, not just
// canonicalize the key the caller happened to pass in. canonicalKey alone
// covers only the saved key, so this is the only kind of case that can tell
// the rekey pass apart from canonicalization -- it must FAIL if the
// normalize/rekey pass is removed from SaveAlias.
func TestSaveAlias_BystanderLegacyEntryRekeyedOnUnrelatedSave(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2025.csv": `Date,Description,Category,Amount
2025-06-01,SQ *CORNER BAKERY,Dining,-18.75
2025-06-02,ACME HARDWARE,Home,-31.50`,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("loaded %d rows, want 2", len(ts.Transactions))
	}
	rowA, rowB := ts.Transactions[0], ts.Transactions[1]
	if rowB.Hash == "" || rowB.StableID == "" {
		t.Fatalf("row B missing an identity: hash=%q stable=%q", rowB.Hash, rowB.StableID)
	}
	if rowB.Hash == rowB.StableID {
		t.Fatal("fixture is degenerate: row B's Hash equals its StableID")
	}

	stageAliases(t, dir, map[string]string{rowB.Hash: "Bystander"})

	if err := loader.SaveAlias(rowA.Hash, "Target"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("aliases has %d entries, want 2: %+v", len(saved), saved)
	}
	if saved[rowB.StableID] != "Bystander" {
		t.Errorf("bystander entry was not rekeyed to its StableID: %+v", saved)
	}
	if _, stillLegacy := saved[rowB.Hash]; stillLegacy {
		t.Errorf("bystander's legacy hash key survived an unrelated save: %+v", saved)
	}
}

// TestSaveAlias_CollisionStableIDWins covers the read- and write-side
// collision contract: when both identity forms are staged for one row with
// DIFFERENT names, the StableID entry wins on read, and any save collapses
// the collision to exactly one entry, filed under the StableID.
func TestSaveAlias_CollisionStableIDWins(t *testing.T) {
	dir, loader, hash, stableID, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{hash: "Old name", stableID: "New name"})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("loaded %d rows, want 1", len(ts.Transactions))
	}
	if got := ts.Transactions[0].DisplayName; got != "New name" {
		t.Errorf("DisplayName = %q on a both-forms collision, want the StableID entry to win", got)
	}

	if err := loader.SaveAlias(hash, "New name"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("aliases after save has %d entries, want the collision collapsed to 1: %+v", len(saved), saved)
	}
	if saved[stableID] != "New name" {
		t.Errorf("surviving entry = %+v, want it filed under the StableID", saved)
	}
}

// TestSaveAlias_RemovalByStableIDWithLegacyDupDoesNotResurrect is the
// regression pin for the attempt-1 defect: with BOTH forms staged for one
// row, removing by the StableID must not be undone by the same write's
// rekey pass promoting the legacy duplicate's value back in under the
// stable key.
func TestSaveAlias_RemovalByStableIDWithLegacyDupDoesNotResurrect(t *testing.T) {
	dir, loader, hash, stableID, cleanup := aliasFixture(t)
	defer cleanup()

	stageAliases(t, dir, map[string]string{hash: "Legacy", stableID: "Stable name"})

	if err := loader.SaveAlias(stableID, ""); err != nil {
		t.Fatalf("SaveAlias(remove by StableID): %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("aliases after removal-by-StableID = %+v, want empty (the legacy dup must not resurrect it)", saved)
	}

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(ts.Transactions))
	}
	if got := ts.Transactions[0].DisplayName; got != "" {
		t.Errorf("DisplayName = %q after removal, want empty", got)
	}
}

// TestSaveAlias_UnresolvableLegacyKeyPreserved covers behavior 3's other
// half: a legacy key naming a row outside the current load is not the
// target of the save, but an unrelated save must not drop it -- the row is
// probably just outside the loaded date range, not gone.
func TestSaveAlias_UnresolvableLegacyKeyPreserved(t *testing.T) {
	dir, loader, hash, _, cleanup := aliasFixture(t)
	defer cleanup()

	const orphan = "0123456789abcdef" // no loaded row has this hash
	stageAliases(t, dir, map[string]string{orphan: "Orphaned but kept"})

	if err := loader.SaveAlias(hash, "Corner Bakery"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}

	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("aliases has %d entries, want 2: %+v", len(saved), saved)
	}
	if saved[orphan] != "Orphaned but kept" {
		t.Errorf("unresolvable legacy entry was dropped by an unrelated save: %+v", saved)
	}
}
