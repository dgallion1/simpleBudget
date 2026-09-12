package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"strings"
	"testing"
)

func TestLifetimeCalculationErrorDefensivelySuppressesDirectAnalyses(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	p := &models.ProjectionResult{CalculationError: "failed", Months: []models.ProjectionMonth{{PortfolioBalance: 1}}}
	if BuildExplainability(p, in) != nil || BudgetFit(in, p) != nil || PresentValue(in, p) != nil || BuildRMD(p, in) != nil || BuildTax(p, in) != nil {
		t.Fatal("direct analysis shaped failed projection")
	}
}
func TestLifetimeTaxOptimizerExplicitlyUnavailable(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Lifetime = &models.LifetimeSettings{Version: 1}
	ok, reason := taxOptimizerEligible(s)
	if ok || !strings.Contains(reason, "lifetime") {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}
func TestLifetimePresentValueUsesCanonicalCompleteRecords(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.ProjectionYears = 1
	s.DiscountRate = 0
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "cash", OpeningValue: 100}}}
	months := make([]models.ProjectionMonth, 12)
	for i := range months {
		months[i].Lifetime = &models.LifetimeMonthOutcome{Year: 2026, Month: i + 1, ExternalIncome: 10, EmployerContributions: 2, ConsumptionAssessed: 8, TaxLiability: 1}
	}
	months[0].Lifetime.Accounts = []models.AccountReconciliation{{AccountID: "cash", Opening: 100, Closing: 100}}
	got := presentValueLifetime(s, &models.ProjectionResult{Months: months})
	if !got.Available || got.StartingAssets != 100 || got.PVIncome != 144 || got.PVExpenses != 96 || got.PVTaxes != 12 || got.SurplusDeficit != 136 {
		t.Fatalf("PV=%+v", got)
	}
	months = months[:11]
	bad := presentValueLifetime(s, &models.ProjectionResult{Months: months})
	if bad.Available || bad.UnavailableReason == "" {
		t.Fatalf("incomplete=%+v", bad)
	}
}
func TestLifetimeBudgetFitUsesSpendableIncomeNotGrossResources(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	prepared := prepare.MustFrom(t, s)
	prepared.Settings().Lifetime = &models.LifetimeSettings{Version: 1}
	p := &models.ProjectionResult{Months: []models.ProjectionMonth{{Lifetime: &models.LifetimeMonthOutcome{ExternalIncome: 10000, EmployeeContributions: 1000, EmployerContributions: 500, ConsumptionAssessed: 8000, TaxLiability: 500}}}}
	got := BudgetFit(engine.Input{Prepared: prepared}, p)
	if got.MonthlyIncome != 9000 || got.GrossIncome != 10000 || got.MonthlyExpenses != 8000 || got.MonthlyTaxes != 500 || got.MonthlyGap != -500 {
		t.Fatalf("budget=%+v", got)
	}
}
