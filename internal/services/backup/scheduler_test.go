package backup

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	now atomic.Value // time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	c := &fakeClock{}
	c.now.Store(start)
	return c
}
func (c *fakeClock) Now() time.Time  { return c.now.Load().(time.Time) }
func (c *fakeClock) Set(t time.Time) { c.now.Store(t) }

func TestScheduler_RunsImmediateThenWaitsForTick(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	clk := newFakeClock(start)
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go svc.runWith(ctx, ticks, 24*time.Hour)

	// Allow the goroutine to do its initial SnapshotIfStale (no prior backup
	// → fires).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
		if len(zips) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("expected 1 zip after initial run, got %d", len(zips))
	}

	// Advance clock 25 hours and tick — second snapshot should run.
	clk.Set(start.Add(25 * time.Hour))
	ticks <- clk.Now()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
		if len(zips) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	zips, _ = filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) < 2 {
		t.Fatalf("expected ≥2 zips after stale tick, got %d", len(zips))
	}
}

func TestScheduler_DisabledFlagShortCircuits(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	clk := newFakeClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err := svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go svc.runWith(ctx, ticks, 24*time.Hour)

	// Wait briefly; no zip should appear.
	time.Sleep(100 * time.Millisecond)
	ticks <- clk.Now().Add(48 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 0 {
		t.Fatalf("disabled scheduler should not snapshot; got %d zips", len(zips))
	}
}

func TestScheduler_CtxCancelExits(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.runWith(ctx, make(chan time.Time), 24*time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit on ctx cancel")
	}
}
