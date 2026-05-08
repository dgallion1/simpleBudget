//go:build !short

package retirement

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

const parityMonteCarloRuns = 1000

// runFullForParity assembles a *models.WhatIfAnalysis using the engine
// for projection and the analysis package for the post-projection
// summaries that have been extracted (RMD, BudgetFit, PresentValue,
// sustainability, explainability, Sensitivity, FailurePoints, Monte
// Carlo, SS, Backtest). Calculator still produces RunFullAnalysis as
// the orchestrator for Task 6.
func runFullForParity(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	tmp := NewCalculatorWithChain(in.Prepared, in.Chain)
	tmp.SetMonteCarloSeedForParity(mcSeed)
	out := tmp.RunFullAnalysis()
	proj := eng.Run(in)
	out.Projection = proj
	out.RMD = analysis.BuildRMD(proj, in)
	out.BudgetFit = analysis.BudgetFit(in)
	out.PresentValue = analysis.PresentValue(in)
	out.Sustainability = analysis.Score(proj, out.BudgetFit)
	out.ProjectionExplainability = analysis.BuildExplainability(proj, in)
	out.Sensitivity = analysis.Sensitivity(eng, in)
	out.FailurePoints = analysis.FailurePoints(eng, in)
	out.MonteCarlo = analysis.MonteCarlo(eng, in, parityMonteCarloRuns, mcSeed)

	out.SocialSecurity = analysis.SSAnalysis(eng, in)
	if out.SocialSecurity != nil && SSPortfolioEligible(in.Prepared.Settings()) {
		out.SocialSecurity.Portfolio = analysis.SSPortfolioWithSeed(eng, in, out.SocialSecurity, mcSeed)
	}
	out.HistoricalBacktest = analysis.HistoricalBacktest(eng, in, history.DefaultData())

	if out.HistoricalBacktest != nil && out.MonteCarlo != nil && out.MonteCarlo.Stats != nil {
		out.HistoricalBacktest.MonteCarloSuccessRate = out.MonteCarlo.Stats.SuccessRate
		out.HistoricalBacktest.HistoricalVsMC = out.HistoricalBacktest.SuccessRate - out.MonteCarlo.Stats.SuccessRate
	}
	return out
}

const floatTol = 1e-9
const floatRelTol = 1e-12

// compareWhatIfAnalysis returns "" on tolerant deep-equality, else a diff
// describing the first mismatch.
func compareWhatIfAnalysis(a, b *models.WhatIfAnalysis) string {
	if a == nil || b == nil {
		if a == b {
			return ""
		}
		return fmt.Sprintf("nil mismatch: a=%v b=%v", a == nil, b == nil)
	}
	return diffValues("WhatIfAnalysis", reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
}

func diffValues(path string, a, b reflect.Value) string {
	if a.Type() != b.Type() {
		return fmt.Sprintf("%s: type mismatch %v vs %v", path, a.Type(), b.Type())
	}
	switch a.Kind() {
	case reflect.Ptr, reflect.Interface:
		if a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: nil mismatch a=%v b=%v", path, a.IsNil(), b.IsNil())
		}
		if a.IsNil() {
			return ""
		}
		return diffValues(path, a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			fname := a.Type().Field(i).Name
			if d := diffValues(path+"."+fname, a.Field(i), b.Field(i)); d != "" {
				return d
			}
		}
		return ""
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: length %d vs %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffValues(fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i)); d != "" {
				return d
			}
		}
		return ""
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map length %d vs %d", path, a.Len(), b.Len())
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				return fmt.Sprintf("%s: missing key %v in b", path, k.Interface())
			}
			if d := diffValues(fmt.Sprintf("%s[%v]", path, k.Interface()), a.MapIndex(k), bv); d != "" {
				return d
			}
		}
		return ""
	case reflect.Float32, reflect.Float64:
		x, y := a.Float(), b.Float()
		if math.IsNaN(x) && math.IsNaN(y) {
			return ""
		}
		d := math.Abs(x - y)
		if d <= floatTol {
			return ""
		}
		mag := math.Max(math.Abs(x), math.Abs(y))
		if mag > 0 && d/mag <= floatRelTol {
			return ""
		}
		return fmt.Sprintf("%s: float %g vs %g (delta %g)", path, x, y, d)
	case reflect.String:
		if a.String() != b.String() {
			return fmt.Sprintf("%s: %q vs %q", path, a.String(), b.String())
		}
		return ""
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			as := fmt.Sprintf("%v", a.Interface())
			bs := fmt.Sprintf("%v", b.Interface())
			if len(as) > 80 {
				as = as[:80] + "…"
			}
			if len(bs) > 80 {
				bs = bs[:80] + "…"
			}
			return fmt.Sprintf("%s: %s vs %s", path, strings.TrimSpace(as), strings.TrimSpace(bs))
		}
		return ""
	}
}
