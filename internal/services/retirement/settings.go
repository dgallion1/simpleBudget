package retirement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"budget2/internal/services/storage"

	"github.com/google/uuid"
)

// Scenario represents a named what-if scenario
type Scenario struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	Active   bool   `json:"active"`
}

// ScenarioChainValidationError reports a user-correctable invalid scenario chain.
type ScenarioChainValidationError struct {
	Err error
}

func (e *ScenarioChainValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "invalid scenario chain"
	}
	return e.Err.Error()
}

func (e *ScenarioChainValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ScenarioValidationError reports invalid scenario input supplied by the user.
type ScenarioValidationError struct {
	Err error
}

func (e *ScenarioValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "invalid scenario"
	}
	return e.Err.Error()
}

func (e *ScenarioValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ScenarioNotFoundError reports a requested scenario file that does not exist.
type ScenarioNotFoundError struct {
	Err error
}

func (e *ScenarioNotFoundError) Error() string {
	if e == nil || e.Err == nil {
		return "scenario not found"
	}
	return e.Err.Error()
}

func (e *ScenarioNotFoundError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ScenarioConflictError reports user-correctable conflicts with the current scenario state.
type ScenarioConflictError struct {
	Err error
}

func (e *ScenarioConflictError) Error() string {
	if e == nil || e.Err == nil {
		return "scenario conflict"
	}
	return e.Err.Error()
}

func (e *ScenarioConflictError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// defaultWhatIfFilename is the settings file backing the default
// "Current Plan" scenario.
const defaultWhatIfFilename = "whatif.json"

// SettingsManager handles persistence of what-if settings
type SettingsManager struct {
	settingsDir string
	filename    string
	store       *storage.Storage
	mu          sync.RWMutex
	cache       *models.WhatIfSettings
}

// NewSettingsManager creates a new settings manager
func NewSettingsManager(settingsDir string, store *storage.Storage) *SettingsManager {
	return &SettingsManager{
		settingsDir: settingsDir,
		filename:    defaultWhatIfFilename,
		store:       store,
	}
}

// filepath returns the full path to the settings file
func (sm *SettingsManager) filepath() string {
	return filepath.Join(sm.settingsDir, sm.filename)
}

type legacyAgeFields struct {
	CurrentAge int `json:"current_age"`
	SpouseAge  int `json:"spouse_age"`
}

func normalizeStartDate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return models.CurrentLocalMonth(), true
	}
	if _, err := time.Parse("2006-01", raw); err != nil {
		return models.CurrentLocalMonth(), true
	}
	return raw, false
}

func hasTaxableFields(rawFields map[string]json.RawMessage) bool {
	return rawFields["taxable_dividend_yield"] != nil ||
		rawFields["taxable_qualified_dividend_percent"] != nil ||
		rawFields["taxable_cap_gains_distribution_rate"] != nil
}

func initializeLoadedSettings(settings *models.WhatIfSettings, rawFields map[string]json.RawMessage) {
	if settings.IncomeSources == nil {
		settings.IncomeSources = []models.IncomeSource{}
	}
	if settings.ExpenseSources == nil {
		settings.ExpenseSources = []models.ExpenseSource{}
	}
	if settings.RemovedIncomeSources == nil {
		settings.RemovedIncomeSources = []models.IncomeSource{}
	}
	if settings.RemovedExpenseSources == nil {
		settings.RemovedExpenseSources = []models.ExpenseSource{}
	}
	if settings.HealthcarePersons == nil {
		settings.HealthcarePersons = []models.HealthcarePerson{}
	}
	if settings.Persons == nil {
		settings.Persons = []models.Person{}
	}

	if settings.TaxConfig == nil {
		settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
	}

	if settings.SpendingPhaseConfig == nil {
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: false,
			Phases:  models.DefaultSpendingPhases(),
		}
	} else if len(settings.SpendingPhaseConfig.Phases) == 0 {
		settings.SpendingPhaseConfig.Phases = models.DefaultSpendingPhases()
	}

	for i := range settings.SpendingPhaseConfig.Phases {
		if settings.SpendingPhaseConfig.Phases[i].Multiplier == 0 {
			settings.SpendingPhaseConfig.Phases[i].Multiplier = 1.0
		}
	}

	if !hasTaxableFields(rawFields) && settings.TaxableQualifiedDividendPercent == 0 {
		settings.TaxableQualifiedDividendPercent = 100
	}

	settings.ProjectionTiming = models.NormalizeProjectionTiming(settings.ProjectionTiming)

	// F-035 migration: scenarios saved before RMDTiming existed have an empty
	// string. Preserve the original start-of-year behaviour for those so that
	// existing saved projections don't change. New scenarios constructed in
	// memory (empty string from a fresh form) get mid-year via
	// NormalizeRMDTiming at calculation time rather than here.
	if settings.RMDTiming == "" {
		settings.RMDTiming = models.RMDTimingStartOfYear
	}

	if rawFields["property_tax_inflation"] == nil && settings.PropertyTaxInflation == 0 {
		settings.PropertyTaxInflation = 4.0
	}
}

