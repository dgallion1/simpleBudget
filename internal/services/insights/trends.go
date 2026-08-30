package insights

import (
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
)

// MajorExpenseTrends groups outflows by matched MajorExpense.Name
// for the current and previous periods and returns the same CategoryTrend
// shape as CategoryTrends so existing UI can render it. Unmatched
// transactions are intentionally excluded — the trend list is meant to
// surface movement on what the user has declared important. Pins win
// over keyword/amount matching, mirroring the rest of the engine.
func MajorExpenseTrends(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string, currentStart, currentEnd time.Time) []models.CategoryTrend {
	if ts == nil || len(defs) == 0 {
		return nil
	}

	defByID := make(map[string]models.MajorExpense, len(defs))
	for _, d := range defs {
		defByID[d.ID] = d
	}

	duration := currentEnd.Sub(currentStart)
	prevStart := currentStart.Add(-duration - 24*time.Hour)
	prevEnd := currentStart.Add(-24 * time.Hour)

	sumByExpense := func(window *models.TransactionSet) map[string]float64 {
		totals := make(map[string]float64)
		for _, t := range window.FilterByType(models.Outflow).Transactions {
			// Pin wins. StableID first, legacy content hash second.
			if id, _, ok := models.ResolveByIdentity(pins, t); ok {
				if def, exists := defByID[id]; exists {
					totals[def.Name] += math.Abs(t.Amount)
					continue
				}
			}
			if id, ok := majorexpenses.MatchTransaction(t, defs); ok {
				totals[defByID[id].Name] += math.Abs(t.Amount)
			}
		}
		return totals
	}

	currentTotals := sumByExpense(ts.FilterByDateRange(currentStart, currentEnd))
	prevTotals := sumByExpense(ts.FilterByDateRange(prevStart, prevEnd))

	nameSet := make(map[string]bool)
	for n := range currentTotals {
		nameSet[n] = true
	}
	for n := range prevTotals {
		nameSet[n] = true
	}

	var trends []models.CategoryTrend
	for name := range nameSet {
		current := currentTotals[name]
		previous := prevTotals[name]

		var changePercent float64
		var direction string
		if previous == 0 {
			if current == 0 {
				changePercent = 0
				direction = "stable"
			} else {
				changePercent = 100
				direction = "up"
			}
		} else {
			changePercent = ((current - previous) / previous) * 100
			switch {
			case changePercent > 5:
				direction = "up"
			case changePercent < -5:
				direction = "down"
			default:
				direction = "stable"
			}
		}

		trends = append(trends, models.CategoryTrend{
			Category:       name,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  changePercent,
			ChangeAmount:   current - previous,
			Direction:      direction,
		})
	}

	sort.Slice(trends, func(i, j int) bool {
		return math.Abs(trends[i].ChangeAmount) > math.Abs(trends[j].ChangeAmount)
	})

	if len(trends) > 10 {
		trends = trends[:10]
	}

	return trends
}

func CategoryTrends(ts *models.TransactionSet, currentStart, currentEnd time.Time) []models.CategoryTrend {
	var trends []models.CategoryTrend

	duration := currentEnd.Sub(currentStart)
	prevStart := currentStart.Add(-duration - 24*time.Hour)
	prevEnd := currentStart.Add(-24 * time.Hour)

	currentFiltered := ts.FilterByDateRange(currentStart, currentEnd)
	prevFiltered := ts.FilterByDateRange(prevStart, prevEnd)

	currentOutflows := currentFiltered.FilterByType(models.Outflow)
	prevOutflows := prevFiltered.FilterByType(models.Outflow)

	currentTotals := currentOutflows.CategoryTotals()
	prevTotals := prevOutflows.CategoryTotals()

	catSet := make(map[string]bool)
	for cat := range currentTotals {
		catSet[cat] = true
	}
	for cat := range prevTotals {
		catSet[cat] = true
	}

	for cat := range catSet {
		current := currentTotals[cat]
		previous := prevTotals[cat]

		var changePercent float64
		var direction string

		if previous == 0 {
			if current == 0 {
				changePercent = 0
				direction = "stable"
			} else {
				changePercent = 100
				direction = "up"
			}
		} else {
			changePercent = ((current - previous) / previous) * 100
			if changePercent > 5 {
				direction = "up"
			} else if changePercent < -5 {
				direction = "down"
			} else {
				direction = "stable"
			}
		}

		trends = append(trends, models.CategoryTrend{
			Category:       cat,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  changePercent,
			ChangeAmount:   current - previous,
			Direction:      direction,
		})
	}

	sort.Slice(trends, func(i, j int) bool {
		return math.Abs(trends[i].ChangeAmount) > math.Abs(trends[j].ChangeAmount)
	})

	if len(trends) > 10 {
		trends = trends[:10]
	}

	return trends
}

