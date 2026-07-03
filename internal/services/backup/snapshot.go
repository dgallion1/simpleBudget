package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	backupNamePrefix = "budget_backup_"
	backupNameSuffix = ".zip"
	tmpSuffix        = ".tmp"
)

// Snapshot builds one backup zip in BackupDir, verifies it, then atomically
// renames it into place. On success, writes meta with success fields. On
// failure, writes meta preserving the prior successful TS and recording
// LastError. Returns ErrSnapshotInProgress when another snapshot is in flight.
func (s *Service) Snapshot(ctx context.Context) error {
	if !s.tryLock() {
		return ErrSnapshotInProgress
	}
	defer s.unlock()
	return s.snapshotLocked(ctx)
}

// SnapshotIfStale runs a snapshot only when the most recent successful
// snapshot is older than maxAge (or no snapshot has run yet). Returns
// nil without snapshotting when the user has disabled auto-backup, so
// every caller (startup, scheduler, future use) honors the toggle
// without having to gate the call site.
func (s *Service) SnapshotIfStale(ctx context.Context, maxAge time.Duration) error {
	if !s.Enabled() {
		return nil
	}
	m, err := loadMeta(s.cfg.BackupDir)
	if err != nil {
		// Treat unreadable meta as "stale, take one".
		return s.Snapshot(ctx)
	}
	if m.TS == "" {
		return s.Snapshot(ctx)
	}
	prev, err := time.Parse("20060102_150405", m.TS)
	if err != nil {
		return s.Snapshot(ctx)
	}
	if s.clock.Now().UTC().Sub(prev) >= maxAge {
		return s.Snapshot(ctx)
	}
	return nil
}

func (s *Service) snapshotLocked(ctx context.Context) error {
	now := s.clock.Now().UTC()
	ts := now.Format("20060102_150405")

	if err := os.MkdirAll(s.cfg.BackupDir, 0700); err != nil {
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("mkdir backup dir: %v", err), now)
		return fmt.Errorf("backup: mkdir: %w", err)
	}
	s.cleanOrphanTmp()

	tmpPath := filepath.Join(s.cfg.BackupDir, backupNamePrefix+ts+backupNameSuffix+tmpSuffix)
	finalPath := filepath.Join(s.cfg.BackupDir, backupNamePrefix+ts+backupNameSuffix)

	count, total, err := s.buildZip(ctx, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("build zip: %v", err), now)
		return fmt.Errorf("backup: build zip: %w", err)
	}

	if err := verifyZip(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("verify zip: %v", err), now)
		return fmt.Errorf("backup: verify zip: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("rename: %v", err), now)
		return fmt.Errorf("backup: rename: %w", err)
	}

	encrypted := false
	if s.cfg.Store != nil {
		encrypted = s.cfg.Store.IsEncrypted()
	}
	if err := writeMetaSuccess(s.cfg.BackupDir, Meta{
		TS:            ts,
		FileCount:     count,
		TotalBytes:    total,
		Encrypted:     encrypted,
		LastAttemptTS: ts,
	}); err != nil {
		// Snapshot itself succeeded — log meta failure but return nil.
		fmt.Fprintf(os.Stderr, "backup: write meta: %v\n", err)
	}

	if err := applyRetention(s.cfg.BackupDir, now); err != nil {
		// Retention failure is non-fatal; record in meta but do not return error.
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("retention: %v", err), now)
	}
	return nil
}

// cleanOrphanTmp deletes *.tmp files in BackupDir older than 1 hour.
// These come from process kills mid-snapshot.
func (s *Service) cleanOrphanTmp() {
	matches, _ := filepath.Glob(filepath.Join(s.cfg.BackupDir, "*"+tmpSuffix))
	cutoff := s.clock.Now().Add(-time.Hour)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
}

// buildZip writes the zip to tmpPath and returns (file count, total bytes).
func (s *Service) buildZip(ctx context.Context, tmpPath string) (int, int64, error) {
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, 0, err
	}
	// Data durability is guaranteed by the explicit f.Sync() below; this
	// deferred close is a backstop whose error is not separately actionable.
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	// Safety double-close; the meaningful, error-checked Close() is below.
	defer func() { _ = zw.Close() }()

	var count int
	var total int64

	skip := s.skipPredicate()

	err = filepath.Walk(s.cfg.DataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if info.IsDir() {
			if skip(path, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if skip(path, false) {
			return nil
		}
		rel, err := filepath.Rel(s.cfg.DataDir, path)
		if err != nil {
			return err
		}
		// Use forward slashes for portable zip entries.
		rel = filepath.ToSlash(rel)

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		n, err := w.Write(raw)
		if err != nil {
			return err
		}
		count++
		total += int64(n)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	if err := zw.Close(); err != nil {
		return 0, 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, 0, err
	}
	return count, total, nil
}

// skipPredicate returns a function that decides whether a path under DataDir
// should be excluded from the snapshot.
func (s *Service) skipPredicate() func(path string, isDir bool) bool {
	// Resolve absolute paths once for the recursive-backup guard.
	absBackup, _ := filepath.Abs(s.cfg.BackupDir)
	absData, _ := filepath.Abs(s.cfg.DataDir)
	return func(path string, isDir bool) bool {
		base := filepath.Base(path)
		// Skip the BackupDir itself if nested under DataDir.
		if absBackup != "" && absData != "" {
			abs, err := filepath.Abs(path)
			if err == nil && (abs == absBackup || strings.HasPrefix(abs, absBackup+string(filepath.Separator))) {
				return true
			}
		}
		if isDir {
			return base == "cache"
		}
		// Skip atomicWrite leftovers and encryption markers.
		if strings.HasSuffix(base, tmpSuffix) {
			return true
		}
		if base == ".encrypted" || base == ".encryption-verify" {
			return true
		}
		return false
	}
}

func verifyZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	// Touch every entry's CRC by reading it.
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			_ = rc.Close()
			return err
		}
		_ = rc.Close()
	}
	return nil
}

