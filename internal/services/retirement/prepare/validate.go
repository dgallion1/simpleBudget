package prepare

import (
	"fmt"
	"strings"

	"budget2/internal/models"
)

// InvalidStartDateError reports that WhatIfSettings.StartDate does not
// parse. Retrieve with errors.As. Err is the underlying
// models.ParseYearMonth failure; Error() reproduces the historical
// "start_date: <err>" text byte-for-byte (ruling 2026-09-24i / I1, I4) --
// callers needing to distinguish this from a per-person failure should use
// errors.As, never inspect Error()'s text.
type InvalidStartDateError struct {
	Err error
}

func (e *InvalidStartDateError) Error() string {
	if e == nil || e.Err == nil {
		return "start_date: invalid"
	}
	return fmt.Sprintf("start_date: %v", e.Err)
}

func (e *InvalidStartDateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// PersonBirthMonthError reports that a person's BirthMonth failed to parse
// -- either empty (after TrimSpace) or present but malformed. Retrieve
// with errors.As. PersonID names the failing row by ID, the sole
// identification a caller may use (ruling 2026-09-24i / I1, I2): IDs are
// unique at this point (parsePersonsForm assigns a UUID to every new row,
// and ValidatePersons checks ID uniqueness before it ever reaches a birth
// month), while Name is NOT reliable for identification -- two submitted
// rows may share a Name. Name and Err exist only so Error() can reproduce
// the historical "persons: invalid birth_month for %q: <err>" text; a
// caller wording a user-facing message must look PersonID up in its OWN
// submitted data instead of reading either field here.
type PersonBirthMonthError struct {
	PersonID string
	Name     string
	Err      error
}

func (e *PersonBirthMonthError) Error() string {
	if e == nil || e.Err == nil {
		return "persons: invalid birth_month"
	}
	return fmt.Sprintf("persons: invalid birth_month for %q: %v", e.Name, e.Err)
}

func (e *PersonBirthMonthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// PersonBirthMonthAfterStartError reports that a person's BirthMonth
// parsed but falls after WhatIfSettings.StartDate. Retrieve via
// errors.As. PersonID names the failing row by ID (see PersonBirthMonthError
// for why ID, never Name); StartDate is the start date it was compared
// against. BirthMonth exists only so Error() can reproduce the historical
// "persons: birth_month %q is after start_date %q" text.
type PersonBirthMonthAfterStartError struct {
	PersonID   string
	BirthMonth string
	StartDate  string
}

func (e *PersonBirthMonthAfterStartError) Error() string {
	if e == nil {
		return "persons: birth_month is after start_date"
	}
	return fmt.Sprintf("persons: birth_month %q is after start_date %q", e.BirthMonth, e.StartDate)
}

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
//
// The StartDate failure and the two BirthMonth failures are returned as
// typed errors (InvalidStartDateError, PersonBirthMonthError,
// PersonBirthMonthAfterStartError) so a caller can identify the failing
// row by ID via errors.As instead of parsing Error()'s text (ruling
// 2026-09-24i). Every other shape here stays a plain error; its own text
// is already truthful and unambiguous, and no caller needs to single out a
// row for it.
func ValidatePersons(s *models.WhatIfSettings) error {
	start, err := models.ParseYearMonth(s.StartDate)
	if err != nil {
		return &InvalidStartDateError{Err: err}
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
			return &PersonBirthMonthError{PersonID: person.ID, Name: person.Name, Err: err}
		}
		if birth.After(start) {
			return &PersonBirthMonthAfterStartError{PersonID: person.ID, BirthMonth: person.BirthMonth, StartDate: s.StartDate}
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
//
// Month is deliberately NOT constrained at all. A NEGATIVE Month is a PAST,
// dormant entry, not an invalid one: the monthly StartDate rollover shifts
// every schedule offset back as time passes, and an entry whose month has gone
// by keeps its negative offset so the user's data is retained rather than
// deleted. The projection loop starts at month 0 and so never charges it.
//
// Month is likewise NOT bounded against ProjectionYears. An entry whose Month
// is beyond the horizon is DORMANT, not invalid: the engine never charges it
// (OneTimeExpensesForMonth only fires within the projection loop, which never
// reaches that month), so the projection runs and renders normally. This lets
// ProjectionYears shrink underneath an existing entry (settings page, MCP
// apply_changes, or any other writer) without bricking prepare.From on every
// subsequent load. The add-handler still rejects a beyond-horizon entry at
// submit time as a likely typo, but that is a handler-level UX check, not a
// shared invariant — see handleWhatIfAddOneTime.
//
// An empty or absent list is always valid.
func ValidateOneTimeExpenses(s *models.WhatIfSettings) error {
	for i, e := range s.OneTimeExpenses {
		if e.Amount < 0 {
			return fmt.Errorf("one_time_expenses[%d] %q: amount must be non-negative, got %v", i, e.Description, e.Amount)
		}
	}
	return nil
}
