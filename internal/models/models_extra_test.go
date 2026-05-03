package models

import (
	"math"
	"testing"
	"time"
)

// --- Healthcare tests ---

func TestNewHealthcarePerson(t *testing.T) {
	tests := []struct {
		name     string
		pName    string
		age      int
		coverage CoverageType
		wantCost float64
	}{
		{"medicare", "User", 67, CoverageMedicare, 459},
		{"aca", "Spouse", 55, CoverageACA, 1100},
		{"employer", "Worker", 50, CoverageEmployer, 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hp := NewHealthcarePerson(tt.pName, tt.age, tt.coverage)
			if hp.ID == "" {
				t.Error("expected non-empty ID")
			}
			if hp.Name != tt.pName {
				t.Errorf("Name = %q, want %q", hp.Name, tt.pName)
			}
			if hp.CurrentAge != tt.age {
				t.Errorf("CurrentAge = %d, want %d", hp.CurrentAge, tt.age)
			}
			if hp.CurrentMonthlyCost != tt.wantCost {
				t.Errorf("CurrentMonthlyCost = %f, want %f", hp.CurrentMonthlyCost, tt.wantCost)
			}
			if hp.MedicareEligibleAge != 65 {
				t.Errorf("MedicareEligibleAge = %d, want 65", hp.MedicareEligibleAge)
			}
			if hp.PostMedicareInflation != 4.0 {
				t.Errorf("PostMedicareInflation = %f, want 4.0", hp.PostMedicareInflation)
			}
		})
	}
}

