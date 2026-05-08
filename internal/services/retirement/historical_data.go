package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

// HistoricalYear is re-exported as an alias for models.HistoricalYear.
// The canonical historical dataset lives in
// internal/services/retirement/history. The alias is kept so the
// retirement-package test suite can refer to HistoricalYear without
// importing models directly.
type HistoricalYear = models.HistoricalYear

// HistoricalReturns is the legacy global view of the canonical
// historical dataset. New code should call history.DefaultData()
// directly; this var is retained for retirement-package tests
// (calculator_coverage_test) that index it.
var HistoricalReturns = []models.HistoricalYear(history.DefaultData())

// GetHistoricalReturns returns all historical data. Wrapper around
// history.DefaultData(); kept as a compat shim for retirement-package
// tests.
func GetHistoricalReturns() []models.HistoricalYear {
	return []models.HistoricalYear(history.DefaultData())
}

// GetHistoricalSequence returns a slice of historical years starting
// from a given year. Wrapper around history.Sequence; kept as a compat
// shim for retirement-package tests.
func GetHistoricalSequence(startYear int, yearsNeeded int) []models.HistoricalYear {
	return []models.HistoricalYear(history.Sequence(history.DefaultData(), startYear, yearsNeeded))
}

// GetAvailableStartYears returns the years that can be used as
// starting points for backtesting given the required projection
// length. Wrapper around history.AvailableStartYears; kept as a compat
// shim for retirement-package tests.
func GetAvailableStartYears(projectionYears int) []int {
	return history.AvailableStartYears(history.DefaultData(), projectionYears)
}

// GetHistoricalStats returns summary statistics for the historical
// data. Wrapper around history.Stats; kept as a compat shim for
// retirement-package tests.
func GetHistoricalStats() (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	return history.Stats(history.DefaultData())
}
