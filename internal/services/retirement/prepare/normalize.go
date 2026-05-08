package prepare

import (
	"budget2/internal/models"
)

// ComputeAges derives CurrentAge and SpouseAge from StartDate plus the
// matching primary/spouse Person's BirthMonth, and refreshes any linked
// HealthcarePerson entries.
//
// Tolerant: if a date can't be parsed, the corresponding age is left
// untouched (CurrentAge) or zeroed (SpouseAge) so that callers can still
// work with partially-valid input. Validation belongs in ValidatePersons.
func ComputeAges(s *models.WhatIfSettings) {
	if primary := s.GetPrimaryPerson(); primary != nil {
		if age, err := models.DeriveAgeAtStartDate(s.StartDate, primary.BirthMonth); err == nil {
			s.CurrentAge = age
		}
	}

	s.SpouseAge = 0
	if spouse := s.GetSpousePerson(); spouse != nil {
		if age, err := models.DeriveAgeAtStartDate(s.StartDate, spouse.BirthMonth); err == nil {
			s.SpouseAge = age
		}
	}

	for i := range s.HealthcarePersons {
		if s.HealthcarePersons[i].PersonID == "" {
			continue
		}
		person := s.FindPerson(s.HealthcarePersons[i].PersonID)
		if person == nil {
			continue
		}
		s.HealthcarePersons[i].Name = person.Name
		if age, err := models.DeriveAgeAtStartDate(s.StartDate, person.BirthMonth); err == nil {
			s.HealthcarePersons[i].CurrentAge = age
		}
	}
}

// NormalizePhaseAgeReference coerces s.PhaseAgeReference to one of the
// recognized values. Unknown values default to "older". The "spouse"
// reference falls back to "older" when the settings lack a spouse Person.
func NormalizePhaseAgeReference(s *models.WhatIfSettings) {
	switch s.PhaseAgeReference {
	case "younger", "older", "primary":
		return
	case "spouse":
		if s.HasSpouse() {
			return
		}
	}
	s.PhaseAgeReference = "older"
}
