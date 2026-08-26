package retirement

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// --- reconcilePreparedPersons direct tests ---

func TestReconcilePreparedPersons_PrimaryHasSpouseEarlyReturn(t *testing.T) {
	// When the primary scenario has a spouse, prepared.Persons is left
	// untouched (the early-return branch).
	prepared := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
			{ID: "p2", Role: models.PersonRoleSpouse, Name: "B"},
		},
	}
	primary := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
			{ID: "p2", Role: models.PersonRoleSpouse, Name: "B"},
		},
	}
	reconcilePreparedPersons(prepared, primary)
	if len(prepared.Persons) != 2 {
		t.Errorf("expected 2 persons untouched, got %d", len(prepared.Persons))
	}
}

func TestReconcilePreparedPersons_PrimaryNoSpouseFiltersSpouse(t *testing.T) {
	// When the primary scenario has no spouse, any spouse person in
	// prepared.Persons is filtered out.
	prepared := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
			{ID: "p2", Role: models.PersonRoleSpouse, Name: "B"},
			{ID: "p3", Role: models.PersonRoleOther, Name: "C"},
		},
	}
	primary := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
		},
	}
	reconcilePreparedPersons(prepared, primary)
	if len(prepared.Persons) != 2 {
		t.Fatalf("expected spouse filtered out, got %d persons", len(prepared.Persons))
	}
	for _, p := range prepared.Persons {
		if p.Role == models.PersonRoleSpouse {
			t.Errorf("spouse should have been filtered, got %+v", p)
		}
	}
}

func TestReconcilePreparedPersons_PrimaryNoSpouseEmptyPrepared(t *testing.T) {
	// No spouse in prepared, no spouse in primary -> empty filtered list.
	prepared := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
		},
	}
	primary := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, Name: "A"},
		},
	}
	reconcilePreparedPersons(prepared, primary)
	if len(prepared.Persons) != 1 {
		t.Errorf("expected 1 person preserved, got %d", len(prepared.Persons))
	}
}

// --- saveInternal error paths via a root-proof write-blocked settings dir ---
//
// This used to chmod the settings dir to 0o500 (r-x, no write) to fail the
// next WriteFile. That is a DAC permission check, and root's
// CAP_DAC_OVERRIDE ignores DAC permission checks entirely -- under root (any
// root CI, this verification container included) the chmod is a no-op and
// the save silently succeeds, so the failure branch these tests defend
// never fires and a mutation there goes undetected.
//
// A later revision replaced settingsDir with a plain FILE at the same path
// (ENOTDIR), which is root-proof but wrong-reasoned: loadInternal ALSO opens
// with store.MkdirAll(sm.settingsDir, 0755) as its very first step (see
// settings.go), so the ENOTDIR failed the LOAD before saveInternal -- the
// branch every test below is named for -- ever ran. Every one of these
// tests calls sm.loadInternal() directly (PurgeRemoved*, UpdateSettings, the
// CRUD methods), so with the directory destroyed a mutant that swallows
// saveInternal's own error return went undetected: the tests' observed
// error always came from the load side.

// corruptSettingsDirToFile makes writes to settingsDir fail while leaving
// the directory itself, and any files already inside it, fully readable --
// so loadInternal's MkdirAll (a no-op against an already-existing directory)
// and its subsequent ReadFile both keep succeeding, and only the eventual
// saveInternal write is what fails. This mirrors
// internal/handlers/majorexpenses/coverage_test.go's makeDirWriteBlocked
// (see that function's doc comment for the full rationale): chmod 0o500 for
// an ordinary uid, whose r-x bits keep the directory readable/traversable
// but refuse the new staging file storage.atomicWrite's rename-based replace
// needs; a bind-mount-onto-itself remounted read-only for root, since root's
// CAP_DAC_OVERRIDE would otherwise ignore the chmod outright and EROFS from
// a read-only mount is enforced at the VFS layer, not a DAC check. The
// retained name keeps this helper's call sites (writeProtectedSM,
// TestUpdateSettings_SaveError, TestCRUD_SaveErrors) unchanged; settingsDir
// must already exist as an ordinary, possibly-populated directory.
func corruptSettingsDirToFile(t *testing.T, settingsDir string) {
	t.Helper()
	if os.Geteuid() != 0 {
		if err := os.Chmod(settingsDir, 0o500); err != nil {
			t.Fatalf("chmod 0500 %s: %v", settingsDir, err)
		}
		t.Cleanup(func() { _ = os.Chmod(settingsDir, 0o755) })
		return
	}
	if err := syscall.Mount(settingsDir, settingsDir, "", syscall.MS_BIND, ""); err != nil {
		t.Fatalf("bind mount %s onto itself: %v", settingsDir, err)
	}
	if err := syscall.Mount("", settingsDir, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
		_ = syscall.Unmount(settingsDir, syscall.MNT_DETACH)
		t.Fatalf("remount %s read-only: %v", settingsDir, err)
	}
	t.Cleanup(func() { _ = syscall.Unmount(settingsDir, syscall.MNT_DETACH) })
}

