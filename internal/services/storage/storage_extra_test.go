package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if s.BaseDir() != dir {
		t.Errorf("expected baseDir %s, got %s", dir, s.BaseDir())
	}

	if s.IsEncrypted() {
		t.Error("new storage should not be encrypted")
	}
}

func TestNewWithEncryptionMarker(t *testing.T) {
	dir := t.TempDir()

	// Create marker file but no config (backward compatibility)
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if !s.IsEncrypted() {
		t.Error("should be encrypted with marker file")
	}

	// Should default to password method
	if s.config.Method != AuthMethodPassword {
		t.Errorf("expected password method, got %s", s.config.Method)
	}
}

func TestNewWithEncryptionConfig(t *testing.T) {
	dir := t.TempDir()

	// Create marker and config
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	saveConfig(dir, &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: "/test"})

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if !s.IsEncrypted() {
		t.Error("should be encrypted")
	}
	if s.config.Method != AuthMethodAge {
		t.Errorf("expected age method, got %s", s.config.Method)
	}
}

func TestNewWithInvalidConfig(t *testing.T) {
	dir := t.TempDir()

	// Create marker and invalid config
	os.WriteFile(filepath.Join(dir, markerFile), []byte("encrypted"), 0644)
	os.WriteFile(filepath.Join(dir, configFile), []byte("not json"), 0600)

	_, err := New(dir)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestIsUnlocked(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Not encrypted = unlocked
	if !s.IsUnlocked() {
		t.Error("non-encrypted storage should be unlocked")
	}

	// Encrypted but no provider = locked
	s.encrypted = true
	if s.IsUnlocked() {
		t.Error("encrypted without provider should be locked")
	}

	// Encrypted with unlocked provider = unlocked
	p := NewPasswordProvider()
	p.Unlock("testpassword")
	s.provider = p
	if !s.IsUnlocked() {
		t.Error("encrypted with unlocked provider should be unlocked")
	}
}

func TestBaseDir(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	if s.BaseDir() != dir {
		t.Errorf("expected %s, got %s", dir, s.BaseDir())
	}
}

func TestStat(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("data"), 0644)

	info, err := s.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size() != 4 {
		t.Errorf("expected size 4, got %d", info.Size())
	}

	// Nonexistent
	_, err = s.Stat(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGlob(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	os.WriteFile(filepath.Join(dir, "a.csv"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.csv"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "c.json"), []byte("c"), 0644)

	matches, err := s.Glob(filepath.Join(dir, "*.csv"))
	if err != nil {
		t.Fatalf("Glob failed: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}
}

func TestMkdirAll(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	newDir := filepath.Join(dir, "a", "b", "c")
	if err := s.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("should be a directory")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "removeme.txt")
	os.WriteFile(testFile, []byte("data"), 0644)

	// Put something in cache
	s.cache[testFile] = &cacheEntry{data: []byte("data"), modTime: 0}

	if err := s.Remove(testFile); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("file should be removed")
	}

	// Cache should be cleared
	if _, ok := s.cache[testFile]; ok {
		t.Error("cache entry should be removed")
	}
}

func TestOpenFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "open.txt")
	content := []byte("hello world")
	os.WriteFile(testFile, content, 0644)

	rc, err := s.OpenFile(testFile)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer rc.Close()

	buf := make([]byte, 100)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != string(content) {
		t.Errorf("content mismatch: got %q", buf[:n])
	}
}

func TestOpenFileNonexistent(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	_, err := s.OpenFile(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadFileCache(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "cached.txt")
	content := []byte("cached content")
	os.WriteFile(testFile, content, 0644)

	// First read should populate cache
	data, err := s.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch")
	}

	// Second read should hit cache
	data2, err := s.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data2) != string(content) {
		t.Errorf("cache content mismatch")
	}

	// Modifying returned data should not affect cache
	data2[0] = 'X'
	data3, _ := s.ReadFile(testFile)
	if data3[0] == 'X' {
		t.Error("cache should not be affected by modifying returned data")
	}
}

func TestReadFileNonexistent(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	_, err := s.ReadFile(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadFileEncryptedButLocked(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Enable encryption
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	// Write an encrypted file
	testFile := filepath.Join(dir, "secret.csv")
	s.WriteFile(testFile, []byte("secret"), 0644)

	// Lock
	s.Lock()

	// Should fail
	_, err := s.ReadFile(testFile)
	if err == nil {
		t.Error("expected error when reading encrypted file while locked")
	}
}

func TestWriteFileSkipsEncryptionForMarkerFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	// Write marker file should not be encrypted
	markerPath := filepath.Join(dir, markerFile)
	s.WriteFile(markerPath, []byte("test"), 0644)

	raw, _ := os.ReadFile(markerPath)
	if isAgeEncrypted(raw) {
		t.Error("marker file should not be encrypted")
	}
}

func TestWriteFileCacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	testFile := filepath.Join(dir, "invalidate.txt")
	os.WriteFile(testFile, []byte("old"), 0644)

	// Populate cache
	s.ReadFile(testFile)

	// Write new content
	s.WriteFile(testFile, []byte("new"), 0644)

	// Read should get new content
	data, _ := s.ReadFile(testFile)
	if string(data) != "new" {
		t.Errorf("expected 'new', got %q", data)
	}
}

func TestLock(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	// Populate cache
	testFile := filepath.Join(dir, "data.csv")
	s.WriteFile(testFile, []byte("data"), 0644)
	s.ReadFile(testFile)

	// Lock should clear provider and cache
	s.Lock()

	if s.provider != nil {
		t.Error("provider should be nil after lock")
	}

	s.cacheMu.RLock()
	cacheLen := len(s.cache)
	s.cacheMu.RUnlock()

	if cacheLen != 0 {
		t.Error("cache should be cleared after lock")
	}
}

func TestUnlockNotEncrypted(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Should be a no-op
	if err := s.Unlock("password"); err != nil {
		t.Errorf("Unlock on non-encrypted should succeed: %v", err)
	}
}

func TestCreateProviderPassword(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// nil config defaults to password
	s.config = nil
	p, err := s.createProvider()
	if err != nil {
		t.Fatalf("createProvider failed: %v", err)
	}
	if p.Method() != AuthMethodPassword {
		t.Errorf("expected password, got %s", p.Method())
	}

	// Explicit password config
	s.config = &EncryptionConfig{Method: AuthMethodPassword}
	p, err = s.createProvider()
	if err != nil {
		t.Fatalf("createProvider failed: %v", err)
	}
	if p.Method() != AuthMethodPassword {
		t.Errorf("expected password, got %s", p.Method())
	}
}

func TestCreateProviderAge(t *testing.T) {
	dir := t.TempDir()
	idPath, _ := createTestAgeIdentityFile(t, dir)

	s, _ := New(dir)
	s.config = &EncryptionConfig{Method: AuthMethodAge, AgeIdentityPath: idPath}

	p, err := s.createProvider()
	if err != nil {
		t.Fatalf("createProvider failed: %v", err)
	}
	if p.Method() != AuthMethodAge {
		t.Errorf("expected age, got %s", p.Method())
	}
}

func TestCreateProviderSSH(t *testing.T) {
	dir := t.TempDir()
	keyPath := createTestSSHKeyPair(t, dir)

	s, _ := New(dir)
	s.config = &EncryptionConfig{Method: AuthMethodSSH, SSHKeyPath: keyPath}

	p, err := s.createProvider()
	if err != nil {
		t.Fatalf("createProvider failed: %v", err)
	}
	if p.Method() != AuthMethodSSH {
		t.Errorf("expected ssh, got %s", p.Method())
	}
}

func TestCreateProviderUnknown(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	s.config = &EncryptionConfig{Method: "unknown"}

	_, err := s.createProvider()
	if err == nil {
		t.Error("expected error for unknown method")
	}
}

func TestShouldSkipEncryption(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	tests := []struct {
		path string
		skip bool
	}{
		{filepath.Join(dir, markerFile), true},
		{filepath.Join(dir, verifyFile), true},
		{filepath.Join(dir, "cache", "plotly.min.js"), true},
		{filepath.Join(dir, "data.csv"), false},
		{filepath.Join(dir, "data.json"), false},
	}

	for _, tt := range tests {
		if got := s.shouldSkipEncryption(tt.path); got != tt.skip {
			t.Errorf("shouldSkipEncryption(%s) = %v, want %v", tt.path, got, tt.skip)
		}
	}
}

func TestIsAgeEncrypted(t *testing.T) {
	tests := []struct {
		data     []byte
		expected bool
	}{
		{[]byte("age-encryption.org/v1\n..."), true},
		{[]byte("not encrypted"), false},
		{[]byte("age"), false}, // too short
		{[]byte(""), false},
		{nil, false},
	}

	for _, tt := range tests {
		if got := isAgeEncrypted(tt.data); got != tt.expected {
			t.Errorf("isAgeEncrypted(%q) = %v, want %v", tt.data, got, tt.expected)
		}
	}
}
