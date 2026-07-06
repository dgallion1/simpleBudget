// Package backup serves the HTTP endpoints behind the Backup admin panel:
// list snapshots, trigger an on-demand snapshot, restore from a zip,
// toggle the scheduler, and stream a downloadable archive. Pure HTTP
// glue around services/backup — no snapshot logic lives here.
package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"budget2/internal/config"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/testdata"
)

var (
	cfg       *config.Config
	store     *storage.Storage
	renderer  *templates.Renderer
	backupSvc *backupsvc.Service
)

// Initialize sets up the backup package with required dependencies.
// It also clears any previously set restore gate — re-initializing the
// package starts a fresh wiring, so the gate must be set (again) after
// Initialize. This keeps repeated wiring (e.g. per-test setup) from
// keeping a stale gate bound to torn-down dependencies.
func Initialize(c *config.Config, s *storage.Storage, r *templates.Renderer, b *backupsvc.Service) {
	cfg = c
	store = s
	renderer = r
	backupSvc = b
	restoreGate = nil
}

// restoreGate, when set, is acquired for the duration of a restore's
// write+prune phase (upload and bundled test-data paths alike) and released
// once the on-disk rewrite is complete. It serializes the restore against
// concurrent settings saves: without it, an in-flight save could re-create
// a pruned scenario file or clobber the freshly restored whatif.json with
// pre-restore data. The returned release also carries the post-rewrite
// bookkeeping (cache invalidation, active-scenario reconciliation) inside
// the same critical section.
var restoreGate func() (release func())

// SetRestoreGate installs the gate acquired around every restore's
// write+prune phase (e.g. retirement.SettingsManager.BeginExternalRewrite).
// Set after Initialize: re-initializing the package clears the gate.
func SetRestoreGate(fn func() func()) {
	restoreGate = fn
}

type backupStatusResponse struct {
	TS            string `json:"ts"`
	FileCount     int    `json:"file_count"`
	TotalBytes    int64  `json:"total_bytes"`
	Encrypted     bool   `json:"encrypted"`
	LastError     string `json:"last_error"`
	LastAttemptTS string `json:"last_attempt_ts"`
	SnapshotCount int    `json:"snapshot_count"`
	Dir           string `json:"dir"`
	Enabled       bool   `json:"enabled"`
}

func HandleBackupStatus(w http.ResponseWriter, r *http.Request) {
	dir := ""
	enabled := false
	if backupSvc != nil {
		dir = backupSvc.BackupDir()
		enabled = backupSvc.Enabled()
	} else if cfg != nil {
		dir = cfg.BackupDir
	}

	resp := backupStatusResponse{Dir: dir, Enabled: enabled}
	if dir != "" {
		// last_backup.json is always plaintext: it lives in BackupDir, which
		// defaults to an XDG path outside DataDir, so the storage layer's
		// encryption never applies. Read it directly with os.ReadFile.
		if data, err := os.ReadFile(filepath.Join(dir, "last_backup.json")); err == nil {
			_ = json.Unmarshal(data, &resp)
			resp.Dir = dir
			resp.Enabled = enabled
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "budget_backup_*.zip"))
		resp.SnapshotCount = len(matches)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleOpenBackupDir launches the OS file manager pointed at the configured
// BackupDir. The path is taken from server config — no client input is used —
// so this cannot be coerced into opening arbitrary directories.
func HandleOpenBackupDir(w http.ResponseWriter, r *http.Request) {
	dir := ""
	if backupSvc != nil {
		dir = backupSvc.BackupDir()
	} else if cfg != nil {
		dir = cfg.BackupDir
	}
	if dir == "" {
		http.Error(w, "backup directory not configured", http.StatusInternalServerError)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dir, 0700); mkErr != nil {
				http.Error(w, fmt.Sprintf("backup directory missing and could not be created: %v", mkErr), http.StatusInternalServerError)
				return
			}
		} else {
			http.Error(w, fmt.Sprintf("stat backup dir: %v", err), http.StatusInternalServerError)
			return
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open backup dir failed: %v", err)
		http.Error(w, fmt.Sprintf("could not launch file manager: %v", err), http.StatusInternalServerError)
		return
	}
	// Don't block on the file manager — it may stay open. Reap the child
	// in a goroutine so we don't leak a zombie.
	go func() { _ = cmd.Wait() }()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "dir": dir})
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// exitFunc is a package-level variable for testing.
var exitFunc = os.Exit

func HandleKillServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("Server shutting down...\n"))
	log.Println("Received /killme request, shutting down")
	go func() {
		time.Sleep(100 * time.Millisecond)
		exitFunc(0)
	}()
}

// resolvedBackupDir returns the backup directory from the service when
// available, falling back to config. Used to build the shared skip
// predicate even when the backup service is not initialized.
func resolvedBackupDir() string {
	if backupSvc != nil {
		return backupSvc.BackupDir()
	}
	if cfg != nil {
		return cfg.BackupDir
	}
	return ""
}

func HandleBackup(w http.ResponseWriter, r *http.Request) {
	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("budget_backup_%s.zip", timestamp)

	// Set headers for file download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Create zip writer directly to the response writer
	zw := zip.NewWriter(w)
	// Deferred Close flushes the central directory; a failure means a corrupt
	// download, which we can only log (headers are already sent).
	defer func() {
		if err := zw.Close(); err != nil {
			log.Printf("backup: closing zip stream: %v", err)
		}
	}()

	dataDir := cfg.DataDirectory
	skip := backupsvc.SkipPredicate(dataDir, resolvedBackupDir())
	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
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

		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		entryName := filepath.ToSlash(relPath)

		f, err := zw.Create(entryName)
		if err != nil {
			return err
		}

		// Read on-disk bytes verbatim. When the store is encrypted, the zip
		// preserves age-encrypted payloads — restore re-attaches them to an
		// unlocked encrypted store. Matches automatic-snapshot behavior so
		// manual and scheduled backups are byte-identical.
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer func() { _ = file.Close() }()

		if _, err := io.Copy(f, file); err != nil {
			return fmt.Errorf("copy %s into backup: %w", path, err)
		}
		return nil
	})

	if err != nil {
		log.Printf("Error creating backup: %v", err)
		// Note: Since we've already started writing headers and potentially content,
		// we can't easily change to an error response, but we can log it.
	}
}

