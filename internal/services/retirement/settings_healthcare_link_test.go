package retirement

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// liveShapedWhatIfFixture mirrors the real saved plan shape from CM1: two
// persons linked by birth month only, two healthcare entries carrying the
// user's real names and ages but no person_id, and no use_current_month
// field (a pre-F-067/CM1 legacy save).
const liveShapedWhatIfFixture = `{
	"start_date": "2026-04",
	"persons": [
		{"id": "you", "name": "You", "role": "primary", "birth_month": "1958-11"},
		{"id": "spouse", "name": "Spouse", "role": "spouse", "birth_month": "1971-08"}
	],
	"healthcare_persons": [
		{"id": "hc1", "name": "Darrell Gallion", "current_age": 67, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65},
		{"id": "hc2", "name": "Christine", "current_age": 54, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65}
	]
}`

func loadFixtureSettings(t *testing.T, fixture string) (*models.WhatIfSettings, *SettingsManager, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	path := filepath.Join(root, "whatif.json")
	if err := store.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sm := NewSettingsManager(root, store)
	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return settings, sm, path
}

// m1: positional link inference must fire for the live-shaped fixture: both
// healthcare entries end linked, their ages match the corresponding
// person's age at the current month, and use_current_month is persisted to
// disk (proves the migration path, not just an in-memory mutation).
func TestPositionalHealthcareLinkInference_LiveShapedFixture(t *testing.T) {
	settings, sm, path := loadFixtureSettings(t, liveShapedWhatIfFixture)

	you := settings.GetPrimaryPerson()
	spouse := settings.GetSpousePerson()
	if you == nil || spouse == nil {
		t.Fatalf("expected primary and spouse persons, got %+v", settings.Persons)
	}

	darrell := findHealthcarePersonByOriginalName(t, settings, "hc1")
	christine := findHealthcarePersonByOriginalName(t, settings, "hc2")

	if darrell.PersonID != you.ID {
		t.Fatalf("Darrell Gallion healthcare entry PersonID = %q, want primary %q", darrell.PersonID, you.ID)
	}
	if christine.PersonID != spouse.ID {
		t.Fatalf("Christine healthcare entry PersonID = %q, want spouse %q", christine.PersonID, spouse.ID)
	}

	month := time.Now().Format("2006-01")
	wantYouAge, err := models.DeriveAgeAtStartDate(month, you.BirthMonth)
	if err != nil {
		t.Fatal(err)
	}
	wantSpouseAge, err := models.DeriveAgeAtStartDate(month, spouse.BirthMonth)
	if err != nil {
		t.Fatal(err)
	}
	if darrell.CurrentAge != wantYouAge {
		t.Fatalf("Darrell Gallion healthcare age = %d, want %d (matches primary person age)", darrell.CurrentAge, wantYouAge)
	}
	if christine.CurrentAge != wantSpouseAge {
		t.Fatalf("Christine healthcare age = %d, want %d (matches spouse person age)", christine.CurrentAge, wantSpouseAge)
	}

	raw, err := sm.store.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var onDisk map[string]interface{}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("json.Unmarshal on-disk settings: %v", err)
	}
	if useCurrentMonth, ok := onDisk["use_current_month"].(bool); !ok || !useCurrentMonth {
		t.Fatalf("use_current_month not persisted true on disk: %v", onDisk["use_current_month"])
	}
}

// m2: the age guard must block a positional link when the sole unlinked
// healthcare entry's age disagrees with the sole unlinked person's age at
// the original saved start date, even though counts match 1:1.
func TestPositionalHealthcareLinkInference_AgeMismatchStaysUnlinked(t *testing.T) {
	fixture := `{
		"start_date": "2026-04",
		"persons": [
			{"id": "you", "name": "You", "role": "primary", "birth_month": "1958-11"}
		],
		"healthcare_persons": [
			{"id": "hc1", "name": "Alpha", "current_age": 40, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65}
		]
	}`
	settings, _, _ := loadFixtureSettings(t, fixture)

	alpha := findHealthcarePersonByOriginalName(t, settings, "hc1")
	if alpha.PersonID != "" {
		t.Fatalf("age-mismatched entry got linked: PersonID = %q", alpha.PersonID)
	}
	if alpha.CurrentAge != 40 {
		t.Fatalf("age-mismatched entry's age changed: got %d, want unchanged 40", alpha.CurrentAge)
	}
}

// m3: the count / all-or-nothing guard must block positional linking when
// unlinked healthcare entries and unlinked persons don't pair 1:1 — three
// entries for two persons must all stay unlinked.
func TestPositionalHealthcareLinkInference_CountMismatchStaysUnlinked(t *testing.T) {
	fixture := `{
		"start_date": "2026-04",
		"persons": [
			{"id": "you", "name": "You", "role": "primary", "birth_month": "1958-11"},
			{"id": "spouse", "name": "Spouse", "role": "spouse", "birth_month": "1971-08"}
		],
		"healthcare_persons": [
			{"id": "hc1", "name": "Darrell Gallion", "current_age": 67, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65},
			{"id": "hc2", "name": "Christine", "current_age": 54, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65},
			{"id": "hc3", "name": "Extra", "current_age": 30, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65}
		]
	}`
	settings, _, _ := loadFixtureSettings(t, fixture)

	for _, id := range []string{"hc1", "hc2", "hc3"} {
		hc := findHealthcarePersonByOriginalName(t, settings, id)
		if hc.PersonID != "" {
			t.Fatalf("healthcare entry %q got linked despite count mismatch: PersonID = %q", id, hc.PersonID)
		}
	}
}

