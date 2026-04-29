package retirement

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// newTestSM creates a SettingsManager backed by a temp directory.
func newTestSM(t *testing.T) *SettingsManager {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return NewSettingsManager(filepath.Join(root, "settings"), store)
}

// --- Income Source CRUD ---

func TestIncomeSource_AddRemoveRestoreUpdate(t *testing.T) {
	sm := newTestSM(t)

	src := models.IncomeSource{ID: "inc1", Name: "Pension", Amount: 3000, StartMonth: 0, COLARate: 0.02}

	// Add
	s, err := sm.AddIncomeSource(src)
	if err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].ID != "inc1" {
		t.Fatalf("expected 1 income source with id inc1, got %+v", s.IncomeSources)
	}

	// Remove (moves to removed list)
	s, err = sm.RemoveIncomeSource("inc1")
	if err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}
	if len(s.IncomeSources) != 0 {
		t.Fatalf("expected 0 active income sources, got %d", len(s.IncomeSources))
	}
	if len(s.RemovedIncomeSources) != 1 || s.RemovedIncomeSources[0].ID != "inc1" {
		t.Fatalf("expected 1 removed income source, got %+v", s.RemovedIncomeSources)
	}

	// Restore
	s, err = sm.RestoreIncomeSource("inc1")
	if err != nil {
		t.Fatalf("RestoreIncomeSource: %v", err)
	}
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].ID != "inc1" {
		t.Fatalf("expected 1 active income source after restore, got %+v", s.IncomeSources)
	}
	if len(s.RemovedIncomeSources) != 0 {
		t.Fatalf("expected 0 removed after restore, got %d", len(s.RemovedIncomeSources))
	}

	// Update
	endYear := 5
	s, err = sm.UpdateIncomeSource("inc1", 2, &endYear, 0.03)
	if err != nil {
		t.Fatalf("UpdateIncomeSource: %v", err)
	}
	updated := s.IncomeSources[0]
	if updated.StartMonth != 24 { // 2 * 12
		t.Errorf("expected StartMonth 24, got %d", updated.StartMonth)
	}
	if updated.EndMonth == nil || *updated.EndMonth != 60 { // 5 * 12
		t.Errorf("expected EndMonth 60, got %v", updated.EndMonth)
	}
	if updated.COLARate != 0.03 {
		t.Errorf("expected COLARate 0.03, got %f", updated.COLARate)
	}

	// Update with nil endYear
	s, err = sm.UpdateIncomeSource("inc1", 1, nil, 0.01)
	if err != nil {
		t.Fatalf("UpdateIncomeSource nil end: %v", err)
	}
	if s.IncomeSources[0].EndMonth != nil {
		t.Errorf("expected nil EndMonth, got %v", s.IncomeSources[0].EndMonth)
	}
}

// --- Expense Source CRUD ---

func TestExpenseSource_AddRemoveRestoreUpdate(t *testing.T) {
	sm := newTestSM(t)

	src := models.ExpenseSource{ID: "exp1", Name: "Insurance", Amount: 500, StartYear: 0, Inflation: true}

	// Add
	s, err := sm.AddExpenseSource(src)
	if err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	if len(s.ExpenseSources) != 1 || s.ExpenseSources[0].ID != "exp1" {
		t.Fatalf("expected 1 expense source, got %+v", s.ExpenseSources)
	}

	// Update
	endYear := 10
	s, err = sm.UpdateExpenseSource("exp1", 2, &endYear, false, true)
	if err != nil {
		t.Fatalf("UpdateExpenseSource: %v", err)
	}
	u := s.ExpenseSources[0]
	if u.StartYear != 2 || u.EndYear != 10 || u.Inflation != false || u.Discretionary != true {
		t.Errorf("unexpected update result: %+v", u)
	}

	// Update with nil endYear sets EndYear to 0
	s, err = sm.UpdateExpenseSource("exp1", 1, nil, true, false)
	if err != nil {
		t.Fatalf("UpdateExpenseSource nil end: %v", err)
	}
	if s.ExpenseSources[0].EndYear != 0 {
		t.Errorf("expected EndYear 0 for perpetual, got %d", s.ExpenseSources[0].EndYear)
	}

	// Remove
	s, err = sm.RemoveExpenseSource("exp1")
	if err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}
	if len(s.ExpenseSources) != 0 {
		t.Fatalf("expected 0 active expense sources, got %d", len(s.ExpenseSources))
	}
	if len(s.RemovedExpenseSources) != 1 {
		t.Fatalf("expected 1 removed expense source, got %d", len(s.RemovedExpenseSources))
	}

	// Restore
	s, err = sm.RestoreExpenseSource("exp1")
	if err != nil {
		t.Fatalf("RestoreExpenseSource: %v", err)
	}
	if len(s.ExpenseSources) != 1 {
		t.Fatalf("expected 1 active expense source after restore, got %d", len(s.ExpenseSources))
	}
	if len(s.RemovedExpenseSources) != 0 {
		t.Fatalf("expected 0 removed after restore, got %d", len(s.RemovedExpenseSources))
	}
}

// --- Spending Phases ---

func TestUpdateSpendingPhases(t *testing.T) {
	sm := newTestSM(t)

	// Enable with default phases (nil SpendingPhaseConfig initially)
	s, err := sm.UpdateSpendingPhases(true, nil)
	if err != nil {
		t.Fatalf("UpdateSpendingPhases: %v", err)
	}
	if s.SpendingPhaseConfig == nil {
		t.Fatal("expected SpendingPhaseConfig to be initialized")
	}
	if !s.SpendingPhaseConfig.Enabled {
		t.Error("expected enabled=true")
	}
	if len(s.SpendingPhaseConfig.Phases) == 0 {
		t.Error("expected default phases to be populated")
	}

	// Update with custom phases
	custom := []models.SpendingPhase{
		{Name: "Active", StartAge: 0, Multiplier: 1.0},
		{Name: "Slow", StartAge: 75, Multiplier: 0.8},
	}
	s, err = sm.UpdateSpendingPhases(false, custom)
	if err != nil {
		t.Fatalf("UpdateSpendingPhases custom: %v", err)
	}
	if s.SpendingPhaseConfig.Enabled {
		t.Error("expected enabled=false")
	}
	if len(s.SpendingPhaseConfig.Phases) != 2 {
		t.Errorf("expected 2 phases, got %d", len(s.SpendingPhaseConfig.Phases))
	}
}

// --- Healthcare Person CRUD ---

