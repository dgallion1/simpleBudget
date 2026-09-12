package whatif

import (
	"budget2/internal/models"
	"strings"
	"testing"
)

func TestLifetimeCalculationErrorRendersOnlyNeutralUnavailableResult(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	a := &models.WhatIfAnalysis{CalculationError: "tax settlement did not converge", Settings: s, Projection: &models.ProjectionResult{CalculationError: "tax settlement did not converge", FinalBalance: 987654, Survives: true}}
	out, err := renderer.RenderToString("whatif-results", buildResultsPartialData(s, a, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="status"`, `Calculation unavailable`, `tax settlement did not converge`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	for _, stale := range []string{`id="whatif-tabs"`, `987,654`, `Funded through`, `data-projection-chart`} {
		if strings.Contains(out, stale) {
			t.Errorf("stale success %s leaked", stale)
		}
	}
}

func TestLifetimeAnnualExplanationUsesSemanticTablesAndPartialLabels(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.Lifetime = &models.LifetimeSettings{Version: 1}
	y := models.LifetimeYearSummary{Year: 2026, FirstMonth: 9, LastMonth: 12, PartialYear: true, Available: true, ExternalIncome: 1000, EmployeeContributions: 100, EmployerContributions: 50, Consumption: 600, ConsumptionAssessed: 625, TaxLiability: 90, TaxPayments: 80, UnpaidTax: 10, ReserveGap: 20, Accounts: []models.AccountReconciliation{{AccountID: "cash", Opening: 200, Deposits: 1050, Withdrawals: 680, Return: 1.25, Closing: 571.25}}, RMD: []models.LifetimeRMDFact{{AccountID: "ira", Year: 2026, Obligation: 40, Distributed: 30, Taxable: 30}}}
	a := &models.WhatIfAnalysis{Settings: s, Projection: &models.ProjectionResult{LifetimeYearSummaries: []models.LifetimeYearSummary{y}}}
	out, err := renderer.RenderToString("whatif-lifetime-year", buildResultsPartialData(s, a, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<select`, `2026 (months 9–12, partial)`, `<table`, `scope="col"`, `External gross income`, `Employee allocation`, `Actual taxes paid`, `Unpaid tax`, `Reserve gap`, `RMD obligation`, `Cash reinvested (all sources)`, `Account reconciliation`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}
func TestLifetimeCalculationErrorSuppressesChartsAndTrajectory(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	p := &models.ProjectionResult{CalculationError: "failed", Months: []models.ProjectionMonth{{PortfolioBalance: 999, TotalIncome: 888}}}
	pc := buildProjectionChartData(s, p, "nominal")
	if pc["calculationError"] != "failed" || len(pc["data"].([]map[string]interface{})) != 0 {
		t.Fatalf("projection chart=%+v", pc)
	}
	ic := buildIncomeChartData(s, p, "nominal")
	if ic["calculationError"] != "failed" || len(ic["data"].([]map[string]interface{})) != 0 {
		t.Fatalf("income chart=%+v", ic)
	}
	if rows := buildSpendingTrajectoryRows(s, p); rows != nil {
		t.Fatalf("trajectory=%+v", rows)
	}
}

func TestLifetimeYearAccountNameAndRenderedEquationAdjustment(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "acct-internal-17", Name: "Household reserve"}}}
	y := models.AggregateLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 1, ExternalIncome: .004, Accounts: []models.AccountReconciliation{{AccountID: "acct-internal-17", AccountName: "Household reserve", Opening: .004, Deposits: .004, Closing: .008}}}})
	a := &models.WhatIfAnalysis{Settings: s, Projection: &models.ProjectionResult{LifetimeYearSummaries: []models.LifetimeYearSummary{y}}}
	out, err := renderer.RenderToString("whatif-lifetime-year", buildResultsPartialData(s, a, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Household reserve") {
		t.Fatalf("account name missing: %s", out)
	}
	if !strings.Contains(out, "Account rounding adjustment") {
		t.Fatalf("rendered equation lacks adjustment: %s", out)
	}
}
