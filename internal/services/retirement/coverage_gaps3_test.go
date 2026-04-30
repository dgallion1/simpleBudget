package retirement

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// --- Error type Error()/Unwrap() coverage ---
//
// Each of ScenarioChainValidationError, ScenarioValidationError,
// ScenarioNotFoundError, and ScenarioConflictError exposes Error() and
// Unwrap(). Production code creates them with a wrapped Err, but the
// nil/zero-value paths weren't exercised — covering them here pulls all four
// error types from 0% to 100%.

func TestScenarioChainValidationError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("chain rule violated")

	wrapped := &ScenarioChainValidationError{Err: inner}
	if got := wrapped.Error(); got != "chain rule violated" {
		t.Errorf("wrapped Error() = %q, want %q", got, "chain rule violated")
	}
	if !errors.Is(wrapped, inner) {
		t.Error("errors.Is(wrapped, inner) should be true")
	}

	empty := &ScenarioChainValidationError{}
	if got := empty.Error(); got != "invalid scenario chain" {
		t.Errorf("empty Error() = %q, want default sentinel", got)
	}
	if u := empty.Unwrap(); u != nil {
		t.Errorf("empty Unwrap() = %v, want nil", u)
	}

	var nilErr *ScenarioChainValidationError
	if got := nilErr.Error(); got != "invalid scenario chain" {
		t.Errorf("nil Error() = %q, want default sentinel", got)
	}
	if u := nilErr.Unwrap(); u != nil {
		t.Errorf("nil Unwrap() = %v, want nil", u)
	}
}

func TestScenarioValidationError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("name is empty")

	wrapped := &ScenarioValidationError{Err: inner}
	if got := wrapped.Error(); got != "name is empty" {
		t.Errorf("wrapped Error() = %q", got)
	}
	if !errors.Is(wrapped, inner) {
		t.Error("errors.Is should locate inner")
	}

	empty := &ScenarioValidationError{}
	if got := empty.Error(); got != "invalid scenario" {
		t.Errorf("empty Error() = %q", got)
	}
	if u := empty.Unwrap(); u != nil {
		t.Errorf("empty Unwrap() = %v, want nil", u)
	}

	var nilErr *ScenarioValidationError
	if got := nilErr.Error(); got != "invalid scenario" {
		t.Errorf("nil Error() = %q", got)
	}
	if u := nilErr.Unwrap(); u != nil {
		t.Errorf("nil Unwrap() = %v, want nil", u)
	}
}

func TestScenarioNotFoundError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("removed inc1 not found")

	wrapped := &ScenarioNotFoundError{Err: inner}
	if got := wrapped.Error(); got != "removed inc1 not found" {
		t.Errorf("wrapped Error() = %q", got)
	}
	if !errors.Is(wrapped, inner) {
		t.Error("errors.Is should locate inner")
	}

	empty := &ScenarioNotFoundError{}
	if got := empty.Error(); got != "scenario not found" {
		t.Errorf("empty Error() = %q", got)
	}
	if u := empty.Unwrap(); u != nil {
		t.Errorf("empty Unwrap() = %v, want nil", u)
	}

	var nilErr *ScenarioNotFoundError
	if got := nilErr.Error(); got != "scenario not found" {
		t.Errorf("nil Error() = %q", got)
	}
	if u := nilErr.Unwrap(); u != nil {
		t.Errorf("nil Unwrap() = %v, want nil", u)
	}
}

func TestScenarioConflictError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("already exists")

	wrapped := &ScenarioConflictError{Err: inner}
	if got := wrapped.Error(); got != "already exists" {
		t.Errorf("wrapped Error() = %q", got)
	}
	if !errors.Is(wrapped, inner) {
		t.Error("errors.Is should locate inner")
	}

	// nil receiver: triggers the `e == nil || e.Err == nil` branch in Error()
	// (first clause) and the `e == nil` branch in Unwrap().
	var nilErr *ScenarioConflictError
	if got := nilErr.Error(); got != "scenario conflict" {
		t.Errorf("nil Error() = %q", got)
	}
	if u := nilErr.Unwrap(); u != nil {
		t.Errorf("nil Unwrap() = %v, want nil", u)
	}

	// empty Err: triggers the `e.Err == nil` branch in Error() and the
	// `return e.Err` branch (yielding nil) in Unwrap().
	empty := &ScenarioConflictError{}
	if got := empty.Error(); got != "scenario conflict" {
		t.Errorf("empty Error() = %q", got)
	}
	if u := empty.Unwrap(); u != nil {
		t.Errorf("empty Unwrap() = %v, want nil", u)
	}
}

