package restore

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// hookHolder runs hook from inside SnapshotAndHold. A restore calls that while
// it already holds the data directory exclusively, so the hook is a seam into
// the middle of the critical section -- without any test-only field on the
// production type.
type hookHolder struct {
	inner SnapshotHolder
	hook  func()
}

func (h hookHolder) SnapshotAndHold(ctx context.Context) (func(), error) {
	h.hook()
	return h.inner.SnapshotAndHold(ctx)
}

// blockWindow: long enough that a loaded machine does not report a false
// block, short enough that the failing direction fails fast.
const blockWindow = 250 * time.Millisecond

// The 4a race, as a test. A restore's write+prune must not interleave with an
// ordinary data-directory write: a file written between the rewrite and the
// prune walk is a file the prune deletes, and the writer (an MCP tool, a page
// handler) never learns its write was undone.
//
// Asserted from the other side of the lock, which needs no hook inside the
// restore: while something else holds the data directory exclusively, a
// restore must wait its turn.
func TestFromZipWaitsForTheDataDirectoryGate(t *testing.T) {
	s, dir := newService(t)
	if err := os.WriteFile(filepath.Join(dir, "stale.csv"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	content := zipOf(t, map[string]string{"fresh.csv": "Date,Amount\n"})

	w := s.deps.Store.BeginExclusive()

	done := make(chan error, 1)
	go func() {
		_, err := s.FromZip(context.Background(), content)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("the restore ran while another writer held the data directory (err = %v); "+
			"a concurrent write can land in its prune window", err)
	case <-time.After(blockWindow):
	}

	// A write through the hold is exactly the MCP-tool write the prune used to
	// delete. It must survive the restore that is queued behind it.
	late := filepath.Join(dir, "written-before-the-restore-got-the-lock.csv")
	if err := w.WriteFile(late, []byte("Date,Amount\n"), 0o644); err != nil {
		t.Fatalf("write through the hold: %v", err)
	}
	w.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FromZip after Release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FromZip never completed after Release; the restore deadlocked on the gate")
	}
}

// This is the 4a race. An ordinary write -- an MCP curation tool, a page
// handler -- that arrives while a restore is running must not be able to land
// between the rewrite and the prune walk, because the prune deletes anything
// the archive did not contain and the writer is never told.
//
// The write must instead wait for the restore and survive it. Both halves are
// asserted: that it does NOT complete during the restore, and that it DOES
// complete and survive afterwards.
func TestAnOrdinaryWriteCannotLandInsideARestore(t *testing.T) {
	s, dir := newService(t)
	if err := os.WriteFile(filepath.Join(dir, "stale.csv"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	late := filepath.Join(dir, "written-during-the-restore.csv")

	wrote := make(chan error, 1)
	blocked := make(chan struct{})
	s.deps.Backups = hookHolder{
		inner: s.deps.Backups,
		hook: func() {
			go func() { wrote <- s.deps.Store.WriteFile(late, []byte("Date,Amount\n"), 0o644) }()
			select {
			case err := <-wrote:
				// t.Errorf, not t.Fatalf: this runs on a goroutine the test
				// does not own. Put the result back so the assertion after the
				// restore does not then block for its full timeout on a
				// channel this branch already drained.
				t.Errorf("a write completed inside the restore's critical section (err = %v); "+
					"the prune will delete it and the writer will never know", err)
				wrote <- err
			case <-time.After(blockWindow):
			}
			close(blocked)
		},
	}

	res, err := s.FromZip(context.Background(), zipOf(t, map[string]string{
		"fresh.csv": "Date,Amount\n2024-01-01,-1.00\n",
	}))
	if err != nil {
		t.Fatalf("FromZip: %v", err)
	}
	<-blocked

	select {
	case err := <-wrote:
		if err != nil {
			t.Fatalf("the queued write failed after the restore: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the queued write never completed; the restore did not release the data directory")
	}

	// It landed after the prune, so it is still there -- and the restore's own
	// work is intact.
	if _, err := os.Stat(late); err != nil {
		t.Errorf("the queued write was lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh.csv")); err != nil {
		t.Errorf("the restored file is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.csv")); !os.IsNotExist(err) {
		t.Errorf("the prune did not run (stale.csv survived): %v", err)
	}
	if res.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1", res.Pruned)
	}
}

// The safety snapshot must be taken with the data directory already held, or
// a write landing between the snapshot and the hold is deleted by the prune
// AND absent from the snapshot -- the one failure with no copy anywhere.
func TestTheSafetySnapshotIsTakenUnderTheHold(t *testing.T) {
	s, dir := newService(t)
	held := make(chan bool, 1)
	// Owned by the test, not the hook, so the probe is waited for before the
	// test returns -- an unwaited goroutine still writing into t.TempDir()
	// races the cleanup that removes it.
	probed := make(chan struct{})
	s.deps.Backups = hookHolder{
		inner: s.deps.Backups,
		hook: func() {
			go func() {
				_ = s.deps.Store.WriteFile(filepath.Join(dir, "probe.csv"), []byte("x"), 0o644)
				close(probed)
			}()
			select {
			case <-probed:
				held <- false
			case <-time.After(blockWindow):
				held <- true
			}
		},
	}

	if _, err := s.FromZip(context.Background(), zipOf(t, map[string]string{"a.csv": "x"})); err != nil {
		t.Fatalf("FromZip: %v", err)
	}
	if !<-held {
		t.Error("the data directory was not held while the safety snapshot ran")
	}
	select {
	case <-probed:
	case <-time.After(10 * time.Second):
		t.Fatal("the probe write never completed after the restore released the data directory")
	}
}

// The ABBA deadlock this lock order exists to prevent.
//
// SettingsManager.SaveWithRevision holds the manager's lock across its write
// through Storage -- settings, then data. A restore that took the data
// directory before the settings gate would close the cycle: the restore
// holding the data directory while waiting for the settings lock, the
// in-flight save holding the settings lock while waiting for the data
// directory. Nothing recovers from that; the server hangs until it is killed.
//
// Staged as it actually happens: the save is already in flight when the
// restore starts. The discriminating assertion is that the save can still
// complete its write while the restore waits -- which is only true if the
// restore is waiting at the settings gate holding nothing else.
func TestRestoreDoesNotDeadlockAgainstAConcurrentSettingsSave(t *testing.T) {
	s, dir := newService(t)

	// settingsLock stands in for SettingsManager.mu, with the same discipline:
	// the gate holds it, and a save holds it across a write through Storage.
	var settingsLock sync.Mutex
	s.deps.Gate = RewriteGateFunc(func() func() {
		settingsLock.Lock()
		return settingsLock.Unlock
	})

	// A save is in flight: it holds the settings lock and has not yet written.
	settingsLock.Lock()

	done := make(chan error, 1)
	go func() {
		_, err := s.FromZip(context.Background(), zipOf(t, map[string]string{"a.csv": "x"}))
		done <- err
	}()
	// Give the restore time to reach whichever lock it takes first. Without
	// this the bad order might not have acquired the data directory yet, and
	// the deadlock it causes would go unobserved.
	time.Sleep(blockWindow)

	wrote := make(chan error, 1)
	go func() {
		wrote <- s.deps.Store.WriteFile(filepath.Join(dir, "settings", "whatif.json"), []byte("{}"), 0o644)
	}()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatalf("the in-flight save could not write: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DEADLOCK: the in-flight save cannot write because the restore holds the data " +
			"directory while waiting for the settings lock; lock order must be settings gate -> " +
			"data directory -> snapshot hold")
	}
	settingsLock.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FromZip: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the restore never finished after the save released the settings lock")
	}
}
