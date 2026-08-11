package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDataDir_Precedence(t *testing.T) {
	noEnv := func(string) string { return "" }
	withEnv := func(v string) func(string) string {
		return func(k string) string {
			if k == "BUDGET_DATA_DIR" {
				return v
			}
			return ""
		}
	}

	if got := resolveDataDir("/explicit/flag", withEnv("/from/env")); got != "/explicit/flag" {
		t.Errorf("flag must win: got %q", got)
	}
	if got, want := resolveDataDir("", withEnv("/from/env")), filepath.Join("/from/env", "settings"); got != want {
		t.Errorf("env: got %q, want %q", got, want)
	}
	if got, want := resolveDataDir("", noEnv), filepath.Join("data", "settings"); got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
}

func TestResolveDataDir_PrefersFlagOverDefault(t *testing.T) {
	noEnv := func(string) string { return "" }
	if got := resolveDataDir("/tmp/custom", noEnv); got != "/tmp/custom" {
		t.Errorf("resolveDataDir(\"/tmp/custom\", noEnv) = %q, want /tmp/custom", got)
	}
	if got := resolveDataDir("", noEnv); !strings.Contains(got, "data") {
		t.Errorf("default data dir = %q, want it to contain \"data\"", got)
	}
}

func TestCheckSettingsDir_MissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := checkSettingsDir(missing); err == nil {
		t.Error("checkSettingsDir(missing) = nil, want an error")
	}
}

func TestCheckSettingsDir_ReadableDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := checkSettingsDir(dir); err != nil {
		t.Errorf("checkSettingsDir(%q) = %v, want nil", dir, err)
	}
}

// TestCheckSettingsDir_UnreadableDirectory is the regression case: os.Stat
// alone does not catch this because stat(2) only needs search permission on
// the parent, not read permission on the target itself, so a mode-000
// directory used to pass the old check and only fail later, per tool call,
// inside storage.New — exactly the failure mode this startup check exists
// to prevent.
func TestCheckSettingsDir_UnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are bypassed, this case cannot be exercised")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod %q to 0o000: %v", dir, err)
	}
	t.Cleanup(func() {
		// Restore permissions so t.TempDir()'s cleanup can remove the directory.
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore chmod on %q: %v", dir, err)
		}
	})

	// Some filesystems (or CI sandboxes) don't honor the chmod. Verify it
	// actually took effect before asserting on it, and skip rather than
	// fail spuriously if it didn't.
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("os.Chmod(0o000) did not make the directory unreadable on this filesystem; skipping")
	}

	if err := checkSettingsDir(dir); err == nil {
		t.Error("checkSettingsDir(unreadable dir) = nil, want an error")
	}
}
