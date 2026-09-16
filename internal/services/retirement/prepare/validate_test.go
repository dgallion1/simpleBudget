package prepare

import (
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