func TestHealthcarePerson_AddUpdateRemove(t *testing.T) {
	sm := newTestSM(t)

	person := models.HealthcarePerson{
		ID:                 "hp1",
		Name:               "Alice",
		CurrentAge:         60,
		CurrentCoverage:    models.CoverageEmployer,
		CurrentMonthlyCost: 400,
	}

	// Add
	s, err := sm.AddHealthcarePerson(person)
	if err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}
	if len(s.HealthcarePersons) != 1 || s.HealthcarePersons[0].ID != "hp1" {
		t.Fatalf("expected 1 healthcare person, got %+v", s.HealthcarePersons)
	}

	// Update
	updates := map[string]interface{}{
		"name":                    "Alice Smith",
		"current_age":             int(62),
		"current_coverage":        "aca",
		"current_monthly_cost":    float64(600),
		"pre_medicare_inflation":  float64(7.0),
		"medicare_monthly_cost":   float64(200),
		"post_medicare_inflation": float64(4.0),
		"employer_coverage_years": int(3),
		"aca_cost_after_employer": float64(800),
	}
	s, err = sm.UpdateHealthcarePerson("hp1", updates)
	if err != nil {
		t.Fatalf("UpdateHealthcarePerson: %v", err)
	}
	hp := s.HealthcarePersons[0]
	if hp.Name != "Alice Smith" {
		t.Errorf("expected name 'Alice Smith', got %q", hp.Name)
	}
	if hp.CurrentAge != 62 {
		t.Errorf("expected age 62, got %d", hp.CurrentAge)
	}
	if hp.CurrentCoverage != models.CoverageACA {
		t.Errorf("expected ACA coverage, got %v", hp.CurrentCoverage)
	}
	if hp.CurrentMonthlyCost != 600 {
		t.Errorf("expected cost 600, got %f", hp.CurrentMonthlyCost)
	}
	if hp.PreMedicareInflation != 7.0 {
		t.Errorf("expected pre-medicare inflation 7.0, got %f", hp.PreMedicareInflation)
	}
	if hp.MedicareMonthlyCost != 200 {
		t.Errorf("expected medicare cost 200, got %f", hp.MedicareMonthlyCost)
	}
	if hp.PostMedicareInflation != 4.0 {
		t.Errorf("expected post-medicare inflation 4.0, got %f", hp.PostMedicareInflation)
	}
	if hp.EmployerCoverageYears != 3 {
		t.Errorf("expected employer years 3, got %d", hp.EmployerCoverageYears)
	}
	if hp.ACACostAfterEmployer != 800 {
		t.Errorf("expected ACA cost 800, got %f", hp.ACACostAfterEmployer)
	}

	// Remove (hard delete, no removed list)
	s, err = sm.RemoveHealthcarePerson("hp1")
	if err != nil {
		t.Fatalf("RemoveHealthcarePerson: %v", err)
	}
	if len(s.HealthcarePersons) != 0 {
		t.Fatalf("expected 0 healthcare persons, got %d", len(s.HealthcarePersons))
	}
}

// --- Big Ticket Item CRUD ---

func TestBigTicketItem_AddRemoveRestore(t *testing.T) {
	sm := newTestSM(t)

	item := models.BigTicketItem{
		ID:     "bt1",
		Name:   "New Roof",
		Amount: 25000,
		Year:   3,
		Type:   models.BigTicketExpense,
	}

	// Add
	s, err := sm.AddBigTicketItem(item)
	if err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	if len(s.BigTicketItems) != 1 || s.BigTicketItems[0].ID != "bt1" {
		t.Fatalf("expected 1 big ticket item, got %+v", s.BigTicketItems)
	}

	// Remove
	s, err = sm.RemoveBigTicketItem("bt1")
	if err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}
	if len(s.BigTicketItems) != 0 {
		t.Fatalf("expected 0 active items, got %d", len(s.BigTicketItems))
	}
	if len(s.RemovedBigTicketItems) != 1 || s.RemovedBigTicketItems[0].ID != "bt1" {
		t.Fatalf("expected 1 removed item, got %+v", s.RemovedBigTicketItems)
	}

	// Restore
	s, err = sm.RestoreBigTicketItem("bt1")
	if err != nil {
		t.Fatalf("RestoreBigTicketItem: %v", err)
	}
	if len(s.BigTicketItems) != 1 {
		t.Fatalf("expected 1 active item after restore, got %d", len(s.BigTicketItems))
	}
	if len(s.RemovedBigTicketItems) != 0 {
		t.Fatalf("expected 0 removed after restore, got %d", len(s.RemovedBigTicketItems))
	}
}

// --- Error paths (save failures) ---

func TestCRUD_SaveErrors(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Seed data so remove/restore/update have something to work with
	src := models.IncomeSource{ID: "i1", Name: "Job", Amount: 1000}
	if _, err := sm.AddIncomeSource(src); err != nil {
		t.Fatalf("seed AddIncomeSource: %v", err)
	}
	exp := models.ExpenseSource{ID: "e1", Name: "Rent", Amount: 500}
	if _, err := sm.AddExpenseSource(exp); err != nil {
		t.Fatalf("seed AddExpenseSource: %v", err)
	}
	hp := models.HealthcarePerson{ID: "h1", Name: "Bob", CurrentAge: 55}
	if _, err := sm.AddHealthcarePerson(hp); err != nil {
		t.Fatalf("seed AddHealthcarePerson: %v", err)
	}
	bt := models.BigTicketItem{ID: "b1", Name: "Car", Amount: 30000, Type: models.BigTicketExpense}
	if _, err := sm.AddBigTicketItem(bt); err != nil {
		t.Fatalf("seed AddBigTicketItem: %v", err)
	}

	// Make settings dir read-only to trigger save errors
	if err := os.Chmod(settingsDir, 0555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(settingsDir, 0755) //nolint: make writable again for cleanup

	// Each method should return an error from saveInternal
	if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i2"}); err == nil {
		t.Error("AddIncomeSource should fail on read-only dir")
	}
	if _, err := sm.RemoveIncomeSource("i1"); err == nil {
		t.Error("RemoveIncomeSource should fail on read-only dir")
	}
	if _, err := sm.RestoreIncomeSource("i1"); err == nil {
		t.Error("RestoreIncomeSource should fail on read-only dir")
	}
	if _, err := sm.UpdateIncomeSource("i1", 1, nil, 0.01); err == nil {
		t.Error("UpdateIncomeSource should fail on read-only dir")
	}
	if _, err := sm.AddExpenseSource(models.ExpenseSource{ID: "e2"}); err == nil {
		t.Error("AddExpenseSource should fail on read-only dir")
	}
	if _, err := sm.UpdateExpenseSource("e1", 1, nil, true, false); err == nil {
		t.Error("UpdateExpenseSource should fail on read-only dir")
	}
	if _, err := sm.RemoveExpenseSource("e1"); err == nil {
		t.Error("RemoveExpenseSource should fail on read-only dir")
	}
	if _, err := sm.RestoreExpenseSource("e1"); err == nil {
		t.Error("RestoreExpenseSource should fail on read-only dir")
	}
	if _, err := sm.UpdateSpendingPhases(true, nil); err == nil {
		t.Error("UpdateSpendingPhases should fail on read-only dir")
	}
	if _, err := sm.AddHealthcarePerson(models.HealthcarePerson{ID: "h2"}); err == nil {
		t.Error("AddHealthcarePerson should fail on read-only dir")
	}
	if _, err := sm.UpdateHealthcarePerson("h1", map[string]interface{}{"name": "X"}); err == nil {
		t.Error("UpdateHealthcarePerson should fail on read-only dir")
	}
	if _, err := sm.RemoveHealthcarePerson("h1"); err == nil {
		t.Error("RemoveHealthcarePerson should fail on read-only dir")
	}
	if _, err := sm.AddBigTicketItem(models.BigTicketItem{ID: "b2"}); err == nil {
		t.Error("AddBigTicketItem should fail on read-only dir")
	}
	if _, err := sm.RemoveBigTicketItem("b1"); err == nil {
		t.Error("RemoveBigTicketItem should fail on read-only dir")
	}
	if _, err := sm.RestoreBigTicketItem("b1"); err == nil {
		t.Error("RestoreBigTicketItem should fail on read-only dir")
	}
	if _, err := sm.CreateScenario("Fail Plan"); err == nil {
		t.Error("CreateScenario should fail on read-only dir")
	}
}

