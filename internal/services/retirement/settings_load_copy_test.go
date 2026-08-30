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

// TestLoadReturnsAPrivateCopy is the enforcement point for the invariant that
// the previous fix could only state as a doc comment: the manager's cached
// settings object must never escape through Load, so no caller can mutate
// state that a concurrent reader is marshaling.
//
// Two Load calls returning the same pointer is the whole failure mode. It is
// asserted directly rather than through a handler because the hazard is not
// any particular handler — it is the next one somebody writes from the
// Load-then-mutate pattern, in a path no test covers.
func TestLoadReturnsAPrivateCopy(t *testing.T) {
	sm := newAgedManager(t)

	first, err := sm.Load()
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	second, err := sm.Load()
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if first == second {
		t.Fatal("two Load calls returned the same pointer: the cached object escaped the manager")
	}
	if first.SpendingPhaseConfig == second.SpendingPhaseConfig {
		t.Fatal("two Load calls aliased SpendingPhaseConfig")
	}
}

// TestLoadResultMutationIsNotVisibleToTheNextLoad states the same invariant in
// the form a caller experiences it: an unsaved edit to a Load result is that
// caller's alone. Nothing else observes it until Save publishes it.
func TestLoadResultMutationIsNotVisibleToTheNextLoad(t *testing.T) {
	sm := newAgedManager(t)

	mine, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	beforePhases := len(mine.SpendingPhaseConfig.Phases)
	beforePortfolio := mine.PortfolioValue

	mine.PortfolioValue = 1234567
	mine.SpendingPhaseConfig.Phases = append(mine.SpendingPhaseConfig.Phases, models.SpendingPhase{
		Name: "Extra", StartAge: 95, Multiplier: 0.5,
	})

	next, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after mutation: %v", err)
	}
	if next.PortfolioValue != beforePortfolio {
		t.Fatalf("unsaved mutation leaked into the next Load: PortfolioValue %v, want %v",
			next.PortfolioValue, beforePortfolio)
	}
	if got := len(next.SpendingPhaseConfig.Phases); got != beforePhases {
		t.Fatalf("unsaved mutation leaked into the next Load: %d phases, want %d", got, beforePhases)
	}
}

// TestLoadFromDiskReturnsAPrivateCopy covers the cache-MISS return point
// specifically, which no other test in this file reaches: they all seed
// through Save, which populates sm.cache, so they enter Load through the
// cache-hit branch and would keep passing if the fresh-decode branch
// published and returned the same object. InvalidateCache forces the decode.
//
// The guard has to be mutation visibility, not pointer distinctness. If the
// cache-miss branch returned the object it published, that object and the
// NEXT Load's copy would still be different pointers — so a pointer
// comparison sees nothing wrong. What gives it away is that mutating the
// first result is then visible to the second.
func TestLoadFromDiskReturnsAPrivateCopy(t *testing.T) {
	sm := newAgedManager(t)
	sm.InvalidateCache()

	fromDisk, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after InvalidateCache: %v", err)
	}
	beforePhases := len(fromDisk.SpendingPhaseConfig.Phases)
	beforePortfolio := fromDisk.PortfolioValue

	fromDisk.PortfolioValue = 1234567
	fromDisk.SpendingPhaseConfig.Phases = append(fromDisk.SpendingPhaseConfig.Phases, models.SpendingPhase{
		Name: "Extra", StartAge: 95, Multiplier: 0.5,
	})

	next, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if next.PortfolioValue != beforePortfolio {
		t.Fatalf("the cache-miss branch published the object it returned: PortfolioValue %v, want %v",
			next.PortfolioValue, beforePortfolio)
	}
	if got := len(next.SpendingPhaseConfig.Phases); got != beforePhases {
		t.Fatalf("the cache-miss branch published the object it returned: %d phases, want %d",
			got, beforePhases)
	}
}

