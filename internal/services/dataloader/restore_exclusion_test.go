package dataloader

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// blockWindow is how long a test waits before concluding an operation is
// genuinely blocked rather than merely slow. Long enough that a loaded machine
// does not report a false block, short enough that a real deadlock in the
// non-blocking direction still fails the suite quickly.
const blockWindow = 250 * time.Millisecond

// TestSidecarSequenceExcludesARestore is the falsification test for the
// read-modify-write race.
//
// A sidecar update reads a JSON file, edits the decoded value in memory, and
// writes it back. Storage locked only around the write, so a restore could be
// granted the exclusive hold in between: it replaces the file on disk, and
// then the sequence writes back a value derived from the pre-restore contents
// — resurrecting the expense the restore had just rolled back, with the
// restore reporting success and nothing logged anywhere.
//
// The test stops the sequence in exactly that window, using the hook that
// fires after the active list is loaded and before it is saved, and asserts
// that a restore cannot take the data directory until the sequence finishes.
func TestSidecarSequenceExcludesARestore(t *testing.T) {
	dl := newRaceLoader(t)

	loaded := make(chan struct{})
	resume := make(chan struct{})
	testHookAfterExpenseLoad = func() {
		testHookAfterExpenseLoad = nil // fire once
		close(loaded)
		<-resume
	}
	t.Cleanup(func() { testHookAfterExpenseLoad = nil })

	addDone := make(chan error, 1)
	go func() {
		_, err := dl.AddMajorExpense(models.MajorExpense{ID: "e1", Name: "Roof"})
		addDone <- err
	}()

	select {
	case <-loaded:
	case <-time.After(5 * time.Second):
		t.Fatal("AddMajorExpense never reached the post-load hook")
	}

	// The sequence is now parked between its load and its save. A restore
	// must not be able to rewrite the directory here.
	restoreHeld := make(chan struct{})
	go func() {
		w := dl.store.BeginExclusive()
		w.Release()
		close(restoreHeld)
	}()

	select {
	case <-restoreHeld:
		t.Fatal("a restore acquired the exclusive hold between the sequence's load and its save; " +
			"the save would then persist pre-restore data over the restored file")
	case <-time.After(blockWindow):
	}

	close(resume)
	if err := <-addDone; err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}

	// The other direction: once the sequence is done the hold must be
	// grantable, or this test would also pass against a permanent deadlock.
	select {
	case <-restoreHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("the exclusive hold was never granted after the sequence completed — deadlock")
	}
}

// TestSidecarSequenceCompletesWhileARestoreWaits is the liveness half. Nothing
// in a sequence may call back into the plain Storage write methods: those take
// the shared lock the sequence already holds, and sync.RWMutex is not
// reentrant, so a second acquisition while a restore is queued for the write
// lock blocks forever.
//
// ArchiveMajorExpense is the strongest case in the package — it reads and
// writes three separate sidecar files in one sequence. If any of those steps
// went to dl.store instead of the transaction, this test would hang rather
// than fail.
func TestSidecarSequenceCompletesWhileARestoreWaits(t *testing.T) {
	dl := newRaceLoader(t)

	if _, err := dl.AddMajorExpense(models.MajorExpense{ID: "e1", Name: "Roof"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if err := dl.SetTransactionPin("hash-1", "e1"); err != nil {
		t.Fatalf("SetTransactionPin: %v", err)
	}

	// Park the archive between its first write and its second, then queue a
	// restore behind it so every remaining step runs with a writer waiting.
	midArchive := make(chan struct{})
	resume := make(chan struct{})
	testHookMidArchive = func() {
		testHookMidArchive = nil
		close(midArchive)
		<-resume
	}
	t.Cleanup(func() { testHookMidArchive = nil })

	archiveDone := make(chan error, 1)
	go func() { archiveDone <- dl.ArchiveMajorExpense("e1") }()

	select {
	case <-midArchive:
	case <-time.After(5 * time.Second):
		t.Fatal("ArchiveMajorExpense never reached the mid-archive hook")
	}

	restoreHeld := make(chan struct{})
	go func() {
		w := dl.store.BeginExclusive()
		w.Release()
		close(restoreHeld)
	}()
	time.Sleep(blockWindow) // let the exclusive request actually queue

	close(resume)
	select {
	case err := <-archiveDone:
		if err != nil {
			t.Fatalf("ArchiveMajorExpense: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ArchiveMajorExpense deadlocked with a restore queued behind it — " +
			"a step inside the sequence took the shared lock a second time")
	}

	select {
	case <-restoreHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("the exclusive hold was never granted after the sequence completed")
	}

	// The archive actually happened, so the test is not passing on a no-op.
	active, err := dl.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active expenses=%d, want 0 after archiving the only one", len(active))
	}
	deleted, err := dl.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("LoadDeletedMajorExpenses: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("archived expenses=%d, want 1", len(deleted))
	}
	if got := deleted[0].PinnedHashes; len(got) != 1 || got[0] != "hash-1" {
		t.Errorf("archived pinned hashes=%v, want [hash-1]", got)
	}
}
