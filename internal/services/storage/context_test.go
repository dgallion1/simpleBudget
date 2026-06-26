package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestReadFileContextCancelledFailsFast verifies the ctx-aware read returns
// ctx.Err() without touching disk when the caller has already cancelled.
func TestReadFileContextCancelledFailsFast(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "data.json")
	if err := store.WriteFile(path, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the read

	if _, err := store.ReadFileContext(ctx, path); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadFileContext = %v, want context.Canceled", err)
	}
}

// TestWriteFileContextCancelledDoesNotWrite verifies the ctx-aware write fails
// fast and leaves no file behind when the caller has already cancelled.
func TestWriteFileContextCancelledDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "out.json")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.WriteFileContext(ctx, path, []byte("x"), 0644); !errors.Is(err, context.Canceled) {
		t.Errorf("WriteFileContext = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not exist after cancelled write, stat err = %v", err)
	}
}

// TestReadFileContextBackgroundStillReads guards the wrapper contract: the
// context-less ReadFile (and an uncancelled ctx) behave exactly as before.
func TestReadFileContextBackgroundStillReads(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "data.json")
	want := []byte(`{"ok":true}`)
	if err := store.WriteFile(path, want, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := store.ReadFileContext(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadFileContext: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("ReadFileContext = %q, want %q", got, want)
	}
}