// --- Load error paths ---

func TestCRUD_LoadErrors(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	// Create settings dir and write invalid JSON
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), []byte("{invalid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)

	if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i1"}); err == nil {
		t.Error("AddIncomeSource should fail on bad JSON")
	}
	if _, err := sm.RemoveIncomeSource("i1"); err == nil {
		t.Error("RemoveIncomeSource should fail on bad JSON")
	}
	if _, err := sm.RestoreIncomeSource("i1"); err == nil {
		t.Error("RestoreIncomeSource should fail on bad JSON")
	}
	if _, err := sm.UpdateIncomeSource("i1", 1, nil, 0.01); err == nil {
		t.Error("UpdateIncomeSource should fail on bad JSON")
	}
	if _, err := sm.AddExpenseSource(models.ExpenseSource{ID: "e1"}); err == nil {
		t.Error("AddExpenseSource should fail on bad JSON")
	}
	if _, err := sm.UpdateExpenseSource("e1", 1, nil, true, false); err == nil {
		t.Error("UpdateExpenseSource should fail on bad JSON")
	}
	if _, err := sm.RemoveExpenseSource("e1"); err == nil {
		t.Error("RemoveExpenseSource should fail on bad JSON")
	}
	if _, err := sm.RestoreExpenseSource("e1"); err == nil {
		t.Error("RestoreExpenseSource should fail on bad JSON")
	}
	if _, err := sm.UpdateSpendingPhases(true, nil); err == nil {
		t.Error("UpdateSpendingPhases should fail on bad JSON")
	}
	if _, err := sm.AddHealthcarePerson(models.HealthcarePerson{ID: "h1"}); err == nil {
		t.Error("AddHealthcarePerson should fail on bad JSON")
	}
	if _, err := sm.UpdateHealthcarePerson("h1", map[string]interface{}{}); err == nil {
		t.Error("UpdateHealthcarePerson should fail on bad JSON")
	}
	if _, err := sm.RemoveHealthcarePerson("h1"); err == nil {
		t.Error("RemoveHealthcarePerson should fail on bad JSON")
	}
	if _, err := sm.AddBigTicketItem(models.BigTicketItem{ID: "b1"}); err == nil {
		t.Error("AddBigTicketItem should fail on bad JSON")
	}
	if _, err := sm.RemoveBigTicketItem("b1"); err == nil {
		t.Error("RemoveBigTicketItem should fail on bad JSON")
	}
	if _, err := sm.RestoreBigTicketItem("b1"); err == nil {
		t.Error("RestoreBigTicketItem should fail on bad JSON")
	}
	if _, err := sm.CreateScenario("Bad"); err == nil {
		t.Error("CreateScenario should fail on bad JSON")
	}
}

// --- slugify ---

func TestSlugify(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"My Scenario", "my-scenario"},
		{"Hello  World", "hello-world"},
		{"special!@#chars", "specialchars"},
		{"---leading-trailing---", "leading-trailing"},
		{"under_score", "under-score"},
		{"MiXeD CaSe 123", "mixed-case-123"},
		{"", "scenario"},
		{"!!!!", "scenario"},
		{"already-slugged", "already-slugged"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- readScenarioName ---

func TestReadScenarioName(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// File with scenario_name
	data, _ := json.Marshal(map[string]string{"scenario_name": "My Plan"})
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_myplan.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got := sm.readScenarioName("whatif_myplan.json")
	if got != "My Plan" {
		t.Errorf("readScenarioName = %q, want %q", got, "My Plan")
	}

	// File with empty scenario_name falls back to filename
	data, _ = json.Marshal(map[string]string{"scenario_name": ""})
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_empty.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got = sm.readScenarioName("whatif_empty.json")
	if got != "whatif_empty.json" {
		t.Errorf("readScenarioName empty = %q, want filename", got)
	}

	// File with whitespace-only scenario_name also falls back to filename
	data, _ = json.Marshal(map[string]string{"scenario_name": "   "})
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif_blankish.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got = sm.readScenarioName("whatif_blankish.json")
	if got != "whatif_blankish.json" {
		t.Errorf("readScenarioName whitespace = %q, want filename", got)
	}

	// Non-existent file falls back to filename
	got = sm.readScenarioName("whatif_nope.json")
	if got != "whatif_nope.json" {
		t.Errorf("readScenarioName missing = %q, want filename", got)
	}

	// Invalid filename (path traversal) falls back to filename
	got = sm.readScenarioName("../bad.json")
	if got != "../bad.json" {
		t.Errorf("readScenarioName invalid = %q, want filename", got)
	}
}

// --- ActiveScenario ---

func TestActiveScenario(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Default scenario
	name := sm.ActiveScenario()
	if name != "Current Plan" {
		t.Errorf("ActiveScenario default = %q, want 'Current Plan'", name)
	}

	// Create a custom scenario and check ActiveScenario reads the name
	if _, err := sm.CreateScenario("My Custom Plan"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	name = sm.ActiveScenario()
	if name != "My Custom Plan" {
		t.Errorf("ActiveScenario custom = %q, want 'My Custom Plan'", name)
	}
}

// --- CreateScenario ---

func TestCreateScenario(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(settingsDir, store)

	// Initialize with some data
	src := models.IncomeSource{ID: "inc1", Name: "Job", Amount: 5000}
	if _, err := sm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}

	// Create scenario
	s, err := sm.CreateScenario("Early Retirement")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	if s.ScenarioName != "Early Retirement" {
		t.Errorf("expected scenario name 'Early Retirement', got %q", s.ScenarioName)
	}

	// Should have switched to new file
	if sm.ActiveFilename() != "whatif_early-retirement.json" {
		t.Errorf("expected filename 'whatif_early-retirement.json', got %q", sm.ActiveFilename())
	}

	// Settings should carry over
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].ID != "inc1" {
		t.Errorf("expected income sources to carry over, got %+v", s.IncomeSources)
	}

	// Verify file exists on disk
	path := filepath.Join(settingsDir, "whatif_early-retirement.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("scenario file should exist: %v", err)
	}

	// Create duplicate name should get unique filename
	s2, err := sm.CreateScenario("Early Retirement")
	if err != nil {
		t.Fatalf("CreateScenario duplicate: %v", err)
	}
	if sm.ActiveFilename() == "whatif_early-retirement.json" {
		t.Error("expected unique filename for duplicate scenario name")
	}
	if s2.ScenarioName != "Early Retirement" {
		t.Errorf("expected scenario name 'Early Retirement', got %q", s2.ScenarioName)
	}
}

func TestCreateScenario_TrimmedName(t *testing.T) {
	sm := newTestSM(t)

	s, err := sm.CreateScenario("  Early Retirement  ")
	if err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	if s.ScenarioName != "Early Retirement" {
		t.Fatalf("ScenarioName = %q, want %q", s.ScenarioName, "Early Retirement")
	}
	if sm.ActiveFilename() != "whatif_early-retirement.json" {
		t.Fatalf("ActiveFilename = %q, want %q", sm.ActiveFilename(), "whatif_early-retirement.json")
	}
}

func TestCreateScenario_WhitespaceName(t *testing.T) {
	sm := newTestSM(t)

	if _, err := sm.CreateScenario("   "); err == nil {
		t.Fatal("expected error for whitespace-only scenario name")
	}
}

// --- ListScenarios ---

