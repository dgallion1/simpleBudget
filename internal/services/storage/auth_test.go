package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()

	// No config file should return nil, nil
	config, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if config != nil {
		t.Error("expected nil config for missing file")
	}

	// Valid config file
	cfg := EncryptionConfig{
		Method:     AuthMethodPassword,
		SSHKeyPath: "/some/path",
	}
	data, _ := json.Marshal(cfg)
	configPath := filepath.Join(dir, configFile)
	os.WriteFile(configPath, data, 0600)

	config, err = loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if config == nil {
		t.Fatal("expected non-nil config")
	}
	if config.Method != AuthMethodPassword {
		t.Errorf("expected %s, got %s", AuthMethodPassword, config.Method)
	}
	if config.SSHKeyPath != "/some/path" {
		t.Errorf("expected /some/path, got %s", config.SSHKeyPath)
	}

	// Invalid JSON
	os.WriteFile(configPath, []byte("not json"), 0600)
	_, err = loadConfig(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()

	cfg := &EncryptionConfig{
		Method:          AuthMethodAge,
		AgeIdentityPath: "/path/to/key",
	}

	if err := saveConfig(dir, cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// Verify it was saved
	loaded, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if loaded.Method != AuthMethodAge {
		t.Errorf("expected %s, got %s", AuthMethodAge, loaded.Method)
	}
	if loaded.AgeIdentityPath != "/path/to/key" {
		t.Errorf("expected /path/to/key, got %s", loaded.AgeIdentityPath)
	}
}

func TestRemoveConfig(t *testing.T) {
	dir := t.TempDir()

	// Remove when file doesn't exist should not error
	if err := removeConfig(dir); err != nil {
		t.Fatalf("removeConfig failed for missing file: %v", err)
	}

	// Create and then remove
	cfg := &EncryptionConfig{Method: AuthMethodPassword}
	saveConfig(dir, cfg)

	configPath := filepath.Join(dir, configFile)
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal("config file should exist after save")
	}

	if err := removeConfig(dir); err != nil {
		t.Fatalf("removeConfig failed: %v", err)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should be removed")
	}
}

func TestGetAuthMethod(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Not encrypted, should return empty
	if m := s.GetAuthMethod(); m != "" {
		t.Errorf("expected empty, got %s", m)
	}

	// Set config
	s.config = &EncryptionConfig{Method: AuthMethodSSH}
	if m := s.GetAuthMethod(); m != AuthMethodSSH {
		t.Errorf("expected %s, got %s", AuthMethodSSH, m)
	}
}

func TestGetConfig(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// No config
	if c := s.GetConfig(); c != nil {
		t.Error("expected nil config")
	}

	// With config
	s.config = &EncryptionConfig{
		Method:     AuthMethodAge,
		SSHKeyPath: "/test",
	}

	c := s.GetConfig()
	if c == nil {
		t.Fatal("expected non-nil config")
	}

	// Should be a copy
	c.SSHKeyPath = "/modified"
	if s.config.SSHKeyPath != "/test" {
		t.Error("GetConfig should return a copy")
	}
}
