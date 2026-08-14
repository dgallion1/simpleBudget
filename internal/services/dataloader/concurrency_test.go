package dataloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"budget2/internal/models"
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
//
// This test passes unconditionally under a plain `go test` -- there is no
// value assertion for a data race to trip. It only has teeth under
// `make race` (or `go test -race ./internal/services/dataloader/`), which
// this repo deliberately keeps out of the per-commit `make check`.
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

// TestConcurrentPinWritesDoNotLoseUpdates pins 32 distinct hashes from 32
// goroutines. Each call is a load->modify->save over one file, so without a
// lock around the whole sequence the later writers save a map they read
// before the earlier writers' changes landed, and pins vanish.
func TestConcurrentPinWritesDoNotLoseUpdates(t *testing.T) {
	loader := newRaceLoader(t)

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("hash-%02d", i)
			if _, err := loader.SetTransactionPins(map[string]string{hash: "expense-1"}); err != nil {
				t.Errorf("SetTransactionPins(%s): %v", hash, err)
			}
		}(i)
	}
	wg.Wait()

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if len(pins) != n {
		t.Fatalf("pins on disk = %d, want %d -- concurrent writes lost updates", len(pins), n)
	}
	for i := 0; i < n; i++ {
		hash := fmt.Sprintf("hash-%02d", i)
		if pins[hash] != "expense-1" {
			t.Errorf("pin %s = %q, want %q", hash, pins[hash], "expense-1")
		}
	}
}

// TestArchiveCannotInterleaveWithAdd is deterministic, not probabilistic.
// It parks AddMajorExpense between its load and its save, then starts an
// ArchiveMajorExpense and asserts the archive makes NO progress while the add
// holds the write lock. Without the lock the archive runs to completion
// during the park, its active-list write is then overwritten by the add's
// stale [A,B] + C, and B ends up in both files.
func TestArchiveCannotInterleaveWithAdd(t *testing.T) {
	loader := newRaceLoader(t)

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "A", Name: "Rent"}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "B", Name: "Insurance"}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	parked := make(chan struct{})
	release := make(chan struct{})
	testHookAfterExpenseLoad = func() {
		testHookAfterExpenseLoad = nil // fire once; the archive path must not re-trigger it
		close(parked)
		<-release
	}
	t.Cleanup(func() { testHookAfterExpenseLoad = nil })

	addDone := make(chan error, 1)
	go func() {
		_, err := loader.AddMajorExpense(models.MajorExpense{ID: "C", Name: "Utilities"})
		addDone <- err
	}()
	<-parked

	archiveDone := make(chan error, 1)
	go func() { archiveDone <- loader.ArchiveMajorExpense("B") }()

	select {
	case err := <-archiveDone:
		t.Fatalf("ArchiveMajorExpense completed (err=%v) while AddMajorExpense held the write lock", err)
	case <-time.After(200 * time.Millisecond):
		// Correct: the archive is blocked on writeMu.
	}

	close(release)
	if err := <-addDone; err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if err := <-archiveDone; err != nil {
		t.Fatalf("ArchiveMajorExpense: %v", err)
	}

	active, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("LoadDeletedMajorExpenses: %v", err)
	}
	activeIDs := map[string]bool{}
	for _, e := range active {
		activeIDs[e.ID] = true
	}
	for _, d := range deleted {
		if activeIDs[d.Expense.ID] {
			t.Fatalf("expense %s is in BOTH the active list and the archive", d.Expense.ID)
		}
	}
	if !activeIDs["C"] {
		t.Error("the added expense C is missing from the active list")
	}
	if activeIDs["B"] {
		t.Error("the archived expense B is still in the active list")
	}
}
