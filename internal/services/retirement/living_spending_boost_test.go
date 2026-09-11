package retirement

import (
	"budget2/internal/models"
	"testing"
)

// Ordinary settings saves and scenario switches must retain the optional boost.
func TestLivingSpendingBoostSettingsPersistence(t *testing.T) {
	sm := newTestSM(t)
	s, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000.01, StopMonth: "2036-09"}
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := sm.CreateScenario("Boost copy"); err != nil {
		t.Fatal(err)
	}
	other := sm.ActiveFilename()
	for _, scenario := range []string{"whatif.json", other, "whatif.json"} {
		if err := sm.SwitchScenario(scenario); err != nil {
			t.Fatal(err)
		}
		s, err = sm.Load()
		if err != nil {
			t.Fatal(err)
		}
		if s.LivingSpendingBoost == nil || s.LivingSpendingBoost.MonthlyReal != 1000.01 || s.LivingSpendingBoost.StopMonth != "2036-09" {
			t.Fatalf("scenario %s lost boost: %+v", scenario, s.LivingSpendingBoost)
		}
		s.MonthlyLivingExpenses += 1
		if err := sm.Save(s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLivingSpendingBoostOrdinaryFormSave(t *testing.T) {
	sm := newTestSM(t)
	s, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.StartDate = "2026-09"
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2036-09"}
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}
	updated, _, err := sm.UpdateSettingsWithPersons(map[string]interface{}{"monthly_living_expenses": 11000.0}, "2035-09", s.Persons)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LivingSpendingBoost == nil || updated.LivingSpendingBoost.MonthlyReal != 1000 || updated.LivingSpendingBoost.StopMonth != "2036-09" {
		t.Fatalf("ordinary form save reset schedule: %+v", updated.LivingSpendingBoost)
	}
}
