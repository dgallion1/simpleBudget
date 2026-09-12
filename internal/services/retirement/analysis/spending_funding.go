package analysis

import (
	"fmt"
	"math"
	"sort"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BuildSpendingFundingTimeline adapts the selected candidate's exact canonical
// projection into monthly real-dollar evidence and calendar-year averages.
func BuildSpendingFundingTimeline(in engine.Input, projection *models.ProjectionResult) (*models.SpendingFundingTimeline, error) {
	if in.Prepared.IsZero() {
		return nil, fmt.Errorf("prepared retirement settings are required")
	}
	if projection == nil || len(projection.Months) == 0 {
		return nil, fmt.Errorf("canonical projection evidence is required")
	}
	s := in.Prepared.Settings()
	expectedMonths := s.ProjectionYears * 12
	if expectedMonths <= 0 {
		return nil, fmt.Errorf("projection horizon must be positive")
	}
	if len(projection.Months) > expectedMonths {
		return nil, fmt.Errorf("canonical projection has %d months beyond the %d-month horizon", len(projection.Months), expectedMonths)
	}
	start, err := models.ParseYearMonth(s.StartDate)
	if err != nil {
		return nil, fmt.Errorf("projection start month: %w", err)
	}

	timeline := &models.SpendingFundingTimeline{
		Title:            "Income and withdrawals over time — base case",
		Months:           make([]models.SpendingFundingMonth, 0, len(projection.Months)),
		StartMonth:       calendarMonthLabel(start),
		ObservedEndMonth: calendarMonthLabel(start.AddDate(0, len(projection.Months)-1, 0)),
		ExpectedEndMonth: calendarMonthLabel(start.AddDate(0, expectedMonths-1, 0)),
		ExpectedMonths:   expectedMonths,
		ObservedMonths:   len(projection.Months),
		Complete:         len(projection.Months) == expectedMonths,
		EndReason:        "base_case_projection_ended",
		AccountingNote:   "These are separate model measures, not a complete cash-flow reconciliation.",
		CoverageNote:     "This base-case view does not certify dependable-income coverage.",
	}
	if timeline.Complete {
		timeline.EndReason = "horizon"
	} else {
		timeline.AvailabilityNote = "Later base-case months are unavailable; separate Monte Carlo minimum evidence covers complete modeled horizons."
	}

	for index, source := range projection.Months {
		if source.Month != index {
			return nil, fmt.Errorf("canonical projection month %d is out of order: got %d", index, source.Month)
		}
		if !finitePositive(source.CumulativeInflation) {
			return nil, fmt.Errorf("canonical projection month %d has invalid cumulative inflation", index)
		}
		income := engine.CalculateMonthlyIncomeBreakdown(in.Hooks, s, source.Month)
		month := models.SpendingFundingMonth{
			Month:                     source.Month,
			CalendarMonth:             calendarMonthLabel(start.AddDate(0, source.Month, 0)),
			SocialSecurityReal:        source.SocialSecurityIncome / source.CumulativeInflation,
			OtherConfiguredIncomeReal: income.OrdinaryIncome / source.CumulativeInflation,
			TaxDeferredWithdrawalReal: source.WithdrawalFromTaxDeferred / source.CumulativeInflation,
			TaxableWithdrawalReal:     source.WithdrawalFromTaxable / source.CumulativeInflation,
			RothWithdrawalReal:        source.WithdrawalFromRoth / source.CumulativeInflation,
			TaxesPaidReal:             source.TaxesPaid / source.CumulativeInflation,
			PlannedLivingReal:         source.PlannedLivingExpenses / source.CumulativeInflation,
			FundedLivingReal:          source.FundedLivingExpenses / source.CumulativeInflation,
			HealthcareReal:            source.HealthcareExpense / source.CumulativeInflation,
		}
		if !spendingFundingMonthFinite(month) {
			return nil, fmt.Errorf("canonical projection month %d contains a non-finite funding value", index)
		}
		timeline.Months = append(timeline.Months, month)
	}

	timeline.AnnualAverages = spendingFundingAnnualAverages(start, timeline.Months)
	timeline.Markers = spendingFundingConfiguredMarkers(in, start, len(timeline.Months))
	return timeline, nil
}

func spendingFundingAnnualAverages(start time.Time, months []models.SpendingFundingMonth) []models.SpendingFundingAnnualAverage {
	years := make([]models.SpendingFundingAnnualAverage, 0, len(months)/12+2)
	for _, month := range months {
		year := start.AddDate(0, month.Month, 0).Year()
		if len(years) == 0 || years[len(years)-1].CalendarYear != year {
			years = append(years, models.SpendingFundingAnnualAverage{CalendarYear: year})
		}
		row := &years[len(years)-1]
		row.ObservedMonths++
		row.SocialSecurityReal += month.SocialSecurityReal
		row.OtherConfiguredIncomeReal += month.OtherConfiguredIncomeReal
		row.TaxDeferredWithdrawalReal += month.TaxDeferredWithdrawalReal
		row.TaxableWithdrawalReal += month.TaxableWithdrawalReal
		row.RothWithdrawalReal += month.RothWithdrawalReal
		row.TaxesPaidReal += month.TaxesPaidReal
		row.PlannedLivingReal += month.PlannedLivingReal
		row.FundedLivingReal += month.FundedLivingReal
		row.HealthcareReal += month.HealthcareReal
	}
	for i := range years {
		row := &years[i]
		count := float64(row.ObservedMonths)
		row.Partial = row.ObservedMonths != 12
		row.SocialSecurityReal /= count
		row.OtherConfiguredIncomeReal /= count
		row.TaxDeferredWithdrawalReal /= count
		row.TaxableWithdrawalReal /= count
		row.RothWithdrawalReal /= count
		row.TaxesPaidReal /= count
		row.PlannedLivingReal /= count
		row.FundedLivingReal /= count
		row.HealthcareReal /= count
	}
	return years
}

func spendingFundingConfiguredMarkers(in engine.Input, start time.Time, observedMonths int) []models.SpendingFundingMarker {
	s := in.Prepared.Settings()
	markers := make([]models.SpendingFundingMarker, 0, len(s.IncomeSources)+1)
	optimizerSS := in.Hooks.SSActive(s)
	for i := range s.IncomeSources {
		source := &s.IncomeSources[i]
		if source.Amount <= 0 || source.StartMonth < 0 || source.StartMonth >= observedMonths ||
			(source.EndMonth != nil && *source.EndMonth <= source.StartMonth) {
			continue
		}
		kind := "configured_income_start"
		if engine.IsSocialSecurityIncomeSource(*source) {
			if optimizerSS {
				continue
			}
			kind = "social_security_start"
		}
		markers = append(markers, models.SpendingFundingMarker{
			Month:         source.StartMonth,
			CalendarMonth: calendarMonthLabel(start.AddDate(0, source.StartMonth, 0)),
			Kind:          kind,
			Label:         source.Name + " starts",
		})
	}
	if boost := s.LivingSpendingBoost; boost != nil {
		if stop, err := models.ParseYearMonth(boost.StopMonth); err == nil {
			month := (stop.Year()-start.Year())*12 + int(stop.Month()-start.Month())
			if month >= 0 && month < observedMonths {
				markers = append(markers, models.SpendingFundingMarker{
					Month:         month,
					CalendarMonth: calendarMonthLabel(stop),
					Kind:          "living_spending_boost_stop",
					Label:         "Early-spending boost stops",
				})
			}
		}
	}
	sort.SliceStable(markers, func(i, j int) bool {
		if markers[i].Month != markers[j].Month {
			return markers[i].Month < markers[j].Month
		}
		if markers[i].Kind != markers[j].Kind {
			return markers[i].Kind < markers[j].Kind
		}
		return markers[i].Label < markers[j].Label
	})
	return markers
}

func spendingFundingMonthFinite(month models.SpendingFundingMonth) bool {
	values := [...]float64{
		month.SocialSecurityReal,
		month.OtherConfiguredIncomeReal,
		month.TaxDeferredWithdrawalReal,
		month.TaxableWithdrawalReal,
		month.RothWithdrawalReal,
		month.TaxesPaidReal,
		month.PlannedLivingReal,
		month.FundedLivingReal,
		month.HealthcareReal,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func calendarMonthLabel(month time.Time) string {
	return month.Format("2006-01")
}
