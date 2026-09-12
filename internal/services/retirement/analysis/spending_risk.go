package analysis

import (
	"fmt"
	"math"
	"sort"

	"budget2/internal/models"
)

func summarizeSpendingRisk(rows []models.MonteCarloResult, nearYears int) (*models.SpendingRiskMetrics, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no simulated paths")
	}
	if nearYears <= 0 {
		return nil, fmt.Errorf("positive near-term observation years required")
	}

	floorEvidence, err := guardrailOptimizerMetrics(rows)
	if err != nil {
		return nil, err
	}
	metrics := &models.SpendingRiskMetrics{Runs: len(rows), FloorEvidence: floorEvidence}
	nearTermMonthly := make([]float64, 0, len(rows))
	firstCutMonths := make([]float64, 0, len(rows))
	maxCutReal := make([]float64, 0, len(rows))
	maxCutPct := make([]float64, 0, len(rows))
	monthsBelowPlan := make([]float64, 0, len(rows))
	longestBelowPlan := make([]float64, 0, len(rows))
	worst := -1

	for i := range rows {
		row := &rows[i]
		out := row.SpendingOutcome
		if err := validateSpendingRiskPath(i, row, nearYears); err != nil {
			return nil, err
		}

		nearTermMonthly = append(nearTermMonthly, out.NearTermFundedLivingReal/float64(out.NearTermMonths))
		if out.MonthsBelowPlan > 0 {
			metrics.CutPaths++
			if out.FirstCutMonth <= nearYears*12 {
				metrics.EarlyCutPaths++
			}
			if out.BelowPlanAtEnd {
				metrics.CutPathsStillBelowPlan++
			}
			firstCutMonths = append(firstCutMonths, float64(out.FirstCutMonth))
			maxCutReal = append(maxCutReal, out.MaxCutReal)
			maxCutPct = append(maxCutPct, out.MaxCutPct)
			monthsBelowPlan = append(monthsBelowPlan, float64(out.MonthsBelowPlan))
			longestBelowPlan = append(longestBelowPlan, float64(out.LongestBelowPlanMonths))
		}
		if out.FloorShortfallMonths > 0 {
			metrics.FloorShortfallPaths++
		}
		if out.UnpaidObligationMonths > 0 {
			metrics.UnpaidObligationPaths++
		}
		if out.DepletionMonth > 0 {
			metrics.DepletionPaths++
			if out.FloorShortfallMonthsAfterDepletion == 0 {
				metrics.DepletionFloorFundedPaths++
			}
		}
		metrics.LargestFloorGapReal = math.Max(metrics.LargestFloorGapReal, out.LargestFloorGapReal)
		metrics.LongestFloorShortfallMonths = max(metrics.LongestFloorShortfallMonths, out.LongestFloorShortfallMonths)
		if worst < 0 || spendingRiskPathWorse(out, rows[worst].SpendingOutcome) {
			worst = i
		}
	}

	if metrics.FloorShortfallPaths != floorEvidence.FloorShortfallPaths {
		return nil, fmt.Errorf("floor failure cross-check mismatch: spending paths=%d floor evidence=%d", metrics.FloorShortfallPaths, floorEvidence.FloorShortfallPaths)
	}
	metrics.MedianNearTermMonthlyReal = spendingRiskPercentile(nearTermMonthly, .5)
	metrics.P10NearTermMonthlyReal = spendingRiskPercentile(nearTermMonthly, .1)
	metrics.WorstPathIndex = worst
	metrics.LowestObservedMonthlyReal = rows[worst].SpendingOutcome.MinFundedMonthlyReal
	metrics.LowestObservedMonth = rows[worst].SpendingOutcome.MinFundedMonth
	if metrics.CutPaths > 0 {
		metrics.MedianFirstCutMonth = spendingRiskFloat(spendingRiskPercentile(firstCutMonths, .5))
		metrics.MedianMaxCutReal = spendingRiskFloat(spendingRiskPercentile(maxCutReal, .5))
		metrics.P95MaxCutReal = spendingRiskFloat(spendingRiskPercentile(maxCutReal, .95))
		metrics.MedianMaxCutPct = spendingRiskFloat(spendingRiskPercentile(maxCutPct, .5))
		metrics.P95MaxCutPct = spendingRiskFloat(spendingRiskPercentile(maxCutPct, .95))
		metrics.MedianMonthsBelowPlan = spendingRiskFloat(spendingRiskPercentile(monthsBelowPlan, .5))
		metrics.P95LongestBelowPlanMonths = spendingRiskFloat(spendingRiskPercentile(longestBelowPlan, .95))
	}
	return metrics, nil
}

