package engine

import (
	"fmt"

	"budget2/internal/models"
)

// Versioned tax constants.
//
// Tax figures are versioned data with effective dates, not literals. A number
// typed inline has no year, no source, and no expiry, so nobody can tell
// whether an answer rests on current law or on a table that moved two
// Revenue Procedures ago.
//
// Every set of federal figures here is keyed by (jurisdiction, tax year,
// effective month) and carries where it came from and when it was last
// checked. Mid-year effective dates are supported because they happen: a
// statute or state program can change on 1 July and both halves of the year
// are then real.
//
// On "fail loudly on a missing year": a retirement projector must reason
// about years for which no legislature has published anything, so refusing to
// project is not an option. The loudness here is labelling instead of
// silence. ResolveTaxYear never quietly hands back last year's table as
// though it were law — it marks the result BasisProjected, records which
// statutory year it was extrapolated from, and lets callers and the UI say
// so. Asking for a year EARLIER than any record is a different matter: that
// is a bug rather than a forecast, and it is an error.

// ConstantBasis says whether a figure is law or a forecast.
type ConstantBasis string

const (
	// BasisStatutory means the figures are as published for that tax year.
	BasisStatutory ConstantBasis = "statutory"
	// BasisProjected means they were extrapolated from a statutory year by
	// applying assumed inflation. Real indexing rounds to $25/$50/$100 steps
	// and some figures are not indexed at all, so a projected table is an
	// estimate and must never be presented as law.
	BasisProjected ConstantBasis = "projected"
)

// ConstantProvenance records where a set of figures came from.
type ConstantProvenance struct {
	// Source is the authority: a Revenue Procedure, a CFR cite, a URL.
	Source string
	// VerifiedOn is when a human last checked Source against these numbers,
	// as YYYY-MM-DD. Figures nobody has re-checked in years are worth
	// surfacing even when nothing has changed.
	VerifiedOn string
}

// TaxYearRecord is one jurisdiction's federal income-tax figures, effective
// from a given month of a given tax year.
type TaxYearRecord struct {
	Jurisdiction string
	Year         int
	// EffectiveFromMonth is 1-12. A year with one record uses 1. A mid-year
	// change is a second record for the same year with a later month; the
	// latest record at or before the month in question wins.
	EffectiveFromMonth int
	Provenance         ConstantProvenance

	OrdinaryBrackets         map[models.FilingStatus][]FederalTaxBracket
	LongTermGainBrackets     map[models.FilingStatus][]FederalTaxBracket
	StandardDeduction        map[models.FilingStatus]float64
	AdditionalDeductionAge65 map[models.FilingStatus]float64
}

// JurisdictionUS is the only jurisdiction with figures today. State income
// tax is still a single flat rate with no notion of which income types a
// state excludes, so there is nothing yet to version.
const JurisdictionUS = "US"

// federalTaxYears holds every statutory record, ascending by (year, month).
//
// Only 2024 is seeded, because 2024 is the only year whose figures this
// repository can cite: IRS Rev. Proc. 2023-34, already the stated source of
// the bundled tables. Later statutory years are a data entry, not a code
// change — append a record with its own Provenance and every consumer picks
// it up. Until then, later years resolve as BasisProjected and say so.
var federalTaxYears = []TaxYearRecord{
	{
		Jurisdiction:       JurisdictionUS,
		Year:               taxBaseYear,
		EffectiveFromMonth: 1,
		Provenance: ConstantProvenance{
			Source:     "IRS Rev. Proc. 2023-34 (tax year 2024); §3.16(2) for the age-65 additional deduction",
			VerifiedOn: "2024-11-09",
		},
		OrdinaryBrackets:         TaxBrackets2024,
		LongTermGainBrackets:     LongTermCapitalGainsBrackets2024,
		StandardDeduction:        StandardDeduction2024,
		AdditionalDeductionAge65: AdditionalStandardDeduction2024Age65,
	},
}

// ResolvedTaxYear is the outcome of a lookup: the figures to use, and an
// honest account of where they came from.
type ResolvedTaxYear struct {
	Record TaxYearRecord
	Basis  ConstantBasis
	// DerivedFromYear is the statutory year a projected record was
	// extrapolated from. Equal to Record.Year when Basis is statutory.
	DerivedFromYear int
	// InflationFactor is the multiplier applied to the statutory figures.
	// 1 for a statutory record.
	InflationFactor float64
}

// Projected reports whether these figures are a forecast rather than law.
func (r ResolvedTaxYear) Projected() bool { return r.Basis == BasisProjected }

// EarliestFederalTaxYear is the first year with figures on file. Asking for
// anything earlier is an error rather than a forecast.
func EarliestFederalTaxYear() int { return federalTaxYears[0].Year }

// LatestStatutoryFederalTaxYear is the most recent year whose figures are as
// published. Anything after it can only be projected.
func LatestStatutoryFederalTaxYear() int {
	return federalTaxYears[len(federalTaxYears)-1].Year
}

