package engine

import (
	"math"
	"strings"
	"testing"

	"budget2/internal/models"
)

func versionedCalculator(t *testing.T, inflation float64) *TaxCalculator {
	t.Helper()
	zero := 0.0
	return NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
		Age65Count:         1,
	}, inflation)
}

// withFederalTaxYears swaps the statutory table for the duration of a test.
// Used to exercise multi-year and mid-year resolution without shipping
// figures this repository cannot cite.
func withFederalTaxYears(t *testing.T, records []TaxYearRecord) {
	t.Helper()
	original := federalTaxYears
	federalTaxYears = records
	t.Cleanup(func() { federalTaxYears = original })
}

func TestResolveTaxYear_StatutoryYear(t *testing.T) {
	tc := versionedCalculator(t, 3.0)

	resolved, err := tc.ResolveTaxYear(taxBaseYear, 1)
	if err != nil {
		t.Fatalf("ResolveTaxYear(%d): %v", taxBaseYear, err)
	}
	if resolved.Basis != BasisStatutory {
		t.Errorf("Basis = %q, want statutory for the base year", resolved.Basis)
	}
	if resolved.Projected() {
		t.Error("Projected() true for a published year")
	}
	if resolved.InflationFactor != 1 {
		t.Errorf("InflationFactor = %v, want 1 — published figures are not scaled",
			resolved.InflationFactor)
	}
	if resolved.Record.Provenance.Source == "" || resolved.Record.Provenance.VerifiedOn == "" {
		t.Errorf("published figures must carry a source and a verified-on date, got %+v",
			resolved.Record.Provenance)
	}
}

// TestResolveTaxYear_LabelsProjectionRatherThanPretending is the heart of §7:
// a year with no published figures must come back marked as a forecast, not
// silently as last year's table.
func TestResolveTaxYear_LabelsProjectionRatherThanPretending(t *testing.T) {
	tc := versionedCalculator(t, 3.0)
	const future = taxBaseYear + 10

	resolved, err := tc.ResolveTaxYear(future, 1)
	if err != nil {
		t.Fatalf("ResolveTaxYear(%d): %v", future, err)
	}
	if resolved.Basis != BasisProjected || !resolved.Projected() {
		t.Errorf("Basis = %q; a year with no published figures must be marked projected",
			resolved.Basis)
	}
	if resolved.DerivedFromYear != taxBaseYear {
		t.Errorf("DerivedFromYear = %d, want %d — a forecast must say what it was built from",
			resolved.DerivedFromYear, taxBaseYear)
	}
	if want := math.Pow(1.03, 10); math.Abs(resolved.InflationFactor-want) > 1e-9 {
		t.Errorf("InflationFactor = %v, want %v", resolved.InflationFactor, want)
	}
	if resolved.Record.Year != future {
		t.Errorf("Record.Year = %d, want %d", resolved.Record.Year, future)
	}

	// The projected figures must actually be scaled, not the base table
	// handed back under a different label.
	base, _ := tc.ResolveTaxYear(taxBaseYear, 1)
	baseTop := bracketsFor(base.Record.OrdinaryBrackets, models.FilingMarriedJoint)[1].MaxIncome
	futureTop := bracketsFor(resolved.Record.OrdinaryBrackets, models.FilingMarriedJoint)[1].MaxIncome
	if futureTop <= baseTop {
		t.Errorf("projected bracket top %.2f did not move from the base %.2f", futureTop, baseTop)
	}
}

// TestResolveTaxYear_FailsLoudlyBeforeTheEarliestYear — no forecast can
// produce the past, so a caller asking for a year with no figures on file has
// a bug and should hear about it.
func TestResolveTaxYear_FailsLoudlyBeforeTheEarliestYear(t *testing.T) {
	tc := versionedCalculator(t, 3.0)
	missing := EarliestFederalTaxYear() - 1

	_, err := tc.ResolveTaxYear(missing, 1)
	if err == nil {
		t.Fatalf("ResolveTaxYear(%d) succeeded; want an error rather than a silent "+
			"fallback to the nearest table", missing)
	}
	for _, want := range []string{"no figures on file", "earliest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q so the cause is obvious", err, want)
		}
	}
}

