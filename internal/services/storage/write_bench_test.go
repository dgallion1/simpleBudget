package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The durability barriers — fileSync on the staging file, syncDir on the
// destination directory — are the dominant cost of a write, so the price is
// recorded here rather than left to be rediscovered by someone wondering why
// saves got slower.
//
// Measured on a Linux SSD with 4KB payloads: roughly 2.5ms/op with the two
// fsyncs against ~50us/op without them, i.e. about 2.4ms added per write.
// That is invisible at the rate a person saves a budget, and it is the only
// reason a save the UI reported as done is still there after a crash.
//
// It does scale with the bulk paths, which pay it once per file: a migration,
// a restore, or an encrypt/decrypt pass over the whole data directory. A real
// data directory here holds a couple of dozen files, so a full pass costs well
// under a second. If that ever changes — tens of thousands of files — the fix
// is to batch the directory fsync across a bulk run rather than to drop the
// barriers.
//
// To see the difference yourself, override the seams the durability tests use:
//
//	overrideFileSync(t, func(*os.File) error { return nil })
//	overrideSyncDir(t, func(string) error { return nil })

func BenchmarkWriteFile(b *testing.B) {
	dir := b.TempDir()
	s, err := New(dir)
	if err != nil {
		b.Fatal(err)
	}
	data := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.json", i%64)), data, 0644); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreateExclusive(b *testing.B) {
	dir := b.TempDir()
	data := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d.json", i))
		if err := createExclusive(p, data, 0644); err != nil {
			b.Fatal(err)
		}
		os.Remove(p)
	}
}