// HandleBackupPlaintext is the "break-glass" plaintext export. Walks the data
// dir reading via store.OpenFile (which decrypts), and returns a zip with
// plaintext entries. Only available when storage is encrypted and unlocked.
// For password method, the user must re-enter their password (re-verified via
// store.Unlock). For age/SSH/YubiKey, the user types "EXPORT" to confirm.
//
// The friction is intentional — accidental plaintext exports defeat
// at-rest encryption.
func HandleBackupPlaintext(w http.ResponseWriter, r *http.Request) {
	if store == nil || !store.IsEncrypted() {
		http.Error(w, "encryption is not enabled; use /backup", http.StatusBadRequest)
		return
	}
	if !store.IsUnlocked() {
		http.Error(w, "storage is locked; unlock first", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	method := store.GetAuthMethod()
	switch method {
	case storage.AuthMethodPassword:
		password := r.FormValue("password")
		if password == "" {
			http.Error(w, "password is required to confirm plaintext export", http.StatusBadRequest)
			return
		}
		if err := store.Unlock(password); err != nil {
			log.Printf("Plaintext export blocked: password verification failed")
			http.Error(w, "incorrect password", http.StatusUnauthorized)
			return
		}
	default:
		// Age/SSH/YubiKey: possession of the unlocked session is the auth
		// signal; the typed phrase prevents one-click drive-by exports.
		if r.FormValue("confirm") != "EXPORT" {
			http.Error(w, `type "EXPORT" to confirm plaintext export`, http.StatusBadRequest)
			return
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("budget_plaintext_%s.zip", timestamp)
	log.Printf("PLAINTEXT EXPORT initiated (method=%s, file=%s)", method, filename)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	zw := zip.NewWriter(w)
	defer func() {
		if err := zw.Close(); err != nil {
			log.Printf("backup: closing plaintext zip stream: %v", err)
		}
	}()

	dataDir := cfg.DataDirectory
	skip := backupsvc.SkipPredicate(dataDir, resolvedBackupDir())
	var fileCount int
	var totalBytes int64
	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
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

		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		entryName := filepath.ToSlash(relPath)

		f, err := zw.Create(entryName)
		if err != nil {
			return err
		}

		// Decrypt on read. This is the whole point of the break-glass path.
		file, err := store.OpenFile(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		n, err := io.Copy(f, file)
		if err != nil {
			return err
		}
		fileCount++
		totalBytes += n
		return nil
	})
	if err != nil {
		log.Printf("PLAINTEXT EXPORT error during walk: %v", err)
		return
	}
	log.Printf("PLAINTEXT EXPORT complete: %d files, %d bytes (method=%s)", fileCount, totalBytes, method)
}

// restoreFromZip extracts every entry of the supplied archive into
// cfg.DataDirectory using store.WriteFile, then prunes files that were
// not present in the archive. It validates the entire archive before
// snapshotting or writing any files, so a malformed entry rejects the
// whole operation before disk state changes.
//
// Archive entries that the backup skip list excludes (encryption-state
// files, *.tmp, anything under the backup dir or a skip-listed directory
// like cache/) are ignored rather than written: they describe local store
// state, not user data, and a legit backup never contains them (older
// backups may contain .encryption-config.json). Skipped entries are
// counted in the result so callers can surface them.
//
// Returns (result, http status, error message). On success, status is 200.
func restoreFromZip(ctx context.Context, content []byte) (restoreResult, int, string) {
	var res restoreResult
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return res, http.StatusBadRequest, "Invalid ZIP file"
	}

	dataAbs, err := filepath.Abs(cfg.DataDirectory)
	if err != nil {
		return res, http.StatusInternalServerError, "Bad data directory"
	}
	skip := backupsvc.SkipPredicate(cfg.DataDirectory, resolvedBackupDir())

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
			return res, http.StatusBadRequest, fmt.Sprintf("Absolute path in archive: %s", zf.Name)
		}
		clean := filepath.Clean(raw)
		if clean == "." || clean == "" {
			continue
		}
		for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
			if seg == ".." {
				return res, http.StatusBadRequest, fmt.Sprintf("Path traversal in archive: %s", zf.Name)
			}
		}
		dest := filepath.Join(cfg.DataDirectory, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil || !(destAbs == dataAbs || strings.HasPrefix(destAbs, dataAbs+string(filepath.Separator))) {
			return res, http.StatusBadRequest, fmt.Sprintf("Path escapes data dir: %s", zf.Name)
		}
		// SkipPredicate is ancestor-aware for file paths, so entries under
		// skip-listed directories (e.g. cache/plotly.min.js) are dropped too.
		// Deduped like restored, so duplicate zip entries count once.
		if skip(dest, false) {
			if _, dup := skippedEntries[destAbs]; !dup {
				skippedEntries[destAbs] = struct{}{}
				res.skippedProtected++
			}
			continue
		}

		rc, err := zf.Open()
		if err != nil {
			return res, http.StatusBadRequest, fmt.Sprintf("Cannot open entry %s: %v", zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return res, http.StatusBadRequest, fmt.Sprintf("Cannot read entry %s: %v", zf.Name, err)
		}

		// Encrypted blob into unencrypted/locked store -> reject the whole archive.
		if storage.IsAgeEncryptedData(data) && !(store.IsEncrypted() && store.IsUnlocked()) {
			return res, http.StatusBadRequest, fmt.Sprintf(
				"Archive contains encrypted entry %s but destination store is not encrypted/unlocked",
				zf.Name,
			)
		}

		queue = append(queue, prepared{dest: dest, data: data})
		archiveEntries[destAbs] = struct{}{}
	}

	if len(queue) == 0 {
		return res, http.StatusBadRequest, "No restorable files in archive"
	}

	if backupSvc == nil {
		return res, http.StatusInternalServerError, "Backup service not initialized"
	}
	// Hold the snapshot lock for the whole restore so a scheduled snapshot
	// (or a second restore) cannot capture a half-restored data dir.
	release, err := backupSvc.SnapshotAndHold(ctx)
	if err != nil {
		if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
			return res, http.StatusConflict, "a backup is currently running; retry shortly"
		}
		return res, http.StatusInternalServerError, fmt.Sprintf("safety snapshot failed: %v", err)
	}
	defer release()

	// Serialize the entire write+prune against settings saves. The gate
	// (when wired) holds the SettingsManager's lock until the deferred
	// endRewrite runs at function exit — i.e. after pruneRestoreExtras —
	// so no save can interleave with a half-restored settings directory,
	// and endRewrite's cache drop + active-scenario reconciliation happen
	// inside the same critical section. Nothing between here and return
	// may call a SettingsManager method (that would deadlock).
	if restoreGate != nil {
		endRewrite := restoreGate()
		defer endRewrite()
	} else {
		// A nil gate means SetRestoreGate was never called after the last
		// Initialize — the restore proceeds UNSERIALIZED against settings
		// saves (the race the gate exists to prevent). Loud so a wiring
		// regression is visible instead of silently racy.
		log.Printf("backup: restore running without a restore gate; concurrent settings saves are not serialized (call SetRestoreGate after Initialize)")
	}

	for _, p := range queue {
		if err := os.MkdirAll(filepath.Dir(p.dest), 0755); err != nil {
			return res, http.StatusInternalServerError, fmt.Sprintf("mkdir: %v", err)
		}
		if err := store.WriteFile(p.dest, p.data, 0644); err != nil {
			return res, http.StatusInternalServerError, fmt.Sprintf("write %s: %v", p.dest, err)
		}
	}

	res.pruned, res.pruneFailures = pruneRestoreExtras(dataAbs, archiveEntries, skip)
	if res.pruneFailures > 0 {
		log.Printf("restore prune completed with %d failures", res.pruneFailures)
	}
	// archiveEntries (not queue) so duplicate zip entries count once.
	res.restored = len(archiveEntries)
	return res, http.StatusOK, ""
}

