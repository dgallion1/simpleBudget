package analysis

import (
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// TestBuildTax_ReportsWhichConstantsTheAnswerRestsOn covers §7's "show the
// user which constants a given answer depends on and when they were last
// verified". A projection that blends published figures with extrapolated
// ones looks equally authoritative in both halves unless it says otherwise.
func TestBuildTax_ReportsWhichConstantsTheAnswerRestsOn(t *testing.T) {
	proj, in := runProj(t, taxableScenario())

	tax := BuildTax(proj, in)
	if tax == nil {
		t.Fatal("BuildTax returned nil")
	}
	basis := tax.ConstantsBasis
	if basis == nil {
		t.Fatal("ConstantsBasis is nil; every answer rests on some set of figures")
	}

	statutoryYear, provenance := engine.LatestStatutoryFederalProvenance()
	if basis.StatutoryYear != statutoryYear {
		t.Errorf("StatutoryYear = %d, want %d", basis.StatutoryYear, statutoryYear)
	}
	if basis.Source != provenance.Source {
		t.Errorf("Source = %q, want %q", basis.Source, provenance.Source)
	}
	if basis.VerifiedOn == "" {
		t.Error("VerifiedOn is empty; a figure nobody has re-checked is worth surfacing")
	}

	// A 25-year projection starting at or after the statutory year must
	// declare that most of it is extrapolated.
	if !basis.HasProjectedYears() {
		t.Fatal("a multi-decade projection cannot be covered by published figures; " +
			"it must declare its projected span")
	}
	if basis.FirstProjectedYear <= statutoryYear {
		t.Errorf("FirstProjectedYear = %d; projection cannot begin at or before the "+
			"last published year %d", basis.FirstProjectedYear, statutoryYear)
	}
	if basis.LastProjectedYear < basis.FirstProjectedYear {
		t.Errorf("projected span is inverted: %d..%d",
			basis.FirstProjectedYear, basis.LastProjectedYear)
	}
	if basis.InflationRate <= 0 {
		t.Errorf("InflationRate = %v; the extrapolation rate is part of the disclosure",
			basis.InflationRate)
	}

	t.Logf("figures: %s (verified %s); years %d-%d projected at %.1f%%/yr",
		basis.Source, basis.VerifiedOn,
		basis.FirstProjectedYear, basis.LastProjectedYear, basis.InflationRate)
}

// TestBuildTax_2026StartPlanRestsOnSeeded2026Statutory pins AC7: a plan
// starting in the seeded 2026 statutory year (IRS Rev. Proc. 2025-32) must
// report that year as statutory, and the first genuinely-extrapolated year
// as the very next one, 2027.
func TestBuildTax_2026StartPlanRestsOnSeeded2026Statutory(t *testing.T) {
	s := taxableScenario()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.ProjectionYears = 3

	proj, in := runProj(t, s)
	tax := BuildTax(proj, in)
	if tax == nil {
		t.Fatal("BuildTax returned nil")
	}
	basis := tax.ConstantsBasis
	if basis == nil {
		t.Fatal("ConstantsBasis is nil")
	}

	if basis.StatutoryYear != 2026 {
		t.Errorf("StatutoryYear = %d, want 2026", basis.StatutoryYear)
	}
	if !strings.Contains(basis.Source, "Rev. Proc. 2025-32") {
		t.Errorf("Source = %q, want it to name Rev. Proc. 2025-32", basis.Source)
	}
	if basis.FirstProjectedYear != 2027 {
		t.Errorf("FirstProjectedYear = %d, want 2027", basis.FirstProjectedYear)
	}
}
