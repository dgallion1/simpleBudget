package prepare

import (
	"testing"

	"budget2/internal/models"
)

func TestComputeAges_StartDateAndLinkedHealthcare(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate: "2026-04",
		Persons: []models.Person{
			{ID: "primary", Name: "Alex", BirthMonth: "1960-05", Role: models.PersonRolePrimary},
			{ID: "spouse", Name: "Casey", BirthMonth: "1962-04", Role: models.PersonRoleSpouse},
		},
		HealthcarePersons: []models.HealthcarePerson{
			{ID: "hp1", PersonID: "primary"},
		},
	}

	ComputeAges(s)

	if s.CurrentAge != 65 {
		t.Fatalf("CurrentAge = %d, want 65", s.CurrentAge)
	}
	if s.SpouseAge != 64 {
		t.Fatalf("SpouseAge = %d, want 64", s.SpouseAge)
	}
	if s.HealthcarePersons[0].Name != "Alex" {
		t.Fatalf("linked healthcare name = %q, want Alex", s.HealthcarePersons[0].Name)
	}
	if s.HealthcarePersons[0].CurrentAge != 65 {
		t.Fatalf("linked healthcare age = %d, want 65", s.HealthcarePersons[0].CurrentAge)
	}
}

func TestNormalizePhaseAgeReference(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		hasSpouse bool
		want      string
	}{
		{"younger stays", "younger", true, "younger"},
		{"older stays", "older", false, "older"},
		{"primary stays", "primary", false, "primary"},
		{"spouse with spouse stays", "spouse", true, "spouse"},
		{"spouse without spouse normalized", "spouse", false, "older"},
		{"empty defaults to older", "", false, "older"},
		{"unknown defaults to older", "bogus", false, "older"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &models.WhatIfSettings{
				PhaseAgeReference: tt.ref,
				StartDate:         "2026-04",
				Persons: []models.Person{
					{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: models.PersonRolePrimary},
				},
			}
			if tt.hasSpouse {
				s.Persons = append(s.Persons, models.Person{
					ID: "s1", Name: "Casey", BirthMonth: "1962-04", Role: models.PersonRoleSpouse,
				})
				ComputeAges(s)
			}
			NormalizePhaseAgeReference(s)
			if s.PhaseAgeReference != tt.want {
				t.Fatalf("got %q, want %q", s.PhaseAgeReference, tt.want)
			}
		})
	}
}
