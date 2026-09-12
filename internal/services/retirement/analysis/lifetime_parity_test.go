package analysis

import (
	"reflect"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"budget2/internal/services/retirement/prepare"
)

func TestLifetimeDiagnosticsParityAcrossPublicLoops(t *testing.T) {
	prior := 106000.0
	caps := models.PlanCapabilities{AllowEmployeeRoth: true, AllowEmployerRoth: true}
	s := &models.WhatIfSettings{StartDate: "2026-09", ProjectionYears: 1, Persons: []models.Person{{ID: "owner", Name: "Owner", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}, RMDTiming: models.RMDTimingEndOfYear,
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", Name: "Cash", OwnerID: "owner", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 200000, CashPercent: 100},
			{ID: "broker", Name: "Brokerage", OwnerID: "owner", LegalType: "brokerage", TaxTreatment: "taxable", OpeningValue: 10000, Basis: 7000, StockPercent: 100},
			{ID: "ira", Name: "IRA", OwnerID: "owner", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 106000, PriorDecemberValue: &prior, BondPercent: 100},
			{ID: "work", Name: "Work plan", OwnerID: "owner", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "group", Capabilities: caps, CurrentEmployerRMDDeferral: true, OpeningValue: 5000, Basis: 0, StockPercent: 100},
		}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "owner", EmployerID: "employer", GrossSalary: 12000, EligibleCompensation: 12000, StartMonth: "2026-01"}}, ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: models.ContributionRate{Mode: "fixed", FixedMonthly: 100}, TraditionalAccountID: "work", Employer: &models.EmployerContribution{Mode: "fixed", FixedMonthly: 50, DestinationAccountID: "work", MatchTiming: "monthly"}}}, YTD: models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 200000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "ira"}}}}
	prepared := prepare.MustFrom(t, s)
	returns := func(*models.WhatIfSettings, int) engine.MonthReturns {
		return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1, AssetClassMonthly: &engine.AssetClassMonthReturns{}}
	}
	base := engine.Input{Prepared: prepared, MonthReturnsOverride: returns}
	if got := engine.New().Run(base); got.LifetimeDiagnostics != nil {
		t.Fatalf("deterministic default diagnostics=%+v", got.LifetimeDiagnostics)
	}
	_, offMC := MonteCarloWithResults(engine.New(), base, 1, 17)
	if offMC.Runs[0].LifetimeDiagnostics != nil {
		t.Fatalf("MC default diagnostics=%+v", offMC.Runs[0].LifetimeDiagnostics)
	}
	offHistorical := HistoricalBacktest(base, history.DefaultData())
	if len(offHistorical.Results) == 0 || offHistorical.Results[0].LifetimeDiagnostics != nil {
		t.Fatalf("historical default diagnostics=%+v", offHistorical)
	}

	base.CaptureLifetimeDiagnostics = true
	deterministic := engine.New().Run(base)
	_, mc := MonteCarloWithResults(engine.New(), base, 1, 17)
	historical := HistoricalBacktest(base, history.DefaultData())
	if deterministic.CalculationError != "" || mc.Runs[0].CalculationError != "" || historical.CalculationError != "" || len(historical.Results) == 0 {
		t.Fatalf("loop errors deterministic=%q mc=%q historical=%q", deterministic.CalculationError, mc.Runs[0].CalculationError, historical.CalculationError)
	}
	want := deterministic.LifetimeDiagnostics
	if want == nil || len(want.Accounts) != 4 || len(want.RMDObligationByAccountYear) == 0 {
		t.Fatalf("unbounded/incomplete deterministic diagnostics=%+v", want)
	}
	if !reflect.DeepEqual(mc.Runs[0].LifetimeDiagnostics, want) {
		t.Fatalf("MC parity\n got=%+v\nwant=%+v", mc.Runs[0].LifetimeDiagnostics, want)
	}
	for _, result := range historical.Results {
		if !reflect.DeepEqual(result.LifetimeDiagnostics, want) {
			t.Fatalf("historical parity start=%d\n got=%+v\nwant=%+v", result.StartYear, result.LifetimeDiagnostics, want)
		}
	}
}
