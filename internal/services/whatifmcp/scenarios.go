package whatifmcp

import (
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
}

// Source reads saved what-if scenarios. Read-only by construction: it exposes
// no method that writes to the settings directory.
type Source struct {
	sm *retirement.SettingsManager
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
		name = s.sm.ActiveFilename()
	}
	settings, err := s.sm.LoadScenarioSettings(name)
	if err != nil {
		return nil, "", fmt.Errorf("scenario %q could not be loaded: %w (available: %s)",
			name, err, strings.Join(s.names(), ", "))
	}
	return settings, name, nil
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
