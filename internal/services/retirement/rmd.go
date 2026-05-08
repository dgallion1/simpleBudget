package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// RMD start age per IRS rules (SECURE 2.0 Act). Re-exported from the
// engine package during the migration window so handler/template code
// reading retirement.RMDStartAge keeps compiling.
const RMDStartAge = engine.RMDStartAge

// RMD-day helpers were moved to the engine package. The aliases below
// keep existing call sites in calculator.go, backtest.go, handler
// code, and tests compiling unchanged. The aliases (and these
// declarations) are removed in Task 8.
var (
	EffectiveRMDStartAge   = engine.EffectiveRMDStartAge
	FirstRMDCalendarYear   = engine.FirstRMDCalendarYear
	RMDApplies             = engine.RMDApplies
	RMDAgeForCalendarYear  = engine.RMDAgeForCalendarYear
	CalculateRMD           = engine.CalculateRMD
	GetLifeExpectancyFactor = engine.GetLifeExpectancyFactor
)

// rmdTriggerMonth and parseStartYear are unexported retirement-side
// names referenced by calculator.go, backtest.go, and tests. They
// forward to the exported engine implementations.
var (
	rmdTriggerMonth = engine.RMDTriggerMonth
	parseStartYear  = engine.ParseStartYear
)

// BuildRMDAnalysis (F-072) builds the RMD analysis from the actual
// projection instead of an isolated standalone math model. It samples
// each RMD year's starting tax-deferred balance and sums the actual
// RMDWithdrawal over the year, so the panel cannot diverge from the
// main projection.
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	s := c.Settings

	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	effectiveStartAge := EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()
	startYear := parseStartYear(s.StartDate)
	firstRMDYear := FirstRMDCalendarYear(s)
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

	if projection == nil || len(projection.Months) == 0 {
		return result
	}

	// Surface depletion year (whole years from start; floor of months/12).
	if projection.DepletionMonth != nil {
		dy := *projection.DepletionMonth / 12
		// F-078: depletion age uses age-at-year-end of the depletion calendar
		// year so it stays consistent with the per-row Age column and the
		// projection engine's RMD trigger year for late-year births.
		da := RMDAgeForCalendarYear(s, startYear+dy)
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
	if maxYears > len(projection.Months)/12 {
		maxYears = len(projection.Months) / 12
	}

	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		calendarYear := startYear + y
		if !RMDApplies(s, calendarYear) {
			continue
		}
		age := RMDAgeForCalendarYear(s, calendarYear)
		// Stop at depletion year — no further rows.
		if result.DepletionYear != nil && y >= *result.DepletionYear {
			break
		}

		// Start-of-year tax-deferred balance.
		startIdx := 12*y - 1
		if y == 0 {
			startIdx = 0
		}
		if startIdx >= len(projection.Months) {
			break
		}
		startBalance := projection.Months[startIdx].TaxDeferredBalance

		// Sum actual RMDWithdrawal across the 12 months of this year.
		startMonth := 12 * y
		endMonth := startMonth + 12
		if endMonth > len(projection.Months) {
			endMonth = len(projection.Months)
		}
		rmdAmount := 0.0
		for m := startMonth; m < endMonth; m++ {
			rmdAmount += projection.Months[m].RMDWithdrawal
		}

		factor := GetLifeExpectancyFactor(age)
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