func TestListScenarios(t *testing.T) {
	t.Run("multiple scenarios including non-default", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Create default scenario with some data
		if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i1", Name: "Job", Amount: 5000}); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}

		// Create two additional scenarios
		if _, err := sm.CreateScenario("Early Retirement"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		// Switch back to default so we can create another
		if err := sm.SwitchScenario("whatif.json"); err != nil {
			t.Fatalf("SwitchScenario: %v", err)
		}
		if _, err := sm.CreateScenario("Late Retirement"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}

		// Switch back to default for listing
		if err := sm.SwitchScenario("whatif.json"); err != nil {
			t.Fatalf("SwitchScenario: %v", err)
		}

		scenarios, err := sm.ListScenarios()
		if err != nil {
			t.Fatalf("ListScenarios: %v", err)
		}

		if len(scenarios) != 3 {
			t.Fatalf("expected 3 scenarios, got %d", len(scenarios))
		}

		// First should always be "Current Plan" (whatif.json)
		if scenarios[0].Filename != "whatif.json" || scenarios[0].Name != "Current Plan" {
			t.Errorf("first scenario should be Current Plan, got %+v", scenarios[0])
		}
		if !scenarios[0].Active {
			t.Error("default scenario should be active")
		}

		// Non-default scenarios should have their names read from the file
		foundEarly := false
		foundLate := false
		for _, s := range scenarios[1:] {
			if s.Name == "Early Retirement" {
				foundEarly = true
				if s.Active {
					t.Error("Early Retirement should not be active")
				}
			}
			if s.Name == "Late Retirement" {
				foundLate = true
			}
		}
		if !foundEarly {
			t.Error("expected to find Early Retirement scenario")
		}
		if !foundLate {
			t.Error("expected to find Late Retirement scenario")
		}
	})

	t.Run("default whatif.json missing", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Create only a non-default scenario file manually
		if err := store.MkdirAll(settingsDir, 0755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		settings := models.DefaultWhatIfSettings()
		settings.ScenarioName = "Custom Plan"
		data, _ := json.MarshalIndent(settings, "", "  ")
		if err := store.WriteFile(filepath.Join(settingsDir, "whatif_custom.json"), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		scenarios, err := sm.ListScenarios()
		if err != nil {
			t.Fatalf("ListScenarios: %v", err)
		}

		// Should still include the default scenario even though the file is missing
		if len(scenarios) != 2 {
			t.Fatalf("expected 2 scenarios (default + custom), got %d", len(scenarios))
		}
		if scenarios[0].Filename != "whatif.json" {
			t.Errorf("first scenario should be whatif.json, got %s", scenarios[0].Filename)
		}
		if scenarios[0].Name != "Current Plan" {
			t.Errorf("default scenario name should be 'Current Plan', got %q", scenarios[0].Name)
		}
		if !scenarios[0].Active {
			t.Error("default scenario should be marked active")
		}
	})
}

// --- SwitchScenario ---

func TestSwitchScenario(t *testing.T) {
	t.Run("switch to valid file", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Save default settings so whatif.json exists on disk
		if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i1", Name: "Job", Amount: 5000}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		// Create a scenario to switch to
		if _, err := sm.CreateScenario("Test Plan"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		createdFile := sm.ActiveFilename()

		// Switch back to default
		if err := sm.SwitchScenario("whatif.json"); err != nil {
			t.Fatalf("SwitchScenario to default: %v", err)
		}
		if sm.ActiveFilename() != "whatif.json" {
			t.Errorf("expected whatif.json, got %s", sm.ActiveFilename())
		}

		// Switch to created scenario
		if err := sm.SwitchScenario(createdFile); err != nil {
			t.Fatalf("SwitchScenario to created: %v", err)
		}
		if sm.ActiveFilename() != createdFile {
			t.Errorf("expected %s, got %s", createdFile, sm.ActiveFilename())
		}
	})

	t.Run("invalid filename", func(t *testing.T) {
		sm := newTestSM(t)

		if err := sm.SwitchScenario("bad_name.txt"); err == nil {
			t.Error("expected error for invalid filename")
		}
		if err := sm.SwitchScenario("../whatif.json"); err == nil {
			t.Error("expected error for path traversal")
		}
		if err := sm.SwitchScenario(""); err == nil {
			t.Error("expected error for empty filename")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		sm := newTestSM(t)

		err := sm.SwitchScenario("whatif_nonexistent.json")
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})
}

// --- DeleteScenario ---

func TestDeleteScenario(t *testing.T) {
	t.Run("delete non-default scenario", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Ensure default file exists on disk
		if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i1", Name: "Job", Amount: 5000}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		// Create a scenario
		if _, err := sm.CreateScenario("To Delete"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()

		// Switch back to default before deleting
		if err := sm.SwitchScenario("whatif.json"); err != nil {
			t.Fatalf("SwitchScenario: %v", err)
		}

		// Delete the scenario
		if err := sm.DeleteScenario(filename); err != nil {
			t.Fatalf("DeleteScenario: %v", err)
		}

		// Verify file no longer exists
		path := filepath.Join(settingsDir, filename)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("expected scenario file to be deleted")
		}
	})

	t.Run("cannot delete default scenario", func(t *testing.T) {
		sm := newTestSM(t)

		err := sm.DeleteScenario("whatif.json")
		if err == nil {
			t.Error("expected error when deleting default scenario")
		}
	})

	t.Run("delete active scenario switches to default", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Create and stay on a scenario
		if _, err := sm.CreateScenario("Active Scenario"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()
		if filename == "whatif.json" {
			t.Fatal("expected to be on a non-default scenario")
		}

		// Delete the active scenario
		if err := sm.DeleteScenario(filename); err != nil {
			t.Fatalf("DeleteScenario: %v", err)
		}

		// Should have switched back to default
		if sm.ActiveFilename() != "whatif.json" {
			t.Errorf("expected to switch back to whatif.json, got %s", sm.ActiveFilename())
		}
	})
}

// --- RenameScenario ---

func TestRenameScenario(t *testing.T) {
	t.Run("rename non-default scenario", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Create a scenario
		if _, err := sm.CreateScenario("Original Name"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()

		// Rename it
		if err := sm.RenameScenario(filename, "New Name"); err != nil {
			t.Fatalf("RenameScenario: %v", err)
		}

		// Verify the name was updated by reading it back
		name := sm.readScenarioName(filename)
		if name != "New Name" {
			t.Errorf("expected scenario name 'New Name', got %q", name)
		}
	})

	t.Run("cannot rename default scenario", func(t *testing.T) {
		sm := newTestSM(t)

		err := sm.RenameScenario("whatif.json", "Something Else")
		if err == nil {
			t.Error("expected error when renaming default scenario")
		}
	})

	t.Run("rename active scenario invalidates cache", func(t *testing.T) {
		root := t.TempDir()
		settingsDir := filepath.Join(root, "settings")
		store, err := storage.New(root)
		if err != nil {
			t.Fatalf("storage.New: %v", err)
		}
		sm := NewSettingsManager(settingsDir, store)

		// Create and stay on a scenario
		if _, err := sm.CreateScenario("Before Rename"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()

		// Load to populate cache
		s1, err := sm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s1.ScenarioName != "Before Rename" {
			t.Fatalf("expected 'Before Rename', got %q", s1.ScenarioName)
		}

		// Rename the active scenario
		if err := sm.RenameScenario(filename, "After Rename"); err != nil {
			t.Fatalf("RenameScenario: %v", err)
		}

		// Load again - should get the new name (cache was invalidated)
		s2, err := sm.Load()
		if err != nil {
			t.Fatalf("Load after rename: %v", err)
		}
		if s2.ScenarioName != "After Rename" {
			t.Errorf("expected 'After Rename', got %q", s2.ScenarioName)
		}
	})

	t.Run("trim whitespace around new name", func(t *testing.T) {
		sm := newTestSM(t)

		if _, err := sm.CreateScenario("Before Rename"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()

		if err := sm.RenameScenario(filename, "  After Rename  "); err != nil {
			t.Fatalf("RenameScenario: %v", err)
		}

		name := sm.readScenarioName(filename)
		if name != "After Rename" {
			t.Fatalf("readScenarioName = %q, want %q", name, "After Rename")
		}
	})

	t.Run("reject whitespace-only name", func(t *testing.T) {
		sm := newTestSM(t)

		if _, err := sm.CreateScenario("Before Rename"); err != nil {
			t.Fatalf("CreateScenario: %v", err)
		}
		filename := sm.ActiveFilename()

		if err := sm.RenameScenario(filename, "   "); err == nil {
			t.Fatal("expected error for whitespace-only scenario name")
		}
	})
}

// --- UpdateSettings ---

func TestUpdateSettings_FieldTypes(t *testing.T) {
	sm := newTestSM(t)

	updates := map[string]interface{}{
		// int fields
		"current_age":              int(55),
		"projection_years":         int(40),
		"healthcare_start_years":   int(2),
		"tax_deferred_delay_years": int(5),
		"spouse_age":               int(52),

		// string fields
		"phase_age_reference": "older",

		// float64 fields (per-account allocations)
		"tax_deferred_stock_percent": float64(70.0),
		"tax_deferred_cash_percent":  float64(5.0),
		"roth_stock_percent":         float64(80.0),
		"roth_cash_percent":          float64(3.0),
		"taxable_stock_percent":      float64(50.0),
		"taxable_cash_percent":       float64(10.0),

		// Other float64 fields
		"portfolio_value":            float64(1000000),
		"monthly_living_expenses":    float64(5000),
		"investment_return":          float64(7.5),
		"discount_rate":              float64(4.0),
		"inflation_rate":             float64(2.5),
		"spending_decline_rate":      float64(1.5),
		"steady_state_override_year": float64(15),
	}

	s, err := sm.UpdateSettings(updates)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	// Verify int fields
	if s.CurrentAge != 55 {
		t.Errorf("CurrentAge: expected 55, got %d", s.CurrentAge)
	}
	if s.ProjectionYears != 40 {
		t.Errorf("ProjectionYears: expected 40, got %d", s.ProjectionYears)
	}
	if s.HealthcareStartYears != 2 {
		t.Errorf("HealthcareStartYears: expected 2, got %d", s.HealthcareStartYears)
	}
	if s.TaxDeferredDelayYears != 5 {
		t.Errorf("TaxDeferredDelayYears: expected 5, got %d", s.TaxDeferredDelayYears)
	}
	if s.SpouseAge != 52 {
		t.Errorf("SpouseAge: expected 52, got %d", s.SpouseAge)
	}

	// Verify string fields
	if s.PhaseAgeReference != "older" {
		t.Errorf("PhaseAgeReference: expected 'older', got %q", s.PhaseAgeReference)
	}

	// Verify per-account allocation fields
	if s.TaxDeferredStockPercent != 70.0 {
		t.Errorf("TaxDeferredStockPercent: expected 70.0, got %f", s.TaxDeferredStockPercent)
	}
	if s.TaxDeferredCashPercent != 5.0 {
		t.Errorf("TaxDeferredCashPercent: expected 5.0, got %f", s.TaxDeferredCashPercent)
	}
	if s.RothStockPercent != 80.0 {
		t.Errorf("RothStockPercent: expected 80.0, got %f", s.RothStockPercent)
	}
	if s.RothCashPercent != 3.0 {
		t.Errorf("RothCashPercent: expected 3.0, got %f", s.RothCashPercent)
	}
	if s.TaxableStockPercent != 50.0 {
		t.Errorf("TaxableStockPercent: expected 50.0, got %f", s.TaxableStockPercent)
	}
	if s.TaxableCashPercent != 10.0 {
		t.Errorf("TaxableCashPercent: expected 10.0, got %f", s.TaxableCashPercent)
	}

	// Verify other float64 fields
	if s.PortfolioValue != 1000000 {
		t.Errorf("PortfolioValue: expected 1000000, got %f", s.PortfolioValue)
	}
	if s.MonthlyLivingExpenses != 5000 {
		t.Errorf("MonthlyLivingExpenses: expected 5000, got %f", s.MonthlyLivingExpenses)
	}
	if s.InvestmentReturn != 7.5 {
		t.Errorf("InvestmentReturn: expected 7.5, got %f", s.InvestmentReturn)
	}
	if s.DiscountRate != 4.0 {
		t.Errorf("DiscountRate: expected 4.0, got %f", s.DiscountRate)
	}
	if s.InflationRate != 2.5 {
		t.Errorf("InflationRate: expected 2.5, got %f", s.InflationRate)
	}
	if s.SpendingDeclineRate != 1.5 {
		t.Errorf("SpendingDeclineRate: expected 1.5, got %f", s.SpendingDeclineRate)
	}
	if s.SteadyStateOverrideYear != 15 {
		t.Errorf("SteadyStateOverrideYear: expected 15, got %f", s.SteadyStateOverrideYear)
	}
}

// --- scenarioPath ---

func TestScenarioPath(t *testing.T) {
	sm := newTestSM(t)

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{"empty filename", "", true},
		{"path traversal with dots", "../whatif.json", true},
		{"path with forward slash", "sub/whatif.json", true},
		{"path with backslash", `sub\whatif.json`, true},
		{"non-whatif prefix", "settings.json", true},
		{"non-json suffix", "whatif.txt", true},
		{"valid default", "whatif.json", false},
		{"valid scenario", "whatif_test.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sm.scenarioPath(tt.filename)
			if tt.wantErr && err == nil {
				t.Errorf("scenarioPath(%q) expected error, got nil", tt.filename)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("scenarioPath(%q) unexpected error: %v", tt.filename, err)
			}
		})
	}
}

// --- loadInternal migration ---

func TestLoadInternal_MigrationOldFormat(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write old-format settings: has monthly_healthcare and current_age but no healthcare_persons
	oldSettings := map[string]interface{}{
		"portfolio_value":         500000,
		"monthly_living_expenses": 4000,
		"monthly_healthcare":      800,
		"healthcare_inflation":    6.0,
		"current_age":             60,
		"tax_deferred_percent":    60,
		"roth_percent":            10,
		"stock_percent":           60,
		"inflation_rate":          3.0,
		"spending_decline_rate":   1.0,
		"discount_rate":           5.0,
		"projection_years":        30,
		"income_sources":          []interface{}{},
		"expense_sources":         []interface{}{},
	}
	data, _ := json.MarshalIndent(oldSettings, "", "  ")
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)
	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Migration should create a healthcare person from legacy values
	if len(s.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 migrated healthcare person, got %d", len(s.HealthcarePersons))
	}
	hp := s.HealthcarePersons[0]
	if hp.ID != "migrated-user" {
		t.Errorf("expected ID 'migrated-user', got %q", hp.ID)
	}
	if hp.CurrentMonthlyCost != 800 {
		t.Errorf("expected cost 800, got %f", hp.CurrentMonthlyCost)
	}
	// Age 60 < 65, so coverage should be ACA
	if hp.CurrentCoverage != models.CoverageACA {
		t.Errorf("expected ACA coverage for age 60, got %v", hp.CurrentCoverage)
	}

	// SpendingPhaseConfig should be initialized (migration)
	if s.SpendingPhaseConfig == nil {
		t.Fatal("expected SpendingPhaseConfig to be initialized via migration")
	}
	if s.SpendingPhaseConfig.Enabled {
		t.Error("expected spending phases to default to disabled for migrated settings")
	}
}

