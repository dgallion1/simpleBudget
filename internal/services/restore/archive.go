package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	backupsvc "budget2/internal/services/backup"
)

// Sentinels for resolving a named archive, raised before FromZip is reached
// and therefore before anything on disk has been touched.
var (
	// 400-class: the caller named something that is not an archive here.
	ErrBadArchiveName = errors.New("not a backup archive name")
	ErrNoSuchArchive  = errors.New("no such backup archive")

	// 500-class: the archive is there but this server cannot read it.
	ErrArchiveUnreadable = errors.New("backup archive cannot be read")
	ErrNoBackupDir       = errors.New("no backup directory is configured")
)

// FromArchive restores the named archive out of the backup directory. name is
// a bare filename as List reports it, never a path: it is validated against
// the exact shape the backup package writes, so it cannot address a file
// outside the backup directory.
//
// Everything FromZip documents applies once the bytes are read — including
// that this is destructive, and that files the archive does not contain are
// deleted.
func (s *Service) FromArchive(ctx context.Context, name string) (Result, error) {
	var res Result
	if s.deps.BackupDir == "" {
		// Without this, filepath.Join("", name) resolves against the process
		// working directory and the name check above would be guarding the
		// wrong directory entirely.
		return res, ErrNoBackupDir
	}
	if !backupsvc.ValidArchiveName(name) {
		return res, fmt.Errorf("%w: %q (expected a name exactly as the backup listing reports it, "+
			"of the form budget_backup_YYYYMMDD_HHMMSS.zip)", ErrBadArchiveName, name)
	}

	content, err := os.ReadFile(filepath.Join(s.deps.BackupDir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res, fmt.Errorf("%w: %s", ErrNoSuchArchive, name)
		}
		return res, fmt.Errorf("%w %s: %v", ErrArchiveUnreadable, name, err)
	}
	return s.FromZip(ctx, content)
}