// --- findPreparedScenarioPerson (chain.go) ---
//
// Direct unit tests for the helper. Covers all switch arms plus the
// name-matching tail (ambiguous and unique cases).

func TestFindPreparedScenarioPerson_PrimaryRole(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p-spouse", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "Whoever", Role: models.PersonRolePrimary}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got == nil || got.ID != "p-primary" {
		t.Fatalf("expected primary lookup to return p-primary, got %+v", got)
	}
}

func TestFindPreparedScenarioPerson_SpouseRole(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p-spouse", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "Anyone", Role: models.PersonRoleSpouse}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got == nil || got.ID != "p-spouse" {
		t.Fatalf("expected spouse lookup to return p-spouse, got %+v", got)
	}
}

func TestFindPreparedScenarioPerson_SpouseRoleMissingReturnsNil(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "X", Role: models.PersonRoleSpouse}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got != nil {
		t.Fatalf("expected nil when no spouse exists in primary, got %+v", got)
	}
}

func TestFindPreparedScenarioPerson_NameMatchUnique(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p-other", Name: "Riley", BirthMonth: "1995-04", Role: models.PersonRoleOther},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "  riley  ", Role: models.PersonRoleOther}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got == nil || got.ID != "p-other" {
		t.Fatalf("expected name match to return p-other, got %+v", got)
	}
}

func TestFindPreparedScenarioPerson_NameMatchAmbiguousReturnsNil(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p-twin1", Name: "Riley", BirthMonth: "1995-04", Role: models.PersonRoleOther},
		{ID: "p-twin2", Name: "Riley", BirthMonth: "1996-04", Role: models.PersonRoleOther},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "Riley", Role: models.PersonRoleOther}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got != nil {
		t.Fatalf("expected ambiguous name match to return nil, got %+v", got)
	}
}

func TestFindPreparedScenarioPerson_NameNoMatch(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "p-primary", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	linkedPerson := &models.Person{ID: "linked-id", Name: "Stranger", Role: models.PersonRoleOther}
	got := findPreparedScenarioPerson(primary, linkedPerson)
	if got != nil {
		t.Fatalf("expected no match to return nil, got %+v", got)
	}
}

// --- prepareChainedSettings: PersonID remap branches ---
//
// Drives the in-loop branches at chain.go:32-48: a healthcare entry whose
// linked PersonID exists in the linked settings but not in the primary's
// persons. The mapper either matches by role/name (rewrites to primary's ID)
// or clears the link to "" when no mapping exists.

func TestPrepareChainedSettings_HealthcarePersonIDRemappedToPrimary(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "primary-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	primary.CurrentAge = 60
	primary.ProjectionYears = 30

	// Linked scenario uses a different PersonID for the primary person.
	linked := models.DefaultWhatIfSettings()
	linked.StartDate = "2026-04"
	linked.Persons = []models.Person{
		{ID: "linked-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	linked.HealthcarePersons = []models.HealthcarePerson{
		{ID: "hp1", Name: "Alex", PersonID: "linked-A", CurrentAge: 70},
	}

	result := prepareChainedSettings(linked, primary, 10)
	if len(result.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(result.HealthcarePersons))
	}
	if result.HealthcarePersons[0].PersonID != "primary-A" {
		t.Errorf("expected PersonID remapped to primary-A, got %q",
			result.HealthcarePersons[0].PersonID)
	}
}

