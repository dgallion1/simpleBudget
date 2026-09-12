package engine

import (
	"budget2/internal/models"
	"time"
)

// LivingSpendingBoostAtMonth returns the scheduled living boost at the supplied
// projection calendar month, using the path's cumulative general inflation.
func LivingSpendingBoostAtMonth(boost *models.LivingSpendingBoost, calendarMonth time.Time, cpi float64) float64 {
	if boost == nil {
		return 0
	}
	stop, err := models.ParseYearMonth(boost.StopMonth)
	if err != nil || !calendarMonth.Before(stop) {
		return 0
	}
	return boost.MonthlyReal * cpi
}

func projectionCalendarMonth(startDate string, month int) time.Time {
	start, _ := models.ParseYearMonth(startDate) // Prepared settings validate StartDate.
	return start.AddDate(0, month, 0)
}
