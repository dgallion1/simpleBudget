package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func spendingRiskRow(avg float64, outcome models.SpendingPathOutcome) models.MonteCarloResult {
	outcome.MonthsObserved = 12
	outcome.NearTermMonths = 12
	outcome.NearTermFundedLivingReal = avg * 12
	if outcome.MinFundedMonth == 0 {
		outcome.MinFundedMonth = 1
		outcome.MinFundedMonthlyReal = avg
	}
	return models.MonteCarloResult{
		Survives: true, ProjectionYears: 1, SpendingOutcome: &outcome,
		FloorOutcome: &models.MonteCarloFloorOutcome{
			FloorFailed: outcome.FloorShortfallMonths > 0, MonthsObserved: 12,
			TotalFundedLivingReal: avg * 12,
		},
	}
}

func TestSpendingRiskAggregatesCompletePaths(t *testing.T) {
	rows := []models.MonteCarloResult{
		spendingRiskRow(100, models.SpendingPathOutcome{FirstCutMonth: 3, MonthsBelowPlan: 2, LongestBelowPlanMonths: 2, MaxCutReal: 20, MaxCutPct: 20}),
		spendingRiskRow(200, models.SpendingPathOutcome{FloorShortfallMonths: 1, LongestFloorShortfallMonths: 1, LargestFloorGapReal: 5}),
		spendingRiskRow(300, models.SpendingPathOutcome{FirstCutMonth: 9, MonthsBelowPlan: 4, LongestBelowPlanMonths: 3, MaxCutReal: 40, MaxCutPct: 10, BelowPlanAtEnd: true}),
		spendingRiskRow(400, models.SpendingPathOutcome{FloorShortfallMonths: 2, LongestFloorShortfallMonths: 2, LargestFloorGapReal: 10, UnpaidObligationMonths: 1}),
	}
	got, err := summarizeSpendingRisk(rows, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runs != 4 || got.MedianNearTermMonthlyReal != 250 || got.P10NearTermMonthlyReal != 130 ||
		got.CutPaths != 2 || got.EarlyCutPaths != 2 || got.FloorShortfallPaths != 2 ||
		got.UnpaidObligationPaths != 1 || got.CutPathsStillBelowPlan != 1 ||
		got.LongestFloorShortfallMonths != 2 || got.LargestFloorGapReal != 10 {
		t.Fatalf("aggregate mismatch: %+v", got)
	}
	if got.MedianFirstCutMonth == nil || *got.MedianFirstCutMonth != 6 ||
		got.MedianMaxCutReal == nil || *got.MedianMaxCutReal != 30 ||
		got.P95MaxCutReal == nil || *got.P95MaxCutReal != 39 ||
		got.MedianMaxCutPct == nil || *got.MedianMaxCutPct != 15 ||
		got.P95MaxCutPct == nil || *got.P95MaxCutPct != 19.5 ||
		got.MedianMonthsBelowPlan == nil || *got.MedianMonthsBelowPlan != 3 ||
		got.P95LongestBelowPlanMonths == nil || *got.P95LongestBelowPlanMonths != 2.95 {
		t.Fatalf("conditional distributions mismatch: %+v", got)
	}
	if got.FloorEvidence.FloorShortfallPaths != got.FloorShortfallPaths {
		t.Fatalf("floor evidence mismatch: %+v", got)
	}
}

func TestSpendingRiskZeroCutMetricsAreNil(t *testing.T) {
	rows := []models.MonteCarloResult{
		spendingRiskRow(100, models.SpendingPathOutcome{}),
		spendingRiskRow(200, models.SpendingPathOutcome{}),
	}
	got, err := summarizeSpendingRisk(rows, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.MedianFirstCutMonth != nil || got.MedianMaxCutReal != nil || got.P95MaxCutReal != nil ||
		got.MedianMaxCutPct != nil || got.P95MaxCutPct != nil || got.MedianMonthsBelowPlan != nil ||
		got.P95LongestBelowPlanMonths != nil {
		t.Fatalf("zero-cut conditional metrics must be nil: %+v", got)
	}
}

func TestSpendingRiskDepletionFloorCoverageIncludesEventMonth(t *testing.T) {
	full := func(failAtEvent bool) models.MonteCarloResult {
		outcome := models.SpendingPathOutcome{MonthsObserved: 120, NearTermMonths: 12,
			NearTermFundedLivingReal: 1200, MinFundedMonthlyReal: 100, MinFundedMonth: 1,
			DepletionMonth: 100}
		if failAtEvent {
			outcome.MinFundedMonthlyReal = 80
			outcome.MinFundedMonth = 100
			outcome.FloorShortfallMonths = 1
			outcome.LongestFloorShortfallMonths = 1
			outcome.LargestFloorGapReal = 10
			outcome.FloorShortfallMonthsAfterDepletion = 1
		}
		return models.MonteCarloResult{ProjectionYears: 10, Survives: false, SpendingOutcome: &outcome,
			FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: 120, FloorFailed: failAtEvent, TotalFundedLivingReal: 12000}}
	}
	got, err := summarizeSpendingRisk([]models.MonteCarloResult{full(false)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.DepletionPaths != 1 || got.DepletionFloorFundedPaths != 1 || got.FloorShortfallPaths != 0 || got.UnpaidObligationPaths != 0 {
		t.Fatalf("funded depletion misclassified: %+v", got)
	}
	got, err = summarizeSpendingRisk([]models.MonteCarloResult{full(true)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.DepletionFloorFundedPaths != 0 {
		t.Fatalf("event-month failure was ignored: %+v", got)
	}
}

func TestSpendingRiskRejectsImpossibleDepletionFloorCounts(t *testing.T) {
	for _, tc := range []struct {
		name           string
		depletionMonth int
		floorFailures  int
		afterFailures  int
	}{
		{name: "early depletion omits required post-depletion failure", depletionMonth: 1, floorFailures: 1, afterFailures: 0},
		{name: "late depletion exceeds remaining post-depletion months", depletionMonth: 12, floorFailures: 2, afterFailures: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := spendingRiskRow(100, models.SpendingPathOutcome{
				MinFundedMonthlyReal: 80, MinFundedMonth: 1,
				FloorShortfallMonths: tc.floorFailures, LongestFloorShortfallMonths: tc.floorFailures,
				LargestFloorGapReal: 10, DepletionMonth: tc.depletionMonth,
				FloorShortfallMonthsAfterDepletion: tc.afterFailures,
			})
			row.Survives = false
			if got, err := summarizeSpendingRisk([]models.MonteCarloResult{row}, 1); err == nil {
				t.Fatalf("impossible depletion evidence qualified: %+v", got)
			}
		})
	}
}

func TestSpendingRiskRejectsIncompleteAndInvalidPaths(t *testing.T) {
	valid := spendingRiskRow(100, models.SpendingPathOutcome{})
	for name, mutate := range map[string]func(*models.MonteCarloResult){
		"short full capture":           func(r *models.MonteCarloResult) { r.SpendingOutcome.MonthsObserved = 6 },
		"short near capture":           func(r *models.MonteCarloResult) { r.SpendingOutcome.NearTermMonths = 6 },
		"missing floor capture":        func(r *models.MonteCarloResult) { r.FloorOutcome = nil },
		"unpaid without floor failure": func(r *models.MonteCarloResult) { r.SpendingOutcome.UnpaidObligationMonths = 1 },
		"nonfinite observation":        func(r *models.MonteCarloResult) { r.SpendingOutcome.NearTermFundedLivingReal = math.NaN() },
		"floor crosscheck":             func(r *models.MonteCarloResult) { r.FloorOutcome.FloorFailed = true },
		"floor failure missing run": func(r *models.MonteCarloResult) {
			r.SpendingOutcome.FloorShortfallMonths = 1
			r.SpendingOutcome.LargestFloorGapReal = 1
			r.FloorOutcome.FloorFailed = true
		},
		"cut missing magnitude": func(r *models.MonteCarloResult) {
			r.SpendingOutcome.MonthsBelowPlan = 1
			r.SpendingOutcome.LongestBelowPlanMonths = 1
			r.SpendingOutcome.FirstCutMonth = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := valid
			spending := *valid.SpendingOutcome
			floor := *valid.FloorOutcome
			row.SpendingOutcome, row.FloorOutcome = &spending, &floor
			mutate(&row)
			if got, err := summarizeSpendingRisk([]models.MonteCarloResult{row}, 1); err == nil {
				t.Fatalf("invalid path qualified: %+v", got)
			}
		})
	}
}

func TestSpendingRiskWorstPathRanking(t *testing.T) {
	rows := []models.MonteCarloResult{
		spendingRiskRow(200, models.SpendingPathOutcome{MinFundedMonthlyReal: 80, MinFundedMonth: 6, FloorShortfallMonths: 2, LongestFloorShortfallMonths: 1, LargestFloorGapReal: 10}),
		spendingRiskRow(200, models.SpendingPathOutcome{MinFundedMonthlyReal: 80, MinFundedMonth: 4, FloorShortfallMonths: 3, LongestFloorShortfallMonths: 2, LargestFloorGapReal: 10}),
		spendingRiskRow(200, models.SpendingPathOutcome{MinFundedMonthlyReal: 90, MinFundedMonth: 2, FloorShortfallMonths: 4, LongestFloorShortfallMonths: 3, LargestFloorGapReal: 1}),
	}
	for i := range rows {
		rows[i].FloorOutcome.FloorFailed = true
	}
	got, err := summarizeSpendingRisk(rows, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorstPathIndex != 1 || got.LowestObservedMonthlyReal != 80 || got.LowestObservedMonth != 4 {
		t.Fatalf("worst path ranking mismatch: %+v", got)
	}
}
