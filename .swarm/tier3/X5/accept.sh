#!/usr/bin/env bash
# Oracle for X5 — aliases.json must survive a description reformat: keyed by
# StableID on write, resolved by either identity form on read.
# Run with cwd set to the tree under test.
#
# Plants its own in-package test in internal/services/dataloader and removes
# it afterwards. Drives only exported surface (LoadData, SaveAlias,
# LoadAliases) plus the package's own test fixtures, so any correct
# implementation shape passes. Consumer assertions (ruling 2026-08-16f): the
# observable output is Transaction.DisplayName as LoadData delivers it to
# every page and MCP tool, plus the on-disk file another save must not orphan.
#
# Shared-tree ruling X-2026-08-29a: commands are scoped to the dataloader
# package; the lead runs the full suite at integration time.
set -u
PKG=internal/services/dataloader
PLANTED="$PKG/zz_oracle_x5_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package dataloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// oracleAliasFixture loads the one-row fixture and returns (dir, loader,
// row-hash, row-stableID, cleanup). Fails the test if the two identity forms
// collide, which would make every later assertion vacuous.
func oracleAliasFixture(t *testing.T) (string, *DataLoader, string, string, func()) {
	t.Helper()
	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"checking-2025.csv": `Date,Description,Amount,Status
2025-07-01,ACME COFFEE ROASTERS,-31.50,Posted`,
	})
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("loaded %d rows, want 1", len(ts.Transactions))
	}
	row := ts.Transactions[0]
	if row.Hash == "" || row.StableID == "" {
		t.Fatalf("fixture row missing an identity: hash=%q stable=%q", row.Hash, row.StableID)
	}
	if row.Hash == row.StableID {
		t.Fatal("fixture is degenerate: Hash equals StableID")
	}
	return dir, loader, row.Hash, row.StableID, cleanup
}

func writeAliases(t *testing.T, dir string, m map[string]string) {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal aliases: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aliases.json"), raw, 0644); err != nil {
		t.Fatalf("stage aliases.json: %v", err)
	}
}

func displayNameAfterReload(t *testing.T, loader *DataLoader) string {
	t.Helper()
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(ts.Transactions) != 1 {
		t.Fatalf("reloaded %d rows, want 1", len(ts.Transactions))
	}
	return ts.Transactions[0].DisplayName
}

// A pre-migration, hash-keyed file must keep working with no write.
func TestOracleX5LegacyHashKeyedAliasStillApplies(t *testing.T) {
	dir, loader, hash, _, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{hash: "Coffee (legacy key)"})
	if got := displayNameAfterReload(t, loader); got != "Coffee (legacy key)" {
		t.Errorf("DisplayName = %q, want the hash-keyed alias to keep applying", got)
	}
}

// THE defect: a StableID-keyed entry — the post-reformat survivor — must
// apply. Today applyAliases matches Hash only, so this fails on master.
func TestOracleX5StableIDKeyedAliasApplies(t *testing.T) {
	dir, loader, _, stable, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{stable: "Coffee (stable key)"})
	if got := displayNameAfterReload(t, loader); got != "Coffee (stable key)" {
		t.Errorf("DisplayName = %q, want the StableID-keyed alias to apply", got)
	}
}

// A save must land the entry under the StableID even when the caller still
// speaks legacy hash (the explorer posts Transaction.Hash), so a later
// description reformat cannot orphan it.
func TestOracleX5SaveRekeysToStableID(t *testing.T) {
	dir, loader, hash, stable, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{hash: "Coffee (legacy key)"})
	if err := loader.SaveAlias(hash, "Coffee (renamed)"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}
	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if saved[stable] != "Coffee (renamed)" {
		t.Errorf("saved aliases = %v, want the entry filed under the StableID", saved)
	}
	if _, ok := saved[hash]; ok {
		t.Errorf("legacy hash key survived the save; a reformat would leave both or orphan one: %v", saved)
	}
	if got := displayNameAfterReload(t, loader); got != "Coffee (renamed)" {
		t.Errorf("DisplayName = %q after rekeying save", got)
	}
}

// Removal (empty name) must reach an entry filed under EITHER identity form.
func TestOracleX5RemovalReachesStableKeyedEntry(t *testing.T) {
	dir, loader, hash, stableID, cleanup := oracleAliasFixture(t)
	defer cleanup()
	// Entry filed under the StableID; the UI hands back the hash.
	writeAliases(t, dir, map[string]string{stableID: "Coffee (stable key)"})
	if err := loader.SaveAlias(hash, ""); err != nil {
		t.Fatalf("SaveAlias(remove): %v", err)
	}
	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases after removal: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("aliases after removal = %v, want empty (removal must reach the stable-keyed entry)", saved)
	}
	if got := displayNameAfterReload(t, loader); got != "" {
		t.Errorf("DisplayName = %q after removal, want empty", got)
	}
}