// IncomePatterns detects recurring income sources from transaction data.
// Exported for use by other packages (e.g., whatif).
func IncomePatterns(ts *models.TransactionSet) []models.IncomePattern {
	var patterns []models.IncomePattern

	income := ts.FilterByType(models.Income)
	if income.Len() < 2 {
		return patterns
	}

	groups := make(map[string][]models.Transaction)
	for _, t := range income.Transactions {
		key := strings.ToLower(strings.TrimSpace(t.Description))
		groups[key] = append(groups[key], t)
	}

	for desc, txns := range groups {
		if len(txns) < 2 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var total float64
		for _, t := range txns {
			total += t.Amount
		}
		avg := total / float64(len(txns))

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		var frequency string
		isRegular := false

		if len(intervals) > 0 {
			sortedIntervals := make([]float64, len(intervals))
			copy(sortedIntervals, intervals)
			sort.Float64s(sortedIntervals)
			medianInterval := sortedIntervals[len(sortedIntervals)/2]

			var sumSq float64
			for _, interval := range intervals {
				diff := interval - medianInterval
				sumSq += diff * diff
			}
			stdDev := math.Sqrt(sumSq / float64(len(intervals)))

			switch {
			case medianInterval >= 5 && medianInterval <= 9 && stdDev < 3:
				frequency = "weekly"
				isRegular = true
			case medianInterval >= 12 && medianInterval <= 16 && stdDev < 4:
				frequency = "biweekly"
				isRegular = true
			case medianInterval >= 25 && medianInterval <= 35 && stdDev < 7:
				frequency = "monthly"
				isRegular = true
			default:
				frequency = "irregular"
				isRegular = false
			}
		} else {
			frequency = "one-time"
		}

		patterns = append(patterns, models.IncomePattern{
			Description: desc,
			AvgAmount:   avg,
			Frequency:   frequency,
			IsRegular:   isRegular,
			Occurrences: len(txns),
			TotalAmount: total,
			LastDate:    txns[len(txns)-1].Date,
		})
	}

	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].TotalAmount > patterns[j].TotalAmount
	})

	if len(patterns) > 10 {
		patterns = patterns[:10]
	}

	return patterns
}

func SpendingVelocity(currentPeriod, allData *models.TransactionSet) *models.SpendingVelocity {
	currentOutflows := currentPeriod.FilterByType(models.Outflow)
	allOutflows := allData.FilterByType(models.Outflow)

	if currentOutflows.Len() == 0 {
		return &models.SpendingVelocity{}
	}

	currentMin := currentPeriod.MinDate()
	currentMax := currentPeriod.MaxDate()
	currentDays := currentMax.Sub(currentMin).Hours()/24 + 1
	if currentDays < 1 {
		currentDays = 1
	}
	dailyAvg := math.Abs(currentOutflows.SumAmount()) / currentDays

	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	historicalDaily := math.Abs(allOutflows.SumAmount()) / allDays

	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	dayOfMonth := now.Day()
	daysRemaining := daysInMonth - dayOfMonth

	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	currentMonthData := currentPeriod.FilterByDateRange(currentMonthStart, now)
	currentMonthOutflows := currentMonthData.FilterByType(models.Outflow)
	spentSoFar := math.Abs(currentMonthOutflows.SumAmount())

	monthProjection := spentSoFar + (dailyAvg * float64(daysRemaining))

	var burnRateChange float64
	if historicalDaily > 0 {
		burnRateChange = ((dailyAvg - historicalDaily) / historicalDaily) * 100
	}

	return &models.SpendingVelocity{
		DailyAverage:    dailyAvg,
		HistoricalDaily: historicalDaily,
		MonthProjection: monthProjection,
		DaysRemaining:   daysRemaining,
		BurnRateChange:  burnRateChange,
	}
}