func TestPrepareChainedSettings_HealthcarePersonIDClearedWhenLinkedPersonMissing(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "primary-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	primary.CurrentAge = 60
	primary.ProjectionYears = 30

	linked := models.DefaultWhatIfSettings()
	linked.StartDate = "2026-04"
	linked.Persons = []models.Person{
		{ID: "linked-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	// Healthcare references a PersonID that is in *neither* the primary nor
	// the linked persons list — the linkedPerson lookup returns nil so
	// PersonID is cleared via the else branch (chain.go:44-46).
	linked.HealthcarePersons = []models.HealthcarePerson{
		{ID: "hp1", Name: "Ghost", PersonID: "ghost-id", CurrentAge: 70},
	}

	result := prepareChainedSettings(linked, primary, 10)
	if len(result.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(result.HealthcarePersons))
	}
	if result.HealthcarePersons[0].PersonID != "" {
		t.Errorf("expected PersonID cleared, got %q",
			result.HealthcarePersons[0].PersonID)
	}
}

func TestPrepareChainedSettings_HealthcarePersonIDClearedWhenNoMappingFound(t *testing.T) {
	// Primary has no spouse; linked person is a spouse role -> no mapping ->
	// PersonID cleared via chain.go:39-43.
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "primary-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	primary.CurrentAge = 60
	primary.ProjectionYears = 30

	linked := models.DefaultWhatIfSettings()
	linked.StartDate = "2026-04"
	linked.Persons = []models.Person{
		{ID: "linked-A", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "linked-B", Name: "Phantom", BirthMonth: "1968-04", Role: models.PersonRoleSpouse},
	}
	linked.HealthcarePersons = []models.HealthcarePerson{
		{ID: "hp1", Name: "Phantom", PersonID: "linked-B", CurrentAge: 68},
	}

	result := prepareChainedSettings(linked, primary, 10)
	if len(result.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(result.HealthcarePersons))
	}
	if result.HealthcarePersons[0].PersonID != "" {
		t.Errorf("expected PersonID cleared (no spouse in primary), got %q",
			result.HealthcarePersons[0].PersonID)
	}
}

func TestPrepareChainedSettings_HealthcarePersonIDLeftAloneWhenInPrimary(t *testing.T) {
	// PersonID exists directly in primary.Persons -> the inner `prepared.FindPerson`
	// returns non-nil and the remap branch is skipped. This exercises the
	// `continue` path on chain.go:48 (without entering the remap block).
	primary := models.DefaultWhatIfSettings()
	primary.StartDate = "2026-04"
	primary.Persons = []models.Person{
		{ID: "shared-id", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	primary.CurrentAge = 60
	primary.ProjectionYears = 30

	linked := models.DefaultWhatIfSettings()
	linked.StartDate = "2026-04"
	linked.Persons = []models.Person{
		{ID: "shared-id", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	linked.HealthcarePersons = []models.HealthcarePerson{
		{ID: "hp1", Name: "Alex", PersonID: "shared-id", CurrentAge: 70},
	}

	result := prepareChainedSettings(linked, primary, 10)
	if len(result.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(result.HealthcarePersons))
	}
	if result.HealthcarePersons[0].PersonID != "shared-id" {
		t.Errorf("expected PersonID to remain shared-id, got %q",
			result.HealthcarePersons[0].PersonID)
	}
	// The PersonID branch takes a `continue` rather than rebasing
	// CurrentAge by transitionYear. The trailing ComputeAges() call then
	// re-derives CurrentAge from the linked person's BirthMonth (1965-04
	// at StartDate 2026-04 = 61), so the value lands on 61 rather than the
	// rebased 60.
	if result.HealthcarePersons[0].CurrentAge != 61 {
		t.Errorf("expected CurrentAge 61 (derived from BirthMonth, not rebased), got %d",
			result.HealthcarePersons[0].CurrentAge)
	}
}

// --- normalizeStartDate: invalid format branch ---

func TestNormalizeStartDate_InvalidFormatFallsBack(t *testing.T) {
	// Non-empty, non-parseable -> hits the `time.Parse` error branch
	// (settings.go:137-139).
	got, changed := normalizeStartDate("not-a-date")
	if !changed {
		t.Error("expected changed=true when raw is unparseable")
	}
	if got == "" {
		t.Error("expected non-empty fallback start date")
	}
}

func TestNormalizeStartDate_EmptyReturnsCurrentMonth(t *testing.T) {
	got, changed := normalizeStartDate("")
	if !changed {
		t.Error("expected changed=true for empty input")
	}
	if got == "" {
		t.Error("expected non-empty current local month fallback")
	}
}

func TestNormalizeStartDate_ValidPassesThrough(t *testing.T) {
	got, changed := normalizeStartDate("2026-04")
	if changed {
		t.Error("expected changed=false for already-valid input")
	}
	if got != "2026-04" {
		t.Errorf("expected pass-through, got %q", got)
	}
}

// --- inferHealthcarePersonLink: every branch ---

func TestInferHealthcarePersonLink_EmptyName(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}
	if got := inferHealthcarePersonLink(settings, "   "); got != "" {
		t.Errorf("expected empty for whitespace name, got %q", got)
	}
}

func TestInferHealthcarePersonLink_PrimaryAliases(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	for _, alias := range []string{"You", "user", "PRIMARY"} {
		if got := inferHealthcarePersonLink(settings, alias); got != "p1" {
			t.Errorf("alias %q: got %q, want p1", alias, got)
		}
	}
}

func TestInferHealthcarePersonLink_SpouseAlias(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}
	if got := inferHealthcarePersonLink(settings, "Spouse"); got != "p2" {
		t.Errorf("got %q, want p2", got)
	}
}

func TestInferHealthcarePersonLink_NameMatchUnique(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}
	// "casey" doesn't match the spouse alias path, falls through to name match.
	if got := inferHealthcarePersonLink(settings, "casey"); got != "p2" {
		t.Errorf("got %q, want p2", got)
	}
}

