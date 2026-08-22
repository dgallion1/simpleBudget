package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigGetwdError(t *testing.T) {
	// Create a temp dir, chdir into it, then remove it so os.Getwd fails
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	// Remove the directory out from under us
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()

	// When Getwd fails, wd falls back to "."
	if cfg.DataDirectory != filepath.Join(".", "data") {
		t.Errorf("DataDirectory = %q, want %q", cfg.DataDirectory, filepath.Join(".", "data"))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	wd, _ := os.Getwd()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.Debug != false {
		t.Error("Debug should default to false")
	}
	if cfg.DataDirectory != filepath.Join(wd, "data") {
		t.Errorf("DataDirectory = %q, want %q", cfg.DataDirectory, filepath.Join(wd, "data"))
	}
	if cfg.UploadsDirectory != filepath.Join(wd, "data", "uploads") {
		t.Errorf("UploadsDirectory = %q, want %q", cfg.UploadsDirectory, filepath.Join(wd, "data", "uploads"))
	}
	if cfg.SettingsDirectory != filepath.Join(wd, "data", "settings") {
		t.Errorf("SettingsDirectory = %q, want %q", cfg.SettingsDirectory, filepath.Join(wd, "data", "settings"))
	}
	if cfg.TemplatesDirectory != filepath.Join(wd, "web", "templates") {
		t.Errorf("TemplatesDirectory = %q, want %q", cfg.TemplatesDirectory, filepath.Join(wd, "web", "templates"))
	}
	if cfg.StaticDirectory != filepath.Join(wd, "web", "static") {
		t.Errorf("StaticDirectory = %q, want %q", cfg.StaticDirectory, filepath.Join(wd, "web", "static"))
	}
	if cfg.UserSettingsFile != filepath.Join(wd, "data", "settings", "user_settings.json") {
		t.Errorf("UserSettingsFile = %q, want %q", cfg.UserSettingsFile, filepath.Join(wd, "data", "settings", "user_settings.json"))
	}
}

func TestLoadDefaults(t *testing.T) {
	// Clear all env vars that Load reads
	envVars := []string{
		"BUDGET_LISTEN_ADDR",
		"BUDGET_DEBUG",
		"BUDGET_DATA_DIR",
		"BUDGET_TEMPLATES_DIR",
		"BUDGET_STATIC_DIR",
	}
	for _, v := range envVars {
		t.Setenv(v, "")
	}

	cfg := Load()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.Debug != false {
		t.Error("Debug should be false when BUDGET_DEBUG is not set")
	}
}

func TestLoadListenAddr(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", ":9090")
	t.Setenv("BUDGET_DEBUG", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")

	cfg := Load()
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
}

func TestLoadDebugTrue(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")

	t.Setenv("BUDGET_DEBUG", "true")
	cfg := Load()
	if !cfg.Debug {
		t.Error("Debug should be true when BUDGET_DEBUG=true")
	}
}

func TestLoadDebugOne(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")

	t.Setenv("BUDGET_DEBUG", "1")
	cfg := Load()
	if !cfg.Debug {
		t.Error("Debug should be true when BUDGET_DEBUG=1")
	}
}

func TestLoadDebugOtherValue(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")

	t.Setenv("BUDGET_DEBUG", "yes")
	cfg := Load()
	if cfg.Debug {
		t.Error("Debug should be false when BUDGET_DEBUG=yes (not true or 1)")
	}
}

func TestLoadDataDir(t *testing.T) {
	tmp := t.TempDir()

	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DEBUG", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")
	t.Setenv("BUDGET_DATA_DIR", tmp)

	cfg := Load()

	if cfg.DataDirectory != tmp {
		t.Errorf("DataDirectory = %q, want %q", cfg.DataDirectory, tmp)
	}
	if cfg.UploadsDirectory != filepath.Join(tmp, "uploads") {
		t.Errorf("UploadsDirectory = %q, want %q", cfg.UploadsDirectory, filepath.Join(tmp, "uploads"))
	}
	if cfg.SettingsDirectory != filepath.Join(tmp, "settings") {
		t.Errorf("SettingsDirectory = %q, want %q", cfg.SettingsDirectory, filepath.Join(tmp, "settings"))
	}
	if cfg.UserSettingsFile != filepath.Join(tmp, "settings", "user_settings.json") {
		t.Errorf("UserSettingsFile = %q, want %q", cfg.UserSettingsFile, filepath.Join(tmp, "settings", "user_settings.json"))
	}
}

func TestLoadTemplatesDir(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DEBUG", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "/custom/templates")

	cfg := Load()
	if cfg.TemplatesDirectory != "/custom/templates" {
		t.Errorf("TemplatesDirectory = %q, want %q", cfg.TemplatesDirectory, "/custom/templates")
	}
}

func TestLoadStaticDir(t *testing.T) {
	t.Setenv("BUDGET_LISTEN_ADDR", "")
	t.Setenv("BUDGET_DEBUG", "")
	t.Setenv("BUDGET_DATA_DIR", "")
	t.Setenv("BUDGET_TEMPLATES_DIR", "")
	t.Setenv("BUDGET_STATIC_DIR", "/custom/static")

	cfg := Load()
	if cfg.StaticDirectory != "/custom/static" {
		t.Errorf("StaticDirectory = %q, want %q", cfg.StaticDirectory, "/custom/static")
	}
}

func TestEnsureDirectoriesCreates(t *testing.T) {
	tmp := t.TempDir()

	cfg := &Config{
		DataDirectory:    filepath.Join(tmp, "data"),
		UploadsDirectory: filepath.Join(tmp, "data", "uploads"),
		SettingsDirectory: filepath.Join(tmp, "data", "settings"),
	}

	cfg.ensureDirectories()

	for _, dir := range []string{cfg.DataDirectory, cfg.UploadsDirectory, cfg.SettingsDirectory} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("directory %q was not created: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestEnsureDirectoriesInvalidPath(t *testing.T) {
	// Use a path under /dev/null which can't contain subdirectories.
	// This exercises the error log branch. It should not panic.
	cfg := &Config{
		DataDirectory:    "/dev/null/impossible",
		UploadsDirectory: "/dev/null/impossible/uploads",
		SettingsDirectory: "/dev/null/impossible/settings",
	}

	// Should not panic; just logs warnings
	cfg.ensureDirectories()
}

func TestLoadUserSettingsFileNotExist(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		UserSettingsFile: filepath.Join(tmp, "nonexistent.json"),
	}

	settings, err := cfg.LoadUserSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(settings) != 0 {
		t.Errorf("expected empty map, got %v", settings)
	}
}

func TestLoadUserSettingsValid(t *testing.T) {
	tmp := t.TempDir()
	settingsFile := filepath.Join(tmp, "settings.json")
	data := map[string]any{"theme": "dark", "count": float64(42)}
	b, _ := json.Marshal(data)
	os.WriteFile(settingsFile, b, 0644)

	cfg := &Config{UserSettingsFile: settingsFile}
	settings, err := cfg.LoadUserSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", settings["theme"])
	}
	if settings["count"] != float64(42) {
		t.Errorf("count = %v, want 42", settings["count"])
	}
}

func TestLoadUserSettingsInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	settingsFile := filepath.Join(tmp, "bad.json")
	os.WriteFile(settingsFile, []byte("{not json}"), 0644)

	cfg := &Config{UserSettingsFile: settingsFile}
	_, err := cfg.LoadUserSettings()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadUserSettingsPermissionError(t *testing.T) {
	tmp := t.TempDir()
	settingsFile := filepath.Join(tmp, "settings.json")
	os.WriteFile(settingsFile, []byte("{}"), 0644)
	// Remove read permission
	os.Chmod(settingsFile, 0000)
	t.Cleanup(func() { os.Chmod(settingsFile, 0644) })

	cfg := &Config{UserSettingsFile: settingsFile}
	_, err := cfg.LoadUserSettings()
	if err == nil {
		t.Fatal("expected permission error")
	}
	// This should NOT be os.IsNotExist, so it should return the error, not an empty map
	if os.IsNotExist(err) {
		t.Fatal("expected permission error, not IsNotExist")
	}
}

func TestSaveUserSettings(t *testing.T) {
	tmp := t.TempDir()
	settingsFile := filepath.Join(tmp, "settings.json")

	cfg := &Config{UserSettingsFile: settingsFile}
	settings := map[string]any{"key": "value"}

	err := cfg.SaveUserSettings(settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify by reading back
	data, err := os.ReadFile(settingsFile)
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var loaded map[string]any
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse saved settings: %v", err)
	}
	if loaded["key"] != "value" {
		t.Errorf("key = %v, want value", loaded["key"])
	}
}

