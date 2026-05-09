package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// TestStateTaxRateChangesProjection asserts that setting a non-zero
// StateIncomeTaxRate produces strictly higher total taxes than rate=0
// across an otherwise-identical scenario. This is the user-visible
// guarantee of the state-tax wiring: changing the input changes the
// output.
func TestStateTaxRateChangesProjection(t *testing.T) {
	build := func(rate float64) *models.WhatIfAnalysis {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 6_000
		s.StartDate = "2026-01"
		s.Persons = []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1960-01", Name: "You"},
		}
		s.TaxConfig = &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: rate,
		}
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, ClaimAge: 67}

		prepared := prepare.MustFrom(t, s)
		return RunFull(engine.New(), engine.Input{Prepared: prepared})
	}

	zero := build(0)
	five := build(5)

	if zero == nil || five == nil || zero.Projection == nil || five.Projection == nil {
		t.Fatal("RunFull returned nil projection")
	}

	zeroTax := totalTaxesPaid(zero.Projection)
	fiveTax := totalTaxesPaid(five.Projection)

	if !(fiveTax > zeroTax) {
		t.Errorf("expected 5%% state tax to produce more total tax than 0%%; got 0%%=%v 5%%=%v",
			zeroTax, fiveTax)
	}
}

// totalTaxesPaid sums TaxesPaid across all months in the projection.
// We intentionally do not depend on TaxAnalysis (which is not yet
// populated by production code) — this regression is about the
// engine-level effect, not the breakdown display.
func totalTaxesPaid(p *models.ProjectionResult) float64 {
	if p == nil {
		return 0
	}
	var sum float64
	for _, m := range p.Months {
		sum += m.TaxesPaid
	}
	return sum
}
