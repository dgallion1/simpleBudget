package analysis

import (
	"testing"

	"budget2/internal/models"
)

// brokerageScenario is a retired household living largely off a taxable
// brokerage account, so taxable withdrawals — and therefore realized gains —
// drive the tax bill.
func brokerageScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 68
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 20
	s.RothPercent = 0 // 80% taxable
	s.MonthlyLivingExpenses = 8000
	s.InvestmentReturn = 5.0
	s.InflationRate = 3.0
	s.ProjectionYears = 25
	return s
}

// TestTaxableCostBasis_ProjectionPaysMoreTax is the end-to-end check for
// FINANCEAPPCONCERNS.md §2. Seeding a real cost basis must move real money
// across a real projection — if it only changed a struct field, the fix would
// be cosmetic.
func TestTaxableCostBasis_ProjectionPaysMoreTax(t *testing.T) {
	unset := brokerageScenario()
	unset.TaxableCostBasis = nil

	// $1.2M taxable, bought for $700k: $500k of embedded gain.
	seeded := brokerageScenario()
	seeded.TaxableCostBasis = models.FloatPtr(700_000)

	projUnset, _ := runProj(t, unset)
	projSeeded, _ := runProj(t, seeded)

	sumTaxes := func(p *models.ProjectionResult) float64 {
		var total float64
		for _, ys := range p.YearlySummaries {
			total += ys.Taxes
		}
		return total
	}

	taxUnset := sumTaxes(projUnset)
	taxSeeded := sumTaxes(projSeeded)

	if taxUnset <= 0 || taxSeeded <= 0 {
		t.Fatalf("scenario produced no tax to compare (unset %.2f, seeded %.2f); "+
			"the fixture is not exercising taxable withdrawals", taxUnset, taxSeeded)
	}
	if taxSeeded <= taxUnset {
		t.Errorf("lifetime tax with a real cost basis = %.2f; want more than the "+
			"zero-embedded-gain assumption's %.2f.\n"+
			"Seeding $500,000 of unrealized gain must increase the tax on taxable "+
			"withdrawals — if it does not, the basis is not reaching the engine.",
			taxSeeded, taxUnset)
	}

	t.Logf("lifetime tax: no basis %.0f -> real basis %.0f (+%.0f, %.1f%%)",
		taxUnset, taxSeeded, taxSeeded-taxUnset,
		(taxSeeded-taxUnset)/taxUnset*100)
}

// TestTaxableCostBasis_UnsetPreservesLegacyProjection guards the compatibility
// promise: a scenario saved before this field existed must project exactly the
// numbers it always did.
func TestTaxableCostBasis_UnsetPreservesLegacyProjection(t *testing.T) {
	a := brokerageScenario()
	a.TaxableCostBasis = nil

	// Explicitly setting the basis to the starting taxable market value is the
	// same assumption the legacy default made, so the two must agree exactly.
	b := brokerageScenario()
	b.TaxableCostBasis = models.FloatPtr(1_500_000 * 0.80)

	projA, _ := runProj(t, a)
	projB, _ := runProj(t, b)

	if len(projA.YearlySummaries) != len(projB.YearlySummaries) {
		t.Fatalf("projection lengths differ: %d vs %d",
			len(projA.YearlySummaries), len(projB.YearlySummaries))
	}
	for i := range projA.YearlySummaries {
		if got, want := projB.YearlySummaries[i].Taxes, projA.YearlySummaries[i].Taxes; got != want {
			t.Fatalf("year %d taxes = %.6f, want %.6f; unset must equal "+
				"basis-at-market-value exactly", i, got, want)
		}
	}
}
