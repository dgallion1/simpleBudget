package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/testdata"
)

var (
	cfg      *config.Config
	store    *storage.Storage
	renderer *templates.Renderer
)

// Initialize sets up the backup package with required dependencies
func Initialize(c *config.Config, s *storage.Storage, r *templates.Renderer) {
	cfg = c
	store = s
	renderer = r
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// exitFunc is a package-level variable for testing.
var exitFunc = os.Exit

func HandleKillServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Server shutting down...\n"))
	log.Println("Received /killme request, shutting down")
	go func() {
		time.Sleep(100 * time.Millisecond)
		exitFunc(0)
	}()
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
	defer zw.Close()

	// Walk the data directory
	dataDir := cfg.DataDirectory
	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip encryption marker and verify files
		base := filepath.Base(path)
		if base == ".encrypted" || base == ".encryption-verify" {
			return nil
		}

		// Create a file in the zip archive
		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}

		f, err := zw.Create(relPath)
		if err != nil {
			return err
		}

		// Read file via storage (handles decryption)
		// Backup files are always unencrypted for portability
		file, err := store.OpenFile(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Copy file content to zip writer
		_, err = io.Copy(f, file)
		return err
	})

	if err != nil {
		log.Printf("Error creating backup: %v", err)
		// Note: Since we've already started writing headers and potentially content,
		// we can't easily change to an error response, but we can log it.
	}
}

// restoreFromZip extracts every entry of the supplied archive into
// cfg.DataDirectory using store.WriteFile. It validates the entire
// archive before writing any files, so a malformed entry rejects the
// whole operation atomically.
//
// Returns (count, http status, error message). On success, status is 200.
func restoreFromZip(content []byte) (int, int, string) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return 0, http.StatusBadRequest, "Invalid ZIP file"
	}

	dataAbs, err := filepath.Abs(cfg.DataDirectory)
	if err != nil {
		return 0, http.StatusInternalServerError, "Bad data directory"
	}

	type prepared struct {
		dest string
		data []byte
	}
	var queue []prepared

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		// Sanitize: forbid absolute, forbid ".." segments, must stay under data dir.
		raw := filepath.ToSlash(zf.Name)
		if strings.HasPrefix(raw, "/") {
			return 0, http.StatusBadRequest, fmt.Sprintf("Absolute path in archive: %s", zf.Name)
		}
		clean := filepath.Clean(raw)
		if clean == "." || clean == "" {
			continue
		}
		for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
			if seg == ".." {
				return 0, http.StatusBadRequest, fmt.Sprintf("Path traversal in archive: %s", zf.Name)
			}
		}
		dest := filepath.Join(cfg.DataDirectory, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil || !(destAbs == dataAbs || strings.HasPrefix(destAbs, dataAbs+string(filepath.Separator))) {
			return 0, http.StatusBadRequest, fmt.Sprintf("Path escapes data dir: %s", zf.Name)
		}

		rc, err := zf.Open()
		if err != nil {
			return 0, http.StatusBadRequest, fmt.Sprintf("Cannot open entry %s: %v", zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return 0, http.StatusBadRequest, fmt.Sprintf("Cannot read entry %s: %v", zf.Name, err)
		}

		// Encrypted blob into unencrypted/locked store → reject the whole archive.
		if storage.IsAgeEncryptedData(data) && !(store.IsEncrypted() && store.IsUnlocked()) {
			return 0, http.StatusBadRequest, fmt.Sprintf(
				"Archive contains encrypted entry %s but destination store is not encrypted/unlocked",
				zf.Name,
			)
		}

		queue = append(queue, prepared{dest: dest, data: data})
	}

	if len(queue) == 0 {
		return 0, http.StatusBadRequest, "No restorable files in archive"
	}

	for _, p := range queue {
		if err := os.MkdirAll(filepath.Dir(p.dest), 0755); err != nil {
			return 0, http.StatusInternalServerError, fmt.Sprintf("mkdir: %v", err)
		}
		if err := store.WriteFile(p.dest, p.data, 0644); err != nil {
			return 0, http.StatusInternalServerError, fmt.Sprintf("write %s: %v", p.dest, err)
		}
	}
	return len(queue), http.StatusOK, ""
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
	defer file.Close()

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
	count, status, msg := restoreFromZip(content)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	log.Printf("Restore complete: %d files restored", count)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Restored %d files", count)
}

func HandleRestoreTestData(w http.ResponseWriter, r *http.Request) {
	content, err := testdata.TestBackupFS.ReadFile("test_backup.zip")
	if err != nil {
		http.Error(w, "Test backup not available", http.StatusInternalServerError)
		return
	}
	count, status, msg := restoreFromZip(content)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	log.Printf("Test data restore complete: %d files restored", count)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Restored %d test files", count)
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
	fmt.Fprintf(w, "Deleted %d files", deletedCount)
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
	fmt.Fprint(w, "Encryption enabled")
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
		if strings.Contains(err.Error(), "incorrect") {
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
	fmt.Fprint(w, "Encryption disabled")
}

func HandleEncryptionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"encrypted": store.IsEncrypted()})
}

func HandlePlotly(w http.ResponseWriter, r *http.Request) {
	cachePath := filepath.Join(cfg.DataDirectory, "cache", "plotly.min.js")

	// Try serving from cache
	if data, err := os.ReadFile(cachePath); err == nil {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=31536000") // 1 year
		w.Write(data)
		return
	}

	// Fetch from CDN
	log.Println("Fetching plotly.min.js from CDN...")
	resp, err := http.Get("https://cdn.plot.ly/plotly-2.35.2.min.js")
	if err != nil {
		http.Error(w, "Failed to fetch plotly: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

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
	w.Write(data)
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
	fmt.Fprint(w, "Unlocked")
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
	json.NewEncoder(w).Encode(map[string]interface{}{
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
	json.NewEncoder(w).Encode(keys)
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
	fmt.Fprint(w, "Encryption enabled with password")
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
	json.NewEncoder(w).Encode(map[string]interface{}{
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
	fmt.Fprint(w, "Encryption enabled with SSH key")
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
	fmt.Fprint(w, "Encryption enabled with YubiKey")
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"encrypted": false,
		})
		return
	}

	// Don't expose sensitive paths fully, just method info
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"identity":  identity,
		"recipient": recipient,
	})
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":         "YubiKey setup requires terminal interaction",
		"setup_command": setupCmd,
		"instructions":  "Run the setup command in your terminal, then click 'Refresh' to detect your new identity.",
	})
}
