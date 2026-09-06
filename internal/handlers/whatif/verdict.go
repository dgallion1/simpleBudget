package whatif

import (
	"fmt"

	"budget2/internal/models"
)

// VerdictView is the precomputed model the sticky verdict bar renders.
type VerdictView struct {
	Health              models.Health
	Headline            string  // Calendar month and year of the actual endpoint.
	Detail              string  // Conditional description of the observed base projection.
	YearsCovered        int     // full horizon if survives, else years to depletion
	MonthlyGap          float64 // selected gap (>0 = additional funding need)
	GapIsShortfall      bool
	RequiredRate        float64
	SuccessRate         float64 // 0-100
	HasMonteCarlo       bool
	HasBudgetFit        bool
	CurrentMonthlyGap   float64
	CurrentRequiredRate float64
	HasFirstYearOutflow bool
	FirstYearOutflow    float64

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
	v := VerdictView{Health: models.HealthNeutral, Headline: "Projection unavailable", Detail: "Base projection results are unavailable"}
	if a == nil || s == nil {
		return v
	}
	start, dateErr := models.ParseYearMonth(s.StartDate)
	if a.BudgetFit != nil {
		b := a.BudgetFit
		v.HasBudgetFit = true
		v.CurrentMonthlyGap, v.CurrentRequiredRate = b.MonthlyGap, b.RequiredRate
		v.MonthlyGap, v.RequiredRate = b.MonthlyGap, b.RequiredRate
		if b.HasSteadyState {
			v.MonthlyGap, v.RequiredRate = b.SteadyStateGap, b.SteadyStateRate
			v.GapAtSteadyState, v.GapYear = true, int(b.SteadyStateYear)
		}
		v.GapIsShortfall = v.MonthlyGap > 0
	}
	if a.MonteCarlo != nil && a.MonteCarlo.Stats != nil && a.MonteCarlo.Stats.Runs > 0 {
		v.HasMonteCarlo, v.SuccessRate = true, a.MonteCarlo.Stats.SuccessRate
	}
	if a.Tax != nil {
		v.HasTaxes, v.TotalTaxes = true, a.Tax.TotalTaxPaid
	}
	p := a.Projection
	if p == nil {
		return v
	}
	v.HasEndBalance, v.EndBalance = true, p.FinalBalance
	if len(p.YearlySummaries) > 0 {
		year := p.YearlySummaries[0]
		v.HasFirstYearOutflow, v.FirstYearOutflow = true, year.Withdrawals
	}
	unpaid := false
	for _, month := range p.Months {
		if month.FundingShortfall > 1e-7 {
			unpaid = true
			break
		}
	}
	if !p.Survives || unpaid {
		v.Health = models.HealthRed
		v.Headline = "Base projection has a funding shortfall"
		v.Detail = "Base projection has a funding shortfall"
		if s.Guardrails != nil && s.Guardrails.Enabled {
			v.Detail += " despite configured guardrails"
		}
		if unpaid {
			v.Detail += "; some spending is unpaid"
		}
		if !p.Survives && p.DepletionMonth != nil && *p.DepletionMonth >= 0 {
			v.YearsCovered = *p.DepletionMonth / 12
			if dateErr == nil {
				v.Headline = "Funds run out in " + start.AddDate(0, *p.DepletionMonth, 0).Format("Jan 2006")
			}
		}
		return v
	}
	v.YearsCovered = s.ProjectionYears
	v.Headline = "Base projection funds spending"
	if dateErr == nil && s.ProjectionYears > 0 {
		v.Headline = "Funded through " + start.AddDate(0, s.ProjectionYears*12-1, 0).Format("Jan 2006")
	}
	v.Detail = fmt.Sprintf("Base projection funds your planned lifestyle under these assumptions for %d years", s.ProjectionYears)
	if projectionHasLivingCuts(p) {
		v.Health = models.HealthAmber
		v.Detail = "Base projection funds spending with circuit-breaker cuts under these assumptions"
	}
	return v
}

// projectionHasLivingCuts compares observed living spending with its planned
// baseline, including phase schedules. Legacy months without a baseline cannot
// establish a cut; a zero multiplier with a positive baseline is a full cut.
func projectionHasLivingCuts(p *models.ProjectionResult) bool {
	if p == nil {
		return false
	}
	for _, month := range p.Months {
		if month.PlannedLivingExpenses > 0 && month.GuardrailMultiplier < 1-1e-9 {
			return true
		}
	}
	return false
}