func TestSaveUserSettingsMarshalError(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{UserSettingsFile: filepath.Join(tmp, "settings.json")}
	// math.Inf cannot be marshaled to JSON
	settings := map[string]any{"bad": math.Inf(1)}
	err := cfg.SaveUserSettings(settings)
	if err == nil {
		t.Fatal("expected marshal error for Inf value")
	}
}

func TestSaveUserSettingsWriteError(t *testing.T) {
	// Point to a directory that doesn't exist and can't be created
	cfg := &Config{UserSettingsFile: "/dev/null/impossible/settings.json"}
	err := cfg.SaveUserSettings(map[string]any{"a": "b"})
	if err == nil {
		t.Fatal("expected error writing to invalid path")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	settingsFile := filepath.Join(tmp, "settings.json")

	cfg := &Config{UserSettingsFile: settingsFile}
	original := map[string]any{
		"name":    "test",
		"enabled": true,
		"rate":    3.14,
	}

	if err := cfg.SaveUserSettings(original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := cfg.LoadUserSettings()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded["name"] != "test" {
		t.Errorf("name = %v, want test", loaded["name"])
	}
	if loaded["enabled"] != true {
		t.Errorf("enabled = %v, want true", loaded["enabled"])
	}
	if loaded["rate"] != 3.14 {
		t.Errorf("rate = %v, want 3.14", loaded["rate"])
	}
}

func TestBackupDir_DefaultUsesXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test-home")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	cfg := DefaultConfig()
	want := filepath.Join("/tmp/xdg-test-home", "budget2", "backups")
	if cfg.BackupDir != want {
		t.Fatalf("BackupDir=%q want %q", cfg.BackupDir, want)
	}
}

func TestBackupDir_DefaultFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this system")
	}
	cfg := DefaultConfig()
	want := filepath.Join(home, ".local", "share", "budget2", "backups")
	if cfg.BackupDir != want {
		t.Fatalf("BackupDir=%q want %q", cfg.BackupDir, want)
	}
}

func TestBackupDir_EnvOverride(t *testing.T) {
	t.Setenv("BUDGET2_BACKUP_DIR", "/tmp/custom-backups")
	cfg := Load()
	if cfg.BackupDir != "/tmp/custom-backups" {
		t.Fatalf("BackupDir=%q want /tmp/custom-backups", cfg.BackupDir)
	}
}

func TestBackupDir_LoadHonorsDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	cfg := Load()
	if !strings.Contains(cfg.BackupDir, "budget2/backups") {
		t.Fatalf("BackupDir=%q does not contain expected default suffix", cfg.BackupDir)
	}
}

func TestImportDir_DefaultUsesXDG(t *testing.T) {
	t.Setenv("XDG_DOWNLOAD_DIR", "/tmp/xdg-downloads-test")
	t.Setenv("BUDGET2_IMPORT_DIR", "")
	cfg := DefaultConfig()
	if cfg.ImportDirectory != "/tmp/xdg-downloads-test" {
		t.Fatalf("ImportDirectory=%q want /tmp/xdg-downloads-test", cfg.ImportDirectory)
	}
}

func TestImportDir_DefaultFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_DOWNLOAD_DIR", "")
	t.Setenv("BUDGET2_IMPORT_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this system")
	}
	cfg := DefaultConfig()
	want := filepath.Join(home, "Downloads")
	if cfg.ImportDirectory != want {
		t.Fatalf("ImportDirectory=%q want %q", cfg.ImportDirectory, want)
	}
}

func TestImportDir_EnvOverride(t *testing.T) {
	t.Setenv("BUDGET2_IMPORT_DIR", "/tmp/custom-imports")
	cfg := Load()
	if cfg.ImportDirectory != "/tmp/custom-imports" {
		t.Fatalf("ImportDirectory=%q want /tmp/custom-imports", cfg.ImportDirectory)
	}
}

func TestImportDir_LoadHonorsDefault(t *testing.T) {
	t.Setenv("XDG_DOWNLOAD_DIR", "")
	t.Setenv("BUDGET2_IMPORT_DIR", "")
	cfg := Load()
	if !strings.HasSuffix(cfg.ImportDirectory, "Downloads") {
		t.Fatalf("ImportDirectory=%q does not end with Downloads", cfg.ImportDirectory)
	}
}