func TestIsOnMedicare(t *testing.T) {
	tests := []struct {
		name string
		hp   HealthcarePerson
		want bool
	}{
		{"coverage medicare", HealthcarePerson{CurrentCoverage: CoverageMedicare, CurrentAge: 60, MedicareEligibleAge: 65}, true},
		{"age >= eligible", HealthcarePerson{CurrentCoverage: CoverageACA, CurrentAge: 66, MedicareEligibleAge: 65}, true},
		{"not on medicare", HealthcarePerson{CurrentCoverage: CoverageACA, CurrentAge: 55, MedicareEligibleAge: 65}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hp.IsOnMedicare(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestYearsUntilMedicare(t *testing.T) {
	tests := []struct {
		name string
		hp   HealthcarePerson
		want int
	}{
		{"already on", HealthcarePerson{CurrentCoverage: CoverageMedicare, CurrentAge: 67, MedicareEligibleAge: 65}, 0},
		{"10 years away", HealthcarePerson{CurrentCoverage: CoverageACA, CurrentAge: 55, MedicareEligibleAge: 65}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hp.YearsUntilMedicare(); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetMonthlyCostWithVariation(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            67,
		CurrentCoverage:       CoverageMedicare,
		CurrentMonthlyCost:    500,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
	}
	base := hp.GetMonthlyCost(0)
	varied := hp.GetMonthlyCostWithVariation(0, 1.1)
	if math.Abs(varied-base*1.1) > 0.01 {
		t.Errorf("got %f, want %f", varied, base*1.1)
	}
}

func TestGetTransitionInfo(t *testing.T) {
	t.Run("already on medicare", func(t *testing.T) {
		hp := HealthcarePerson{CurrentCoverage: CoverageMedicare, CurrentAge: 67, MedicareEligibleAge: 65}
		has, _, _, _ := hp.GetTransitionInfo()
		if has {
			t.Error("should not have transition when already on Medicare")
		}
	})

	t.Run("ACA to medicare", func(t *testing.T) {
		hp := HealthcarePerson{
			CurrentAge:           55,
			CurrentCoverage:      CoverageACA,
			CurrentMonthlyCost:   1000,
			PreMedicareInflation: 7.0,
			MedicareMonthlyCost:  500,
			MedicareEligibleAge:  65,
		}
		has, years, _, medicareCost := hp.GetTransitionInfo()
		if !has {
			t.Error("should have transition")
		}
		if years != 10 {
			t.Errorf("years = %d, want 10", years)
		}
		if medicareCost != 500 {
			t.Errorf("medicareCost = %f, want 500", medicareCost)
		}
	})
}

func TestDefaultHealthcarePersons(t *testing.T) {
	persons := DefaultHealthcarePersons()
	if len(persons) != 2 {
		t.Fatalf("expected 2 persons, got %d", len(persons))
	}
	if persons[0].Name != "User" {
		t.Errorf("first person = %q, want User", persons[0].Name)
	}
	if persons[1].Name != "Spouse" {
		t.Errorf("second person = %q, want Spouse", persons[1].Name)
	}
}

func TestGetMonthlyCostEmployerLimited(t *testing.T) {
	t.Run("during employer coverage", func(t *testing.T) {
		hp := HealthcarePerson{
			CurrentAge:            50,
			CurrentCoverage:       CoverageEmployer,
			CurrentMonthlyCost:    500,
			EmployerCoverageYears: 5,
			ACACostAfterEmployer:  1100,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}
		// Month 24 = 2 years in, still on employer
		got := hp.GetMonthlyCost(24)
		if got != 500 {
			t.Errorf("during employer: got %f, want 500", got)
		}
	})

	t.Run("employer ends then ACA", func(t *testing.T) {
		hp := HealthcarePerson{
			CurrentAge:            50,
			CurrentCoverage:       CoverageEmployer,
			CurrentMonthlyCost:    500,
			EmployerCoverageYears: 5,
			ACACostAfterEmployer:  1100,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}
		// Month 72 = 6 years, employer ended at year 5, now on ACA
		got := hp.GetMonthlyCost(72)
		monthsAfterEmployer := 72 - 60 // 12 months on ACA
		want := 1100.0 * math.Pow(1.07, float64(monthsAfterEmployer)/12.0)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("ACA after employer: got %f, want %f", got, want)
		}
	})

	t.Run("employer ends already medicare eligible", func(t *testing.T) {
		hp := HealthcarePerson{
			CurrentAge:            63,
			CurrentCoverage:       CoverageEmployer,
			CurrentMonthlyCost:    500,
			EmployerCoverageYears: 5,
			ACACostAfterEmployer:  1100,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}
		// Month 72 = 6 years, employer ended at year 5 (age 68, past Medicare)
		got := hp.GetMonthlyCost(72)
		monthsAfterEmployer := 72 - 60
		want := 600.0 * math.Pow(1.04, float64(monthsAfterEmployer)/12.0)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("medicare after employer: got %f, want %f", got, want)
		}
	})

	t.Run("employer then ACA then medicare", func(t *testing.T) {
		hp := HealthcarePerson{
			CurrentAge:            50,
			CurrentCoverage:       CoverageEmployer,
			CurrentMonthlyCost:    500,
			EmployerCoverageYears: 5,
			ACACostAfterEmployer:  1100,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		}
		// Age at employer end = 55, Medicare at 65, so 10 years ACA
		// Month 180 = 15 years in. Employer ends at month 60.
		// ACA from month 60 to month 180 (10 years of ACA = 120 months)
		// Medicare starts at monthsAfterEmployer >= 120
		// Month 192 = 16 years, monthsAfterEmployer = 132, monthsOnACA = 120
		// monthsOnMedicare = 132 - 120 = 12
		got := hp.GetMonthlyCost(192)
		monthsOnMedicare := 192 - 60 - 120 // 12
		want := 600.0 * math.Pow(1.04, float64(monthsOnMedicare)/12.0)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("medicare after ACA after employer: got %f, want %f", got, want)
		}
	})
}

func TestGetMonthlyCostACAToMedicare(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            60,
		CurrentCoverage:       CoverageACA,
		CurrentMonthlyCost:    1000,
		PreMedicareInflation:  7.0,
		MedicareMonthlyCost:   500,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
	}

	// Pre-Medicare (month 48 = 4 years, still age 64)
	got := hp.GetMonthlyCost(48)
	want := 1000.0 * math.Pow(1.07, 4.0)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("pre-medicare: got %f, want %f", got, want)
	}

	// Post-Medicare (month 72 = 6 years, age 66)
	got = hp.GetMonthlyCost(72)
	monthsOnMedicare := 72 - (5 * 12) // 12
	want = 500.0 * math.Pow(1.04, float64(monthsOnMedicare)/12.0)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("post-medicare: got %f, want %f", got, want)
	}
}

func TestGetMonthlyCostUnlimitedEmployer(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            60,
		CurrentCoverage:       CoverageEmployer,
		CurrentMonthlyCost:    500,
		EmployerCoverageYears: 0, // unlimited until Medicare
		PreMedicareInflation:  7.0,
		MedicareMonthlyCost:   600,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
	}

	// Pre-Medicare with unlimited employer (treated like ACA/pre-medicare path)
	got := hp.GetMonthlyCost(24)
	want := 500.0 * math.Pow(1.07, 2.0)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("unlimited employer pre-medicare: got %f, want %f", got, want)
	}

	// Post-Medicare
	got = hp.GetMonthlyCost(72)
	monthsOnMedicare := 72 - (5 * 12)
	want = 600.0 * math.Pow(1.04, float64(monthsOnMedicare)/12.0)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("unlimited employer post-medicare: got %f, want %f", got, want)
	}
}