// defaultTaxConfigForPersons returns a TaxConfig with filing status
// inferred from the household shape. Single-person scenarios default
// to single filing; scenarios with a spouse Person default to
// married-jointly. This avoids the completeness banner producing a
// false-positive "MFJ but no spouse" error on legacy single-person
// scenarios that never had a TaxConfig saved.
func defaultTaxConfigForPersons(persons []models.Person) *models.TaxConfig {
	cfg := models.DefaultTaxConfig()
	for _, p := range persons {
		if p.Role == models.PersonRoleSpouse {
			cfg.FilingStatus = models.FilingMarriedJoint
			break
		}
	}
	return cfg
}

func parseLegacyAges(rawFields map[string]json.RawMessage) legacyAgeFields {
	var legacy legacyAgeFields
	_ = json.Unmarshal(rawFields["current_age"], &legacy.CurrentAge)
	_ = json.Unmarshal(rawFields["spouse_age"], &legacy.SpouseAge)
	return legacy
}

func normalizePersonName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func inferHealthcarePersonLink(settings *models.WhatIfSettings, name string) string {
	normalized := normalizePersonName(name)
	if normalized == "" {
		return ""
	}

	if primary := settings.GetPrimaryPerson(); primary != nil {
		switch normalized {
		case "you", "user", "primary":
			return primary.ID
		}
	}
	if spouse := settings.GetSpousePerson(); spouse != nil && normalized == "spouse" {
		return spouse.ID
	}

	var matchID string
	for _, person := range settings.Persons {
		if normalizePersonName(person.Name) != normalized {
			continue
		}
		if matchID != "" {
			return ""
		}
		matchID = person.ID
	}
	return matchID
}

func normalizeLoadedWhatIfSettings(settings *models.WhatIfSettings, rawFields map[string]json.RawMessage) (bool, error) {
	initializeLoadedSettings(settings, rawFields)

	legacy := parseLegacyAges(rawFields)
	changed := false

	var startChanged bool
	settings.StartDate, startChanged = normalizeStartDate(settings.StartDate)
	changed = changed || startChanged

	if len(settings.Persons) == 0 {
		primaryAge := legacy.CurrentAge
		if primaryAge <= 0 {
			primaryAge = 65
		}
		settings.Persons = append(settings.Persons, models.Person{
			ID:         uuid.New().String(),
			Name:       "You",
			BirthMonth: models.BirthMonthForAge(settings.StartDate, primaryAge),
			Role:       models.PersonRolePrimary,
		})

		if legacy.SpouseAge > 0 {
			settings.Persons = append(settings.Persons, models.Person{
				ID:         uuid.New().String(),
				Name:       "Spouse",
				BirthMonth: models.BirthMonthForAge(settings.StartDate, legacy.SpouseAge),
				Role:       models.PersonRoleSpouse,
			})
		}
		changed = true
	}

	// First pass: derive ages so the healthcare migration below can read
	// settings.CurrentAge to pick ACA vs Medicare coverage.
	prepare.NormalizePhaseAgeReference(settings)
	prepare.ComputeAges(settings)

	if len(settings.HealthcarePersons) == 0 && settings.MonthlyHealthcare > 0 {
		coverage := models.CoverageMedicare
		if settings.CurrentAge < 65 {
			coverage = models.CoverageACA
		}
		person := models.HealthcarePerson{
			ID:                    "migrated-user",
			Name:                  "User",
			CurrentAge:            settings.CurrentAge,
			CurrentCoverage:       coverage,
			CurrentMonthlyCost:    settings.MonthlyHealthcare,
			PreMedicareInflation:  settings.HealthcareInflation,
			MedicareMonthlyCost:   settings.MonthlyHealthcare,
			PostMedicareInflation: settings.HealthcareInflation,
			MedicareEligibleAge:   65,
		}
		if primary := settings.GetPrimaryPerson(); primary != nil {
			person.PersonID = primary.ID
			person.Name = primary.Name
		}
		settings.HealthcarePersons = []models.HealthcarePerson{person}
		changed = true
	}

	for i := range settings.HealthcarePersons {
		if settings.HealthcarePersons[i].PersonID != "" {
			continue
		}
		personID := inferHealthcarePersonLink(settings, settings.HealthcarePersons[i].Name)
		if personID == "" {
			continue
		}
		settings.HealthcarePersons[i].PersonID = personID
		changed = true
	}

	// Second pass: healthcare link inference above may have changed PersonIDs,
	// so re-derive ages and re-normalize phase reference for the final state.
	beforePhase := settings.PhaseAgeReference
	prepare.NormalizePhaseAgeReference(settings)
	if settings.PhaseAgeReference != beforePhase {
		changed = true
	}
	prepare.ComputeAges(settings)

	if err := prepare.ValidatePersons(settings); err != nil {
		return changed, err
	}

	return changed, nil
}

func (sm *SettingsManager) decodeSettings(data []byte) (*models.WhatIfSettings, bool, error) {
	var settings models.WhatIfSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return models.DefaultWhatIfSettings(), false, err
	}

	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(data, &rawFields)

	changed, err := normalizeLoadedWhatIfSettings(&settings, rawFields)
	if err != nil {
		return nil, changed, err
	}
	return &settings, changed, nil
}

// Load reads settings from disk, returning defaults if file doesn't exist.
// Context-less convenience wrapper around LoadContext for non-request callers.
func (sm *SettingsManager) Load() (*models.WhatIfSettings, error) {
	return sm.LoadContext(context.Background())
}

