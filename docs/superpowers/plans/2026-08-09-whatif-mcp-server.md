# What-If MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the what-if planner's analysis over MCP so a plan can be discussed conversationally in Claude Code, with the ability to re-run the engine under modified assumptions.

**Architecture:** Pure logic lives in `internal/services/whatifmcp` (shaped views, scenario loading, override application) and is fully testable with no MCP dependency. A thin `cmd/whatif-mcp/main.go` wires those into an MCP server over stdio. Read and run only — nothing writes to `data/`.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk` v1.7.0, existing `internal/services/retirement` (orchestrator, engine, analysis, prepare) and `internal/services/storage`.

**Design spec:** `docs/superpowers/specs/2026-08-09-whatif-mcp-server-design.md`

## Global Constraints

- **Never write to `data/`.** No tool may create, modify, rename, or delete a scenario file. Loading and in-memory mutation of copies only.
- **No changes to `internal/services/retirement/{engine,analysis,prepare}` or the `retirement` orchestrator.** This is a consumer. If a change there seems required, stop and raise it.
- **No network calls and no credentials.** The binary holds no API key and opens no sockets. stdio transport only.
- **Never return per-month series except from `get_months`.** `Projection.Months` is 360 records for a 30-year plan.
- **Never return embedded `*models.WhatIfSettings`.** `TaxOptimizerAnalysis` candidates each carry one.
- **Round all currency to whole dollars** in shaped output using `math.Round`.
- **Go toolchain 1.26.5**, per `go.mod`. Gate is `go build ./... && go vet ./... && go test ./... && staticcheck ./...`; the pre-commit hook runs `make check` (which also runs `govulncheck`).
- **Run tests bare** — never pipe `go test` through `grep`/`head`; the pipe reports the last command's exit code and a red suite reads as exit 0.
- Analysis-package-style tests build inputs with `prepare.MustFrom(t, s)`. Set ages via `s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, age)`, never `s.CurrentAge` (which `prepare.From` recomputes and overwrites).

---

### Task 1: Shaped analysis view

The core of the build: convert `*models.WhatIfAnalysis` into a compact struct safe to put in a model's context.

**Files:**
- Create: `internal/services/whatifmcp/view.go`
- Test: `internal/services/whatifmcp/view_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type AnalysisView struct{…}`, `func ShapeAnalysis(a *models.WhatIfAnalysis, includeMonteCarlo bool) AnalysisView`. Tasks 4 and 5 call `ShapeAnalysis`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/whatifmcp/view_test.go`:

```go
package whatifmcp

import (
	"testing"

	"budget2/internal/models"
)

func sampleAnalysis() *models.WhatIfAnalysis {
	months := make([]models.ProjectionMonth, 24)
	for i := range months {
		months[i] = models.ProjectionMonth{Month: i, PortfolioBalance: 1_000_000 - float64(i)*1_000}
	}
	return &models.WhatIfAnalysis{
		Settings: &models.WhatIfSettings{PortfolioValue: 1_000_000, ProjectionYears: 2},
		Projection: &models.ProjectionResult{
			Months:       months,
			FinalBalance: 976_000.4567,
			Survives:     true,
			YearlySummaries: []models.ProjectionYearSummary{
				{Year: 0, StartingBalance: 1_000_000, EndingBalance: 988_000.9, Taxes: 5_000.4, IRMAA: 0},
				{Year: 1, StartingBalance: 988_000.9, EndingBalance: 976_000.4, Taxes: 5_100.6, IRMAA: 0},
			},
		},
		Sustainability: &models.SustainabilityScore{Score: 88, Label: "Good"},
		BudgetFit:      &models.BudgetFitAnalysis{MonthlyExpenses: 4_000.2, MonthlyIncome: 1_000.8, MonthlyGap: 3_000.1},
		RMD:            &models.RMDAnalysis{StartAge: 73, StartsInYears: 8, TaxDeferredValue: 600_000.7},
		Tax:            &models.TaxAnalysis{TotalTaxPaid: 10_101.9, AverageEffectiveRate: 12.5},
	}
}

func TestShapeAnalysis_ExcludesPerMonthSeries(t *testing.T) {
	v := ShapeAnalysis(sampleAnalysis(), true)
	if len(v.Years) != 2 {
		t.Fatalf("Years = %d, want 2", len(v.Years))
	}
	// The view type must not carry a per-month field at all; this test guards
	// the year series being present without months leaking in alongside it.
	if v.Headline.FinalBalance != 976_000 {
		t.Errorf("FinalBalance = %v, want 976000 (rounded)", v.Headline.FinalBalance)
	}
}

func TestShapeAnalysis_RoundsCurrencyToWholeDollars(t *testing.T) {
	v := ShapeAnalysis(sampleAnalysis(), true)
	if v.Years[0].Taxes != 5_000 {
		t.Errorf("Years[0].Taxes = %v, want 5000", v.Years[0].Taxes)
	}
	if v.Budget.MonthlyGap != 3_000 {
		t.Errorf("MonthlyGap = %v, want 3000", v.Budget.MonthlyGap)
	}
	if v.Tax.TotalTaxPaid != 10_102 {
		t.Errorf("TotalTaxPaid = %v, want 10102", v.Tax.TotalTaxPaid)
	}
}

func TestShapeAnalysis_OmitsMonteCarloWhenNotRequested(t *testing.T) {
	a := sampleAnalysis()
	a.MonteCarlo = &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 91.5}}

	if got := ShapeAnalysis(a, true); got.MonteCarlo == nil {
		t.Fatal("MonteCarlo should be present when includeMonteCarlo is true")
	}
	if got := ShapeAnalysis(a, false); got.MonteCarlo != nil {
		t.Errorf("MonteCarlo = %+v, want nil when includeMonteCarlo is false", got.MonteCarlo)
	}
}

func TestShapeAnalysis_NilSectionsDoNotPanic(t *testing.T) {
	a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{}}
	v := ShapeAnalysis(a, true)
	if v.Budget != nil || v.RMD != nil || v.Tax != nil || v.MonteCarlo != nil {
		t.Errorf("expected nil sections for an empty analysis, got %+v", v)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/whatifmcp/ -run TestShapeAnalysis -v`