// --- WhatIf tests ---

func TestNormalizeProjectionTiming(t *testing.T) {
	tests := []struct {
		input ProjectionTiming
		want  ProjectionTiming
	}{
		{ProjectionTimingStartOfMonth, ProjectionTimingStartOfMonth},
		{ProjectionTimingMidMonth, ProjectionTimingMidMonth},
		{ProjectionTimingEndOfMonth, ProjectionTimingEndOfMonth},
		{"invalid", ProjectionTimingEndOfMonth},
		{"", ProjectionTimingEndOfMonth},
	}
	for _, tt := range tests {
		if got := NormalizeProjectionTiming(tt.input); got != tt.want {
			t.Errorf("NormalizeProjectionTiming(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDefaultSpendingPhases(t *testing.T) {
	phases := DefaultSpendingPhases()
	if len(phases) != 5 {
		t.Fatalf("expected 5 phases, got %d", len(phases))
	}
	if phases[0].Multiplier != 1.0 {
		t.Errorf("first phase multiplier = %f, want 1.0", phases[0].Multiplier)
	}
	if phases[4].Name != "No-Go" {
		t.Errorf("last phase name = %q, want No-Go", phases[4].Name)
	}
}

func TestHasSpouse(t *testing.T) {
	s := &WhatIfSettings{SpouseAge: 0}
	if s.HasSpouse() {
		t.Error("SpouseAge=0 should not have spouse")
	}
	s.SpouseAge = 60
	if !s.HasSpouse() {
		t.Error("SpouseAge=60 should have spouse")
	}
}

func TestGetYoungerAge(t *testing.T) {
	tests := []struct {
		name       string
		currentAge int
		spouseAge  int
		want       int
	}{
		{"no spouse", 65, 0, 65},
		{"spouse younger", 65, 60, 60},
		{"spouse older", 60, 65, 60},
		{"same age", 65, 65, 65},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &WhatIfSettings{CurrentAge: tt.currentAge, SpouseAge: tt.spouseAge}
			if got := s.GetYoungerAge(); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetOlderAge(t *testing.T) {
	tests := []struct {
		name       string
		currentAge int
		spouseAge  int
		want       int
	}{
		{"no spouse", 65, 0, 65},
		{"spouse older", 60, 65, 65},
		{"spouse younger", 65, 60, 65},
		{"same age", 65, 65, 65},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &WhatIfSettings{CurrentAge: tt.currentAge, SpouseAge: tt.spouseAge}
			if got := s.GetOlderAge(); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPhaseReferenceAge(t *testing.T) {
	s := &WhatIfSettings{CurrentAge: 65, SpouseAge: 60}

	tests := []struct {
		ref          string
		yearsElapsed int
		want         int
	}{
		{"primary", 5, 70},
		{"", 5, 70}, // default = primary
		{"spouse", 5, 65},
		{"younger", 5, 65},
		{"older", 5, 70},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			s.PhaseAgeReference = tt.ref
			if got := s.GetPhaseReferenceAge(tt.yearsElapsed); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}

	// No spouse, "spouse" reference falls back to primary
	s2 := &WhatIfSettings{CurrentAge: 65, SpouseAge: 0, PhaseAgeReference: "spouse"}
	if got := s2.GetPhaseReferenceAge(5); got != 70 {
		t.Errorf("spouse ref no spouse: got %d, want 70", got)
	}
}

func TestSpendingMultiplierAt(t *testing.T) {
	// Primary turns 65 in Jan 2025; default phases: Go-Go (0+, 1.0),
	// Active (65+, 0.95), Slow-Go (75+, 0.85), Late Slow-Go (80+, 0.75),
	// No-Go (85+, 0.65).
	s := &WhatIfSettings{
		StartDate: "2025-01",
		Persons: []Person{
			{ID: "p1", Name: "P", Role: PersonRolePrimary, BirthMonth: BirthMonthForAge("2025-01", 65)},
		},
		PhaseAgeReference: "primary",
		SpendingPhaseConfig: &SpendingPhaseConfig{
			Enabled: true,
			Phases:  DefaultSpendingPhases(),
		},
	}
	s.ComputeAges()

	tests := []struct {
		name string
		when time.Time
		want float64
	}{
		{"start of projection age 65", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), 0.95},
		{"five years later (still 65+)", time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC), 0.95},
		{"age 75 → Slow-Go", time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC), 0.85},
		{"age 85 → No-Go", time.Date(2045, 1, 1, 0, 0, 0, 0, time.UTC), 0.65},
		{"3 years before start (age 62)", time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.SpendingMultiplierAt(tt.when)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	// Phases disabled → always 1.0
	s.SpendingPhaseConfig.Enabled = false
	if got := s.SpendingMultiplierAt(time.Date(2045, 1, 1, 0, 0, 0, 0, time.UTC)); got != 1.0 {
		t.Errorf("phases disabled: got %v, want 1.0", got)
	}

	// Bad StartDate → 1.0 (defensive fallback)
	s2 := &WhatIfSettings{
		StartDate: "not-a-date",
		SpendingPhaseConfig: &SpendingPhaseConfig{
			Enabled: true,
			Phases:  DefaultSpendingPhases(),
		},
	}
	if got := s2.SpendingMultiplierAt(time.Now()); got != 1.0 {
		t.Errorf("invalid StartDate: got %v, want 1.0", got)
	}
}

func TestComputeAgesUsesStartDateAndLinkedHealthcare(t *testing.T) {
	s := &WhatIfSettings{
		StartDate: "2026-04",
		Persons: []Person{
			{ID: "primary", Name: "Alex", BirthMonth: "1960-05", Role: PersonRolePrimary},
			{ID: "spouse", Name: "Casey", BirthMonth: "1962-04", Role: PersonRoleSpouse},
		},
		HealthcarePersons: []HealthcarePerson{
			{ID: "hp1", PersonID: "primary"},
		},
	}

	s.ComputeAges()

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

func TestValidatePersons(t *testing.T) {
	validBase := func() *WhatIfSettings {
		return &WhatIfSettings{
			StartDate: "2026-04",
			Persons: []Person{
				{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: PersonRolePrimary},
			},
		}
	}

	t.Run("valid single primary", func(t *testing.T) {
		if err := validBase().ValidatePersons(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid primary and spouse", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, Person{ID: "s1", Name: "Casey", BirthMonth: "1962-04", Role: PersonRoleSpouse})
		if err := s.ValidatePersons(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid start_date", func(t *testing.T) {
		s := validBase()
		s.StartDate = "bad"
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for invalid start_date")
		}
	})

	t.Run("empty persons", func(t *testing.T) {
		s := validBase()
		s.Persons = nil
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for empty persons")
		}
	})

	t.Run("missing primary", func(t *testing.T) {
		s := validBase()
		s.Persons[0].Role = PersonRoleSpouse
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for missing primary")
		}
	})

	t.Run("duplicate primary", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, Person{ID: "p2", Name: "Other", BirthMonth: "1965-01", Role: PersonRolePrimary})
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for duplicate primary")
		}
	})

	t.Run("multiple spouses", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons,
			Person{ID: "s1", Name: "Spouse1", BirthMonth: "1962-01", Role: PersonRoleSpouse},
			Person{ID: "s2", Name: "Spouse2", BirthMonth: "1963-01", Role: PersonRoleSpouse},
		)
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for multiple spouses")
		}
	})

	t.Run("duplicate IDs", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, Person{ID: "p1", Name: "Dup", BirthMonth: "1970-01", Role: PersonRoleSpouse})
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for duplicate IDs")
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		s := validBase()
		s.Persons[0].ID = ""
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for empty ID")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		s := validBase()
		s.Persons[0].Name = ""
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for empty name")
		}
	})

	t.Run("invalid birth_month", func(t *testing.T) {
		s := validBase()
		s.Persons[0].BirthMonth = "not-a-date"
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for invalid birth_month")
		}
	})

	t.Run("birth_month after start_date", func(t *testing.T) {
		s := validBase()
		s.Persons[0].BirthMonth = "2026-05"
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for future birth_month")
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		s := validBase()
		s.Persons = append(s.Persons, Person{ID: "x1", Name: "X", BirthMonth: "1970-01", Role: "invalid"})
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for invalid role")
		}
	})

	t.Run("healthcare link to missing person", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []HealthcarePerson{{ID: "hp1", PersonID: "nonexistent"}}
		if err := s.ValidatePersons(); err == nil {
			t.Fatal("expected error for healthcare link to missing person")
		}
	})

	t.Run("healthcare link to valid person", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []HealthcarePerson{{ID: "hp1", PersonID: "p1"}}
		if err := s.ValidatePersons(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unlinked healthcare passes", func(t *testing.T) {
		s := validBase()
		s.HealthcarePersons = []HealthcarePerson{{ID: "hp1", PersonID: "", Name: "Manual", CurrentAge: 60}}
		if err := s.ValidatePersons(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPersonHelpers(t *testing.T) {
	s := &WhatIfSettings{
		StartDate: "2026-04",
		Persons: []Person{
			{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: PersonRolePrimary},
			{ID: "s1", Name: "Casey", BirthMonth: "1962-06", Role: PersonRoleSpouse},
			{ID: "o1", Name: "Other", BirthMonth: "1990-01", Role: PersonRoleOther},
		},
	}

	t.Run("GetPrimaryPerson", func(t *testing.T) {
		p := s.GetPrimaryPerson()
		if p == nil || p.ID != "p1" {
			t.Fatalf("expected primary p1, got %v", p)
		}
	})

	t.Run("GetSpousePerson", func(t *testing.T) {
		p := s.GetSpousePerson()
		if p == nil || p.ID != "s1" {
			t.Fatalf("expected spouse s1, got %v", p)
		}
	})

	t.Run("GetSpousePerson nil when no spouse", func(t *testing.T) {
		s2 := &WhatIfSettings{Persons: []Person{{ID: "p1", Role: PersonRolePrimary}}}
		if s2.GetSpousePerson() != nil {
			t.Fatal("expected nil for no spouse")
		}
	})

	t.Run("FindPerson", func(t *testing.T) {
		if s.FindPerson("o1") == nil {
			t.Fatal("expected to find o1")
		}
		if s.FindPerson("missing") != nil {
			t.Fatal("expected nil for missing ID")
		}
	})

	t.Run("PersonAge", func(t *testing.T) {
		if age := s.PersonAge("p1"); age != 66 {
			t.Fatalf("primary age = %d, want 66", age)
		}
		if age := s.PersonAge("s1"); age != 63 {
			t.Fatalf("spouse age = %d, want 63", age)
		}
		if age := s.PersonAge("missing"); age != 0 {
			t.Fatalf("missing person age = %d, want 0", age)
		}
	})
}

func TestDeriveAgeAtStartDate(t *testing.T) {
	tests := []struct {
		name       string
		startDate  string
		birthMonth string
		wantAge    int
		wantErr    bool
	}{
		{"exact years", "2026-04", "1960-04", 66, false},
		{"birth month equals start month", "2026-06", "1960-06", 66, false},
		{"born later in year, not aged up", "2026-04", "1960-07", 65, false},
		{"born earlier in year, aged up", "2026-07", "1960-04", 66, false},
		{"born one month before start", "2026-02", "1960-01", 66, false},
		{"born same month as start", "2026-01", "2026-01", 0, false},
		{"born one month after start", "2026-04", "2026-05", 0, true},
		{"age zero boundary", "2026-04", "2026-04", 0, false},
		{"11 months not a full year", "2026-03", "1960-04", 65, false},
		{"12 months is exactly one year", "1961-04", "1960-04", 1, false},
		{"invalid start date", "bad", "1960-04", 0, true},
		{"invalid birth month", "2026-04", "bad", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			age, err := deriveAgeAtStartDate(tt.startDate, tt.birthMonth)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && age != tt.wantAge {
				t.Fatalf("age = %d, want %d", age, tt.wantAge)
			}
		})
	}
}

func TestBirthMonthForAge(t *testing.T) {
	tests := []struct {
		name      string
		startDate string
		age       int
		want      string
	}{
		{"age 65 from 2026-04", "2026-04", 65, "1961-04"},
		{"age 0", "2026-04", 0, "2026-04"},
		{"negative age", "2026-04", -1, ""},
		{"invalid start date", "bad", 65, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BirthMonthForAge(tt.startDate, tt.age)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
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
			s := &WhatIfSettings{
				PhaseAgeReference: tt.ref,
				StartDate:         "2026-04",
				Persons: []Person{
					{ID: "p1", Name: "Alex", BirthMonth: "1960-04", Role: PersonRolePrimary},
				},
			}
			if tt.hasSpouse {
				s.Persons = append(s.Persons, Person{ID: "s1", Name: "Casey", BirthMonth: "1962-04", Role: PersonRoleSpouse})
				s.ComputeAges()
			}
			s.NormalizePhaseAgeReference()
			if s.PhaseAgeReference != tt.want {
				t.Fatalf("got %q, want %q", s.PhaseAgeReference, tt.want)
			}
		})
	}
}

func TestPrimaryAgeAt(t *testing.T) {
	s := &WhatIfSettings{CurrentAge: 65}
	if got := s.PrimaryAgeAt(5); got != 70 {
		t.Errorf("got %d, want 70", got)
	}
}

func TestSpouseAgeAt(t *testing.T) {
	s := &WhatIfSettings{CurrentAge: 65, SpouseAge: 60}
	if got := s.SpouseAgeAt(5); got != 65 {
		t.Errorf("got %d, want 65", got)
	}

	s2 := &WhatIfSettings{CurrentAge: 65, SpouseAge: 0}
	if got := s2.SpouseAgeAt(5); got != 0 {
		t.Errorf("no spouse: got %d, want 0", got)
	}
}

func TestGetSpendingMultiplier(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		s := &WhatIfSettings{}
		if got := s.GetSpendingMultiplier(80); got != 1.0 {
			t.Errorf("got %f, want 1.0", got)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		s := &WhatIfSettings{SpendingPhaseConfig: &SpendingPhaseConfig{Enabled: false}}
		if got := s.GetSpendingMultiplier(80); got != 1.0 {
			t.Errorf("got %f, want 1.0", got)
		}
	})

	t.Run("empty phases", func(t *testing.T) {
		s := &WhatIfSettings{SpendingPhaseConfig: &SpendingPhaseConfig{Enabled: true, Phases: nil}}
		if got := s.GetSpendingMultiplier(80); got != 1.0 {
			t.Errorf("got %f, want 1.0", got)
		}
	})

	t.Run("enabled with phases", func(t *testing.T) {
		s := &WhatIfSettings{
			SpendingPhaseConfig: &SpendingPhaseConfig{
				Enabled: true,
				Phases: []SpendingPhase{
					{StartAge: 0, Multiplier: 1.0},
					{StartAge: 75, Multiplier: 0.85},
					{StartAge: 85, Multiplier: 0.65},
				},
			},
		}
		if got := s.GetSpendingMultiplier(70); got != 1.0 {
			t.Errorf("age 70: got %f, want 1.0", got)
		}
		if got := s.GetSpendingMultiplier(80); got != 0.85 {
			t.Errorf("age 80: got %f, want 0.85", got)
		}
		if got := s.GetSpendingMultiplier(90); got != 0.65 {
			t.Errorf("age 90: got %f, want 0.65", got)
		}
	})
}

