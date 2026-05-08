// Package analysis hosts pure post-projection analyses that consume a
// projection result (and engine input where tax tables / scenario chain
// matter) and produce user-facing summaries. Functions here have no
// hidden state and never run a projection themselves.
package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BuildRMD (F-072) builds the RMD analysis from the actual projection
// instead of an isolated standalone math model. It samples each RMD
// year's starting tax-deferred balance and sums the actual
// RMDWithdrawal over the year, so the panel cannot diverge from the
// main projection.
func BuildRMD(proj *models.ProjectionResult, in engine.Input) *models.RMDAnalysis {
	s := in.Prepared.Settings()

	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	effectiveStartAge := engine.EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()
	startYear := engine.ParseStartYear(s.StartDate)
	firstRMDYear := engine.FirstRMDCalendarYear(s)
	// F-078: startsInYears = calendar gap to first RMD year, not floor'd-age
	// subtraction. Late-year births differ by one.
	startsInYears := firstRMDYear - startYear
	if startsInYears < 0 {
		startsInYears = 0
	}

	result := &models.RMDAnalysis{
		StartsInYears:    startsInYears,
		StartAge:         effectiveStartAge,
		CurrentAge:       olderAge,
		TaxDeferredValue: taxDeferredValue,
		Projections:      []models.RMDProjection{},
	}

	if taxDeferredValue == 0 {
		return result
	}

	if proj == nil || len(proj.Months) == 0 {
		return result
	}

	// Surface depletion year (whole years from start; floor of months/12).
	if proj.DepletionMonth != nil {
		dy := *proj.DepletionMonth / 12
		// F-078: depletion age uses age-at-year-end of the depletion calendar
		// year so it stays consistent with the per-row Age column and the
		// projection engine's RMD trigger year for late-year births.
		da := engine.RMDAgeForCalendarYear(s, startYear+dy)
		result.DepletionYear = &dy
		result.DepletionAge = &da
		if dy <= startsInYears {
			result.DepletedBeforeRMD = true
			return result
		}
	}

	// Iterate projection years, emit a row once RMDs apply to the older
	// household member for that calendar year (F-078 calendar-year gate).
	maxYears := s.ProjectionYears
	if maxYears > len(proj.Months)/12 {
		maxYears = len(proj.Months) / 12
	}

	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		calendarYear := startYear + y
		if !engine.RMDApplies(s, calendarYear) {
			continue
		}
		age := engine.RMDAgeForCalendarYear(s, calendarYear)
		// Stop at depletion year — no further rows.
		if result.DepletionYear != nil && y >= *result.DepletionYear {
			break
		}

		// Start-of-year tax-deferred balance.
		startIdx := 12*y - 1
		if y == 0 {
			startIdx = 0
		}
		if startIdx >= len(proj.Months) {
			break
		}
		startBalance := proj.Months[startIdx].TaxDeferredBalance

		// Sum actual RMDWithdrawal across the 12 months of this year.
		startMonth := 12 * y
		endMonth := startMonth + 12
		if endMonth > len(proj.Months) {
			endMonth = len(proj.Months)
		}
		rmdAmount := 0.0
		for m := startMonth; m < endMonth; m++ {
			rmdAmount += proj.Months[m].RMDWithdrawal
		}

		factor := engine.GetLifeExpectancyFactor(age)
		rmdPercent := 0.0
		if factor > 0 {
			rmdPercent = 100.0 / factor
		}

		result.Projections = append(result.Projections, models.RMDProjection{
			Age:            age,
			Year:           y,
			TaxDeferredBal: startBalance,
			LifeExpFactor:  factor,
			RMDAmount:      rmdAmount,
			RMDPercent:     rmdPercent,
		})

		if rmdCount < 10 {
			result.TotalRMDsOver10Yr += rmdAmount
		}
		rmdCount++
	}

	return result
}
