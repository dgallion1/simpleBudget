package whatif

import (
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestFailureBoundsRender_ApproximateTransitionAndTestedRange(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out := renderFailurePoints(t, &models.FailurePointAnalysis{
		BaselineSurvives: true,
		FailurePoints: []models.FailurePoint{{
			ParamName: "inflation_rate", ParamLabel: "Inflation Rate",
			CurrentValue: 3, Threshold: 8, SearchMin: 3, SearchMax: 15,
			ThresholdFound: true, Direction: "above", Margin: 5, SafetyLevel: "safe",
		}},
	})
	flat := failureBoundsVisibleText(out)
	for _, want := range []string{
		"Approximate failure thresholds",
		"One assumption changes at a time; all other settings stay fixed.",
		"Approximate transition near 8.0%",
		"Tested range: 3.0% to 15.0%",
		"Larger modeled margin",
		"Modeled margin:",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("rendered failure point missing %q: %s", want, flat)
		}
	}
	for _, misleading := range []string{"Fails if above", "exact thresholds", "Safe", "Safety margin:"} {
		if strings.Contains(flat, misleading) {
			t.Errorf("rendered failure point must not claim %q: %s", misleading, flat)
		}
	}
}

func TestFailureBoundsRender_EndpointSurvivalIsNotATransition(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out := renderFailurePoints(t, &models.FailurePointAnalysis{
		BaselineSurvives: true,
		FailurePoints: []models.FailurePoint{{
			ParamName: "inflation_rate", ParamLabel: "Inflation Rate",
			CurrentValue: 3, Threshold: 15, SearchMin: 3, SearchMax: 15,
			ThresholdFound: false, Direction: "above", Margin: 12, SafetyLevel: "safe",
		}},
	})
	flat := failureBoundsVisibleText(out)
	for _, want := range []string{"No depletion found within the tested range.", "Tested range: 3.0% to 15.0%"} {
		if !strings.Contains(flat, want) {
			t.Errorf("endpoint-survival card missing %q: %s", want, flat)
		}
	}
	for _, misleading := range []string{"Approximate transition near 15.0%", "Fails if above: 15.0%"} {
		if strings.Contains(flat, misleading) {
			t.Errorf("endpoint survival must not render %q: %s", misleading, flat)
		}
	}
}

func TestFailureBoundsRender_FormatsMoneyRanges(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out := renderFailurePoints(t, &models.FailurePointAnalysis{
		BaselineSurvives: true,
		FailurePoints: []models.FailurePoint{
			{ParamName: "monthly_expenses", ParamLabel: "Monthly Expenses", CurrentValue: 2_000, Threshold: 2_150, SearchMin: 2_000, SearchMax: 6_000, ThresholdFound: true},
			{ParamName: "portfolio_value", ParamLabel: "Portfolio Value", CurrentValue: 300_000, Threshold: 236_000, SearchMin: 0, SearchMax: 300_000, ThresholdFound: true},
		},
	})
	flat := failureBoundsVisibleText(out)
	for _, want := range []string{
		"Approximate transition near $2,150.00/mo",
		"Tested range: $2,000.00/mo to $6,000.00/mo",
		"Approximate transition near $236,000.00",
		"Tested range: $0.00 to $300,000.00",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("rendered money bound missing %q: %s", want, flat)
		}
	}
}

func TestFailureBoundsRender_PreservesBaselineDepletedMessage(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out := renderFailurePoints(t, &models.FailurePointAnalysis{BaselineSurvives: false})
	want := "Portfolio already depletes under current settings. Adjust parameters to find survival thresholds."
	if !strings.Contains(failureBoundsVisibleText(out), want) {
		t.Errorf("baseline-depleted message changed: %s", out)
	}
}

func TestFailureBoundsRender_AllocationSentinelInventsNoRange(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out := renderFailurePoints(t, &models.FailurePointAnalysis{BaselineSurvives: true})
	flat := failureBoundsVisibleText(out)
	if !strings.Contains(flat, "No depletion found within the tested range.") {
		t.Errorf("empty threshold analysis missing bounded empty state: %s", flat)
	}
	if strings.Contains(flat, "Tested range:") || strings.Contains(flat, "Approximate transition near") {
		t.Errorf("empty threshold analysis must not invent an allocation range: %s", flat)
	}
}

func TestMonteCarloRender_DefinesSimulationAndNominalSampleMinimum(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out, err := renderer.RenderToString("whatif-monte-carlo", map[string]any{
		"Analysis": &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{
			Stats:        &models.MonteCarloStats{Runs: 1_000, SuccessRate: 82.5, WorstCase: 1_234},
			Distribution: &models.MonteCarloDistribution{},
		}},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"Success means avoiding modeled portfolio depletion through each run's horizon",
		"longevity varies from 5 years shorter to 5 years longer than the base projection",
		"configured guardrail rules remain in effect",
		"Lowest simulated ending balance",
		"Ending balances are nominal dollars at each run's simulated endpoint.",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("Monte Carlo card missing %q: %s", want, flat)
		}
	}
	if strings.Contains(flat, "Worst Case") {
		t.Errorf("sample minimum must not be labeled Worst Case: %s", flat)
	}
}

func TestMonteCarloRender_SurvivalDifferencesUsePercentagePoints(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out, err := renderer.RenderToString("whatif-monte-carlo", map[string]any{
		"Analysis": &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{
			Stats:        &models.MonteCarloStats{Runs: 1_000, SequenceRiskImpact: 6},
			Distribution: &models.MonteCarloDistribution{},
		}},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	flat := strings.Join(strings.Fields(out), " ")
	if !strings.Contains(flat, "Observed difference in avoiding modeled depletion between crash groups: 6 percentage points") {
		t.Errorf("survival-rate difference must use percentage points: %s", flat)
	}
}

func TestHistoricalBacktestRender_DifferenceUsesPercentagePoints(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	for _, tc := range []struct {
		name string
		diff float64
		want string
	}{
		{"higher", 7.5, "Historical success rate is 7.5 percentage points higher than Monte Carlo."},
		{"lower", -4.25, "Historical success rate is 4.2 percentage points lower than Monte Carlo."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderer.RenderToString("whatif-historical-backtest", map[string]any{
				"Analysis": &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 1000, SuccessRate: 72.5}}, HistoricalBacktest: &models.HistoricalBacktestAnalysis{
					DataStartYear: 1928, DataEndYear: 2025, TotalSequences: 50,
					SuccessRate: 80, MonteCarloSuccessRate: 72.5, HistoricalVsMC: tc.diff,
				}},
			})
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			flat := strings.Join(strings.Fields(out), " ")
			if !strings.Contains(flat, tc.want) {
				t.Errorf("historical comparison missing %q: %s", tc.want, flat)
			}
		})
	}
}

func renderFailurePoints(t *testing.T, failurePoints *models.FailurePointAnalysis) string {
	t.Helper()
	out, err := renderer.RenderToString("whatif-failure-points", map[string]any{
		"Analysis": &models.WhatIfAnalysis{FailurePoints: failurePoints},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

var failureBoundsTagRE = regexp.MustCompile(`(?s)<[^>]*>`)

func failureBoundsVisibleText(s string) string {
	return strings.Join(strings.Fields(failureBoundsTagRE.ReplaceAllString(s, " ")), " ")
}