func TestGetTotalHealthcareCost(t *testing.T) {
	t.Run("multi-person", func(t *testing.T) {
		s := &WhatIfSettings{
			HealthcarePersons: []HealthcarePerson{
				{CurrentAge: 67, CurrentCoverage: CoverageMedicare, CurrentMonthlyCost: 459, PostMedicareInflation: 4.0, MedicareEligibleAge: 65},
				{CurrentAge: 67, CurrentCoverage: CoverageMedicare, CurrentMonthlyCost: 459, PostMedicareInflation: 4.0, MedicareEligibleAge: 65},
			},
		}
		got := s.GetTotalHealthcareCost(0)
		if got != 918 {
			t.Errorf("got %f, want 918", got)
		}
	})

	t.Run("legacy before start", func(t *testing.T) {
		s := &WhatIfSettings{
			MonthlyHealthcare:    500,
			HealthcareStartYears: 2,
			HealthcareInflation:  6.0,
		}
		got := s.GetTotalHealthcareCost(12) // 1 year, before 2-year start
		if got != 0 {
			t.Errorf("before start: got %f, want 0", got)
		}
	})

	t.Run("legacy after start", func(t *testing.T) {
		s := &WhatIfSettings{
			MonthlyHealthcare:    500,
			HealthcareStartYears: 0,
			HealthcareInflation:  6.0,
		}
		got := s.GetTotalHealthcareCost(12)
		want := 500.0 * math.Pow(1.06, 1.0)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("after start: got %f, want %f", got, want)
		}
	})
}

