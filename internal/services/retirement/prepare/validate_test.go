package prepare

import (
	"errors"
	"strings"
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

// I1/I5(c): the two per-person BirthMonth failures and the StartDate
// failure are retrievable via errors.As and carry the failing row's ID
// (ruling 2026-09-24i) -- the ONLY thing a caller may use to identify the
// row; see personsSaveErrorMessage in internal/handlers/whatif.
func TestValidatePersons_TypedErrors_ErrorsAsAndPersonID(t *testing.T) {
	t.Run("invalid start_date is InvalidStartDateError", func(t *testing.T) {
		s := validSettings(t, false)
		s.StartDate = "bad"
		err := ValidatePersons(s)
		var startErr *InvalidStartDateError
		if !errors.As(err, &startErr) {
			t.Fatalf("expected *InvalidStartDateError, got %T: %v", err, err)
		}
		if startErr.Err == nil {
			t.Error("expected a wrapped parse error")
		}
	})

	t.Run("unparseable birth_month is PersonBirthMonthError with PersonID", func(t *testing.T) {
		s := validSettings(t, false)
		s.Persons[0].ID = "row-7"
		s.Persons[0].BirthMonth = "not-a-date"
		err := ValidatePersons(s)
		var birthErr *PersonBirthMonthError
		if !errors.As(err, &birthErr) {
			t.Fatalf("expected *PersonBirthMonthError, got %T: %v", err, err)
		}
		if birthErr.PersonID != "row-7" {
			t.Errorf("PersonID = %q, want %q", birthErr.PersonID, "row-7")
		}
	})

	t.Run("empty birth_month is PersonBirthMonthError with PersonID", func(t *testing.T) {
		s := validSettings(t, false)
		s.Persons[0].ID = "row-empty"
		s.Persons[0].BirthMonth = ""
		err := ValidatePersons(s)
		var birthErr *PersonBirthMonthError
		if !errors.As(err, &birthErr) {
			t.Fatalf("expected *PersonBirthMonthError, got %T: %v", err, err)
		}
		if birthErr.PersonID != "row-empty" {
			t.Errorf("PersonID = %q, want %q", birthErr.PersonID, "row-empty")
		}
	})

	t.Run("birth_month after start_date is PersonBirthMonthAfterStartError with PersonID and StartDate", func(t *testing.T) {
		s := validSettings(t, false)
		s.StartDate = "2026-04"
		s.Persons[0].ID = "row-9"
		s.Persons[0].BirthMonth = "2026-05"
		err := ValidatePersons(s)
		var afterErr *PersonBirthMonthAfterStartError
		if !errors.As(err, &afterErr) {
			t.Fatalf("expected *PersonBirthMonthAfterStartError, got %T: %v", err, err)
		}
		if afterErr.PersonID != "row-9" {
			t.Errorf("PersonID = %q, want %q", afterErr.PersonID, "row-9")
		}
		if afterErr.StartDate != s.StartDate {
			t.Errorf("StartDate = %q, want %q", afterErr.StartDate, s.StartDate)
		}
	})

	t.Run("a birth-month failure is found by the failing row's OWN ID even when Names collide", func(t *testing.T) {
		// ID uniqueness is checked before birth months (ValidatePersons'
		// own ordering, and parsePersonsForm's UUID assignment upstream),
		// so at the point a birth-month error fires every ID present is
		// unique -- this pins that a row is found by ITS OWN ID, never by
		// Name, even when two rows share the same Name (ruling
		// 2026-09-24h: "Pat Twin" x2 must never name the wrong row).
		s := validSettings(t, false)
		s.Persons[0].ID = "twin-1"
		s.Persons[0].Name = "Pat Twin"
		s.Persons[0].BirthMonth = "1990-01"
		s.Persons = append(s.Persons, models.Person{
			ID: "twin-2", Name: "Pat Twin", BirthMonth: "19711-08", Role: models.PersonRoleOther,
		})
		err := ValidatePersons(s)
		var birthErr *PersonBirthMonthError
		if !errors.As(err, &birthErr) {
			t.Fatalf("expected *PersonBirthMonthError, got %T: %v", err, err)
		}
		if birthErr.PersonID != "twin-2" {
			t.Errorf("PersonID = %q, want %q (the SECOND Pat Twin, whose birth month actually fails)", birthErr.PersonID, "twin-2")
		}
	})
}

// I4: Error() text for every ValidatePersons shape stays byte-identical to
// the historical, untyped text -- settings.go's saveInternal, the load-time
// pass, and prepare.From all surface it to callers (including the MCP
// apply_changes tool) as plain text.
func TestValidatePersons_ErrorTextPinned(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s *models.WhatIfSettings)
		wantErr string
	}{
		{
			name:    "invalid start_date",
			mutate:  func(s *models.WhatIfSettings) { s.StartDate = "bad" },
			wantErr: `start_date: invalid month "bad"`,
		},
		{
			name:    "empty persons",
			mutate:  func(s *models.WhatIfSettings) { s.Persons = nil },
			wantErr: "persons: at least one person is required",
		},
		{
			name:    "empty ID",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].ID = "" },
			wantErr: "persons: id is required",
		},
		{
			name: "duplicate ID",
			mutate: func(s *models.WhatIfSettings) {
				s.Persons = append(s.Persons, models.Person{ID: "p1", Name: "Dup", BirthMonth: "1970-01", Role: models.PersonRoleSpouse})
			},
			wantErr: `persons: duplicate id "p1"`,
		},
		{
			name:    "empty name",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].Name = "" },
			wantErr: "persons: name is required",
		},
		{
			name:    "empty birth_month",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].BirthMonth = "" },
			wantErr: `persons: invalid birth_month for "Alex": month is required`,
		},
		{
			name:    "unparseable birth_month",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].BirthMonth = "not-a-date" },
			wantErr: `persons: invalid birth_month for "Alex": invalid month "not-a-date"`,
		},
		{
			name:    "birth_month after start_date",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].BirthMonth = "2026-05" },
			wantErr: `persons: birth_month "2026-05" is after start_date "2026-04"`,
		},
		{
			name:    "invalid role",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].Role = "bogus" },
			wantErr: `persons: invalid role "bogus"`,
		},
		{
			name:    "missing primary",
			mutate:  func(s *models.WhatIfSettings) { s.Persons[0].Role = models.PersonRoleSpouse },
			wantErr: "persons: expected exactly one primary person, got 0",
		},
		{
			name: "multiple spouses",
			mutate: func(s *models.WhatIfSettings) {
				s.Persons = append(s.Persons,
					models.Person{ID: "s1", Name: "S1", BirthMonth: "1962-01", Role: models.PersonRoleSpouse},
					models.Person{ID: "s2", Name: "S2", BirthMonth: "1963-01", Role: models.PersonRoleSpouse},
				)
			},
			wantErr: "persons: expected at most one spouse person, got 2",
		},
		{
			name: "healthcare link to missing person",
			mutate: func(s *models.WhatIfSettings) {
				s.HealthcarePersons = []models.HealthcarePerson{{ID: "hp1", PersonID: "nonexistent"}}
			},
			wantErr: `healthcare_persons: person_id "nonexistent" not found`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.WhatIfSettings{
				StartDate: "2026-04",
				Persons: []models.Person{
					{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: models.PersonRolePrimary},
				},
			}
			tc.mutate(s)
			err := ValidatePersons(s)
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if err.Error() != tc.wantErr {
				t.Errorf("Error() = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestOneTimeExpense_ValidationRejectsBadEntries(t *testing.T) {
	base := func() *models.WhatIfSettings {
		return &models.WhatIfSettings{ProjectionYears: 10}
	}

	t.Run("negative amount rejected", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 3 * 12, Amount: -1}}
		err := ValidateOneTimeExpenses(s)
		if err == nil {
			t.Fatal("expected error for negative amount")
		}
		if !strings.Contains(err.Error(), "one_time_expenses") {
			t.Errorf("error %q does not name one_time_expenses", err.Error())
		}
	})

	// A negative month is a PAST entry, not a malformed one: the monthly
	// StartDate rollover shifts every offset back as time passes, and the
	// entry is kept (never charged) rather than rejected or deleted.
	t.Run("negative month accepted as a past, dormant entry", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: -1, Amount: 1000}}
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("a past one-time expense must be dormant, not an error: %v", err)
		}
	})

	t.Run("month at projection horizon is dormant, not rejected", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 10 * 12, Amount: 1000}}
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("month == ProjectionYears*12 must be dormant, not an error: %v", err)
		}
	})

	t.Run("month past projection horizon is dormant, not rejected", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 99 * 12, Amount: 1000}}
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("month beyond ProjectionYears*12 must be dormant, not an error: %v", err)
		}
	})

	t.Run("valid entry within horizon passes", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 9 * 12, Amount: 1000}}
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty list passes", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = nil
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("unexpected error for empty list: %v", err)
		}
	})

	t.Run("zero amount and zero month pass", func(t *testing.T) {
		s := base()
		s.OneTimeExpenses = []models.OneTimeExpense{{Description: "free thing", Month: 0, Amount: 0}}
		if err := ValidateOneTimeExpenses(s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestOneTimeExpense_OutOfHorizonWiredThroughFromAsDormant(t *testing.T) {
	s := validSettings(t, false)
	s.ProjectionYears = 5
	s.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "boat", Month: 5 * 12, Amount: 10000}, // == ProjectionYears*12: dormant, not an error
	}
	if _, err := From(s); err != nil {
		t.Fatalf("From must accept a dormant out-of-horizon one-time expense, got: %v", err)
	}
}

func TestOneTimeExpense_MalformedStillRejectedThroughFrom(t *testing.T) {
	s := validSettings(t, false)
	s.ProjectionYears = 5
	s.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "boat", Month: 12, Amount: -10000},
	}
	if _, err := From(s); err == nil {
		t.Fatal("expected From to reject a malformed one-time expense (negative amount)")
	} else if !strings.Contains(err.Error(), "one_time_expenses") {
		t.Errorf("error %q does not name one_time_expenses", err.Error())
	}
}

// A one-time expense whose month has already passed rides through the whole
// prepare pipeline untouched: the rollover leaves it negative rather than
// deleting the user's entry, and From must not treat that as malformed.
func TestOneTimeExpense_PastMonthAcceptedThroughFrom(t *testing.T) {
	s := validSettings(t, false)
	s.ProjectionYears = 5
	s.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "boat", Month: -1, Amount: 10000},
	}
	prepared, err := From(s)
	if err != nil {
		t.Fatalf("From must accept a past one-time expense, got: %v", err)
	}
	if got := prepared.Settings().OneTimeExpenses[0].Month; got != -1 {
		t.Errorf("past one-time expense month = %d, want -1 (retained, not clamped)", got)
	}
}
