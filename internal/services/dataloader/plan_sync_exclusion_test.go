package dataloader

import (
	"testing"

	"budget2/internal/models"
)

// ExcludeFromPlanSync must round-trip through Add/Save/Load exactly like the
// rest of MajorExpense's fields (D-SY-a).
func TestExcludeFromPlanSync_AddSaveLoadRoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{
		ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{
		ID: "gym", Name: "Gym", Keywords: []string{"gym"},
	}); err != nil {
		t.Fatalf("add gym: %v", err)
	}

	list, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	byID := make(map[string]models.MajorExpense, len(list))
	for _, me := range list {
		byID[me.ID] = me
	}
	if !byID["car"].ExcludeFromPlanSync {
		t.Errorf("car loan entry should round-trip ExcludeFromPlanSync=true, got %+v", byID["car"])
	}
	if byID["gym"].ExcludeFromPlanSync {
		t.Errorf("gym entry should default ExcludeFromPlanSync=false, got %+v", byID["gym"])
	}
}

// SaveMajorExpenses/LoadMajorExpenses is the lower-level persistence path
// AddMajorExpense sits on top of; it must round-trip the flag too.
func TestExcludeFromPlanSync_SaveLoadRoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	in := []models.MajorExpense{
		{ID: "x", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true},
	}
	if err := loader.SaveMajorExpenses(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || !out[0].ExcludeFromPlanSync {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

// UpdateMajorExpense's explicit field-copy block must copy
// ExcludeFromPlanSync in both directions: turning it on, and turning it back
// off (a field-copy block that only ORs the value in, or omits it entirely,
// would silently keep a stale exclusion after the user unflags it).
func TestUpdateMajorExpense_CopiesExcludeFromPlanSync(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	if _, err := loader.AddMajorExpense(models.MajorExpense{
		ID: "car", Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Turn the flag ON.
	list, err := loader.UpdateMajorExpense("car", models.MajorExpense{
		Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: true,
	})
	if err != nil {
		t.Fatalf("update on: %v", err)
	}
	if !list[0].ExcludeFromPlanSync {
		t.Fatalf("expected ExcludeFromPlanSync=true after update, got %+v", list[0])
	}

	// Reload from disk to prove it was actually persisted, not just returned.
	reloaded, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded[0].ExcludeFromPlanSync {
		t.Fatalf("expected persisted ExcludeFromPlanSync=true, got %+v", reloaded[0])
	}

	// Turn the flag back OFF.
	list2, err := loader.UpdateMajorExpense("car", models.MajorExpense{
		Name: "Car loan", ExpectedMin: 500, ExpectedMax: 500, ExcludeFromPlanSync: false,
	})
	if err != nil {
		t.Fatalf("update off: %v", err)
	}
	if list2[0].ExcludeFromPlanSync {
		t.Fatalf("expected ExcludeFromPlanSync=false after update, got %+v", list2[0])
	}
}