func TestHasMultiPersonHealthcare(t *testing.T) {
	s := &WhatIfSettings{}
	if s.HasMultiPersonHealthcare() {
		t.Error("empty should be false")
	}
	s.HealthcarePersons = []HealthcarePerson{{}}
	if !s.HasMultiPersonHealthcare() {
		t.Error("with persons should be true")
	}
}

func TestAssetAllocationIsSet(t *testing.T) {
	s := &WhatIfSettings{}
	if s.AssetAllocationIsSet() {
		t.Error("all zeros should be false")
	}

	s.StockPercent = 70
	if !s.AssetAllocationIsSet() {
		t.Error("StockPercent=70 should be true")
	}

	s2 := &WhatIfSettings{CashPercent: 5}
	if !s2.AssetAllocationIsSet() {
		t.Error("CashPercent=5 should be true")
	}

	s3 := &WhatIfSettings{TaxDeferredStockPercent: 80}
	if !s3.AssetAllocationIsSet() {
		t.Error("per-account set should be true")
	}
}

func TestGetEffectiveAssetAllocation(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		s := &WhatIfSettings{}
		stock, bond, cash := s.GetEffectiveAssetAllocation()
		if stock != 60 || bond != 40 || cash != 0 {
			t.Errorf("got (%f,%f,%f), want (60,40,0)", stock, bond, cash)
		}
	})

	t.Run("custom", func(t *testing.T) {
		s := &WhatIfSettings{StockPercent: 70, CashPercent: 10}
		stock, bond, cash := s.GetEffectiveAssetAllocation()
		if stock != 70 || bond != 20 || cash != 10 {
			t.Errorf("got (%f,%f,%f), want (70,20,10)", stock, bond, cash)
		}
	})
}

