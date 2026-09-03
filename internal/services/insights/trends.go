package insights

import (
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
)

// ChangeDollarFloor is the ONE named threshold (U5) below which a
// period-over-period Change is shown as a signed dollar delta instead of a
// percent -- a tiny previous-period baseline makes any percent (however
// computed) misleading (e.g. $30 -> $6,931 reads "+23004.0%"). See
// ChangeDisplay, the single function every Change-rendering surface must
// go through.
const ChangeDollarFloor = 100.0

// ChangeDisplay is THE ONE classifier (U5 contract v3, SPEC §2d rule 2,
// after two same-class FAILs: attempt 1 derived the dollar delta from
// unrounded floats; attempt 2 rounded inside ChangeDisplay but the
// producer still classified and computed percent/direction from its OWN
// raw floats -- a split classification that let a float-noise sum
// (0.10+0.20-0.30 ~= 5e-17) render "$0.00 -> $0.00, +100.0%, up"). This
// function now owns EVERY decision derived from the pair -- Kind, the
// dollar Amount, the Percent, and the Direction -- so there is exactly one
// place that can disagree with what the user sees. Producers do nothing
// but call this and copy its fields onto the row (Rule 2: "delete the
// producers' own percent/direction computation entirely").
//
// previous and current are rounded to cents FIRST (models.RoundToCents --
// the exact fmt.Sprintf("%.2f") primitive formatMoney itself uses), and
// every downstream value (change, pct, Kind, Direction) is derived from
// THAT rounded pair, never the raw inputs. Callers are also expected to
// round at the source (Rule 1) before storing CurrentAmount/PreviousAmount
// on the row; rounding again here is idempotent and makes this function
// correct on its own regardless of what a caller passes.
//
//   - previous == 0 && current > 0 -> Kind "new": no prior spend to
//     compare against.
//   - previous == 0 && current == 0 -> Kind "none": truly no activity
//     either period (the float-noise-sum case above lands here once
//     rounded) -- Direction is always "stable".
//   - (previous != 0 && |previous| < ChangeDollarFloor) || (previous == 0
//     && current < 0) -> Kind "dollar": either previous is nonzero but too
//     small for a percent to mean anything, or there is no prior baseline
//     and the whole period is a net refund (current < 0) -- a percent
//     against a zero baseline is not just misleading, it is undefined.
//     Amount is current-previous (SIGNED, rendered via formatMoney with a
//     leading "+" added by the template for positive values).
//   - otherwise -> Kind "percent": pct = change / |previous| * 100, signed,
//     one decimal, exactly zero previous excluded by the branches above.
//
// pct itself: previous == 0 sets it to +100 (current > 0), -100
// (current < 0), or 0 (current == 0, i.e. the "none" case) -- matching the
// existing zero-baseline convention MCP callers already depend on: else
// change / |previous| * 100. Percent is ALWAYS populated (even for
// "new"/"none"/"dollar" rows) so MCP's change_percent has one source
// regardless of which text the web UI shows. Amount is likewise ALWAYS
// populated. Direction follows pct with the existing +-5 band.
func ChangeDisplay(previous, current float64) models.ChangeCell {
	previous = models.RoundToCents(previous)
	current = models.RoundToCents(current)
	change := models.RoundToCents(current - previous)

	var pct float64
	switch {
	case previous == 0 && current > 0:
		pct = 100
	case previous == 0 && current < 0:
		pct = -100
	case previous == 0:
		pct = 0
	default:
		pct = change / math.Abs(previous) * 100
	}

	var kind, text string
	switch {
	case previous == 0 && current > 0:
		kind, text = models.ChangeKindNew, "new"
	case previous == 0 && current == 0:
		kind, text = models.ChangeKindNone, "—"
	case (previous != 0 && math.Abs(previous) < ChangeDollarFloor) || (previous == 0 && current < 0):
		kind = models.ChangeKindDollar
	default:
		kind = models.ChangeKindPercent
	}

	direction := "stable"
	switch {
	case pct > 5:
		direction = "up"
	case pct < -5:
		direction = "down"
	}

	return models.ChangeCell{Kind: kind, Text: text, Amount: change, Percent: pct, Direction: direction}
}

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
		// Rule 1 (SPEC §2d contract v3): round at the source. MajorExpenseTrends'
		// totals are SIGNED (CB3-D) float sums -- a category with no activity in
		// a period, or a period whose signed transactions cancel out (e.g.
		// 0.10+0.20-0.30), can land on a float-noise value like 5.55e-17 instead
		// of exactly 0. Rounding immediately after summation, before ANYTHING
		// downstream sees these totals, means "no activity" reads as exactly
		// $0.00 everywhere (producer, MCP, template) rather than tripping
		// ChangeDisplay's previous==0/current==0 comparison on noise.
		current := models.RoundToCents(currentTotals[name])
		previous := models.RoundToCents(prevTotals[name])

		// Rule 2: ONE classifier owns change, percent, and direction --
		// CategoryTrend's own fields are copied FROM the cell, never computed
		// independently (that split is exactly what let attempt 2's producer
		// disagree with its own rounded ChangeDisplay call).
		cell := ChangeDisplay(previous, current)

		trends = append(trends, models.CategoryTrend{
			Category:       name,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  cell.Percent,
			ChangeAmount:   cell.Amount,
			Direction:      cell.Direction,
			Change:         cell,
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
		// Rule 1: round at the source. CategoryTotals() sums are non-negative
		// (Abs-based) so float noise is rarer here than in MajorExpenseTrends,
		// but the contract is "no unrounded money leaves a producer" -- always
		// round, not only when it happens to matter for a given category.
		current := models.RoundToCents(currentTotals[cat])
		previous := models.RoundToCents(prevTotals[cat])

		// Rule 2: ONE classifier owns change, percent, and direction.
		cell := ChangeDisplay(previous, current)

		trends = append(trends, models.CategoryTrend{
			Category:       cat,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  cell.Percent,
			ChangeAmount:   cell.Amount,
			Direction:      cell.Direction,
			Change:         cell,
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
	dailyAvg := -currentOutflows.SumAmount() / currentDays

	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	// Signed net over the whole ledger (CB3-D), same contract as dailyAvg.
	historicalDaily := -allOutflows.SumAmount() / allDays

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
	spentSoFar := -currentMonthOutflows.SumAmount()

	monthProjection := spentSoFar + (dailyAvg * float64(daysRemaining))

	// CB3-D downstream trace: historicalDaily can now be negative (a
	// refund-dominant full ledger). The `> 0` guard already here (needed
	// even before CB3-D, for a zero-outflow ledger) also covers that case:
	// burnRateChange is left at its zero value rather than dividing by a
	// negative baseline. This is a pre-existing guarded degradation, not
	// new misbehavior -- no crash, no NaN, no sign inversion, just an
	// unreported change stat when the baseline itself was net-refund.
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
