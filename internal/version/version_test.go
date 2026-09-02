package version

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	// Save originals and restore after test
	origVersion := Version
	origBuildTime := BuildTime
	origCommit := Commit
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
		Commit = origCommit
	})

	Version = "v1.2.3"
	BuildTime = "2026-01-01T00:00:00Z"
	Commit = "abc1234"

	info := Get()

	if info.Version != "v1.2.3" {
		t.Errorf("expected Version v1.2.3, got %s", info.Version)
	}
	if info.BuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("expected BuildTime 2026-01-01T00:00:00Z, got %s", info.BuildTime)
	}
	if info.Commit != Commit {
		t.Errorf("expected Info.Commit to equal the package Commit var (%s), got %s", Commit, info.Commit)
	}
	// GoVersion should be populated from debug.ReadBuildInfo in test binary
	if info.GoVersion == "" {
		t.Error("expected GoVersion to be populated")
	}
}

// TestGet_CommitJSONMarshaling asserts the Info.Commit field round-trips
// through JSON as "commit" and tracks the package Commit var (not a
// hard-coded literal), so this holds under any ldflags stamp.
func TestGet_CommitJSONMarshaling(t *testing.T) {
	origCommit := Commit
	t.Cleanup(func() { Commit = origCommit })

	Commit = "sentinel-commit-xyz"

	info := Get()
	if info.Commit != Commit {
		t.Fatalf("expected Info.Commit %q, got %q", Commit, info.Commit)
	}

	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal(info) failed: %v", err)
	}
	if !strings.Contains(string(b), `"commit":"sentinel-commit-xyz"`) {
		t.Errorf("expected marshaled Info JSON to contain the commit field, got %s", string(b))
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

func TestGet_WithVCSSettings(t *testing.T) {
	origVersion := Version
	origBuildTime := BuildTime
	orig := readBuildInfo
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
		readBuildInfo = orig
	})

	Version = "v2.0.0"
	BuildTime = "2026-03-01T00:00:00Z"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.23.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123def456"},
				{Key: "vcs.time", Value: "2026-02-28T12:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	info := Get()

	if info.Version != "v2.0.0" {
		t.Errorf("expected Version v2.0.0, got %s", info.Version)
	}
	if info.GoVersion != "go1.23.0" {
		t.Errorf("expected GoVersion go1.23.0, got %s", info.GoVersion)
	}
	if info.VCSRevision != "abc123def456" {
		t.Errorf("expected VCSRevision abc123def456, got %s", info.VCSRevision)
	}
	if info.VCSTime != "2026-02-28T12:00:00Z" {
		t.Errorf("expected VCSTime 2026-02-28T12:00:00Z, got %s", info.VCSTime)
	}
	if !info.VCSModified {
		t.Error("expected VCSModified to be true")
	}
}

// TestGet_CommitNeverFallsBackToVCSRevision pins the no-fallback contract:
// an unstamped build reports Commit "unknown" even when the buildvcs stamp
// carries a revision, because that stamp records the PARENT checkout's HEAD
// for binaries built under .claude/worktrees/* and must never leak into the
// fingerprint. If someone adds a "helpful" VCSRevision fallback, this fails.
func TestGet_CommitNeverFallsBackToVCSRevision(t *testing.T) {
	origCommit := Commit
	orig := readBuildInfo
	t.Cleanup(func() {
		Commit = origCommit
		readBuildInfo = orig
	})

	Commit = "unknown" // the unstamped default (bare go build / go run / tests)
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.23.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "deadbeefcafe1234"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	}

	info := Get()

	if info.Commit != "unknown" {
		t.Errorf("Commit must stay %q when unstamped, got %q — a vcs.revision fallback has been introduced", "unknown", info.Commit)
	}
	if info.VCSRevision != "deadbeefcafe1234" {
		t.Errorf("VCSRevision should still surface informationally, got %q", info.VCSRevision)
	}
}

func TestGet_VCSModifiedFalse(t *testing.T) {
	orig := readBuildInfo
	t.Cleanup(func() { readBuildInfo = orig })

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			GoVersion: "go1.23.0",
			Settings: []debug.BuildSetting{
				{Key: "vcs.modified", Value: "false"},
			},
		}, true
	}

	info := Get()
	if info.VCSModified {
		t.Error("expected VCSModified to be false")
	}
}

func TestGet_ReadBuildInfoFails(t *testing.T) {
	origVersion := Version
	origBuildTime := BuildTime
	orig := readBuildInfo
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
		readBuildInfo = orig
	})

	Version = "v3.0.0"
	BuildTime = "2026-04-01"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	info := Get()

	if info.Version != "v3.0.0" {
		t.Errorf("expected Version v3.0.0, got %s", info.Version)
	}
	if info.BuildTime != "2026-04-01" {
		t.Errorf("expected BuildTime 2026-04-01, got %s", info.BuildTime)
	}
	if info.GoVersion != "" {
		t.Errorf("expected empty GoVersion when ReadBuildInfo fails, got %s", info.GoVersion)
	}
	if info.VCSRevision != "" {
		t.Errorf("expected empty VCSRevision, got %s", info.VCSRevision)
	}
	if info.VCSTime != "" {
		t.Errorf("expected empty VCSTime, got %s", info.VCSTime)
	}
	if info.VCSModified {
		t.Error("expected VCSModified to be false")
	}
}
