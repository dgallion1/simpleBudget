package version

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	// Save originals and restore after test
	origVersion := Version
	origBuildTime := BuildTime
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
	})

	Version = "v1.2.3"
	BuildTime = "2026-01-01T00:00:00Z"

	info := Get()

	if info.Version != "v1.2.3" {
		t.Errorf("expected Version v1.2.3, got %s", info.Version)
	}
	if info.BuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("expected BuildTime 2026-01-01T00:00:00Z, got %s", info.BuildTime)
	}
	// GoVersion should be populated from debug.ReadBuildInfo in test binary
	if info.GoVersion == "" {
		t.Error("expected GoVersion to be populated")
	}
}

func TestString_AllFields(t *testing.T) {
	info := Info{
		Version:     "v1.0.0",
		BuildTime:   "2026-01-01",
		GoVersion:   "go1.22.0",
		VCSRevision: "abcdef1234567890",
		VCSTime:     "2026-01-01T12:00:00Z",
		VCSModified: false,
	}

	s := info.String()

	if !strings.Contains(s, "Version: v1.0.0") {
		t.Errorf("expected version in output, got %s", s)
	}
	if !strings.Contains(s, "Built: 2026-01-01") {
		t.Errorf("expected build time in output, got %s", s)
	}
	if !strings.Contains(s, "Go: go1.22.0") {
		t.Errorf("expected go version in output, got %s", s)
	}
	// Revision should be truncated to 8 chars
	if !strings.Contains(s, "Commit: abcdef12") {
		t.Errorf("expected truncated commit in output, got %s", s)
	}
	if strings.Contains(s, "(modified)") {
		t.Errorf("should not contain modified marker, got %s", s)
	}
	if !strings.Contains(s, "Committed: 2026-01-01T12:00:00Z") {
		t.Errorf("expected commit time in output, got %s", s)
	}
}

func TestString_BuildTimeUnknown(t *testing.T) {
	info := Info{
		Version:   "v1.0.0",
		BuildTime: "unknown",
		GoVersion: "go1.22.0",
	}

	s := info.String()

	if strings.Contains(s, "Built:") {
		t.Errorf("should not contain Built when BuildTime is unknown, got %s", s)
	}
}

func TestString_ShortRevision(t *testing.T) {
	info := Info{
		Version:     "v1.0.0",
		BuildTime:   "unknown",
		GoVersion:   "go1.22.0",
		VCSRevision: "abcd",
	}

	s := info.String()

	// Short revision should not be truncated
	if !strings.Contains(s, "Commit: abcd") {
		t.Errorf("expected short commit hash, got %s", s)
	}
}

func TestString_ExactlyEightCharRevision(t *testing.T) {
	info := Info{
		Version:     "v1.0.0",
		BuildTime:   "unknown",
		GoVersion:   "go1.22.0",
		VCSRevision: "abcdef12",
	}

	s := info.String()

	if !strings.Contains(s, "Commit: abcdef12") {
		t.Errorf("expected 8-char commit hash, got %s", s)
	}
}

func TestString_ModifiedRevision(t *testing.T) {
	info := Info{
		Version:     "v1.0.0",
		BuildTime:   "unknown",
		GoVersion:   "go1.22.0",
		VCSRevision: "abcdef1234567890",
		VCSModified: true,
	}

	s := info.String()

	if !strings.Contains(s, "abcdef12 (modified)") {
		t.Errorf("expected modified marker after truncated hash, got %s", s)
	}
}

func TestString_NoRevisionNoVCSTime(t *testing.T) {
	info := Info{
		Version:   "dev",
		BuildTime: "unknown",
		GoVersion: "go1.22.0",
	}

	s := info.String()

	if strings.Contains(s, "Commit:") {
		t.Errorf("should not contain Commit when no revision, got %s", s)
	}
	if strings.Contains(s, "Committed:") {
		t.Errorf("should not contain Committed when no VCSTime, got %s", s)
	}
}

func TestCheck_Modified(t *testing.T) {
	info := Info{VCSModified: true, VCSRevision: "abc", Version: "v1.0.0"}
	result := info.Check()
	if result != "WARNING: Binary built from modified source tree" {
		t.Errorf("expected modified warning, got %q", result)
	}
}

func TestCheck_DevNoVCS(t *testing.T) {
	info := Info{Version: "dev", VCSRevision: ""}
	result := info.Check()
	if result != "WARNING: No version control information available (development build)" {
		t.Errorf("expected dev warning, got %q", result)
	}
}

func TestCheck_Clean(t *testing.T) {
	info := Info{Version: "v1.0.0", VCSRevision: "abc123"}
	result := info.Check()
	if result != "" {
		t.Errorf("expected empty string for clean build, got %q", result)
	}
}

func TestCheck_DevWithRevision(t *testing.T) {
	// dev version but has VCS revision -- should not warn
	info := Info{Version: "dev", VCSRevision: "abc123"}
	result := info.Check()
	if result != "" {
		t.Errorf("expected empty string when dev has revision, got %q", result)
	}
}

func TestCheck_NonDevNoRevision(t *testing.T) {
	// Non-dev version with no revision -- should not warn (only warns when both dev AND no revision)
	info := Info{Version: "v1.0.0", VCSRevision: ""}
	result := info.Check()
	if result != "" {
		t.Errorf("expected empty string for non-dev without revision, got %q", result)
	}
}
