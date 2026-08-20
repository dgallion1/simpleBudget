package storage

// Tier-3 acceptance oracle for R11 (atomicWrite stages at a fixed path).
// Lead-authored before dispatch; copied into the package by accept.sh and
// removed afterwards. Both blind implementations are judged against THIS file,
// so neither worker may edit it.
//
// The defect: atomicWrite stages at `path + ".tmp"`, a name derived solely from
// the destination. Storage.WriteFile holds only dataMu.RLock (shared), so
// concurrent writers to one path legitimately race on the same staging file:
// each os.WriteFile truncates and writes it, then each renames. The winner can
// therefore be a mixture of two writers' bytes, with every call reporting
// success. CreateExclusive (storage.go:392) already stages correctly with
// os.CreateTemp; atomicWrite does not.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Check 1 — the defect itself. Concurrent writers to one path must leave the
// destination holding exactly one writer's payload, never a mixture and never a
// truncated length. Payloads are large enough that a single write is split
// across syscalls, and the whole thing repeats so detection does not depend on
// one lucky interleaving.
func TestZZOracleR11_ConcurrentWritesNeverTear(t *testing.T) {
	const writers = 8
	const size = 1 << 21 // 2 MiB
	const rounds = 5

	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('A' + i)}, size)
	}

	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		s, err := New(dir)
		if err != nil {
			t.Fatalf("round %d: New: %v", round, err)
		}
		path := filepath.Join(dir, "sidecar.json")

		var wg sync.WaitGroup
		errs := make(chan error, writers)
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs <- s.WriteFile(path, payloads[i], 0644)
			}(i)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatalf("round %d: WriteFile reported an error: %v", round, e)
			}
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("round %d: ReadFile: %v", round, err)
		}
		if len(got) != size {
			t.Fatalf("round %d: file length = %d, want %d — the write was torn",
				round, len(got), size)
		}
		first := got[0]
		for i, b := range got {
			if b != first {
				t.Fatalf("round %d: byte %d = %q but byte 0 = %q — the file holds a "+
					"mixture of two writers' payloads", round, i, b, first)
			}
		}
		if first < 'A' || first >= byte('A'+writers) {
			t.Fatalf("round %d: winning byte %q belongs to no writer", round, first)
		}
	}
}

// Check 2 — no staging file survives a completed set of concurrent writes.
func TestZZOracleR11_NoStagingFileSurvives(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "sidecar.json")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.WriteFile(path, bytes.Repeat([]byte{byte('a' + i)}, 4096), 0644)
		}(i)
	}
	wg.Wait()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("staging file %q survived a completed write", e.Name())
		}
	}
}

// Check 3 — the single-writer contract is unchanged: content and permissions
// land exactly as before, and a rewrite replaces rather than appends.
func TestZZOracleR11_SingleWriterContractUnchanged(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "sidecar.json")

	if err := s.WriteFile(path, []byte("first-and-longer"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := s.WriteFile(path, []byte("second"), 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q — a rewrite must replace, not append or leave a tail", got, "second")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0644 {
		t.Fatalf("perm = %v, want 0644", fi.Mode().Perm())
	}
}