func TestLoadInternal_QualifiedDividendMigration(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	t.Run("legacy file without taxable fields defaults qualified to 100", func(t *testing.T) {
		legacySettings := map[string]interface{}{
			"portfolio_value":         500000,
			"monthly_living_expenses": 4000,
			"current_age":             65,
			"projection_years":        30,
			"income_sources":          []interface{}{},
			"expense_sources":         []interface{}{},
		}
		data, _ := json.MarshalIndent(legacySettings, "", "  ")
		if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		sm := NewSettingsManager(settingsDir, store)
		s, err := sm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.TaxableQualifiedDividendPercent != 100 {
			t.Fatalf("expected migration to set qualified to 100, got %.1f", s.TaxableQualifiedDividendPercent)
		}
	})

	t.Run("explicit zero qualified percent is preserved when taxable fields present", func(t *testing.T) {
		explicitSettings := map[string]interface{}{
			"portfolio_value":                    500000,
			"monthly_living_expenses":            4000,
			"current_age":                        65,
			"projection_years":                   30,
			"taxable_dividend_yield":             2.0,
			"taxable_qualified_dividend_percent": 0,
			"income_sources":                     []interface{}{},
			"expense_sources":                    []interface{}{},
		}
		data, _ := json.MarshalIndent(explicitSettings, "", "  ")
		if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		sm := NewSettingsManager(settingsDir, store)
		s, err := sm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.TaxableQualifiedDividendPercent != 0 {
			t.Fatalf("expected explicit 0%% qualified to be preserved, got %.1f", s.TaxableQualifiedDividendPercent)
		}
	})
}

