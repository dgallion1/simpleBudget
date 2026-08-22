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
	"budget2/internal/services/retirement/overrides"
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

	// revision advances whenever something changes what the what-if page
	// should display. It exists so a polling page can detect a change without
	// recomputing the analysis. In-memory and not persisted: it only has to be
	// monotonic within one process, because a page load reads the current value
	// as its baseline.
	revision int
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
//
// The returned pointer is a PRIVATE deep copy. Every call allocates a fresh
// object; the manager's cached object never escapes through Load. Callers may
// mutate what they get and hand it back to Save/SaveWithRevision, and nothing
// else — no concurrent reader, no later Load — observes the mutation until
// Save publishes it.
//
// That is what makes a published settings object effectively immutable: once
// saveInternal stores a pointer in sm.cache, nothing mutates that object
// again, so a reader holding it from an earlier Load always sees a stable
// value even though it holds no lock while marshaling. This used to be a
// contract stated only in this comment, which made the wrong thing the easy
// thing: a handler written from the Load-then-mutate pattern wrote to state
// the 2s /whatif/poll path was marshaling, and a slice-header mutation
// (spending phases, scenario chain) could be read torn rather than merely
// stale. Copying here removes the escape hatch instead of documenting it.
//
// The copy goes through prepare.Clone, not prepare.DeepCopy, because
// DeepCopy's JSON round-trip drops every json:"-" field — including
// CurrentAge/SpouseAge, which validateChainInternal reads between a load and
// the save that follows it.
//
// Concurrency caveat for load-modify-save callers — the semantics are
// NARROWER than the in-place mutation they replace, not equal to it. Two
// overlapping edits resolve whole-object last-writer-wins: each holds an
// independent snapshot, so a handler that loaded before another's save and
// saves after it writes back the whole pre-save object and reverts the other's
// field. The shared-pointer version merged such edits at field level instead —
// both handlers mutated one struct, so both fields survived whichever save
// landed last. That merge was accidental and was itself the data race (it
// could tear a slice header), so trading it for a short, well-defined
// last-writer-wins window is the point; but it is a trade, not a wash.
//
// The fix for that lost update is to move such handlers behind manager methods
// that hold the write lock across load and save, as AddIncomeSource and
// friends already do. That is not this change.
//
// Cost: one marshal/unmarshal per call, microseconds. It lands only where a
// projection or a render follows immediately anyway — the /whatif/poll 204
// branch never calls Load.
func (sm *SettingsManager) Load() (*models.WhatIfSettings, error) {
	return sm.LoadContext(context.Background())
}

// LoadContext is Load with caller-supplied cancellation. It fails fast on
// entry (so an abandoned what-if request stops before any disk read) and
// threads ctx into the underlying decrypting read.
//
// Like Load, the returned pointer is a private copy; see Load's contract.
func (sm *SettingsManager) LoadContext(ctx context.Context) (*models.WhatIfSettings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sm.mu.RLock()
	// Return cache if available
	if sm.cache != nil {
		defer sm.mu.RUnlock()
		return prepare.Clone(sm.cache)
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check cache after acquiring write lock. Reachable only when
	// another goroutine populated the cache in the window between the
	// read-unlock above and this write-lock acquisition, so it has no
	// deterministic test: the private-copy guards cover the other two return
	// points and this one is the same one-line copy.
	if sm.cache != nil {
		return prepare.Clone(sm.cache)
	}

	settings, err := sm.loadInternalContext(ctx)
	if err != nil {
		return nil, err
	}

	// Publish the freshly decoded object, then hand out a copy of it: the
	// cached object is the one nothing may mutate, so it must not be the one
	// the caller receives.
	sm.cache = settings
	return prepare.Clone(settings)
}

// InvalidateCache drops the in-memory settings cache so the next Load
// re-reads from disk. Call it after anything rewrites the settings file
// behind the manager's back (e.g. a backup restore).
func (sm *SettingsManager) InvalidateCache() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cache = nil
	sm.bumpLocked()
}

