package retirement

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/overrides"
	"budget2/internal/services/storage"
)

func TestSettingsManager_Cache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "settings_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, _ := storage.New(tmpDir)
	sm := NewSettingsManager(tmpDir, store)

	// 1. Initial Load should return default settings
	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if settings.PortfolioValue != 0 {
		t.Errorf("Expected default portfolio value 0, got %v", settings.PortfolioValue)
	}

	// 2. Update settings
	settings.PortfolioValue = 1000000
	err = sm.Save(settings)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// 3. Load again should return cached/saved settings
	settings, err = sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if settings.PortfolioValue != 1000000 {
		t.Errorf("Expected portfolio value 1000000, got %v", settings.PortfolioValue)
	}

	// 4. Verify cache is actually used (by modifying file directly and checking if Load returns old value)
	// Note: sm.cache is private, so we'll just trust the logic for now or use reflection if needed.
	// A better way is to check the cache field if it was exported, but it's not.
}

func TestSettingsManager_InvalidateCacheForcesDiskReload(t *testing.T) {
	root := t.TempDir()

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}
	sm := NewSettingsManager(root, store)

	// Prime the cache (no file yet -> defaults).
	first, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if first.PortfolioValue != 0 {
		t.Fatalf("expected default portfolio value 0, got %v", first.PortfolioValue)
	}

	// Rewrite the settings file behind the manager's back (as a backup
	// restore does). No mtime dependence: Load never checks timestamps,
	// only the in-memory cache.
	updated := models.DefaultWhatIfSettings()
	updated.PortfolioValue = 424242
	data, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := store.WriteFile(filepath.Join(root, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// The cache still serves the pre-rewrite settings.
	stale, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if stale.PortfolioValue != 0 {
		t.Fatalf("expected cached value 0 before invalidation, got %v", stale.PortfolioValue)
	}

	sm.InvalidateCache()

	fresh, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() after InvalidateCache error: %v", err)
	}
	if fresh.PortfolioValue != 424242 {
		t.Fatalf("expected re-read value 424242 after InvalidateCache, got %v", fresh.PortfolioValue)
	}
}

func TestSettingsManager_ConcurrentUpdates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "settings_concurrent_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, _ := storage.New(tmpDir)
	sm := NewSettingsManager(tmpDir, store)

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func(val int) {
			defer wg.Done()
			updates := map[string]interface{}{
				"portfolio_value": float64(val),
			}
			_, _, _ = sm.UpdateSettings(updates)
		}(i)
	}

	wg.Wait()

	// Final load to ensure no race/corruption
	_, err = sm.Load()
	if err != nil {
		t.Errorf("Load() failed after concurrent updates: %v", err)
	}
}

func TestSettingsManager_LoadSelfHealsWhenActiveScenarioFileMissing(t *testing.T) {
	root := t.TempDir()

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}
	sm := NewSettingsManager(root, store)

	// Persist a recognizable default plan (whatif.json).
	base, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	base.PortfolioValue = 111111
	if err := sm.Save(base); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Create + switch to a scenario, then prune its file behind the
	// manager's back (as a full-replace backup restore does) WITHOUT any
	// gate or reconcile call — the next cache-miss load must self-heal.
	if _, err := sm.CreateScenario("Doomed"); err != nil {
		t.Fatalf("CreateScenario() error: %v", err)
	}
	active := sm.ActiveFilename()
	if active == "whatif.json" {
		t.Fatalf("expected non-default active filename after CreateScenario, got %q", active)
	}
	if err := os.Remove(filepath.Join(root, active)); err != nil {
		t.Fatalf("os.Remove() error: %v", err)
	}

	sm.InvalidateCache()

	fresh, err := sm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("LoadContext() after prune error: %v", err)
	}
	if got := sm.ActiveFilename(); got != "whatif.json" {
		t.Fatalf("expected Load to self-heal active filename to whatif.json, got %q", got)
	}
	if fresh.PortfolioValue != 111111 {
		t.Fatalf("expected Load to serve default whatif.json (111111), got %v", fresh.PortfolioValue)
	}
	// The pruned scenario file must not have been resurrected.
	if _, err := os.Stat(filepath.Join(root, active)); !os.IsNotExist(err) {
		t.Fatalf("pruned scenario file should stay gone, stat err=%v", err)
	}
}

