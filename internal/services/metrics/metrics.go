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

// phaseNameAt returns the spending-phase name active at calendar instant
// t, mirroring the "last phase with StartAge <= age" rule
// WhatIfSettings.GetSpendingMultiplier applies internally. That method
// (and SpendingMultiplierAt, its calendar-aware wrapper) only returns the
// numeric multiplier, not the phase name, and both are unexported/private
// logic inside the models package that this task may not modify -- so the
// age-resolution step (ParseYearMonth + GetPhaseReferenceAge, both
// exported by models) is necessarily re-derived here. This duplicates only
// that single age lookup, not the calendar walk itself, which lives solely
// in phaseWalk below. Returns "" when phases are disabled/unconfigured or
// StartDate can't be parsed.
func phaseNameAt(s *models.WhatIfSettings, t time.Time) string {
	config := s.SpendingPhaseConfig
	if config == nil || !config.Enabled || len(config.Phases) == 0 {
		return ""
	}
	sd, err := models.ParseYearMonth(s.StartDate)
	if err != nil {
		return ""
	}
	monthsFromStart := (t.Year()-sd.Year())*12 + int(t.Month()) - int(sd.Month())
	yearsElapsed := monthsFromStart / 12
	if monthsFromStart < 0 && monthsFromStart%12 != 0 {
		yearsElapsed--
	}
	age := s.GetPhaseReferenceAge(yearsElapsed)

	name := ""
	for _, phase := range config.Phases {
		if age >= phase.StartAge {
			name = phase.Name
		}
	}
	return name
}

// phaseWalk performs the single calendar-month walk over [rangeStart,
// rangeEnd] that both phaseAdjustedMonthlyTarget and TargetProvenance
// need: it visits each whole calendar month in the range, reads
// SpendingMultiplierAt for each, and reports the averaged multiplier
// alongside the phase name active at rangeStart and whether more than one
// distinct multiplier was seen (a straddled phase transition, where the
// averaged multiplier is a weighted average rather than one phase's flat
// value). Neither caller re-walks the range on its own.
//
// Precondition: callers gate on s.SpendingPhaseConfig being enabled with
// at least one phase before calling.
func phaseWalk(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) (avgMultiplier float64, phaseName string, straddles bool) {
	cur := time.Date(rangeStart.Year(), rangeStart.Month(), 1, 0, 0, 0, 0, rangeStart.Location())
	end := time.Date(rangeEnd.Year(), rangeEnd.Month(), 1, 0, 0, 0, 0, rangeEnd.Location())
	phaseName = phaseNameAt(s, cur)

	if end.Before(cur) {
		return s.SpendingMultiplierAt(cur), phaseName, false
	}

	var sum float64
	count := 0
	first := true
	var firstMult float64
	for m := cur; !m.After(end); m = m.AddDate(0, 1, 0) {
		mult := s.SpendingMultiplierAt(m)
		if first {
			firstMult = mult
			first = false
		} else if mult != firstMult {
			straddles = true
		}
		sum += mult
		count++
	}
	if count == 0 {
		// Unreachable: end >= cur (checked above) means the loop always
		// runs at least once. Kept as a defensive fallback matching the
		// pre-refactor behavior of returning the base unmultiplied
		// (multiplier 1.0).
		return 1.0, phaseName, false
	}
	return sum / float64(count), phaseName, straddles
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
	avgMultiplier, _, _ := phaseWalk(s, rangeStart, rangeEnd)
	return base * avgMultiplier
}

// BudgetTargetProvenance carries the "why" behind the phase-adjusted
// monthly living-expense target the dashboard shows as "Target $X" -- the
// unadjusted plan base, the multiplier actually applied over the range
// (read from the same phaseWalk that phaseAdjustedMonthlyTarget performs,
// not re-derived by dividing target/base), the phase active at the range
// start, and whether the range straddles a phase transition (in which
// case Multiplier is a weighted average across the walked months, not one
// phase's flat value).
//
// Annotate is false when there's nothing worth surfacing: nil settings,
// zero MonthlyLivingExpenses, phases disabled/unconfigured, or an
// effective multiplier of exactly 1.0 (the target equals the base, so
// noting a multiplier would only add noise).
type BudgetTargetProvenance struct {
	Base       float64
	Multiplier float64
	PhaseName  string
	Straddles  bool
	Annotate   bool
}