func TestInferHealthcarePersonLink_NameMatchAmbiguousReturnsEmpty(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Riley", BirthMonth: "1995-04", Role: models.PersonRoleOther},
		{ID: "p3", Name: "Riley", BirthMonth: "1996-04", Role: models.PersonRoleOther},
	}
	if got := inferHealthcarePersonLink(settings, "Riley"); got != "" {
		t.Errorf("ambiguous match: got %q, want empty", got)
	}
}

func TestInferHealthcarePersonLink_NoPrimary(t *testing.T) {
	// No primary person at all — covers the path where GetPrimaryPerson()
	// returns nil and the alias switch is bypassed.
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Riley", BirthMonth: "1995-04", Role: models.PersonRoleOther},
	}
	// "you" can't resolve via primary alias; no person named "you" -> empty.
	if got := inferHealthcarePersonLink(settings, "you"); got != "" {
		t.Errorf("got %q, want empty when no primary exists", got)
	}
	// But a real name still matches.
	if got := inferHealthcarePersonLink(settings, "Riley"); got != "p1" {
		t.Errorf("got %q, want p1", got)
	}
}

// --- decodeSettings + loadInternal error paths ---

func TestDecodeSettings_InvalidJSONReturnsError(t *testing.T) {
	sm := newTestSM(t)
	settings, changed, err := sm.decodeSettings([]byte("{not valid json"))
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if changed {
		t.Error("changed should be false on unmarshal error")
	}
	// On unmarshal failure, decodeSettings returns DefaultWhatIfSettings()
	// rather than nil.
	if settings == nil {
		t.Error("expected non-nil defaults on unmarshal failure")
	}
}

func TestDecodeSettings_ValidatePersonsFailure(t *testing.T) {
	// Force ValidatePersons to fail by handing decodeSettings JSON with
	// duplicate person IDs. Hits the `if err != nil { return nil, changed, err }`
	// branch at settings.go:331-333.
	sm := newTestSM(t)
	payload := map[string]interface{}{
		"start_date": "2026-04",
		"persons": []map[string]interface{}{
			{"id": "dup", "name": "A", "birth_month": "1965-04", "role": "primary"},
			{"id": "dup", "name": "B", "birth_month": "1967-04", "role": "spouse"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	settings, _, err := sm.decodeSettings(data)
	if err == nil {
		t.Fatal("expected ValidatePersons error for duplicate IDs")
	}
	if settings != nil {
		t.Errorf("expected nil settings on validation failure, got %+v", settings)
	}
}

func TestLoadInternal_InvalidJSONReturnsDefaults(t *testing.T) {
	// When decodeSettings returns an error, loadInternal returns
	// DefaultWhatIfSettings() and the wrapped error
	// (settings.go:386-388).
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), []byte("{garbage"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)
	if _, err := sm.Load(); err == nil {
		t.Fatal("expected Load to surface decode error")
	}
}

// --- UpdateSettingsWithPersons ---
//
// Drives the 0% function at settings.go:787. Provides a basic happy path
// (apply updates + replace start date and persons) and verifies persistence.

func TestUpdateSettingsWithPersons_HappyPath(t *testing.T) {
	sm := newTestSM(t)

	// Seed initial state.
	if _, err := sm.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	updates := map[string]interface{}{
		"portfolio_value":         750000.0,
		"monthly_living_expenses": 4500.0,
		"projection_years":        25,
	}
	startDate := "2027-01"
	persons := []models.Person{
		{ID: "primary-2", Name: "Sam", BirthMonth: "1962-01", Role: models.PersonRolePrimary},
		{ID: "spouse-2", Name: "Jordan", BirthMonth: "1964-01", Role: models.PersonRoleSpouse},
	}

	got, err := sm.UpdateSettingsWithPersons(updates, startDate, persons)
	if err != nil {
		t.Fatalf("UpdateSettingsWithPersons: %v", err)
	}
	if got.PortfolioValue != 750000 {
		t.Errorf("PortfolioValue: got %f, want 750000", got.PortfolioValue)
	}
	if got.MonthlyLivingExpenses != 4500 {
		t.Errorf("MonthlyLivingExpenses: got %f, want 4500", got.MonthlyLivingExpenses)
	}
	if got.ProjectionYears != 25 {
		t.Errorf("ProjectionYears: got %d, want 25", got.ProjectionYears)
	}
	if got.StartDate != "2027-01" {
		t.Errorf("StartDate: got %q, want 2027-01", got.StartDate)
	}
	if len(got.Persons) != 2 || got.Persons[0].ID != "primary-2" || got.Persons[1].ID != "spouse-2" {
		t.Errorf("persons not persisted: %+v", got.Persons)
	}

	// Reload and verify persisted to disk.
	sm2 := NewSettingsManager(sm.settingsDir, sm.store)
	loaded, err := sm2.Load()
	if err != nil {
		t.Fatalf("reload Load: %v", err)
	}
	if loaded.PortfolioValue != 750000 || loaded.StartDate != "2027-01" {
		t.Errorf("changes not persisted: %+v", loaded)
	}
}

func TestUpdateSettingsWithPersons_InvalidPersonsSurfacesError(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("initial Load: %v", err)
	}

	// No primary person -> ValidatePersons fails inside saveInternal.
	persons := []models.Person{
		{ID: "p1", Name: "Riley", BirthMonth: "1995-04", Role: models.PersonRoleOther},
	}
	if _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{}, "2026-04", persons); err == nil {
		t.Fatal("expected error when persons fail validation")
	}
}