// statutoryRecordFor returns the record in effect for a calendar year and
// month, honouring mid-year effective dates, and whether one exists.
func statutoryRecordFor(year, month int) (TaxYearRecord, bool) {
	if month < 1 {
		month = 1
	}
	if month > 12 {
		month = 12
	}
	var best TaxYearRecord
	found := false
	for _, rec := range federalTaxYears {
		if rec.Year != year || rec.EffectiveFromMonth > month {
			continue
		}
		if !found || rec.EffectiveFromMonth > best.EffectiveFromMonth {
			best, found = rec, true
		}
	}
	return best, found
}

// ResolveTaxYear returns the federal figures to use for a calendar year and
// month, together with their provenance.
//
// A year with published figures resolves statutory. A later year resolves
// projected: the latest statutory table scaled by the calculator's assumed
// inflation, marked as an estimate and tagged with the year it came from. A
// year before the earliest record is an error — no forecast can produce the
// past, so a caller asking for one has a bug.
func (tc *TaxCalculator) ResolveTaxYear(calendarYear, month int) (ResolvedTaxYear, error) {
	if tc == nil {
		return ResolvedTaxYear{}, fmt.Errorf("tax constants: nil calculator")
	}
	if earliest := EarliestFederalTaxYear(); calendarYear < earliest {
		return ResolvedTaxYear{}, fmt.Errorf(
			"tax constants: no figures on file for %d; the earliest year available is %d",
			calendarYear, earliest)
	}

	if rec, ok := statutoryRecordFor(calendarYear, month); ok {
		return ResolvedTaxYear{
			Record:          rec,
			Basis:           BasisStatutory,
			DerivedFromYear: rec.Year,
			InflationFactor: 1,
		}, nil
	}

	base := federalTaxYears[len(federalTaxYears)-1]
	factor := tc.InflationFactor(calendarYear - base.Year)
	projected := base
	projected.Year = calendarYear
	projected.OrdinaryBrackets = scaleBracketSets(base.OrdinaryBrackets, factor)
	projected.LongTermGainBrackets = scaleBracketSets(base.LongTermGainBrackets, factor)
	projected.StandardDeduction = scaleAmounts(base.StandardDeduction, factor)
	projected.AdditionalDeductionAge65 = scaleAmounts(base.AdditionalDeductionAge65, factor)

	return ResolvedTaxYear{
		Record:          projected,
		Basis:           BasisProjected,
		DerivedFromYear: base.Year,
		InflationFactor: factor,
	}, nil
}

// scaleAmounts multiplies every dollar figure by factor.
func scaleAmounts(in map[models.FilingStatus]float64, factor float64) map[models.FilingStatus]float64 {
	if factor == 1 {
		return in
	}
	out := make(map[models.FilingStatus]float64, len(in))
	for status, amount := range in {
		out[status] = amount * factor
	}
	return out
}

// resolveForYearsFromBase resolves the figures for a projection offset from
// the tax base year.
//
// Negative offsets are clamped to the base year rather than erroring, which
// is the behaviour every existing caller relies on: the engine hands out
// offsets derived from a plan's start date, and a plan starting before the
// base year is a modelling nuisance, not a reason to refuse to project.
// ResolveTaxYear is the honest API and does return an error there.
func (tc *TaxCalculator) resolveForYearsFromBase(yearsFromBase int) ResolvedTaxYear {
	if yearsFromBase < 0 {
		yearsFromBase = 0
	}
	resolved, err := tc.ResolveTaxYear(taxBaseYear+yearsFromBase, 1)
	if err != nil {
		// Unreachable once clamped, but never hand back a zero record.
		return ResolvedTaxYear{
			Record:          federalTaxYears[0],
			Basis:           BasisStatutory,
			DerivedFromYear: federalTaxYears[0].Year,
			InflationFactor: 1,
		}
	}
	return resolved
}

// bracketsFor reads a filing status out of a bracket set, falling back to
// married-filing-jointly the way the tables always have.
func bracketsFor(set map[models.FilingStatus][]FederalTaxBracket, status models.FilingStatus) []FederalTaxBracket {
	if brackets := set[status]; brackets != nil {
		return brackets
	}
	return set[models.FilingMarriedJoint]
}

// scaleBracketSets multiplies every bracket edge by factor, leaving an
// unbounded top edge unbounded.
func scaleBracketSets(in map[models.FilingStatus][]FederalTaxBracket, factor float64) map[models.FilingStatus][]FederalTaxBracket {
	if factor == 1 {
		return in
	}
	out := make(map[models.FilingStatus][]FederalTaxBracket, len(in))
	for status, brackets := range in {
		out[status] = scaleBrackets(brackets, factor)
	}
	return out
}

// LatestStatutoryFederalProvenance returns the most recent year with
// published figures and where those figures came from. Callers surface this
// so a user can see which law an answer rests on and when a human last
// checked it.
func LatestStatutoryFederalProvenance() (int, ConstantProvenance) {
	latest := federalTaxYears[len(federalTaxYears)-1]
	return latest.Year, latest.Provenance
}