// writeProtectedSM constructs a SettingsManager seeded with one of each
// removable kind, then corrupts the settings directory so that subsequent
// saves fail. This is the documented project pattern.
func writeProtectedSM(t *testing.T, seed func(sm *SettingsManager)) *SettingsManager {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	settingsDir := filepath.Join(root, "settings")
	sm := NewSettingsManager(settingsDir, store)
	if seed != nil {
		seed(sm)
	}
	corruptSettingsDirToFile(t, settingsDir)
	return sm
}

func TestPurgeRemovedIncomeSource_SaveFailure(t *testing.T) {
	sm := writeProtectedSM(t, func(sm *SettingsManager) {
		src := models.IncomeSource{ID: "p-1", Name: "Pension", Amount: 100, Type: models.IncomeFixed}
		if _, err := sm.AddIncomeSource(src); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}
		if _, err := sm.RemoveIncomeSource("p-1"); err != nil {
			t.Fatalf("RemoveIncomeSource: %v", err)
		}
	})

	_, err := sm.PurgeRemovedIncomeSource("p-1")
	if err == nil {
		t.Fatal("expected save error when settings dir is read-only")
	}
}

func TestPurgeRemovedExpenseSource_SaveFailure(t *testing.T) {
	sm := writeProtectedSM(t, func(sm *SettingsManager) {
		src := models.ExpenseSource{ID: "ex-1", Name: "Travel", Amount: 100}
		if _, err := sm.AddExpenseSource(src); err != nil {
			t.Fatalf("AddExpenseSource: %v", err)
		}
		if _, err := sm.RemoveExpenseSource("ex-1"); err != nil {
			t.Fatalf("RemoveExpenseSource: %v", err)
		}
	})

	_, err := sm.PurgeRemovedExpenseSource("ex-1")
	if err == nil {
		t.Fatal("expected save error when settings dir is read-only")
	}
}

func TestPurgeRemovedBigTicketItem_SaveFailure(t *testing.T) {
	sm := writeProtectedSM(t, func(sm *SettingsManager) {
		item := models.BigTicketItem{ID: "bt-1", Name: "Car", Amount: 30000, Year: 5}
		if _, err := sm.AddBigTicketItem(item); err != nil {
			t.Fatalf("AddBigTicketItem: %v", err)
		}
		if _, err := sm.RemoveBigTicketItem("bt-1"); err != nil {
			t.Fatalf("RemoveBigTicketItem: %v", err)
		}
	})

	_, err := sm.PurgeRemovedBigTicketItem("bt-1")
	if err == nil {
		t.Fatal("expected save error when settings dir is read-only")
	}
}

// --- Load reset path: nil cache → loadInternal → cache update ---

func TestLoad_NilCacheRoundTrip(t *testing.T) {
	sm := newTestSM(t)
	// First load populates cache from disk (no file → defaults).
	if _, err := sm.Load(); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	// Force cache miss by clearing the cache directly; a second Load
	// then re-reads from disk via loadInternal.
	sm.mu.Lock()
	sm.cache = nil
	sm.mu.Unlock()
	got, err := sm.Load()
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil settings on second load")
	}
}

// --- ScenarioNotFoundError fast-path for filter-only-no-match cases ---

func TestPurgeRemovedExpenseSource_ActiveOnlyNoOp(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.AddExpenseSource(models.ExpenseSource{ID: "active", Name: "Active"}); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	_, err := sm.PurgeRemovedExpenseSource("nope")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError for unknown id")
	}
	var nf *ScenarioNotFoundError
	if !errors.As(err, &nf) {
		t.Errorf("expected *ScenarioNotFoundError, got %T", err)
	}
}
