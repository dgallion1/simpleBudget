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
	"budget2/testdata"
)

var (
	cfg   *config.Config
	store *storage.Storage
)

// Initialize sets up the backup package with required dependencies
func Initialize(c *config.Config, s *storage.Storage) {
	cfg = c
	store = s
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func HandleKillServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Server shutting down...\n"))
	log.Println("Received /killme request, shutting down")
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
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

	// Read the entire file into memory to create a ReaderAt
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Open zip archive from memory
	zipReader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		http.Error(w, "Invalid ZIP file", http.StatusBadRequest)
		return
	}

	// Extract all CSV files from the zip
	restoredCount := 0
	for _, zipFile := range zipReader.File {
		// Skip directories
		if zipFile.FileInfo().IsDir() {
			continue
		}

		// Only extract CSV files
		if !strings.HasSuffix(strings.ToLower(zipFile.Name), ".csv") {
			continue
		}

		// Sanitize filename - use only the base name to prevent path traversal
		baseName := filepath.Base(zipFile.Name)
		if strings.Contains(baseName, "..") {
			continue
		}

		// Open the file in the zip
		rc, err := zipFile.Open()
		if err != nil {
			log.Printf("Error opening zip entry %s: %v", zipFile.Name, err)
			continue
		}

		// Read content from zip
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			log.Printf("Error reading zip entry %s: %v", zipFile.Name, err)
			continue
		}

		// Write via storage (handles encryption if enabled)
		destPath := filepath.Join(cfg.DataDirectory, baseName)
		if err := store.WriteFile(destPath, data, 0644); err != nil {
			log.Printf("Error writing file %s: %v", destPath, err)
			continue
		}

		restoredCount++
		log.Printf("Restored file: %s", baseName)
	}

	if restoredCount == 0 {
		http.Error(w, "No CSV files found in backup", http.StatusBadRequest)
		return
	}

	log.Printf("Restore complete: %d files restored", restoredCount)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Restored %d files", restoredCount)
}

func HandleRestoreTestData(w http.ResponseWriter, r *http.Request) {
	// Read the embedded test backup
	content, err := testdata.TestBackupFS.ReadFile("test_backup.zip")
	if err != nil {
		http.Error(w, "Test backup not available", http.StatusInternalServerError)
		return
	}

	// Open zip archive from memory
	zipReader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		http.Error(w, "Invalid embedded ZIP file", http.StatusInternalServerError)
		return
	}

	// Extract all CSV files from the zip
	restoredCount := 0
	for _, zipFile := range zipReader.File {
		// Skip directories
		if zipFile.FileInfo().IsDir() {
			continue
		}

		// Only extract CSV files
		if !strings.HasSuffix(strings.ToLower(zipFile.Name), ".csv") {
			continue
		}

		// Sanitize filename - use only the base name to prevent path traversal
		baseName := filepath.Base(zipFile.Name)
		if strings.Contains(baseName, "..") {
			continue
		}

		// Open the file in the zip
		rc, err := zipFile.Open()
		if err != nil {
			log.Printf("Error opening zip entry %s: %v", zipFile.Name, err)
			continue
		}

		// Read content from zip
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			log.Printf("Error reading zip entry %s: %v", zipFile.Name, err)
			continue
		}

		// Write via storage (handles encryption if enabled)
		destPath := filepath.Join(cfg.DataDirectory, baseName)
		if err := store.WriteFile(destPath, data, 0644); err != nil {
			log.Printf("Error writing file %s: %v", destPath, err)
			continue
		}

		restoredCount++
		log.Printf("Restored file from test data: %s", baseName)
	}

	if restoredCount == 0 {
		http.Error(w, "No CSV files found in test backup", http.StatusBadRequest)
		return
	}

	log.Printf("Test data restore complete: %d files restored", restoredCount)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Restored %d test files", restoredCount)
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
