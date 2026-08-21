package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// frozenMtime stands in for a filesystem whose timestamps are coarse or
// effectively frozen (WSL2 9p, some network mounts, the CI sandbox). The
// cache keys staleness on mtime+size, so freezing the mtime and holding the
// payload length constant is what makes a wrongly cached entry survive
// instead of being papered over by the next stat.
var frozenMtime = time.Unix(1_000_000_000, 0)

// TestReadFileConcurrentWriterNeverServesStale pins the ordering guarantee
// between a write and the cache: once WriteFile has returned, no later read
// may serve the pre-write payload.
//
// The regression it guards: writeFileLocked used to invalidate the cache
// before staging the new bytes and never touch it again. A read that stat'd
// the file just before the rename, and published what it saw just after,
// installed the OLD payload under the OLD mtime and size -- and with the
// mtime frozen and the payload length unchanged, ReadFile then served those
// stale bytes indefinitely.
func TestReadFileConcurrentWriterNeverServesStale(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")

	// Equal-length payloads: a length change would invalidate the cache on
	// size alone and hide the race being tested.
	payloads := [][]byte{[]byte("AAAA"), []byte("BBBB")}

	write := func(round int) []byte {
		want := payloads[round%len(payloads)]
		if err := s.WriteFile(path, want, 0644); err != nil {
			t.Fatalf("round %d: WriteFile: %v", round, err)
		}
		if err := os.Chtimes(path, frozenMtime, frozenMtime); err != nil {
			t.Fatalf("round %d: Chtimes: %v", round, err)
		}
		return want
	}

	write(0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := s.ReadFile(path)
				if err != nil {
					t.Errorf("concurrent ReadFile: %v", err)
					return
				}
				if got := string(data); got != "AAAA" && got != "BBBB" {
					t.Errorf("concurrent ReadFile returned torn payload %q", got)
					return
				}
			}
		}()
	}

	// The assertion: with the writer quiescent, a read must agree with the
	// write that just completed. Any stale entry a concurrent reader managed
	// to install is visible here.
	for round := 1; round <= 400; round++ {
		want := write(round)
		got, err := s.ReadFile(path)
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("round %d: ReadFile after write: %v", round, err)
		}
		if string(got) != string(want) {
			close(stop)
			wg.Wait()
			t.Fatalf("round %d: ReadFile returned %q after WriteFile(%q) returned; the cache is serving stale data", round, got, want)
		}
	}

	close(stop)
	wg.Wait()

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("final ReadFile: %v", err)
	}
	if string(got) != string(onDisk) {
		t.Fatalf("final Storage.ReadFile = %q, disk holds %q", got, onDisk)
	}
}