// scenarioReconcileConfirmDelay is how long the cache-miss self-heal waits
// before re-checking that the active scenario file is really gone. External
// tools that replace files non-atomically (editor delete-then-create,
// Dropbox/rsync sync) open a brief window where Stat reports ENOENT for a
// file that is about to reappear; reverting on that blip would silently
// divert the user's subsequent saves to the default whatif.json.
const scenarioReconcileConfirmDelay = 100 * time.Millisecond

// reconcileActiveScenarioLocked reverts the active scenario to the default
// whatif.json when the active scenario's file no longer exists on disk.
// The active filename is pure in-process state (nothing on disk records
// it), so when something rewrites the settings directory behind the
// manager's back — e.g. a full-replace backup restore that prunes the
// active scenario file — the manager would otherwise keep pointing at a
// missing file, silently serve default settings, and resurrect the pruned
// file with defaults on the next save.
//
// confirmDelay > 0 re-checks after that delay and only reverts if the file
// is still missing, guarding against transient ENOENT windows (see
// scenarioReconcileConfirmDelay). Authoritative callers — the post-restore
// gate, which itself pruned the file — pass 0 to revert immediately.
//
// Caller must hold sm.mu (write); the confirm delay sleeps while holding
// it, which is acceptable because the missing-file path is rare and
// resolves permanently (revert or reappearance) after one confirmation.
func (sm *SettingsManager) reconcileActiveScenarioLocked(confirmDelay time.Duration) {
	if sm.filename == defaultWhatIfFilename {
		return
	}
	if _, err := sm.store.Stat(sm.filepath()); !os.IsNotExist(err) {
		return
	}
	if confirmDelay > 0 {
		time.Sleep(confirmDelay)
		if _, err := sm.store.Stat(sm.filepath()); !os.IsNotExist(err) {
			log.Printf("settings: active scenario file %s reappeared within %v; keeping it active",
				sm.filename, confirmDelay)
			return
		}
	}
	log.Printf("settings: active scenario file %s no longer exists; reverting to %s",
		sm.filename, defaultWhatIfFilename)
	sm.filename = defaultWhatIfFilename
	sm.cache = nil
	// The server now serves a DIFFERENT plan than it did a moment ago, and
	// every open page still renders the old one and still names the old
	// scenario. That is exactly what the revision exists to announce, so bump
	// it here rather than leaving the page confidently displaying a plan the
	// server no longer has. The caller holds the write lock (see the doc
	// comment above), so this must be bumpLocked, never Revision().
	sm.bumpLocked()
}

// BeginExternalRewrite serializes an external rewrite of the settings
// directory (e.g. a full-replace backup restore) against every other
// SettingsManager operation. It takes the manager's write lock and returns
// end; the caller performs the on-disk rewrite, then calls end exactly
// once. end drops the in-memory cache, reverts the active scenario to the
// default whatif.json if the rewrite removed its file, and releases the
// lock — all atomically with respect to concurrent saves and loads.
//
// Callers MUST NOT invoke any SettingsManager method between
// BeginExternalRewrite and end: the manager's lock is held for the whole
// window, so any such call deadlocks.
//
// Accepted residual: a save whose contents were computed BEFORE the
// rewrite but issued AFTER end() still wins last-writer-wins and can
// overwrite the rewritten data. The gate serializes in-flight operations;
// it cannot retract a stale caller's intent.
func (sm *SettingsManager) BeginExternalRewrite() (end func()) {
	sm.mu.Lock()
	return func() {
		sm.cache = nil
		sm.bumpLocked()
		// No confirm delay: the rewrite this gate serialized is the
		// authoritative source of the file's absence.
		sm.reconcileActiveScenarioLocked(0)
		sm.mu.Unlock()
	}
}

// loadInternal reads settings without acquiring lock (caller must hold lock).
// Context-less wrapper used by the in-package CRUD methods.
//
// Unlike Load, this returns the RAW object, not a copy — deliberately. It is
// the manager's own path: its callers hold the write lock, mutate what they
// get, and pass it to saveInternal, which publishes that same object as
// sm.cache. Copying here would be pure waste, and copying between the load and
// the save would break the invariant that the object saveInternal publishes is
// the one the mutation was applied to.
func (sm *SettingsManager) loadInternal() (*models.WhatIfSettings, error) {
	return sm.loadInternalContext(context.Background())
}

