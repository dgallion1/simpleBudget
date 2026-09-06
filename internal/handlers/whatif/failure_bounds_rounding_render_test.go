package whatif

import (
	"math"
	"strings"
	"testing"

	"budget2/internal/models"
	retirementanalysis "budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func TestFailureBoundsRender_ActualSearchRoundingIsApproximate(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	tests := []struct {
		name              string
		settings          *models.WhatIfSettings
		find              func(*engine.Engine, engine.Input) *models.FailurePoint
		set               func(*models.WhatIfSettings, float64)
		searchMin         float64
		searchMax         float64
		roundingQuantum   float64
		failureAtLow      bool
		wantThreshold     float64
		wantTransition    string
		wantRenderedRange string
	}{
		{
			name: "return", settings: roundingFailureSettings(8, 400_000, 3_500, 3),
			find:      retirementanalysis.FindReturnThreshold,
			set:       func(s *models.WhatIfSettings, v float64) { s.InvestmentReturn = v },
			searchMin: -5, searchMax: 3, roundingQuantum: 0.1, failureAtLow: true,
			wantThreshold: -1.4, wantTransition: "Approximate transition near -1.4%",
			wantRenderedRange: "Tested range: -5.0% to 3.0%",
		},
		{
			name: "inflation", settings: roundingFailureSettings(10, 400_000, 2_000, 3),
			find:      retirementanalysis.FindInflationThreshold,
			set:       func(s *models.WhatIfSettings, v float64) { s.InflationRate = v },
			searchMin: 3, searchMax: 15, roundingQuantum: 0.1,
			wantThreshold: 14.3, wantTransition: "Approximate transition near 14.3%",
			wantRenderedRange: "Tested range: 3.0% to 15.0%",
		},
		{
			name: "expenses", settings: roundingFailureSettings(8, 200_000, 2_000, 3),
			find:      retirementanalysis.FindExpensesThreshold,
			set:       func(s *models.WhatIfSettings, v float64) { s.MonthlyLivingExpenses = v },
			searchMin: 2_000, searchMax: 6_000, roundingQuantum: 50,
			wantThreshold: 2_150, wantTransition: "Approximate transition near $2,150.00/mo",
			wantRenderedRange: "Tested range: $2,000.00/mo to $6,000.00/mo",
		},
		{
			name: "portfolio", settings: roundingFailureSettings(8, 300_000, 2_500, 3),
			find:      retirementanalysis.FindPortfolioThreshold,
			set:       func(s *models.WhatIfSettings, v float64) { s.PortfolioValue = v },
			searchMin: 0, searchMax: 300_000, roundingQuantum: 1_000, failureAtLow: true,
			wantThreshold: 236_000, wantTransition: "Approximate transition near $236,000.00",
			wantRenderedRange: "Tested range: $0.00 to $300,000.00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng := engine.New()
			in := engine.Input{Prepared: prepare.MustFrom(t, tc.settings)}
			fp := tc.find(eng, in)
			if fp == nil || !fp.ThresholdFound {
				t.Fatalf("expected actual bracketed search result, got %+v", fp)
			}
			if math.Abs(fp.Threshold-tc.wantThreshold) > 1e-9 {
				t.Fatalf("production rounded threshold = %v, want fixed fixture value %v", fp.Threshold, tc.wantThreshold)
			}

			fineLow, fineHigh := independentlyBisectFailureTransition(
				t, tc.settings, tc.set, tc.searchMin, tc.searchMax, tc.roundingQuantum/10_000,
			)
			transition := (fineLow + fineHigh) / 2
			if tc.failureAtLow {
				if !(tc.wantThreshold < transition && transition < tc.wantThreshold+tc.roundingQuantum/2) {
					t.Errorf("fine transition %.8f must lie between failing rounded value %.8f and its upper rounding boundary %.8f", transition, tc.wantThreshold, tc.wantThreshold+tc.roundingQuantum/2)
				}
			} else if !(tc.wantThreshold-tc.roundingQuantum/2 < transition && transition < tc.wantThreshold) {
				t.Errorf("fine transition %.8f must lie between lower rounding boundary %.8f and failing rounded value %.8f", transition, tc.wantThreshold-tc.roundingQuantum/2, tc.wantThreshold)
			}
			if independentlySurvives(t, tc.settings, tc.set, tc.wantThreshold) {
				t.Errorf("rounded display value %v must be on the failing side of the independently located transition", tc.wantThreshold)
			}

			out, err := renderer.RenderToString("whatif-failure-points", map[string]any{
				"Analysis": &models.WhatIfAnalysis{FailurePoints: &models.FailurePointAnalysis{
					BaselineSurvives: true,
					FailurePoints:    []models.FailurePoint{*fp},
				}},
			})
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			flat := failureBoundsVisibleText(out)
			for _, want := range []string{tc.wantTransition, tc.wantRenderedRange} {
				if !strings.Contains(flat, want) {
					t.Errorf("actual search result render missing %q: %s", want, flat)
				}
			}
			if strings.Contains(flat, "Fails if") {
				t.Errorf("rounded failing-side value must remain approximate in the view: %s", flat)
			}
		})
	}
}

func independentlyBisectFailureTransition(
	t *testing.T,
	settings *models.WhatIfSettings,
	set func(*models.WhatIfSettings, float64),
	low, high, tolerance float64,
) (float64, float64) {
	t.Helper()
	lowSurvives := independentlySurvives(t, settings, set, low)
	highSurvives := independentlySurvives(t, settings, set, high)
	if lowSurvives == highSurvives {
		t.Fatalf("independent bisection needs opposite endpoint outcomes: low=%v high=%v", lowSurvives, highSurvives)
	}

	for high-low > tolerance {
		mid := (low + high) / 2
		if independentlySurvives(t, settings, set, mid) == lowSurvives {
			low = mid
		} else {
			high = mid
		}
	}
	if independentlySurvives(t, settings, set, low) != lowSurvives || independentlySurvives(t, settings, set, high) != highSurvives {
		t.Fatal("fine bisection lost its independently observed surviving/failing bracket")
	}
	return low, high
}

func independentlySurvives(
	t *testing.T,
	settings *models.WhatIfSettings,
	set func(*models.WhatIfSettings, float64),
	value float64,
) bool {
	t.Helper()
	candidate := *settings
	set(&candidate, value)
	return engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, &candidate)}).Survives
}

func roundingFailureSettings(years int, portfolio, expenses, investmentReturn float64) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = portfolio
	s.MonthlyLivingExpenses = expenses
	s.MonthlyHealthcare = 0
	s.InvestmentReturn = investmentReturn
	s.InflationRate = 3
	s.ProjectionYears = years
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil
	return s
}