// TestPublishCacheRejectsEntryOvertakenByWrite drives the same race as
// TestReadFileConcurrentWriterNeverServesStale, but deterministically: it
// performs a read's two halves by hand with a complete write in between,
// which is the interleaving the stress test can only reach by luck.
func TestPublishCacheRejectsEntryOvertakenByWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")

	if err := s.WriteFile(path, []byte("AAAA"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, frozenMtime, frozenMtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// First half of a read: sample the generation, then stat and read the
	// bytes that are on disk at this moment -- exactly what ReadFileContext
	// does before it publishes.
	gen := s.cacheGeneration()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	overtaken, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	// A whole write lands in the window: same length as the old payload and,
	// with the mtime frozen, indistinguishable from it by stat alone.
	if err := s.WriteFile(path, []byte("BBBB"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, frozenMtime, frozenMtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Second half of the read. The entry it carries is now a description of a
	// file state that no longer exists, and must be refused.
	s.publishCache(path, gen, &cacheEntry{
		data:    overtaken,
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
	})

	s.cacheMu.RLock()
	cached, ok := s.cache[path]
	s.cacheMu.RUnlock()
	if ok {
		t.Errorf("publishCache installed an entry overtaken by a write: %q", cached.data)
	}

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "BBBB" {
		t.Errorf("Storage.ReadFile = %q, disk holds %q", got, "BBBB")
	}
}

// TestPublishCacheKeepsEntryFromUncontendedRead is the other half of the
// contract: the generation guard must not be so eager that ordinary reads
// stop caching.
func TestPublishCacheKeepsEntryFromUncontendedRead(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")

	if err := s.WriteFile(path, []byte("AAAA"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := s.ReadFile(path); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	s.cacheMu.RLock()
	entry, ok := s.cache[path]
	s.cacheMu.RUnlock()
	if !ok {
		t.Fatal("an uncontended ReadFile did not populate the cache")
	}
	if string(entry.data) != "AAAA" {
		t.Errorf("cached %q, want %q", entry.data, "AAAA")
	}

	// Serve the second read from that entry: rewriting the file behind
	// Storage's back leaves the cached bytes in place until something
	// invalidates them.
	if err := os.WriteFile(path, []byte("CCCC"), 0644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	if err := os.Chtimes(path, frozenMtime, frozenMtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	s.cacheMu.Lock()
	s.cache[path].modTime = frozenMtime.UnixNano()
	s.cacheMu.Unlock()

	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "AAAA" {
		t.Errorf("second ReadFile = %q, want the cached %q -- the cache is not being used", got, "AAAA")
	}
}

// TestRemoveOrdersCacheAgainstUnlink covers removeLocked's half of the same
// ordering rule.
func TestRemoveOrdersCacheAgainstUnlink(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")

	if err := s.WriteFile(path, []byte("AAAA"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gen := s.cacheGeneration()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	overtaken, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	if err := s.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	s.publishCache(path, gen, &cacheEntry{
		data:    overtaken,
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
	})

	s.cacheMu.RLock()
	_, ok := s.cache[path]
	s.cacheMu.RUnlock()
	if ok {
		t.Error("publishCache installed an entry for a file Remove had already unlinked")
	}
}

// TestLockBarsPublishFromReadStartedBeforeLock pins Lock's half of the
// generation contract: a read that sampled the generation before the lock
// must not be able to put what it decrypted back into the freshly cleared
// map. Clearing alone is not enough -- the clear happens at a moment, and an
// in-flight read outlives moments.
//
// The two halves of the read are driven directly rather than through
// ReadFileContext because ReadFileContext holds s.mu across read, decrypt and
// publish alike while Lock takes s.mu exclusively, so the two cannot in fact
// interleave today. That makes the generation bump defence in depth: the
// guarantee tested here is Lock's own, and must survive s.mu's coverage
// narrowing later.
func TestLockBarsPublishFromReadStartedBeforeLock(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}

	path := filepath.Join(dir, "ledger.csv")
	if err := s.WriteFile(path, []byte("AAAA"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// First half of a read, taken while the store is still unlocked: sample
	// the generation, stat, and hold the plaintext the read would have
	// decrypted.
	gen := s.cacheGeneration()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	plaintext, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(plaintext) != "AAAA" {
		t.Fatalf("ReadFile = %q, want %q", plaintext, "AAAA")
	}

	s.Lock()

	// Second half. The read is carrying plaintext from before the lock; the
	// generation bump is what keeps it out.
	s.publishCache(path, gen, &cacheEntry{
		data:    plaintext,
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
	})

	s.cacheMu.RLock()
	entry, ok := s.cache[path]
	s.cacheMu.RUnlock()
	if ok {
		t.Errorf("a read started before Lock put plaintext back into the cleared cache: %q", entry.data)
	}

	if _, err := s.ReadFile(path); err == nil {
		t.Error("ReadFile succeeded while the store was locked")
	}
}

// TestLockUnderConcurrentReadsLeavesNoPlaintext is the end-to-end form of the
// same property: whatever reads are in flight when Lock lands, no plaintext
// may remain cached once they have drained. It exercises the real
// ReadFile/Lock paths under -race rather than the halves.
func TestLockUnderConcurrentReadsLeavesNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}

	path := filepath.Join(dir, "ledger.csv")
	if err := s.WriteFile(path, []byte("AAAA"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Readers spin until the lock lands and their read starts failing.
	locked := make(chan struct{})
	var wg sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				data, err := s.ReadFile(path)
				if err != nil {
					return
				}
				if got := string(data); got != "AAAA" {
					t.Errorf("ReadFile returned %q, want %q", got, "AAAA")
					return
				}
				select {
				case <-locked:
					// Lock has landed; one more pass, then err ends the loop.
				default:
				}
			}
		}()
	}

	s.Lock()
	close(locked)
	wg.Wait()

	s.cacheMu.RLock()
	cached := len(s.cache)
	entry, ok := s.cache[path]
	s.cacheMu.RUnlock()
	if ok {
		t.Errorf("plaintext survived Lock in the cache: %q", entry.data)
	}
	if cached != 0 {
		t.Errorf("cache holds %d entries after Lock, want 0", cached)
	}
}
