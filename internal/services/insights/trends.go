package insights

import (
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/metrics"
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
			// Signed net per period (CB3-D): matches CB3-A's drilldown
			// contract (Total = -(signed sum)) and bucketMajorExpenses'
			// list-row total. Refunds (positive Outflow amounts per
			// classifier convention) reduce the period total instead of
			// inflating it via AbsAmount.
			if id, _, ok := models.ResolveByIdentity(pins, t); ok {
				if def, exists := defByID[id]; exists {
					totals[def.Name] -= t.Amount
					continue
				}
			}
			if id, ok := majorexpenses.MatchTransaction(t, defs); ok {
				totals[defByID[id].Name] -= t.Amount
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
		change := current - previous

		// CB3-c: MajorExpenseTrends' totals are now SIGNED (CB3-D), so the
		// old flat "previous==0 -> +100/up" and the signed-previous
		// denominator both broke: a refund-dominant swing (e.g.
		// current=0, previous=-628 -> change=+628) used to divide by the
		// SIGNED previous and land on changePercent=-100/"down", directly
		// contradicting a positive change. Fixed the same way CB3-E
		// documents for PercentChange: divide by |previous| so the result's
		// sign always tracks change's sign, and pick previous==0's
		// changePercent by the SIGN OF CHANGE (not a hardcoded +100)
		// so it agrees too. Direction then derives from changePercent
		// using the EXISTING +-5 stable band, unchanged. CategoryTrends
		// (a separate function, abs-based, out of CB3 scope) keeps its own
		// classifier untouched.
		var changePercent float64
		if previous == 0 {
			switch {
			case change > 0:
				changePercent = 100
			case change < 0:
				changePercent = -100
			default:
				changePercent = 0
			}
		} else {
			changePercent = (change / math.Abs(previous)) * 100
		}

		var direction string
		switch {
		case changePercent > 5:
			direction = "up"
		case changePercent < -5:
			direction = "down"
		default:
			direction = "stable"
		}

		trends = append(trends, models.CategoryTrend{
			Category:       name,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  changePercent,
			ChangeAmount:   change,
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
	// Signed period net (CB3-D): outflows are negative and refunds
	// (positive Outflow amounts per classifier convention) are credits,
	// so -SumAmount() is positive spend, same as the old math.Abs. A
	// refund-dominant period yields a NEGATIVE daily average -- honest;
	// see burnRateChange's guard below for how a negative historicalDaily
	// is handled downstream.
	// Routed through metrics.SignedNet (CB9): a window whose outflows cancel
	// exactly must yield +0, not IEEE -0, which json.Marshal emits as "-0".
	dailyAvg := metrics.SignedNet(currentOutflows) / currentDays

	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	// Signed net over the whole ledger (CB3-D), same contract as dailyAvg.
	historicalDaily := metrics.SignedNet(allOutflows) / allDays

	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	dayOfMonth := now.Day()
	daysRemaining := daysInMonth - dayOfMonth

	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	currentMonthData := currentPeriod.FilterByDateRange(currentMonthStart, now)
	currentMonthOutflows := currentMonthData.FilterByType(models.Outflow)
	// Signed net for the current calendar month (CB3-D), same contract as
	// dailyAvg; monthProjection below inherits the sign (a refund-dominant
	// month-to-date projects a negative total -- honest, not a division).
	spentSoFar := metrics.SignedNet(currentMonthOutflows)

	monthProjection := spentSoFar + (dailyAvg * float64(daysRemaining))

	// CB8 (ruling CB8-2026-09-03a): CB3-D made historicalDaily signed, but
	// left the `> 0` guard, so a ledger whose entire history nets a refund
	// (historicalDaily < 0) reported burnRateChange=0 regardless of the
	// current pace -- silently hiding a real acceleration. Fixed the same
	// way CB3-c fixed MajorExpenseTrends' changePercent: divide by
	// |historicalDaily| so the sign of the result ALWAYS tracks the sign
	// of the change (spending faster than history -> positive, never
	// inverted by a negative base), and for a historicalDaily of exactly
	// zero, pick the result by the SIGN OF CHANGE rather than leaving it
	// at (or hardcoding it to) a flat value -- do not call
	// metrics.PercentChange here, whose zero-base case is an unconditional
	// +100 and would misreport a slowdown from a zero baseline as growth.
	// For an ordinary positive historicalDaily this is arithmetically
	// identical to the pre-CB8 formula (dividing by historicalDaily itself
	// vs. |historicalDaily| is the same number when historicalDaily > 0).
	change := dailyAvg - historicalDaily
	var burnRateChange float64
	switch {
	case historicalDaily != 0:
		burnRateChange = change / math.Abs(historicalDaily) * 100
	case change > 0:
		burnRateChange = 100
	case change < 0:
		burnRateChange = -100
	default:
		burnRateChange = 0
	}

	return &models.SpendingVelocity{
		DailyAverage:    dailyAvg,
		HistoricalDaily: historicalDaily,
		MonthProjection: monthProjection,
		DaysRemaining:   daysRemaining,
		BurnRateChange:  burnRateChange,
	}
}