// m4: linked entries must carry the person's birth month (F-067
// month-precise ACA→Medicare transition), not just the derived age.
func TestPositionalHealthcareLinkInference_MirrorsBirthMonth(t *testing.T) {
	settings, _, _ := loadFixtureSettings(t, liveShapedWhatIfFixture)

	you := settings.GetPrimaryPerson()
	spouse := settings.GetSpousePerson()
	darrell := findHealthcarePersonByOriginalName(t, settings, "hc1")
	christine := findHealthcarePersonByOriginalName(t, settings, "hc2")

	if darrell.BirthMonth != you.BirthMonth {
		t.Fatalf("Darrell Gallion healthcare BirthMonth = %q, want %q", darrell.BirthMonth, you.BirthMonth)
	}
	if christine.BirthMonth != spouse.BirthMonth {
		t.Fatalf("Christine healthcare BirthMonth = %q, want %q", christine.BirthMonth, spouse.BirthMonth)
	}
}

// m5: positional linking must adopt the healthcare entry's real name onto a
// placeholder person ("You"/"Spouse"), and the healthcare entry's own name
// must survive the load unchanged (not regress to "You"/"Spouse" via the
// Name-sync in prepare.ComputeAges).
func TestPositionalHealthcareLinkInference_KeepsUserNames(t *testing.T) {
	settings, _, _ := loadFixtureSettings(t, liveShapedWhatIfFixture)

	you := settings.GetPrimaryPerson()
	spouse := settings.GetSpousePerson()
	darrell := findHealthcarePersonByOriginalName(t, settings, "hc1")
	christine := findHealthcarePersonByOriginalName(t, settings, "hc2")

	if you.Name != "Darrell Gallion" {
		t.Fatalf("primary person Name = %q, want adopted %q", you.Name, "Darrell Gallion")
	}
	if spouse.Name != "Christine" {
		t.Fatalf("spouse person Name = %q, want adopted %q", spouse.Name, "Christine")
	}
	if darrell.Name != "Darrell Gallion" {
		t.Fatalf("healthcare entry hc1 Name = %q, want unchanged %q", darrell.Name, "Darrell Gallion")
	}
	if christine.Name != "Christine" {
		t.Fatalf("healthcare entry hc2 Name = %q, want unchanged %q", christine.Name, "Christine")
	}
}

// m6: the age guard must require EXACT agreement, not an off-by-one
// tolerance. One pair off by exactly one year must block the whole
// all-or-nothing positional link, leaving both entries unlinked with their
// ages and names untouched, even though the other pair matches exactly.
func TestPositionalHealthcareLinkInference_OffByOneStaysUnlinked(t *testing.T) {
	fixture := `{
		"start_date": "2026-04",
		"persons": [
			{"id": "you", "name": "You", "role": "primary", "birth_month": "1958-11"},
			{"id": "spouse", "name": "Spouse", "role": "spouse", "birth_month": "1971-08"}
		],
		"healthcare_persons": [
			{"id": "hc1", "name": "Alpha", "current_age": 66, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65},
			{"id": "hc2", "name": "Beta", "current_age": 54, "current_coverage": "aca", "current_monthly_cost": 900, "pre_medicare_inflation": 5, "medicare_monthly_cost": 400, "post_medicare_inflation": 3, "medicare_eligible_age": 65}
		]
	}`
	settings, _, _ := loadFixtureSettings(t, fixture)

	alpha := findHealthcarePersonByOriginalName(t, settings, "hc1")
	beta := findHealthcarePersonByOriginalName(t, settings, "hc2")

	if alpha.PersonID != "" {
		t.Fatalf("off-by-one entry got linked: PersonID = %q", alpha.PersonID)
	}
	if beta.PersonID != "" {
		t.Fatalf("exact-match entry got linked despite sibling mismatch: PersonID = %q", beta.PersonID)
	}
	if alpha.CurrentAge != 66 {
		t.Fatalf("off-by-one entry's age changed: got %d, want unchanged 66", alpha.CurrentAge)
	}
	if beta.CurrentAge != 54 {
		t.Fatalf("exact-match entry's age changed: got %d, want unchanged 54", beta.CurrentAge)
	}
	if alpha.Name != "Alpha" {
		t.Fatalf("off-by-one entry's name changed: got %q, want unchanged %q", alpha.Name, "Alpha")
	}
	if beta.Name != "Beta" {
		t.Fatalf("exact-match entry's name changed: got %q, want unchanged %q", beta.Name, "Beta")
	}
}

func findHealthcarePersonByOriginalName(t *testing.T, settings *models.WhatIfSettings, id string) models.HealthcarePerson {
	t.Helper()
	for _, hc := range settings.HealthcarePersons {
		if hc.ID == id {
			return hc
		}
	}
	t.Fatalf("healthcare person id %q not found in %+v", id, settings.HealthcarePersons)
	return models.HealthcarePerson{}
}
