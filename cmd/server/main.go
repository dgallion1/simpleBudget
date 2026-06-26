// Command server is the SimpleBudget HTTP application. It boots the chi
// router, wires the dataloader / retirement / backup services into the
// handler packages, serves the dashboard and what-if UI over HTMX, and
// installs the graceful-shutdown signal handler that triggers a final
// backup snapshot before exit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"budget2/internal/config"
	"budget2/internal/handlers/backup"
	"budget2/internal/handlers/dashboard"
	"budget2/internal/handlers/duplicates"
	"budget2/internal/handlers/explorer"
	"budget2/internal/handlers/insights"
	"budget2/internal/handlers/majorexpenses"
	"budget2/internal/handlers/whatif"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/templates"
	"budget2/internal/version"
	"budget2/web"
)

var (
	cfg           *config.Config
	store         *storage.Storage
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
	backupService *backupsvc.Service
)

// SetupDependencies initializes all global dependencies with the given config.
// This is exported for testing purposes.
func SetupDependencies(c *config.Config) error {
	cfg = c

	// Initialize data loader with storage
	loader = dataloader.New(cfg.DataDirectory, store)

	// Initialize template renderer
	var err error
	if cfg.Debug {
		// Development: use filesystem for hot reload
		renderer, err = templates.New(cfg.TemplatesDirectory, true)
	} else {
		// Production: use embedded filesystem
		templatesFS, _ := fs.Sub(web.EmbeddedFS, "templates")
		renderer, err = templates.NewFromFS(templatesFS, false)
	}
	if err != nil {
		return fmt.Errorf("template validation failed: %w", err)
	}

	// Initialize retirement settings manager with storage
	settingsDir := filepath.Join(cfg.DataDirectory, "settings")
	retirementMgr = retirement.NewSettingsManager(settingsDir, store)

	// Initialize backup service
	backupService, err = backupsvc.New(backupsvc.Config{
		BackupDir: cfg.BackupDir,
		DataDir:   cfg.DataDirectory,
		Store:     store,
	})
	if err != nil {
		return fmt.Errorf("backup service init: %w", err)
	}

	// Initialize handler packages
	dashboard.Initialize(loader, renderer, retirementMgr)
	explorer.Initialize(loader, renderer, cfg, store)
	whatif.Initialize(loader, renderer, retirementMgr)
	insights.Initialize(loader, renderer)
	majorexpenses.Initialize(loader, renderer)
	duplicates.Initialize(loader, renderer)
	backup.Initialize(cfg, store, renderer, backupService)

	return nil
}

// SetupRouter creates and configures the HTTP router.
// This is exported for testing purposes.
func SetupRouter() chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Static files
	var fileServer http.Handler
	if cfg.Debug {
		// Development: serve from filesystem
		fileServer = http.FileServer(http.Dir(cfg.StaticDirectory))
	} else {
		// Production: serve from embedded filesystem
		staticFS, _ := fs.Sub(web.EmbeddedFS, "static")
		fileServer = http.FileServer(http.FS(staticFS))
	}

	// Plotly handler - fetches from CDN and caches locally
	r.Get("/static/vendor/plotly.min.js", backup.HandlePlotly)

	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Unlock routes (always accessible)
	r.Get("/unlock", backup.HandleUnlockPage)
	r.Post("/unlock", backup.HandleUnlock)

	// File manager accessible when locked (has its own unlock UI)
	r.Get("/filemanager", explorer.HandleFileManagerPage)
	r.Get("/encryption/config", backup.HandleGetEncryptionConfig)

	// YubiKey routes (accessible when locked for initial setup)
	r.Get("/encryption/yubikey-identity", backup.HandleYubiKeyIdentity)
	r.Post("/encryption/yubikey-setup", backup.HandleYubiKeySetup)

	// Health and control endpoints (always accessible)
	r.Get("/api/health", backup.HandleHealth)
	r.Get("/api/version", handleVersion)
	r.Get("/killme", backup.HandleKillServer)

	// Apply lock check middleware to protected routes
	r.Group(func(r chi.Router) {
		r.Use(lockCheckMiddleware)

		// Root redirect
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
		})

		// Register handler packages
		dashboard.RegisterRoutes(r)
		explorer.RegisterRoutes(r)
		whatif.RegisterRoutes(r)
		insights.RegisterRoutes(r)
		majorexpenses.RegisterRoutes(r)
		duplicates.RegisterRoutes(r)

		// File manager page (also registered outside middleware for unlock access)

		// Backup and restore routes
		r.Get("/backup", backup.HandleBackup)
		r.Post("/restore", backup.HandleRestore)
		r.Post("/restore/test-data", backup.HandleRestoreTestData)
		r.Delete("/data/all", backup.HandleDeleteAllData)
		r.Get("/backup/status", backup.HandleBackupStatus)
		r.Post("/backup/auto-enabled", backup.HandleSetAutoBackupEnabled)
		r.Post("/backup/open-dir", backup.HandleOpenBackupDir)
		r.Post("/backup/plaintext", backup.HandleBackupPlaintext)

		// Encryption management routes
		r.Post("/encryption/enable", backup.HandleEnableEncryptionWithMethod)
		r.Post("/encryption/disable", backup.HandleDisableEncryption)
		r.Get("/encryption/status", backup.HandleEncryptionStatus)
		r.Get("/encryption/methods", backup.HandleGetAuthMethods)
		r.Get("/encryption/detect-keys", backup.HandleDetectKeys)
		r.Get("/encryption/config", backup.HandleGetEncryptionConfig)
		r.Post("/encryption/change-method", backup.HandleChangeAuthMethod)
	})

	return r
}