// LoadContext is Load with caller-supplied cancellation. It fails fast on
// entry (so an abandoned what-if request stops before any disk read) and
// threads ctx into the underlying decrypting read.
func (sm *SettingsManager) LoadContext(ctx context.Context) (*models.WhatIfSettings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sm.mu.RLock()
	// Return cache if available
	if sm.cache != nil {
		defer sm.mu.RUnlock()
		return sm.cache, nil
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check cache after acquiring write lock
	if sm.cache != nil {
		return sm.cache, nil
	}

	settings, err := sm.loadInternalContext(ctx)
	if err != nil {
		return nil, err
	}

	sm.cache = settings
	return settings, nil
}

// InvalidateCache drops the in-memory settings cache so the next Load
// re-reads from disk. Call it after anything rewrites the settings file
// behind the manager's back (e.g. a backup restore).
func (sm *SettingsManager) InvalidateCache() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cache = nil
}

// ReconcileActiveScenario reverts the active scenario to the default
// whatif.json when the active scenario's file no longer exists on disk.
// The active filename is pure in-process state (nothing on disk records
// it), so when something rewrites the settings directory behind the
// manager's back — e.g. a full-replace backup restore that prunes the
// active scenario file — the manager would otherwise keep pointing at a
// missing file, silently serve default settings, and resurrect the pruned
// file with defaults on the next save. Call it alongside InvalidateCache
// after any such external rewrite.
func (sm *SettingsManager) ReconcileActiveScenario() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.filename == defaultWhatIfFilename {
		return
	}
	if _, err := sm.store.Stat(sm.filepath()); os.IsNotExist(err) {
		sm.filename = defaultWhatIfFilename
		sm.cache = nil
	}
}

// loadInternal reads settings without acquiring lock (caller must hold lock).
// Context-less wrapper used by the in-package CRUD methods.
func (sm *SettingsManager) loadInternal() (*models.WhatIfSettings, error) {
	return sm.loadInternalContext(context.Background())
}

// loadInternalContext is loadInternal with caller-supplied cancellation,
// threaded into the decrypting file read.
func (sm *SettingsManager) loadInternalContext(ctx context.Context) (*models.WhatIfSettings, error) {
	// Ensure settings directory exists
	if err := sm.store.MkdirAll(sm.settingsDir, 0755); err != nil {
		return nil, err
	}

	path := sm.filepath()

	// Check if file exists
	if _, err := sm.store.Stat(path); os.IsNotExist(err) {
		// Return defaults (caller should save if needed)
		return models.DefaultWhatIfSettings(), nil
	}

	// Read file (storage handles decryption)
	data, err := sm.store.ReadFileContext(ctx, path)
	if err != nil {
		return models.DefaultWhatIfSettings(), err
	}

	settings, changed, err := sm.decodeSettings(data)
	if err != nil {
		return models.DefaultWhatIfSettings(), err
	}
	if changed {
		if err := sm.saveInternal(settings); err != nil {
			return nil, err
		}
	}
	return settings, nil
}

// LoadScenarioSettings loads a scenario's settings without switching the active scenario.
// This is a read-only operation used for pre-resolving chained scenarios.
func (sm *SettingsManager) LoadScenarioSettings(filename string) (*models.WhatIfSettings, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	path, err := sm.scenarioPath(filename)
	if err != nil {
		return nil, err
	}

	if _, err := sm.store.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("scenario file not found: %s", filename)
	}

	data, err := sm.store.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenario %s: %w", filename, err)
	}

	// decodeSettings returns a changed flag, but LoadScenarioSettings is
	// intentionally read-only — chained-scenario loading during analysis must
	// not rewrite unrelated scenario files as a side effect.
	settingsPtr, _, err := sm.decodeSettings(data)
	if err != nil {
		return nil, fmt.Errorf("parsing scenario %s: %w", filename, err)
	}
	return settingsPtr, nil
}

// ValidateScenarioChain validates that a scenario chain is well-formed.
// It acquires a read lock and delegates to validateChainInternal.
func (sm *SettingsManager) ValidateScenarioChain(chain []models.ScenarioChainLink, settings *models.WhatIfSettings, currentFilename string) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.validateChainInternal(chain, settings, currentFilename)
}

// validateChainInternal validates a scenario chain without acquiring any lock.
// Caller must hold at least a read lock (or a write lock).
func (sm *SettingsManager) validateChainInternal(chain []models.ScenarioChainLink, settings *models.WhatIfSettings, currentFilename string) error {
	seen := make(map[string]bool)
	prevAge := -1
	maxAge := settings.CurrentAge + settings.ProjectionYears

	for i, link := range chain {
		// Rule 1: Transition ages strictly ascending
		if link.TransitionAge <= prevAge {
			return fmt.Errorf("chain link %d: transition ages must be strictly ascending (got %d after %d)", i, link.TransitionAge, prevAge)
		}
		prevAge = link.TransitionAge

		// Rule 4: Ages >= CurrentAge and < CurrentAge + ProjectionYears
		if link.TransitionAge < settings.CurrentAge {
			return fmt.Errorf("chain link %d: transition age %d is below current age %d", i, link.TransitionAge, settings.CurrentAge)
		}
		if link.TransitionAge >= maxAge {
			return fmt.Errorf("chain link %d: transition age %d is beyond projection end age %d", i, link.TransitionAge, maxAge-1)
		}

		// Rule 3: No self-reference
		if link.ScenarioFilename == currentFilename {
			return fmt.Errorf("chain link %d: scenario cannot reference itself (%s)", i, link.ScenarioFilename)
		}

		// Rule 5: No duplicate filenames
		if seen[link.ScenarioFilename] {
			return fmt.Errorf("chain link %d: duplicate scenario filename %s", i, link.ScenarioFilename)
		}
		seen[link.ScenarioFilename] = true

		// Rule 2: Referenced scenario files must exist
		path, err := sm.scenarioPath(link.ScenarioFilename)
		if err != nil {
			return fmt.Errorf("chain link %d: invalid scenario filename %s: %w", i, link.ScenarioFilename, err)
		}
		if _, err := sm.store.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("chain link %d: scenario file not found: %s", i, link.ScenarioFilename)
		}
	}
	return nil
}