func TestSettingsManager_LoadKeepsActiveScenarioWhenFileExists(t *testing.T) {
	root := t.TempDir()

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}
	sm := NewSettingsManager(root, store)

	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	settings.PortfolioValue = 222222
	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if _, err := sm.CreateScenario("Survivor"); err != nil {
		t.Fatalf("CreateScenario() error: %v", err)
	}
	active := sm.ActiveFilename()

	// A cache-miss load while the scenario file exists must NOT revert.
	sm.InvalidateCache()
	loaded, err := sm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("LoadContext() error: %v", err)
	}
	if got := sm.ActiveFilename(); got != active {
		t.Fatalf("expected active filename unchanged (%q), got %q", active, got)
	}
	if loaded.PortfolioValue != 222222 {
		t.Fatalf("expected scenario settings (222222), got %v", loaded.PortfolioValue)
	}
	if loaded.ScenarioName != "Survivor" {
		t.Fatalf("expected scenario name Survivor, got %q", loaded.ScenarioName)
	}
}

func TestSettingsManager_BeginExternalRewriteBlocksSave(t *testing.T) {
	root := t.TempDir()

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}
	sm := NewSettingsManager(root, store)

	end := sm.BeginExternalRewrite()

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 777777
		done <- sm.Save(s)
	}()
	<-started

	// The save must not complete while the gate is held.
	select {
	case err := <-done:
		t.Fatalf("Save completed while external rewrite gate held (err=%v)", err)
	case <-time.After(50 * time.Millisecond):
	}

	end()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Save() after end() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save did not complete after end() released the gate")
	}

	// end() dropped the cache, so the post-gate save's value is served.
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.PortfolioValue != 777777 {
		t.Fatalf("expected post-gate save value 777777, got %v", loaded.PortfolioValue)
	}
}

func TestSettingsManager_ExternalRewriteRevertsPrunedActiveScenario(t *testing.T) {
	root := t.TempDir()

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}
	sm := NewSettingsManager(root, store)

	// Persist a recognizable default plan, then switch to a scenario.
	base, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	base.PortfolioValue = 333333
	if err := sm.Save(base); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if _, err := sm.CreateScenario("Pruned"); err != nil {
		t.Fatalf("CreateScenario() error: %v", err)
	}
	active := sm.ActiveFilename()
	if active == "whatif.json" {
		t.Fatalf("expected non-default active filename, got %q", active)
	}

	// Simulate a full-replace restore pruning the scenario file while the
	// gate is held. No SettingsManager method may run in this window, so
	// the resurrection interleaving from the race finding cannot occur.
	end := sm.BeginExternalRewrite()
	if err := os.Remove(filepath.Join(root, active)); err != nil {
		end()
		t.Fatalf("os.Remove() error: %v", err)
	}
	end()

	if got := sm.ActiveFilename(); got != "whatif.json" {
		t.Fatalf("expected end() to revert active filename to whatif.json, got %q", got)
	}
	loaded, err := sm.LoadContext(context.Background())
	if err != nil {
		t.Fatalf("LoadContext() error: %v", err)
	}
	if loaded.PortfolioValue != 333333 {
		t.Fatalf("expected default whatif.json contents (333333), got %v", loaded.PortfolioValue)
	}
	// The pruned scenario file must not have been resurrected.
	if _, err := os.Stat(filepath.Join(root, active)); !os.IsNotExist(err) {
		t.Fatalf("pruned scenario file should stay gone, stat err=%v", err)
	}
}

func TestSettingsManager_ListScenariosIncludesDefaultWhenMissing(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)

	scenarios, err := sm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios() error: %v", err)
	}

	if len(scenarios) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(scenarios))
	}
	if scenarios[0].Filename != "whatif.json" {
		t.Fatalf("expected default scenario filename, got %q", scenarios[0].Filename)
	}
	if scenarios[0].Name != "Current Plan" {
		t.Fatalf("expected default scenario name, got %q", scenarios[0].Name)
	}
	if !scenarios[0].Active {
		t.Fatal("expected default scenario to be active")
	}
}

func TestSettingsManager_RejectsScenarioPathTraversal(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)

	externalPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(externalPath, []byte(`{"scenario_name":"outside"}`), 0644); err != nil {
		t.Fatalf("os.WriteFile() error: %v", err)
	}

	if err := sm.SwitchScenario("../config.json"); err == nil {
		t.Fatal("expected SwitchScenario to reject path traversal")
	}

	if sm.ActiveFilename() != "whatif.json" {
		t.Fatalf("expected active filename to remain default, got %q", sm.ActiveFilename())
	}
}