// loadInternalContext is loadInternal with caller-supplied cancellation,
// threaded into the decrypting file read. It too returns the raw object; see
// loadInternal.
func (sm *SettingsManager) loadInternalContext(ctx context.Context) (*models.WhatIfSettings, error) {
	// Ensure settings directory exists
	if err := sm.store.MkdirAll(sm.settingsDir, 0755); err != nil {
		return nil, err
	}

	// Self-heal: if the ACTIVE SCENARIO's file vanished behind our back
	// (e.g. a full-replace backup restore pruned it), revert to the default
	// whatif.json and load that instead of silently serving defaults for a
	// phantom file — which a later save would resurrect with stale data.
	// This runs on every cache-miss load, so it holds even if a future
	// restore-like path forgets to notify the manager. The confirm delay
	// keeps a transient ENOENT (non-atomic external file replace) from
	// silently switching the user off their scenario.
	sm.reconcileActiveScenarioLocked(scenarioReconcileConfirmDelay)

	path := sm.filepath()

	// Check if file exists (a missing DEFAULT file still means defaults)
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
	_, err := sm.SaveWithRevision(settings)
	return err
}

// SaveWithRevision writes settings to disk and returns the revision this write
// produced, read under the same write lock that performed it.
//
// Callers that render the saved state must use this number rather than reading
// Revision() afterwards: between the save and that read, a concurrent writer
// (the MCP /whatif/apply path) can bump the counter, and a client told to store
// that higher number as its baseline would poll with a revision that leads the
// state it was actually sent — every later poll answers 204 and the page shows
// pre-change figures forever.
func (sm *SettingsManager) SaveWithRevision(settings *models.WhatIfSettings) (int, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := sm.saveInternalAndBump(settings); err != nil {
		sm.cache = nil
		return 0, err
	}
	return sm.revision, nil
}

// Revision returns the current display revision.
func (sm *SettingsManager) Revision() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.revision
}

// SettingsDir returns the directory this manager reads scenarios from.
func (sm *SettingsManager) SettingsDir() string {
	return sm.settingsDir
}

// bumpLocked advances the revision. Caller must hold the write lock.
func (sm *SettingsManager) bumpLocked() {
	sm.revision++
}

// saveInternalAndBump is saveInternal plus a revision bump. Every mutation path
// calls this; saveInternal itself is left un-bumping because loadInternalContext
// calls it on a *read* when decode reports a migration, and a cache-miss load
// must not make every open page re-render.
func (sm *SettingsManager) saveInternalAndBump(settings *models.WhatIfSettings) error {
	if err := sm.saveInternal(settings); err != nil {
		return err
	}
	sm.bumpLocked()
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdateSettings updates all settings fields from form data and saves
// atomically, returning the saved settings and the revision this write
// produced (see SaveWithRevision for why the caller must not read Revision()
// afterwards instead).
func (sm *SettingsManager) UpdateSettings(updates map[string]interface{}) (*models.WhatIfSettings, int, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, 0, err
	}

	sm.applySettingsUpdates(settings, updates)

	if err := sm.saveInternalAndBump(settings); err != nil {
		return nil, 0, err
	}

	return settings, sm.revision, nil
}