// restoreResult summarizes a restoreFromZip run: files written, stale files
// pruned, archive entries dropped by the skip list, and prune removals that
// failed (details in the server log).
type restoreResult struct {
	restored         int
	pruned           int
	skippedProtected int
	pruneFailures    int
}

// restoreResponseMessage renders the client-facing summary for a successful
// restore. noun distinguishes "files" from "test files".
func restoreResponseMessage(res restoreResult, noun string) string {
	msg := fmt.Sprintf("Restored %d %s, removed %d stale files", res.restored, noun, res.pruned)
	if res.skippedProtected > 0 {
		msg += fmt.Sprintf(", skipped %d protected entries", res.skippedProtected)
	}
	if res.pruneFailures > 0 {
		msg += fmt.Sprintf(", %d stale files could not be removed (see server log)", res.pruneFailures)
	}
	return msg
}

// pruneRestoreExtras deletes every file under dataAbs that is neither an
// archive entry nor excluded by skip (the shared backup skip predicate),
// then removes directories left empty. Directories that were already empty
// before the restore are removed too — zip archives cannot represent empty
// directories, so full replace treats them as stale.
func pruneRestoreExtras(dataAbs string, archiveEntries map[string]struct{}, skip func(path string, isDir bool) bool) (int, int) {
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
		if err := store.Remove(pathAbs); err != nil {
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
		if err := store.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) > 0 {
				continue
			}
			log.Printf("restore prune: remove empty dir %s: %v", dir, err)
			failures++
		}
	}
	return removed, failures
}