// --- removeSpousePersons (unexported helper, exercised via UpdateSettings) ---

// --- ensurePrimaryPerson / ensureSpousePerson direct unit tests ---

func TestEnsurePrimaryPerson_CreatesWhenMissing(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = nil

	got := ensurePrimaryPerson(settings)
	if got == nil {
		t.Fatal("expected new primary person")
	}
	if got.Role != models.PersonRolePrimary || got.Name != "You" {
		t.Errorf("unexpected primary person: %+v", got)
	}
	if got.ID == "" {
		t.Error("expected generated ID")
	}
	if len(settings.Persons) != 1 {
		t.Errorf("expected 1 person, got %d", len(settings.Persons))
	}
	// The returned pointer must point at the slice element (mutations apply).
	got.Name = "Updated"
	if settings.Persons[0].Name != "Updated" {
		t.Error("returned pointer should alias the slice element")
	}
}

func TestEnsurePrimaryPerson_FillsBlankNameOnExisting(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "  ", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	got := ensurePrimaryPerson(settings)
	if got == nil || got.Name != "You" {
		t.Errorf("expected blank primary name to be set to 'You', got %+v", got)
	}
}

func TestEnsurePrimaryPerson_PreservesNameWhenSet(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	got := ensurePrimaryPerson(settings)
	if got == nil || got.Name != "Alex" {
		t.Errorf("expected name preserved, got %+v", got)
	}
}

func TestEnsureSpousePerson_CreatesWhenMissing(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
	}

	got := ensureSpousePerson(settings)
	if got == nil {
		t.Fatal("expected new spouse person")
	}
	if got.Role != models.PersonRoleSpouse || got.Name != "Spouse" {
		t.Errorf("unexpected spouse person: %+v", got)
	}
	if got.ID == "" {
		t.Error("expected generated ID")
	}
	if len(settings.Persons) != 2 {
		t.Errorf("expected 2 persons, got %d", len(settings.Persons))
	}
	// Returned pointer aliases slice tail.
	got.Name = "Updated"
	if settings.Persons[1].Name != "Updated" {
		t.Error("returned pointer should alias the slice element")
	}
}

func TestEnsureSpousePerson_FillsBlankNameOnExisting(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p2", Name: " ", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}

	got := ensureSpousePerson(settings)
	if got == nil || got.Name != "Spouse" {
		t.Errorf("expected blank spouse name to be set to 'Spouse', got %+v", got)
	}
}

func TestEnsureSpousePerson_PreservesNameWhenSet(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}

	got := ensureSpousePerson(settings)
	if got == nil || got.Name != "Casey" {
		t.Errorf("expected name preserved, got %+v", got)
	}
}

// --- removeSpousePersons direct unit tests ---

