# Scenario Chaining Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a primary What-If scenario define an ordered chain of future scenarios that switch assumptions at specific ages while preserving live portfolio balances.

**Architecture:** Chain config stored in `WhatIfSettings.ScenarioChain`. Handler layer pre-resolves linked settings into `ResolvedChain` on the Calculator. Projection-style loops (deterministic, Monte Carlo, historical backtest) check for transitions at year boundaries and swap active settings without mutating `c.Settings`. Shared helpers handle rebasing, age correction, and allocation refresh.

**Tech Stack:** Go, HTMX, Go html/template, JSON file storage

**Spec:** `docs/superpowers/specs/2026-03-25-scenario-chaining-design.md`

---

### Task 1: Add ScenarioChainLink Type and WhatIfSettings Field

**Files:**
- Modify: `internal/models/whatif.go:6-74` (WhatIfSettings struct)

- [ ] **Step 1: Write the failing test**

Create test in `internal/models/whatif_test.go` (or add to existing):

```go
func TestWhatIfSettings_ScenarioChainSerialization(t *testing.T) {
	settings := &WhatIfSettings{
		ScenarioName: "Test",
		ScenarioChain: []ScenarioChainLink{
			{ScenarioFilename: "whatif_post-ss.json", TransitionAge: 70},
			{ScenarioFilename: "whatif_late.json", TransitionAge: 80},
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WhatIfSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.ScenarioChain) != 2 {
		t.Fatalf("expected 2 chain links, got %d", len(decoded.ScenarioChain))
	}
	if decoded.ScenarioChain[0].TransitionAge != 70 {
		t.Errorf("expected transition age 70, got %d", decoded.ScenarioChain[0].TransitionAge)
	}
	if decoded.ScenarioChain[1].ScenarioFilename != "whatif_late.json" {
		t.Errorf("expected whatif_late.json, got %s", decoded.ScenarioChain[1].ScenarioFilename)
	}
}

func TestWhatIfSettings_EmptyChainOmitted(t *testing.T) {
	settings := &WhatIfSettings{ScenarioName: "Test"}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "scenario_chain") {
		t.Error("empty scenario_chain should be omitted from JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/models/ -run TestWhatIfSettings_ScenarioChain -v`
Expected: FAIL — `ScenarioChainLink` type not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/models/whatif.go` before the `WhatIfSettings` struct:

```go
// ScenarioChainLink references a scenario to transition to at a given age
type ScenarioChainLink struct {
	ScenarioFilename string `json:"scenario_filename"`
	TransitionAge    int    `json:"transition_age"`
}
```

Add field to `WhatIfSettings` struct after `ScenarioName`:

```go
	// Scenario chaining: ordered list of scenarios to run after this one
	ScenarioChain []ScenarioChainLink `json:"scenario_chain,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/models/ -run TestWhatIfSettings_ScenarioChain -v`
Expected: PASS

- [ ] **Step 5: Build the whole project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success — no other files reference the new type yet

- [ ] **Step 6: Commit**

```bash
git add internal/models/whatif.go internal/models/whatif_test.go
git commit -m "feat: add ScenarioChainLink type and chain field to WhatIfSettings"
```

---

### Task 2: Add LoadScenarioSettings to SettingsManager

**Files:**
- Modify: `internal/services/retirement/settings.go:75-160` (near loadInternal)
- Test: `internal/services/retirement/settings_crud_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/services/retirement/settings_crud_test.go`:

```go
func TestLoadScenarioSettings_ReadsWithoutSwitching(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	// Save default scenario
	defaults := models.DefaultWhatIfSettings()
	defaults.PortfolioValue = 1000000
	if err := sm.Save(defaults); err != nil {
		t.Fatalf("save default: %v", err)
	}

	// Create a named scenario
	if _, err := sm.CreateScenario("Post-SS"); err != nil {
		t.Fatalf("create scenario: %v", err)
	}

	// Find the created scenario filename
	scenarios, _ := sm.ListScenarios()
	var postSSFilename string
	for _, s := range scenarios {
		if s.Name == "Post-SS" {
			postSSFilename = s.Filename
			break
		}
	}
	if postSSFilename == "" {
		t.Fatal("Post-SS scenario not found")
	}

	// Switch back to default
	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	// Load the post-SS scenario without switching
	loaded, err := sm.LoadScenarioSettings(postSSFilename)
	if err != nil {
		t.Fatalf("load scenario settings: %v", err)
	}

	// Verify it loaded something valid
	if loaded.PortfolioValue != 1000000 {
		t.Errorf("expected portfolio 1000000, got %f", loaded.PortfolioValue)
	}

	// Verify active scenario did NOT change
	if sm.ActiveFilename() != "whatif.json" {
		t.Errorf("active scenario changed to %s, expected whatif.json", sm.ActiveFilename())
	}
}

func TestLoadScenarioSettings_InvalidFilename(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	_, err := sm.LoadScenarioSettings("../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestLoadScenarioSettings_MissingFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	_, err := sm.LoadScenarioSettings("whatif_nonexistent.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestLoadScenarioSettings -v`
Expected: FAIL — `LoadScenarioSettings` method not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/services/retirement/settings.go` after `loadInternal()` (around line 160):

```go
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

	var settings models.WhatIfSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parsing scenario %s: %w", filename, err)
	}

	// Apply same initialization and migrations as loadInternal
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
	if len(settings.HealthcarePersons) == 0 && settings.MonthlyHealthcare > 0 {
		coverage := models.CoverageMedicare
		if settings.CurrentAge < 65 {
			coverage = models.CoverageACA
		}
		settings.HealthcarePersons = []models.HealthcarePerson{
			{
				ID:                    "migrated-user",
				Name:                  "User",
				CurrentAge:            settings.CurrentAge,
				CurrentCoverage:       coverage,
				CurrentMonthlyCost:    settings.MonthlyHealthcare,
				PreMedicareInflation:  settings.HealthcareInflation,
				MedicareMonthlyCost:   settings.MonthlyHealthcare,
				PostMedicareInflation: settings.HealthcareInflation,
				MedicareEligibleAge:   65,
			},
		}
	}

	return &settings, nil
}
```

> **Implementation note:** The initialization/migration code is duplicated from `loadInternal()`. A follow-up refactor could extract a shared `initializeSettings(*models.WhatIfSettings)` helper, but for now duplicating is safer — it avoids changing the existing `loadInternal` code path.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestLoadScenarioSettings -v`
Expected: PASS