// ApplyOverrides applies a sparse override set to the active scenario and saves
// it, returning the saved settings, the scenario filename it wrote to, and the
// revision this write produced.
//
// The whole body runs under one write lock. A caller doing Load → Apply → Save
// would not: Load releases the lock and hands back a private snapshot, so a
// concurrent UpdateSettings between the load and the save is silently reverted
// when the snapshot is written back whole. Every other mutation on this type
// loads, modifies, and saves inside one lock; this is no exception.
//
// expectedScenario, when non-empty, is the scenario filename the caller
// believes is active — typically the one it snapshotted before calling. It is
// compared against the active filename INSIDE the lock and before any load or
// write, so a scenario switch that lands between the caller's read and this
// call cannot divert the write to a plan the caller never backed up. A
// mismatch writes nothing and returns a *ScenarioConflictError. Empty means
// "no expectation" and preserves the previous behavior.
//
// The returned filename and revision are read under the same lock that
// performed the write. Callers must not read ActiveFilename() or Revision()
// afterwards — under concurrency either can describe a different writer's work.
func (sm *SettingsManager) ApplyOverrides(o overrides.Overrides, expectedScenario string) (*models.WhatIfSettings, string, int, error) {
	if err := o.ValidateWritable(); err != nil {
		return nil, "", 0, err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Before the load, and before the write: this is the only point at which
	// the check is a guarantee rather than an after-the-fact report. There is
	// no undo for a write that lands on an unbacked-up plan.
	if expectedScenario != "" && expectedScenario != sm.filename {
		return nil, "", 0, &ScenarioConflictError{Err: fmt.Errorf(
			"refusing to write: the active scenario is %s, but this change was prepared for %s "+
				"(the active scenario changed between the two). Nothing was written",
			sm.filename, expectedScenario)}
	}

	current, err := sm.loadInternal()
	if err != nil {
		return nil, "", 0, err
	}
	// The load itself can move the active scenario: reconcileActiveScenarioLocked
	// reverts to whatif.json when the active file has vanished. Re-check rather
	// than write to a file the caller never named. Still nothing written.
	if expectedScenario != "" && expectedScenario != sm.filename {
		return nil, "", 0, &ScenarioConflictError{Err: fmt.Errorf(
			"refusing to write: loading %s reverted the active scenario to %s. Nothing was written",
			expectedScenario, sm.filename)}
	}
	updated, err := overrides.Apply(current, o)
	if err != nil {
		return nil, "", 0, err
	}
	if err := sm.saveInternalAndBump(updated); err != nil {
		return nil, "", 0, err
	}
	return updated, sm.filename, sm.revision, nil
}

// UpdateSettingsWithPersons is UpdateSettings plus the household fields. It
// returns the revision this write produced for the same reason UpdateSettings
// does.
func (sm *SettingsManager) UpdateSettingsWithPersons(updates map[string]interface{}, startDate string, persons []models.Person) (*models.WhatIfSettings, int, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, 0, err
	}

	sm.applySettingsUpdates(settings, updates)
	settings.StartDate = startDate
	settings.Persons = persons

	if err := sm.saveInternalAndBump(settings); err != nil {
		return nil, 0, err
	}

	return settings, sm.revision, nil
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
	if v, ok := updates["taxable_cost_basis"].(*float64); ok {
		settings.TaxableCostBasis = v
	}
	applyACAUpdates(settings, updates)
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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

	if err := sm.saveInternalAndBump(settings); err != nil {
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
	sm.bumpLocked()
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
	if err := sm.saveInternalAndBump(settings); err != nil {
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
	sm.bumpLocked()

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
	sm.bumpLocked()

	log.Printf("Renamed scenario %s to %q", filename, newName)
	return nil
}

// applyACAUpdates folds the Affordable Care Act household facts into settings,
// allocating the config only when something is actually being set so a plan
// with no marketplace coverage does not sprout an empty ACA block.
func applyACAUpdates(settings *models.WhatIfSettings, updates map[string]interface{}) {
	size, hasSize := updates["aca_household_size"].(int)
	credit, hasCredit := updates["aca_premium_credit"].(*float64)
	advance, hasAdvance := updates["aca_advance_credits"].(bool)

	if !hasSize && !hasCredit && !hasAdvance {
		return
	}
	if settings.ACA == nil {
		settings.ACA = &models.ACAConfig{}
	}
	if hasSize {
		settings.ACA.HouseholdSize = size
	}
	if hasCredit {
		settings.ACA.AnnualPremiumTaxCredit = credit
	}
	if hasAdvance {
		settings.ACA.AdvanceCreditsTaken = advance
	}
}