Expected: build failure — `undefined: ShapeAnalysis`.

- [ ] **Step 3: Write the implementation**

Create `internal/services/whatifmcp/view.go`:

```go
// Package whatifmcp shapes what-if analysis output for MCP consumption and
// applies scenario overrides. It reads scenarios and runs the engine; it never
// writes to the data directory.
package whatifmcp

import (
	"math"

	"budget2/internal/models"
)

// round0 rounds a currency amount to whole dollars. The engine's sub-cent
// precision is meaningful internally and noise in a conversation.
func round0(v float64) float64 { return math.Round(v) }

// AnalysisView is a compact projection of models.WhatIfAnalysis. It carries
// headline scalars and per-YEAR series only: models.ProjectionMonth series are
// 360 records for a 30-year plan and are served separately by MonthWindow, and
// the tax-optimizer candidates each embed a full *models.WhatIfSettings which
// is excluded entirely.
type AnalysisView struct {
	Headline   HeadlineView    `json:"headline"`
	Budget     *BudgetView     `json:"budget,omitempty"`
	Years      []YearView      `json:"years,omitempty"`
	RMD        *RMDView        `json:"rmd,omitempty"`
	Tax        *TaxView        `json:"tax,omitempty"`
	MonteCarlo *MonteCarloView `json:"monte_carlo,omitempty"`
}

type HeadlineView struct {
	PortfolioValue      float64 `json:"portfolio_value"`
	FinalBalance        float64 `json:"final_balance"`
	Survives            bool    `json:"survives"`
	DepletionMonth      *int    `json:"depletion_month,omitempty"`
	ProjectionYears     int     `json:"projection_years"`
	SustainabilityScore int     `json:"sustainability_score,omitempty"`
	SustainabilityLabel string  `json:"sustainability_label,omitempty"`
}

type BudgetView struct {
	MonthlyExpenses float64 `json:"monthly_expenses"`
	MonthlyIncome   float64 `json:"monthly_income"`
	MonthlyTaxes    float64 `json:"monthly_taxes"`
	MonthlyIRMAA    float64 `json:"monthly_irmaa"`
	MonthlyRMD      float64 `json:"monthly_rmd"`
	MonthlyGap      float64 `json:"monthly_gap"`
}

type YearView struct {
	Year            int     `json:"year"`
	StartingBalance float64 `json:"starting_balance"`
	EndingBalance   float64 `json:"ending_balance"`
	Growth          float64 `json:"growth"`
	MAGI            float64 `json:"magi"`
	Taxes           float64 `json:"taxes"`
	IRMAA           float64 `json:"irmaa"`
	Expenses        float64 `json:"expenses"`
	Withdrawals     float64 `json:"withdrawals"`
}

type RMDView struct {
	StartAge         int     `json:"start_age"`
	StartsInYears    int     `json:"starts_in_years"`
	TaxDeferredValue float64 `json:"tax_deferred_value"`
	TotalFirst10Yr   float64 `json:"total_rmds_first_10yr"`
}

type TaxView struct {
	TotalFederalTaxPaid  float64 `json:"total_federal_tax_paid"`
	TotalStateTaxPaid    float64 `json:"total_state_tax_paid"`
	TotalTaxPaid         float64 `json:"total_tax_paid"`
	AverageEffectiveRate float64 `json:"average_effective_rate"`
	ConversionTaxPaid    float64 `json:"conversion_tax_paid"`
}

// MonteCarloView carries stats only, never the full distribution.
type MonteCarloView struct {
	SuccessRate float64 `json:"success_rate"`
}

// ShapeAnalysis converts a full analysis into its compact view.
//
// includeMonteCarlo is false for run_scenario: the orchestrator auto-seeds the
// Monte Carlo RNG from the clock, so MC figures differ between two runs of
// identical inputs. Including them in an override comparison would present that
// noise as an effect of the override.
func ShapeAnalysis(a *models.WhatIfAnalysis, includeMonteCarlo bool) AnalysisView {
	v := AnalysisView{}
	if a == nil {
		return v
	}

	if a.Settings != nil {
		v.Headline.PortfolioValue = round0(a.Settings.PortfolioValue)
		v.Headline.ProjectionYears = a.Settings.ProjectionYears
	}
	if p := a.Projection; p != nil {
		v.Headline.FinalBalance = round0(p.FinalBalance)
		v.Headline.Survives = p.Survives
		v.Headline.DepletionMonth = p.DepletionMonth
		for _, y := range p.YearlySummaries {
			v.Years = append(v.Years, YearView{
				Year:            y.Year,
				StartingBalance: round0(y.StartingBalance),
				EndingBalance:   round0(y.EndingBalance),
				Growth:          round0(y.Growth),
				MAGI:            round0(y.MAGI),
				Taxes:           round0(y.Taxes),
				IRMAA:           round0(y.IRMAA),
				Expenses:        round0(y.Expenses),
				Withdrawals:     round0(y.Withdrawals),
			})
		}
	}
	if s := a.Sustainability; s != nil {
		v.Headline.SustainabilityScore = s.Score
		v.Headline.SustainabilityLabel = s.Label
	}
	if b := a.BudgetFit; b != nil {
		v.Budget = &BudgetView{
			MonthlyExpenses: round0(b.MonthlyExpenses),
			MonthlyIncome:   round0(b.MonthlyIncome),
			MonthlyTaxes:    round0(b.MonthlyTaxes),
			MonthlyIRMAA:    round0(b.MonthlyIRMAA),
			MonthlyRMD:      round0(b.MonthlyRMD),
			MonthlyGap:      round0(b.MonthlyGap),
		}
	}
	if r := a.RMD; r != nil {
		v.RMD = &RMDView{
			StartAge:         r.StartAge,
			StartsInYears:    r.StartsInYears,
			TaxDeferredValue: round0(r.TaxDeferredValue),
			TotalFirst10Yr:   round0(r.TotalRMDsOver10Yr),
		}
	}
	if t := a.Tax; t != nil {
		v.Tax = &TaxView{
			TotalFederalTaxPaid:  round0(t.TotalFederalTaxPaid),
			TotalStateTaxPaid:    round0(t.TotalStateTaxPaid),
			TotalTaxPaid:         round0(t.TotalTaxPaid),
			AverageEffectiveRate: t.AverageEffectiveRate,
			ConversionTaxPaid:    round0(t.ConversionTaxPaid),
		}
	}
	if includeMonteCarlo && a.MonteCarlo != nil && a.MonteCarlo.Stats != nil {
		v.MonteCarlo = &MonteCarloView{SuccessRate: a.MonteCarlo.Stats.SuccessRate}
	}
	return v
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/services/whatifmcp/view.go internal/services/whatifmcp/view_test.go
git commit -m "feat(whatifmcp): shaped analysis view for MCP output"
```

