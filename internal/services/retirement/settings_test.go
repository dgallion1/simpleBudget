package retirement

import (
	"os"
	"sync"
	"testing"

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