- [ ] **Step 5: Run all existing settings tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestCRUD -v`
Expected: All existing tests still pass

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat: add LoadScenarioSettings for read-only scenario loading"
```

---

### Task 3: Add Chain Validation to SettingsManager

**Files:**
- Modify: `internal/services/retirement/settings.go`
- Test: `internal/services/retirement/settings_crud_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `settings_crud_test.go`:

```go
func TestValidateScenarioChain_AscendingAges(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70},
		{ScenarioFilename: "whatif_b.json", TransitionAge: 65}, // not ascending
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for non-ascending ages")
	}
}

func TestValidateScenarioChain_SelfReference(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif.json", TransitionAge: 70},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for self-reference")
	}
}

func TestValidateScenarioChain_AgeBelowCurrent(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 55},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for age below current age")
	}
}

func TestValidateScenarioChain_AgeBeyondProjection(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 96},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35 // max age = 95

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for age beyond projection")
	}
}

func TestValidateScenarioChain_MissingFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_nonexistent.json", TransitionAge: 70},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for missing scenario file")
	}
}

func TestValidateScenarioChain_DuplicateFilenames(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70},
		{ScenarioFilename: "whatif_a.json", TransitionAge: 80},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err == nil {
		t.Error("expected error for duplicate filenames")
	}
}