---

### Task 2: Scenario source

Lists and loads saved scenarios, and resolves "no scenario named" to the active one.

**Files:**
- Create: `internal/services/whatifmcp/scenarios.go`
- Test: `internal/services/whatifmcp/scenarios_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `type Source struct{…}`, `func NewSource(settingsDir string, store *storage.Storage) *Source`, `func (s *Source) List() ([]ScenarioInfo, error)`, `func (s *Source) Load(name string) (*models.WhatIfSettings, string, error)`. Tasks 3 and 5 call these.

- [ ] **Step 1: Write the failing test**

Create `internal/services/whatifmcp/scenarios_test.go`:

```go
package whatifmcp

import (
	"strings"
	"testing"
)

func TestSource_LoadUnknownScenarioNamesValidOnes(t *testing.T) {
	s := newTestSource(t)
	_, _, err := s.Load("no-such-plan.json")
	if err == nil {
		t.Fatal("expected an error for an unknown scenario")
	}
	if !strings.Contains(err.Error(), "no-such-plan.json") {
		t.Errorf("error should name the requested scenario, got: %v", err)
	}
	if !strings.Contains(err.Error(), "whatif.json") {
		t.Errorf("error should list valid scenario names, got: %v", err)
	}
}

func TestSource_LoadEmptyNameResolvesActive(t *testing.T) {
	s := newTestSource(t)
	settings, name, err := s.Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if settings == nil {
		t.Fatal("Load(\"\") returned nil settings")
	}
	if name == "" {
		t.Error("Load(\"\") should report the resolved scenario filename")
	}
}

func TestSource_ListReportsActiveFlag(t *testing.T) {
	s := newTestSource(t)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("List() returned no scenarios")
	}
	active := 0
	for _, sc := range list {
		if sc.Active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("expected exactly one active scenario, got %d", active)
	}
}
```

- [ ] **Step 2: Add the test helper**

Append to `internal/services/whatifmcp/scenarios_test.go`. It copies the repo's real `data/settings` fixtures into a temp dir so the test never touches live data:

```go
import (
	"os"
	"path/filepath"

	"budget2/internal/services/storage"
)

// newTestSource builds a Source over a temp copy of the repo's shipped
// settings fixtures. Never point a test at the real data/ directory.
func newTestSource(t *testing.T) *Source {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("..", "..", "..", "data", "settings")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSource(dir, store)
}
```

Note `storage.New(baseDir string) (*Storage, error)` returns two values.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/services/whatifmcp/ -run TestSource -v`
Expected: build failure — `undefined: NewSource`.

- [ ] **Step 4: Write the implementation**

Create `internal/services/whatifmcp/scenarios.go`:

```go
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
		name = s.sm.ActiveScenario()
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS. If `ActiveScenario()` returns an empty string for the fixture set, have `Load` fall back to `"whatif.json"` before erroring and add a test asserting that fallback.

- [ ] **Step 6: Commit**

```bash
git add internal/services/whatifmcp/scenarios.go internal/services/whatifmcp/scenarios_test.go
git commit -m "feat(whatifmcp): read-only scenario source with active-scenario resolution"
```

---

### Task 3: Overrides

Applies a sparse override set to a deep copy of a scenario and runs the engine.

**Files:**
- Create: `internal/services/whatifmcp/overrides.go`
- Test: `internal/services/whatifmcp/overrides_test.go`

**Interfaces:**
- Consumes: `round0` and `AnalysisView` (Task 1); `*Source` (Task 2).
- Produces: `type Overrides struct{…}`, `func Apply(base *models.WhatIfSettings, o Overrides) (*models.WhatIfSettings, error)`, `func RunWithOverrides(base *models.WhatIfSettings, o Overrides) (AnalysisView, error)`. Task 5 calls `RunWithOverrides`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/whatifmcp/overrides_test.go`:

