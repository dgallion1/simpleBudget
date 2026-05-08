package prepare

import (
	"testing"

	"budget2/internal/models"
)

func TestValidatePersons(t *testing.T) {
	validBase := func() *models.WhatIfSettings {
		return &models.WhatIfSettings{
			StartDate: "2026-04",
			Persons: []models.Person{
				{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: models.PersonRolePrimary},
			},
		}
	}

	t.Run("valid single primary", func(t *testing.T) {
		if err := ValidatePersons(validBase()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid primary and spouse", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, models.Person{
			ID: "s1", Name: "Casey", BirthMonth: "1962-04", Role: models.PersonRoleSpouse,
		})
		if err := ValidatePersons(s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid start_date", func(t *testing.T) {
		s := validBase()
		s.StartDate = "bad"
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for invalid start_date")
		}
	})

	t.Run("empty persons", func(t *testing.T) {
		s := validBase()
		s.Persons = nil
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for empty persons")
		}
	})

	t.Run("missing primary", func(t *testing.T) {
		s := validBase()
		s.Persons[0].Role = models.PersonRoleSpouse
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for missing primary")
		}
	})

	t.Run("duplicate primary", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, models.Person{
			ID: "p2", Name: "Other", BirthMonth: "1965-01", Role: models.PersonRolePrimary,
		})
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for duplicate primary")
		}
	})

	t.Run("multiple spouses", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons,
			models.Person{ID: "s1", Name: "Spouse1", BirthMonth: "1962-01", Role: models.PersonRoleSpouse},
			models.Person{ID: "s2", Name: "Spouse2", BirthMonth: "1963-01", Role: models.PersonRoleSpouse},
		)
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for multiple spouses")
		}
	})

	t.Run("duplicate IDs", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, models.Person{
			ID: "p1", Name: "Dup", BirthMonth: "1970-01", Role: models.PersonRoleSpouse,
		})
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for duplicate IDs")
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		s := validBase()
		s.Persons[0].ID = ""
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for empty ID")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		s := validBase()
		s.Persons[0].Name = ""
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for empty name")
		}
	})

	t.Run("invalid birth_month", func(t *testing.T) {
		s := validBase()
		s.Persons[0].BirthMonth = "not-a-date"
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for invalid birth_month")
		}
	})

	t.Run("birth_month after start_date", func(t *testing.T) {
		s := validBase()
		s.Persons[0].BirthMonth = "2026-05"
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for future birth_month")
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, models.Person{
			ID: "x1", Name: "X", BirthMonth: "1970-01", Role: "invalid",
		})
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for invalid role")
		}
	})

	t.Run("healthcare link to missing person", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []models.HealthcarePerson{{ID: "hp1", PersonID: "nonexistent"}}
		if err := ValidatePersons(s); err == nil {
			t.Fatal("expected error for healthcare link to missing person")
		}
	})

	t.Run("healthcare link to valid person", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []models.HealthcarePerson{{ID: "hp1", PersonID: "p1"}}
		if err := ValidatePersons(s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unlinked healthcare passes", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []models.HealthcarePerson{{ID: "hp1", PersonID: "", Name: "Manual", CurrentAge: 60}}
		if err := ValidatePersons(s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
