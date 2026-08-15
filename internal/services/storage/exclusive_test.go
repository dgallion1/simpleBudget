package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// blockWindow is how long a test waits before concluding that an operation is
// genuinely blocked rather than merely slow. Long enough that a machine under
// load does not report a false block, short enough that a real deadlock in the
// non-blocking direction still fails the suite quickly.
const blockWindow = 250 * time.Millisecond

func newStore(t *testing.T) (*Storage, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, dir
}

// The whole point of the hold: a write cannot land while a restore owns the
// data directory, because a write that lands mid-rewrite is a write the
// prune deletes.
func TestExclusiveWriterBlocksOrdinaryWrites(t *testing.T) {
	s, dir := newStore(t)
	w := s.BeginExclusive()

	done := make(chan error, 1)
	go func() { done <- s.WriteFile(filepath.Join(dir, "late.csv"), []byte("x"), 0o644) }()

	select {
	case err := <-done:
		t.Fatalf("WriteFile completed while an exclusive hold was open (err = %v)", err)
	case <-time.After(blockWindow):
	}

	w.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteFile after Release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WriteFile never completed after Release; the hold did not unwind")
	}
	if _, err := os.Stat(filepath.Join(dir, "late.csv")); err != nil {
		t.Errorf("the blocked write did not land after Release: %v", err)
	}
}

func TestExclusiveWriterBlocksRemovesAndMkdirs(t *testing.T) {
	s, dir := newStore(t)
	victim := filepath.Join(dir, "victim.csv")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	w := s.BeginExclusive()

	removed := make(chan error, 1)
	made := make(chan error, 1)
	go func() { removed <- s.Remove(victim) }()
	go func() { made <- s.MkdirAll(filepath.Join(dir, "sub"), 0o755) }()

	select {
	case <-removed:
		t.Fatal("Remove completed while an exclusive hold was open")
	case <-made:
		t.Fatal("MkdirAll completed while an exclusive hold was open")
	case <-time.After(blockWindow):
	}

	w.Release()
	for i := 0; i < 2; i++ {
		select {
		case err := <-removed:
			if err != nil {
				t.Errorf("Remove after Release: %v", err)
			}
		case err := <-made:
			if err != nil {
				t.Errorf("MkdirAll after Release: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an operation never completed after Release")
		}
	}
}

// The holder must be able to write, or the exclusion is useless: this is the
// reentrancy trap the *Locked helpers exist to avoid.
func TestExclusiveWriterCanWriteWhileHolding(t *testing.T) {
	s, dir := newStore(t)
	w := s.BeginExclusive()
	defer w.Release()

	done := make(chan error, 1)
	go func() {
		if err := w.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			done <- err
			return
		}
		if err := w.WriteFile(filepath.Join(dir, "sub", "a.csv"), []byte("x"), 0o644); err != nil {
			done <- err
			return
		}
		done <- w.Remove(filepath.Join(dir, "sub", "a.csv"))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("writing through the hold: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the holder deadlocked on its own write")
	}
}

// A second exclusive hold must wait for the first. Two restores overlapping
// is the case the backup service's snapshot hold already prevents, but this
// lock must not be the one that lets them through.
func TestExclusiveWriterExcludesASecondHolder(t *testing.T) {
	s, _ := newStore(t)
	first := s.BeginExclusive()

	second := make(chan *ExclusiveWriter, 1)
	go func() { second <- s.BeginExclusive() }()

	select {
	case <-second:
		t.Fatal("a second exclusive hold was granted while the first was open")
	case <-time.After(blockWindow):
	}

	first.Release()
	select {
	case w := <-second:
		w.Release()
	case <-time.After(5 * time.Second):
		t.Fatal("the second hold was never granted")
	}
}

// Release is idempotent so an explicit release followed by a deferred one
// cannot unlock a mutex twice -- which panics and takes the process with it.
func TestReleaseIsIdempotent(t *testing.T) {
	s, _ := newStore(t)
	w := s.BeginExclusive()
	w.Release()
	w.Release()

	// And the lock really is free afterwards.
	done := make(chan struct{})
	go func() { s.BeginExclusive().Release(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the data directory stayed locked after a double Release")
	}
}

// Writing through a released hold is a programming error, and it must be
// reported rather than silently writing outside the exclusion.
func TestWritingThroughAReleasedHoldIsRefused(t *testing.T) {
	s, dir := newStore(t)
	w := s.BeginExclusive()
	w.Release()

	if err := w.WriteFile(filepath.Join(dir, "a.csv"), []byte("x"), 0o644); err == nil {
		t.Error("WriteFile through a released hold was allowed")
	}
	if err := w.Remove(filepath.Join(dir, "a.csv")); err == nil {
		t.Error("Remove through a released hold was allowed")
	}
	if err := w.MkdirAll(filepath.Join(dir, "sub"), 0o755); err == nil {
		t.Error("MkdirAll through a released hold was allowed")
	}
}
