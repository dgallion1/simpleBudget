package whatifmcp

import (
	"context"
	"fmt"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
)

// ScenarioInfo is one row of the list_scenarios result.
type ScenarioInfo struct {
	Filename        string  `json:"filename"`
	Name            string  `json:"name"`
	Active          bool    `json:"active"`
	PortfolioValue  float64 `json:"portfolio_value"`
	ProjectionYears int     `json:"projection_years"`
	Unreadable      bool    `json:"unreadable,omitempty"`
	LoadError       string  `json:"load_error,omitempty"`
}

// Source reads saved what-if scenarios. Read-only by construction: it exposes
// no method that writes to the settings directory.
type Source struct {
	sm *retirement.SettingsManager

	// live is optional and unset by NewSource; NewServer attaches it (same
	// package, so the unexported field is reachable directly) when a Client
	// is available. It lets Load("") resolve the active scenario the running
	// web server actually reports instead of guessing whatif.json -- the
	// active filename is in-process state in that server, so a separate
	// process has no other way to know it.
	live *Client
}

func NewSource(settingsDir string, store *storage.Storage) *Source {
	return &Source{sm: retirement.NewSettingsManager(settingsDir, store)}
}

// List returns every saved scenario with a one-line summary.
func (s *Source) List() ([]ScenarioInfo, error) {
	scenarios, err := s.sm.ListScenarios()
	if err != nil {
		return nil, fmt.Errorf("list scenarios: %w", err)
	}
	out := make([]ScenarioInfo, 0, len(scenarios))
	for _, sc := range scenarios {
		info := ScenarioInfo{Filename: sc.Filename, Name: sc.Name, Active: sc.Active}
		if settings, err := s.sm.LoadScenarioSettings(sc.Filename); err == nil && settings != nil {
			info.PortfolioValue = round0(settings.PortfolioValue)
			info.ProjectionYears = settings.ProjectionYears
		} else {
			info.Unreadable = true
			if err != nil {
				info.LoadError = err.Error()
			} else {
				info.LoadError = "scenario settings unavailable"
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// Load returns the named scenario's settings and the filename actually used.
// An empty name resolves to the active scenario. An unknown name produces an
// error that lists the valid names, so the caller can retry without a second
// round trip.
func (s *Source) Load(name string) (*models.WhatIfSettings, string, error) {
	if name == "" {
		name = s.resolveActiveFilename()
	}
	settings, err := s.sm.LoadScenarioSettings(name)
	if err != nil {
		return nil, "", fmt.Errorf("scenario %q could not be loaded: %w (available: %s)",
			name, err, strings.Join(s.names(), ", "))
	}
	return settings, name, nil
}

// resolveActiveFilename answers "which scenario is active" preferring the
// running web server's own answer over a local guess: if a scenario was
// switched through the browser, this process's on-disk view of "active" is
// stale until it re-reads, but the server's in-memory state is not. State
// already verifies app identity and settings-dir match, so a mismatched or
// unreachable server falls through to the local guess rather than erroring
// -- reads must keep working with the server down.
func (s *Source) resolveActiveFilename() string {
	if s.live != nil {
		if st, err := s.live.State(context.Background()); err == nil {
			return st.Active
		}
	}
	return s.sm.ActiveFilename()
}

func (s *Source) names() []string {
	scenarios, err := s.sm.ListScenarios()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(scenarios))
	for _, sc := range scenarios {
		names = append(names, sc.Filename)
	}
	return names
}