func TestEffectiveStockPercent(t *testing.T) {
	s := &WhatIfSettings{}
	if got := s.EffectiveStockPercent(); got != 60 {
		t.Errorf("got %f, want 60", got)
	}
}

func TestEffectiveBondPercent(t *testing.T) {
	s := &WhatIfSettings{}
	if got := s.EffectiveBondPercent(); got != 40 {
		t.Errorf("got %f, want 40", got)
	}
}

func TestAllocationFallbackToGlobal(t *testing.T) {
	s := &WhatIfSettings{StockPercent: 70, CashPercent: 10}

	stock, bond, cash := s.GetTaxDeferredAllocation()
	if stock != 70 || bond != 20 || cash != 10 {
		t.Errorf("TaxDeferred fallback: got (%f,%f,%f), want (70,20,10)", stock, bond, cash)
	}

	stock, bond, cash = s.GetRothAllocation()
	if stock != 70 || bond != 20 || cash != 10 {
		t.Errorf("Roth fallback: got (%f,%f,%f), want (70,20,10)", stock, bond, cash)
	}

	stock, bond, cash = s.GetTaxableAllocation()
	if stock != 70 || bond != 20 || cash != 10 {
		t.Errorf("Taxable fallback: got (%f,%f,%f), want (70,20,10)", stock, bond, cash)
	}
}

