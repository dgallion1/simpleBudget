package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// At a far-out steady-state month a bucket the projection has drained must
// not be assigned a phantom gap-closing withdrawal. The suggested mix is
// weighted by the projection's ACTUAL bucket balances, not the static
// portfolio allocation.
func TestSteadyStateWithdrawalMixShares_SkipsDepletedBuckets(t *testing.T) {
	// Allocation says 60% tax-deferred / 20% Roth / 20% taxable, but the
	// projection has drained the tax-deferred bucket by the target month.
	s := &models.WhatIfSettings{TaxDeferredPercent: 60, RothPercent: 20}
	proj := &models.ProjectionResult{Months: []models.ProjectionMonth{
		{TaxDeferredBalance: 0, TaxableBalance: 300_000, RothBalance: 100_000},
	}}

	pTD, pTX, pR := steadyStateWithdrawalMixShares(proj, 0, s)

	if pTD != 0 {
		t.Errorf("expected 0 tax-deferred share when the bucket is depleted, got %.4f", pTD)
	}
	if math.Abs(pTX-0.75) > 1e-9 || math.Abs(pR-0.25) > 1e-9 {
		t.Errorf("expected balance-weighted shares taxable=0.75 roth=0.25, got taxable=%.4f roth=%.4f", pTX, pR)
	}
	if sum := pTD + pTX + pR; math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("shares must sum to 1, got %.6f", sum)
	}
}

// With no projection (or all buckets empty) it falls back to the static
// allocation split so the contract (shares sum to 1) still holds.
func TestSteadyStateWithdrawalMixShares_FallsBackToAllocation(t *testing.T) {
	s := &models.WhatIfSettings{TaxDeferredPercent: 50, RothPercent: 10}
	pTD, pTX, pR := steadyStateWithdrawalMixShares(nil, 120, s)

	wantTD, wantTX, wantR := withdrawalMixShares(s)
	if math.Abs(pTD-wantTD) > 1e-9 || math.Abs(pTX-wantTX) > 1e-9 || math.Abs(pR-wantR) > 1e-9 {
		t.Errorf("nil projection should fall back to allocation shares (%.4f/%.4f/%.4f), got %.4f/%.4f/%.4f",
			wantTD, wantTX, wantR, pTD, pTX, pR)
	}
}
