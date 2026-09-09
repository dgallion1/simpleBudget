package analysis

import (
	"budget2/internal/models"
	"fmt"
	"math"
	"sort"
)

// SummarizeGuardrailSimulationYears aggregates complete captured years, with
// each measure sorted independently. Ended paths do not contribute later zeros.
// Entirely absent capture is supported for legacy/custom simulation runners.
func SummarizeGuardrailSimulationYears(rows []models.MonteCarloResult) ([]models.GuardrailSimulationYear, error) {
	captured := 0
	for _, row := range rows {
		if row.FloorOutcome != nil && row.FloorOutcome.Years != nil {
			captured++
		}
	}
	if captured == 0 {
		return nil, nil
	}
	if captured != len(rows) {
		return nil, fmt.Errorf("mixed annual simulation capture")
	}
	groups := make(map[int][]models.GuardrailPathYear)
	maxYear := 0
	for i, row := range rows {
		f := row.FloorOutcome
		if f.MonthsObserved < 0 || row.ProjectionYears < 0 || f.MonthsObserved/12 != len(f.Years) || f.MonthsObserved > row.ProjectionYears*12 {
			return nil, fmt.Errorf("path %d: inconsistent annual horizon", i)
		}
		for j, y := range f.Years {
			if y.Year != j+1 {
				return nil, fmt.Errorf("path %d: annual years must be consecutive from one", i)
			}
			for _, v := range []float64{y.LivingReal, y.LivingNominal, y.PortfolioReal, y.PortfolioNominal} {
				if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
					return nil, fmt.Errorf("path %d year %d: invalid annual value", i, y.Year)
				}
			}
			groups[y.Year] = append(groups[y.Year], y)
			maxYear = max(maxYear, y.Year)
		}
	}
	result := make([]models.GuardrailSimulationYear, 0, maxYear)
	for year := 1; year <= maxYear; year++ {
		group := groups[year]
		values := [4][]float64{}
		for _, y := range group {
			values[0] = append(values[0], y.LivingReal)
			values[1] = append(values[1], y.LivingNominal)
			values[2] = append(values[2], y.PortfolioReal)
			values[3] = append(values[3], y.PortfolioNominal)
		}
		bands := [4]models.GuardrailPercentiles{}
		for i, v := range values {
			sort.Float64s(v)
			quantile := func(q float64) float64 {
				at := q * float64(len(v)-1)
				lo, hi := int(math.Floor(at)), int(math.Ceil(at))
				return v[lo] + (v[hi]-v[lo])*(at-float64(lo))
			}
			bands[i] = models.GuardrailPercentiles{P10: quantile(.1), P50: quantile(.5), P90: quantile(.9)}
		}
		result = append(result, models.GuardrailSimulationYear{Year: year, Paths: len(group), LivingReal: bands[0], LivingNominal: bands[1], PortfolioReal: bands[2], PortfolioNominal: bands[3]})
	}
	return result, nil
}
