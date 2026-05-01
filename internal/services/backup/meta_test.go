package backup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMeta_LoadMissingReturnsZero(t *testing.T) {
	dir := t.TempDir()
	m, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if m.TS != "" || m.FileCount != 0 || m.LastError != "" {
		t.Fatalf("missing meta should be zero, got %+v", m)
	}
}

func TestMeta_WriteSuccessAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	in := Meta{
		TS:            now.Format("20060102_150405"),
		FileCount:     12,
		TotalBytes:    345_678,
		Encrypted:     true,
		LastAttemptTS: now.Format("20060102_150405"),
	}
	if err := writeMetaSuccess(dir, in); err != nil { t.Fatal(err) }
	got, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if got != in {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, in)
	}
}

func TestMeta_FailureWritePreservesPriorTS(t *testing.T) {
	dir := t.TempDir()
	prior := Meta{
		TS: "20260430_080000", FileCount: 5, TotalBytes: 100, Encrypted: false,
		LastAttemptTS: "20260430_080000",
	}
	if err := writeMetaSuccess(dir, prior); err != nil { t.Fatal(err) }

	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	if err := writeMetaFailure(dir, "disk full", now); err != nil { t.Fatal(err) }

	got, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if got.TS != prior.TS {
		t.Fatalf("failure write changed TS: got %q want %q", got.TS, prior.TS)
	}
	if got.FileCount != prior.FileCount {
		t.Fatalf("failure write changed FileCount: got %d want %d", got.FileCount, prior.FileCount)
	}
	if got.LastError != "disk full" {
		t.Fatalf("LastError got %q want %q", got.LastError, "disk full")
	}
	if got.LastAttemptTS != now.Format("20060102_150405") {
		t.Fatalf("LastAttemptTS not updated: %q", got.LastAttemptTS)
	}
}

func TestMeta_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	if err := writeMetaSuccess(dir, Meta{TS: "20260501_120000"}); err != nil { t.Fatal(err) }

	// No .tmp leftover should remain after a successful write.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("atomic write left .tmp files: %v", matches)
	}
}
