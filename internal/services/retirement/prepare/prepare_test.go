package prepare

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"budget2/internal/models"
)

// validSettings returns a settings that will pass ValidatePersons.
// Built from DefaultWhatIfSettings (canonical valid config) plus an optional
// spouse for tests that need one.
func validSettings(t *testing.T, withSpouse bool) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	if withSpouse {
		s.Persons = append(s.Persons, models.Person{
			ID:         "spouse-1",
			Name:       "Spouse",
			BirthMonth: models.BirthMonthForAge(s.StartDate, 63),
			Role:       models.PersonRoleSpouse,
		})
	}
	return s
}

func TestFrom_NilReturnsError(t *testing.T) {
	p, err := From(nil)
	if err == nil {
		t.Fatal("expected error for nil settings, got nil")
	}
	if !p.IsZero() {
		t.Errorf("expected zero PreparedSettings on error, got %+v", p)
	}
}

func TestFrom_HappyPath(t *testing.T) {
	s := validSettings(t, false)
	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if p.IsZero() {
		t.Fatal("expected non-zero PreparedSettings")
	}
	got := p.Settings()
	if got == nil {
		t.Fatal("Settings returned nil")
	}
	if got.CurrentAge <= 0 {
		t.Errorf("expected CurrentAge > 0, got %d", got.CurrentAge)
	}
	if got.PortfolioValue != s.PortfolioValue {
		t.Errorf("PortfolioValue: got %v want %v", got.PortfolioValue, s.PortfolioValue)
	}
}

func TestFrom_DeepCopy_OriginalMutationDoesNotLeak(t *testing.T) {
	s := validSettings(t, false)
	s.PortfolioValue = 1_000_000
	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	// Mutate original after preparation.
	s.PortfolioValue = 99
	s.InflationRate = 999.0

	prepared := p.Settings()
	if prepared.PortfolioValue != 1_000_000 {
		t.Errorf("prepared PortfolioValue leaked from original: got %v", prepared.PortfolioValue)
	}
	if prepared.InflationRate == 999.0 {
		t.Errorf("prepared InflationRate leaked from original")
	}
}

func TestFrom_DeepCopy_SliceMutationDoesNotLeak(t *testing.T) {
	s := validSettings(t, false)
	s.IncomeSources = []models.IncomeSource{
		{ID: "inc-1", Name: "Pension", Amount: 1000, Type: models.IncomeFixed},
	}
	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}

	// Mutate the original slice; prepared snapshot must be untouched.
	s.IncomeSources[0].Amount = 9999
	s.IncomeSources = append(s.IncomeSources, models.IncomeSource{ID: "inc-2", Name: "Side", Amount: 500, Type: models.IncomeFixed})

	prepared := p.Settings()
	if len(prepared.IncomeSources) != 1 {
		t.Fatalf("prepared IncomeSources len: got %d want 1", len(prepared.IncomeSources))
	}
	if prepared.IncomeSources[0].Amount != 1000 {
		t.Errorf("prepared IncomeSources[0].Amount leaked: got %v want 1000",
			prepared.IncomeSources[0].Amount)
	}
}

func TestFrom_NormalizesPhaseReference(t *testing.T) {
	s := validSettings(t, false)
	s.PhaseAgeReference = "garbage"
	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	got := p.Settings().PhaseAgeReference
	switch got {
	case "younger", "older", "primary", "spouse":
		// pass
	default:
		t.Errorf("PhaseAgeReference not normalized: got %q", got)
	}
	// Original should also be unaffected (deep copy).
	if s.PhaseAgeReference != "garbage" {
		t.Errorf("original PhaseAgeReference mutated: got %q", s.PhaseAgeReference)
	}
}

func TestFrom_ComputesAges(t *testing.T) {
	s := validSettings(t, true)
	// Force ages to 0 to prove From recomputes them.
	s.CurrentAge = 0
	s.SpouseAge = 0

	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	prepared := p.Settings()
	if prepared.CurrentAge <= 0 {
		t.Errorf("expected CurrentAge to be derived, got %d", prepared.CurrentAge)
	}
	if prepared.SpouseAge <= 0 {
		t.Errorf("expected SpouseAge to be derived, got %d", prepared.SpouseAge)
	}
}

