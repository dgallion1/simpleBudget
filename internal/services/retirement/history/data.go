// Package history exposes historical market data used by backtest
// analyses. It is a leaf package — only models is imported.
package history

import (
	"math"

	"budget2/internal/models"
)

// Data is a sequence of annual market data points.
type Data []models.HistoricalYear

// DefaultData returns the canonical historical dataset.
func DefaultData() Data {
	return defaultData
}

// Sequence returns yearsNeeded years starting from startYear. Returns
// nil if the starting year is not present in data or if the requested
// horizon runs past the end of the dataset.
func Sequence(data Data, startYear, yearsNeeded int) Data {
	startIdx := -1
	for i, year := range data {
		if year.Year == startYear {
			startIdx = i
			break
		}
	}

	if startIdx < 0 || startIdx+yearsNeeded > len(data) {
		return nil
	}

	return data[startIdx : startIdx+yearsNeeded]
}

// AvailableStartYears returns every starting year from which a sequence
// of projectionYears length is available. F-057: inclusive upper bound
// — for an N-year horizon the last viable start year is
// (lastYear - N + 1).
func AvailableStartYears(data Data, projectionYears int) []int {
	maxIdx := len(data) - projectionYears + 1
	if maxIdx <= 0 {
		return nil
	}

	years := make([]int, maxIdx)
	for i := 0; i < maxIdx; i++ {
		years[i] = data[i].Year
	}
	return years
}