// TestLoadDoesNotReturnTheObjectSavePublished closes the other door into the
// cache: Save stores the very object it is handed, so a Load that returned the
// cache would hand the saver's own pointer straight back to every other
// caller.
func TestLoadDoesNotReturnTheObjectSavePublished(t *testing.T) {
	sm := newAgedManager(t)

	published := models.DefaultWhatIfSettings()
	published.StartDate = "2026-01"
	published.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 61)},
	}
	published.PortfolioValue = 750000
	if err := sm.Save(published); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == published {
		t.Fatal("Load returned the object Save published: the cached object escaped the manager")
	}
	if loaded.PortfolioValue != 750000 {
		t.Fatalf("Load lost the saved value: PortfolioValue %v, want 750000", loaded.PortfolioValue)
	}
}

// TestLoadPreservesAgesAcrossRoundTrip is the field-drop guard from the fix's
// own hazard: prepare.DeepCopy would hand back zeroed CurrentAge/SpouseAge,
// and anything reading them between the load and the save (validateChainInternal
// does) would silently compute against age 0.
func TestLoadPreservesAgesAcrossRoundTrip(t *testing.T) {
	sm := newAgedManager(t)

	mine, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mine.CurrentAge != 61 {
		t.Fatalf("Load dropped CurrentAge: got %d, want 61", mine.CurrentAge)
	}
	if mine.SpouseAge != 59 {
		t.Fatalf("Load dropped SpouseAge: got %d, want 59", mine.SpouseAge)
	}

	// ...and the ages survive the save the handler performs next, so the
	// object the manager republishes is not age-zeroed either.
	//
	// NOTE: the post-save age assertion below is documentation, NOT the guard.
	// saveInternal calls prepare.ComputeAges, which re-derives
	// CurrentAge/SpouseAge from Persons + StartDate on every save, so it holds
	// whether or not Clone carries the ages. The check that actually catches a
	// dropped field is the pre-save one above, at the point a handler reads the
	// ages between Load and Save (validateChainInternal does exactly that). Do
	// not delete the pre-save check on the grounds that this one covers it.
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

// TestLoadPreservesPerYearOverrides covers the json:"-" field that nothing
// re-derives: unlike the ages, ComputeAges cannot bring PerYearOverrides back,
// so losing it in the copy would lose it for good.
func TestLoadPreservesPerYearOverrides(t *testing.T) {
	sm := newAgedManager(t)

	// Seed through Save rather than by mutating what Load returned: a Load
	// result is private to its caller, so a mutation that is never saved is
	// not a seed at all.
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

	mine, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mine.RothConversion == nil {
		t.Fatal("Load dropped RothConversion")
	}
	if got := mine.RothConversion.PerYearOverrides[3]; got != 12000 {
		t.Fatalf("Load dropped PerYearOverrides: got %v, want 12000", got)
	}

	// The map must be copied, not aliased, or a writer editing its own copy
	// would still be writing to state a concurrent reader holds.
	mine.RothConversion.PerYearOverrides[3] = 1
	other, err := sm.Load()
	if err != nil {
		t.Fatalf("Load after mutation: %v", err)
	}
	if got := other.RothConversion.PerYearOverrides[3]; got != 12000 {
		t.Fatalf("Load aliased PerYearOverrides: another caller's map now %v", got)
	}
}

// TestMutatorReturnDoesNotAliasCache extends the Load invariant above to the
// ~20 SettingsManager mutators (AddIncomeSource and friends): each loads,
// mutates, calls saveInternalAndBump — which publishes the mutated object as
// sm.cache — and used to return that same object to its caller. A caller
// mutating its return value was therefore mutating sm.cache directly, the
// same hazard TestLoadReturnsAPrivateCopy guards for Load, just reachable
// from every mutator instead of only from Load.
//
// A table over three mutators from different families exercises the fix
// (return prepare.Clone(settings) instead of settings) across the family
// rather than trusting one representative to stand in for all ~20: income
// (AddIncomeSource), big-ticket (AddBigTicketItem), and a multi-field update
// (UpdateSpendingPhases, which sets both Enabled and Phases in one call).
// Each case mutates a field that the mutator under test just wrote — not an
// unrelated canary — so a failure also demonstrates that specific mutator's
// aliasing hazard, not a generic one.
func TestMutatorReturnDoesNotAliasCache(t *testing.T) {
	cases := []struct {
		name string
		// call invokes the mutator under test and returns its result.
		call func(sm *SettingsManager) (*models.WhatIfSettings, error)
		// mutate corrupts the returned object using a field this mutator
		// just wrote.
		mutate func(result *models.WhatIfSettings)
		// verify asserts the given settings (a fresh Load, or the second
		// call's return value) still show the pre-mutation state.
		verify func(t *testing.T, s *models.WhatIfSettings)
		// second re-invokes the mutator family in a way that does not
		// itself overwrite the field mutate touched, so a leaked mutation
		// would still be visible in its return value.
		second func(sm *SettingsManager) (*models.WhatIfSettings, error)
	}{
		{
			name: "AddIncomeSource",
			call: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.AddIncomeSource(models.IncomeSource{ID: "inc-1", Name: "Pension"})
			},
			mutate: func(result *models.WhatIfSettings) {
				result.IncomeSources[0].Name = "CORRUPTED"
			},
			verify: func(t *testing.T, s *models.WhatIfSettings) {
				t.Helper()
				if len(s.IncomeSources) == 0 || s.IncomeSources[0].Name != "Pension" {
					t.Fatalf("mutating the return value leaked into the manager's state: "+
						"IncomeSources = %+v, want [0].Name = \"Pension\"", s.IncomeSources)
				}
			},
			second: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.AddIncomeSource(models.IncomeSource{ID: "inc-2", Name: "Annuity"})
			},
		},
		{
			name: "AddBigTicketItem",
			call: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.AddBigTicketItem(models.BigTicketItem{ID: "bt-1", Name: "Roof", Amount: 20000, Year: 2, Type: models.BigTicketExpense})
			},
			mutate: func(result *models.WhatIfSettings) {
				result.BigTicketItems[0].Amount = 999999999
			},
			verify: func(t *testing.T, s *models.WhatIfSettings) {
				t.Helper()
				if len(s.BigTicketItems) == 0 || s.BigTicketItems[0].Amount != 20000 {
					t.Fatalf("mutating the return value leaked into the manager's state: "+
						"BigTicketItems = %+v, want [0].Amount = 20000", s.BigTicketItems)
				}
			},
			second: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.AddBigTicketItem(models.BigTicketItem{ID: "bt-2", Name: "Car", Amount: 30000, Year: 4, Type: models.BigTicketExpense})
			},
		},
		{
			name: "UpdateSpendingPhases",
			call: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.UpdateSpendingPhases(true, []models.SpendingPhase{
					{Name: "Go-Go", StartAge: 61, Multiplier: 1.0},
					{Name: "Slow-Go", StartAge: 75, Multiplier: 0.85},
				})
			},
			mutate: func(result *models.WhatIfSettings) {
				// A multi-field mutator: corrupt both fields it just set.
				result.SpendingPhaseConfig.Enabled = false
				result.SpendingPhaseConfig.Phases[0].Multiplier = 0.01
			},
			verify: func(t *testing.T, s *models.WhatIfSettings) {
				t.Helper()
				if !s.SpendingPhaseConfig.Enabled {
					t.Fatalf("mutating the return value leaked into the manager's state: " +
						"SpendingPhaseConfig.Enabled = false, want true")
				}
				if got := s.SpendingPhaseConfig.Phases[0].Multiplier; got != 1.0 {
					t.Fatalf("mutating the return value leaked into the manager's state: "+
						"Phases[0].Multiplier = %v, want 1.0", got)
				}
			},
			// nil phases: UpdateSpendingPhases only overwrites Phases when
			// len(phases) > 0, so this second call reads Phases straight from
			// whatever the manager published — a mutation leaked from the
			// first call's return value would still show up here.
			second: func(sm *SettingsManager) (*models.WhatIfSettings, error) {
				return sm.UpdateSpendingPhases(true, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm := newAgedManager(t)

			result, err := tc.call(sm)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			// Mutate the object the mutator handed back.
			tc.mutate(result)

			// A fresh Load reads the manager's own published state, not the
			// caller's copy. If the mutator had returned the cached object
			// itself, this mutation would be visible here.
			fresh, err := sm.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			tc.verify(t, fresh)

			// Same check from a second call instead of Load, so the guard is
			// not accidentally specific to Load's own copy-on-return path.
			again, err := tc.second(sm)
			if err != nil {
				t.Fatalf("second call: %v", err)
			}
			tc.verify(t, again)
		})
	}
}