func TestSettingsManager_RenameAndDeleteUseValidatedScenarioPath(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)
	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	path := filepath.Join(settingsDir, "whatif_sample.json")
	data, err := json.Marshal(models.DefaultWhatIfSettings())
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if err := store.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	if err := sm.RenameScenario("../whatif_sample.json", "Renamed"); err == nil {
		t.Fatal("expected RenameScenario to reject path traversal")
	}
	if err := sm.DeleteScenario("../whatif_sample.json"); err == nil {
		t.Fatal("expected DeleteScenario to reject path traversal")
	}
}

func TestSettingsManager_SaveOmitsDerivedAgeFields(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")

	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New() error: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: "1961-04", Role: models.PersonRolePrimary},
		{ID: "spouse", Name: "Spouse", BirthMonth: "1963-04", Role: models.PersonRoleSpouse},
	}

	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	raw, err := store.ReadFile(filepath.Join(settingsDir, "whatif.json"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	text := string(raw)
	if strings.Contains(text, `"current_age"`) {
		t.Fatal("saved settings should not persist current_age")
	}
	if strings.Contains(text, `"spouse_age"`) {
		t.Fatal("saved settings should not persist spouse_age")
	}
	if !strings.Contains(text, `"start_date"`) || !strings.Contains(text, `"persons"`) {
		t.Fatal("saved settings should persist start_date and persons")
	}
}

func TestRevision_BumpsOnSave(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)

	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := sm.Revision()
	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := sm.Revision(); got <= before {
		t.Fatalf("Revision did not advance across Save: %d -> %d", before, got)
	}
}

func TestRevision_BumpsOnSwitchAndCreateScenario(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Persist the default whatif.json first: Load() on a not-yet-existing file
	// returns in-memory defaults without writing them, so SwitchScenario back
	// to "whatif.json" below would otherwise fail with file-not-found. Every
	// other test in this file that follows Load with CreateScenario does the
	// same (see TestSettingsManager_LoadSelfHealsWhenActiveScenarioFileMissing).
	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	beforeCreate := sm.Revision()
	if _, err := sm.CreateScenario("alt"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	afterCreate := sm.Revision()
	if afterCreate <= beforeCreate {
		t.Fatalf("CreateScenario did not bump: %d -> %d", beforeCreate, afterCreate)
	}

	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}
	if got := sm.Revision(); got <= afterCreate {
		t.Fatalf("SwitchScenario did not bump: %d -> %d", afterCreate, got)
	}
}

func TestRevision_DoesNotBumpOnCacheMissLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	sm.InvalidateCache()
	afterInvalidate := sm.Revision()

	// A cache-miss load may internally re-save when decode reports a migration.
	// That is a read, not a change: the page must not re-render for it.
	if _, err := sm.Load(); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := sm.Revision(); got != afterInvalidate {
		t.Fatalf("a cache-miss load bumped the revision: %d -> %d", afterInvalidate, got)
	}
}

// TestRevision_BumpsWhenTheActiveScenarioIsReverted is the counterpart to the
// test above: a cache-miss load that merely migrates must not bump, but one
// that REVERTS the active scenario must. Without the bump the server starts
// serving a different plan while every open page keeps rendering the old one
// and keeps displaying the old scenario name — the page states something about
// the user's plan that is no longer true, with nothing to correct it short of
// a reload.
func TestRevision_BumpsWhenTheActiveScenarioIsReverted(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(root, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := sm.CreateScenario("Doomed"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	active := sm.ActiveFilename()
	if active == "whatif.json" {
		t.Fatalf("expected a non-default active scenario, got %q", active)
	}
	// Prune the active scenario's file behind the manager's back, exactly as
	// a full-replace backup restore does.
	if err := os.Remove(filepath.Join(root, active)); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}

	sm.InvalidateCache()
	before := sm.Revision()

	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load after prune: %v", err)
	}
	if got := sm.ActiveFilename(); got != "whatif.json" {
		t.Fatalf("expected the reconcile to revert to whatif.json, got %q", got)
	}
	if got := sm.Revision(); got == before {
		t.Fatalf("reverting the active scenario did not bump the revision (still %d); open pages would keep showing %q", got, active)
	}
}

