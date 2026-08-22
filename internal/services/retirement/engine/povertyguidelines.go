package engine

import (
	"fmt"

	"budget2/internal/models"
)

// Federal poverty guidelines, versioned the same way the tax tables are.
//
// These place the ACA premium tax credit's 400% cliff, so an out-of-date or
// silently extrapolated figure moves the single most expensive discontinuity
// in a pre-Medicare household's plan.
//
// Two caveats that are modelled as stated rather than silently ignored:
//
//   - Alaska and Hawaii have their own, higher guidelines. Only the figures
//     for the 48 contiguous states and DC are carried here, so a household in
//     Alaska or Hawaii measures against a cliff that is too low.
//   - Marketplace eligibility for a coverage year uses the guidelines
//     published BEFORE that year's open enrolment — in practice the prior
//     year's table. PovertyGuidelineFor takes the coverage year and applies
//     that lookback, rather than reading the same year's table.

// PovertyGuidelineRecord is one year's published guidelines for the 48
// contiguous states and DC.
type PovertyGuidelineRecord struct {
	// Year is the year the guidelines were published FOR, not the coverage
	// year they end up governing.
	Year                int
	OnePerson           float64
	PerAdditionalPerson float64
	Provenance          ConstantProvenance
}

// povertyGuidelines holds every record on file, ascending by year.
//
// Only 2024 is seeded, for the same reason only 2024 tax figures are: it is
// the year this repository can cite. Later years resolve as projected and say
// so. Adding a year is a data change — append a record with its own
// provenance.
var povertyGuidelines = []PovertyGuidelineRecord{
	{
		Year:                2024,
		OnePerson:           15060,
		PerAdditionalPerson: 5380,
		Provenance: ConstantProvenance{
			Source:     "HHS annual poverty guidelines for 2024, 48 contiguous states and DC (published in the Federal Register)",
			VerifiedOn: "2024-11-09",
		},
	},
}

// ResolvedPovertyGuideline is a lookup outcome with its provenance, mirroring
// ResolvedTaxYear.
type ResolvedPovertyGuideline struct {
	Record PovertyGuidelineRecord
	Basis  ConstantBasis
	// DerivedFromYear is the published year a projected record was
	// extrapolated from.
	DerivedFromYear int
	InflationFactor float64
	// GuidelineYear is the year whose table governs the coverage year asked
	// about — one earlier, per the open-enrolment lookback.
	GuidelineYear int
}

// Projected reports whether the figures are a forecast rather than published.
func (r ResolvedPovertyGuideline) Projected() bool { return r.Basis == BasisProjected }

// FederalPovertyLevel returns the poverty level for a household size under
// these guidelines.
func (r ResolvedPovertyGuideline) FederalPovertyLevel(householdSize int) float64 {
	return models.FederalPovertyLevel(r.Record.OnePerson, r.Record.PerAdditionalPerson, householdSize)
}

// PovertyGuidelineFor returns the guidelines that govern a given COVERAGE
// year, applying the open-enrolment lookback: coverage for year Y is measured
// against the guidelines published for Y-1.
//
// Years with no published table are extrapolated from the most recent one and
// marked projected. A coverage year earlier than any table on file is an
// error, for the same reason the tax tables refuse one: no forecast produces
// the past.
func (tc *TaxCalculator) PovertyGuidelineFor(coverageYear int) (ResolvedPovertyGuideline, error) {
	guidelineYear := coverageYear - 1

	if earliest := povertyGuidelines[0].Year; guidelineYear < earliest {
		return ResolvedPovertyGuideline{}, fmt.Errorf(
			"poverty guidelines: coverage year %d is measured against the %d guidelines, "+
				"but the earliest on file is %d", coverageYear, guidelineYear, earliest)
	}

	for _, rec := range povertyGuidelines {
		if rec.Year == guidelineYear {
			return ResolvedPovertyGuideline{
				Record:          rec,
				Basis:           BasisStatutory,
				DerivedFromYear: rec.Year,
				InflationFactor: 1,
				GuidelineYear:   guidelineYear,
			}, nil
		}
	}

	base := povertyGuidelines[len(povertyGuidelines)-1]
	factor := 1.0
	if tc != nil {
		factor = tc.InflationFactor(guidelineYear - base.Year)
	}
	projected := base
	projected.Year = guidelineYear
	projected.OnePerson = base.OnePerson * factor
	projected.PerAdditionalPerson = base.PerAdditionalPerson * factor

	return ResolvedPovertyGuideline{
		Record:          projected,
		Basis:           BasisProjected,
		DerivedFromYear: base.Year,
		InflationFactor: factor,
		GuidelineYear:   guidelineYear,
	}, nil
}