func TestTemplatePctHelpers(t *testing.T) {
	s := &WhatIfSettings{
		TaxDeferredStockPercent: 80,
		TaxDeferredCashPercent:  5,
		RothStockPercent:        60,
		RothCashPercent:         10,
		TaxableStockPercent:     50,
		TaxableCashPercent:      20,
	}

	if got := s.TaxDeferredStockPct(); got != 80 {
		t.Errorf("TaxDeferredStockPct = %f, want 80", got)
	}
	if got := s.TaxDeferredBondPct(); got != 15 {
		t.Errorf("TaxDeferredBondPct = %f, want 15", got)
	}
	if got := s.TaxDeferredCashPct(); got != 5 {
		t.Errorf("TaxDeferredCashPct = %f, want 5", got)
	}
	if got := s.RothStockPct(); got != 60 {
		t.Errorf("RothStockPct = %f, want 60", got)
	}
	if got := s.RothBondPct(); got != 30 {
		t.Errorf("RothBondPct = %f, want 30", got)
	}
	if got := s.RothCashPct(); got != 10 {
		t.Errorf("RothCashPct = %f, want 10", got)
	}
	if got := s.TaxableStockPct(); got != 50 {
		t.Errorf("TaxableStockPct = %f, want 50", got)
	}
	if got := s.TaxableBondPct(); got != 30 {
		t.Errorf("TaxableBondPct = %f, want 30", got)
	}
	if got := s.TaxableCashPct(); got != 20 {
		t.Errorf("TaxableCashPct = %f, want 20", got)
	}
}

func TestDefaultWhatIfSettings(t *testing.T) {
	s := DefaultWhatIfSettings()
	if s == nil {
		t.Fatal("nil")
	}
	if s.CurrentAge != 65 {
		t.Errorf("CurrentAge = %d, want 65", s.CurrentAge)
	}
	if s.StartDate == "" {
		t.Error("StartDate should not be empty")
	}
	if len(s.Persons) != 1 || s.Persons[0].Role != PersonRolePrimary {
		t.Fatalf("expected default primary person, got %+v", s.Persons)
	}
	if s.ProjectionYears != 30 {
		t.Errorf("ProjectionYears = %d, want 30", s.ProjectionYears)
	}
	if s.TaxConfig == nil {
		t.Error("TaxConfig should not be nil")
	}
	if s.SpendingPhaseConfig == nil {
		t.Error("SpendingPhaseConfig should not be nil")
	}
	if s.RothConversion == nil {
		t.Error("RothConversion should not be nil")
	}
	if s.IncomeSources == nil {
		t.Error("IncomeSources should not be nil")
	}
	if s.ExpenseSources == nil {
		t.Error("ExpenseSources should not be nil")
	}
	if s.BigTicketItems == nil {
		t.Error("BigTicketItems should not be nil")
	}
}