```go
package whatifmcp

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func baseSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge("2026-01", 65)
	s.PortfolioValue = 1_500_000
	s.ProjectionYears = 5
	return s
}

func TestApply_ChangesOnlyTheNamedField(t *testing.T) {
	base := baseSettings()
	got, err := Apply(base, Overrides{MonthlyLivingExpenses: ptr(7_000.0)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.MonthlyLivingExpenses != 7_000 {
		t.Errorf("MonthlyLivingExpenses = %v, want 7000", got.MonthlyLivingExpenses)
	}
	if got.ProjectionYears != base.ProjectionYears {
		t.Errorf("ProjectionYears changed to %v, want %v", got.ProjectionYears, base.ProjectionYears)
	}
	if base.MonthlyLivingExpenses == 7_000 {
		t.Error("Apply mutated the base settings; it must operate on a copy")
	}
}

func TestApply_PreservesPerYearOverridesAcrossDeepCopy(t *testing.T) {
	base := baseSettings()
	base.RothConversion = &models.RothConversionConfig{
		Enabled:          true,
		PerYearOverrides: map[int]float64{0: 50_000},
	}
	got, err := Apply(base, Overrides{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.RothConversion == nil || got.RothConversion.PerYearOverrides[0] != 50_000 {
		t.Fatalf("PerYearOverrides lost in copy: %+v", got.RothConversion)
	}
}

func TestApply_RejectsInvalidValuesNamingTheField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		o     Overrides
		field string
	}{
		{"negative expenses", Overrides{MonthlyLivingExpenses: ptr(-1.0)}, "monthly_living_expenses"},
		{"claim age too low", Overrides{SocialSecurityClaimAge: ptrInt(61)}, "social_security_claim_age"},
		{"claim age too high", Overrides{SocialSecurityClaimAge: ptrInt(71)}, "social_security_claim_age"},
		{"zero projection years", Overrides{ProjectionYears: ptrInt(0)}, "projection_years"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Apply(baseSettings(), tc.o); err == nil {
				t.Fatal("expected a validation error")
			} else if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error should name %q, got: %v", tc.field, err)
			}
		})
	}
}

func TestRunWithOverrides_HigherExpensesLowerFinalBalance(t *testing.T) {
	lo, err := RunWithOverrides(baseSettings(), Overrides{MonthlyLivingExpenses: ptr(3_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(low): %v", err)
	}
	hi, err := RunWithOverrides(baseSettings(), Overrides{MonthlyLivingExpenses: ptr(9_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(high): %v", err)
	}
	if hi.Headline.FinalBalance >= lo.Headline.FinalBalance {
		t.Errorf("higher expenses should reduce final balance: low=%v high=%v",
			lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	}
}

func TestRunWithOverrides_OmitsMonteCarlo(t *testing.T) {
	v, err := RunWithOverrides(baseSettings(), Overrides{})
	if err != nil {
		t.Fatalf("RunWithOverrides: %v", err)
	}
	if v.MonteCarlo != nil {
		t.Error("run_scenario output must omit Monte Carlo: it is auto-seeded and varies between identical runs")
	}
}

func ptr(f float64) *float64 { return &f }
func ptrInt(i int) *int      { return &i }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/whatifmcp/ -run 'TestApply|TestRunWithOverrides' -v`
Expected: build failure — `undefined: Apply`, `undefined: Overrides`.

- [ ] **Step 3: Write the implementation**

Create `internal/services/whatifmcp/overrides.go`:

```go
package whatifmcp

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// Overrides is a sparse set of scenario changes. A nil pointer means "leave
// unchanged" — that is why every field is a pointer rather than a value.
type Overrides struct {
	MonthlyLivingExpenses  *float64 `json:"monthly_living_expenses,omitempty" jsonschema:"monthly living expenses in dollars"`
	HealthcareInflation    *float64 `json:"healthcare_inflation,omitempty" jsonschema:"annual healthcare inflation as a percent, e.g. 6 for 6%"`
	InflationRate          *float64 `json:"inflation_rate,omitempty" jsonschema:"annual general inflation as a percent"`
	InvestmentReturn       *float64 `json:"investment_return,omitempty" jsonschema:"annual investment return as a percent; 0 means use the asset allocation"`
	RothConversionAmount   *float64 `json:"roth_conversion_amount,omitempty" jsonschema:"annual Roth conversion amount in dollars"`
	RothConversionStart    *int     `json:"roth_conversion_start_year,omitempty" jsonschema:"projection year the conversions begin, 0 = now"`
	RothConversionEnd      *int     `json:"roth_conversion_end_year,omitempty" jsonschema:"projection year the conversions end, 0 = indefinite"`
	SocialSecurityClaimAge *int     `json:"social_security_claim_age,omitempty" jsonschema:"primary Social Security claim age, 62-70"`
	SpouseClaimAge         *int     `json:"spouse_claim_age,omitempty" jsonschema:"spouse Social Security claim age, 62-70"`
	ProjectionYears        *int     `json:"projection_years,omitempty" jsonschema:"length of the projection in years"`
	FilingStatus           *string  `json:"filing_status,omitempty" jsonschema:"single, married_joint, married_separate, or head_of_household"`
}

// Apply returns a deep copy of base with the overrides applied. base is never
// mutated. Invalid values are rejected before any engine work, naming the field.
func Apply(base *models.WhatIfSettings, o Overrides) (*models.WhatIfSettings, error) {
	if base == nil {
		return nil, fmt.Errorf("apply overrides: nil base settings")
	}
	if err := o.validate(); err != nil {
		return nil, err
	}

	s, err := prepare.DeepCopy(base)
	if err != nil {
		return nil, fmt.Errorf("copy settings: %w", err)
	}
	// prepare.DeepCopy round-trips through JSON, so fields tagged json:"-" do
	// not survive. PerYearOverrides is the known instance; re-attach it, the
	// same workaround analysis/tax_optimizer.go applies.
	if base.RothConversion != nil && base.RothConversion.PerYearOverrides != nil && s.RothConversion != nil {
		s.RothConversion.PerYearOverrides = base.RothConversion.PerYearOverrides
	}

	if o.MonthlyLivingExpenses != nil {
		s.MonthlyLivingExpenses = *o.MonthlyLivingExpenses
	}
	if o.HealthcareInflation != nil {
		s.HealthcareInflation = *o.HealthcareInflation
	}
	if o.InflationRate != nil {
		s.InflationRate = *o.InflationRate
	}
	if o.InvestmentReturn != nil {
		s.InvestmentReturn = *o.InvestmentReturn
	}
	if o.ProjectionYears != nil {
		s.ProjectionYears = *o.ProjectionYears
	}
	if o.RothConversionAmount != nil || o.RothConversionStart != nil || o.RothConversionEnd != nil {
		if s.RothConversion == nil {
			s.RothConversion = &models.RothConversionConfig{}
		}
		if o.RothConversionAmount != nil {
			s.RothConversion.AnnualAmount = *o.RothConversionAmount
			s.RothConversion.Enabled = *o.RothConversionAmount > 0
		}
		if o.RothConversionStart != nil {
			s.RothConversion.StartYear = *o.RothConversionStart
		}
		if o.RothConversionEnd != nil {
			s.RothConversion.EndYear = *o.RothConversionEnd
		}
	}
	if o.SocialSecurityClaimAge != nil || o.SpouseClaimAge != nil {
		if s.SocialSecurity == nil {
			return nil, fmt.Errorf("scenario has no social_security configuration to override")
		}
		if o.SocialSecurityClaimAge != nil {
			s.SocialSecurity.ClaimAge = *o.SocialSecurityClaimAge
		}
		if o.SpouseClaimAge != nil {
			s.SocialSecurity.SpouseClaimAge = *o.SpouseClaimAge
		}
	}
	if o.FilingStatus != nil {
		if s.TaxConfig == nil {
			s.TaxConfig = models.DefaultTaxConfig()
		}
		s.TaxConfig.FilingStatus = models.FilingStatus(*o.FilingStatus)
	}
	return s, nil
}

func (o Overrides) validate() error {
	if o.MonthlyLivingExpenses != nil && *o.MonthlyLivingExpenses < 0 {
		return fmt.Errorf("monthly_living_expenses must be >= 0, got %v", *o.MonthlyLivingExpenses)
	}
	if o.RothConversionAmount != nil && *o.RothConversionAmount < 0 {
		return fmt.Errorf("roth_conversion_amount must be >= 0, got %v", *o.RothConversionAmount)
	}
	if o.ProjectionYears != nil && (*o.ProjectionYears < 1 || *o.ProjectionYears > 60) {
		return fmt.Errorf("projection_years must be between 1 and 60, got %d", *o.ProjectionYears)
	}
	if a := o.SocialSecurityClaimAge; a != nil && (*a < 62 || *a > 70) {
		return fmt.Errorf("social_security_claim_age must be between 62 and 70, got %d", *a)
	}
	if a := o.SpouseClaimAge; a != nil && (*a < 62 || *a > 70) {
		return fmt.Errorf("spouse_claim_age must be between 62 and 70, got %d", *a)
	}
	if f := o.FilingStatus; f != nil {
		switch models.FilingStatus(*f) {
		case models.FilingSingle, models.FilingMarriedJoint,
			models.FilingMarriedSeparate, models.FilingHeadOfHousehold:
		default:
			return fmt.Errorf("filing_status %q is not one of single, married_joint, married_separate, head_of_household", *f)
		}
	}
	return nil
}

// RunWithOverrides applies the overrides and runs the full analysis, returning
// the shaped view. Monte Carlo is excluded: the orchestrator auto-seeds its RNG
// from the clock, so including it would make two identical runs disagree.
func RunWithOverrides(base *models.WhatIfSettings, o Overrides) (AnalysisView, error) {
	s, err := Apply(base, o)
	if err != nil {
		return AnalysisView{}, err
	}
	prepared, err := prepare.From(s)
	if err != nil {
		return AnalysisView{}, fmt.Errorf("prepare settings: %w", err)
	}
	a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})
	return ShapeAnalysis(a, false), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS. `TestRunWithOverrides_*` runs the full fan-out including 1000 Monte Carlo iterations, so expect a few seconds.

- [ ] **Step 5: Commit**

```bash
git add internal/services/whatifmcp/overrides.go internal/services/whatifmcp/overrides_test.go
git commit -m "feat(whatifmcp): sparse scenario overrides with validation and engine run"
```

---

### Task 4: Month window

The range-bounded drill-in for per-month detail.

**Files:**
- Create: `internal/services/whatifmcp/months.go`
- Test: `internal/services/whatifmcp/months_test.go`

**Interfaces:**
- Consumes: `round0` (Task 1).
- Produces: `const MaxMonthSpan = 120`, `type MonthRow struct{…}`, `func MonthWindow(p *models.ProjectionResult, from, to int) ([]MonthRow, error)`. Task 5 calls `MonthWindow`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/whatifmcp/months_test.go`:

```go
package whatifmcp

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func projectionWithMonths(n int) *models.ProjectionResult {
	months := make([]models.ProjectionMonth, n)
	for i := range months {
		months[i] = models.ProjectionMonth{
			Month:            i,
			PortfolioBalance: 1_000_000 - float64(i)*100.6,
			TaxesPaid:        10.4,
			RMDWithdrawal:    5.5,
		}
	}
	return &models.ProjectionResult{Months: months}
}

func TestMonthWindow_ReturnsInclusiveRange(t *testing.T) {
	rows, err := MonthWindow(projectionWithMonths(360), 12, 23)
	if err != nil {
		t.Fatalf("MonthWindow: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("len(rows) = %d, want 12", len(rows))
	}
	if rows[0].Month != 12 || rows[11].Month != 23 {
		t.Errorf("range = %d..%d, want 12..23", rows[0].Month, rows[11].Month)
	}
	if rows[0].TaxesPaid != 10 {
		t.Errorf("TaxesPaid = %v, want 10 (rounded)", rows[0].TaxesPaid)
	}
}

func TestMonthWindow_RejectsSpanOverLimitStatingTheLimit(t *testing.T) {
	_, err := MonthWindow(projectionWithMonths(360), 0, 359)
	if err == nil {
		t.Fatal("expected an error for a 360-month span")
	}
	if !strings.Contains(err.Error(), "120") {
		t.Errorf("error should state the 120-month limit, got: %v", err)
	}
}

func TestMonthWindow_RejectsOutOfRangeStatingValidRange(t *testing.T) {
	_, err := MonthWindow(projectionWithMonths(24), 30, 40)
	if err == nil {
		t.Fatal("expected an error for an out-of-range window")
	}
	if !strings.Contains(err.Error(), "0") || !strings.Contains(err.Error(), "23") {
		t.Errorf("error should state the valid 0..23 range, got: %v", err)
	}
}

func TestMonthWindow_RejectsInvertedRange(t *testing.T) {
	if _, err := MonthWindow(projectionWithMonths(24), 10, 5); err == nil {
		t.Fatal("expected an error when from > to")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/whatifmcp/ -run TestMonthWindow -v`