// lockCheckMiddleware redirects to unlock page if storage is encrypted but locked
func lockCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if backup.IsStorageLocked() {
			http.Redirect(w, r, "/unlock", http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// run initializes the application and returns the configured handler and listen address.
// Extracted from main() for testability.
func run() (http.Handler, string, error) {
	// Log version information
	versionInfo := version.Get()
	log.Printf("SimpleBudget %s", versionInfo.String())
	if warning := versionInfo.Check(); warning != "" {
		log.Printf("%s", warning)
	}

	// Load configuration
	c := config.Load()
	log.Printf("Starting Budget Dashboard on %s", c.ListenAddr)
	log.Printf("Data directory: %s", c.DataDirectory)

	// Initialize storage
	var err error
	store, err = storage.New(c.DataDirectory)
	if err != nil {
		return nil, "", fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Log encryption status
	if store.IsEncrypted() {
		log.Printf("Encrypted storage detected - unlock via web interface at /unlock")
	}

	// Kill any previous instance running on this port
	killPreviousInstance(c.ListenAddr)

	// Setup dependencies
	if err := SetupDependencies(c); err != nil {
		return nil, "", err
	}

	// Setup router
	r := SetupRouter()

	log.Printf("Server starting on %s", cfg.ListenAddr)
	return r, cfg.ListenAddr, nil
}

func main() {
	handler, addr, err := run()
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}

	// Initial stale-check (fast no-op if a fresh backup already exists).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := backupService.SnapshotIfStale(ctx, 24*time.Hour); err != nil {
			log.Printf("backup: initial snapshot: %v", err)
		}
	}()

	// Hourly scheduler — exits when the server context is cancelled.
	schedCtx, schedCancel := context.WithCancel(context.Background())
	go backupService.Run(schedCtx, 24*time.Hour)

	// Start HTTP server in background. ReadHeaderTimeout bounds how long a
	// client may take to send request headers, closing the door on Slowloris-
	// style header-dribble connection exhaustion.
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Print("shutting down")

	// Stop scheduler.
	schedCancel()

	// Drain in-flight HTTP requests first so the shutdown snapshot captures a
	// quiescent data directory (bounded to 30 s).
	srvCtx, srvCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer srvCancel()
	if err := srv.Shutdown(srvCtx); err != nil {
		log.Printf("http: graceful shutdown: %v", err)
	}

	// Best-effort shutdown snapshot, with its own independent deadline.
	snapCtx, snapCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer snapCancel()
	if err := backupService.Snapshot(snapCtx); err != nil &&
		!errors.Is(err, backupsvc.ErrSnapshotInProgress) {
		log.Printf("backup: shutdown snapshot: %v", err)
	}
}

// handleVersion returns version information as JSON
func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(version.Get()); err != nil {
		log.Printf("version: encoding JSON: %v", err)
	}
}

// killPreviousInstance attempts to shut down any existing server on the same address
func killPreviousInstance(addr string) {
	// Build the killme URL
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	killURL := fmt.Sprintf("http://%s/killme", host)

	// Try to contact the existing server
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(killURL)
	if err != nil {
		// No server running or not reachable - that's fine
		return
	}
	resp.Body.Close()

	log.Printf("Sent shutdown signal to previous instance, waiting...")

	// Wait for the old server to release the port (up to 3 seconds)
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		// Try to connect - if it fails, the old server is gone
		resp, err := client.Get(fmt.Sprintf("http://%s/health", host))
		if err != nil {
			log.Printf("Previous instance terminated")
			return
		}
		resp.Body.Close()
	}
	log.Printf("Warning: previous instance may still be running")
}
