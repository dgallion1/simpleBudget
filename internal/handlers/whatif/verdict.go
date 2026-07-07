package whatif

import (
	"fmt"

	"budget2/internal/models"
)

// mcStrongThreshold is the Monte Carlo success rate (0-100) at or above which a
// fully-funded plan is considered green rather than amber.
const mcStrongThreshold = 70.0

// earlyDepletionYears: depleting within this many years is "red", later is "amber".
const earlyDepletionYears = 10

// VerdictView is the precomputed model the sticky verdict bar renders.
type VerdictView struct {
	Health         models.Health
	Headline       string  // e.g. "Funded through 2064" / "Funds run out in 2032"
	Detail         string  // e.g. "spending covered for all 38 years"
	YearsCovered   int     // full horizon if survives, else years to depletion
	MonthlyGap     float64 // gap shown in the bar (>0 = shortfall)
	GapIsShortfall bool
	RequiredRate   float64
	SuccessRate    float64 // 0-100
	HasMonteCarlo  bool

	// Strip extras: lifetime income tax and projected end balance shown in
	// the sticky bar so the Overview tab needs no duplicate KPI row.
	TotalTaxes    float64
	HasTaxes      bool
	EndBalance    float64
	HasEndBalance bool

	// GapAtSteadyState reports whether MonthlyGap/RequiredRate reflect the
	// steady-state year currently in view (driven by the budget slider)
	// rather than today's values. GapYear is that year when true.
	GapAtSteadyState bool
	GapYear          int
}

// BuildVerdict derives the verdict bar model from analysis already computed by
// the engine. It performs no projection math of its own.
func BuildVerdict(a *models.WhatIfAnalysis, s *models.WhatIfSettings) VerdictView {
	v := VerdictView{Health: models.HealthAmber}
	if a == nil || s == nil {
		return v
	}

	startYear := 0
	if t, err := models.ParseYearMonth(s.StartDate); err == nil {
		startYear = t.Year()
	}

	if a.BudgetFit != nil {
		// Default to today's gap/rate. When a steady-state year is in view
		// (the budget slider sets BudgetFit.SteadyStateYear), report that
		// year's figures instead so the verdict bar tracks the slider.
		gap := a.BudgetFit.MonthlyGap
		rate := a.BudgetFit.RequiredRate
		if a.BudgetFit.HasSteadyState {
			gap = a.BudgetFit.SteadyStateGap
			rate = a.BudgetFit.SteadyStateRate
			v.GapAtSteadyState = true
			v.GapYear = int(a.BudgetFit.SteadyStateYear)
		}
		v.MonthlyGap = gap
		v.GapIsShortfall = gap > 0
		v.RequiredRate = rate
	}
	if a.MonteCarlo != nil && a.MonteCarlo.Stats != nil {
		v.HasMonteCarlo = true
		v.SuccessRate = a.MonteCarlo.Stats.SuccessRate
	}

	if a.Tax != nil {
		v.HasTaxes = true
		v.TotalTaxes = a.Tax.TotalTaxPaid
	}
	if a.Projection != nil {
		v.HasEndBalance = true
		v.EndBalance = a.Projection.FinalBalance
	}

	survives := a.Projection != nil && a.Projection.Survives
	if survives {
		v.YearsCovered = s.ProjectionYears
		v.Headline = fmt.Sprintf("Funded through %d", startYear+s.ProjectionYears)
		v.Detail = fmt.Sprintf("spending covered for all %d years", s.ProjectionYears)
		if !v.HasMonteCarlo || v.SuccessRate >= mcStrongThreshold {
			v.Health = models.HealthGreen
		} else {
			// Median path survives but a material share of simulations fail.
			// The words must not out-promise the health band.
			v.Health = models.HealthAmber
			v.Detail = fmt.Sprintf("covers the median path — %.0f%% of market simulations fall short", 100-v.SuccessRate)
		}
		return v
	}

	// Depletes within the horizon.
	depletionYears := s.ProjectionYears
	if a.Projection != nil && a.Projection.DepletionMonth != nil {
		depletionYears = *a.Projection.DepletionMonth / 12
	}
	v.YearsCovered = depletionYears
	v.Headline = fmt.Sprintf("Funds run out in %d", startYear+depletionYears)
	v.Detail = fmt.Sprintf("covered for %d of %d years", depletionYears, s.ProjectionYears)
	if depletionYears < earlyDepletionYears {
		v.Health = models.HealthRed
	} else {
		v.Health = models.HealthAmber
	}
	return v
}
