package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

// HistoricalYear is re-exported as an alias for models.HistoricalYear.
// The canonical historical dataset lives in
// internal/services/retirement/history. The alias keeps existing
// retirement-side callers compiling during the migration window;
// removed in Task 8.
type HistoricalYear = models.HistoricalYear

// HistoricalReturns is the legacy global view of the canonical
// historical dataset. New code should call history.DefaultData()
// directly. Removed in Task 8.
var HistoricalReturns = []models.HistoricalYear(history.DefaultData())

// GetHistoricalReturns returns all historical data. Wrapper around
// history.DefaultData(); removed in Task 8.
func GetHistoricalReturns() []models.HistoricalYear {
	return []models.HistoricalYear(history.DefaultData())
}

// GetHistoricalSequence returns a slice of historical years starting
// from a given year. Wrapper around history.Sequence; removed in
// Task 8.
func GetHistoricalSequence(startYear int, yearsNeeded int) []models.HistoricalYear {
	return []models.HistoricalYear(history.Sequence(history.DefaultData(), startYear, yearsNeeded))
}

// GetAvailableStartYears returns the years that can be used as
// starting points for backtesting given the required projection
// length. Wrapper around history.AvailableStartYears; removed in
// Task 8.
func GetAvailableStartYears(projectionYears int) []int {
	return history.AvailableStartYears(history.DefaultData(), projectionYears)
}

// GetHistoricalStats returns summary statistics for the historical
// data. Wrapper around history.Stats; removed in Task 8.
func GetHistoricalStats() (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	return history.Stats(history.DefaultData())
}
