package prepare

import (
	"fmt"
	"strings"

	"budget2/internal/models"
)

// ValidatePersons checks the settings' Persons slice for the invariants the
// retirement engine relies on:
//
//   - StartDate parses
//   - at least one person
//   - person IDs are non-empty and unique
//   - person Names are non-empty
//   - BirthMonth parses and is no later than StartDate
//   - exactly one PersonRolePrimary
//   - at most one PersonRoleSpouse
//   - HealthcarePerson entries reference valid Person IDs (or are unlinked)
func ValidatePersons(s *models.WhatIfSettings) error {
	start, err := models.ParseYearMonth(s.StartDate)
	if err != nil {
		return fmt.Errorf("start_date: %w", err)
	}
	if len(s.Persons) == 0 {
		return fmt.Errorf("persons: at least one person is required")
	}

	primaryCount := 0
	spouseCount := 0
	ids := make(map[string]struct{}, len(s.Persons))
	for _, person := range s.Persons {
		if strings.TrimSpace(person.ID) == "" {
			return fmt.Errorf("persons: id is required")
		}
		if _, exists := ids[person.ID]; exists {
			return fmt.Errorf("persons: duplicate id %q", person.ID)
		}
		ids[person.ID] = struct{}{}

		if strings.TrimSpace(person.Name) == "" {
			return fmt.Errorf("persons: name is required")
		}
		birth, err := models.ParseYearMonth(person.BirthMonth)
		if err != nil {
			return fmt.Errorf("persons: invalid birth_month for %q: %w", person.Name, err)
		}
		if birth.After(start) {
			return fmt.Errorf("persons: birth_month %q is after start_date %q", person.BirthMonth, s.StartDate)
		}

		switch person.Role {
		case models.PersonRolePrimary:
			primaryCount++
		case models.PersonRoleSpouse:
			spouseCount++
		case models.PersonRoleOther:
		default:
			return fmt.Errorf("persons: invalid role %q", person.Role)
		}
	}

	if primaryCount != 1 {
		return fmt.Errorf("persons: expected exactly one primary person, got %d", primaryCount)
	}
	if spouseCount > 1 {
		return fmt.Errorf("persons: expected at most one spouse person, got %d", spouseCount)
	}

	for _, hp := range s.HealthcarePersons {
		if hp.PersonID == "" {
			continue
		}
		if _, ok := ids[hp.PersonID]; !ok {
			return fmt.Errorf("healthcare_persons: person_id %q not found", hp.PersonID)
		}
	}

	return nil
}

// ValidateOneTimeExpenses checks the settings' OneTimeExpenses slice for the
// invariants the retirement engine relies on:
//
//   - Amount is non-negative
//   - Year is non-negative
//
// Year is deliberately NOT bounded against ProjectionYears here. An entry
// whose Year >= ProjectionYears is DORMANT, not invalid: the engine never
// charges it (OneTimeExpensesForYear only fires within the projection loop,
// which never reaches that year), so the projection runs and renders
// normally. This lets ProjectionYears shrink underneath an existing entry
// (settings page, MCP apply_changes, or any other writer) without bricking
// prepare.From on every subsequent load. The add-handler still rejects a
// beyond-horizon entry at submit time as a likely typo, but that is a
// handler-level UX check, not a shared invariant — see
// handleWhatIfAddOneTime.
//
// An empty or absent list is always valid.
func ValidateOneTimeExpenses(s *models.WhatIfSettings) error {
	for i, e := range s.OneTimeExpenses {
		if e.Amount < 0 {
			return fmt.Errorf("one_time_expenses[%d] %q: amount must be non-negative, got %v", i, e.Description, e.Amount)
		}
		if e.Year < 0 {
			return fmt.Errorf("one_time_expenses[%d] %q: year must be non-negative, got %d", i, e.Description, e.Year)
		}
	}
	return nil
}
