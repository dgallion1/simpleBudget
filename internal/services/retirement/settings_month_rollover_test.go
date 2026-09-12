package retirement

import (
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"
)

func TestCurrentMonthBirthdayAndYearRollover(t *testing.T) {
	s := &models.WhatIfSettings{UseCurrentMonth: true, Persons: []models.Person{
		{ID: "you", Role: models.PersonRolePrimary, BirthMonth: "1960-09"},
		{ID: "spouse", Role: models.PersonRoleSpouse, BirthMonth: "1962-12"},
	}, HealthcarePersons: []models.HealthcarePerson{{PersonID: "you"}}}
	for _, tc := range []struct {
		month           string
		primary, spouse int
	}{
		{"2026-08", 65, 63}, {"2026-09", 66, 63}, {"2026-12", 66, 64}, {"2027-01", 66, 64},
	} {
		now, err := time.Parse("2006-01", tc.month)
		if err != nil {
			t.Fatal(err)
		}
		resolveCurrentMonth(s, now)
		if s.StartDate != tc.month || s.CurrentAge != tc.primary || s.SpouseAge != tc.spouse {
			t.Fatalf("%s: start=%s ages=(%d,%d)", tc.month, s.StartDate, s.CurrentAge, s.SpouseAge)
		}
		if s.HealthcarePersons[0].CurrentAge != tc.primary {
			t.Fatal("linked healthcare age did not advance")
		}
	}
	if s.Persons[0].BirthMonth != "1960-09" || s.Persons[1].BirthMonth != "1962-12" {
		t.Fatal("birth months changed")
	}
}

func TestCurrentMonthExplicitFixedDateSurvivesLoad(t *testing.T) {
	sm := &SettingsManager{}
	s, _, err := sm.decodeSettings([]byte(`{"use_current_month":false,"start_date":"2020-01","persons":[{"id":"you","name":"You","role":"primary","birth_month":"1960-09"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	sm.cache = s
	got, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.StartDate != "2020-01" || got.CurrentAge != 59 {
		t.Fatalf("fixed plan changed: %s age %d", got.StartDate, got.CurrentAge)
	}
}

func TestCurrentMonthLegacyBirthMonthPreserved(t *testing.T) {
	sm := &SettingsManager{}
	s, _, err := sm.decodeSettings([]byte(`{"start_date":"2020-01","current_age":60,"spouse_age":58}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.GetPrimaryPerson().BirthMonth != "1960-01" || s.GetSpousePerson().BirthMonth != "1962-01" {
		t.Fatal("legacy birth months derived against current date")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := sm.decodeSettings(data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetPrimaryPerson().BirthMonth != "1960-01" || !loaded.UseCurrentMonth {
		t.Fatal("reload lost migrated settings")
	}
}