// Save writes settings to disk
func (sm *SettingsManager) Save(settings *models.WhatIfSettings) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := sm.saveInternal(settings); err != nil {
		sm.cache = nil
		return err
	}
	return nil
}

// saveInternal writes settings without acquiring lock (caller must hold lock)
func (sm *SettingsManager) saveInternal(settings *models.WhatIfSettings) error {
	prepare.NormalizePhaseAgeReference(settings)
	if err := prepare.ValidatePersons(settings); err != nil {
		return err
	}
	prepare.ComputeAges(settings)

	// Validate scenario chain if one is present; invalid chains must be surfaced
	// back to the caller instead of being silently discarded on save.
	if len(settings.ScenarioChain) > 0 {
		if err := sm.validateChainInternal(settings.ScenarioChain, settings, sm.filename); err != nil {
			return &ScenarioChainValidationError{Err: err}
		}
	}

	settings.ProjectionTiming = models.NormalizeProjectionTiming(settings.ProjectionTiming)
	// Do NOT normalize RMDTiming here: empty string is a valid sentinel meaning
	// "new scenario, use mid-year at calculation time". The migration in
	// initializeLoadedSettings handles legacy saved files.

	// Ensure settings directory exists
	if err := sm.store.MkdirAll(sm.settingsDir, 0755); err != nil {
		return err
	}

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	// Write file (storage handles encryption)
	if err := sm.store.WriteFile(sm.filepath(), data, 0644); err != nil {
		return err
	}

	// Update cache
	sm.cache = settings
	return nil
}

// AddIncomeSource adds a new income source and saves atomically
func (sm *SettingsManager) AddIncomeSource(source models.IncomeSource) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.IncomeSources = append(settings.IncomeSources, source)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveIncomeSource moves an income source to the removed list by ID and saves atomically
func (sm *SettingsManager) RemoveIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.IncomeSource, 0, len(settings.IncomeSources))
	for _, source := range settings.IncomeSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Move to removed list
			settings.RemovedIncomeSources = append(settings.RemovedIncomeSources, source)
		}
	}
	settings.IncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RestoreIncomeSource moves an income source back from the removed list atomically.
// Returns a ScenarioConflictError if the active list already contains the ID
// (e.g. from a hand-edited file with the ID present in both lists).
func (sm *SettingsManager) RestoreIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for _, source := range settings.IncomeSources {
		if source.ID == id {
			return nil, &ScenarioConflictError{Err: fmt.Errorf("income source %s already exists in the active list", id)}
		}
	}

	filtered := make([]models.IncomeSource, 0, len(settings.RemovedIncomeSources))
	restored := false
	for _, source := range settings.RemovedIncomeSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Restore to active list
			settings.IncomeSources = append(settings.IncomeSources, source)
			restored = true
		}
	}
	if !restored {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed income source %s not found", id)}
	}
	settings.RemovedIncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// PurgeRemovedIncomeSource permanently removes an income source from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedIncomeSources. Does not touch the active IncomeSources list.
func (sm *SettingsManager) PurgeRemovedIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.IncomeSource, 0, len(settings.RemovedIncomeSources))
	purged := false
	for _, source := range settings.RemovedIncomeSources {
		if source.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, source)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed income source %s not found", id)}
	}
	settings.RemovedIncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateIncomeSource updates an existing income source by ID atomically
func (sm *SettingsManager) UpdateIncomeSource(id string, startYear int, endYear *int, colaRate float64) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.IncomeSources {
		if settings.IncomeSources[i].ID == id {
			settings.IncomeSources[i].StartMonth = startYear * 12
			settings.IncomeSources[i].COLARate = colaRate
			if endYear != nil {
				endMonth := *endYear * 12
				settings.IncomeSources[i].EndMonth = &endMonth
			} else {
				settings.IncomeSources[i].EndMonth = nil
			}
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// AddExpenseSource adds a new expense source and saves atomically
func (sm *SettingsManager) AddExpenseSource(source models.ExpenseSource) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.ExpenseSources = append(settings.ExpenseSources, source)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateExpenseSource updates an existing expense source by ID atomically
func (sm *SettingsManager) UpdateExpenseSource(id string, startYear int, endYear *int, inflation, discretionary bool) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.ExpenseSources {
		if settings.ExpenseSources[i].ID == id {
			settings.ExpenseSources[i].StartYear = startYear
			if endYear != nil {
				settings.ExpenseSources[i].EndYear = *endYear
			} else {
				settings.ExpenseSources[i].EndYear = 0 // In ExpenseSource, 0 is perpetual
			}
			settings.ExpenseSources[i].Inflation = inflation
			settings.ExpenseSources[i].Discretionary = discretionary
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveExpenseSource moves an expense source to the removed list by ID and saves atomically
func (sm *SettingsManager) RemoveExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.ExpenseSources))
	for _, source := range settings.ExpenseSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Move to removed list
			settings.RemovedExpenseSources = append(settings.RemovedExpenseSources, source)
		}
	}
	settings.ExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RestoreExpenseSource moves an expense source back from the removed list atomically.
