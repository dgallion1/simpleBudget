package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Folding a positive Roth-earnings term into bracketFillIncomeForYear must
// shrink the sized conversion so that taxable ordinary income INCLUDING the
// earnings lands on the bracket ceiling. With no Social Security in play, the
// shrink equals the earnings exactly (earnings displace conversion dollar-for-
// dollar in the ordinary bracket).
func TestBracketFillIncomeForYear_FoldsRothEarningsIntoOrdinary(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1] // single filer
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 55)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	// No SS benefit in the window: isolates the earnings→ordinary fold.
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 0, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	ceiling, ok := inflatedBracketTopForYear(ps, 0.22, 0)
	if !ok {
		t.Fatal("no inflated 22% ceiling for year 0")
	}

	const earnings = 20_000.0
	convNoEarn := bracketFillIncomeForYear(ps, 0, 0).bracketFillConversion(ceiling)
	convWithEarn := bracketFillIncomeForYear(ps, 0, earnings).bracketFillConversion(ceiling)

	if !(convWithEarn < convNoEarn) {
		t.Fatalf("earnings should shrink the conversion: noEarn=%.0f withEarn=%.0f", convNoEarn, convWithEarn)
	}
	// Ordinary income (incl. earnings) at the chosen conversion lands on the ceiling.
	got := bracketFillIncomeForYear(ps, 0, earnings).taxableOrdinaryIncome(convWithEarn)
	if math.Abs(got-ceiling) > 1.0 {
		t.Fatalf("ordinary incl earnings should land on ceiling: got=%.0f ceiling=%.0f", got, ceiling)
	}
	// With no SS, the shrink equals the earnings exactly.
	if d := (convNoEarn - convWithEarn) - earnings; math.Abs(d) > 1.0 {
		t.Fatalf("conversion should shrink by exactly the earnings; off by %.2f", d)
	}
}
