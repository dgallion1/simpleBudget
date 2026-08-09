// Package whatifmcp shapes what-if analysis output for MCP consumption and
// applies scenario overrides. It reads scenarios and runs the engine; it never
// writes to the data directory.
package whatifmcp

import (
	"math"

	"budget2/internal/models"
)

// round0 rounds a currency amount to whole dollars. The engine's sub-cent
// precision is meaningful internally and noise in a conversation.
func round0(v float64) float64 { return math.Round(v) }

// AnalysisView is a compact projection of models.WhatIfAnalysis. It carries
// headline scalars and per-YEAR series only: models.ProjectionMonth series are
// 360 records for a 30-year plan and are served separately by MonthWindow, and
// the tax-optimizer candidates each embed a full *models.WhatIfSettings which
// is excluded entirely.
type AnalysisView struct {
	Headline   HeadlineView    `json:"headline"`
	Budget     *BudgetView     `json:"budget,omitempty"`
	Years      []YearView      `json:"years,omitempty"`
	RMD        *RMDView        `json:"rmd,omitempty"`
	Tax        *TaxView        `json:"tax,omitempty"`
	MonteCarlo *MonteCarloView `json:"monte_carlo,omitempty"`
}

type HeadlineView struct {
	PortfolioValue      float64 `json:"portfolio_value"`
	FinalBalance        float64 `json:"final_balance"`
	Survives            bool    `json:"survives"`
	DepletionMonth      *int    `json:"depletion_month,omitempty"`
	ProjectionYears     int     `json:"projection_years"`
	SustainabilityScore int     `json:"sustainability_score"`
	SustainabilityLabel string  `json:"sustainability_label,omitempty"`
}

type BudgetView struct {
	MonthlyExpenses float64 `json:"monthly_expenses"`
	MonthlyIncome   float64 `json:"monthly_income"`
	MonthlyTaxes    float64 `json:"monthly_taxes"`
	MonthlyIRMAA    float64 `json:"monthly_irmaa"`
	MonthlyRMD      float64 `json:"monthly_rmd"`
	MonthlyGap      float64 `json:"monthly_gap"`
}

type YearView struct {
	Year            int     `json:"year"`
	StartingBalance float64 `json:"starting_balance"`
	EndingBalance   float64 `json:"ending_balance"`
	Growth          float64 `json:"growth"`
	MAGI            float64 `json:"magi"`
	Taxes           float64 `json:"taxes"`
	IRMAA           float64 `json:"irmaa"`
	Expenses        float64 `json:"expenses"`
	Withdrawals     float64 `json:"withdrawals"`
}

type RMDView struct {
	StartAge         int     `json:"start_age"`
	StartsInYears    int     `json:"starts_in_years"`
	TaxDeferredValue float64 `json:"tax_deferred_value"`
	TotalFirst10Yr   float64 `json:"total_rmds_first_10yr"`
}

type TaxView struct {
	TotalFederalTaxPaid  float64 `json:"total_federal_tax_paid"`
	TotalStateTaxPaid    float64 `json:"total_state_tax_paid"`
	TotalTaxPaid         float64 `json:"total_tax_paid"`
	AverageEffectiveRate float64 `json:"average_effective_rate"`
}

// MonteCarloView carries stats only, never the full distribution.
type MonteCarloView struct {
	SuccessRate float64 `json:"success_rate"`
}

// ShapeAnalysis converts a full analysis into its compact view.
//
// includeMonteCarlo is false for run_scenario: the orchestrator auto-seeds the
// Monte Carlo RNG from the clock, so MC figures differ between two runs of
// identical inputs. Including them in an override comparison would present that
// noise as an effect of the override.
func ShapeAnalysis(a *models.WhatIfAnalysis, includeMonteCarlo bool) AnalysisView {
	v := AnalysisView{}
	if a == nil {
		return v
	}

	if a.Settings != nil {
		v.Headline.PortfolioValue = round0(a.Settings.PortfolioValue)
		v.Headline.ProjectionYears = a.Settings.ProjectionYears
	}
	if p := a.Projection; p != nil {
		v.Headline.FinalBalance = round0(p.FinalBalance)
		v.Headline.Survives = p.Survives
		v.Headline.DepletionMonth = p.DepletionMonth
		for _, y := range p.YearlySummaries {
			v.Years = append(v.Years, YearView{
				Year:            y.Year,
				StartingBalance: round0(y.StartingBalance),
				EndingBalance:   round0(y.EndingBalance),
				Growth:          round0(y.Growth),
				MAGI:            round0(y.MAGI),
				Taxes:           round0(y.Taxes),
				IRMAA:           round0(y.IRMAA),
				Expenses:        round0(y.Expenses),
				Withdrawals:     round0(y.Withdrawals),
			})
		}
	}
	if s := a.Sustainability; s != nil {
		v.Headline.SustainabilityScore = s.Score
		v.Headline.SustainabilityLabel = s.Label
	}
	if b := a.BudgetFit; b != nil {
		v.Budget = &BudgetView{
			MonthlyExpenses: round0(b.MonthlyExpenses),
			MonthlyIncome:   round0(b.MonthlyIncome),
			MonthlyTaxes:    round0(b.MonthlyTaxes),
			MonthlyIRMAA:    round0(b.MonthlyIRMAA),
			MonthlyRMD:      round0(b.MonthlyRMD),
			MonthlyGap:      round0(b.MonthlyGap),
		}
	}
	if r := a.RMD; r != nil {
		v.RMD = &RMDView{
			StartAge:         r.StartAge,
			StartsInYears:    r.StartsInYears,
			TaxDeferredValue: round0(r.TaxDeferredValue),
			TotalFirst10Yr:   round0(r.TotalRMDsOver10Yr),
		}
	}
	if t := a.Tax; t != nil {
		v.Tax = &TaxView{
			TotalFederalTaxPaid:  round0(t.TotalFederalTaxPaid),
			TotalStateTaxPaid:    round0(t.TotalStateTaxPaid),
			TotalTaxPaid:         round0(t.TotalTaxPaid),
			AverageEffectiveRate: t.AverageEffectiveRate,
		}
	}
	if includeMonteCarlo && a.MonteCarlo != nil && a.MonteCarlo.Stats != nil {
		v.MonteCarlo = &MonteCarloView{SuccessRate: a.MonteCarlo.Stats.SuccessRate}
	}
	return v
}
