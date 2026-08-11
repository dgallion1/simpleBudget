package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// newAgedManager returns a SettingsManager whose saved plan has a primary
// person aged 61 and a spouse aged 59, so CurrentAge/SpouseAge are non-zero
// and a dropped json:"-" field is visible as a zero.
func newAgedManager(t *testing.T) *SettingsManager {
	t.Helper()

	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(dir, store)

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-01"
	settings.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 61)},
		{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge("2026-01", 59)},
	}
	settings.ProjectionYears = 30
	if err := sm.Save(settings); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return sm
}

// TestLoadForUpdateIsolatesTheCachedObject is the writers-copy invariant
// stated directly: mutating what LoadForUpdate returns must never be visible
// through Load until the mutation is saved.
func TestLoadForUpdateIsolatesTheCachedObject(t *testing.T) {
	sm := newAgedManager(t)

	shared, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := len(shared.SpendingPhaseConfig.Phases)

	mine, err := sm.LoadForUpdate()
	if err != nil {
		t.Fatalf("LoadForUpdate: %v", err)
	}
	if mine == shared {
		t.Fatal("LoadForUpdate returned the shared cached pointer")
	}
	if mine.SpendingPhaseConfig == shared.SpendingPhaseConfig {
		t.Fatal("LoadForUpdate aliased SpendingPhaseConfig with the cached object")
	}

	mine.SpendingPhaseConfig.Phases = append(mine.SpendingPhaseConfig.Phases, models.SpendingPhase{
		Name: "Extra", StartAge: 95, Multiplier: 0.5,
	})
	mine.PortfolioValue = 1234567

	if got := len(shared.SpendingPhaseConfig.Phases); got != before {
		t.Fatalf("mutating the LoadForUpdate copy changed the shared object: phases %d, want %d", got, before)
	}
	if shared.PortfolioValue == 1234567 {
		t.Fatal("mutating the LoadForUpdate copy changed the shared object's PortfolioValue")
	}
}

// TestLoadForUpdatePreservesAgesAcrossRoundTrip is the field-drop guard from
// the fix's own hazard: prepare.DeepCopy would hand back zeroed
// CurrentAge/SpouseAge, and anything reading them between the load and the
// save (validateChainInternal does) would silently compute against age 0.
func TestLoadForUpdatePreservesAgesAcrossRoundTrip(t *testing.T) {
	sm := newAgedManager(t)

	shared, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if shared.CurrentAge != 61 || shared.SpouseAge != 59 {
		t.Fatalf("seed ages wrong: CurrentAge=%d SpouseAge=%d, want 61/59", shared.CurrentAge, shared.SpouseAge)
	}

	mine, err := sm.LoadForUpdate()
	if err != nil {
		t.Fatalf("LoadForUpdate: %v", err)
	}
	if mine.CurrentAge != 61 {
		t.Fatalf("LoadForUpdate dropped CurrentAge: got %d, want 61", mine.CurrentAge)
	}
	if mine.SpouseAge != 59 {
		t.Fatalf("LoadForUpdate dropped SpouseAge: got %d, want 59", mine.SpouseAge)
	}

	// ...and the ages survive the save the handler performs next, so the
	// object the manager republishes is not age-zeroed either.
	//
	// NOTE: the post-save age assertion below is documentation, NOT the guard.
	// saveInternal calls prepare.ComputeAges, which re-derives
	// CurrentAge/SpouseAge from Persons + StartDate on every save, so it holds
	// whether or not Clone carries the ages. The check that actually catches a
	// dropped field is the pre-save one above, at the point a handler reads the
	// ages between LoadForUpdate and Save (validateChainInternal does exactly
	// that). Do not delete the pre-save check on the grounds that this one
	// covers it.
	mine.PortfolioValue = 900000
	if err := sm.Save(mine); err != nil {
		t.Fatalf("Save: %v", err)
	}

	after, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if after.CurrentAge != 61 || after.SpouseAge != 59 {
		t.Fatalf("ages after load-mutate-save: CurrentAge=%d SpouseAge=%d, want 61/59",
			after.CurrentAge, after.SpouseAge)
	}
	if after.PortfolioValue != 900000 {
		t.Fatalf("load-mutate-save did not persist PortfolioValue: got %v, want 900000", after.PortfolioValue)
	}
}

// TestLoadForUpdatePreservesPerYearOverrides covers the json:"-" field that
// nothing re-derives: unlike the ages, ComputeAges cannot bring
// PerYearOverrides back, so losing it in the copy would lose it for good.
func TestLoadForUpdatePreservesPerYearOverrides(t *testing.T) {
	sm := newAgedManager(t)

	// Seed through Save rather than by mutating what Load returned: Load's
	// contract is that its result is read-only, and a test that violated it
	// would be silently coupled to Load returning the shared pointer.
	//
	// Save works even though PerYearOverrides is json:"-" and so never reaches
	// disk: saveInternal publishes the very object it was handed as sm.cache,
	// and the map is still attached to it in memory. That is the same
	// in-memory-only lifetime the Tax Optimizer relies on.
	seed := models.DefaultWhatIfSettings()
	seed.StartDate = "2026-01"
	seed.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 61)},
	}
	seed.RothConversion = &models.RothConversionConfig{
		Enabled:          true,
		AnnualAmount:     50000,
		PerYearOverrides: map[int]float64{3: 12000},
	}
	if err := sm.Save(seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	shared, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if shared.RothConversion == nil || shared.RothConversion.PerYearOverrides[3] != 12000 {
		t.Fatalf("seed did not take: %+v", shared.RothConversion)
	}

	mine, err := sm.LoadForUpdate()
	if err != nil {
		t.Fatalf("LoadForUpdate: %v", err)
	}
	if mine.RothConversion == nil {
		t.Fatal("LoadForUpdate dropped RothConversion")
	}
	if got := mine.RothConversion.PerYearOverrides[3]; got != 12000 {
		t.Fatalf("LoadForUpdate dropped PerYearOverrides: got %v, want 12000", got)
	}

	// The map must be copied, not aliased, or a writer editing its own copy
	// would still be writing to state a concurrent reader holds.
	mine.RothConversion.PerYearOverrides[3] = 1
	if got := shared.RothConversion.PerYearOverrides[3]; got != 12000 {
		t.Fatalf("LoadForUpdate aliased PerYearOverrides: shared map now %v", got)
	}
}
