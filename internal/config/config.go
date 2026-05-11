// Package config loads and persists the application's runtime configuration:
// listen address, data/settings/uploads/backup directory paths, and feature
// flags. Defaults follow the XDG base-directory spec for cross-user
// installs and fall back to repo-relative paths for development.
package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// Config holds application configuration
type Config struct {
	// Server settings
	ListenAddr string `json:"listen_addr"`
	Debug      bool   `json:"debug"`

	// Directories
	DataDirectory      string `json:"data_directory"`
	UploadsDirectory   string `json:"uploads_directory"`
	SettingsDirectory  string `json:"settings_directory"`
	TemplatesDirectory string `json:"templates_directory"`
	StaticDirectory    string `json:"static_directory"`
	BackupDir          string `json:"backup_dir"`

	// File paths
	UserSettingsFile string `json:"user_settings_file"`
}

// defaultBackupDir returns the default location for automatic backup zips.
// Follows XDG_DATA_HOME, falling back to $HOME/.local/share, falling back
// to <DataDirectory>/../backups if no home is available.
func defaultBackupDir(dataDir string) string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "budget2", "backups")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", "budget2", "backups")
	}
	return filepath.Join(filepath.Dir(dataDir), "budget2-backups")
}

// DefaultConfig returns configuration with sensible defaults
func DefaultConfig() *Config {
	// Get working directory
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}

	return &Config{
		ListenAddr:         ":8080",
		Debug:              false,
		DataDirectory:      filepath.Join(wd, "data"),
		UploadsDirectory:   filepath.Join(wd, "data", "uploads"),
		SettingsDirectory:  filepath.Join(wd, "data", "settings"),
		TemplatesDirectory: filepath.Join(wd, "web", "templates"),
		StaticDirectory:    filepath.Join(wd, "web", "static"),
		UserSettingsFile:   filepath.Join(wd, "data", "settings", "user_settings.json"),
		BackupDir:          defaultBackupDir(filepath.Join(wd, "data")),
	}
}

// Load loads configuration from environment and/or config file
func Load() *Config {
	cfg := DefaultConfig()

	// Override with environment variables
	if addr := os.Getenv("BUDGET_LISTEN_ADDR"); addr != "" {
		cfg.ListenAddr = addr
	}
	if debug := os.Getenv("BUDGET_DEBUG"); debug == "true" || debug == "1" {
		cfg.Debug = true
	}
	if dataDir := os.Getenv("BUDGET_DATA_DIR"); dataDir != "" {
		cfg.DataDirectory = dataDir
		cfg.UploadsDirectory = filepath.Join(dataDir, "uploads")
		cfg.SettingsDirectory = filepath.Join(dataDir, "settings")
		cfg.UserSettingsFile = filepath.Join(dataDir, "settings", "user_settings.json")
	}
	if templatesDir := os.Getenv("BUDGET_TEMPLATES_DIR"); templatesDir != "" {
		cfg.TemplatesDirectory = templatesDir
	}
	if staticDir := os.Getenv("BUDGET_STATIC_DIR"); staticDir != "" {
		cfg.StaticDirectory = staticDir
	}
	if backupDir := os.Getenv("BUDGET2_BACKUP_DIR"); backupDir != "" {
		cfg.BackupDir = backupDir
	}

	// Ensure directories exist
	cfg.ensureDirectories()

	return cfg
}

// ensureDirectories creates required directories if they don't exist
func (c *Config) ensureDirectories() {
	dirs := []string{
		c.DataDirectory,
		c.UploadsDirectory,
		c.SettingsDirectory,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: could not create directory %s: %v", dir, err)
		}
	}
}

// LoadUserSettings loads user settings from JSON file
func (c *Config) LoadUserSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(c.UserSettingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// SaveUserSettings saves user settings to JSON file
func (c *Config) SaveUserSettings(settings map[string]interface{}) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.UserSettingsFile, data, 0644)
}