func TestFrom_ValidationErrorPropagates(t *testing.T) {
	s := validSettings(t, false)
	// Two primaries — ValidatePersons requires exactly one.
	s.Persons = append(s.Persons, models.Person{
		ID:         "p2",
		Name:       "Second Primary",
		BirthMonth: models.BirthMonthForAge(s.StartDate, 60),
		Role:       models.PersonRolePrimary,
	})

	p, err := From(s)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !p.IsZero() {
		t.Errorf("expected zero PreparedSettings on error, got %+v", p)
	}
	if !strings.Contains(err.Error(), "primary") {
		t.Errorf("expected error to mention primary, got: %v", err)
	}
	// Wrap chain check: err is wrapped through "prepare.From: validate:"
	if !strings.Contains(err.Error(), "prepare.From") {
		t.Errorf("expected error to be wrapped by prepare.From, got: %v", err)
	}
}

func TestFrom_Idempotent(t *testing.T) {
	s := validSettings(t, true)
	p1, err := From(s)
	if err != nil {
		t.Fatalf("first From: %v", err)
	}
	p2, err := From(p1.Settings())
	if err != nil {
		t.Fatalf("second From: %v", err)
	}

	a, _ := json.Marshal(p1.Settings())
	b, _ := json.Marshal(p2.Settings())
	if string(a) != string(b) {
		t.Errorf("From not idempotent:\n  first:  %s\n  second: %s", a, b)
	}
}

func TestPreparedSettings_IsZero(t *testing.T) {
	var zero PreparedSettings
	if !zero.IsZero() {
		t.Errorf("zero value should report IsZero=true")
	}

	s := validSettings(t, false)
	p, err := From(s)
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if p.IsZero() {
		t.Errorf("prepared value should report IsZero=false")
	}
}

func TestDeepCopy_PreservesAllJSONFields(t *testing.T) {
	// Populate as many fields as possible from the canonical default, then
	// add representative slice and pointer values.
	s := validSettings(t, true)
	s.PortfolioValue = 2_500_000
	s.InflationRate = 3.5
	s.InvestmentReturn = 6.0
	s.IncomeSources = []models.IncomeSource{
		{ID: "inc-1", Name: "SS", Amount: 2000, Type: models.IncomeFixed, StartMonth: 24},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "exp-1", Name: "Mortgage", Amount: 1500, EndYear: 10, Inflation: true},
	}
	s.RemovedIncomeSources = []models.IncomeSource{
		{ID: "rem-inc-1", Name: "Old job", Amount: 100, Type: models.IncomeTemporary},
	}

	// Marshal-before deep copy:
	before, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}

	clone, err := DeepCopy(s)
	if err != nil {
		t.Fatalf("DeepCopy: %v", err)
	}

	after, err := json.Marshal(clone)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	if string(before) != string(after) {
		t.Errorf("DeepCopy round-trip lost JSON-tagged fields:\n  before: %s\n  after:  %s",
			before, after)
	}

	// Sanity: the clone is a different pointer.
	if clone == s {
		t.Errorf("DeepCopy returned the same pointer")
	}
}

// recordingTB wraps a testing.TB so MustFrom can be tested without aborting
// the surrounding test. Real Fatalf calls runtime.Goexit; our override only
// records the call.
type recordingTB struct {
	testing.TB
	fatalCalled bool
	fatalMsg    string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatalCalled = true
	r.fatalMsg = fmt.Sprintf(format, args...)
	// Deliberately do NOT call runtime.Goexit so the test can observe.
}

func TestMustFrom_FatalsOnError(t *testing.T) {
	rec := &recordingTB{TB: t}
	got := MustFrom(rec, nil)

	if !rec.fatalCalled {
		t.Fatal("expected Fatalf to be invoked")
	}
	if !strings.Contains(rec.fatalMsg, "prepare.MustFrom") {
		t.Errorf("expected fatal msg to be prefixed by prepare.MustFrom, got: %q", rec.fatalMsg)
	}
	if !got.IsZero() {
		t.Errorf("expected zero PreparedSettings after fatal, got non-zero")
	}
}

func TestMustFrom_HappyPath(t *testing.T) {
	s := validSettings(t, false)
	p := MustFrom(t, s)
	if p.IsZero() {
		t.Fatal("MustFrom returned zero PreparedSettings on valid input")
	}
}

func TestDeepCopy_NilReturnsError(t *testing.T) {
	out, err := DeepCopy(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
	if out != nil {
		t.Errorf("expected nil output, got %+v", out)
	}
}
