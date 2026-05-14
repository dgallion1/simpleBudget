package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestWithdrawalMixShares_SumIsOne(t *testing.T) {
	cases := []struct {
		name              string
		taxDeferred, roth float64
		wantTD, wantTX, wantR float64
	}{
		{"balanced 60/10/30", 60, 10, 0.60, 0.30, 0.10},
		{"all tax-deferred", 100, 0, 1.00, 0.00, 0.00},
		{"all roth", 0, 100, 0.00, 0.00, 1.00},
		{"all taxable", 0, 0, 0.00, 1.00, 0.00},
		{"overflow 80/40 → renormalized 80/40 of 120", 80, 40, 80.0 / 120, 0, 40.0 / 120},
		{"overflow 110/10 → renormalized 110/10 of 120", 110, 10, 110.0 / 120, 0, 10.0 / 120},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.WhatIfSettings{
				TaxDeferredPercent: tc.taxDeferred,
				RothPercent:        tc.roth,
			}
			pTD, pTX, pR := withdrawalMixShares(s)
			if math.Abs(pTD-tc.wantTD) > 1e-9 || math.Abs(pTX-tc.wantTX) > 1e-9 || math.Abs(pR-tc.wantR) > 1e-9 {
				t.Fatalf("shares: got TD=%v TX=%v R=%v, want TD=%v TX=%v R=%v",
					pTD, pTX, pR, tc.wantTD, tc.wantTX, tc.wantR)
			}
			if sum := pTD + pTX + pR; math.Abs(sum-1.0) > 1e-9 {
				t.Fatalf("sum=%v, want 1.0", sum)
			}
		})
	}
}
