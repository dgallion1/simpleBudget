package analysis

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"budget2/internal/services/retirement/prepare"
)

func lifetimeErrorInput(t *testing.T) engine.Input {
	t.Helper()
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, Persons: []models.Person{{ID: "p", Name: "P", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}},
		TaxConfig:     &models.TaxConfig{FilingStatus: models.FilingSingle},
		IncomeSources: []models.IncomeSource{{ID: "pension", Amount: 10000}}, RMDTiming: models.RMDTimingStartOfYear,
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", CashPercent: 100},
			{ID: "broker", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
			{ID: "ira", OwnerID: "p", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 100000, PriorDecemberValue: func() *float64 { v := 100000.0; return &v }(), StockPercent: 100},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "ira"}}}}
	return engine.Input{Prepared: prepare.MustFrom(t, s), TaxSettlementMaxIterations: 1}
}
func TestLifetimeMonteCarloCalculationErrorInvalidatesEnsemble(t *testing.T) {
	got, _ := MonteCarloWithResults(engine.New(), lifetimeErrorInput(t), 2, 1)
	if got.CalculationError == "" || got.Stats != nil || got.Distribution != nil {
		t.Fatalf("analysis=%+v", got)
	}
}
func TestLifetimeHistoricalCalculationErrorInvalidatesEnsemble(t *testing.T) {
	got := HistoricalBacktest(lifetimeErrorInput(t), history.DefaultData())
	if got.CalculationError == "" || got.TotalSequences != 0 || got.SuccessRate != 0 {
		t.Fatalf("analysis=%+v", got)
	}
}
