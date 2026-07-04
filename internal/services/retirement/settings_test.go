package retirement

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"budget2/internal/models"
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
			_, _ = sm.UpdateSettings(updates)
		}(i)
	}

	wg.Wait()

	// Final load to ensure no race/corruption
	_, err = sm.Load()
	if err != nil {
		t.Errorf("Load() failed after concurrent updates: %v", err)
	}
}

func TestSettingsManager_ReconcileActiveScenario_RevertsWhenFileMissing(t *testing.T) {
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
	// manager's back (as a full-replace backup restore does).
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
	sm.ReconcileActiveScenario()

	if got := sm.ActiveFilename(); got != "whatif.json" {
		t.Fatalf("expected active filename reverted to whatif.json, got %q", got)
	}
	fresh, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() after reconcile error: %v", err)
	}
	if fresh.PortfolioValue != 111111 {
		t.Fatalf("expected Load to serve restored whatif.json (111111), got %v", fresh.PortfolioValue)
	}
	// The pruned scenario file must not have been resurrected.
	if _, err := os.Stat(filepath.Join(root, active)); !os.IsNotExist(err) {
		t.Fatalf("pruned scenario file should stay gone, stat err=%v", err)
	}
}

func TestSettingsManager_ReconcileActiveScenario_NoOpWhenFileExists(t *testing.T) {
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

	sm.ReconcileActiveScenario()

	if got := sm.ActiveFilename(); got != active {
		t.Fatalf("expected active filename unchanged (%q), got %q", active, got)
	}
	// InvalidateCache still forces a disk re-read of the scenario file.
	sm.InvalidateCache()
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.PortfolioValue != 222222 {
		t.Fatalf("expected scenario settings (222222), got %v", loaded.PortfolioValue)
	}
	if loaded.ScenarioName != "Survivor" {
		t.Fatalf("expected scenario name Survivor, got %q", loaded.ScenarioName)
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