// --- Load concurrent ---

func TestLoad_Concurrent(t *testing.T) {
	sm := newTestSM(t)

	// Seed some data
	if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "i1", Name: "Job", Amount: 5000}); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}

	// Clear cache to force re-reads
	sm.mu.Lock()
	sm.cache = nil
	sm.mu.Unlock()

	// Concurrent loads should not race
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := sm.Load()
			if err != nil {
				errs <- err
				return
			}
			if len(s.IncomeSources) != 1 {
				errs <- fmt.Errorf("expected 1 income source, got %d", len(s.IncomeSources))
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Load error: %v", err)
	}
}

// --- ValidateScenarioChain ---

// writeScenarioFile is a test helper that writes a minimal scenario JSON file.
func writeScenarioFile(t *testing.T, sm *SettingsManager, store *storage.Storage, dir, filename string) {
	t.Helper()
	_ = store.MkdirAll(dir, 0755)
	settings := models.DefaultWhatIfSettings()
	settings.ScenarioName = filename
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	if err := store.WriteFile(filepath.Join(dir, filename), data, 0644); err != nil {
		t.Fatalf("write scenario %s: %v", filename, err)
	}
}

func TestValidateScenarioChain_AscendingAges(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_a.json")

	settings := models.DefaultWhatIfSettings() // CurrentAge=65, ProjectionYears=30

	// Non-ascending: second age same as first
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70},
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70}, // duplicate age
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Error("expected error for non-ascending transition ages")
	}

	// Descending ages
	writeScenarioFile(t, sm, store, dir, "whatif_b.json")
	chain2 := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_b.json", TransitionAge: 80},
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70},
	}
	if err := sm.ValidateScenarioChain(chain2, settings, "whatif.json"); err == nil {
		t.Error("expected error for descending transition ages")
	}
}

func TestValidateScenarioChain_SelfReference(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_self.json")

	settings := models.DefaultWhatIfSettings()
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_self.json", TransitionAge: 70},
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif_self.json"); err == nil {
		t.Error("expected error for self-reference in chain")
	}
}

func TestValidateScenarioChain_AgeBelowCurrent(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_a.json")

	settings := models.DefaultWhatIfSettings() // CurrentAge=65
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 64}, // below CurrentAge
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Error("expected error for transition age below current age")
	}
}

func TestValidateScenarioChain_AgeBeyondProjection(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_a.json")

	settings := models.DefaultWhatIfSettings() // CurrentAge=65, ProjectionYears=30 → max valid age = 94
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 95}, // >= 65+30=95
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Error("expected error for transition age beyond projection end")
	}
}

func TestValidateScenarioChain_MissingFile(t *testing.T) {
	sm, _, _ := newTestSMWithDir(t)

	settings := models.DefaultWhatIfSettings()
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_missing.json", TransitionAge: 70},
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Error("expected error for missing scenario file")
	}
}

func TestValidateScenarioChain_DuplicateFilenames(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_a.json")

	settings := models.DefaultWhatIfSettings()
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_a.json", TransitionAge: 70},
		{ScenarioFilename: "whatif_a.json", TransitionAge: 75}, // same file again
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Error("expected error for duplicate filenames in chain")
	}
}

func TestValidateScenarioChain_ValidChain(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)
	writeScenarioFile(t, sm, store, dir, "whatif_mid.json")
	writeScenarioFile(t, sm, store, dir, "whatif_late.json")

	settings := models.DefaultWhatIfSettings() // CurrentAge=65, ProjectionYears=30
	chain := []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_mid.json", TransitionAge: 70},
		{ScenarioFilename: "whatif_late.json", TransitionAge: 80},
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err != nil {
		t.Errorf("expected valid chain to pass validation, got: %v", err)
	}
}

func TestSave_RejectsInvalidChainOnAgeChange(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Create a scenario file that the chain will reference
	writeScenarioFile(t, sm, store, dir, "whatif_future.json")

	// Save settings with a valid chain: CurrentAge=65, TransitionAge=70
	settings := models.DefaultWhatIfSettings() // CurrentAge=65, ProjectionYears=30
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_future.json", TransitionAge: 70},
	}
	if err := sm.Save(settings); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Verify chain was saved (age 70 is valid for age 65 with 30-year projection)
	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if len(loaded.ScenarioChain) != 1 {
		t.Fatalf("expected chain to be saved, got %d links", len(loaded.ScenarioChain))
	}

	// Now bump CurrentAge to 75 — transition age 70 is now below CurrentAge
	settings2 := models.DefaultWhatIfSettings()
	settings2.StartDate = "2026-04"
	settings2.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: "1951-04", Role: models.PersonRolePrimary},
	}
	settings2.ComputeAges()
	settings2.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_future.json", TransitionAge: 70}, // now invalid
	}
	if err := sm.Save(settings2); err == nil {
		t.Fatal("expected save to fail when age change invalidates scenario chain")
	}

	// The previous valid settings should still be on disk.
	loaded2, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after age change: %v", err)
	}
	if len(loaded2.ScenarioChain) != 1 {
		t.Errorf("expected prior valid chain to remain, got %d links", len(loaded2.ScenarioChain))
	}
	if loaded2.CurrentAge != 65 {
		t.Errorf("expected CurrentAge 65 from last successful save, got %d", loaded2.CurrentAge)
	}
}