func HandleRestore(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 50MB for backup files)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error reading file", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		http.Error(w, "Only ZIP backup files are allowed", http.StatusBadRequest)
		return
	}

	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}
	res, status, msg := restoreFromZip(r.Context(), content)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	log.Printf("Restore complete: %d files restored, %d stale files removed, %d protected entries skipped, %d prune failures",
		res.restored, res.pruned, res.skippedProtected, res.pruneFailures)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, restoreResponseMessage(res, "files"))
}

func HandleRestoreTestData(w http.ResponseWriter, r *http.Request) {
	content, err := testdata.TestBackupFS.ReadFile("test_backup.zip")
	if err != nil {
		http.Error(w, "Test backup not available", http.StatusInternalServerError)
		return
	}
	res, status, msg := restoreFromZip(r.Context(), content)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	log.Printf("Test data restore complete: %d files restored, %d stale files removed, %d protected entries skipped, %d prune failures",
		res.restored, res.pruned, res.skippedProtected, res.pruneFailures)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, restoreResponseMessage(res, "test files"))
}

func HandleDeleteAllData(w http.ResponseWriter, r *http.Request) {
	// Read data directory
	entries, err := os.ReadDir(cfg.DataDirectory)
	if err != nil {
		http.Error(w, "Error reading data directory", http.StatusInternalServerError)
		return
	}

	deletedCount := 0
	for _, entry := range entries {
		// Skip BackupDir to defend the safety net even if future code broadens
		// what HandleDeleteAllData removes.
		if cfg.BackupDir != "" {
			backupAbs, _ := filepath.Abs(cfg.BackupDir)
			entryAbs, _ := filepath.Abs(filepath.Join(cfg.DataDirectory, entry.Name()))
			if backupAbs != "" && (entryAbs == backupAbs || strings.HasPrefix(entryAbs, backupAbs+string(filepath.Separator))) {
				continue
			}
		}
		// Only delete CSV files, skip directories and other files
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".csv") {
			continue
		}

		filePath := filepath.Join(cfg.DataDirectory, entry.Name())
		if err := store.Remove(filePath); err != nil {
			log.Printf("Error deleting file %s: %v", filePath, err)
			continue
		}
		deletedCount++
		log.Printf("Deleted file: %s", entry.Name())
	}

	log.Printf("Deleted %d data files", deletedCount)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "Deleted %d files", deletedCount)
}

func HandleEnableEncryption(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirmPassword")

	// Validate password
	if len(password) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	if password != confirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	// Enable encryption
	if err := store.EnableEncryption(password); err != nil {
		log.Printf("Failed to enable encryption: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Encryption enabled successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Encryption enabled")
}

func HandleDisableEncryption(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Get credentials (may be empty for Age method)
	credentials := r.FormValue("password")

	// For password method, require non-empty credentials
	config := store.GetConfig()
	if config != nil && config.Method == storage.AuthMethodPassword && credentials == "" {
		http.Error(w, "Password is required", http.StatusBadRequest)
		return
	}

	// Disable encryption (this verifies the credentials internally)
	if err := store.DisableEncryption(credentials); err != nil {
		log.Printf("Failed to disable encryption: %v", err)
		if errors.Is(err, storage.ErrIncorrectCredentials) {
			if config != nil && config.Method == storage.AuthMethodPassword {
				http.Error(w, "Incorrect password", http.StatusUnauthorized)
			} else {
				http.Error(w, fmt.Sprintf("Decryption failed: %v", err), http.StatusUnauthorized)
			}
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Encryption disabled successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Encryption disabled")
}

func HandleEncryptionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"encrypted": store.IsEncrypted()})
}