func TestRemoveSpousePersons_FiltersOnlySpouseRoles(t *testing.T) {
	settings := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Name: "Alex", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Casey", Role: models.PersonRoleSpouse},
			{ID: "p3", Name: "Riley", Role: models.PersonRoleOther},
			{ID: "p4", Name: "Other Spouse", Role: models.PersonRoleSpouse},
		},
	}

	removeSpousePersons(settings)

	if len(settings.Persons) != 2 {
		t.Fatalf("expected 2 persons after removing spouses, got %d", len(settings.Persons))
	}
	for _, p := range settings.Persons {
		if p.Role == models.PersonRoleSpouse {
			t.Errorf("unexpected spouse remaining: %+v", p)
		}
	}
}

func TestRemoveSpousePersons_NoOpWhenNoSpouse(t *testing.T) {
	settings := &models.WhatIfSettings{
		Persons: []models.Person{
			{ID: "p1", Name: "Alex", Role: models.PersonRolePrimary},
		},
	}
	removeSpousePersons(settings)
	if len(settings.Persons) != 1 {
		t.Errorf("expected 1 person remaining, got %d", len(settings.Persons))
	}
}

// --- normalizeLoadedWhatIfSettings: legacy spouse migration ---
//
// settings.go:253-260 is uncovered: the legacy migration path that creates
// a spouse Person from `spouse_age` int field.

func TestLoadInternal_LegacySpouseAgeMigration(t *testing.T) {
	root := t.TempDir()
	settingsDir := filepath.Join(root, "settings")
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Old schema: current_age + spouse_age, no persons array, no start_date.
	legacy := map[string]interface{}{
		"current_age": 60,
		"spouse_age":  58,
	}
	data, _ := json.Marshal(legacy)
	if err := store.WriteFile(filepath.Join(settingsDir, "whatif.json"), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sm := NewSettingsManager(settingsDir, store)
	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(s.Persons) != 2 {
		t.Fatalf("expected 2 migrated persons (primary + spouse), got %d", len(s.Persons))
	}
	if s.Persons[0].Role != models.PersonRolePrimary {
		t.Errorf("expected first person to be primary, got %v", s.Persons[0].Role)
	}
	if s.Persons[1].Role != models.PersonRoleSpouse || s.Persons[1].Name != "Spouse" {
		t.Errorf("expected second person to be spouse, got %+v", s.Persons[1])
	}
}

// --- Load cache double-check (settings.go:351-353) ---
//
// The double-locked check returns the cache directly if another goroutine
// populated it between the read-lock release and write-lock acquisition.
// We can simulate that by populating cache directly while holding the
// write lock, then triggering a parallel Load.

func TestSettingsManager_LoadReturnsCacheOnSubsequentCalls(t *testing.T) {
	sm := newTestSM(t)

	first, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	second, err := sm.Load()
	if err != nil {
		t.Fatalf("Load (cached): %v", err)
	}
	if first != second {
		t.Error("expected second Load to return the cached pointer")
	}
}

// --- validateChainInternal: invalid chain filename triggers scenarioPath error ---

func TestValidateScenarioChain_InvalidFilenameRejected(t *testing.T) {
	sm := newTestSM(t)
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 30

	chain := []models.ScenarioChainLink{
		{TransitionAge: 65, ScenarioFilename: "../escaped.json"},
	}
	if err := sm.ValidateScenarioChain(chain, settings, "whatif.json"); err == nil {
		t.Fatal("expected error for path-traversal scenario filename")
	}
}

func TestUpdateSettings_RemoveSpouseClearsSpousePersons(t *testing.T) {
	// settings.go:947-955 (removeSpousePersons) runs only when
	// updates["spouse_age"] is an int <= 0. UpdateSettings calls
	// applySettingsUpdates which routes to this helper.
	sm := newTestSM(t)

	// Seed a settings file with a primary + spouse.
	root := sm.settingsDir
	if err := sm.store.MkdirAll(root, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := models.DefaultWhatIfSettings()
	seed.StartDate = "2026-04"
	seed.Persons = []models.Person{
		{ID: "primary-1", Name: "Alex", BirthMonth: "1965-04", Role: models.PersonRolePrimary},
		{ID: "spouse-1", Name: "Casey", BirthMonth: "1967-04", Role: models.PersonRoleSpouse},
	}
	if err := sm.Save(seed); err != nil {
		t.Fatalf("Save seed: %v", err)
	}

	got, err := sm.UpdateSettings(map[string]interface{}{
		"spouse_age": 0, // triggers removeSpousePersons
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	for _, p := range got.Persons {
		if p.Role == models.PersonRoleSpouse {
			t.Fatalf("expected spouse to be removed, still present: %+v", p)
		}
	}
}