// Returns a ScenarioConflictError if the active list already contains the ID.
func (sm *SettingsManager) RestoreExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for _, source := range settings.ExpenseSources {
		if source.ID == id {
			return nil, &ScenarioConflictError{Err: fmt.Errorf("expense source %s already exists in the active list", id)}
		}
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.RemovedExpenseSources))
	restored := false
	for _, source := range settings.RemovedExpenseSources {
		if source.ID != id {
			filtered = append(filtered, source)
		} else {
			// Restore to active list
			settings.ExpenseSources = append(settings.ExpenseSources, source)
			restored = true
		}
	}
	if !restored {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed expense source %s not found", id)}
	}
	settings.RemovedExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// PurgeRemovedExpenseSource permanently removes an expense source from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedExpenseSources. Does not touch the active ExpenseSources list.
func (sm *SettingsManager) PurgeRemovedExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.RemovedExpenseSources))
	purged := false
	for _, source := range settings.RemovedExpenseSources {
		if source.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, source)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed expense source %s not found", id)}
	}
	settings.RemovedExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateSettings updates all settings fields from form data and saves atomically
func (sm *SettingsManager) UpdateSettings(updates map[string]interface{}) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	sm.applySettingsUpdates(settings, updates)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func (sm *SettingsManager) UpdateSettingsWithPersons(updates map[string]interface{}, startDate string, persons []models.Person) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	sm.applySettingsUpdates(settings, updates)
	settings.StartDate = startDate
	settings.Persons = persons

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func (sm *SettingsManager) applySettingsUpdates(settings *models.WhatIfSettings, updates map[string]interface{}) {
	if v, ok := updates["portfolio_value"].(float64); ok {
		settings.PortfolioValue = v
	}
	if v, ok := updates["monthly_living_expenses"].(float64); ok {
		settings.MonthlyLivingExpenses = v
	}
	if v, ok := updates["monthly_healthcare"].(float64); ok {
		settings.MonthlyHealthcare = v
	}
	if v, ok := updates["monthly_property_tax"].(float64); ok {
		settings.MonthlyPropertyTax = v
	}
	if v, ok := updates["healthcare_start_years"].(int); ok {
		settings.HealthcareStartYears = v
	}
	if v, ok := updates["current_age"].(int); ok {
		settings.CurrentAge = v
	}
	if v, ok := updates["spouse_age"].(int); ok {
		settings.SpouseAge = v
	}
	if v, ok := updates["phase_age_reference"].(string); ok {
		settings.PhaseAgeReference = v
	}
	if v, ok := updates["tax_deferred_percent"].(float64); ok {
		settings.TaxDeferredPercent = v
	}
	if v, ok := updates["roth_percent"].(float64); ok {
		settings.RothPercent = v
	}
	if v, ok := updates["roth_first_funded_year"].(int); ok {
		settings.RothFirstFundedYear = v
	}
	if v, ok := updates["stock_percent"].(float64); ok {
		settings.StockPercent = v
	}
	if v, ok := updates["cash_percent"].(float64); ok {
		settings.CashPercent = v
	}
	// Per-account asset allocation
	if v, ok := updates["tax_deferred_stock_percent"].(float64); ok {
		settings.TaxDeferredStockPercent = v
	}
	if v, ok := updates["tax_deferred_cash_percent"].(float64); ok {
		settings.TaxDeferredCashPercent = v
	}
	if v, ok := updates["roth_stock_percent"].(float64); ok {
		settings.RothStockPercent = v
	}
	if v, ok := updates["roth_cash_percent"].(float64); ok {
		settings.RothCashPercent = v
	}
	if v, ok := updates["taxable_stock_percent"].(float64); ok {
		settings.TaxableStockPercent = v
	}
	if v, ok := updates["taxable_cash_percent"].(float64); ok {
		settings.TaxableCashPercent = v
	}
	if v, ok := updates["inflation_rate"].(float64); ok {
		settings.InflationRate = v
	}
	if v, ok := updates["healthcare_inflation"].(float64); ok {
		settings.HealthcareInflation = v
	}
	if v, ok := updates["property_tax_inflation"].(float64); ok {
		settings.PropertyTaxInflation = v
	}
	if v, ok := updates["spending_decline_rate"].(float64); ok {
		settings.SpendingDeclineRate = v
	}
	if v, ok := updates["investment_return"].(float64); ok {
		settings.InvestmentReturn = v
	}
	if v, ok := updates["discount_rate"].(float64); ok {
		settings.DiscountRate = v
	}
	if v, ok := updates["taxable_dividend_yield"].(float64); ok {
		settings.TaxableDividendYield = v
	}
	if v, ok := updates["taxable_qualified_dividend_percent"].(float64); ok {
		settings.TaxableQualifiedDividendPercent = v
	}
	if v, ok := updates["taxable_cap_gains_distribution_rate"].(float64); ok {
		settings.TaxableCapitalGainsDistributionRate = v
	}
	if v, ok := updates["projection_years"].(int); ok {
		settings.ProjectionYears = v
	}
	if v, ok := updates["projection_timing"].(models.ProjectionTiming); ok {
		settings.ProjectionTiming = models.NormalizeProjectionTiming(v)
	}
	if v, ok := updates["rmd_timing"].(models.RMDTiming); ok {
		settings.RMDTiming = v
	}
	if v, ok := updates["spouse_sole_beneficiary"].(bool); ok {
		settings.SpouseSoleBeneficiary = &v
	}
	if v, ok := updates["tax_deferred_delay_years"].(int); ok {
		settings.TaxDeferredDelayYears = v
	}
	if v, ok := updates["steady_state_override_year"].(float64); ok {
		settings.SteadyStateOverrideYear = v
	}
	if v, ok := updates["state_income_tax_rate"].(*float64); ok {
		if settings.TaxConfig == nil {
			settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
		}
		settings.TaxConfig.StateIncomeTaxRate = v
	}
	if v, ok := updates["filing_status"].(string); ok {
		if settings.TaxConfig == nil {
			settings.TaxConfig = defaultTaxConfigForPersons(settings.Persons)
		}
		settings.TaxConfig.FilingStatus = models.FilingStatus(v)
	}

	if v, ok := updates["current_age"].(int); ok {
		person := ensurePrimaryPerson(settings)
		person.BirthMonth = models.BirthMonthForAge(settings.StartDate, v)
	}
	if v, ok := updates["spouse_age"].(int); ok {
		if v > 0 {
			person := ensureSpousePerson(settings)
			person.BirthMonth = models.BirthMonthForAge(settings.StartDate, v)
		} else {
			removeSpousePersons(settings)
		}
	}
}

