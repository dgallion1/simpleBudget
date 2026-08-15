// Package restore rewrites the data directory from a backup archive. It is
// the inverse of internal/services/backup's snapshot: that package makes the
// zip, this one puts it back and prunes what the archive does not contain.
//
// It lives outside internal/services/backup deliberately. Restore depends on
// the settings rewrite gate and the prune-extras walk, neither of which the
// snapshot side knows anything about; keeping them apart keeps a deliberately
// small service small. The dependency runs one way -- restore imports backup,
// never the reverse.
package restore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
)

// Sentinels. The handler maps these to the HTTP statuses it has always
// returned; a future MCP tool can classify them without parsing prose. Each
// is wrapped with %w at its raise site so the offending entry name survives
// into the message.
var (
	// 400-class: the archive or its contents are the problem.
	ErrInvalidArchive  = errors.New("invalid zip archive")
	ErrUnsafePath      = errors.New("unsafe path in archive")
	ErrUnreadableEntry = errors.New("cannot read archive entry")
	ErrEncryptedEntry  = errors.New("encrypted entry cannot be restored into a store that is not encrypted and unlocked")
	ErrEmptyArchive    = errors.New("no restorable files in archive")

	// 500-class: the server or its wiring is the problem.
	ErrBadDataDir      = errors.New("bad data directory")
	ErrNoStore         = errors.New("no storage layer is configured")
	ErrNoBackupService = errors.New("no backup service is configured")
	ErrSnapshotFailed  = errors.New("safety snapshot failed")
	ErrWriteFailed     = errors.New("write failed")
)

// SnapshotHolder takes the safety snapshot and holds the snapshot lock for
// the duration of the restore. *backupsvc.Service satisfies it.
//
// NOT named Snapshotter: internal/services/mcpsvc/snapshot already exports a
// Snapshotter and it is an entirely different thing (sidecar .bak copies, not
// archive locks). Two types with one name in one codebase is how the wrong
// one gets wired.
type SnapshotHolder interface {
	SnapshotAndHold(ctx context.Context) (func(), error)
}

// SettingsRewriteGate serializes an external rewrite of the settings files
// (a restore's write+prune phase) against the settings manager's saves.
// Acquiring it holds the manager's lock -- no in-flight save can interleave
// with a half-restored settings dir -- and the returned end func carries the
// post-rewrite bookkeeping (cache invalidation, active-scenario
// reconciliation) inside the same critical section. Implemented by
// retirement.SettingsManager.
type SettingsRewriteGate interface {
	BeginExternalRewrite() (end func())
}

// RewriteGateFunc adapts a bare acquire function to SettingsRewriteGate.
type RewriteGateFunc func() (end func())

// BeginExternalRewrite implements SettingsRewriteGate.
func (f RewriteGateFunc) BeginExternalRewrite() func() { return f() }

// Deps is everything a restore needs. Gate may be nil (see FromZip); the
// others are required and their absence is reported as an error rather than
// a panic.
type Deps struct {
	DataDir   string
	BackupDir string
	Store     *storage.Storage
	Backups   SnapshotHolder
	Gate      SettingsRewriteGate
}

// Result summarizes a FromZip run: files written, stale files pruned, archive
// entries dropped by the skip list, and prune removals that failed (details
// in the server log).
type Result struct {
	Restored         int
	Pruned           int
	SkippedProtected int
	PruneFailures    int
}

// Service restores archives into one data directory.
type Service struct{ deps Deps }

// New returns a Service bound to deps.
func New(d Deps) *Service { return &Service{deps: d} }

