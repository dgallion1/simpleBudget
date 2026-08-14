// Package metrics derives dashboard KPIs and trend sparklines from a
// TransactionSet — totals, savings rate, monthly buckets, and budget
// deltas. Pure functions over models with no I/O.
package metrics

import (
	"math"
	"sort"
	"time"

	"budget2/internal/models"
)

// avgDaysPerMonth is 365.25 / 12 — the standard average-calendar-month length.
const avgDaysPerMonth = 30.4375

// HealthInsuranceCategory is the canonical category name used to
// identify health-insurance premium transactions for the dashboard
// Healthcare KPI. Matches what bank/credit-card CSVs export.
const HealthInsuranceCategory = "Health Insurance"

// MonthsBetween returns the average-calendar-month count between two
// inclusive dates. A single-day span returns 1/avgDaysPerMonth (~0.033),
// never zero, so callers can safely divide by the result.
func MonthsBetween(start, end time.Time) float64 {
	days := end.Sub(start).Hours()/24 + 1
	if days < 1 {
		days = 1
	}
	return days / avgDaysPerMonth
}

// currentHealthcareTarget returns the active monthly healthcare
// premium budget pulled from what-if. Uses GetTotalHealthcareCost(0)
// — month 0 represents "today's" planned premium, which is what the
// dashboard compares against recent "Health Insurance" transactions.
// Healthcare is intentionally NOT phase-multiplied (the calculator
// does not apply spending phases to healthcare, since costs tend to
// rise with age rather than fall).
//
// Returns 0 when settings is nil or no healthcare is configured so
// callers can rely on that as the "no healthcare budget" sentinel.
func currentHealthcareTarget(s *models.WhatIfSettings) float64 {
	if s == nil {
		return 0
	}
	return s.GetTotalHealthcareCost(0)
}

// phaseAdjustedMonthlyTarget returns the phase-adjusted monthly living
// expense target averaged across [rangeStart, rangeEnd]. When phases
// are disabled or unavailable, returns settings.MonthlyLivingExpenses
// unchanged. When phases are enabled, each calendar month in the range
// contributes its own multiplier so a range that straddles a phase
// transition (e.g., crossing the 65th-birthday "Active" cutoff)
// produces a weighted average.
//
// Returns 0 when settings is nil or MonthlyLivingExpenses is zero so
// callers can rely on that as the "no budget configured" sentinel.
func phaseAdjustedMonthlyTarget(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) float64 {
	if s == nil || s.MonthlyLivingExpenses <= 0 {
		return 0
	}
	base := s.MonthlyLivingExpenses
	if s.SpendingPhaseConfig == nil || !s.SpendingPhaseConfig.Enabled || len(s.SpendingPhaseConfig.Phases) == 0 {
		return base
	}

	cur := time.Date(rangeStart.Year(), rangeStart.Month(), 1, 0, 0, 0, 0, rangeStart.Location())
	end := time.Date(rangeEnd.Year(), rangeEnd.Month(), 1, 0, 0, 0, 0, rangeEnd.Location())
	if end.Before(cur) {
		return base * s.SpendingMultiplierAt(cur)
	}

	var sum float64
	count := 0
	for !cur.After(end) {
		sum += s.SpendingMultiplierAt(cur)
		count++
		cur = cur.AddDate(0, 1, 0)
	}
	if count == 0 {
		return base
	}
	return base * (sum / float64(count))
}

// BudgetTargets returns the monthly living-expense and healthcare targets a
// plan implies over the given window. Both are zero when settings is nil,
// which callers read as "no target set" -- the same meaning the dashboard's
// hasBudgetTarget/hasHealthcareTarget flags carry.
func BudgetTargets(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) (living, healthcare float64) {
	if s == nil {
		return 0, 0
	}
	return phaseAdjustedMonthlyTarget(s, rangeStart, rangeEnd), currentHealthcareTarget(s)
}

