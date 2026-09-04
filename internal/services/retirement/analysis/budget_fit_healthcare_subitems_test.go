package analysis

import (
	"fmt"
	"math"
	"testing"

	"budget2/internal/models"
)

// The Healthcare row must carry one sub-item per HealthcarePerson, in
// HealthcarePersons order, so the lump figure is traceable to the
// Healthcare card's individual premiums rather than ledger spending.
func TestBudgetFit_HealthcareSubItems_TwoPersons(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                  "Darrell",
			CurrentAge:            67,
			CurrentCoverage:       models.CoverageMedicare,
			CurrentMonthlyCost:    600,
			MedicareEligibleAge:   65,
			PostMedicareInflation: 4.0,
		},
		{
			Name:                  "Christine",
			CurrentAge:            54,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1655.30,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)

	item := healthcareItem(t, result)
	if math.Abs(item.Amount-2255.30) > 0.005 {
		t.Errorf("Healthcare amount = %.2f, want 2255.30", item.Amount)
	}
	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}

	medicare := item.SubItems[0]
	aca := item.SubItems[1]

	if medicare.Name != "Darrell (Medicare)" {
		t.Errorf("sub-item[0] name = %q, want %q", medicare.Name, "Darrell (Medicare)")
	}
	if math.Abs(medicare.Amount-600.00) > 0.005 {
		t.Errorf("sub-item[0] amount = %.2f, want 600.00", medicare.Amount)
	}
	if medicare.SignedAmount {
		t.Errorf("sub-item[0] SignedAmount must be false")
	}

	if aca.Name != "Christine (ACA)" {
		t.Errorf("sub-item[1] name = %q, want %q", aca.Name, "Christine (ACA)")
	}
	if math.Abs(aca.Amount-1655.30) > 0.005 {
		t.Errorf("sub-item[1] amount = %.2f, want 1655.30", aca.Amount)
	}
	if aca.SignedAmount {
		t.Errorf("sub-item[1] SignedAmount must be false")
	}
}

// Ruling 2026-08-29b: "sum must hold" means the RENDERED strings, not the
// underlying floats. A fractional-cent fixture must still produce sub-row
// strings (via the same "%.2f" path formatMoney/centsFromDecimalString use)
// that sum exactly to the rendered Healthcare row string.
func TestBudgetFit_HealthcareSubItems_FractionalCentIdentity(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:            "Darrell",
			CurrentAge:      67,
			CurrentCoverage: models.CoverageMedicare,
			// 600.006 renders as 600.01 and 1655.306 as 1655.31, but the
			// total 2255.312 renders as 2255.31: the naive rendered sum is
			// one cent high, so the residual is NON-zero and this fixture
			// discriminates (600.005/1655.305 had residual 0 and let the
			// absorption branch be deleted unnoticed — EX1 attempt 1).
			CurrentMonthlyCost:    600.006,
			MedicareEligibleAge:   65,
			PostMedicareInflation: 4.0,
		},
		{
			Name:                  "Christine",
			CurrentAge:            54,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1655.306,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)
	item := healthcareItem(t, result)

	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}

	rowRendered := fmt.Sprintf("%.2f", item.Amount)
	if rowRendered != "2255.31" {
		t.Fatalf("rendered Healthcare row = %s, want 2255.31 (fixture no longer discriminates)", rowRendered)
	}
	var subCents int64
	for _, sub := range item.SubItems {
		subCents += centsFromDecimalString(sub.Amount)
	}
	subSumRendered := fmt.Sprintf("%.2f", float64(subCents)/100)
	if subSumRendered != rowRendered {
		t.Errorf("rendered sub-row sum %s != rendered Healthcare row %s", subSumRendered, rowRendered)
	}
	// The residual lands on the LAST row: first row renders its own
	// "%.2f" value; the last row is one cent below its naive rendering.
	if got := fmt.Sprintf("%.2f", item.SubItems[0].Amount); got != "600.01" {
		t.Errorf("first sub-row rendered %s, want 600.01", got)
	}
	if got := fmt.Sprintf("%.2f", item.SubItems[1].Amount); got != "1655.30" {
		t.Errorf("last sub-row rendered %s, want 1655.30 (absorbs the -1 cent residual)", got)
	}
}