// A save for row A must also rekey a resolvable legacy entry for row B —
// the opportunistic migration every other rekeyed sidecar performs. Added
// after the attempt-1 verify pass proved a rekey-deleting mutant survived
// the original oracle: canonicalKey covers the saved key, so only a
// BYSTANDER entry can distinguish the rekey step from the canonicalization.
func TestOracleX5BystanderLegacyEntryRekeyedOnUnrelatedSave(t *testing.T) {
	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"checking-2025.csv": `Date,Description,Amount,Status
2025-07-01,ACME COFFEE ROASTERS,-31.50,Posted
2025-07-02,RIVERSIDE HARDWARE,-64.20,Posted`,
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
	if rowB.Hash == rowB.StableID {
		t.Fatal("fixture is degenerate: row B's Hash equals its StableID")
	}
	writeAliases(t, dir, map[string]string{rowB.Hash: "Bystander"})
	if err := loader.SaveAlias(rowA.Hash, "Target"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}
	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("aliases = %v, want exactly 2 entries", saved)
	}
	if saved[rowB.StableID] != "Bystander" {
		t.Errorf("bystander was not rekeyed to its StableID: %v", saved)
	}
	if _, ok := saved[rowB.Hash]; ok {
		t.Errorf("bystander's legacy hash key survived the unrelated save: %v", saved)
	}
}

// When both identity forms are present for one row, the StableID entry wins
// on read, and a write drops the redundant legacy form in its favour.
func TestOracleX5StableIDEntryWinsCollision(t *testing.T) {
	dir, loader, hash, stable, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{hash: "Old name", stable: "New name"})
	if got := displayNameAfterReload(t, loader); got != "New name" {
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
		t.Fatalf("aliases after save = %v, want the collision collapsed to 1 entry", saved)
	}
	if saved[stable] != "New name" {
		t.Errorf("surviving entry = %v, want it filed under the StableID", saved)
	}
}

// Removal by StableID must not be resurrected by the same write's rekey
// pass. Attempt-1 defect: with both forms staged, SaveAlias(stableID, "")
// deleted only the stable key, then rekeyToStable re-promoted the legacy
// duplicate's value under it — the removal silently undone.
func TestOracleX5RemovalByStableIDDoesNotResurrect(t *testing.T) {
	dir, loader, hash, stable, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{hash: "Legacy", stable: "Stable name"})
	if err := loader.SaveAlias(stable, ""); err != nil {
		t.Fatalf("SaveAlias(remove by StableID): %v", err)
	}
	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("aliases after removal-by-StableID = %v, want empty (the legacy dup must not resurrect it)", saved)
	}
	if got := displayNameAfterReload(t, loader); got != "" {
		t.Errorf("DisplayName = %q after removal, want empty", got)
	}
}

// An unresolvable legacy key (row not in the load) is preserved by a save,
// never dropped — same contract as every other rekeyed sidecar.
func TestOracleX5UnresolvableLegacyKeyPreserved(t *testing.T) {
	dir, loader, hash, _, cleanup := oracleAliasFixture(t)
	defer cleanup()
	writeAliases(t, dir, map[string]string{"deadbeef00000000": "Orphaned but kept"})
	if err := loader.SaveAlias(hash, "Coffee (new)"); err != nil {
		t.Fatalf("SaveAlias: %v", err)
	}
	saved, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("LoadAliases: %v", err)
	}
	if saved["deadbeef00000000"] != "Orphaned but kept" {
		t.Errorf("unresolvable legacy entry was dropped by an unrelated save: %v", saved)
	}
}
GO

go test -count=1 -run 'TestOracleX5' -v ./internal/services/dataloader/ 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL)|PASS|FAIL|ok)' | tail -20
go test -count=1 -run 'TestOracleX5' ./internal/services/dataloader/ >/dev/null 2>&1
ck "01-oracle-behaviors" 0 "$?"

# Nothing already covered in the package may regress.
go test -count=1 ./internal/services/dataloader/ >/dev/null 2>&1
ck "02-package-suite" 0 "$?"

go vet ./internal/services/dataloader/ >/dev/null 2>&1
ck "03-vet-package" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 )) || exit 1
echo "ORACLE PASS"
