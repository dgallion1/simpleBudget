package models

import "testing"

func TestPerAccountAllocationWithZeroRoth(t *testing.T) {
	s := &WhatIfSettings{
		TaxDeferredPercent: 20,
		RothPercent:        0,
		// Taxable = 80%

		TaxDeferredStockPercent: 80,
		TaxDeferredCashPercent:  0,
		RothStockPercent:        0, // 100% bonds
		RothCashPercent:         0,
		TaxableStockPercent:     80,
		TaxableCashPercent:      0,
	}

	// Verify per-account mode is detected
	if !s.PerAccountAllocationIsSet() {
		t.Error("PerAccountAllocationIsSet should be true when TaxDeferredStockPercent=80")
	}

	// Verify Tax-Deferred allocation
	tdStock, tdBond, tdCash := s.GetTaxDeferredAllocation()
	if tdStock != 80 || tdBond != 20 || tdCash != 0 {
		t.Errorf("Tax-Deferred: expected (80,20,0), got (%.0f,%.0f,%.0f)", tdStock, tdBond, tdCash)
	}

	// Verify Roth allocation - should be 0/100/0 (100% bonds), not fallback to 60/40
	rothStock, rothBond, rothCash := s.GetRothAllocation()
	if rothStock != 0 || rothBond != 100 || rothCash != 0 {
		t.Errorf("Roth: expected (0,100,0), got (%.0f,%.0f,%.0f)", rothStock, rothBond, rothCash)
	}

	// Verify Taxable allocation
	taxStock, taxBond, taxCash := s.GetTaxableAllocation()
	if taxStock != 80 || taxBond != 20 || taxCash != 0 {
		t.Errorf("Taxable: expected (80,20,0), got (%.0f,%.0f,%.0f)", taxStock, taxBond, taxCash)
	}
}

func TestAllocationAffectsExpectedReturn(t *testing.T) {
	// All 80/20 allocation
	s1 := &WhatIfSettings{
		TaxDeferredPercent:      80,
		RothPercent:             10,
		TaxDeferredStockPercent: 80,
		RothStockPercent:        80,
		TaxableStockPercent:     80,
	}

	// Roth at 100% bonds (0% stocks)
	s2 := &WhatIfSettings{
		TaxDeferredPercent:      80,
		RothPercent:             10,
		TaxDeferredStockPercent: 80,
		RothStockPercent:        0, // 100% bonds
		TaxableStockPercent:     80,
	}

	r1 := s1.GetExpectedReturnFromAllocation()
	r2 := s2.GetExpectedReturnFromAllocation()

	// s2 should have LOWER return because Roth is in bonds
	if r2 >= r1 {
		t.Errorf("Expected s2 (Roth=100%% bonds) to have lower return than s1 (Roth=80%% stocks)")
		t.Errorf("s1 return: %.2f%%, s2 return: %.2f%%", r1, r2)
	}

	// With 10% in Roth: diff should be about 10% * (7% - 4%) * 0.8 = ~0.24%
	diff := r1 - r2
	if diff < 0.2 || diff > 0.3 {
		t.Errorf("Expected ~0.24%% difference, got %.2f%%", diff)
	}
	t.Logf("s1 return: %.2f%%, s2 return: %.2f%%, diff: %.2f%%", r1, r2, diff)
}

func TestGlidePathStockPct(t *testing.T) {
	s := &WhatIfSettings{
		GlidePath: &GlidePathConfig{
			Enabled:         true,
			StartStockPct:   80,
			EndStockPct:     30,
			TransitionYears: 20,
		},
	}

	t.Run("year 0 returns start", func(t *testing.T) {
		if got := s.GlidePathStockPct(0); got != 80 {
			t.Errorf("year 0 = %.1f, want 80", got)
		}
	})

	t.Run("mid-transition", func(t *testing.T) {
		got := s.GlidePathStockPct(10)
		want := 55.0 // 80 + 0.5*(30-80)
		if got != want {
			t.Errorf("year 10 = %.1f, want %.1f", got, want)
		}
	})

	t.Run("at transition end", func(t *testing.T) {
		if got := s.GlidePathStockPct(20); got != 30 {
			t.Errorf("year 20 = %.1f, want 30", got)
		}
	})

	t.Run("past transition", func(t *testing.T) {
		if got := s.GlidePathStockPct(30); got != 30 {
			t.Errorf("year 30 = %.1f, want 30", got)
		}
	})

	t.Run("disabled returns -1", func(t *testing.T) {
		s2 := &WhatIfSettings{}
		if got := s2.GlidePathStockPct(5); got != -1 {
			t.Errorf("disabled = %.1f, want -1", got)
		}
	})
}

func TestGetAllocationAtYear_WithGlidePath(t *testing.T) {
	s := &WhatIfSettings{
		TaxDeferredStockPercent: 80,
		TaxDeferredCashPercent:  5,
		RothStockPercent:        60,
		RothCashPercent:         10,
		TaxableStockPercent:     70,
		TaxableCashPercent:      0,
		GlidePath: &GlidePathConfig{
			Enabled:         true,
			StartStockPct:   80,
			EndStockPct:     40,
			TransitionYears: 20,
		},
	}

	t.Run("year 0 uses start stock pct", func(t *testing.T) {
		tdS, _, _, rothS, _, _, taxS, _, _ := s.GetAllocationAtYear(0)
		if tdS != 80 || rothS != 80 || taxS != 80 {
			t.Errorf("year 0: tdS=%.0f, rothS=%.0f, taxS=%.0f, all should be 80", tdS, rothS, taxS)
		}
	})

	t.Run("year 20 uses end stock pct", func(t *testing.T) {
		tdS, tdB, tdC, _, _, _, _, _, _ := s.GetAllocationAtYear(20)
		if tdS != 40 {
			t.Errorf("tdS = %.0f, want 40", tdS)
		}
		// Cash should be preserved from original
		if tdC != 5 {
			t.Errorf("tdC = %.0f, want 5 (preserved)", tdC)
		}
		if tdB != 55 {
			t.Errorf("tdB = %.0f, want 55", tdB)
		}
	})

	t.Run("no glide path returns static allocation", func(t *testing.T) {
		s2 := &WhatIfSettings{
			TaxDeferredStockPercent: 80,
			TaxDeferredCashPercent:  5,
			RothStockPercent:        60,
			RothCashPercent:         10,
			TaxableStockPercent:     70,
			TaxableCashPercent:      0,
		}
		tdS, _, _, rothS, _, _, taxS, _, _ := s2.GetAllocationAtYear(10)
		if tdS != 80 || rothS != 60 || taxS != 70 {
			t.Errorf("static: tdS=%.0f, rothS=%.0f, taxS=%.0f", tdS, rothS, taxS)
		}
	})
}
