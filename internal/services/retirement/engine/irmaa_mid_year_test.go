package engine

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// midYearMedicareScenario is employerCoverageScenario's single filer with a
// birth month that puts the Medicare transition six months into projection
// year 0 instead of on its first day. Pension income stays well above the
// IRMAA threshold throughout, so any month the household is enrolled is a
// month the surcharge should be charged.
func midYearMedicareScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()

	// 0 employer-coverage years, so the age-based transition is what decides
	// the start month rather than a coverage lapse.
	s := employerCoverageScenario(0)
	s.CurrentAge = 64
	s.Persons[0].BirthMonth = "1961-07"
	s.HealthcarePersons[0].BirthMonth = "1961-07"
	s.HealthcarePersons[0].CurrentAge = 64

	// The whole point of the scenario. Asserted rather than assumed so a
	// change to the birth-month arithmetic fails here, naming the cause,
	// instead of quietly turning the tests below into no-ops.
	if got := s.HealthcarePersons[0].MedicareStartMonth(s.StartDate); got != 6 {
		t.Fatalf("scenario setup: MedicareStartMonth=%d, want 6", got)
	}
	return s
}

// TestMedicareEligibleAdultCountAtMonthCountsFromTheTransitionMonth pins the
// resolution of the eligibility test. The count used to be evaluated on the
// enclosing projection year's first month, so a start in month 6 was not
// counted until month 12.
func TestMedicareEligibleAdultCountAtMonthCountsFromTheTransitionMonth(t *testing.T) {
	s := midYearMedicareScenario(t)

	for m := 0; m < 6; m++ {
		if got := MedicareEligibleAdultCountAtMonth(s, m); got != 0 {
			t.Errorf("month %d: count=%d, want 0 — Medicare has not started yet", m, got)
		}
	}
	for m := 6; m < 24; m++ {
		if got := MedicareEligibleAdultCountAtMonth(s, m); got != 1 {
			t.Errorf("month %d: count=%d, want 1 — Medicare started in month 6", m, got)
		}
	}
}

// TestIRMAAChargedForTheMedicareTransitionYear is the projection-level
// regression. Enrolling in month 6 means six months of Part B and Part D
// premiums in projection year 0, and the surcharge on them is part of what
// that year costs. Evaluating eligibility once per year on the year's first
// month dropped all six, so the transition year was billed a Medicare premium
// with no IRMAA on top of it.
func TestIRMAAChargedForTheMedicareTransitionYear(t *testing.T) {
	s := midYearMedicareScenario(t)

	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	if len(proj.YearlySummaries) < 2 {
		t.Fatalf("yearly summaries=%d, want at least 2", len(proj.YearlySummaries))
	}
	for y, ys := range proj.YearlySummaries {
		t.Logf("year %d: IRMAA=%.2f MAGI=%.2f", y, ys.IRMAA, ys.MAGI)
	}

	if proj.YearlySummaries[0].IRMAA <= 0 {
		t.Errorf("year 0: IRMAA=%.2f, want > 0 — the household is enrolled and paying "+
			"Medicare premiums from month 6, and MAGI is above the threshold",
			proj.YearlySummaries[0].IRMAA)
	}

	// A partial year, not a full one: the surcharge starts in month 6, so
	// year 0 must come in under the first full year of enrolment. Without
	// this, charging twelve months in year 0 would also pass the check above.
	if proj.YearlySummaries[0].IRMAA >= proj.YearlySummaries[1].IRMAA {
		t.Errorf("year 0 IRMAA=%.2f, year 1 IRMAA=%.2f: year 0 covers six enrolled "+
			"months and year 1 covers twelve, so year 0 must be the smaller",
			proj.YearlySummaries[0].IRMAA, proj.YearlySummaries[1].IRMAA)
	}
}

// TestMedicareEligibleAdultCountAtYearKeepsFirstMonthSemantics fixes the
// wrapper's contract. The analysis layer's point-in-time snapshots price one
// representative month at the start of a year and have no month to pass, so
// AtYear must keep answering for that month — unchanged by the stepper moving
// to month resolution.
func TestMedicareEligibleAdultCountAtYearKeepsFirstMonthSemantics(t *testing.T) {
	s := midYearMedicareScenario(t)

	if got := MedicareEligibleAdultCountAtYear(s, 0); got != 0 {
		t.Errorf("year 0: count=%d, want 0 — the year begins before the transition month", got)
	}
	if got := MedicareEligibleAdultCountAtYear(s, 1); got != 1 {
		t.Errorf("year 1: count=%d, want 1 — the year begins after the transition month", got)
	}
}
