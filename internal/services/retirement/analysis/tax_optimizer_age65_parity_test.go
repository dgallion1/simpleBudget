package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// TestBracketFillDeductionMatchesEngine is the second half of audit finding
// F-3. The bracket-fill optimizer sizes conversions against its own standard
// deduction; if that is larger than the deduction the engine then applies, the
// optimizer over-converts and the plan overshoots the bracket ceiling it was
// aiming at. The two must derive the age-65 filer count the same way, in every
// projection year, including the year a filer crosses 65 mid-projection.
func TestBracketFillDeductionMatchesEngine(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		// Primary is already 65; the spouse crosses 65 in projection year 3,
		// so the count must step 1 → 2 partway through.
		{ID: "primary", Name: "You", BirthMonth: models.BirthMonthForAge("2026-01", 65), Role: models.PersonRolePrimary},
		{ID: "spouse", Name: "Spouse", BirthMonth: models.BirthMonthForAge("2026-01", 62), Role: models.PersonRoleSpouse},
	}
	s.PortfolioValue = 1_500_000
	s.MonthlyLivingExpenses = 5_000
	s.ProjectionYears = 6
	s.InflationRate = 0
	s.SocialSecurity = nil

	tc := models.DefaultTaxConfig()
	tc.FilingStatus = models.FilingMarriedJoint
	tc.Age65Count = 0 // as every shipped scenario carries it
	s.TaxConfig = tc

	// Read ages back the way the engine sees them; prepare.From recomputes
	// CurrentAge/SpouseAge from BirthMonth + StartDate.
	in := engineInput(t, s)
	prepared := in.Prepared.Settings()

	sawStep := false
	for year := 0; year < prepared.ProjectionYears; year++ {
		optimizer := bracketFillIncomeForYear(prepared, year, 0).standardDeduction

		want := engine.NewTaxCalculator(prepared.TaxConfig, prepared.InflationRate)
		want.Age65Count = engine.Age65CountForYear(prepared, year)
		engineDeduction := want.GetAdjustedStandardDeduction(engine.YearsFromTaxBase(prepared, year))

		if math.Abs(optimizer-engineDeduction) > 0.01 {
			t.Errorf("year %d: optimizer deduction=%.2f, engine deduction=%.2f (age-65 count %d)",
				year, optimizer, engineDeduction, want.Age65Count)
		}
		if want.Age65Count == 2 {
			sawStep = true
		}
	}
	if !sawStep {
		t.Fatal("no projection year reached an age-65 count of 2; the scenario cannot exercise the step")
	}
}