func TestRevision_RaceClean(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.Save(settings)
			_ = sm.Revision()
		}()
	}
	wg.Wait()
	if sm.Revision() < 8 {
		t.Fatalf("expected at least 8 bumps, got %d", sm.Revision())
	}
}

func TestApplyOverrides_PersistsAndReturnsItsOwnRevision(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := 4321.0
	saved, scenario, rev, err := sm.ApplyOverrides(overrides.Overrides{MonthlyLivingExpenses: &want}, "")
	if err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}
	if saved.MonthlyLivingExpenses != want {
		t.Fatalf("returned settings has %v, want %v", saved.MonthlyLivingExpenses, want)
	}
	if scenario != "whatif.json" {
		t.Fatalf("returned scenario = %q, want %q", scenario, "whatif.json")
	}
	if rev == 0 {
		t.Fatal("ApplyOverrides returned revision 0")
	}

	sm.InvalidateCache()
	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.MonthlyLivingExpenses != want {
		t.Fatalf("value did not persist: got %v, want %v", reloaded.MonthlyLivingExpenses, want)
	}
}

// TestApplyOverrides_RefusesWhenExpectedScenarioIsNotActive pins the guard
// that turns the MCP's collision detection into collision PREVENTION. The
// comparison happens inside the write lock, before the load and before the
// save, so a scenario switch racing the caller cannot land a write on a plan
// the caller never snapshotted -- and the error is typed so the HTTP layer
// answers 409 rather than 500.
func TestApplyOverrides_RefusesWhenExpectedScenarioIsNotActive(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	baseline, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantExpenses := baseline.MonthlyLivingExpenses

	value := 9999.0
	_, scenario, rev, err := sm.ApplyOverrides(
		overrides.Overrides{MonthlyLivingExpenses: &value}, "whatif_not-the-active-one.json")
	if err == nil {
		t.Fatal("expected a refusal when the expected scenario is not the active one")
	}
	var conflict *ScenarioConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error type = %T, want *ScenarioConflictError so the handler answers 409: %v", err, err)
	}
	if scenario != "" || rev != 0 {
		t.Fatalf("a refused apply must report no scenario and no revision, got %q and %d", scenario, rev)
	}

	sm.InvalidateCache()
	after, err := sm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.MonthlyLivingExpenses != wantExpenses {
		t.Fatalf("a refused apply still wrote: %v -> %v", wantExpenses, after.MonthlyLivingExpenses)
	}
}

// The regression this whole design exists to prevent.
func TestApplyOverrides_DoesNotLoseAConcurrentUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Each goroutine records the exact value it last wrote successfully. A
	// static floor is too weak here: Load() returns the live sm.cache
	// pointer, so a naive Load->Apply->Save sequence's staleness window is
	// only microseconds wide, and a revert costs at most the last handful of
	// the other goroutine's increments -- nowhere near a floor set near the
	// start of either range. Asserting exact equality against each
	// goroutine's own last write catches a revert of any size, deterministically.
	var lastRothAmount, lastMonthlyExpenses float64

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			amount := float64(10000 + i)
			if _, _, _, err := sm.ApplyOverrides(overrides.Overrides{RothConversionAmount: &amount}, ""); err != nil {
				t.Errorf("ApplyOverrides: %v", err)
				return
			}
			lastRothAmount = amount
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			expense := float64(3000 + i)
			if _, _, err := sm.UpdateSettings(map[string]interface{}{
				"monthly_living_expenses": expense,
			}); err != nil {
				t.Errorf("UpdateSettings: %v", err)
				return
			}
			lastMonthlyExpenses = expense
		}
	}()
	wg.Wait()

	sm.InvalidateCache()
	final, err := sm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// Both writers touched disjoint fields. If the apply path did a
	// read-modify-write outside the lock, one writer's field reverts to
	// something earlier than its own last successful write.
	if final.MonthlyLivingExpenses != lastMonthlyExpenses {
		t.Fatalf("UpdateSettings' field was lost: got %v, want its own last write %v", final.MonthlyLivingExpenses, lastMonthlyExpenses)
	}
	if final.RothConversion == nil || final.RothConversion.AnnualAmount != lastRothAmount {
		t.Fatalf("ApplyOverrides' field was lost: got %+v, want AnnualAmount %v", final.RothConversion, lastRothAmount)
	}
}