// TestResolveTaxYear_MidYearEffectiveDate — a statute or program can change
// partway through a year, and both halves are then real. The latest record at
// or before the month wins.
func TestResolveTaxYear_MidYearEffectiveDate(t *testing.T) {
	const year = 2099
	early := TaxYearRecord{
		Jurisdiction: JurisdictionUS, Year: year, EffectiveFromMonth: 1,
		Provenance:               ConstantProvenance{Source: "first half", VerifiedOn: "2099-01-01"},
		OrdinaryBrackets:         TaxBrackets2024,
		LongTermGainBrackets:     LongTermCapitalGainsBrackets2024,
		StandardDeduction:        map[models.FilingStatus]float64{models.FilingMarriedJoint: 30000},
		AdditionalDeductionAge65: AdditionalStandardDeduction2024Age65,
	}
	late := early
	late.EffectiveFromMonth = 7
	late.Provenance = ConstantProvenance{Source: "second half", VerifiedOn: "2099-07-01"}
	late.StandardDeduction = map[models.FilingStatus]float64{models.FilingMarriedJoint: 40000}

	withFederalTaxYears(t, []TaxYearRecord{early, late})
	tc := versionedCalculator(t, 3.0)

	cases := []struct {
		month      int
		wantSource string
		wantAmount float64
	}{
		{month: 1, wantSource: "first half", wantAmount: 30000},
		{month: 6, wantSource: "first half", wantAmount: 30000},
		{month: 7, wantSource: "second half", wantAmount: 40000},
		{month: 12, wantSource: "second half", wantAmount: 40000},
	}

	for _, c := range cases {
		resolved, err := tc.ResolveTaxYear(year, c.month)
		if err != nil {
			t.Fatalf("month %d: %v", c.month, err)
		}
		if resolved.Basis != BasisStatutory {
			t.Errorf("month %d: Basis = %q, want statutory", c.month, resolved.Basis)
		}
		if got := resolved.Record.Provenance.Source; got != c.wantSource {
			t.Errorf("month %d: source = %q, want %q", c.month, got, c.wantSource)
		}
		if got := resolved.Record.StandardDeduction[models.FilingMarriedJoint]; got != c.wantAmount {
			t.Errorf("month %d: deduction = %.0f, want %.0f", c.month, got, c.wantAmount)
		}
	}
}

// TestResolveTaxYear_ProjectsFromTheLatestStatutoryYear — adding a newer
// statutory record must change what later years are extrapolated from,
// without touching any calling code.
func TestResolveTaxYear_ProjectsFromTheLatestStatutoryYear(t *testing.T) {
	newer := federalTaxYears[0]
	newer.Year = taxBaseYear + 2
	newer.Provenance = ConstantProvenance{Source: "later authority", VerifiedOn: "2026-01-15"}
	withFederalTaxYears(t, []TaxYearRecord{federalTaxYears[0], newer})

	if got := LatestStatutoryFederalTaxYear(); got != taxBaseYear+2 {
		t.Fatalf("LatestStatutoryFederalTaxYear = %d, want %d", got, taxBaseYear+2)
	}

	tc := versionedCalculator(t, 3.0)
	resolved, err := tc.ResolveTaxYear(taxBaseYear+5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.DerivedFromYear != taxBaseYear+2 {
		t.Errorf("DerivedFromYear = %d, want the newest statutory year %d",
			resolved.DerivedFromYear, taxBaseYear+2)
	}
	// Three years of extrapolation, not five.
	if want := math.Pow(1.03, 3); math.Abs(resolved.InflationFactor-want) > 1e-9 {
		t.Errorf("InflationFactor = %v, want %v", resolved.InflationFactor, want)
	}
}

// TestVersionedConstants_PreserveExistingValues pins the migration: routing
// the accessors through the versioned store must not move a single number.
func TestVersionedConstants_PreserveExistingValues(t *testing.T) {
	tc := versionedCalculator(t, 3.0)

	for _, yearsFromBase := range []int{-3, 0, 1, 7, 25} {
		factor := tc.InflationFactor(yearsFromBase)

		wantDeduction := (StandardDeduction2024[models.FilingMarriedJoint] +
			AdditionalStandardDeduction2024Age65[models.FilingMarriedJoint]) * factor
		if got := tc.GetAdjustedStandardDeduction(yearsFromBase); math.Abs(got-wantDeduction) > 1e-6 {
			t.Errorf("yearsFromBase %d: standard deduction = %.6f, want %.6f",
				yearsFromBase, got, wantDeduction)
		}

		got := tc.GetAdjustedBrackets(yearsFromBase)
		want := TaxBrackets2024[models.FilingMarriedJoint]
		for i := range want {
			if math.Abs(got[i].MinIncome-want[i].MinIncome*factor) > 1e-6 {
				t.Errorf("yearsFromBase %d bracket %d: min %.6f, want %.6f",
					yearsFromBase, i, got[i].MinIncome, want[i].MinIncome*factor)
			}
			if want[i].MaxIncome == math.MaxFloat64 {
				if got[i].MaxIncome != math.MaxFloat64 {
					t.Errorf("yearsFromBase %d bracket %d: unbounded top edge was scaled to %v",
						yearsFromBase, i, got[i].MaxIncome)
				}
				continue
			}
			if math.Abs(got[i].MaxIncome-want[i].MaxIncome*factor) > 1e-6 {
				t.Errorf("yearsFromBase %d bracket %d: max %.6f, want %.6f",
					yearsFromBase, i, got[i].MaxIncome, want[i].MaxIncome*factor)
			}
		}
	}
}

// TestVersionedConstants_ProjectionDoesNotMutateTheStatutoryTable — the
// projected record shares maps with the base record unless scaled, so a bug
// here would silently corrupt every later lookup.
func TestVersionedConstants_ProjectionDoesNotMutateTheStatutoryTable(t *testing.T) {
	tc := versionedCalculator(t, 3.0)
	before := TaxBrackets2024[models.FilingMarriedJoint][1].MaxIncome

	if _, err := tc.ResolveTaxYear(taxBaseYear+20, 1); err != nil {
		t.Fatal(err)
	}
	if after := TaxBrackets2024[models.FilingMarriedJoint][1].MaxIncome; after != before {
		t.Errorf("statutory table mutated by a projection: %.2f -> %.2f", before, after)
	}
}
