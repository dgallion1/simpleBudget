package retirement

import "math"

// HistoricalYear contains annual market data for backtesting
type HistoricalYear struct {
	Year          int     // Calendar year
	SP500Return   float64 // S&P 500 total return (%)
	BondReturn    float64 // 10-year Treasury bond return (%)
	CashReturn    float64 // 3-month T-bill return (%)
	InflationRate float64 // Annual CPI inflation (%)
}

// HistoricalReturns contains annual market data from 1928-2024
// Sources:
// - S&P 500: Shiller data (http://www.econ.yale.edu/~shiller/data.htm)
// - Bonds: 10-Year Treasury (FRED)
// - Cash: 3-Month T-Bill (NYU Stern / Damodaran)
// - Inflation: BLS CPI-U
var HistoricalReturns = []HistoricalYear{
	// Year, S&P500, 10Y Bond, T-Bill, Inflation
	{1928, 43.81, 0.84, 3.08, -1.16},
	{1929, -8.30, 4.20, 3.16, 0.58},
	{1930, -25.12, 4.54, 4.55, -6.40},
	{1931, -43.84, -2.56, 2.31, -9.32},
	{1932, -8.64, 8.79, 1.07, -10.27},
	{1933, 49.98, 1.86, 0.96, 0.76},
	{1934, -1.19, 7.96, 0.28, 1.52},
	{1935, 46.74, 4.47, 0.17, 2.99},
	{1936, 31.94, 5.02, 0.17, 1.45},
	{1937, -35.34, 1.38, 0.28, 2.86},
	{1938, 29.28, 4.21, 0.07, -2.78},
	{1939, -1.10, 4.41, 0.05, 0.00},
	{1940, -10.67, 5.40, 0.04, 0.71},
	{1941, -12.77, -2.02, 0.13, 9.93},
	{1942, 19.17, 2.29, 0.34, 9.03},
	{1943, 25.06, 2.49, 0.38, 2.96},
	{1944, 19.03, 2.58, 0.38, 2.30},
	{1945, 35.82, 3.80, 0.38, 2.25},
	{1946, -8.43, 3.13, 0.38, 18.13},
	{1947, 5.20, 0.92, 0.60, 8.84},
	{1948, 5.70, 1.95, 1.05, 2.99},
	{1949, 18.30, 4.66, 1.12, -2.07},
	{1950, 30.81, 0.43, 1.20, 5.93},
	{1951, 23.68, -0.30, 1.52, 6.00},
	{1952, 18.15, 2.27, 1.72, 0.75},
	{1953, -1.21, 4.14, 1.89, 0.75},
	{1954, 52.56, 3.29, 0.94, -0.74},
	{1955, 32.60, -1.34, 1.72, 0.37},
	{1956, 7.44, -2.26, 2.62, 2.99},
	{1957, -10.46, 6.80, 3.22, 2.90},
	{1958, 43.72, -2.10, 1.77, 1.76},
	{1959, 12.06, -2.65, 3.39, 1.73},
	{1960, 0.34, 11.64, 2.87, 1.36},
	{1961, 26.64, 2.06, 2.35, 0.67},
	{1962, -8.81, 5.69, 2.77, 1.33},
	{1963, 22.61, 1.68, 3.16, 1.64},
	{1964, 16.42, 3.73, 3.55, 0.97},
	{1965, 12.40, 0.71, 3.95, 1.92},
	{1966, -9.97, 2.91, 4.86, 3.46},
	{1967, 23.80, -1.58, 4.29, 3.04},
	{1968, 10.81, 3.27, 5.34, 4.72},
	{1969, -8.24, -5.01, 6.67, 6.20},
	{1970, 3.56, 16.75, 6.39, 5.57},
	{1971, 14.22, 9.79, 4.33, 3.27},
	{1972, 18.76, 2.82, 4.06, 3.41},
	{1973, -14.31, 3.66, 7.04, 8.71},
	{1974, -25.90, 4.00, 7.85, 12.34},
	{1975, 37.00, 3.61, 5.79, 6.94},
	{1976, 23.83, 15.98, 4.98, 4.86},
	{1977, -6.98, 1.29, 5.26, 6.70},
	{1978, 6.51, -0.78, 7.18, 9.02},
	{1979, 18.52, 0.67, 10.05, 13.29},
	{1980, 31.74, -2.99, 11.39, 12.52},
	{1981, -4.70, 8.20, 14.04, 8.92},
	{1982, 20.42, 32.81, 10.60, 3.83},
	{1983, 22.34, 3.20, 8.62, 3.79},
	{1984, 6.15, 13.73, 9.54, 3.95},
	{1985, 31.24, 25.71, 7.47, 3.80},
	{1986, 18.49, 24.28, 5.97, 1.10},
	{1987, 5.81, -4.96, 5.78, 4.43},
	{1988, 16.54, 8.22, 6.67, 4.42},
	{1989, 31.48, 17.69, 8.11, 4.65},
	{1990, -3.06, 6.24, 7.50, 6.11},
	{1991, 30.23, 15.00, 5.38, 3.06},
	{1992, 7.49, 9.36, 3.43, 2.90},
	{1993, 9.97, 14.21, 3.00, 2.75},
	{1994, 1.33, -8.04, 4.25, 2.67},
	{1995, 37.20, 23.48, 5.49, 2.54},
	{1996, 22.68, 1.43, 5.01, 3.32},
	{1997, 33.10, 9.94, 5.06, 1.70},
	{1998, 28.34, 14.92, 4.78, 1.61},
	{1999, 20.89, -8.25, 4.64, 2.68},
	{2000, -9.03, 16.66, 5.82, 3.39},
	{2001, -11.85, 5.57, 3.40, 1.55},
	{2002, -21.97, 15.12, 1.61, 2.38},
	{2003, 28.36, 0.38, 1.01, 1.88},
	{2004, 10.74, 4.49, 1.37, 3.26},
	{2005, 4.83, 2.87, 3.15, 3.42},
	{2006, 15.61, 1.96, 4.73, 2.54},
	{2007, 5.48, 10.21, 4.36, 4.08},
	{2008, -36.55, 20.10, 1.37, 0.09},
	{2009, 25.94, -11.12, 0.15, 2.72},
	{2010, 14.82, 8.46, 0.14, 1.50},
	{2011, 2.10, 16.04, 0.05, 2.96},
	{2012, 15.89, 2.97, 0.09, 1.74},
	{2013, 32.15, -9.10, 0.06, 1.50},
	{2014, 13.52, 10.75, 0.03, 0.76},
	{2015, 1.38, 1.28, 0.05, 0.73},
	{2016, 11.77, 0.69, 0.32, 2.07},
	{2017, 21.61, 2.80, 0.93, 2.11},
	{2018, -4.23, 0.01, 1.94, 1.91},
	{2019, 31.21, 8.72, 2.06, 2.29},
	{2020, 18.02, 11.33, 0.35, 1.36},
	{2021, 28.47, -4.42, 0.05, 7.04},
	{2022, -18.04, -17.83, 2.02, 6.45},
	{2023, 26.06, 3.88, 5.07, 3.35},
	{2024, 24.89, -2.10, 4.97, 2.75},
}