// --- DeleteScenario referential integrity ---

func TestDeleteScenario_RejectsReferencedScenario(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Create the "referenced" scenario file
	writeScenarioFile(t, sm, store, dir, "whatif_referenced.json")

	// Write the default scenario with a chain that references whatif_referenced.json
	settings := models.DefaultWhatIfSettings()
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "whatif_referenced.json", TransitionAge: 70},
	}
	// Write directly (bypass saveInternal validation by writing raw JSON)
	data, _ := json.MarshalIndent(settings, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	if err := store.WriteFile(filepath.Join(dir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("write default scenario: %v", err)
	}
	// Clear cache
	sm.mu.Lock()
	sm.cache = nil
	sm.mu.Unlock()

	// Attempt to delete the referenced scenario — should fail
	err := sm.DeleteScenario("whatif_referenced.json")
	if err == nil {
		t.Fatal("expected error when deleting a referenced scenario, got nil")
	}
	if !strings.Contains(err.Error(), "whatif_referenced.json") {
		t.Errorf("error should mention the referenced file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "whatif.json") {
		t.Errorf("error should mention the referencing file, got: %v", err)
	}

	// File should still exist
	if _, statErr := store.Stat(filepath.Join(dir, "whatif_referenced.json")); statErr != nil {
		t.Errorf("referenced scenario file should still exist after rejected delete: %v", statErr)
	}
}

// newTestSMWithDir returns a SettingsManager and its settings directory path.
func newTestSMWithDir(t *testing.T) (*SettingsManager, string, *storage.Storage) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dir := filepath.Join(root, "settings")
	return NewSettingsManager(dir, store), dir, store
}

func TestLoadInternal_LegacyHealthcareMigration(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Write a settings file with legacy healthcare but no healthcare persons
	legacy := map[string]interface{}{
		"monthly_healthcare":      500,
		"healthcare_inflation":    6.0,
		"current_age":             60,
		"healthcare_persons":      []interface{}{},
		"income_sources":          []interface{}{},
		"expense_sources":         []interface{}{},
		"removed_income_sources":  []interface{}{},
		"removed_expense_sources": []interface{}{},
	}

	data, _ := json.MarshalIndent(legacy, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	_ = store.WriteFile(filepath.Join(dir, "whatif.json"), data, 0644)

	loaded, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 migrated healthcare person, got %d", len(loaded.HealthcarePersons))
	}
	p := loaded.HealthcarePersons[0]
	if p.CurrentCoverage != models.CoverageACA {
		t.Errorf("expected ACA coverage for age 60, got %s", p.CurrentCoverage)
	}
	if p.CurrentMonthlyCost != 500 {
		t.Errorf("expected monthly cost 500, got %f", p.CurrentMonthlyCost)
	}
}

func TestLoadInternal_SpendingPhasesMigration(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Write settings with nil spending phase config
	settings := models.DefaultWhatIfSettings()
	settings.SpendingPhaseConfig = nil

	data, _ := json.MarshalIndent(settings, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	_ = store.WriteFile(filepath.Join(dir, "whatif.json"), data, 0644)

	loaded, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}

	if loaded.SpendingPhaseConfig == nil {
		t.Fatal("expected SpendingPhaseConfig to be initialized")
	}
	if loaded.SpendingPhaseConfig.Enabled {
		t.Error("expected spending phases to be disabled by default")
	}
	if len(loaded.SpendingPhaseConfig.Phases) == 0 {
		t.Error("expected default phases to be populated")
	}
}

func TestLoadInternal_EmptySpendingPhases(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Write settings with spending phase config that has empty phases
	settings := models.DefaultWhatIfSettings()
	settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{},
	}

	data, _ := json.MarshalIndent(settings, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	_ = store.WriteFile(filepath.Join(dir, "whatif.json"), data, 0644)

	loaded, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.SpendingPhaseConfig.Phases) == 0 {
		t.Error("expected default phases to be populated for empty phase list")
	}
}

func TestLoadInternal_ZeroMultiplierMigration(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	settings := models.DefaultWhatIfSettings()
	settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 0}, // 0 should become 1.0
		},
	}

	data, _ := json.MarshalIndent(settings, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	_ = store.WriteFile(filepath.Join(dir, "whatif.json"), data, 0644)

	loaded, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}

	if loaded.SpendingPhaseConfig.Phases[0].Multiplier != 1.0 {
		t.Errorf("expected multiplier 1.0, got %f", loaded.SpendingPhaseConfig.Phases[0].Multiplier)
	}
}

func TestUpdateSettings_AllFieldTypes(t *testing.T) {
	sm := newTestSM(t)

	updates := map[string]interface{}{
		"current_age":                70,
		"spouse_age":                 68,
		"projection_years":           25,
		"healthcare_start_years":     2,
		"tax_deferred_delay_years":   5,
		"phase_age_reference":        "older",
		"tax_deferred_stock_percent": 80.0,
		"tax_deferred_cash_percent":  5.0,
		"roth_stock_percent":         90.0,
		"roth_cash_percent":          0.0,
		"taxable_stock_percent":      60.0,
		"taxable_cash_percent":       10.0,
		"healthcare_inflation":       7.5,
		"spending_decline_rate":      1.5,
		"steady_state_override_year": 3.0,
	}

	result, err := sm.UpdateSettings(updates)
	if err != nil {
		t.Fatal(err)
	}

	if result.CurrentAge != 70 {
		t.Errorf("expected CurrentAge 70, got %d", result.CurrentAge)
	}
	if result.SpouseAge != 68 {
		t.Errorf("expected SpouseAge 68, got %d", result.SpouseAge)
	}
	if result.ProjectionYears != 25 {
		t.Errorf("expected ProjectionYears 25, got %d", result.ProjectionYears)
	}
	if result.TaxDeferredDelayYears != 5 {
		t.Errorf("expected TaxDeferredDelayYears 5, got %d", result.TaxDeferredDelayYears)
	}
	if result.PhaseAgeReference != "older" {
		t.Errorf("expected PhaseAgeReference 'older', got %s", result.PhaseAgeReference)
	}
	if result.TaxDeferredStockPercent != 80.0 {
		t.Errorf("expected TaxDeferredStockPercent 80, got %f", result.TaxDeferredStockPercent)
	}
	if result.RothStockPercent != 90.0 {
		t.Errorf("expected RothStockPercent 90, got %f", result.RothStockPercent)
	}
	if result.TaxableStockPercent != 60.0 {
		t.Errorf("expected TaxableStockPercent 60, got %f", result.TaxableStockPercent)
	}
	if result.SteadyStateOverrideYear != 3.0 {
		t.Errorf("expected SteadyStateOverrideYear 3.0, got %f", result.SteadyStateOverrideYear)
	}
}

func TestRenameScenario_ActiveScenario(t *testing.T) {
	sm, dir, store := newTestSMWithDir(t)

	// Create a scenario file and switch to it
	settings := models.DefaultWhatIfSettings()
	settings.ScenarioName = "Old Name"
	data, _ := json.MarshalIndent(settings, "", "  ")
	_ = store.MkdirAll(dir, 0755)
	_ = store.WriteFile(filepath.Join(dir, "whatif_test.json"), data, 0644)

	_ = sm.SwitchScenario("whatif_test.json")

	// Rename the active scenario
	err := sm.RenameScenario("whatif_test.json", "New Name")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the name was updated
	name := sm.ActiveScenario()
	if name != "New Name" {
		t.Errorf("expected 'New Name', got %q", name)
	}
}