Expected: build failure — `undefined: MonthWindow`.

- [ ] **Step 3: Write the implementation**

Create `internal/services/whatifmcp/months.go`:

```go
package whatifmcp

import (
	"fmt"

	"budget2/internal/models"
)

// MaxMonthSpan bounds a single get_months call. A 30-year projection holds 360
// month records; returning them all would swamp the conversation this server
// exists to support.
const MaxMonthSpan = 120

// MonthRow is one month of projection detail.
type MonthRow struct {
	Month              int     `json:"month"`
	PortfolioBalance   float64 `json:"portfolio_balance"`
	TaxDeferredBalance float64 `json:"tax_deferred_balance"`
	TaxableBalance     float64 `json:"taxable_balance"`
	RothBalance        float64 `json:"roth_balance"`
	TotalExpenses      float64 `json:"total_expenses"`
	TotalIncome        float64 `json:"total_income"`
	TaxesPaid          float64 `json:"taxes_paid"`
	RMDWithdrawal      float64 `json:"rmd_withdrawal"`
	NetWithdrawal      float64 `json:"net_withdrawal"`
	Depleted           bool    `json:"depleted"`
}

// MonthWindow returns the inclusive [from, to] month range, rejecting spans
// wider than MaxMonthSpan and windows outside the projection.
func MonthWindow(p *models.ProjectionResult, from, to int) ([]MonthRow, error) {
	if p == nil || len(p.Months) == 0 {
		return nil, fmt.Errorf("projection has no months")
	}
	last := len(p.Months) - 1
	if from > to {
		return nil, fmt.Errorf("from_month (%d) must not exceed to_month (%d)", from, to)
	}
	if from < 0 || to > last {
		return nil, fmt.Errorf("requested months %d..%d are outside the projection; valid range is %d..%d", from, to, 0, last)
	}
	if span := to - from + 1; span > MaxMonthSpan {
		return nil, fmt.Errorf("requested %d months; at most %d may be returned per call — narrow the range", span, MaxMonthSpan)
	}

	rows := make([]MonthRow, 0, to-from+1)
	for _, m := range p.Months[from : to+1] {
		rows = append(rows, MonthRow{
			Month:              m.Month,
			PortfolioBalance:   round0(m.PortfolioBalance),
			TaxDeferredBalance: round0(m.TaxDeferredBalance),
			TaxableBalance:     round0(m.TaxableBalance),
			RothBalance:        round0(m.RothBalance),
			TotalExpenses:      round0(m.TotalExpenses),
			TotalIncome:        round0(m.TotalIncome),
			TaxesPaid:          round0(m.TaxesPaid),
			RMDWithdrawal:      round0(m.RMDWithdrawal),
			NetWithdrawal:      round0(m.NetWithdrawal),
			Depleted:           m.Depleted,
		})
	}
	return rows, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/whatifmcp/months.go internal/services/whatifmcp/months_test.go
git commit -m "feat(whatifmcp): range-bounded per-month drill-in"
```

---

### Task 5: MCP server and tool handlers

Wires the pure logic into an MCP server. This task adds the only new dependency.

**Files:**
- Create: `internal/services/whatifmcp/server.go`
- Create: `internal/services/whatifmcp/assumptions.md`
- Test: `internal/services/whatifmcp/server_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: `Source`, `ShapeAnalysis`, `RunWithOverrides`, `MonthWindow`, `Overrides`, `AnalysisView`, `MonthRow`, all from Tasks 1–4.
- Produces: `func NewServer(src *Source) *mcp.Server`. Task 6's `main.go` calls it.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/modelcontextprotocol/go-sdk@latest
go mod tidy
```

Confirm `go.mod` gained `github.com/modelcontextprotocol/go-sdk` and that `go build ./...` still passes.

- [ ] **Step 2: Write the assumptions resource**

Create `internal/services/whatifmcp/assumptions.md`:

```markdown
# Engine assumptions and known limitations

These are properties of the projection engine, not of any one scenario. A
recommendation that depends on something in this list is not supported by the
model and should say so.

- **No mortality modeling.** Both members are assumed alive for the full
  horizon. There is no survivor's penalty: no drop to single-filer brackets, no
  loss of the smaller Social Security benefit.
- **Filing status is frozen** for the whole projection.
- **Tax-deferred savings are one household pool** driven by the older member's
  age for both the RMD start year and the life-expectancy divisor. Account
  ownership is not modeled, so a household whose tax-deferred balance belongs to
  the younger spouse will see RMDs start earlier and larger than reality.
- **IRMAA is annual.** Eligibility turns on the plan anniversary rather than the
  birthday, and a mid-year Medicare start is billed for the whole year.
- **IRMAA surcharge dollars** grow at an assumed 5.5% Medicare per-capita cost
  rate; the MAGI thresholds grow at the plan's CPI assumption.
- **No QCDs, no tax-exempt municipal interest, no itemized deductions, no
  enhanced senior deduction.**
- **The reported marginal rate is the statutory bracket.** It excludes the §86
  Social Security phase-in and the IRMAA cliff, so it is not the rate a
  conversion decision actually turns on.
- **Lifetime tax figures exclude IRMAA.**
- **Monte Carlo is stochastic and auto-seeded**, so success rates differ
  slightly between two runs of the same scenario. `run_scenario` therefore omits
  Monte Carlo entirely; only `get_analysis` reports it.
```

- [ ] **Step 3: Write the failing test**

Create `internal/services/whatifmcp/server_test.go`:

```go
package whatifmcp

import (
	"strings"
	"testing"
)

func TestAssumptionsResourceIsEmbeddedAndNonEmpty(t *testing.T) {
	if len(assumptionsMD) == 0 {
		t.Fatal("assumptions.md was not embedded")
	}
	for _, want := range []string{"No mortality modeling", "one household pool", "Monte Carlo is stochastic"} {
		if !strings.Contains(assumptionsMD, want) {
			t.Errorf("assumptions resource missing %q", want)
		}
	}
}

func TestNewServerRegistersTheFourTools(t *testing.T) {
	if s := NewServer(newTestSource(t)); s == nil {
		t.Fatal("NewServer returned nil")
	}
	// Tool registration is exercised end-to-end by the smoke test in
	// cmd/whatif-mcp; this asserts construction does not panic.
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/services/whatifmcp/ -run 'TestAssumptions|TestNewServer' -v`
Expected: build failure — `undefined: assumptionsMD`, `undefined: NewServer`.

- [ ] **Step 5: Write the implementation**

Create `internal/services/whatifmcp/server.go`. Verify the SDK symbol names against the installed module before writing — the shape below matches v1.7.0:

```go
package whatifmcp

import (
	"context"
	_ "embed"
	"fmt"

	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed assumptions.md
var assumptionsMD string

type listInput struct{}

type listOutput struct {
	Scenarios []ScenarioInfo `json:"scenarios"`
}

type analysisInput struct {
	Scenario string `json:"scenario,omitempty" jsonschema:"saved scenario filename from list_scenarios; omit for the active scenario"`
}

type analysisOutput struct {
	Scenario string       `json:"scenario"`
	Analysis AnalysisView `json:"analysis"`
}

type monthsInput struct {
	Scenario  string `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active scenario"`
	FromMonth int    `json:"from_month" jsonschema:"first projection month, 0-based, inclusive"`
	ToMonth   int    `json:"to_month" jsonschema:"last projection month, inclusive; at most 120 months per call"`
}

type monthsOutput struct {
	Scenario string     `json:"scenario"`
	Months   []MonthRow `json:"months"`
}

type runInput struct {
	Scenario  string    `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active scenario"`
	Overrides Overrides `json:"overrides" jsonschema:"settings to change before running; omitted fields keep the scenario's value"`
}

type runOutput struct {
	Scenario  string       `json:"scenario"`
	Applied   Overrides    `json:"applied_overrides"`
	Analysis  AnalysisView `json:"analysis"`
	Stochastic bool        `json:"monte_carlo_omitted"`
}

