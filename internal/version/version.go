// Package version provides build information and version details.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// These are set via ldflags at build time
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// Info contains version and build information
type Info struct {
	Version    string `json:"version"`
	BuildTime  string `json:"buildTime"`
	GoVersion  string `json:"goVersion"`
	VCSRevision string `json:"vcsRevision,omitempty"`
	VCSTime    string `json:"vcsTime,omitempty"`
	VCSModified bool   `json:"vcsModified"`
}

// Get returns the current version and build information
func Get() Info {
	info := Info{
		Version:   Version,
		BuildTime: BuildTime,
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = buildInfo.GoVersion

		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.VCSRevision = setting.Value
			case "vcs.time":
				info.VCSTime = setting.Value
			case "vcs.modified":
				info.VCSModified = setting.Value == "true"
			}
		}
	}

	return info
}

// String returns a human-readable version string
func (i Info) String() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Version: %s", i.Version))

	if i.BuildTime != "unknown" {
		parts = append(parts, fmt.Sprintf("Built: %s", i.BuildTime))
	}

	parts = append(parts, fmt.Sprintf("Go: %s", i.GoVersion))

	if i.VCSRevision != "" {
		rev := i.VCSRevision
		if len(rev) > 8 {
			rev = rev[:8]
		}
		if i.VCSModified {
			rev += " (modified)"
		}
		parts = append(parts, fmt.Sprintf("Commit: %s", rev))
	}

	if i.VCSTime != "" {
		parts = append(parts, fmt.Sprintf("Committed: %s", i.VCSTime))
	}

	return strings.Join(parts, ", ")
}

// Check logs a warning if the binary appears to have been modified after build
func (i Info) Check() string {
	if i.VCSModified {
		return "WARNING: Binary built from modified source tree"
	}
	if i.VCSRevision == "" && i.Version == "dev" {
		return "WARNING: No version control information available (development build)"
	}
	return ""
}