func ensurePrimaryPerson(settings *models.WhatIfSettings) *models.Person {
	if person := settings.GetPrimaryPerson(); person != nil {
		if strings.TrimSpace(person.Name) == "" {
			person.Name = "You"
		}
		return person
	}

	person := models.Person{
		ID:         uuid.New().String(),
		Name:       "You",
		BirthMonth: models.BirthMonthForAge(settings.StartDate, 65),
		Role:       models.PersonRolePrimary,
	}
	settings.Persons = append([]models.Person{person}, settings.Persons...)
	return &settings.Persons[0]
}

func ensureSpousePerson(settings *models.WhatIfSettings) *models.Person {
	if person := settings.GetSpousePerson(); person != nil {
		if strings.TrimSpace(person.Name) == "" {
			person.Name = "Spouse"
		}
		return person
	}

	person := models.Person{
		ID:         uuid.New().String(),
		Name:       "Spouse",
		BirthMonth: models.BirthMonthForAge(settings.StartDate, 65),
		Role:       models.PersonRoleSpouse,
	}
	settings.Persons = append(settings.Persons, person)
	return &settings.Persons[len(settings.Persons)-1]
}

func removeSpousePersons(settings *models.WhatIfSettings) {
	filtered := make([]models.Person, 0, len(settings.Persons))
	for _, person := range settings.Persons {
		if person.Role == models.PersonRoleSpouse {
			continue
		}
		filtered = append(filtered, person)
	}
	settings.Persons = filtered
}