// TargetProvenance computes BudgetTargetProvenance for the living-expense
// target over [rangeStart, rangeEnd]. Shares phaseWalk with
// phaseAdjustedMonthlyTarget so there is exactly one phase-walk
// implementation, not two independently-maintained copies.
func TargetProvenance(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) BudgetTargetProvenance {
	if s == nil || s.MonthlyLivingExpenses <= 0 {
		return BudgetTargetProvenance{}
	}
	base := s.MonthlyLivingExpenses
	if s.SpendingPhaseConfig == nil || !s.SpendingPhaseConfig.Enabled || len(s.SpendingPhaseConfig.Phases) == 0 {
		return BudgetTargetProvenance{Base: base, Multiplier: 1.0}
	}
	mult, name, straddles := phaseWalk(s, rangeStart, rangeEnd)
	return BudgetTargetProvenance{
		Base:       base,
		Multiplier: mult,
		PhaseName:  name,
		Straddles:  straddles,
		Annotate:   mult != 1.0,
	}
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

// HealthcareCoverageStart returns the date of the EARLIEST outflow-typed,
// negative-amount transaction in category HealthInsuranceCategory across
// ts, and ok=false when no such transaction exists. Positive-amount rows
// (refunds/credits) never count -- coverage begins the day a real premium
// is actually paid, not when money moves back the other way. Callers must
// pass the app's full canonical (post duplicate-resolution) transaction
// set, never a range-filtered one: coverage start is a lifetime fact about
// the ledger, independent of whatever window the dashboard currently has
// selected. This is the single derivation every consumer of a healthcare
// coverage start goes through (split-classification rule, ruling
// 2026-08-29a).
func HealthcareCoverageStart(ts *models.TransactionSet) (start time.Time, ok bool) {
	if ts == nil {
		return time.Time{}, false
	}
	bills := ts.FilterByType(models.Outflow).FilterByCategory(HealthInsuranceCategory)
	for _, t := range bills.Transactions {
		if t.Amount >= 0 {
			continue
		}
		if !ok || t.Date.Before(start) {
			start = t.Date
			ok = true
		}
	}
	return start, ok
}

// ClippedHealthcareMonths is the single place a healthcare-target accrual
// is clipped to actual coverage: the average-calendar-month count of
// [segStart, segEnd] that falls on/after coverageStart. Returns 0 when
// hasCoverage is false, or when coverageStart is after segEnd (no covered
// day in the segment). Returns the segment's full MonthsBetween when
// coverageStart is at/before segStart (coverage predates the segment, so
// the segment is unclipped). Every site that multiplies a healthcare
// target by a month count -- metrics.Calculate's cumulative delta/target-
// total/per-segment balance walk, and the dashboard's budget-vs-actual
// chart -- goes through this one function (split-classification rule,
// ruling 2026-08-29a).
func ClippedHealthcareMonths(segStart, segEnd, coverageStart time.Time, hasCoverage bool) float64 {
	if !hasCoverage || coverageStart.After(segEnd) {
		return 0
	}
	start := segStart
	if coverageStart.After(start) {
		start = coverageStart
	}
	return MonthsBetween(start, segEnd)
}

// Calculate derives the dashboard's KPI/trend metrics over [rangeStart,
// rangeEnd]. coverageStart/hasCoverage (from HealthcareCoverageStart,
// applied to the FULL unfiltered transaction set -- never ts, which is
// typically already range-filtered) clip every healthcare-target accrual
// to the window's intersection with actual coverage via
// ClippedHealthcareMonths: hasCoverage=false, or a coverageStart that
// leaves zero covered months in [rangeStart, rangeEnd], suppresses the
// healthcare budget exactly as healthcareTarget==0 does --
// HasHealthcareTarget=false, and every healthcare-derived field stays
// finite (no NaN/Inf) because the division below is guarded.
func Calculate(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, budgetTarget, healthcareTarget float64, coverageStart time.Time, hasCoverage bool) *models.DashboardMetrics {
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

	// coverageMonths is the healthcare-specific month count -- MonthsInRange
	// clipped to [coverageStart, rangeEnd] via the single clipping helper.
	// Every healthcare accrual below (actual rate, target total, cumulative
	// delta) uses coverageMonths instead of monthsInRange; living arithmetic
	// keeps monthsInRange unchanged, per the split-classification rule.
	coverageMonths := ClippedHealthcareMonths(rangeStart, rangeEnd, coverageStart, hasCoverage)
	healthcareCoverageInRange := hasCoverage && !coverageStart.Before(rangeStart) && !coverageStart.After(rangeEnd)

	healthcareOutflows := outflows.FilterByCategory(HealthInsuranceCategory)
	healthcareTotal := math.Abs(healthcareOutflows.SumAmount())
	var healthcareActual float64
	if coverageMonths > 0 {
		healthcareActual = healthcareTotal / coverageMonths
	}
	healthcarePerMonthDelta := healthcareActual - healthcareTarget
	healthcareCumulativeDelta := healthcareTotal - healthcareTarget*coverageMonths
	hasHealthcareTarget := healthcareTarget > 0 && coverageMonths > 0

	livingTotal := totalExpenses - healthcareTotal
	actualMonthly := livingTotal / monthsInRange
	perMonthDelta := actualMonthly - budgetTarget
	cumulativeDelta := livingTotal - budgetTarget*monthsInRange
	hasBudgetTarget := budgetTarget > 0

	// Combined plan variance — single number that nets Living + Healthcare
	// against their summed targets. Drives the Budget KPI card so a category
	// being under can offset another being over. CombinedTarget stays the
	// raw monthly-rate sum (unaffected by coverage timing -- it answers "is
	// a target configured at all", not "is it accruing this window").
	// CombinedCumulativeDelta is the sum of the two cumulative deltas
	// directly, so it inherits healthcareCumulativeDelta's clipped basis
	// without re-deriving the arithmetic.
	combinedTarget := budgetTarget + healthcareTarget
	combinedActualMonthly := actualMonthly + healthcareActual
	combinedPerMonthDelta := combinedActualMonthly - combinedTarget
	combinedCumulativeDelta := cumulativeDelta + healthcareCumulativeDelta
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
	}

	// Combined cumulative balance — a calendar-month walk over
	// [rangeStart, rangeEnd], built in its own loop independent of the
	// transaction-month trend loop above. Each calendar month intersecting
	// the range contributes a pro-rated target accrual -- living's share via
	// plain MonthsBetween(seg), healthcare's share via the same
	// ClippedHealthcareMonths(seg, coverageStart, hasCoverage) helper
	// Calculate's own healthcare totals use, so a segment before coverage
	// starts contributes $0 of healthcare accrual -- less that month's
	// actual outflow spend (all outflows — living + healthcare — matching
	// CombinedCumulativeDelta's basis). A month with no transactions still
	// produces a point: the target accrues, nothing is spent. See the
	// field doc on models.DashboardMetrics.CombinedCumulativeBalance for
	// the resulting invariant and its pre-filtered-TransactionSet
	// precondition.
	var combinedCumulativeBalance []float64
	if hasCombinedTarget {
		loc := rangeStart.Location()
		monthCursor := time.Date(rangeStart.Year(), rangeStart.Month(), 1, 0, 0, 0, 0, loc)
		lastMonth := time.Date(rangeEnd.Year(), rangeEnd.Month(), 1, 0, 0, 0, 0, loc)

		var running float64
		for cur := monthCursor; !cur.After(lastMonth); cur = cur.AddDate(0, 1, 0) {
			monthStart := cur
			monthEnd := cur.AddDate(0, 1, 0).AddDate(0, 0, -1) // last calendar day of cur's month

			segStart := monthStart
			if rangeStart.After(segStart) {
				segStart = rangeStart
			}
			segEnd := monthEnd
			if rangeEnd.Before(segEnd) {
				segEnd = rangeEnd
			}

			accrual := budgetTarget*MonthsBetween(segStart, segEnd) +
				healthcareTarget*ClippedHealthcareMonths(segStart, segEnd, coverageStart, hasCoverage)

			spend := 0.0
			if bucket, ok := monthlyOutflows[cur.Format("2006-01")]; ok {
				spend = math.Abs(bucket.SumAmount())
			}

			running += accrual - spend
			combinedCumulativeBalance = append(combinedCumulativeBalance, running)
		}

		// Display cap: keep only the LAST 6 walked points. Running totals
		// (and therefore the dropped months' carry-in) are preserved —
		// only which points are plotted is trimmed.
		if len(combinedCumulativeBalance) > 6 {
			combinedCumulativeBalance = combinedCumulativeBalance[len(combinedCumulativeBalance)-6:]
		}
	}

	return &models.DashboardMetrics{
		TotalIncome:                    totalIncome,
		TotalExpenses:                  totalExpenses,
		NetSavings:                     netSavings,
		SavingsRate:                    savingsRate,
		TransactionCount:               ts.Len(),
		StartDate:                      ts.MinDate(),
		EndDate:                        ts.MaxDate(),
		IncomeTrend:                    incomeTrend,
		ExpensesTrend:                  expensesTrend,
		SavingsTrend:                   savingsTrend,
		TrendLabels:                    trendLabels,
		MonthsInRange:                  monthsInRange,
		LivingExpensesTotal:            livingTotal,
		ActualMonthly:                  actualMonthly,
		BudgetTarget:                   budgetTarget,
		PerMonthDelta:                  perMonthDelta,
		CumulativeDelta:                cumulativeDelta,
		HasBudgetTarget:                hasBudgetTarget,
		LivingExpensesTrend:            livingTrend,
		HealthcareActual:               healthcareActual,
		HealthcareTotal:                healthcareTotal,
		HealthcareTarget:               healthcareTarget,
		HealthcarePerMonthDelta:        healthcarePerMonthDelta,
		HealthcareCumulativeDelta:      healthcareCumulativeDelta,
		HasHealthcareTarget:            hasHealthcareTarget,
		HealthcareTrend:                healthcareTrend,
		CombinedTarget:                 combinedTarget,
		CombinedActualMonthly:          combinedActualMonthly,
		CombinedPerMonthDelta:          combinedPerMonthDelta,
		CombinedCumulativeDelta:        combinedCumulativeDelta,
		HasCombinedTarget:              hasCombinedTarget,
		LivingTargetTotal:              budgetTarget * monthsInRange,
		HealthcareTargetTotal:          healthcareTarget * coverageMonths,
		CombinedCumulativeBalance:      combinedCumulativeBalance,
		HealthcareCoverageStart:        coverageStart,
		HealthcareHasCoverage:          hasCoverage,
		HealthcareCoverageStartInRange: healthcareCoverageInRange,
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
	// data.Active() strips duplicate-resolved rows before deriving coverage
	// start, matching every other consumer's basis (dataloader duplicate
	// resolution) -- Active() is idempotent, so this is a no-op for callers
	// that already pass an active-only set (as of this writing, both
	// handlers.go call sites do), but it means Comparison no longer relies
	// on that caller discipline. Both windows below then clip against the
	// same coverage start, not a per-window re-derivation.
	coverageStart, hasCoverage := HealthcareCoverageStart(data.Active())
	currentMetrics := Calculate(currentFiltered, start, end, currentTarget, healthTarget, coverageStart, hasCoverage)
	compMetrics := Calculate(compFiltered, compStart, compEnd, compTarget, healthTarget, coverageStart, hasCoverage)

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
