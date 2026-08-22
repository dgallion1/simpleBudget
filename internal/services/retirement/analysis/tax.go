package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BuildTax summarizes the projection's tax burden into a TaxAnalysis. It
// reads the per-year totals the engine already computed (proj.YearlySummaries:
// income tax and MAGI) and folds in the per-month state-tax, Roth-conversion,
// and RMD figures, so the panel can never diverge from the main projection.
//
// The income-tax total equals the sum of YearlySummaries.Taxes — the same
// figure the explainability panel reports as TotalTaxes. IRMAA is a Medicare
// premium surcharge, not income tax, so it is deliberately excluded here.
//
// ConversionTaxPaid is left zero: isolating the marginal tax attributable to
// Roth conversions requires a counterfactual no-conversion projection, which
// this pure post-projection summary does not run.
// nearestCliff finds the year that comes closest to a step-cost income
// threshold without the plan doing anything about it. That year is where a
// small change in timing is worth the most, so it is the one worth naming.
func nearestCliff(summaries []models.ProjectionYearSummary, startYear, olderAge int) *models.CliffProximity {
	var best *models.CliffProximity
	for _, ys := range summaries {
		if ys.NextCliffLabel == "" {
			continue
		}
		if best != nil && ys.NextCliffHeadroom >= best.Headroom {
			continue
		}
		best = &models.CliffProximity{
			Year:       startYear + ys.Year,
			Age:        olderAge + ys.Year,
			Label:      ys.NextCliffLabel,
			Headroom:   ys.NextCliffHeadroom,
			AnnualCost: ys.NextCliffAnnualCost,
		}
	}
	return best
}

// constantsBasis reports which published tax figures the analysis rests on
// and which of its years are extrapolated from them.
func constantsBasis(s *models.WhatIfSettings, startYear, years int) *models.TaxConstantsBasis {
	statutoryYear, provenance := engine.LatestStatutoryFederalProvenance()
	basis := &models.TaxConstantsBasis{
		StatutoryYear: statutoryYear,
		Source:        provenance.Source,
		VerifiedOn:    provenance.VerifiedOn,
	}
	if years <= 0 {
		return basis
	}

	lastYear := startYear + years - 1
	if lastYear <= statutoryYear {
		return basis
	}
	first := statutoryYear + 1
	if startYear > first {
		first = startYear
	}
	basis.FirstProjectedYear = first
	basis.LastProjectedYear = lastYear
	basis.InflationRate = s.InflationRate
	return basis
}

func BuildTax(proj *models.ProjectionResult, in engine.Input) *models.TaxAnalysis {
	if proj == nil || len(proj.Months) == 0 {
		return nil
	}

	s := in.Prepared.Settings()
	startYear := engine.ParseStartYear(s.StartDate)
	olderAge := s.GetOlderAge()

	// Per-relative-year figures not carried on the yearly summary.
	type yearAgg struct {
		stateTax       float64
		rothConversion float64
		rmd            float64
	}
	byYear := make(map[int]*yearAgg, len(proj.Months)/12+1)
	for _, m := range proj.Months {
		y := m.Month / 12
		agg := byYear[y]
		if agg == nil {
			agg = &yearAgg{}
			byYear[y] = agg
		}
		agg.stateTax += m.StateTaxPaid
		agg.rothConversion += m.RothConversions
		agg.rmd += m.RMDWithdrawal
	}

	result := &models.TaxAnalysis{
		YearlyTaxSummary: make([]models.YearlyTaxSummary, 0, len(proj.YearlySummaries)),
	}

	var totalGrossIncome float64
	for _, ys := range proj.YearlySummaries {
		stateTax := 0.0
		rothConversion := 0.0
		rmd := 0.0
		if agg := byYear[ys.Year]; agg != nil {
			stateTax = agg.stateTax
			rothConversion = agg.rothConversion
			rmd = agg.rmd
		}

		totalTax := ys.Taxes
		federalTax := totalTax - stateTax
		if federalTax < 0 {
			federalTax = 0
		}

		effectiveRate := 0.0
		if ys.GrossIncome > 0 {
			effectiveRate = totalTax / ys.GrossIncome * 100
		}

		calendarYear := startYear + ys.Year

		result.TotalTaxPaid += totalTax
		result.TotalFederalTaxPaid += federalTax
		result.TotalStateTaxPaid += stateTax
		totalGrossIncome += ys.GrossIncome

		result.YearlyTaxSummary = append(result.YearlyTaxSummary, models.YearlyTaxSummary{
			Year:          calendarYear,
			Age:           olderAge + ys.Year,
			TaxableIncome: ys.MAGI,
			FederalTax:    federalTax,
			StateTax:      stateTax,
			TotalTax:      totalTax,
			EffectiveRate: effectiveRate,
			// Measured numerically by the engine from this year's own income
			// composition, so it reflects capital-gain stacking and the § 86
			// phase-in. Previously a bracket-table lookup fed MAGI, which
			// understated the rate and passed the wrong quantity in.
			MarginalRate:             ys.MarginalRate,
			MarginalRateLongTermGain: ys.MarginalRateLongTermGain,
			RothConversion:           rothConversion,
			RMDAmount:                rmd,
		})
	}

	if totalGrossIncome > 0 {
		result.AverageEffectiveRate = result.TotalTaxPaid / totalGrossIncome * 100
	}
	result.NearestCliff = nearestCliff(proj.YearlySummaries, startYear, olderAge)
	result.ConstantsBasis = constantsBasis(s, startYear, len(proj.YearlySummaries))

	return result
}