// GetHistoricalReturns returns all historical data
func GetHistoricalReturns() []HistoricalYear {
	return HistoricalReturns
}

// GetHistoricalSequence returns a slice of historical years starting from a given year
// Returns nil if the starting year is not available or there isn't enough data
func GetHistoricalSequence(startYear int, yearsNeeded int) []HistoricalYear {
	startIdx := -1
	for i, year := range HistoricalReturns {
		if year.Year == startYear {
			startIdx = i
			break
		}
	}

	if startIdx < 0 || startIdx+yearsNeeded > len(HistoricalReturns) {
		return nil
	}

	return HistoricalReturns[startIdx : startIdx+yearsNeeded]
}

// GetAvailableStartYears returns the years that can be used as starting points for backtesting
// given the required projection length
func GetAvailableStartYears(projectionYears int) []int {
	maxIdx := len(HistoricalReturns) - projectionYears
	if maxIdx <= 0 {
		return nil
	}

	years := make([]int, maxIdx)
	for i := 0; i < maxIdx; i++ {
		years[i] = HistoricalReturns[i].Year
	}
	return years
}

// GetHistoricalStats returns summary statistics for the historical data
func GetHistoricalStats() (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	n := float64(len(HistoricalReturns))
	if n == 0 {
		return
	}

	// Calculate means
	var sumStock, sumBond, sumCash, sumInflation float64
	for _, year := range HistoricalReturns {
		sumStock += year.SP500Return
		sumBond += year.BondReturn
		sumCash += year.CashReturn
		sumInflation += year.InflationRate
	}

	avgStock = sumStock / n
	avgBond = sumBond / n
	avgCash = sumCash / n
	avgInflation = sumInflation / n

	// Calculate standard deviations
	var sumSqDiffStock, sumSqDiffBond float64
	for _, year := range HistoricalReturns {
		diffStock := year.SP500Return - avgStock
		sumSqDiffStock += diffStock * diffStock
		diffBond := year.BondReturn - avgBond
		sumSqDiffBond += diffBond * diffBond
	}
	stockStdDev = math.Sqrt(sumSqDiffStock / n)
	bondStdDev = math.Sqrt(sumSqDiffBond / n)

	return
}
