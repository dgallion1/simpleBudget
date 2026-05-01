package backup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNew_HoldsConfigAndDir(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: t.TempDir()})
	if err != nil { t.Fatal(err) }
	if svc.BackupDir() != dir {
		t.Fatalf("BackupDir=%q want %q", svc.BackupDir(), dir)
	}
}

func TestSnapshot_MutexReturnsErrSnapshotInProgress(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }

	// Hold the mutex by manually locking it (simulates an in-flight snapshot).
	svc.mu.Lock()
	defer svc.mu.Unlock()

	err = svc.Snapshot(context.Background())
	if !errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("got %v want ErrSnapshotInProgress", err)
	}
}

func TestSnapshot_ConcurrentInvocationsSerialize(t *testing.T) {
	// Two concurrent Snapshot calls should produce: one success and either
	// (a) a second success or (b) one ErrSnapshotInProgress. Never two
	// in-flight at once.
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }

	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results <- svc.Snapshot(context.Background()) }()
	go func() { defer wg.Done(); results <- svc.Snapshot(context.Background()) }()
	wg.Wait()
	close(results)

	var ok, busy int
	for r := range results {
		switch {
		case r == nil:
			ok++
		case errors.Is(r, ErrSnapshotInProgress):
			busy++
		default:
			t.Fatalf("unexpected error: %v", r)
		}
	}
	if ok < 1 {
		t.Fatalf("expected at least one successful snapshot; ok=%d busy=%d", ok, busy)
	}
}

func TestEnabled_DefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(Config{BackupDir: dir, DataDir: t.TempDir()})
	if !svc.Enabled() {
		t.Fatalf("Enabled() default should be true")
	}
}

func TestSetEnabled_PersistsAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, _ := New(Config{BackupDir: dir, DataDir: dataDir})
	if err := svc.SetEnabled(false); err != nil { t.Fatal(err) }

	// Recreate the service and confirm persistence.
	svc2, _ := New(Config{BackupDir: dir, DataDir: dataDir})
	if svc2.Enabled() {
		t.Fatalf("Enabled() should persist as false across restarts")
	}
}

// Stubs used by other test files in this package — declare here to share.
var _ = time.Hour
