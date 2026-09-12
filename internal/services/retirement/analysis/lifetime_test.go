package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func closeLifetime(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSummarizeLifetimeYearUsesCanonicalCalendarFacts(t *testing.T) {
	prior := 1000.0
	in := []models.LifetimeMonthOutcome{
		{Year: 2026, Month: 11, ExternalIncome: 100, EmployeeContributions: 10, EmployerContributions: 5, ConsumptionAssessed: 30, ConsumptionPaid: 25, TaxLiability: 12, TaxPayments: 8, UnpaidTax: 4, UnfundedExpenses: 5, UnfundedSaving: 2, ReserveGap: 9, AllSourceReinvestment: 7,
			Accounts: []models.AccountReconciliation{{AccountID: "a", Opening: 1000, Deposits: 105, Withdrawals: 33, Return: 10, Closing: 1082}},
			RMD:      []models.LifetimeRMDFact{{AccountID: "a", Year: 2026, Obligation: 40, Distributed: 30, Credited: 30, Taxable: 30}}},
		{Year: 2026, Month: 12, ExternalIncome: 200, EmployeeContributions: 20, EmployerContributions: 10, ConsumptionAssessed: 50, ConsumptionPaid: 50, TaxLiability: 15, TaxPayments: 15, ReserveGap: 3, AllSourceReinvestment: 2,
			Accounts: []models.AccountReconciliation{{AccountID: "a", Opening: 1082, Deposits: 210, Withdrawals: 65, Return: 20, Closing: 1247}},
			RMD:      []models.LifetimeRMDFact{{AccountID: "a", Year: 2026, Obligation: 40, Distributed: 10, Credited: 10, Taxable: 10}}},
	}
	got := SummarizeLifetimeYear(in)
	if got.Year != 2026 || !got.PartialYear || got.FirstMonth != 11 || got.LastMonth != 12 {
		t.Fatalf("calendar metadata = %+v", got)
	}
	if !closeLifetime(got.ExternalIncome, 300) || !closeLifetime(got.EmployeeContributions, 30) || !closeLifetime(got.EmployerContributions, 15) {
		t.Fatalf("income/contributions = %+v", got)
	}
	if !closeLifetime(got.Consumption, 75) || !closeLifetime(got.ConsumptionAssessed, 80) || !closeLifetime(got.TaxLiability, 27) || !closeLifetime(got.TaxPayments, 23) || !closeLifetime(got.UnpaidTax, 4) {
		t.Fatalf("needs/payments = %+v", got)
	}
	if !closeLifetime(got.ReserveGap, 3) || !closeLifetime(got.UnfundedExpenses, 5) || !closeLifetime(got.UnfundedSaving, 2) || !closeLifetime(got.AllSourceReinvestment, 9) {
		t.Fatalf("stocks/flows = %+v", got)
	}
	if len(got.RMD) != 1 || !closeLifetime(got.RMD[0].Obligation, 40) || !closeLifetime(got.RMD[0].Distributed, 40) {
		t.Fatalf("RMD annual dedupe = %+v", got.RMD)
	}
	if !closeLifetime(got.OpeningWealth, 1000) || !closeLifetime(got.ClosingWealth, 1247) || got.HouseholdRoundingAdjustment != 0 {
		t.Fatalf("household equation = %+v", got)
	}
	if len(got.Accounts) != 1 || !closeLifetime(got.Accounts[0].Opening, prior) || !closeLifetime(got.Accounts[0].Deposits, 315) || !closeLifetime(got.Accounts[0].Withdrawals, 98) || !closeLifetime(got.Accounts[0].Return, 30) || !closeLifetime(got.Accounts[0].Closing, 1247) {
		t.Fatalf("account summary = %+v", got.Accounts)
	}
}

func TestSummarizeLifetimeYearRejectsMixedYears(t *testing.T) {
	got := SummarizeLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 12}, {Year: 2027, Month: 1}})
	if got.Available || got.UnavailableReason == "" {
		t.Fatalf("mixed years must be unavailable: %+v", got)
	}
}

func TestSummarizeLifetimeYearDoesNotHideFinancialMismatchAsRounding(t *testing.T) {
	got := SummarizeLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 1, ExternalIncome: 100, Accounts: []models.AccountReconciliation{{AccountID: "cash", Opening: 0, Closing: 90}}}})
	if got.Available || got.UnavailableReason == "" || math.Abs(got.HouseholdRoundingAdjustment) > 0 {
		t.Fatalf("mismatch hidden=%+v", got)
	}
}
func TestSummarizeLifetimeYearJanAndDecemberOnlyIsPartial(t *testing.T) {
	got := SummarizeLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 1}, {Year: 2026, Month: 12}})
	if !got.Available || !got.PartialYear {
		t.Fatalf("sparse year=%+v", got)
	}
}
func TestSummarizeLifetimeYearLabelsFractionalCentDisplayAdjustment(t *testing.T) {
	got := SummarizeLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 1, ExternalIncome: .005, EmployerContributions: .005, Accounts: []models.AccountReconciliation{{AccountID: "cash", Opening: 0, Deposits: .01, Closing: .01}}}})
	if !got.Available || math.Abs(got.HouseholdRoundingAdjustment) > .03 || got.HouseholdRoundingAdjustment == 0 {
		t.Fatalf("adjustment=%+v", got)
	}
}