// FromZip restores content over the data directory and prunes files the
// archive does not contain. It is destructive by design: a file present on
// disk but absent from the archive is removed unless the skip predicate
// protects it.
func (s *Service) FromZip(ctx context.Context, content []byte) (Result, error) {
	var res Result
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}

	dataAbs, err := filepath.Abs(s.deps.DataDir)
	if err != nil {
		return res, fmt.Errorf("%w %q: %v", ErrBadDataDir, s.deps.DataDir, err)
	}
	skip := backupsvc.SkipPredicate(s.deps.DataDir, s.deps.BackupDir)

	type prepared struct {
		dest string
		data []byte
	}
	var queue []prepared
	archiveEntries := make(map[string]struct{})
	skippedEntries := make(map[string]struct{})

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		// Sanitize: forbid absolute, forbid ".." segments, must stay under data dir.
		raw := filepath.ToSlash(zf.Name)
		if strings.HasPrefix(raw, "/") {
			return res, fmt.Errorf("%w: absolute path %s", ErrUnsafePath, zf.Name)
		}
		clean := filepath.Clean(raw)
		if clean == "." || clean == "" {
			continue
		}
		for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
			if seg == ".." {
				return res, fmt.Errorf("%w: path traversal in %s", ErrUnsafePath, zf.Name)
			}
		}
		dest := filepath.Join(s.deps.DataDir, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil || !(destAbs == dataAbs || strings.HasPrefix(destAbs, dataAbs+string(filepath.Separator))) {
			return res, fmt.Errorf("%w: %s escapes the data directory", ErrUnsafePath, zf.Name)
		}
		// SkipPredicate is ancestor-aware for file paths, so entries under
		// skip-listed directories (e.g. cache/plotly.min.js) are dropped too.
		// Deduped like restored, so duplicate zip entries count once.
		if skip(dest, false) {
			if _, dup := skippedEntries[destAbs]; !dup {
				skippedEntries[destAbs] = struct{}{}
				res.SkippedProtected++
			}
			continue
		}

		rc, err := zf.Open()
		if err != nil {
			return res, fmt.Errorf("%w %s: %v", ErrUnreadableEntry, zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return res, fmt.Errorf("%w %s: %v", ErrUnreadableEntry, zf.Name, err)
		}

		// Encrypted blob into unencrypted/locked store -> reject the whole archive.
		if storage.IsAgeEncryptedData(data) && !(s.deps.Store.IsEncrypted() && s.deps.Store.IsUnlocked()) {
			return res, fmt.Errorf("%w: entry %s", ErrEncryptedEntry, zf.Name)
		}

		queue = append(queue, prepared{dest: dest, data: data})
		archiveEntries[destAbs] = struct{}{}
	}

	if len(queue) == 0 {
		return res, ErrEmptyArchive
	}

	if s.deps.Store == nil {
		return res, ErrNoStore
	}
	if s.deps.Backups == nil {
		return res, ErrNoBackupService
	}

	// LOCK ORDER -- settings gate, then data directory, then snapshot hold.
	// Nothing may take them in another order, and the deferred releases
	// unwind in reverse.
	//
	// The order is forced by SettingsManager.SaveWithRevision, which holds
	// the manager's lock across its store.WriteFile and therefore takes
	// settings-then-data. A restore taking data-then-settings would be the
	// other half of an ABBA deadlock: the restore holding the data directory
	// and waiting for the settings lock, an in-flight save holding the
	// settings lock and waiting for the data directory.
	//
	// Serialize the entire snapshot + write + prune against settings saves.
	// The gate (when wired) holds the SettingsManager's lock until the
	// deferred endRewrite runs at function exit -- i.e. after pruneExtras --
	// so no save can interleave with a half-restored settings directory, and
	// endRewrite's cache drop + active-scenario reconciliation happen inside
	// the same critical section. Nothing between here and return may call a
	// SettingsManager method (that would deadlock).
	//
	// Accepted consequence of taking it first: a restore that fails at the
	// snapshot step below has still opened and closed the gate, so it bumps
	// the settings revision and drops the cache. That costs one page refresh
	// on a failed restore, which is the cheap side of this trade.
	if s.deps.Gate != nil {
		endRewrite := s.deps.Gate.BeginExternalRewrite()
		defer endRewrite()
	} else {
		// A nil gate means the service was wired without a settings manager
		// -- the restore proceeds UNSERIALIZED against settings saves (the
		// race the gate exists to prevent). Loud so a wiring regression is
		// visible instead of silently racy.
		log.Printf("restore: running without a restore gate; concurrent settings saves are not serialized (pass a SettingsRewriteGate in Deps)")
	}

	// Hold the data directory against every other writer for the whole
	// snapshot + write + prune. Without this, an ordinary write (an MCP tool,
	// a page handler) can land in the window between the rewrite and the
	// prune walk, and the prune deletes it -- silently, since the writer was
	// told its write succeeded.
	//
	// Taken BEFORE the safety snapshot, not after, so the snapshot captures
	// exactly the state the prune will act on. Acquired the other way round,
	// a write landing between the two would be deleted by the prune AND
	// missing from the safety archive: data with no copy anywhere.
	writer := s.deps.Store.BeginExclusive()
	defer writer.Release()

	// Hold the snapshot lock for the whole restore so a scheduled snapshot
	// (or a second restore) cannot capture a half-restored data dir. Innermost
	// because nothing holding it ever writes through Storage or touches the
	// settings manager -- snapshots write into the backup dir with os
	// directly -- so it adds no edge back to the two locks above.
	release, err := s.deps.Backups.SnapshotAndHold(ctx)
	if err != nil {
		if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
			// Returned as-is (not wrapped in a restore sentinel) so the
			// caller's errors.Is sees the backup package's own identity
			// rather than a restore alias.
			return res, backupsvc.ErrSnapshotInProgress
		}
		return res, fmt.Errorf("%w: %v", ErrSnapshotFailed, err)
	}
	defer release()

	// Through the exclusive writer, never through Store's own methods: those
	// take the lock this function already holds, and sync.RWMutex is not
	// reentrant.
	for _, p := range queue {
		if err := writer.MkdirAll(filepath.Dir(p.dest), 0755); err != nil {
			return res, fmt.Errorf("%w: mkdir %s: %v", ErrWriteFailed, filepath.Dir(p.dest), err)
		}
		if err := writer.WriteFile(p.dest, p.data, 0644); err != nil {
			return res, fmt.Errorf("%w: write %s: %v", ErrWriteFailed, p.dest, err)
		}
	}

	res.Pruned, res.PruneFailures = s.pruneExtras(writer, dataAbs, archiveEntries, skip)
	if res.PruneFailures > 0 {
		log.Printf("restore prune completed with %d failures", res.PruneFailures)
	}
	// archiveEntries (not queue) so duplicate zip entries count once.
	res.Restored = len(archiveEntries)
	return res, nil
}