// NewServer builds the MCP server. Every tool is read-only with respect to the
// data directory: scenarios are loaded and copied, never written.
func NewServer(src *Source) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "whatif", Version: "v0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_scenarios",
		Description: "List the saved what-if retirement scenarios with a one-line summary of each. " +
			"Call this first to find out which plans exist and which one is active.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (*mcp.CallToolResult, listOutput, error) {
		list, err := src.List()
		if err != nil {
			return nil, listOutput{}, err
		}
		return nil, listOutput{Scenarios: list}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_analysis",
		Description: "Get the full analysis for a saved scenario: headline balances, per-year projection, " +
			"budget fit, RMD schedule, tax totals and Monte Carlo success rate. Per-year detail only — " +
			"use get_months for month-by-month figures. Read the whatif://assumptions resource before " +
			"drawing conclusions; several real-world effects are not modeled.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in analysisInput) (*mcp.CallToolResult, analysisOutput, error) {
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, analysisOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, analysisOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})
		return nil, analysisOutput{Scenario: name, Analysis: ShapeAnalysis(a, true)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_months",
		Description: "Get month-by-month projection detail for an inclusive month range, for explaining " +
			"why a particular year behaves the way it does. At most 120 months per call.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monthsInput) (*mcp.CallToolResult, monthsOutput, error) {
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, monthsOutput{}, err
		}
		prepared, err := prepare.From(settings)
		if err != nil {
			return nil, monthsOutput{}, fmt.Errorf("prepare %s: %w", name, err)
		}
		proj := engine.New().Run(engine.Input{Prepared: prepared})
		rows, err := MonthWindow(proj, in.FromMonth, in.ToMonth)
		if err != nil {
			return nil, monthsOutput{}, err
		}
		return nil, monthsOutput{Scenario: name, Months: rows}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "run_scenario",
		Description: "Re-run a saved scenario with changed assumptions and return the resulting analysis, " +
			"without modifying the saved plan. Use this to check a claim before making it. " +
			"Monte Carlo is omitted from the result because it is stochastic and would make two " +
			"identical runs disagree; compare the deterministic figures instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, runOutput, error) {
		settings, name, err := src.Load(in.Scenario)
		if err != nil {
			return nil, runOutput{}, err
		}
		view, err := RunWithOverrides(settings, in.Overrides)
		if err != nil {
			return nil, runOutput{}, err
		}
		return nil, runOutput{Scenario: name, Applied: in.Overrides, Analysis: view, Stochastic: true}, nil
	})

	s.AddResource(&mcp.Resource{
		URI:         "whatif://assumptions",
		Name:        "Engine assumptions and limitations",
		Description: "What the projection engine does and does not model. Read before drawing conclusions from any analysis.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "whatif://assumptions",
				MIMEType: "text/markdown",
				Text:     assumptionsMD,
			}},
		}, nil
	})

	return s
}
```

If `AddResource`'s handler signature or `ResourceContents` shape differs in the installed version, adjust to match the SDK and keep the behavior: reading `whatif://assumptions` returns `assumptionsMD` as markdown.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/services/whatifmcp/server.go internal/services/whatifmcp/server_test.go internal/services/whatifmcp/assumptions.md
git commit -m "feat(whatifmcp): MCP server exposing four read-and-run tools"
```

---

### Task 6: Binary, panic recovery, and wiring

**Files:**
- Create: `cmd/whatif-mcp/main.go`
- Test: `cmd/whatif-mcp/main_test.go`
- Create: `.mcp.json`
- Modify: `README.md`

**Interfaces:**
- Consumes: `whatifmcp.NewSource`, `whatifmcp.NewServer` (Tasks 2 and 5).
- Produces: the `whatif-mcp` binary.

- [ ] **Step 1: Write the failing test**

Create `cmd/whatif-mcp/main_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestResolveDataDir_PrefersFlagOverDefault(t *testing.T) {
	if got := resolveDataDir("/tmp/custom"); got != "/tmp/custom" {
		t.Errorf("resolveDataDir(\"/tmp/custom\") = %q, want /tmp/custom", got)
	}
	if got := resolveDataDir(""); !strings.Contains(got, "data") {
		t.Errorf("default data dir = %q, want it to contain \"data\"", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/whatif-mcp/ -v`
Expected: build failure — no Go files / `undefined: resolveDataDir`.

- [ ] **Step 3: Write the implementation**

Create `cmd/whatif-mcp/main.go`:

```go
// Command whatif-mcp serves the what-if retirement planner over MCP on stdio,
// so a plan can be discussed in Claude Code. It reads saved scenarios and runs
// the projection engine; it never writes to the data directory and makes no
// network calls.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"budget2/internal/services/storage"
	"budget2/internal/services/whatifmcp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveDataDir returns the settings directory: the flag when set, otherwise
// ./data/settings relative to the working directory.
func resolveDataDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.Join("data", "settings")
}

func main() {
	dir := flag.String("data", "", "settings directory (default ./data/settings)")
	flag.Parse()

	settingsDir := resolveDataDir(*dir)
	if _, err := os.Stat(settingsDir); err != nil {
		// stdout is the MCP transport — diagnostics must go to stderr.
		log.Fatalf("settings directory %q is not readable: %v", settingsDir, err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		log.Fatalf("open storage at %q: %v", settingsDir, err)
	}

	src := whatifmcp.NewSource(settingsDir, store)
	if err := whatifmcp.NewServer(src).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("whatif-mcp: %v", err)
	}
}
```

- [ ] **Step 4: Add panic recovery at the tool boundary**

An engine panic must not kill the stdio session. Add to `internal/services/whatifmcp/server.go` and wrap each handler body:

```go
// recoverToError converts a panic into an error so a bad scenario fails one
// tool call instead of terminating the stdio session.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}
```

Apply it by giving each handler a named error return and deferring, e.g. for `run_scenario`:

```go
}, func(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (res *mcp.CallToolResult, out runOutput, err error) {
    defer recoverToError("run_scenario", &err)
    // ...unchanged body...
})
```

Do the same for `get_analysis` and `get_months`. Add a test in `server_test.go`:

```go
func TestRecoverToErrorConvertsPanic(t *testing.T) {
	var err error
	func() {
		defer recoverToError("demo", &err)
		panic("boom")
	}()
	if err == nil || !strings.Contains(err.Error(), "demo panicked") {
		t.Errorf("err = %v, want a wrapped panic", err)
	}
}
```

- [ ] **Step 5: Run the tests and build**

Run: `go build ./... && go test ./cmd/whatif-mcp/ ./internal/services/whatifmcp/ -v`
Expected: PASS.

- [ ] **Step 6: Add the client wiring**

Create `.mcp.json` at the repo root:

```json
{
  "mcpServers": {
    "whatif": {
      "command": "go",
      "args": ["run", "./cmd/whatif-mcp"]
    }
  }
}
```

Verify the key names against the current Claude Code MCP config format before committing; if `go run` startup latency is noticeable, switch to a built binary path and note the `go build -o` step in the README.

- [ ] **Step 7: Document it**

Add to `README.md`, near the existing tooling sections:

```markdown
## Talking to your plan (MCP)

`cmd/whatif-mcp` serves the what-if planner over MCP on stdio, so you can ask
questions about a plan in Claude Code — what a number means, why it moved, and
what happens under a different assumption — and have it re-run the engine to
check. It reads scenarios and runs projections; it never writes to `data/` and
makes no network calls.

The repo ships a `.mcp.json`, so Claude Code picks it up from the repo root. Four
tools: `list_scenarios`, `get_analysis`, `get_months`, `run_scenario`, plus a
`whatif://assumptions` resource describing what the engine does not model.
```

- [ ] **Step 8: Run the full gate**

Run each bare, never piped:

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
```

Expected: all green.

- [ ] **Step 9: Commit**

```bash
git add cmd/whatif-mcp .mcp.json README.md internal/services/whatifmcp/server.go internal/services/whatifmcp/server_test.go
git commit -m "feat(whatif-mcp): stdio MCP binary, panic recovery, and client wiring"
```

---

## Deferred from the spec

**Tax-optimizer output is not in `get_analysis`.** The spec listed optimizer
eligibility, baseline-vs-best, and candidate count under `get_analysis`, but
`retirement.RunFull` does not populate `WhatIfAnalysis.TaxOptimizer` — the
optimizer is a separate `retirement.RunTaxOptimizer` call, deliberately kept off
the interactive recalc path because it scores many candidates and runs Monte
Carlo over the finalists. Including a field that is always nil would be dead
code, and folding the optimizer into `get_analysis` would make every routine
call pay its cost.

The right shape is a fifth tool, `get_tax_optimizer`, that calls
`RunTaxOptimizer` explicitly — mirroring how the web app treats it as its own
endpoint rather than part of the recalc. That is deliberately **not** in this
plan: it is a self-contained addition once the four tools are working, and
bundling it here would delay a usable server. The spec has been amended so the
two documents agree.

## Manual verification

After Task 6, confirm the server actually answers a real client — the unit
tests do not exercise the transport:

1. Start a Claude Code session in the repo root.
2. Ask it to list your what-if scenarios. Expect the saved plans with the active
   one flagged.
3. Ask what your plan shows for RMDs. Expect `get_analysis`, and figures that
   match the what-if page.
4. Ask what happens if healthcare inflation is 7% instead. Expect
   `run_scenario` with `healthcare_inflation: 7`, and a changed final balance.
5. Ask what the model does *not* account for. Expect it to read
   `whatif://assumptions` and name the survivor's penalty and the single-pool
   RMD assumption.

Step 3 is the real check: if those numbers disagree with the UI for the same
scenario, the shaping or the run path is wrong — stop and fix before going on.