// Stats computes aggregate statistics across the dataset (means and
// standard deviations for stocks and bonds).
func Stats(data Data) (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	n := float64(len(data))
	if n == 0 {
		return
	}

	// Calculate means
	var sumStock, sumBond, sumCash, sumInflation float64
	for _, year := range data {
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
	for _, year := range data {
		diffStock := year.SP500Return - avgStock
		sumSqDiffStock += diffStock * diffStock
		diffBond := year.BondReturn - avgBond
		sumSqDiffBond += diffBond * diffBond
	}
	stockStdDev = math.Sqrt(sumSqDiffStock / n)
	bondStdDev = math.Sqrt(sumSqDiffBond / n)

	return
}

// defaultData holds the canonical historical sequence (1928-2024).
//
// Sources:
//   - S&P 500: Shiller data (http://www.econ.yale.edu/~shiller/data.htm)
//   - Bonds: 10-Year Treasury (FRED)
//   - Cash: 3-Month T-Bill (NYU Stern / Damodaran)
//   - Inflation: BLS CPI-U
var defaultData = Data{
	// Year, S&P500, 10Y Bond, T-Bill, Inflation
	{Year: 1928, SP500Return: 43.81, BondReturn: 0.84, CashReturn: 3.08, InflationRate: -1.16},
	{Year: 1929, SP500Return: -8.30, BondReturn: 4.20, CashReturn: 3.16, InflationRate: 0.58},
	{Year: 1930, SP500Return: -25.12, BondReturn: 4.54, CashReturn: 4.55, InflationRate: -6.40},
	{Year: 1931, SP500Return: -43.84, BondReturn: -2.56, CashReturn: 2.31, InflationRate: -9.32},
	{Year: 1932, SP500Return: -8.64, BondReturn: 8.79, CashReturn: 1.07, InflationRate: -10.27},
	{Year: 1933, SP500Return: 49.98, BondReturn: 1.86, CashReturn: 0.96, InflationRate: 0.76},
	{Year: 1934, SP500Return: -1.19, BondReturn: 7.96, CashReturn: 0.28, InflationRate: 1.52},
	{Year: 1935, SP500Return: 46.74, BondReturn: 4.47, CashReturn: 0.17, InflationRate: 2.99},
	{Year: 1936, SP500Return: 31.94, BondReturn: 5.02, CashReturn: 0.17, InflationRate: 1.45},
	{Year: 1937, SP500Return: -35.34, BondReturn: 1.38, CashReturn: 0.28, InflationRate: 2.86},
	{Year: 1938, SP500Return: 29.28, BondReturn: 4.21, CashReturn: 0.07, InflationRate: -2.78},
	{Year: 1939, SP500Return: -1.10, BondReturn: 4.41, CashReturn: 0.05, InflationRate: 0.00},
	{Year: 1940, SP500Return: -10.67, BondReturn: 5.40, CashReturn: 0.04, InflationRate: 0.71},
	{Year: 1941, SP500Return: -12.77, BondReturn: -2.02, CashReturn: 0.13, InflationRate: 9.93},
	{Year: 1942, SP500Return: 19.17, BondReturn: 2.29, CashReturn: 0.34, InflationRate: 9.03},
	{Year: 1943, SP500Return: 25.06, BondReturn: 2.49, CashReturn: 0.38, InflationRate: 2.96},
	{Year: 1944, SP500Return: 19.03, BondReturn: 2.58, CashReturn: 0.38, InflationRate: 2.30},
	{Year: 1945, SP500Return: 35.82, BondReturn: 3.80, CashReturn: 0.38, InflationRate: 2.25},
	{Year: 1946, SP500Return: -8.43, BondReturn: 3.13, CashReturn: 0.38, InflationRate: 18.13},
	{Year: 1947, SP500Return: 5.20, BondReturn: 0.92, CashReturn: 0.60, InflationRate: 8.84},
	{Year: 1948, SP500Return: 5.70, BondReturn: 1.95, CashReturn: 1.05, InflationRate: 2.99},
	{Year: 1949, SP500Return: 18.30, BondReturn: 4.66, CashReturn: 1.12, InflationRate: -2.07},
	{Year: 1950, SP500Return: 30.81, BondReturn: 0.43, CashReturn: 1.20, InflationRate: 5.93},
	{Year: 1951, SP500Return: 23.68, BondReturn: -0.30, CashReturn: 1.52, InflationRate: 6.00},
	{Year: 1952, SP500Return: 18.15, BondReturn: 2.27, CashReturn: 1.72, InflationRate: 0.75},
	{Year: 1953, SP500Return: -1.21, BondReturn: 4.14, CashReturn: 1.89, InflationRate: 0.75},
	{Year: 1954, SP500Return: 52.56, BondReturn: 3.29, CashReturn: 0.94, InflationRate: -0.74},
	{Year: 1955, SP500Return: 32.60, BondReturn: -1.34, CashReturn: 1.72, InflationRate: 0.37},
	{Year: 1956, SP500Return: 7.44, BondReturn: -2.26, CashReturn: 2.62, InflationRate: 2.99},
	{Year: 1957, SP500Return: -10.46, BondReturn: 6.80, CashReturn: 3.22, InflationRate: 2.90},
	{Year: 1958, SP500Return: 43.72, BondReturn: -2.10, CashReturn: 1.77, InflationRate: 1.76},
	{Year: 1959, SP500Return: 12.06, BondReturn: -2.65, CashReturn: 3.39, InflationRate: 1.73},
	{Year: 1960, SP500Return: 0.34, BondReturn: 11.64, CashReturn: 2.87, InflationRate: 1.36},
	{Year: 1961, SP500Return: 26.64, BondReturn: 2.06, CashReturn: 2.35, InflationRate: 0.67},
	{Year: 1962, SP500Return: -8.81, BondReturn: 5.69, CashReturn: 2.77, InflationRate: 1.33},
	{Year: 1963, SP500Return: 22.61, BondReturn: 1.68, CashReturn: 3.16, InflationRate: 1.64},
	{Year: 1964, SP500Return: 16.42, BondReturn: 3.73, CashReturn: 3.55, InflationRate: 0.97},
	{Year: 1965, SP500Return: 12.40, BondReturn: 0.71, CashReturn: 3.95, InflationRate: 1.92},
	{Year: 1966, SP500Return: -9.97, BondReturn: 2.91, CashReturn: 4.86, InflationRate: 3.46},
	{Year: 1967, SP500Return: 23.80, BondReturn: -1.58, CashReturn: 4.29, InflationRate: 3.04},
	{Year: 1968, SP500Return: 10.81, BondReturn: 3.27, CashReturn: 5.34, InflationRate: 4.72},
	{Year: 1969, SP500Return: -8.24, BondReturn: -5.01, CashReturn: 6.67, InflationRate: 6.20},
	{Year: 1970, SP500Return: 3.56, BondReturn: 16.75, CashReturn: 6.39, InflationRate: 5.57},
	{Year: 1971, SP500Return: 14.22, BondReturn: 9.79, CashReturn: 4.33, InflationRate: 3.27},
	{Year: 1972, SP500Return: 18.76, BondReturn: 2.82, CashReturn: 4.06, InflationRate: 3.41},
	{Year: 1973, SP500Return: -14.31, BondReturn: 3.66, CashReturn: 7.04, InflationRate: 8.71},
	{Year: 1974, SP500Return: -25.90, BondReturn: 4.00, CashReturn: 7.85, InflationRate: 12.34},
	{Year: 1975, SP500Return: 37.00, BondReturn: 3.61, CashReturn: 5.79, InflationRate: 6.94},
	{Year: 1976, SP500Return: 23.83, BondReturn: 15.98, CashReturn: 4.98, InflationRate: 4.86},
	{Year: 1977, SP500Return: -6.98, BondReturn: 1.29, CashReturn: 5.26, InflationRate: 6.70},
	{Year: 1978, SP500Return: 6.51, BondReturn: -0.78, CashReturn: 7.18, InflationRate: 9.02},
	{Year: 1979, SP500Return: 18.52, BondReturn: 0.67, CashReturn: 10.05, InflationRate: 13.29},
	{Year: 1980, SP500Return: 31.74, BondReturn: -2.99, CashReturn: 11.39, InflationRate: 12.52},
	{Year: 1981, SP500Return: -4.70, BondReturn: 8.20, CashReturn: 14.04, InflationRate: 8.92},
	{Year: 1982, SP500Return: 20.42, BondReturn: 32.81, CashReturn: 10.60, InflationRate: 3.83},
	{Year: 1983, SP500Return: 22.34, BondReturn: 3.20, CashReturn: 8.62, InflationRate: 3.79},
	{Year: 1984, SP500Return: 6.15, BondReturn: 13.73, CashReturn: 9.54, InflationRate: 3.95},
	{Year: 1985, SP500Return: 31.24, BondReturn: 25.71, CashReturn: 7.47, InflationRate: 3.80},
	{Year: 1986, SP500Return: 18.49, BondReturn: 24.28, CashReturn: 5.97, InflationRate: 1.10},
	{Year: 1987, SP500Return: 5.81, BondReturn: -4.96, CashReturn: 5.78, InflationRate: 4.43},
	{Year: 1988, SP500Return: 16.54, BondReturn: 8.22, CashReturn: 6.67, InflationRate: 4.42},
	{Year: 1989, SP500Return: 31.48, BondReturn: 17.69, CashReturn: 8.11, InflationRate: 4.65},
	{Year: 1990, SP500Return: -3.06, BondReturn: 6.24, CashReturn: 7.50, InflationRate: 6.11},
	{Year: 1991, SP500Return: 30.23, BondReturn: 15.00, CashReturn: 5.38, InflationRate: 3.06},
	{Year: 1992, SP500Return: 7.49, BondReturn: 9.36, CashReturn: 3.43, InflationRate: 2.90},
	{Year: 1993, SP500Return: 9.97, BondReturn: 14.21, CashReturn: 3.00, InflationRate: 2.75},
	{Year: 1994, SP500Return: 1.33, BondReturn: -8.04, CashReturn: 4.25, InflationRate: 2.67},
	{Year: 1995, SP500Return: 37.20, BondReturn: 23.48, CashReturn: 5.49, InflationRate: 2.54},
	{Year: 1996, SP500Return: 22.68, BondReturn: 1.43, CashReturn: 5.01, InflationRate: 3.32},
	{Year: 1997, SP500Return: 33.10, BondReturn: 9.94, CashReturn: 5.06, InflationRate: 1.70},
	{Year: 1998, SP500Return: 28.34, BondReturn: 14.92, CashReturn: 4.78, InflationRate: 1.61},
	{Year: 1999, SP500Return: 20.89, BondReturn: -8.25, CashReturn: 4.64, InflationRate: 2.68},
	{Year: 2000, SP500Return: -9.03, BondReturn: 16.66, CashReturn: 5.82, InflationRate: 3.39},
	{Year: 2001, SP500Return: -11.85, BondReturn: 5.57, CashReturn: 3.40, InflationRate: 1.55},
	{Year: 2002, SP500Return: -21.97, BondReturn: 15.12, CashReturn: 1.61, InflationRate: 2.38},
	{Year: 2003, SP500Return: 28.36, BondReturn: 0.38, CashReturn: 1.01, InflationRate: 1.88},
	{Year: 2004, SP500Return: 10.74, BondReturn: 4.49, CashReturn: 1.37, InflationRate: 3.26},
	{Year: 2005, SP500Return: 4.83, BondReturn: 2.87, CashReturn: 3.15, InflationRate: 3.42},
	{Year: 2006, SP500Return: 15.61, BondReturn: 1.96, CashReturn: 4.73, InflationRate: 2.54},
	{Year: 2007, SP500Return: 5.48, BondReturn: 10.21, CashReturn: 4.36, InflationRate: 4.08},
	{Year: 2008, SP500Return: -36.55, BondReturn: 20.10, CashReturn: 1.37, InflationRate: 0.09},
	{Year: 2009, SP500Return: 25.94, BondReturn: -11.12, CashReturn: 0.15, InflationRate: 2.72},
	{Year: 2010, SP500Return: 14.82, BondReturn: 8.46, CashReturn: 0.14, InflationRate: 1.50},
	{Year: 2011, SP500Return: 2.10, BondReturn: 16.04, CashReturn: 0.05, InflationRate: 2.96},
	{Year: 2012, SP500Return: 15.89, BondReturn: 2.97, CashReturn: 0.09, InflationRate: 1.74},
	{Year: 2013, SP500Return: 32.15, BondReturn: -9.10, CashReturn: 0.06, InflationRate: 1.50},
	{Year: 2014, SP500Return: 13.52, BondReturn: 10.75, CashReturn: 0.03, InflationRate: 0.76},
	{Year: 2015, SP500Return: 1.38, BondReturn: 1.28, CashReturn: 0.05, InflationRate: 0.73},
	{Year: 2016, SP500Return: 11.77, BondReturn: 0.69, CashReturn: 0.32, InflationRate: 2.07},
	{Year: 2017, SP500Return: 21.61, BondReturn: 2.80, CashReturn: 0.93, InflationRate: 2.11},
	{Year: 2018, SP500Return: -4.23, BondReturn: 0.01, CashReturn: 1.94, InflationRate: 1.91},
	{Year: 2019, SP500Return: 31.21, BondReturn: 8.72, CashReturn: 2.06, InflationRate: 2.29},
	{Year: 2020, SP500Return: 18.02, BondReturn: 11.33, CashReturn: 0.35, InflationRate: 1.36},
	{Year: 2021, SP500Return: 28.47, BondReturn: -4.42, CashReturn: 0.05, InflationRate: 7.04},
	{Year: 2022, SP500Return: -18.04, BondReturn: -17.83, CashReturn: 2.02, InflationRate: 6.45},
	{Year: 2023, SP500Return: 26.06, BondReturn: 3.88, CashReturn: 5.07, InflationRate: 3.35},
	{Year: 2024, SP500Return: 24.89, BondReturn: -2.10, CashReturn: 4.97, InflationRate: 2.75},
}