// UpdateSpendingPhases updates spending phase configuration atomically
func (sm *SettingsManager) UpdateSpendingPhases(enabled bool, phases []models.SpendingPhase) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	// Initialize config if needed
	if settings.SpendingPhaseConfig == nil {
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Phases: models.DefaultSpendingPhases(),
		}
	}

	settings.SpendingPhaseConfig.Enabled = enabled

	// Update phases if provided
	if len(phases) > 0 {
		settings.SpendingPhaseConfig.Phases = phases
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// AddHealthcarePerson adds a new healthcare person and saves atomically
func (sm *SettingsManager) AddHealthcarePerson(person models.HealthcarePerson) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.HealthcarePersons = append(settings.HealthcarePersons, person)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateHealthcarePerson updates an existing healthcare person by ID atomically
func (sm *SettingsManager) UpdateHealthcarePerson(id string, updates map[string]interface{}) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for i := range settings.HealthcarePersons {
		if settings.HealthcarePersons[i].ID == id {
			applyHealthcareUpdates(&settings.HealthcarePersons[i], updates)
			break
		}
	}

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func applyHealthcareUpdates(person *models.HealthcarePerson, updates map[string]interface{}) {
	if v, ok := updates["person_id"].(string); ok {
		person.PersonID = v
	}
	if v, ok := updates["name"].(string); ok {
		person.Name = v
	}
	if v, ok := updates["current_age"].(int); ok {
		person.CurrentAge = v
	}
	if v, ok := updates["current_coverage"].(string); ok {
		person.CurrentCoverage = models.CoverageType(v)
	}
	if v, ok := updates["current_monthly_cost"].(float64); ok {
		person.CurrentMonthlyCost = v
	}
	if v, ok := updates["pre_medicare_inflation"].(float64); ok {
		person.PreMedicareInflation = v
	}
	if v, ok := updates["medicare_monthly_cost"].(float64); ok {
		person.MedicareMonthlyCost = v
	}
	if v, ok := updates["post_medicare_inflation"].(float64); ok {
		person.PostMedicareInflation = v
	}
	if v, ok := updates["employer_coverage_years"].(int); ok {
		person.EmployerCoverageYears = v
	}
	if v, ok := updates["aca_cost_after_employer"].(float64); ok {
		person.ACACostAfterEmployer = v
	}
}

// RemoveHealthcarePerson removes a healthcare person by ID atomically
func (sm *SettingsManager) RemoveHealthcarePerson(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.HealthcarePerson, 0, len(settings.HealthcarePersons))
	for _, person := range settings.HealthcarePersons {
		if person.ID != id {
			filtered = append(filtered, person)
		}
	}
	settings.HealthcarePersons = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// AddBigTicketItem adds a new big ticket item and saves atomically
func (sm *SettingsManager) AddBigTicketItem(item models.BigTicketItem) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	settings.BigTicketItems = append(settings.BigTicketItems, item)

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RemoveBigTicketItem moves a big ticket item to the removed list by ID and saves atomically
func (sm *SettingsManager) RemoveBigTicketItem(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.BigTicketItem, 0, len(settings.BigTicketItems))
	for _, item := range settings.BigTicketItems {
		if item.ID != id {
			filtered = append(filtered, item)
		} else {
			// Move to removed list
			settings.RemovedBigTicketItems = append(settings.RemovedBigTicketItems, item)
		}
	}
	settings.BigTicketItems = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// RestoreBigTicketItem moves a big ticket item back from the removed list atomically.
// Returns a ScenarioConflictError if the active list already contains the ID.
func (sm *SettingsManager) RestoreBigTicketItem(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	for _, item := range settings.BigTicketItems {
		if item.ID == id {
			return nil, &ScenarioConflictError{Err: fmt.Errorf("big ticket item %s already exists in the active list", id)}
		}
	}

	filtered := make([]models.BigTicketItem, 0, len(settings.RemovedBigTicketItems))
	restored := false
	for _, item := range settings.RemovedBigTicketItems {
		if item.ID != id {
			filtered = append(filtered, item)
		} else {
			// Restore to active list
			settings.BigTicketItems = append(settings.BigTicketItems, item)
			restored = true
		}
	}
	if !restored {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed big ticket item %s not found", id)}
	}
	settings.RemovedBigTicketItems = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// PurgeRemovedBigTicketItem permanently removes a big ticket item from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedBigTicketItems. Does not touch the active BigTicketItems list.
func (sm *SettingsManager) PurgeRemovedBigTicketItem(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.BigTicketItem, 0, len(settings.RemovedBigTicketItems))
	purged := false
	for _, item := range settings.RemovedBigTicketItems {
		if item.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed big ticket item %s not found", id)}
	}
	settings.RemovedBigTicketItems = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// slugify converts a scenario name to a URL-safe filename slug
func slugify(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '-' || r == '_' {
			return '-'
		}
		return -1
	}, slug)
	// Collapse multiple hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "scenario"
	}
	return slug
}

// readScenarioName reads the scenario_name field from a whatif JSON file
func (sm *SettingsManager) readScenarioName(filename string) string {
	path, err := sm.scenarioPath(filename)
	if err != nil {
		return filename
	}
	data, err := sm.store.ReadFile(path)
	if err != nil {
		return filename
	}
	var partial struct {
		ScenarioName string `json:"scenario_name"`
	}
	if err := json.Unmarshal(data, &partial); err != nil {
		return filename
	}
	name := strings.TrimSpace(partial.ScenarioName)
	if name == "" {
		return filename
	}
	return name
}