func TestValidateScenarioChain_ValidChain(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	// Save default scenario
	defaults := models.DefaultWhatIfSettings()
	if err := sm.Save(defaults); err != nil {
		t.Fatalf("save default: %v", err)
	}
	if _, err := sm.CreateScenario("Phase2"); err != nil {
		t.Fatalf("create Phase2: %v", err)
	}

	// Find the created filename
	scenarios, _ := sm.ListScenarios()
	var phase2File string
	for _, s := range scenarios {
		if s.Name == "Phase2" {
			phase2File = s.Filename
			break
		}
	}

	chain := []models.ScenarioChainLink{
		{ScenarioFilename: phase2File, TransitionAge: 70},
	}
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 35

	err := sm.ValidateScenarioChain(chain, settings, "whatif.json")
	if err != nil {
		t.Errorf("expected valid chain, got error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestValidateScenarioChain -v`
Expected: FAIL — `ValidateScenarioChain` not defined

- [ ] **Step 3: Write the implementation**

Add to `internal/services/retirement/settings.go`:

```go
// ValidateScenarioChain validates a chain configuration against the given settings.
// currentFilename is the filename of the scenario being saved (for self-reference check).
func (sm *SettingsManager) ValidateScenarioChain(chain []models.ScenarioChainLink, settings *models.WhatIfSettings, currentFilename string) error {
	if len(chain) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	prevAge := settings.CurrentAge - 1 // allow transition at CurrentAge

	for i, link := range chain {
		// Self-reference check
		if link.ScenarioFilename == currentFilename {
			return fmt.Errorf("chain link %d: cannot chain to self", i+1)
		}

		// Duplicate filename check
		if seen[link.ScenarioFilename] {
			return fmt.Errorf("chain link %d: duplicate scenario %s", i+1, link.ScenarioFilename)
		}
		seen[link.ScenarioFilename] = true

		// Age bounds check
		if link.TransitionAge < settings.CurrentAge {
			return fmt.Errorf("chain link %d: transition age %d is below current age %d", i+1, link.TransitionAge, settings.CurrentAge)
		}
		if link.TransitionAge >= settings.CurrentAge+settings.ProjectionYears {
			return fmt.Errorf("chain link %d: transition age %d is at or beyond projection end %d", i+1, link.TransitionAge, settings.CurrentAge+settings.ProjectionYears)
		}

		// Ascending age check
		if link.TransitionAge <= prevAge {
			return fmt.Errorf("chain link %d: transition age %d must be greater than previous %d", i+1, link.TransitionAge, prevAge)
		}
		prevAge = link.TransitionAge

		// File existence check
		path, err := sm.scenarioPath(link.ScenarioFilename)
		if err != nil {
			return fmt.Errorf("chain link %d: %w", i+1, err)
		}
		if _, err := sm.store.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("chain link %d: scenario file %s not found", i+1, link.ScenarioFilename)
		}
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestValidateScenarioChain -v`
Expected: All PASS

- [ ] **Step 5: Wire validation into Save()**

The spec requires chain validation on every save, not just the chain endpoint. If `CurrentAge` or `ProjectionYears` changes, an existing chain could become invalid. Add validation to `Save()` in `settings.go`:

In `Save()`, after acquiring the lock and before calling `saveInternal`, add:

```go
func (sm *SettingsManager) Save(settings *models.WhatIfSettings) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Validate chain if present
	if len(settings.ScenarioChain) > 0 {
		if err := sm.ValidateScenarioChain(settings.ScenarioChain, settings, sm.filename); err != nil {
			// Strip the invalid chain rather than rejecting the save —
			// the user may be changing CurrentAge without realizing it breaks the chain
			log.Printf("Warning: scenario chain invalidated, removing: %v", err)
			settings.ScenarioChain = nil
		}
	}

	return sm.saveInternal(settings)
}
```

Note: `ValidateScenarioChain` currently takes a read lock via `scenarioPath` and `store.Stat`. Since `Save` holds a write lock, `ValidateScenarioChain` must not acquire locks. Extract the validation logic into an internal `validateChainInternal` that does not lock, and have both `ValidateScenarioChain` (public, takes read lock) and `Save` (already holds write lock) call it.

- [ ] **Step 6: Test that Save strips invalid chains**

```go
func TestSave_StripsInvalidChainOnAgeChange(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	defaults := models.DefaultWhatIfSettings()
	defaults.CurrentAge = 60
	defaults.ProjectionYears = 35
	if err := sm.Save(defaults); err != nil {
		t.Fatalf("save default: %v", err)
	}

	if _, err := sm.CreateScenario("Phase2"); err != nil {
		t.Fatalf("create: %v", err)
	}

	scenarios, _ := sm.ListScenarios()
	var phase2File string
	for _, s := range scenarios {
		if s.Name == "Phase2" {
			phase2File = s.Filename
			break
		}
	}

	// Set a valid chain
	settings, _ := sm.Load()
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: phase2File, TransitionAge: 70},
	}
	if err := sm.Save(settings); err != nil {
		t.Fatalf("save with chain: %v", err)
	}

	// Now change CurrentAge to make the chain invalid
	settings, _ = sm.Load()
	settings.CurrentAge = 75 // transition at 70 is now below current age
	if err := sm.Save(settings); err != nil {
		t.Fatalf("save with changed age: %v", err)
	}

	// Chain should have been stripped
	reloaded, _ := sm.Load()
	if len(reloaded.ScenarioChain) != 0 {
		t.Errorf("expected chain to be stripped, got %d links", len(reloaded.ScenarioChain))
	}
}
```

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat: add ValidateScenarioChain with save-time validation"
```

---

### Task 4: Add Referential Integrity Check on Scenario Deletion

**Files:**
- Modify: `internal/services/retirement/settings.go:891-915` (DeleteScenario)
- Test: `internal/services/retirement/settings_crud_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestDeleteScenario_RejectsReferencedScenario(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir, storage.NewLocalStorage(""))

	// Save default with initial settings
	defaults := models.DefaultWhatIfSettings()
	if err := sm.Save(defaults); err != nil {
		t.Fatalf("save default: %v", err)
	}

	// Create a scenario to be referenced
	if _, err := sm.CreateScenario("Target"); err != nil {
		t.Fatalf("create Target: %v", err)
	}

	// Find the target filename
	scenarios, _ := sm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Target" {
			targetFile = s.Filename
			break
		}
	}

	// Add a chain reference from default to target
	settings, _ := sm.Load()
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: targetFile, TransitionAge: 70},
	}
	if err := sm.Save(settings); err != nil {
		t.Fatalf("save with chain: %v", err)
	}

	// Try to delete the referenced scenario
	err := sm.DeleteScenario(targetFile)
	if err == nil {
		t.Error("expected error when deleting referenced scenario")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestDeleteScenario_RejectsReferenced -v`
Expected: FAIL — deletion succeeds when it should not

- [ ] **Step 3: Modify DeleteScenario**

In `internal/services/retirement/settings.go`, add a helper method and modify `DeleteScenario()`.

Add the helper:

```go
// scenariosReferencingFile returns scenario names that reference the given filename in their chain.
// Caller must hold at least a read lock.
func (sm *SettingsManager) scenariosReferencingFile(filename string) []string {
	pattern := filepath.Join(sm.settingsDir, "whatif*.json")
	matches, err := sm.store.Glob(pattern)
	if err != nil {
		return nil
	}

	var referencing []string
	for _, match := range matches {
		base := filepath.Base(match)
		if base == filename {
			continue
		}
		data, err := sm.store.ReadFile(match)
		if err != nil {
			continue
		}
		var s models.WhatIfSettings
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		for _, link := range s.ScenarioChain {
			if link.ScenarioFilename == filename {
				name := s.ScenarioName
				if name == "" {
					name = base
				}
				referencing = append(referencing, name)
				break
			}
		}
	}
	return referencing
}
```

In `DeleteScenario`, after the `scenarioPath` call and before `sm.store.Remove`, add:

```go
	// Check referential integrity
	refs := sm.scenariosReferencingFile(filename)
	if len(refs) > 0 {
		return fmt.Errorf("cannot delete %s: referenced by scenario chain in %s", filename, strings.Join(refs, ", "))
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestDeleteScenario -v`
Expected: All PASS (new test and existing delete tests)

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat: reject deletion of scenarios referenced in chains"
```

---

### Task 5: Add ResolvedChain to Calculator and Constructor

**Files:**
- Modify: `internal/services/retirement/calculator.go:13-20` (Calculator struct and NewCalculator)
- Test: `internal/services/retirement/calculator_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestNewCalculatorWithChain(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 35

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 3000

	chain := []retirement.ResolvedScenarioChainLink{
		{
			TransitionAge: 70,
			Settings:      linked,
		},
	}

	calc := retirement.NewCalculatorWithChain(primary, chain)
	if calc == nil {
		t.Fatal("expected non-nil calculator")
	}
	if len(calc.ResolvedChain) != 1 {
		t.Errorf("expected 1 chain link, got %d", len(calc.ResolvedChain))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestNewCalculatorWithChain -v`
Expected: FAIL — `ResolvedScenarioChainLink` and `NewCalculatorWithChain` not defined

- [ ] **Step 3: Write the implementation**

In `internal/services/retirement/calculator.go`, update the struct and add the new constructor:

```go
// ResolvedScenarioChainLink holds a pre-loaded chain link for runtime use
type ResolvedScenarioChainLink struct {
	ScenarioFilename string
	TransitionAge    int
	Settings         *models.WhatIfSettings
}

// Calculator performs retirement projections and analysis
type Calculator struct {
	Settings      *models.WhatIfSettings
	ResolvedChain []ResolvedScenarioChainLink
}

// NewCalculator creates a new retirement calculator with the given settings (no chain)
func NewCalculator(settings *models.WhatIfSettings) *Calculator {
	return &Calculator{Settings: settings}
}

// NewCalculatorWithChain creates a chain-aware retirement calculator.
// The resolved chain must be pre-loaded and sorted by TransitionAge ascending.
func NewCalculatorWithChain(settings *models.WhatIfSettings, chain []ResolvedScenarioChainLink) *Calculator {
	return &Calculator{Settings: settings, ResolvedChain: chain}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestNewCalculatorWithChain -v`
Expected: PASS

- [ ] **Step 5: Build the whole project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success — existing `NewCalculator` calls are unchanged

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_test.go
git commit -m "feat: add ResolvedScenarioChainLink and NewCalculatorWithChain"
```

---

### Task 6: Extract Shared Chain Transition Helpers

**Files:**
- Create: `internal/services/retirement/chain.go`
- Create: `internal/services/retirement/chain_test.go`

This task creates the reusable helpers that all three projection loops will call. No loop changes yet.

- [ ] **Step 1: Write the failing tests**

Create `internal/services/retirement/chain_test.go`:

```go
package retirement

import (
	"testing"

	"budget2/internal/models"
)

func intPtr(v int) *int { return &v }

func TestRebaseIncomeSources(t *testing.T) {
	sources := []models.IncomeSource{
		{Name: "SS", StartMonth: 0, EndMonth: nil, Amount: 2000},          // perpetual
		{Name: "Part-time", StartMonth: 24, EndMonth: intPtr(60), Amount: 1000}, // ends at month 60
		{Name: "Expired", StartMonth: 0, EndMonth: intPtr(12), Amount: 500},     // ended before transition
	}

	result := rebaseIncomeSources(sources, 36) // transition at month 36

	if len(result) != 2 {
		t.Fatalf("expected 2 sources (expired dropped), got %d", len(result))
	}

	// SS: was active from 0, should now be offset 0, no end
	if result[0].StartMonth != 0 {
		t.Errorf("SS StartMonth: expected 0, got %d", result[0].StartMonth)
	}
	if result[0].EndMonth != nil {
		t.Errorf("SS EndMonth: expected nil, got %d", *result[0].EndMonth)
	}

	// Part-time: was 24-60, rebased to 0-24 (24-36=negative clamped to 0, 60-36=24)
	if result[1].StartMonth != 0 {
		t.Errorf("Part-time StartMonth: expected 0, got %d", result[1].StartMonth)
	}
	if result[1].EndMonth == nil || *result[1].EndMonth != 24 {
		t.Errorf("Part-time EndMonth: expected 24, got %v", result[1].EndMonth)
	}
}

func TestRebaseExpenseSources(t *testing.T) {
	sources := []models.ExpenseSource{
		{Name: "Gym", StartYear: 0, EndYear: 0, Amount: 100},
		{Name: "Tuition", StartYear: 2, EndYear: 5, Amount: 500},
		{Name: "Expired", StartYear: 0, EndYear: 1, Amount: 200},
	}

	result := rebaseExpenseSources(sources, 3) // transition at year 3

	if len(result) != 2 {
		t.Fatalf("expected 2 sources (expired dropped), got %d", len(result))
	}

	if result[0].StartYear != 0 {
		t.Errorf("Gym StartYear: expected 0, got %d", result[0].StartYear)
	}
	if result[1].EndYear != 2 {
		t.Errorf("Tuition EndYear: expected 2, got %d", result[1].EndYear)
	}
}

func TestRebaseBigTicketItems(t *testing.T) {
	items := []models.BigTicketItem{
		{Name: "Home Sale", Year: 5, Amount: 200000},
		{Name: "Past Event", Year: 1, Amount: 50000},
		{Name: "At Transition", Year: 3, Amount: 100000},
	}

	result := rebaseBigTicketItems(items, 3) // transition at year 3

	if len(result) != 2 {
		t.Fatalf("expected 2 items (past dropped), got %d", len(result))
	}

	// "At Transition" fires at year 0 of the rebased timeline
	if result[0].Year != 0 {
		t.Errorf("At Transition Year: expected 0, got %d", result[0].Year)
	}

	// "Home Sale" fires at year 2
	if result[1].Year != 2 {
		t.Errorf("Home Sale Year: expected 2, got %d", result[1].Year)
	}
}

func TestRebaseRothConversion(t *testing.T) {
	config := &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000,
		StartYear:    2,
		EndYear:      8,
	}

	result := rebaseRothConversion(config, 3)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.StartYear != 0 {
		t.Errorf("StartYear: expected 0, got %d", result.StartYear)
	}
	if result.EndYear != 5 {
		t.Errorf("EndYear: expected 5, got %d", result.EndYear)
	}
}

func TestRebaseRothConversion_ExpiredDisabled(t *testing.T) {
	config := &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000,
		StartYear:    0,
		EndYear:      2,
	}

	result := rebaseRothConversion(config, 3)

	if result != nil && result.Enabled {
		t.Error("expected disabled/nil result for expired conversion")
	}
}

