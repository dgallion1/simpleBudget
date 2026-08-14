package dataloader

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"budget2/internal/services/storage"
)

// newRaceLoader builds a loader over a temp dir holding one small CSV, so
// LoadData does real work (and really stamps the derived fields) without
// the test depending on a fixture.
func newRaceLoader(t *testing.T) *DataLoader {
	t.Helper()
	dir := t.TempDir()
	csv := "Date,Description,Amount,Status\n" +
		"2024-01-05,CHECK #1001,-250.00,Posted\n" +
		"2024-01-03,ACME BILL PAY,-250.00,Scheduled\n" +
		"2024-01-09,GROCERY STORE,-42.10,\n"
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return New(dir, store)
}

// TestDerivedStateIsRaceFree exercises the fields LoadData stamps against
// the accessors that read them, plus the enabled-files map an HTTP handler
// can rewrite mid-load. It asserts nothing about values -- the assertion is
// the race detector's, and this test is only meaningful under -race.
func TestDerivedStateIsRaceFree(t *testing.T) {
	loader := newRaceLoader(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := loader.LoadData(); err != nil {
				t.Errorf("LoadData: %v", err)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.UnresolvedDuplicateCount()
			_ = loader.UnresolvedDuplicates()
			_ = loader.ResolvedDuplicates()
			_ = loader.FilteredTransfers()
			if _, err := loader.GetFileInfo(); err != nil {
				t.Errorf("GetFileInfo: %v", err)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loader.SetEnabledFiles([]string{"a.csv"})
		}()
	}
	wg.Wait()
}
