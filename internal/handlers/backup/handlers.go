// Package backup serves the HTTP endpoints behind the Backup admin panel:
// list snapshots, trigger an on-demand snapshot, restore from a zip,
// toggle the scheduler, and stream a downloadable archive. Pure HTTP
// glue around services/backup — no snapshot logic lives here.
package backup

import (
	"archive/zip"
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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/config"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/restore"
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

// SettingsRewriteGate serializes an external rewrite of the settings files
// against the settings manager's saves. Defined in internal/services/restore,
// aliased here so existing callers and tests keep compiling.
type SettingsRewriteGate = restore.SettingsRewriteGate

// RewriteGateFunc adapts a bare acquire function to SettingsRewriteGate.
type RewriteGateFunc = restore.RewriteGateFunc

// restoreSvc performs the actual restore. Built in Initialize so the gate and
// the data/backup directories cannot be registered out of order or forgotten.
var restoreSvc *restore.Service

// Initialize sets up the backup package with required dependencies. The
// gate is part of the wiring, not a post-Initialize registration, so it
// cannot be forgotten or installed in the wrong order; pass nil only when
// no settings manager exists (isolated tests) — restores then run
// unserialized, loudly.
//
// It returns the restore service it built so cmd/server can hand that exact
// instance to the MCP server: the browser's /restore and the restore_backup
// tool must share one service, or they would be two objects racing over the
// same data directory with no shared snapshot hold between them.
func Initialize(c *config.Config, s *storage.Storage, r *templates.Renderer, b *backupsvc.Service, gate SettingsRewriteGate) *restore.Service {
	cfg = c
	store = s
	renderer = r
	backupSvc = b

	rd := restore.Deps{
		DataDir:   c.DataDirectory,
		BackupDir: resolvedBackupDir(),
		Store:     s,
		Gate:      gate,
	}
	// b is a *backupsvc.Service; restore.Deps.Backups is an interface, so
	// assigning a nil *backupsvc.Service unconditionally would yield a
	// non-nil interface holding a nil pointer and restore.ErrNoBackupService
	// would never fire. Guard it.
	if b != nil {
		rd.Backups = b
	}
	restoreSvc = restore.New(rd)
	return restoreSvc
}

// RegisterPublicRoutes registers the endpoints that must stay reachable
// while storage is locked: unlock, initial YubiKey setup, the encryption
// config probe, health, and the kill switch. Everything else backup owns
// goes through RegisterRoutes behind the lock-check middleware.
func RegisterPublicRoutes(r chi.Router) {
	r.Get("/unlock", HandleUnlockPage)
	r.Post("/unlock", HandleUnlock)
	r.Get("/encryption/config", HandleGetEncryptionConfig)
	r.Get("/encryption/yubikey-identity", HandleYubiKeyIdentity)
	r.Post("/encryption/yubikey-setup", HandleYubiKeySetup)
	r.Get("/api/health", HandleHealth)
	r.Get("/killme", HandleKillServer)
}

// RegisterRoutes registers the lock-protected backup, restore, and
// encryption admin routes, matching the other handler packages.
func RegisterRoutes(r chi.Router) {
	r.Get("/backup", HandleBackup)
	r.Post("/restore", HandleRestore)
	r.Post("/restore/test-data", HandleRestoreTestData)
	r.Delete("/data/all", HandleDeleteAllData)
	r.Get("/backup/status", HandleBackupStatus)
	r.Post("/backup/auto-enabled", HandleSetAutoBackupEnabled)
	r.Post("/backup/open-dir", HandleOpenBackupDir)
	r.Post("/backup/plaintext", HandleBackupPlaintext)

	r.Post("/encryption/enable", HandleEnableEncryptionWithMethod)
	r.Post("/encryption/disable", HandleDisableEncryption)
	r.Get("/encryption/status", HandleEncryptionStatus)
	r.Get("/encryption/methods", HandleGetAuthMethods)
	r.Get("/encryption/detect-keys", HandleDetectKeys)
	r.Get("/encryption/config", HandleGetEncryptionConfig)
	r.Post("/encryption/change-method", HandleChangeAuthMethod)
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
		// Counted through the service rather than an inline glob, so this
		// page and list_backups cannot disagree about what an archive is:
		// the glob counted budget_backup_NOT_A_DATE.zip as one, the service
		// does not. Without a service the count stays 0 rather than falling
		// back to a glob -- that configuration has no backups (enabled is
		// false above for the same reason), and a second definition of
		// "archive" living here is exactly what this replaced.
		if backupSvc != nil {
			archives, err := backupSvc.List()
			if err == nil {
				resp.SnapshotCount = len(archives)
			}
		}
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
		if !backupsvc.ArchiveEntry(path, info) {
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
		if !backupsvc.ArchiveEntry(path, info) {
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

// restoreFailure maps a restore service error to the status and message this
// endpoint has always returned. The three static messages are preserved
// verbatim because they are user-facing; the detail-carrying cases render the
// service's own message, which now names the offending entry.
func restoreFailure(err error) (int, string) {
	switch {
	case errors.Is(err, backupsvc.ErrSnapshotInProgress):
		return http.StatusConflict, "a backup is currently running; retry shortly"
	case errors.Is(err, restore.ErrInvalidArchive):
		return http.StatusBadRequest, "Invalid ZIP file"
	case errors.Is(err, restore.ErrEmptyArchive):
		return http.StatusBadRequest, "No restorable files in archive"
	case errors.Is(err, restore.ErrUnsafePath),
		errors.Is(err, restore.ErrUnreadableEntry),
		errors.Is(err, restore.ErrEncryptedEntry):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, restore.ErrBadDataDir):
		return http.StatusInternalServerError, "Bad data directory"
	case errors.Is(err, restore.ErrNoBackupService):
		return http.StatusInternalServerError, "Backup service not initialized"
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// restoreResponseMessage renders the client-facing summary for a successful
// restore. noun distinguishes "files" from "test files".
func restoreResponseMessage(res restore.Result, noun string) string {
	msg := fmt.Sprintf("Restored %d %s, removed %d stale files", res.Restored, noun, res.Pruned)
	if res.SkippedProtected > 0 {
		msg += fmt.Sprintf(", skipped %d protected entries", res.SkippedProtected)
	}
	if res.PruneFailures > 0 {
		msg += fmt.Sprintf(", %d stale files could not be removed (see server log)", res.PruneFailures)
	}
	return msg
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
	if restoreSvc == nil {
		http.Error(w, "Restore service not initialized", http.StatusInternalServerError)
		return
	}
	res, err := restoreSvc.FromZip(r.Context(), content)
	if err != nil {
		status, msg := restoreFailure(err)
		http.Error(w, msg, status)
		return
	}
	log.Printf("Restore complete: %d files restored, %d stale files removed, %d protected entries skipped, %d prune failures",
		res.Restored, res.Pruned, res.SkippedProtected, res.PruneFailures)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, restoreResponseMessage(res, "files"))
}

func HandleRestoreTestData(w http.ResponseWriter, r *http.Request) {
	content, err := testdata.TestBackupFS.ReadFile("test_backup.zip")
	if err != nil {
		http.Error(w, "Test backup not available", http.StatusInternalServerError)
		return
	}
	if restoreSvc == nil {
		http.Error(w, "Restore service not initialized", http.StatusInternalServerError)
		return
	}
	res, err := restoreSvc.FromZip(r.Context(), content)
	if err != nil {
		status, msg := restoreFailure(err)
		http.Error(w, msg, status)
		return
	}
	log.Printf("Test data restore complete: %d files restored, %d stale files removed, %d protected entries skipped, %d prune failures",
		res.Restored, res.Pruned, res.SkippedProtected, res.PruneFailures)
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
	// This write is a bare os.WriteFile: if cachePath is a symlink, it writes
	// through the link rather than replacing it, outside the stage-and-rename
	// contract documented on storage.atomicWrite. That is acceptable here
	// because this is only an app-managed cache (worst case, the cached bytes
	// land in the link's target); it is not a pattern to copy for user data.
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