// A person whose month-0 contribution is 0 (e.g. still on indefinite
// employer coverage) still gets a sub-row at 0 — provenance is the point.
func TestBudgetFit_HealthcareSubItems_ZeroCostPersonStillGetsRow(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                  "Darrell",
			CurrentAge:            60,
			CurrentCoverage:       models.CoverageEmployer,
			CurrentMonthlyCost:    0,
			EmployerCoverageYears: 0, // indefinite
			MedicareEligibleAge:   65,
			PostMedicareInflation: 4.0,
			MedicareMonthlyCost:   600,
		},
		{
			Name:                  "Christine",
			CurrentAge:            54,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1655.30,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)
	item := healthcareItem(t, result)

	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}
	zero := item.SubItems[0]
	if zero.Name != "Darrell (Employer)" {
		t.Errorf("sub-item[0] name = %q, want %q", zero.Name, "Darrell (Employer)")
	}
	if zero.Amount != 0 {
		t.Errorf("sub-item[0] amount = %.2f, want 0.00", zero.Amount)
	}
}

// Legacy single-scalar healthcare (no HealthcarePersons) gets no sub-rows.
func TestBudgetFit_HealthcareSubItems_AbsentForLegacyScalar(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyHealthcare = 500
	s.HealthcareStartYears = 0
	s.HealthcarePersons = nil

	in := engineInput(t, s)
	result := BudgetFit(in, nil)
	item := healthcareItem(t, result)
	if item.SubItems != nil {
		t.Errorf("expected no sub-items for legacy scalar healthcare, got %+v", item.SubItems)
	}
}

// The existing "$0 / employer covered" branch (total healthcare cost is 0
// across all persons) is unchanged: no sub-rows, note text intact.
func TestBudgetFit_HealthcareSubItems_AbsentWhenTotalZero(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                  "Darrell",
			CurrentAge:            60,
			CurrentCoverage:       models.CoverageEmployer,
			CurrentMonthlyCost:    0,
			EmployerCoverageYears: 0, // indefinite
			MedicareEligibleAge:   65,
			PostMedicareInflation: 4.0,
			MedicareMonthlyCost:   0,
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)
	item := healthcareItem(t, result)
	if item.SubItems != nil {
		t.Errorf("expected no sub-items when total healthcare cost is 0, got %+v", item.SubItems)
	}
	if item.Note != "employer covered" {
		t.Errorf("note = %q, want %q", item.Note, "employer covered")
	}
}

// Care cost at month 0 (CareStartAge already reached) is included in that
// person's sub-row, exactly the two terms GetTotalHealthcareCost sums.
//
// The person with care must NOT be the last row: the last row's amount is
// re-derived from the row total (residual absorption), so a single-person
// fixture passes even when CareCostAt is dropped from the per-person sum —
// the care dollars silently move to whoever renders last (EX1 attempt 1).
func TestBudgetFit_HealthcareSubItems_IncludesCareCostAtMonthZero(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                  "Darrell",
			CurrentAge:            80,
			CurrentCoverage:       models.CoverageMedicare,
			CurrentMonthlyCost:    600,
			MedicareEligibleAge:   65,
			PostMedicareInflation: 4.0,
			CareStartAge:          80,
			CareMonthlyCost:       3000,
		},
		{
			Name:                  "Christine",
			CurrentAge:            54,
			CurrentCoverage:       models.CoverageACA,
			CurrentMonthlyCost:    1655.30,
			PreMedicareInflation:  7.0,
			MedicareMonthlyCost:   600,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
	}

	in := engineInput(t, s)
	result := BudgetFit(in, nil)
	item := healthcareItem(t, result)

	if len(item.SubItems) != 2 {
		t.Fatalf("expected 2 sub-items, got %d: %+v", len(item.SubItems), item.SubItems)
	}
	if got := fmt.Sprintf("%.2f", item.Amount); got != "5255.30" {
		t.Errorf("Healthcare row rendered %s, want 5255.30", got)
	}
	if got := fmt.Sprintf("%.2f", item.SubItems[0].Amount); got != "3600.00" {
		t.Errorf("Darrell sub-row rendered %s, want 3600.00 (Medicare premium + care cost)", got)
	}
	if item.SubItems[0].Name != "Darrell (Medicare)" {
		t.Errorf("sub-item name = %q, want %q", item.SubItems[0].Name, "Darrell (Medicare)")
	}
	if got := fmt.Sprintf("%.2f", item.SubItems[1].Amount); got != "1655.30" {
		t.Errorf("Christine sub-row rendered %s, want 1655.30 (care must not leak onto the last row)", got)
	}
}

func healthcareItem(t *testing.T, result *models.BudgetFitAnalysis) models.ExpenseBreakdownItem {
	t.Helper()
	for _, item := range result.ExpenseBreakdown {
		if item.Name == "Healthcare" {
			return item
		}
	}
	t.Fatalf("no Healthcare row in ExpenseBreakdown: %+v", result.ExpenseBreakdown)
	return models.ExpenseBreakdownItem{}
}