func TestPrepareChainedSettings(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.SpouseAge = 58

	linked := models.DefaultWhatIfSettings()
	linked.CurrentAge = 70 // should be overwritten
	linked.SpouseAge = 68  // should be overwritten
	linked.MonthlyLivingExpenses = 3000

	result := prepareChainedSettings(linked, primary, 10) // transition 10 years in

	// Age fields should match primary
	if result.CurrentAge != 60 {
		t.Errorf("CurrentAge: expected 60, got %d", result.CurrentAge)
	}
	if result.SpouseAge != 58 {
		t.Errorf("SpouseAge: expected 58, got %d", result.SpouseAge)
	}

	// Expenses should come from linked
	if result.MonthlyLivingExpenses != 3000 {
		t.Errorf("MonthlyLivingExpenses: expected 3000, got %f", result.MonthlyLivingExpenses)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestRebase|TestPrepareChained" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write the implementation**

Create `internal/services/retirement/chain.go`:

```go
package retirement

import (
	"budget2/internal/models"
)

// prepareChainedSettings creates a copy of the linked settings with age fields
// overwritten from the primary scenario and time-based fields rebased to the
// transition year. The returned settings are safe to use in a projection loop
// without mutating the original.
func prepareChainedSettings(linked *models.WhatIfSettings, primary *models.WhatIfSettings, transitionYear int) *models.WhatIfSettings {
	// Clone to avoid mutating the pre-loaded settings
	prepared := *linked

	// Overwrite age fields from primary so all age calculations use one timeline
	prepared.CurrentAge = primary.CurrentAge
	prepared.SpouseAge = primary.SpouseAge
	prepared.PhaseAgeReference = primary.PhaseAgeReference

	// Overwrite projection config from primary
	prepared.ProjectionYears = primary.ProjectionYears

	// Preserve primary's TaxDeferredDelayYears (not restarted)
	prepared.TaxDeferredDelayYears = primary.TaxDeferredDelayYears

	transitionMonth := transitionYear * 12

	// Rebase time-offset fields
	prepared.IncomeSources = rebaseIncomeSources(linked.IncomeSources, transitionMonth)
	prepared.ExpenseSources = rebaseExpenseSources(linked.ExpenseSources, transitionYear)
	prepared.BigTicketItems = rebaseBigTicketItems(linked.BigTicketItems, transitionYear)
	prepared.RothConversion = rebaseRothConversion(linked.RothConversion, transitionYear)

	// Rebase healthcare person ages
	if len(linked.HealthcarePersons) > 0 {
		persons := make([]models.HealthcarePerson, len(linked.HealthcarePersons))
		copy(persons, linked.HealthcarePersons)
		for i := range persons {
			persons[i].CurrentAge = persons[i].CurrentAge - transitionYear
		}
		prepared.HealthcarePersons = persons
	}

	return &prepared
}

// rebaseIncomeSources adjusts income source time offsets for a chain transition.
// Sources whose end month is before or at the transition are dropped.
// Note: IncomeSource.EndMonth is *int (nil = perpetual).
func rebaseIncomeSources(sources []models.IncomeSource, transitionMonth int) []models.IncomeSource {
	result := make([]models.IncomeSource, 0, len(sources))
	for _, s := range sources {
		s := s // copy

		// Drop sources that have ended before the transition
		if s.EndMonth != nil && *s.EndMonth <= transitionMonth {
			continue
		}

		s.StartMonth = max(0, s.StartMonth-transitionMonth)
		if s.EndMonth != nil {
			rebased := *s.EndMonth - transitionMonth
			s.EndMonth = &rebased
		}

		result = append(result, s)
	}
	return result
}

// rebaseExpenseSources adjusts expense source time offsets for a chain transition.
// Sources whose end year is before or at the transition are dropped.
func rebaseExpenseSources(sources []models.ExpenseSource, transitionYear int) []models.ExpenseSource {
	result := make([]models.ExpenseSource, 0, len(sources))
	for _, s := range sources {
		s := s // copy

		if s.EndYear > 0 && s.EndYear <= transitionYear {
			continue
		}

		s.StartYear = max(0, s.StartYear-transitionYear)
		if s.EndYear > 0 {
			s.EndYear = s.EndYear - transitionYear
		}

		result = append(result, s)
	}
	return result
}

// rebaseBigTicketItems adjusts big-ticket item years for a chain transition.
// Items with year before the transition are dropped.
func rebaseBigTicketItems(items []models.BigTicketItem, transitionYear int) []models.BigTicketItem {
	result := make([]models.BigTicketItem, 0, len(items))
	for _, item := range items {
		item := item // copy
		rebased := item.Year - transitionYear
		if rebased < 0 {
			continue
		}
		item.Year = rebased
		result = append(result, item)
	}
	return result
}

// rebaseRothConversion adjusts Roth conversion timing for a chain transition.
// Returns a disabled config if the conversion period has already ended.
func rebaseRothConversion(config *models.RothConversionConfig, transitionYear int) *models.RothConversionConfig {
	if config == nil || !config.Enabled {
		return config
	}

	result := *config // copy

	if result.EndYear > 0 && result.EndYear <= transitionYear {
		result.Enabled = false
		return &result
	}

	result.StartYear = max(0, result.StartYear-transitionYear)
	if result.EndYear > 0 {
		result.EndYear = result.EndYear - transitionYear
	}

	return &result
}

// nextChainTransition returns the updated chain index and prepared settings for the
// next chain transition that should fire at the given year. Returns the same index
// and nil if no transition is due.
func (c *Calculator) nextChainTransition(currentYear int, nextChainIndex int, primarySettings *models.WhatIfSettings) (int, *models.WhatIfSettings) {
	if nextChainIndex >= len(c.ResolvedChain) {
		return nextChainIndex, nil
	}

	link := c.ResolvedChain[nextChainIndex]
	currentAge := primarySettings.CurrentAge + currentYear

	if currentAge >= link.TransitionAge {
		transitionYear := link.TransitionAge - primarySettings.CurrentAge
		prepared := prepareChainedSettings(link.Settings, primarySettings, transitionYear)
		return nextChainIndex + 1, prepared
	}

	return nextChainIndex, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestRebase|TestPrepareChained" -v`
Expected: All PASS

- [ ] **Step 5: Build the whole project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/chain.go internal/services/retirement/chain_test.go
git commit -m "feat: add chain transition helpers for rebasing and age correction"
```

---

### Task 7: Wire Chain Transitions into RunProjection

**Files:**
- Modify: `internal/services/retirement/calculator.go:335-850` (RunProjection)
- Test: `internal/services/retirement/calculator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `calculator_test.go`:

```go
func TestRunProjection_ChainTransition_BalancesCarryOver(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000
	linked.InvestmentReturn = 4.0

	chain := []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	}

	calcNoChain := NewCalculator(primary)
	projNoChain := calcNoChain.RunProjection()

	calcChain := NewCalculatorWithChain(primary, chain)
	projChain := calcChain.RunProjection()

	// Before transition (month 119), both should be identical
	if len(projChain.Months) < 121 {
		t.Fatalf("expected at least 121 months, got %d", len(projChain.Months))
	}

	if projChain.Months[119].TotalBalance != projNoChain.Months[119].TotalBalance {
		t.Errorf("month 119 balance should match: chain=%f, nochain=%f",
			projChain.Months[119].TotalBalance, projNoChain.Months[119].TotalBalance)
	}

	// After transition, chained should have lower balance due to higher expenses
	if projChain.Months[132].TotalBalance >= projNoChain.Months[132].TotalBalance {
		t.Errorf("after transition, chained balance should be lower: chain=%f, nochain=%f",
			projChain.Months[132].TotalBalance, projNoChain.Months[132].TotalBalance)
	}
}

func TestRunProjection_ChainTransition_AtCurrentAge(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	chain := []ResolvedScenarioChainLink{
		{TransitionAge: 60, Settings: linked},
	}

	calc := NewCalculatorWithChain(primary, chain)
	proj := calc.RunProjection()

	// Linked scenario's expenses ($5000) should be used from month 0
	if proj.Months[0].TotalExpenses < 4500 {
		t.Errorf("expected expenses near 5000, got %f", proj.Months[0].TotalExpenses)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestRunProjection_ChainTransition" -v`
Expected: FAIL — chain transitions not wired in

- [ ] **Step 3: Wire chain transitions into RunProjection**

In `RunProjection()`, replace line 336 (`s := c.Settings`) with:

```go
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings
```

At the start of the `if m%12 == 0` block (line 367), add the chain transition check before the existing annual adjustment code:

```go
		if m%12 == 0 {
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					// Recalculate living expenses from new settings
					if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
						phaseMultiplier := s.GetSpendingMultiplier(phaseAge)
						currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * cumulativeInflation
					} else {
						currentLivingExpenses = s.MonthlyLivingExpenses * cumulativeInflation
					}
				}
			}
```

The existing annual adjustment code follows and naturally uses `s` for all calculations.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestRunProjection_ChainTransition" -v`
Expected: PASS

- [ ] **Step 5: Run all existing projection tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestRunProjection" -v`
Expected: All existing tests still PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_test.go
git commit -m "feat: wire chain transitions into RunProjection"
```

---

### Task 8: Wire Chain Transitions into Monte Carlo Simulation

**Files:**
- Modify: `internal/services/retirement/calculator.go:1394-1660` (runSingleMonteCarloSimulation)
- Test: `internal/services/retirement/calculator_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestMonteCarloSimulation_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 2000000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 8000
	linked.InvestmentReturn = 4.0

	chainCalc := NewCalculatorWithChain(primary, []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	})
	noChainCalc := NewCalculator(primary)

	chainMC := chainCalc.RunMonteCarloSimulation(100)
	noChainMC := noChainCalc.RunMonteCarloSimulation(100)

	// Higher expenses after 70 should reduce success rate
	if chainMC.Stats.SuccessRate >= noChainMC.Stats.SuccessRate {
		t.Errorf("chained MC success rate (%f) should be lower than no-chain (%f)",
			chainMC.Stats.SuccessRate, noChainMC.Stats.SuccessRate)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestMonteCarloSimulation_ChainTransition -v -timeout 60s`
Expected: FAIL

- [ ] **Step 3: Wire chain transitions into runSingleMonteCarloSimulation**

Replace line 1395 (`s := c.Settings`) with:

```go
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings
```

At the start of the `if m%12 == 0` block (line 1455), add before the existing code:

```go
		if m%12 == 0 {
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					// Refresh cached allocation variables
					tdStock, tdBond, tdCash = s.GetTaxDeferredAllocation()
					rothStock, rothBond, rothCash = s.GetRothAllocation()
					taxStock, taxBond, taxCash = s.GetTaxableAllocation()

					// Recalculate living expenses
					if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
						phaseMultiplier := s.GetSpendingMultiplier(phaseAge)
						currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * cumulativeInflation
					} else {
						currentLivingExpenses = s.MonthlyLivingExpenses * cumulativeInflation
					}
				}
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestMonteCarloSimulation_ChainTransition -v -timeout 60s`
Expected: PASS

- [ ] **Step 5: Run all existing Monte Carlo tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestMonteCarlo|TestRunSingleMonteCarlo" -v -timeout 60s`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_test.go
git commit -m "feat: wire chain transitions into Monte Carlo simulation"
```

---

### Task 9: Wire Chain Transitions into Historical Backtest

**Files:**
- Modify: `internal/services/retirement/backtest.go:127-250` (runSingleHistoricalSequence)
- Test: `internal/services/retirement/backtest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHistoricalBacktest_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 2000000
	primary.MonthlyLivingExpenses = 3000
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 10000

	chainCalc := NewCalculatorWithChain(primary, []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	})
	noChainCalc := NewCalculator(primary)

	chainBT := chainCalc.RunHistoricalBacktest()
	noChainBT := noChainCalc.RunHistoricalBacktest()

	if chainBT == nil || noChainBT == nil {
		t.Fatal("expected non-nil backtest results")
	}

	if chainBT.SuccessRate >= noChainBT.SuccessRate {
		t.Errorf("chained backtest success (%f) should be lower than no-chain (%f)",
			chainBT.SuccessRate, noChainBT.SuccessRate)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestHistoricalBacktest_ChainTransition -v -timeout 60s`
Expected: FAIL

- [ ] **Step 3: Wire chain transitions into runSingleHistoricalSequence**

Replace line 128 (`s := c.Settings`) with:

```go
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings
```

At the start of the `if m%12 == 0` block (line 175), add before existing code:

```go
		if m%12 == 0 {
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					// Refresh cached allocation variables
					tdStock, tdBond, tdCash = s.GetTaxDeferredAllocation()
					rothStock, rothBond, rothCash = s.GetRothAllocation()
					taxStock, taxBond, taxCash = s.GetTaxableAllocation()

					// Recalculate living expenses
					if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
						phaseMultiplier := s.GetSpendingMultiplier(phaseAge)
						currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * cumulativeInflation
					} else {
						currentLivingExpenses = s.MonthlyLivingExpenses * cumulativeInflation
					}
				}
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestHistoricalBacktest_ChainTransition -v -timeout 60s`
Expected: PASS

- [ ] **Step 5: Run all existing backtest tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestHistorical|TestBacktest" -v -timeout 60s`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/backtest.go internal/services/retirement/backtest_test.go
git commit -m "feat: wire chain transitions into historical backtest"
```

---

### Task 10: Propagate Chain Through Sensitivity and Failure-Point Analysis

**Files:**
- Modify: `internal/services/retirement/calculator.go:858-953` (CalculateSensitivity and CalculateFailurePoints)
- Test: `internal/services/retirement/calculator_test.go`

Sensitivity and failure-point analyses create derived calculators via `NewCalculator(&modifiedSettings)`. These need the chain propagated.

- [ ] **Step 1: Write the failing test**

```go
func TestSensitivity_ChainPropagated(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 20
	primary.PortfolioValue = 1000000
	primary.MonthlyLivingExpenses = 3000
	primary.InvestmentReturn = 6.0
	primary.InflationRate = 3.0

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 5000

	calcChain := NewCalculatorWithChain(primary, []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	})
	calcNoChain := NewCalculator(primary)

	sensChain := calcChain.CalculateSensitivity()
	sensNoChain := calcNoChain.CalculateSensitivity()

	if len(sensChain) == 0 || len(sensNoChain) == 0 {
		t.Fatal("expected sensitivity results")
	}

	// At least one scenario should differ
	anyDifferent := false
	for i := range sensChain {
		if sensChain[i].LongevityYears != sensNoChain[i].LongevityYears {
			anyDifferent = true
			break
		}
	}
	if !anyDifferent {
		t.Error("expected at least one sensitivity scenario to differ with chain")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestSensitivity_ChainPropagated -v -timeout 60s`
Expected: FAIL

- [ ] **Step 3: Update all derived calculator calls to propagate chain**

In `CalculateSensitivity()` (line 900), change:

```go
		modCalc := NewCalculatorWithChain(&modifiedSettings, c.ResolvedChain)
```

In `findReturnThreshold()`, `findInflationThreshold()`, `findExpensesThreshold()`, and `findPortfolioThreshold()` — every `NewCalculator(&modSettings)` call becomes:

```go
		modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
```

Search for all occurrences within the calculator file:

Run: `grep -n 'NewCalculator(&mod' internal/services/retirement/calculator.go`

Replace each one.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run TestSensitivity_ChainPropagated -v -timeout 60s`
Expected: PASS

- [ ] **Step 5: Run all existing sensitivity and failure-point tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/services/retirement/ -run "TestSensitivity|TestFailure|TestCalculateFailure" -v -timeout 60s`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/services/retirement/calculator.go internal/services/retirement/calculator_test.go
git commit -m "feat: propagate chain through sensitivity and failure-point analyses"
```

---

### Task 11: Add buildCalculator Helper and Chain-Aware Cache Key

**Files:**
- Modify: `internal/handlers/whatif/handlers.go`

- [ ] **Step 1: Add buildCalculator helper**

Add after the cache functions (around line 60):

```go
// buildCalculator creates a chain-aware calculator from settings.
// Returns the calculator, a dependency hash, and an error if any chained scenario fails to load.
func buildCalculator(settings *models.WhatIfSettings) (*retirement.Calculator, string, error) {
	hashData := getSettingsHash(settings)

	if len(settings.ScenarioChain) == 0 {
		return retirement.NewCalculator(settings), hashData, nil
	}

	chain := make([]retirement.ResolvedScenarioChainLink, 0, len(settings.ScenarioChain))
	for _, link := range settings.ScenarioChain {
		linked, err := retirementMgr.LoadScenarioSettings(link.ScenarioFilename)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load chained scenario %s: %w", link.ScenarioFilename, err)
		}

		linkedHash := getSettingsHash(linked)
		hashData += linkedHash

		chain = append(chain, retirement.ResolvedScenarioChainLink{
			ScenarioFilename: link.ScenarioFilename,
			TransitionAge:    link.TransitionAge,
			Settings:         linked,
		})
	}

	combined := sha256.Sum256([]byte(hashData))
	combinedHash := fmt.Sprintf("%x", combined[:8])

	return retirement.NewCalculatorWithChain(settings, chain), combinedHash, nil
}
```

- [ ] **Step 2: Update runAnalysisWithCache to use buildCalculator**

Replace `runAnalysisWithCache()` (lines 77-91):

```go
func runAnalysisWithCache(settings *models.WhatIfSettings) (*models.WhatIfAnalysis, error) {
	calc, depHash, err := buildCalculator(settings)
	if err != nil {
		return nil, err
	}

	cache.mu.RLock()
	if cache.hash == depHash && time.Since(cache.cachedAt) < 5*time.Minute {
		cached := cache.analysis
		cache.mu.RUnlock()
		return cached, nil
	}
	cache.mu.RUnlock()

	analysis := calc.RunFullAnalysis()

	cache.mu.Lock()
	cache.hash = depHash
	cache.analysis = analysis
	cache.cachedAt = time.Now()
	cache.mu.Unlock()

	return analysis, nil
}
```

**Important:** All callers of `runAnalysisWithCache` must be updated to handle the error. The typical pattern:

```go
analysis, err := runAnalysisWithCache(settings)
if err != nil {
	renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
	return
}
```

Update every existing call site (there are several in `handlers.go`).

- [ ] **Step 3: Replace all direct NewCalculator calls in handlers**

Search for all `retirement.NewCalculator(settings)` in the handlers file:

Run: `grep -n 'retirement.NewCalculator' internal/handlers/whatif/handlers.go`

Replace each with `calc, _ := buildCalculator(settings)` (for chart/MC endpoints) or use `buildCalculator` through `runAnalysisWithCache` (for full analysis endpoints).

For `handleWhatIfProjectionChart` (line 977):
```go
	calc, _, err := buildCalculator(settings)
	if err != nil {
		renderError(w, "Failed to build calculator: "+err.Error(), http.StatusInternalServerError)
		return
	}
	projection := calc.RunProjection()
```

For `handleWhatIfMonteCarlo` (line 1072):
```go
	calc, _, err := buildCalculator(settings)
	if err != nil {
		renderError(w, "Failed to build calculator: "+err.Error(), http.StatusInternalServerError)
		return
	}
	analysis := calc.RunFullAnalysis()
```

For all other `retirement.NewCalculator(settings)` occurrences (including line 1709), same pattern. There are 4 total call sites to replace.

- [ ] **Step 4: Build the whole project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/whatif/handlers.go
git commit -m "feat: add buildCalculator helper with chain-aware cache key"
```

---

### Task 12: Add Chain CRUD Handlers and Routes

**Files:**
- Modify: `internal/handlers/whatif/handlers.go` (add handlers and routes)

- [ ] **Step 1: Add the route registrations**

In `RegisterRoutes()` (around line 193-197), add:

```go
	r.Post("/whatif/chain", handleWhatIfUpdateChain)
	r.Delete("/whatif/chain/{index}", handleWhatIfDeleteChainLink)
```

- [ ] **Step 2: Implement handleWhatIfUpdateChain**

```go
func handleWhatIfUpdateChain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	scenarioFiles := r.Form["chain_scenario[]"]
	ageStrings := r.Form["chain_age[]"]

	if len(scenarioFiles) != len(ageStrings) {
		renderError(w, "Mismatched chain scenario and age counts", http.StatusBadRequest)
		return
	}

	chain := make([]models.ScenarioChainLink, 0, len(scenarioFiles))
	for i := range scenarioFiles {
		if scenarioFiles[i] == "" {
			continue
		}
		age, err := strconv.Atoi(ageStrings[i])
		if err != nil {
			renderError(w, fmt.Sprintf("Invalid age at position %d: %s", i+1, ageStrings[i]), http.StatusBadRequest)
			return
		}
		chain = append(chain, models.ScenarioChainLink{
			ScenarioFilename: scenarioFiles[i],
			TransitionAge:    age,
		})
	}

	// Sort by transition age
	sort.Slice(chain, func(i, j int) bool {
		return chain[i].TransitionAge < chain[j].TransitionAge
	})

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	activeFilename := retirementMgr.ActiveFilename()
	if err := retirementMgr.ValidateScenarioChain(chain, settings, activeFilename); err != nil {
		renderError(w, "Invalid chain: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings.ScenarioChain = chain
	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

	scenarios, _ := retirementMgr.ListScenarios()
	partialData := map[string]interface{}{
		"Settings":       settings,
		"Analysis":       analysis,
		"Scenarios":      scenarios,
		"ActiveFilename": activeFilename,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}
```

- [ ] **Step 3: Implement handleWhatIfDeleteChainLink**

```go
func handleWhatIfDeleteChainLink(w http.ResponseWriter, r *http.Request) {
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		renderError(w, "Invalid index", http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if index < 0 || index >= len(settings.ScenarioChain) {
		renderError(w, "Index out of range", http.StatusBadRequest)
		return
	}

	settings.ScenarioChain = append(settings.ScenarioChain[:index], settings.ScenarioChain[index+1:]...)

	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

	activeFilename := retirementMgr.ActiveFilename()
	scenarios, _ := retirementMgr.ListScenarios()
	partialData := map[string]interface{}{
		"Settings":       settings,
		"Analysis":       analysis,
		"Scenarios":      scenarios,
		"ActiveFilename": activeFilename,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}
```

- [ ] **Step 4: Add necessary imports**

Ensure `"sort"` is in the import block.

- [ ] **Step 5: Build the whole project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/whatif/handlers.go
git commit -m "feat: add chain update and delete handlers with routes"
```

---

### Task 13: Create Scenario Chain UI Card

**Files:**
- Create: `web/templates/components/whatif/scenario-chain-card.html`
- Modify: `web/templates/pages/whatif.html:67-76` (include the card)

- [ ] **Step 1: Create the card template**

Create `web/templates/components/whatif/scenario-chain-card.html`. The card should:
- Show "Scenario Chain" header with "Sequential plans" subtitle
- When empty, show helper text and "Add Step" button
- When populated, show each chain link as a row with: scenario dropdown, "at age" input, remove button
- Use HTMX `hx-post="/whatif/chain"` targeting `#whatif-results`
- The "Add Step" button uses JavaScript to clone a new row using safe DOM creation methods (createElement, appendChild, textContent) — **do not use innerHTML for dynamic content**
- Use the Go template `{{range .Scenarios}}` to build scenario option lists
- Exclude the active scenario from dropdown options via `{{if ne .Filename $.ActiveFilename}}`
- Follow the same card styling as the existing whatif component cards (bg-white/dark:bg-gray-800, rounded-lg, shadow, p-4)

- [ ] **Step 2: Include the card in whatif.html**

In `web/templates/pages/whatif.html`, add after the expense card (line 75):

```html
        {{template "whatif-scenario-chain" .}}
```

- [ ] **Step 3: Build and verify**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 4: Manual test**

Start the app and verify:
- The "Scenario Chain" card appears at the bottom of the left column
- "Add Step" adds a row with scenario dropdown and age input
- The dropdown excludes the active scenario
- "Apply" saves and re-runs analysis

- [ ] **Step 5: Commit**

```bash
git add web/templates/components/whatif/scenario-chain-card.html web/templates/pages/whatif.html
git commit -m "feat: add scenario chain UI card with HTMX integration"
```

---

### Task 14: Add Non-Chain-Aware Panel Notes

**Files:**
- Modify: `web/templates/components/whatif/budget-analysis.html`
- Modify: `web/templates/components/whatif/present-value.html`
- Modify: `web/templates/components/whatif/rmd.html`

- [ ] **Step 1: Add chain note to each panel**

Add at the top of each card body, conditionally:

```html
{{if .Settings.ScenarioChain}}
<p class="text-xs text-amber-600 dark:text-amber-400 mb-2 italic">Scenario chain is not yet applied to this panel.</p>
{{end}}
```

Add this to:
- `budget-analysis.html`
- `present-value.html`
- `rmd.html`

- [ ] **Step 2: Build and verify**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add web/templates/components/whatif/budget-analysis.html web/templates/components/whatif/present-value.html web/templates/components/whatif/rmd.html
git commit -m "feat: add chain-not-applied notes to budget, PV, and RMD panels"
```

---

### Task 15: Add Cache Regression Test

**Files:**
- Modify: `internal/handlers/whatif/handlers_test.go`

- [ ] **Step 1: Write the cache regression test**

```go
func TestCacheKey_IncludesChainedScenarios(t *testing.T) {
	settings1 := models.DefaultWhatIfSettings()
	settings1.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_b.json", TransitionAge: 70},
	}

	settings2 := models.DefaultWhatIfSettings()
	settings2.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_c.json", TransitionAge: 70},
	}

	hash1 := getSettingsHash(settings1)
	hash2 := getSettingsHash(settings2)

	if hash1 == hash2 {
		t.Error("different chain references should produce different cache keys")
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/whatif/ -run TestCacheKey -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/whatif/handlers_test.go
git commit -m "test: add cache key regression test for scenario chains"
```

---

### Task 16: Full Integration Test

- [ ] **Step 1: Run all tests**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./... -timeout 120s`
Expected: All PASS

- [ ] **Step 2: Build the full project**

Run: `cd /home/darrell/bin/ai/budget2 && go build ./...`
Expected: Success

- [ ] **Step 3: Manual end-to-end test**

1. Start the app
2. Go to What-If page
3. Create a second scenario ("Post-SS") with different expenses
4. Switch back to "Current Plan"
5. In the Scenario Chain card, add a step: select "Post-SS" at age 70
6. Click Apply
7. Verify the projection chart updates
8. Verify Monte Carlo and Historical Backtest panels reflect the chain
9. Verify Budget Fit, PV, and RMD panels show the "not yet applied" note

- [ ] **Step 4: Commit any fixes found during manual testing**

```bash
git add -A
git commit -m "fix: address issues found during manual integration testing"
```
