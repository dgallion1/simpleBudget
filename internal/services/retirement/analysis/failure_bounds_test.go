package analysis

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func TestFailurePointBoundMetadataJSONContract(t *testing.T) {
	b, err := json.Marshal(models.FailurePoint{})
	if err != nil {
		t.Fatalf("Marshal FailurePoint: %v", err)
	}
	got := string(b)
	for _, field := range []string{`"search_min":0`, `"search_max":0`, `"threshold_found":false`} {
		if !strings.Contains(got, field) {
			t.Errorf("FailurePoint JSON missing %s: %s", field, got)
		}
	}
}

func TestFailureThresholdSearchBounds(t *testing.T) {
	t.Run("return survives at searched floor", func(t *testing.T) {
		s := outerBoundSettings()
		eng, in := failureInput(t, s)
		assertSearchedBound(t, findReturnThreshold(eng, in), -5, 7, -5)
	})

	t.Run("inflation survives at searched ceiling", func(t *testing.T) {
		s := outerBoundSettings()
		eng, in := failureInput(t, s)
		assertSearchedBound(t, findInflationThreshold(eng, in), 3, 15, 15)
	})

	t.Run("expenses survive at searched ceiling", func(t *testing.T) {
		s := outerBoundSettings()
		eng, in := failureInput(t, s)
		assertSearchedBound(t, findExpensesThreshold(eng, in), 100, 300, 300)
	})

	t.Run("portfolio survives at searched floor", func(t *testing.T) {
		s := outerBoundSettings()
		s.PortfolioValue = 500_000
		s.MonthlyLivingExpenses = 1_000
		s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 5_000, StartMonth: 0, COLARate: 0.03}}
		eng, in := failureInput(t, s)
		assertSearchedBound(t, findPortfolioThreshold(eng, in), 0, 500_000, 0)
	})
}

func TestFailureThresholdSearchOmitsUnsearchedOutOfDefaultRateRange(t *testing.T) {
	t.Run("return below fixed floor", func(t *testing.T) {
		s := outerBoundSettings()
		s.InvestmentReturn = -10
		eng, in := failureInput(t, s)
		if !eng.Run(in).Survives {
			t.Fatal("fixture baseline must survive")
		}
		if fp := findReturnThreshold(eng, in); fp != nil {
			t.Fatalf("return below the fixed floor has no adverse searched interval; got %+v", fp)
		}
	})

	t.Run("inflation above fixed ceiling", func(t *testing.T) {
		s := outerBoundSettings()
		s.InflationRate = 20
		eng, in := failureInput(t, s)
		if !eng.Run(in).Survives {
			t.Fatal("fixture baseline must survive")
		}
		if fp := findInflationThreshold(eng, in); fp != nil {
			t.Fatalf("inflation above the fixed ceiling has no adverse searched interval; got %+v", fp)
		}
	})
}

func TestFailureThresholdSearchFindsBracketedTransitions(t *testing.T) {
	tests := []struct {
		name          string
		settings      *models.WhatIfSettings
		find          func(*engine.Engine, engine.Input) *models.FailurePoint
		wantMin       float64
		wantMax       float64
		wantThreshold float64
		setThreshold  func(*models.WhatIfSettings, float64)
	}{
		{
			name: "return", settings: failureBoundSettings(8, 400_000, 3_500, 3),
			find: findReturnThreshold, wantMin: -5, wantMax: 3, wantThreshold: -1.4,
			setThreshold: func(s *models.WhatIfSettings, v float64) { s.InvestmentReturn = v },
		},
		{
			name: "inflation", settings: failureBoundSettings(10, 400_000, 2_000, 3),
			find: findInflationThreshold, wantMin: 3, wantMax: 15, wantThreshold: 14.3,
			setThreshold: func(s *models.WhatIfSettings, v float64) { s.InflationRate = v },
		},
		{
			name: "expenses", settings: failureBoundSettings(8, 200_000, 2_000, 3),
			find: findExpensesThreshold, wantMin: 2_000, wantMax: 6_000, wantThreshold: 2_150,
			setThreshold: func(s *models.WhatIfSettings, v float64) { s.MonthlyLivingExpenses = v },
		},
		{
			name: "portfolio", settings: failureBoundSettings(8, 300_000, 2_500, 3),
			find: findPortfolioThreshold, wantMin: 0, wantMax: 300_000, wantThreshold: 236_000,
			setThreshold: func(s *models.WhatIfSettings, v float64) { s.PortfolioValue = v },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, in := failureInput(t, tc.settings)
			if !eng.Run(in).Survives {
				t.Fatal("fixture baseline must survive")
			}
			fp := tc.find(eng, in)
			if fp == nil {
				t.Fatal("expected a failure-point result")
			}
			if !fp.ThresholdFound {
				t.Fatal("expected bracketed transition to set ThresholdFound")
			}
			if fp.SearchMin != tc.wantMin || fp.SearchMax != tc.wantMax {
				t.Errorf("search range = [%v, %v], want [%v, %v]", fp.SearchMin, fp.SearchMax, tc.wantMin, tc.wantMax)
			}
			if math.Abs(fp.Threshold-tc.wantThreshold) > 1e-9 {
				t.Errorf("rounded threshold = %v, want fixed fixture value %v", fp.Threshold, tc.wantThreshold)
			}

			atRoundedThreshold := *tc.settings
			tc.setThreshold(&atRoundedThreshold, tc.wantThreshold)
			thresholdEngine, thresholdInput := failureInput(t, &atRoundedThreshold)
			if thresholdEngine.Run(thresholdInput).Survives {
				t.Errorf("fixed rounded value %v must exercise the failing side of this fixture", tc.wantThreshold)
			}
		})
	}
}

func TestFailureThresholdAllocationSentinelHasNoInventedRange(t *testing.T) {
	s := healthySettings()
	s.InvestmentReturn = 0
	eng, in := failureInput(t, s)
	if fp := findReturnThreshold(eng, in); fp != nil {
		t.Fatalf("allocation-mode sentinel must omit the threshold and range, got %+v", fp)
	}
}

func outerBoundSettings() *models.WhatIfSettings {
	s := healthySettings()
	s.PortfolioValue = 10_000_000
	s.MonthlyLivingExpenses = 100
	s.InvestmentReturn = 7
	s.InflationRate = 3
	s.ProjectionYears = 5
	return s
}

func failureBoundSettings(years int, portfolio, expenses, investmentReturn float64) *models.WhatIfSettings {
	s := healthySettings()
	s.ProjectionYears = years
	s.PortfolioValue = portfolio
	s.MonthlyLivingExpenses = expenses
	s.InvestmentReturn = investmentReturn
	return s
}

func assertSearchedBound(t *testing.T, fp *models.FailurePoint, wantMin, wantMax, wantThreshold float64) {
	t.Helper()
	if fp == nil {
		t.Fatal("expected a searched-bound result")
	}
	if fp.ThresholdFound {
		t.Fatal("survival at the outer endpoint must not be labeled a found transition")
	}
	if fp.SearchMin != wantMin || fp.SearchMax != wantMax {
		t.Errorf("search range = [%v, %v], want [%v, %v]", fp.SearchMin, fp.SearchMax, wantMin, wantMax)
	}
	if fp.Threshold != wantThreshold {
		t.Errorf("legacy threshold = %v, want surviving endpoint %v", fp.Threshold, wantThreshold)
	}
}
