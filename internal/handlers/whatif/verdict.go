package whatif

import (
	"fmt"

	"budget2/internal/models"
)

// VerdictHealth classifies the overall plan outcome for the verdict bar tint.
type VerdictHealth string

const (
	VerdictGreen VerdictHealth = "green"
	VerdictAmber VerdictHealth = "amber"
	VerdictRed   VerdictHealth = "red"
)

// mcStrongThreshold is the Monte Carlo success rate (0-100) at or above which a
// fully-funded plan is considered green rather than amber.
const mcStrongThreshold = 70.0

// earlyDepletionYears: depleting within this many years is "red", later is "amber".
const earlyDepletionYears = 10

// VerdictView is the precomputed model the sticky verdict bar renders.
type VerdictView struct {
	Health         VerdictHealth
	Headline       string  // e.g. "Funded through 2064" / "Funds run out in 2032"
	Detail         string  // e.g. "spending covered for all 38 years"
	YearsCovered   int     // full horizon if survives, else years to depletion
	MonthlyGap     float64 // BudgetFit.MonthlyGap (>0 = shortfall)
	GapIsShortfall bool
	RequiredRate   float64
	SuccessRate    float64 // 0-100
	HasMonteCarlo  bool
}

// BuildVerdict derives the verdict bar model from analysis already computed by
// the engine. It performs no projection math of its own.
func BuildVerdict(a *models.WhatIfAnalysis, s *models.WhatIfSettings) VerdictView {
	v := VerdictView{Health: VerdictAmber}
	if a == nil || s == nil {
		return v
	}

	startYear := 0
	if t, err := models.ParseYearMonth(s.StartDate); err == nil {
		startYear = t.Year()
	}

	if a.BudgetFit != nil {
		v.MonthlyGap = a.BudgetFit.MonthlyGap
		v.GapIsShortfall = a.BudgetFit.MonthlyGap > 0
		v.RequiredRate = a.BudgetFit.RequiredRate
	}
	if a.MonteCarlo != nil && a.MonteCarlo.Stats != nil {
		v.HasMonteCarlo = true
		v.SuccessRate = a.MonteCarlo.Stats.SuccessRate
	}

	survives := a.Projection != nil && a.Projection.Survives
	if survives {
		v.YearsCovered = s.ProjectionYears
		v.Headline = fmt.Sprintf("Funded through %d", startYear+s.ProjectionYears)
		v.Detail = fmt.Sprintf("spending covered for all %d years", s.ProjectionYears)
		if !v.HasMonteCarlo || v.SuccessRate >= mcStrongThreshold {
			v.Health = VerdictGreen
		} else {
			v.Health = VerdictAmber
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
		v.Health = VerdictRed
	} else {
		v.Health = VerdictAmber
	}
	return v
}