func Calculate(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, budgetTarget, healthcareTarget float64) *models.DashboardMetrics {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	totalIncome := income.SumAmount()
	totalExpenses := math.Abs(outflows.SumAmount())
	netSavings := totalIncome - totalExpenses

	var savingsRate float64
	if totalIncome > 0 {
		savingsRate = (netSavings / totalIncome) * 100
	}

	// Budget tracking — uses the dashboard date range (not transaction min/max)
	// so a sparse range still divides expenses across the full window the user
	// selected.
	//
	// Healthcare premiums are tracked by their own KPI below, so they are
	// subtracted from the living-expenses figure used for the Monthly
	// Living Expenses card and the Budget cumulative variance. Without
	// this split, premium spend would be counted in both cards and the
	// living-vs-target variance would silently include non-living costs.
	monthsInRange := MonthsBetween(rangeStart, rangeEnd)

	healthcareOutflows := outflows.FilterByCategory(HealthInsuranceCategory)
	healthcareTotal := math.Abs(healthcareOutflows.SumAmount())
	healthcareActual := healthcareTotal / monthsInRange
	healthcarePerMonthDelta := healthcareActual - healthcareTarget
	healthcareCumulativeDelta := healthcareTotal - healthcareTarget*monthsInRange
	hasHealthcareTarget := healthcareTarget > 0

	livingTotal := totalExpenses - healthcareTotal
	actualMonthly := livingTotal / monthsInRange
	perMonthDelta := actualMonthly - budgetTarget
	cumulativeDelta := livingTotal - budgetTarget*monthsInRange
	hasBudgetTarget := budgetTarget > 0

	// Combined plan variance — single number that nets Living + Healthcare
	// against their summed targets. Drives the Budget KPI card so a category
	// being under can offset another being over.
	combinedTarget := budgetTarget + healthcareTarget
	combinedActualMonthly := actualMonthly + healthcareActual
	combinedPerMonthDelta := combinedActualMonthly - combinedTarget
	combinedCumulativeDelta := (livingTotal + healthcareTotal) - combinedTarget*monthsInRange
	hasCombinedTarget := combinedTarget > 0

	// Calculate monthly trends
	var incomeTrend, expensesTrend, savingsTrend, healthcareTrend, livingTrend []float64
	var trendLabels []string

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()
	monthlyHealthcare := healthcareOutflows.GroupByMonth()

	// Get sorted months
	monthSet := make(map[string]bool)
	for m := range monthlyIncome {
		monthSet[m] = true
	}
	for m := range monthlyOutflows {
		monthSet[m] = true
	}

	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	// Take last 6 months
	if len(months) > 6 {
		months = months[len(months)-6:]
	}

	var combinedCumulativeBalance []float64
	var runningCombinedBalance float64
	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = math.Abs(hc.SumAmount())
		}

		livingMonth := expAmt - hcAmt

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		healthcareTrend = append(healthcareTrend, hcAmt)
		livingTrend = append(livingTrend, livingMonth)
		trendLabels = append(trendLabels, m)

		if hasCombinedTarget {
			runningCombinedBalance += combinedTarget - (livingMonth + hcAmt)
			combinedCumulativeBalance = append(combinedCumulativeBalance, runningCombinedBalance)
		}
	}

	return &models.DashboardMetrics{
		TotalIncome:               totalIncome,
		TotalExpenses:             totalExpenses,
		NetSavings:                netSavings,
		SavingsRate:               savingsRate,
		TransactionCount:          ts.Len(),
		StartDate:                 ts.MinDate(),
		EndDate:                   ts.MaxDate(),
		IncomeTrend:               incomeTrend,
		ExpensesTrend:             expensesTrend,
		SavingsTrend:              savingsTrend,
		TrendLabels:               trendLabels,
		MonthsInRange:             monthsInRange,
		LivingExpensesTotal:       livingTotal,
		ActualMonthly:             actualMonthly,
		BudgetTarget:              budgetTarget,
		PerMonthDelta:             perMonthDelta,
		CumulativeDelta:           cumulativeDelta,
		HasBudgetTarget:           hasBudgetTarget,
		LivingExpensesTrend:       livingTrend,
		HealthcareActual:          healthcareActual,
		HealthcareTotal:           healthcareTotal,
		HealthcareTarget:          healthcareTarget,
		HealthcarePerMonthDelta:   healthcarePerMonthDelta,
		HealthcareCumulativeDelta: healthcareCumulativeDelta,
		HasHealthcareTarget:       hasHealthcareTarget,
		HealthcareTrend:           healthcareTrend,
		CombinedTarget:            combinedTarget,
		CombinedActualMonthly:     combinedActualMonthly,
		CombinedPerMonthDelta:     combinedPerMonthDelta,
		CombinedCumulativeDelta:   combinedCumulativeDelta,
		HasCombinedTarget:         hasCombinedTarget,
		LivingTargetTotal:         budgetTarget * monthsInRange,
		HealthcareTargetTotal:     healthcareTarget * monthsInRange,
		CombinedCumulativeBalance: combinedCumulativeBalance,
	}
}

func Comparison(data *models.TransactionSet, start, end time.Time, compType string, settings *models.WhatIfSettings) *models.PeriodComparison {
	duration := end.Sub(start)

	var compStart, compEnd time.Time

	switch compType {
	case "previous":
		compEnd = start.Add(-24 * time.Hour) // Day before start
		compStart = compEnd.Add(-duration)
	case "year":
		compStart = start.AddDate(-1, 0, 0)
		compEnd = end.AddDate(-1, 0, 0)
	default:
		return nil
	}

	currentFiltered := data.Active().FilterByDateRange(start, end)
	compFiltered := data.Active().FilterByDateRange(compStart, compEnd)

	if compFiltered.Len() == 0 {
		return &models.PeriodComparison{HasData: false}
	}

	// Phase-adjust the target for each range independently — comparison
	// windows can sit in different phases (e.g., "year ago" was Go-Go,
	// current is Active), and a flat target would hide that effect.
	// Healthcare target is not phase-adjusted (uses today's premium).
	currentTarget := phaseAdjustedMonthlyTarget(settings, start, end)
	compTarget := phaseAdjustedMonthlyTarget(settings, compStart, compEnd)
	healthTarget := currentHealthcareTarget(settings)
	currentMetrics := Calculate(currentFiltered, start, end, currentTarget, healthTarget)
	compMetrics := Calculate(compFiltered, compStart, compEnd, compTarget, healthTarget)

	incomeChange := PercentChange(currentMetrics.TotalIncome, compMetrics.TotalIncome)
	expensesChange := PercentChange(currentMetrics.TotalExpenses, compMetrics.TotalExpenses)
	savingsChange := PercentChange(currentMetrics.NetSavings, compMetrics.NetSavings)
	savingsRateChange := currentMetrics.SavingsRate - compMetrics.SavingsRate

	return &models.PeriodComparison{
		Current:               currentMetrics,
		Previous:              compMetrics,
		HasData:               true,
		IncomeChange:          incomeChange,
		ExpensesChange:        expensesChange,
		SavingsChange:         savingsChange,
		SavingsRateChange:     savingsRateChange,
		ActualMonthlyChange:   currentMetrics.ActualMonthly - compMetrics.ActualMonthly,
		CumulativeDeltaChange: currentMetrics.CumulativeDelta - compMetrics.CumulativeDelta,
	}
}

func PercentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return ((current - previous) / math.Abs(previous)) * 100
}