// pruneExtras removes files under dataAbs that the archive did not contain,
// then removes directories left empty, deepest first. Returns (removed,
// failures); failures are logged with their cause and never abort the
// restore, because a half-pruned tree is still a correctly restored one.
func (s *Service) pruneExtras(writer *storage.ExclusiveWriter, dataAbs string, archiveEntries map[string]struct{}, skip func(path string, isDir bool) bool) (int, int) {
	var dirs []string
	removed := 0
	failures := 0
	err := filepath.Walk(dataAbs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			log.Printf("restore prune: walk %s: %v", path, walkErr)
			failures++
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			log.Printf("restore prune: abs %s: %v", path, err)
			failures++
			return nil
		}
		if pathAbs == dataAbs {
			return nil
		}
		if info.IsDir() {
			if skip(pathAbs, true) {
				return filepath.SkipDir
			}
			dirs = append(dirs, pathAbs)
			return nil
		}
		if skip(pathAbs, false) {
			return nil
		}
		if _, ok := archiveEntries[pathAbs]; ok {
			return nil
		}
		if err := writer.Remove(pathAbs); err != nil {
			log.Printf("restore prune: remove stale file %s: %v", pathAbs, err)
			failures++
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		log.Printf("restore prune: walk root %s: %v", dataAbs, err)
		failures++
	}

	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, dir := range dirs {
		if dir == dataAbs || skip(dir, true) {
			continue
		}
		if err := writer.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
				continue
			}
			log.Printf("restore prune: remove empty dir %s: %v", dir, err)
			failures++
		}
	}
	return removed, failures
}