func TestRenameScenario_DefaultForbidden(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("whatif.json", "Something")
	if err == nil {
		t.Error("expected error when renaming default scenario")
	}
}

func TestRenameScenario_InvalidFilename(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("../evil.json", "Bad")
	if err == nil {
		t.Error("expected error for invalid filename")
	}
}

func TestRenameScenario_NonexistentFile(t *testing.T) {
	sm := newTestSM(t)
	err := sm.RenameScenario("whatif_nonexistent.json", "New Name")
	if err == nil {
		t.Error("expected error for non-existent scenario file")
	}
}

func TestUpdateSettings_MoreFields(t *testing.T) {
	sm := newTestSM(t)

	updates := map[string]interface{}{
		"portfolio_value":         500000.0,
		"monthly_living_expenses": 3500.0,
		"monthly_healthcare":      200.0,
		"stock_percent":           70.0,
		"cash_percent":            10.0,
		"investment_return":       6.5,
		"discount_rate":           4.0,
		"roth_percent":            15.0,
		"tax_deferred_percent":    55.0,
	}

	result, err := sm.UpdateSettings(updates)
	if err != nil {
		t.Fatal(err)
	}

	if result.PortfolioValue != 500000.0 {
		t.Errorf("expected PortfolioValue 500000, got %f", result.PortfolioValue)
	}
	if result.MonthlyLivingExpenses != 3500.0 {
		t.Errorf("expected MonthlyLivingExpenses 3500, got %f", result.MonthlyLivingExpenses)
	}
	if result.InvestmentReturn != 6.5 {
		t.Errorf("expected InvestmentReturn 6.5, got %f", result.InvestmentReturn)
	}
	if result.StockPercent != 70.0 {
		t.Errorf("expected StockPercent 70, got %f", result.StockPercent)
	}
	if result.CashPercent != 10.0 {
		t.Errorf("expected CashPercent 10, got %f", result.CashPercent)
	}
	if result.DiscountRate != 4.0 {
		t.Errorf("expected DiscountRate 4, got %f", result.DiscountRate)
	}
}

func TestLoadScenarioSettings_ReadsWithoutSwitching(t *testing.T) {
	sm := newTestSM(t)

	defaults := models.DefaultWhatIfSettings()
	defaults.PortfolioValue = 1000000
	if err := sm.Save(defaults); err != nil {
		t.Fatalf("save default: %v", err)
	}

	if _, err := sm.CreateScenario("Post-SS"); err != nil {
		t.Fatalf("create scenario: %v", err)
	}

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

	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("switch: %v", err)
	}

	loaded, err := sm.LoadScenarioSettings(postSSFilename)
	if err != nil {
		t.Fatalf("load scenario settings: %v", err)
	}

	if loaded.PortfolioValue != 1000000 {
		t.Errorf("expected portfolio 1000000, got %f", loaded.PortfolioValue)
	}

	if sm.ActiveFilename() != "whatif.json" {
		t.Errorf("active scenario changed to %s, expected whatif.json", sm.ActiveFilename())
	}
}

func TestLoadScenarioSettings_InvalidFilename(t *testing.T) {
	sm := newTestSM(t)

	_, err := sm.LoadScenarioSettings("../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestLoadScenarioSettings_MissingFile(t *testing.T) {
	sm := newTestSM(t)

	_, err := sm.LoadScenarioSettings("whatif_nonexistent.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// --- Restore-path duplicate detection (regression: #B4) ---

// seedSettingsWithIDInBothLists writes a hand-crafted settings file with the
// same ID present in both the active and removed lists. Simulates a corrupted
// JSON or a legacy hand-edit; the restore path must reject this rather than
// blindly create a duplicate active entry.
func seedSettingsWithIDInBothLists(t *testing.T, sm *SettingsManager, build func(s *models.WhatIfSettings)) {
	t.Helper()
	settings := models.DefaultWhatIfSettings()
	build(settings)
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if err := sm.saveInternal(settings); err != nil {
		t.Fatalf("saveInternal: %v", err)
	}
}

func TestRestoreIncomeSource_DuplicateActiveIDRejected(t *testing.T) {
	sm := newTestSM(t)
	seedSettingsWithIDInBothLists(t, sm, func(s *models.WhatIfSettings) {
		src := models.IncomeSource{ID: "dup", Name: "Pension", Amount: 1000, StartMonth: 0}
		s.IncomeSources = []models.IncomeSource{src}
		s.RemovedIncomeSources = []models.IncomeSource{src}
	})

	_, err := sm.RestoreIncomeSource("dup")
	if err == nil {
		t.Fatal("expected ScenarioConflictError, got nil")
	}
	var conflictErr *ScenarioConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ScenarioConflictError, got %T: %v", err, err)
	}

	// State must be unchanged: active list still has it once, removed list still has it.
	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].ID != "dup" {
		t.Errorf("active list mutated: %+v", s.IncomeSources)
	}
	if len(s.RemovedIncomeSources) != 1 || s.RemovedIncomeSources[0].ID != "dup" {
		t.Errorf("removed list mutated: %+v", s.RemovedIncomeSources)
	}
}

func TestRestoreExpenseSource_DuplicateActiveIDRejected(t *testing.T) {
	sm := newTestSM(t)
	seedSettingsWithIDInBothLists(t, sm, func(s *models.WhatIfSettings) {
		src := models.ExpenseSource{ID: "dup", Name: "Rent", Amount: 2000, StartYear: 0}
		s.ExpenseSources = []models.ExpenseSource{src}
		s.RemovedExpenseSources = []models.ExpenseSource{src}
	})

	_, err := sm.RestoreExpenseSource("dup")
	if err == nil {
		t.Fatal("expected ScenarioConflictError, got nil")
	}
	var conflictErr *ScenarioConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ScenarioConflictError, got %T: %v", err, err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.ExpenseSources) != 1 {
		t.Errorf("active list mutated: %+v", s.ExpenseSources)
	}
	if len(s.RemovedExpenseSources) != 1 {
		t.Errorf("removed list mutated: %+v", s.RemovedExpenseSources)
	}
}

func TestRestoreBigTicketItem_DuplicateActiveIDRejected(t *testing.T) {
	sm := newTestSM(t)
	seedSettingsWithIDInBothLists(t, sm, func(s *models.WhatIfSettings) {
		item := models.BigTicketItem{ID: "dup", Name: "Boat", Amount: 50000, Year: 2030, Type: "expense"}
		s.BigTicketItems = []models.BigTicketItem{item}
		s.RemovedBigTicketItems = []models.BigTicketItem{item}
	})

	_, err := sm.RestoreBigTicketItem("dup")
	if err == nil {
		t.Fatal("expected ScenarioConflictError, got nil")
	}
	var conflictErr *ScenarioConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ScenarioConflictError, got %T: %v", err, err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.BigTicketItems) != 1 {
		t.Errorf("active list mutated: %+v", s.BigTicketItems)
	}
	if len(s.RemovedBigTicketItems) != 1 {
		t.Errorf("removed list mutated: %+v", s.RemovedBigTicketItems)
	}
}

func TestRestoreIncomeSource_NotFound(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "active", Name: "Wage", Amount: 5000}); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}

	_, err := sm.RestoreIncomeSource("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}

func TestRestoreExpenseSource_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.RestoreExpenseSource("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}

func TestRestoreBigTicketItem_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.RestoreBigTicketItem("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}
