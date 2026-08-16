package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateExclusiveRefusesAnExistingFile(t *testing.T) {
	s, dir := newStore(t)
	path := filepath.Join(dir, "ledger.csv")

	if err := s.CreateExclusive(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first CreateExclusive: %v", err)
	}

	err := s.CreateExclusive(path, []byte("second"), 0644)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("second CreateExclusive: err=%v, want one satisfying errors.Is(err, os.ErrExist)", err)
	}

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "first" {
		t.Errorf("contents=%q, want %q — the refused write must not have touched the file", got, "first")
	}
}

// TestCreateExclusiveLeavesNoStagingFileBehind guards the implementation
// detail that makes the create atomic: the payload is staged beside its
// destination before being linked into place. A refused or failed create must
// not leave that staging file in the data directory, where the file manager
// would list it.
func TestCreateExclusiveLeavesNoStagingFileBehind(t *testing.T) {
	s, dir := newStore(t)
	path := filepath.Join(dir, "ledger.csv")

	if err := s.CreateExclusive(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first CreateExclusive: %v", err)
	}
	if err := s.CreateExclusive(path, []byte("second"), 0644); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second CreateExclusive: err=%v, want os.ErrExist", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if name := e.Name(); name != "ledger.csv" && name[0] != '.' {
			t.Errorf("leftover file %q in the data directory", name)
		}
	}
}

// TestCreateExclusiveLetsExactlyOneConcurrentWriterWin is the race the
// replaced Stat-then-WriteFile sequence lost. Both callers could observe
// "absent" and then both write, so the second silently destroyed the first.
// Run with -race.
func TestCreateExclusiveLetsExactlyOneConcurrentWriterWin(t *testing.T) {
	const writers = 8

	s, dir := newStore(t)
	path := filepath.Join(dir, "ledger.csv")

	var (
		mu       sync.Mutex
		winners  []string
		unwanted []error
		wg       sync.WaitGroup
		start    = make(chan struct{})
	)

	for i := 0; i < writers; i++ {
		payload := fmt.Sprintf("payload-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := s.CreateExclusive(path, []byte(payload), 0644)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, payload)
			case errors.Is(err, os.ErrExist):
				// Expected for every loser.
			default:
				unwanted = append(unwanted, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range unwanted {
		t.Errorf("unexpected error: %v (want nil or os.ErrExist)", err)
	}
	if len(winners) != 1 {
		t.Fatalf("%d writers succeeded, want exactly 1: %v", len(winners), winners)
	}

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != winners[0] {
		t.Errorf("contents=%q, want the winner's payload %q — a loser overwrote it", got, winners[0])
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if name := e.Name(); name != "ledger.csv" && name[0] != '.' {
			t.Errorf("leftover file %q from a losing writer", name)
		}
	}
}

// TestCreateExclusiveEncryptsLikeWriteFile checks that the new write path
// applies the same encryption rule as the existing one, rather than saving a
// plaintext file into an encrypted store.
func TestCreateExclusiveEncryptsLikeWriteFile(t *testing.T) {
	s, dir := newStore(t)
	if err := s.EnableEncryption("correct-horse-battery-staple"); err != nil {
		t.Skipf("encryption unavailable in this environment: %v", err)
	}

	path := filepath.Join(dir, "ledger.csv")
	if err := s.CreateExclusive(path, []byte("date,amount\n"), 0644); err != nil {
		t.Fatalf("CreateExclusive: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("raw ReadFile: %v", err)
	}
	if !isAgeEncrypted(raw) {
		t.Errorf("on-disk bytes are not age-encrypted; CreateExclusive bypassed encryption")
	}

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("store ReadFile: %v", err)
	}
	if string(got) != "date,amount\n" {
		t.Errorf("round-tripped contents=%q, want %q", got, "date,amount\n")
	}
}