func HandlePlotly(w http.ResponseWriter, r *http.Request) {
	cachePath := filepath.Join(cfg.DataDirectory, "cache", "plotly.min.js")

	// Try serving from cache
	if data, err := os.ReadFile(cachePath); err == nil {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year
		_, _ = w.Write(data)
		return
	}

	// Fetch from CDN
	log.Println("Fetching plotly.min.js from CDN...")
	resp, err := http.Get("https://cdn.plot.ly/plotly-2.35.2.min.js")
	if err != nil {
		http.Error(w, "Failed to fetch plotly: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "CDN returned status: "+resp.Status, http.StatusBadGateway)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read plotly response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Cache for next time
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		log.Printf("Warning: could not create cache directory: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		log.Printf("Warning: could not cache plotly.min.js: %v", err)
	} else {
		log.Println("Cached plotly.min.js for future requests")
	}

	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	_, _ = w.Write(data)
}

// HandleUnlockPage serves the unlock page for encrypted storage
func HandleUnlockPage(w http.ResponseWriter, r *http.Request) {
	// If storage is not locked, redirect to dashboard
	if !IsStorageLocked() {
		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
		return
	}

	if err := renderer.Render(w, "unlock", nil); err != nil {
		http.Error(w, "Failed to render unlock page", http.StatusInternalServerError)
		log.Printf("Error rendering unlock page: %v", err)
	}
}

// HandleUnlock unlocks the encrypted storage with the provided credentials
func HandleUnlock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Get credentials (may be empty for Age/SSH methods)
	credentials := r.FormValue("password")

	// For password method, require non-empty credentials
	config := store.GetConfig()
	if config != nil && config.Method == storage.AuthMethodPassword && credentials == "" {
		http.Error(w, "Password is required", http.StatusBadRequest)
		return
	}

	if err := store.Unlock(credentials); err != nil {
		log.Printf("Failed unlock attempt: %v", err)
		if config != nil && config.Method == storage.AuthMethodPassword {
			http.Error(w, "Incorrect password", http.StatusUnauthorized)
		} else {
			http.Error(w, fmt.Sprintf("Unlock failed: %v", err), http.StatusUnauthorized)
		}
		return
	}

	log.Printf("Storage unlocked successfully via web interface")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Unlocked")
}

// IsStorageLocked returns true if the storage is encrypted and not yet unlocked
func IsStorageLocked() bool {
	return store != nil && store.IsEncrypted() && !store.IsUnlocked()
}

// MethodInfo describes an available authentication method
type MethodInfo struct {
	Method      string `json:"method"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
	Current     bool   `json:"current"`
}

// HandleGetAuthMethods returns available authentication methods
func HandleGetAuthMethods(w http.ResponseWriter, r *http.Request) {
	methods := []MethodInfo{
		{
			Method:      string(storage.AuthMethodPassword),
			Enabled:     true,
			Description: "Password-based encryption (scrypt)",
		},
		{
			Method:      string(storage.AuthMethodAge),
			Enabled:     true,
			Description: "Age identity file (X25519 key)",
		},
		{
			Method:      string(storage.AuthMethodSSH),
			Enabled:     true,
			Description: "SSH key encryption",
		},
		{
			Method:      string(storage.AuthMethodYubiKey),
			Enabled:     storage.IsYubiKeyPluginInstalled(),
			Description: "YubiKey hardware key",
		},
	}

	// Mark current method if encrypted
	currentMethod := store.GetAuthMethod()
	for i := range methods {
		if storage.AuthMethod(methods[i].Method) == currentMethod {
			methods[i].Current = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"current_method":    currentMethod,
		"available_methods": methods,
	})
}

// DetectedKeys contains detected SSH keys and age identities
type DetectedKeys struct {
	SSHKeys           []storage.SSHKeyInfo `json:"ssh_keys"`
	AgeIdentities     []string             `json:"age_identities"`
	YubiKeyInstalled  bool                 `json:"yubikey_installed"`
	YubiKeyIdentities []string             `json:"yubikey_identities"`
}

// HandleDetectKeys returns detected SSH keys and age identities
func HandleDetectKeys(w http.ResponseWriter, r *http.Request) {
	keys := DetectedKeys{}

	// Detect SSH keys
	sshKeys, err := storage.DetectSSHKeys()
	if err == nil {
		keys.SSHKeys = sshKeys
	}

	// Detect age identities
	ageIdentities, err := storage.DetectAgeIdentities()
	if err == nil {
		keys.AgeIdentities = ageIdentities
	}

	// Detect YubiKey
	keys.YubiKeyInstalled = storage.IsYubiKeyPluginInstalled()
	if keys.YubiKeyInstalled {
		yubikeyIdentities, err := storage.DetectYubiKeyIdentities()
		if err == nil {
			keys.YubiKeyIdentities = yubikeyIdentities
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keys)
}

// HandleEnableEncryptionWithMethod enables encryption with a specific auth method
func HandleEnableEncryptionWithMethod(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	method := storage.AuthMethod(r.FormValue("method"))

	switch method {
	case storage.AuthMethodPassword, "":
		// Default to password method
		handleEnablePasswordEncryption(w, r)

	case storage.AuthMethodAge:
		handleEnableAgeEncryption(w, r)

	case storage.AuthMethodSSH:
		handleEnableSSHEncryption(w, r)

	case storage.AuthMethodYubiKey:
		handleEnableYubiKeyEncryption(w, r)

	default:
		http.Error(w, "Unknown encryption method", http.StatusBadRequest)
	}
}

func handleEnablePasswordEncryption(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirmPassword")

	if len(password) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	if password != confirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	if err := store.EnableEncryption(password); err != nil {
		log.Printf("Failed to enable encryption: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Password encryption enabled successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Encryption enabled with password")
}

func handleEnableAgeEncryption(w http.ResponseWriter, r *http.Request) {
	identityPath := r.FormValue("identity_path")
	generateNew := r.FormValue("generate_new") == "true"

	var provider *storage.AgeProvider
	var err error

	if generateNew {
		// Generate a new identity
		if identityPath == "" {
			identityPath = "~/.config/budget2/age-identity.txt"
		}
		provider, err = storage.GenerateAgeIdentity(identityPath)
		if err != nil {
			http.Error(w, "Failed to generate age identity: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if identityPath == "" {
			http.Error(w, "Identity path is required", http.StatusBadRequest)
			return
		}
		provider, err = storage.NewAgeProvider(identityPath)
		if err != nil {
			http.Error(w, "Failed to load age identity: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	config := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}

	if err := store.EnableEncryptionWithProvider(provider, config); err != nil {
		log.Printf("Failed to enable age encryption: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Age encryption enabled successfully")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"message":    "Encryption enabled with age identity",
		"public_key": provider.GetPublicKey(),
	})
}

func handleEnableSSHEncryption(w http.ResponseWriter, r *http.Request) {
	keyPath := r.FormValue("ssh_key_path")
	passphrase := r.FormValue("passphrase")

	if keyPath == "" {
		http.Error(w, "SSH key path is required", http.StatusBadRequest)
		return
	}

	provider, err := storage.NewSSHProvider(keyPath)
	if err != nil {
		http.Error(w, "Failed to load SSH key: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Unlock with passphrase if provided
	if err := provider.Unlock(passphrase); err != nil {
		http.Error(w, "Failed to unlock SSH key: "+err.Error(), http.StatusBadRequest)
		return
	}

	config := &storage.EncryptionConfig{
		Method:     storage.AuthMethodSSH,
		SSHKeyPath: keyPath,
	}

	if err := store.EnableEncryptionWithProvider(provider, config); err != nil {
		log.Printf("Failed to enable SSH encryption: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("SSH encryption enabled successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Encryption enabled with SSH key")
}

func handleEnableYubiKeyEncryption(w http.ResponseWriter, r *http.Request) {
	identityStr := r.FormValue("yubikey_identity")
	recipientStr := r.FormValue("yubikey_recipient")

	if identityStr == "" {
		http.Error(w, "YubiKey identity is required", http.StatusBadRequest)
		return
	}

	if !storage.IsYubiKeyPluginInstalled() {
		http.Error(w, "age-plugin-yubikey is not installed", http.StatusBadRequest)
		return
	}

	var provider *storage.YubiKeyProvider
	var err error

	if recipientStr != "" {
		provider, err = storage.NewYubiKeyProviderWithRecipient(identityStr, recipientStr)
	} else {
		provider, err = storage.NewYubiKeyProvider(identityStr)
	}

	if err != nil {
		http.Error(w, "Failed to load YubiKey: "+err.Error(), http.StatusBadRequest)
		return
	}

	config := &storage.EncryptionConfig{
		Method:           storage.AuthMethodYubiKey,
		YubiKeyIdentity:  identityStr,
		YubiKeyRecipient: recipientStr,
	}

	if err := store.EnableEncryptionWithProvider(provider, config); err != nil {
		log.Printf("Failed to enable YubiKey encryption: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("YubiKey encryption enabled successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "Encryption enabled with YubiKey")
}

// HandleChangeAuthMethod changes the encryption method (re-encrypts data)
func HandleChangeAuthMethod(w http.ResponseWriter, r *http.Request) {
	// For now, to change methods, user must disable encryption and re-enable
	// A more sophisticated approach would migrate in place
	http.Error(w, "To change authentication method, please disable encryption first and then re-enable with the new method", http.StatusBadRequest)
}

// HandleGetEncryptionConfig returns the current encryption configuration
func HandleGetEncryptionConfig(w http.ResponseWriter, r *http.Request) {
	config := store.GetConfig()
	if config == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"encrypted": false,
		})
		return
	}

	// Don't expose sensitive paths fully, just method info
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"encrypted": true,
		"method":    config.Method,
	})
}

// HandleYubiKeyIdentity returns the identity for a given YubiKey recipient
func HandleYubiKeyIdentity(w http.ResponseWriter, r *http.Request) {
	recipient := r.URL.Query().Get("recipient")
	if recipient == "" {
		http.Error(w, "recipient parameter is required", http.StatusBadRequest)
		return
	}

	if !storage.IsYubiKeyPluginInstalled() {
		http.Error(w, "age-plugin-yubikey is not installed", http.StatusBadRequest)
		return
	}

	identity, err := storage.GetYubiKeyIdentityForRecipient(recipient)
	if err != nil {
		log.Printf("Failed to get YubiKey identity: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"identity":  identity,
		"recipient": recipient,
	})
}

func HandleSetAutoBackupEnabled(w http.ResponseWriter, r *http.Request) {
	if backupSvc == nil {
		http.Error(w, "backup service not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	val := r.FormValue("enabled")
	var enabled bool
	switch val {
	case "true", "1", "on", "yes":
		enabled = true
	case "false", "0", "off", "no":
		enabled = false
	default:
		http.Error(w, "enabled must be true/false", http.StatusBadRequest)
		return
	}
	if err := backupSvc.SetEnabled(enabled); err != nil {
		http.Error(w, fmt.Sprintf("persist enabled: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleYubiKeySetup returns instructions for YubiKey setup
// YubiKey setup requires terminal interaction and cannot be done via web
func HandleYubiKeySetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !storage.IsYubiKeyPluginInstalled() {
		http.Error(w, "age-plugin-yubikey is not installed", http.StatusBadRequest)
		return
	}

	// Get YubiKey info to provide the right setup command
	keys, _ := storage.DetectYubiKeys()
	setupCmd := "age-plugin-yubikey --generate"
	if len(keys) > 0 && keys[0].SetupCommand != "" {
		setupCmd = keys[0].SetupCommand
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":         "YubiKey setup requires terminal interaction",
		"setup_command": setupCmd,
		"instructions":  "Run the setup command in your terminal, then click 'Refresh' to detect your new identity.",
	})
}