func (sm *SettingsManager) scenarioPath(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", &ScenarioValidationError{Err: errors.New("scenario filename is required")}
	}
	if filepath.Base(filename) != filename || strings.Contains(filename, "..") {
		return "", &ScenarioValidationError{Err: fmt.Errorf("invalid scenario filename: %s", filename)}
	}
	if strings.ContainsAny(filename, `/\`) {
		return "", &ScenarioValidationError{Err: fmt.Errorf("invalid scenario filename: %s", filename)}
	}
	if !strings.HasPrefix(filename, "whatif") || !strings.HasSuffix(filename, ".json") {
		return "", &ScenarioValidationError{Err: fmt.Errorf("invalid scenario filename: %s", filename)}
	}
	return filepath.Join(sm.settingsDir, filename), nil
}

// ListScenarios returns all available what-if scenarios
func (sm *SettingsManager) ListScenarios() ([]Scenario, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	pattern := filepath.Join(sm.settingsDir, "whatif*.json")
	matches, err := sm.store.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("listing scenarios: %w", err)
	}

	var scenarios []Scenario
	hasDefault := false
	for _, match := range matches {
		fname := filepath.Base(match)
		var name string
		if fname == "whatif.json" {
			hasDefault = true
			name = "Current Plan"
		} else {
			name = sm.readScenarioName(fname)
		}
		scenarios = append(scenarios, Scenario{
			Name:     name,
			Filename: fname,
			Active:   fname == sm.filename,
		})
	}

	if !hasDefault {
		scenarios = append(scenarios, Scenario{
			Name:     "Current Plan",
			Filename: "whatif.json",
			Active:   sm.filename == "whatif.json",
		})
	}

	// Sort with "Current Plan" (whatif.json) always first, rest alphabetical by name
	sort.Slice(scenarios, func(i, j int) bool {
		if scenarios[i].Filename == "whatif.json" {
			return true
		}
		if scenarios[j].Filename == "whatif.json" {
			return false
		}
		return scenarios[i].Name < scenarios[j].Name
	})

	return scenarios, nil
}

// ActiveScenario returns the display name of the current scenario
func (sm *SettingsManager) ActiveScenario() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.filename == "whatif.json" {
		return "Current Plan"
	}
	return sm.readScenarioName(sm.filename)
}

// ActiveFilename returns the filename of the current active scenario
func (sm *SettingsManager) ActiveFilename() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.filename
}

// SwitchScenario changes the active scenario to the specified file
func (sm *SettingsManager) SwitchScenario(filename string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	filename = strings.TrimSpace(filename)

	// Validate the file exists
	path, err := sm.scenarioPath(filename)
	if err != nil {
		return err
	}
	if _, err := sm.store.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return &ScenarioNotFoundError{Err: fmt.Errorf("scenario file not found: %s", filename)}
		}
		return fmt.Errorf("checking scenario file: %w", err)
	}

	sm.filename = filename
	sm.cache = nil
	return nil
}

// CreateScenario copies the current settings to a new scenario file and switches to it
func (sm *SettingsManager) CreateScenario(name string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &ScenarioValidationError{Err: errors.New("scenario name is required")}
	}

	// Load current settings
	settings, err := sm.loadInternal()
	if err != nil {
		return nil, fmt.Errorf("loading current settings: %w", err)
	}

	// Generate filename
	slug := slugify(name)
	filename := fmt.Sprintf("whatif_%s.json", slug)

	// Ensure unique filename
	path := filepath.Join(sm.settingsDir, filename)
	counter := 1
	for {
		if _, err := sm.store.Stat(path); os.IsNotExist(err) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("checking scenario file: %w", err)
		}
		counter++
		filename = fmt.Sprintf("whatif_%s-%d.json", slug, counter)
		path = filepath.Join(sm.settingsDir, filename)
	}

	// Set scenario name on the settings
	settings.ScenarioName = name

	// Switch to the new file and save
	sm.filename = filename
	sm.cache = nil
	if err := sm.saveInternal(settings); err != nil {
		return nil, fmt.Errorf("saving new scenario: %w", err)
	}

	log.Printf("Created scenario %q as %s", name, filename)
	return settings, nil
}

// scenariosReferencingFile returns the filenames of all whatif*.json scenarios
// that include the given filename in their ScenarioChain.
// Caller must hold the write lock (no locking performed here).
func (sm *SettingsManager) scenariosReferencingFile(filename string) []string {
	pattern := filepath.Join(sm.settingsDir, "whatif*.json")
	matches, err := sm.store.Glob(pattern)
	if err != nil {
		return nil
	}

	var referencing []string
	for _, match := range matches {
		fname := filepath.Base(match)
		if fname == filename {
			continue
		}
		data, err := sm.store.ReadFile(match)
		if err != nil {
			continue
		}
		var partial struct {
			ScenarioChain []models.ScenarioChainLink `json:"scenario_chain"`
		}
		if err := json.Unmarshal(data, &partial); err != nil {
			continue
		}
		for _, link := range partial.ScenarioChain {
			if link.ScenarioFilename == filename {
				referencing = append(referencing, fname)
				break
			}
		}
	}
	return referencing
}

// DeleteScenario removes a scenario file
func (sm *SettingsManager) DeleteScenario(filename string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	filename = strings.TrimSpace(filename)
	if filename == "whatif.json" {
		return &ScenarioConflictError{Err: errors.New("cannot delete the default scenario")}
	}

	// Referential integrity: reject deletion if other scenarios reference this file
	if refs := sm.scenariosReferencingFile(filename); len(refs) > 0 {
		return &ScenarioConflictError{Err: fmt.Errorf("cannot delete scenario %s: referenced by %s", filename, strings.Join(refs, ", "))}
	}

	path, err := sm.scenarioPath(filename)
	if err != nil {
		return err
	}
	if err := sm.store.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return &ScenarioNotFoundError{Err: fmt.Errorf("scenario file not found: %s", filename)}
		}
		return fmt.Errorf("deleting scenario: %w", err)
	}

	// If we just deleted the active scenario, switch back to default
	if sm.filename == filename {
		sm.filename = "whatif.json"
		sm.cache = nil
	}

	log.Printf("Deleted scenario %s", filename)
	return nil
}

// RenameScenario updates the display name of a scenario
func (sm *SettingsManager) RenameScenario(filename, newName string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	filename = strings.TrimSpace(filename)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return &ScenarioValidationError{Err: errors.New("scenario name is required")}
	}

	if filename == "whatif.json" {
		return &ScenarioConflictError{Err: errors.New("cannot rename the default scenario")}
	}

	path, err := sm.scenarioPath(filename)
	if err != nil {
		return err
	}
	data, err := sm.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ScenarioNotFoundError{Err: fmt.Errorf("scenario file not found: %s", filename)}
		}
		return fmt.Errorf("reading scenario: %w", err)
	}

	var settings models.WhatIfSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing scenario: %w", err)
	}

	settings.ScenarioName = newName

	updated, err := json.MarshalIndent(&settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling scenario: %w", err)
	}

	if err := sm.store.WriteFile(path, updated, 0644); err != nil {
		return fmt.Errorf("writing scenario: %w", err)
	}

	// Invalidate cache if this is the active scenario
	if sm.filename == filename {
		sm.cache = nil
	}

	log.Printf("Renamed scenario %s to %q", filename, newName)
	return nil
}
