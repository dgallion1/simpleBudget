package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// archiveStampLayout is the timestamp embedded in every archive filename.
// One constant, used by both the name parser and the writer, so the two
// cannot drift.
const archiveStampLayout = "20060102_150405"

// Archive describes one backup archive on disk.
//
// Name is the bare filename, not a path: it is what restore takes, and
// keeping the directory out of it means a caller cannot be handed a name
// that points somewhere other than the backup directory.
type Archive struct {
	Name  string
	TS    time.Time // parsed from Name, UTC
	Bytes int64
}

// parseArchiveStamp extracts the timestamp from a bare archive filename,
// reporting whether base is a name this package would have written.
func parseArchiveStamp(base string) (time.Time, bool) {
	if !strings.HasPrefix(base, backupNamePrefix) || !strings.HasSuffix(base, backupNameSuffix) {
		return time.Time{}, false
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(base, backupNamePrefix), backupNameSuffix)
	ts, err := time.Parse(archiveStampLayout, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

// ValidArchiveName reports whether name is a bare archive filename of the
// exact shape this package writes: budget_backup_<YYYYMMDD_HHMMSS>.zip.
//
// It is the gate a caller uses before joining an externally supplied name
// onto the backup directory. The timestamp must parse, so the name cannot
// carry a separator, a ".." segment or any other traversal — but the
// explicit Base check stays anyway, because a validator that only rejects
// traversal as a side effect of an unrelated rule is one refactor away from
// not rejecting it at all.
func ValidArchiveName(name string) bool {
	if name == "" || name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	_, ok := parseArchiveStamp(name)
	return ok
}

// List returns the archives in the backup directory, NEWEST FIRST. The
// order is part of the contract: "restore the most recent backup" is the
// common request, and making the caller sort is how it gets sorted wrong.
//
// An archive whose file has disappeared since the directory was scanned
// (retention prunes concurrently) is omitted rather than reported with a
// zero size — it is not restorable, so listing it would only invite a call
// that fails. Any other stat failure keeps the entry with Bytes unset: the
// archive is there, only its size is unknown.
func (s *Service) List() ([]Archive, error) {
	entries, err := listBackupTimes(s.cfg.BackupDir)
	if err != nil {
		return nil, fmt.Errorf("backup: list archives in %s: %w", s.cfg.BackupDir, err)
	}
	out := make([]Archive, 0, len(entries))
	// listBackupTimes sorts oldest first; walk it backwards.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		a := Archive{Name: filepath.Base(e.path), TS: e.ts}
		switch fi, statErr := os.Stat(e.path); {
		case statErr == nil:
			a.Bytes = fi.Size()
		case errors.Is(statErr, os.ErrNotExist):
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
