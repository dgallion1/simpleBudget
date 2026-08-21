package analysis

import (
	"testing"

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