func TestGetProjectionTiming(t *testing.T) {
	s := &WhatIfSettings{ProjectionTiming: ProjectionTimingMidMonth}
	if got := s.GetProjectionTiming(); got != ProjectionTimingMidMonth {
		t.Errorf("got %q, want mid_month", got)
	}

	s2 := &WhatIfSettings{ProjectionTiming: "bad"}
	if got := s2.GetProjectionTiming(); got != ProjectionTimingEndOfMonth {
		t.Errorf("got %q, want end_of_month", got)
	}
}

func TestGetTaxableQualifiedDividendPercent(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-5, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{150, 100},
	}
	for _, tt := range tests {
		s := &WhatIfSettings{TaxableQualifiedDividendPercent: tt.input}
		if got := s.GetTaxableQualifiedDividendPercent(); got != tt.want {
			t.Errorf("input %f: got %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestCalculateSustainabilityScore(t *testing.T) {
	tests := []struct {
		rate     float64
		survives bool
		label    string
		score    int
	}{
		{0, false, "Critical", 0},
		{2.5, true, "Excellent", 100},
		{3.5, true, "Good", 90},
		{4.5, true, "Fair", 75},
		{5.5, true, "Caution", 60},
		{7.0, true, "Poor", 40},
		{10.0, true, "Critical", 0}, // (100 - (10-3)*15) = -5, clamped to 0
	}
	for _, tt := range tests {
		result := CalculateSustainabilityScore(tt.rate, tt.survives)
		if result.Label != tt.label {
			t.Errorf("rate=%f survives=%v: label = %q, want %q", tt.rate, tt.survives, result.Label, tt.label)
		}
		if result.Score != tt.score {
			t.Errorf("rate=%f survives=%v: score = %d, want %d", tt.rate, tt.survives, result.Score, tt.score)
		}
		if result.Color == "" {
			t.Errorf("rate=%f: color should not be empty", tt.rate)
		}
		if result.Description == "" {
			t.Errorf("rate=%f: description should not be empty", tt.rate)
		}
	}
}

func TestDefaultTaxConfig(t *testing.T) {
	tc := DefaultTaxConfig()
	if tc.FilingStatus != FilingMarriedJoint {
		t.Errorf("FilingStatus = %q, want married_joint", tc.FilingStatus)
	}
	if tc.StateIncomeTaxRate != 0 {
		t.Errorf("StateIncomeTaxRate = %f, want 0", tc.StateIncomeTaxRate)
	}
}

func TestBigTicketItemGetNetAmount(t *testing.T) {
	income := BigTicketItem{Amount: 50000, Type: BigTicketIncome}
	if got := income.GetNetAmount(); got != 50000 {
		t.Errorf("income: got %f, want 50000", got)
	}

	expense := BigTicketItem{Amount: 30000, Type: BigTicketExpense}
	if got := expense.GetNetAmount(); got != -30000 {
		t.Errorf("expense: got %f, want -30000", got)
	}
}

func TestGetExpectedReturnNegativeTaxWeight(t *testing.T) {
	// TaxDeferred + Roth > 100%, taxWeight should be clamped to 0
	s := &WhatIfSettings{
		TaxDeferredPercent:      80,
		RothPercent:             30,
		TaxDeferredStockPercent: 60,
		RothStockPercent:        60,
		TaxableStockPercent:     60,
	}
	ret := s.GetExpectedReturnFromAllocation()
	if ret <= 0 {
		t.Errorf("return should be positive, got %f", ret)
	}
}

func TestGetMonthlyCostAlreadyPastMedicare(t *testing.T) {
	// Person past Medicare age but coverage is ACA (edge case)
	hp := HealthcarePerson{
		CurrentAge:            70,
		CurrentCoverage:       CoverageACA,
		CurrentMonthlyCost:    1000,
		PreMedicareInflation:  7.0,
		MedicareMonthlyCost:   500,
		PostMedicareInflation: 4.0,
		MedicareEligibleAge:   65,
	}
	// yearsUntilMedicare will be 0 (since 65-70 = -5, clamped)
	// At month 0, ageAtMonth = 70 >= 65, so should use Medicare cost
	got := hp.GetMonthlyCost(0)
	// monthsOnMedicare = 0 - (0 * 12) = 0
	want := 500.0
	if math.Abs(got-want) > 0.01 {
		t.Errorf("already past medicare: got %f, want %f", got, want)
	}
}