func validateSpendingRiskPath(index int, row *models.MonteCarloResult, nearYears int) error {
	if row.ProjectionYears <= 0 {
		return fmt.Errorf("path %d has invalid projection length", index)
	}
	expectedMonths := row.ProjectionYears * 12
	out := row.SpendingOutcome
	if out == nil {
		return fmt.Errorf("path %d has no spending observation", index)
	}
	if row.FloorOutcome == nil {
		return fmt.Errorf("path %d has no floor observation", index)
	}
	if out.MonthsObserved != expectedMonths || row.FloorOutcome.MonthsObserved != expectedMonths {
		return fmt.Errorf("path %d has incomplete full-horizon observation", index)
	}
	if out.NearTermMonths != nearYears*12 || out.NearTermMonths > out.MonthsObserved {
		return fmt.Errorf("path %d has incomplete near-term observation", index)
	}
	if !finiteSpendingValue(out.NearTermFundedLivingReal) || !finiteSpendingValue(out.MinFundedMonthlyReal) ||
		!finiteSpendingValue(out.LargestFloorGapReal) || !finiteSpendingValue(out.MaxCutReal) ||
		!finiteSpendingValue(out.MaxCutPct) || out.NearTermFundedLivingReal < 0 ||
		out.MinFundedMonthlyReal < 0 || out.LargestFloorGapReal < 0 || out.MaxCutReal < 0 || out.MaxCutPct < 0 {
		return fmt.Errorf("path %d has invalid spending values", index)
	}
	if out.MinFundedMonth < 1 || out.MinFundedMonth > out.MonthsObserved ||
		out.FloorShortfallMonths < 0 || out.FloorShortfallMonths > out.MonthsObserved ||
		out.LongestFloorShortfallMonths < 0 || out.LongestFloorShortfallMonths > out.FloorShortfallMonths ||
		out.UnpaidObligationMonths < 0 || out.UnpaidObligationMonths > out.FloorShortfallMonths ||
		out.FloorShortfallMonthsAfterDepletion < 0 || out.FloorShortfallMonthsAfterDepletion > out.FloorShortfallMonths ||
		out.MonthsBelowPlan < 0 || out.MonthsBelowPlan > out.MonthsObserved ||
		out.LongestBelowPlanMonths < 0 || out.LongestBelowPlanMonths > out.MonthsBelowPlan {
		return fmt.Errorf("path %d has inconsistent spending counts", index)
	}
	if (out.FloorShortfallMonths > 0) != row.FloorOutcome.FloorFailed {
		return fmt.Errorf("path %d disagrees with floor observation", index)
	}
	if out.FloorShortfallMonths == 0 {
		if out.LongestFloorShortfallMonths != 0 || out.LargestFloorGapReal != 0 || out.UnpaidObligationMonths != 0 || out.FloorShortfallMonthsAfterDepletion != 0 {
			return fmt.Errorf("path %d has minimum-failure details without a failure", index)
		}
	} else if out.LongestFloorShortfallMonths == 0 || out.LargestFloorGapReal == 0 {
		return fmt.Errorf("path %d has incomplete minimum-failure details", index)
	}
	if out.DepletionMonth < 0 || out.DepletionMonth > out.MonthsObserved ||
		(out.DepletionMonth == 0 && out.FloorShortfallMonthsAfterDepletion != 0) ||
		(out.DepletionMonth > 0) == row.Survives {
		return fmt.Errorf("path %d has inconsistent depletion observation", index)
	}
	if out.DepletionMonth > 0 {
		minimumAfterDepletion := max(0, out.FloorShortfallMonths-(out.DepletionMonth-1))
		maximumAfterDepletion := out.MonthsObserved - out.DepletionMonth + 1
		if out.FloorShortfallMonthsAfterDepletion < minimumAfterDepletion ||
			out.FloorShortfallMonthsAfterDepletion > maximumAfterDepletion {
			return fmt.Errorf("path %d has impossible post-depletion floor failures", index)
		}
	}
	if out.MonthsBelowPlan == 0 {
		if out.FirstCutMonth != 0 || out.LongestBelowPlanMonths != 0 || out.MaxCutReal != 0 || out.MaxCutPct != 0 || out.BelowPlanAtEnd {
			return fmt.Errorf("path %d has cut details without a cut", index)
		}
	} else if out.FirstCutMonth < 1 || out.FirstCutMonth > out.MonthsObserved || out.LongestBelowPlanMonths == 0 || out.MaxCutReal == 0 || out.MaxCutPct == 0 {
		return fmt.Errorf("path %d has incomplete cut details", index)
	}
	return nil
}

func spendingRiskPathWorse(a, b *models.SpendingPathOutcome) bool {
	if a.MinFundedMonthlyReal != b.MinFundedMonthlyReal {
		return a.MinFundedMonthlyReal < b.MinFundedMonthlyReal
	}
	return a.FloorShortfallMonths > b.FloorShortfallMonths
}

func spendingRiskPercentile(values []float64, q float64) float64 {
	sort.Float64s(values)
	position := float64(len(values)-1) * q
	low := int(position)
	high := min(low+1, len(values)-1)
	return values[low] + (values[high]-values[low])*(position-float64(low))
}

func spendingRiskFloat(value float64) *float64 { return &value }
