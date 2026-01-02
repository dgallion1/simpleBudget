package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"budget2/internal/config"
	"budget2/internal/handlers/backup"
	"budget2/internal/handlers/dashboard"
	"budget2/internal/handlers/explorer"
	"budget2/internal/handlers/insights"
	"budget2/internal/handlers/whatif"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
	"budget2/web"
)

var (
	cfg           *config.Config
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
)

// SetupDependencies initializes all global dependencies with the given config.
// This is exported for testing purposes.
func SetupDependencies(c *config.Config) error {
	cfg = c

	// Initialize data loader
	loader = dataloader.New(cfg.DataDirectory)

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

	// Initialize retirement settings manager
	settingsDir := filepath.Join(cfg.DataDirectory, "settings")
	retirementMgr = retirement.NewSettingsManager(settingsDir)

	// Initialize handler packages
	dashboard.Initialize(loader, renderer)
	explorer.Initialize(loader, renderer, cfg)
	whatif.Initialize(loader, renderer, retirementMgr)
	insights.Initialize(loader, renderer)
	backup.Initialize(cfg)

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

	// Root redirect
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	})

	// Register handler packages
	dashboard.RegisterRoutes(r)
	explorer.RegisterRoutes(r)
	whatif.RegisterRoutes(r)
	insights.RegisterRoutes(r)

	// Health and control endpoints
	r.Get("/api/health", backup.HandleHealth)
	r.Get("/killme", backup.HandleKillServer)

	// File manager page
	r.Get("/filemanager", explorer.HandleFileManagerPage)

	// Backup and restore routes
	r.Get("/backup", backup.HandleBackup)
	r.Post("/restore", backup.HandleRestore)
	r.Post("/restore/test-data", backup.HandleRestoreTestData)
	r.Delete("/data/all", backup.HandleDeleteAllData)

	return r
}

func main() {
	// Load configuration
	c := config.Load()
	log.Printf("Starting Budget Dashboard on %s", c.ListenAddr)
	log.Printf("Data directory: %s", c.DataDirectory)

	// Kill any previous instance running on this port
	killPreviousInstance(c.ListenAddr)

	// Setup dependencies
	if err := SetupDependencies(c); err != nil {
		log.Fatalf("FATAL: %v", err)
	}

	// Setup router
	r := SetupRouter()

	// Start server
	log.Printf("Server starting on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, r))
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
