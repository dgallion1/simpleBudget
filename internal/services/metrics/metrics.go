// Package metrics derives dashboard KPIs and trend sparklines from a
// TransactionSet — totals, savings rate, monthly buckets, and budget
// deltas. Pure functions over models with no I/O.
package metrics

import (
	"math"
	"sort"
	"strings"
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

// PlanExcludedOutflows returns the subset of outflows whose Hash is a key in
// planExclusions -- transactions the what-if plan sync already excludes from
// its living-expense average because it models them separately (an
// ExpenseSource, e.g. a car loan; see
// majorexpenses.ComputePlanSyncExclusions). Any row in
// HealthInsuranceCategory is skipped even when it also appears in
// planExclusions: the healthcare split above already removes HI-category
// rows from living, and D-SY-e's ordering (mirrored here from the sync's own
// iteration) claims an HI+flagged overlap row for HI exactly once, never
// double-subtracting it. Nil-safe: a nil outflows or empty/nil
// planExclusions returns an empty, non-nil *models.TransactionSet, so
// callers can call .SumAmount()/.Len()/.GroupByMonth() unconditionally.
//
// This function is used two ways: (1) as DISPLAY-ONLY annotation data
// (PlanExcludedTotal/PlanExcludedCount below, and whatif/sync.go's own
// ExcludedGroups preview), and (2) as the building block LivingOutflows
// uses to derive the SET every living arithmetic actually runs on (ruling
// SY-2026-08-30d) -- never arithmetic subtraction. See LivingOutflows' doc
// for why (2) is a set-exclusion, not a subtracted total.
func PlanExcludedOutflows(outflows *models.TransactionSet, planExclusions map[string]models.MajorExpense) *models.TransactionSet {
	result := &models.TransactionSet{}
	if outflows == nil || len(planExclusions) == 0 {
		return result
	}
	for _, t := range outflows.Transactions {
		if strings.EqualFold(t.Category, HealthInsuranceCategory) {
			continue
		}
		if _, ok := planExclusions[t.Hash]; ok {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}

// LivingOutflows returns the outflows that count toward the living-expense
// figures: every row in outflows EXCEPT HealthInsuranceCategory rows
// (tracked separately by the Healthcare KPI) and any row PlanExcludedOutflows
// claims (a flagged plan-sync exclusion, with the same HI-first precedence --
// an HI+flagged overlap row is removed once, as HI, matching D-SY-e).
//
// This set is consumed DIRECTLY, at every granularity Calculate needs, with
// no arithmetic subtraction of a separately-computed total from an
// already-summed figure -- this function is the ONLY place any row is ever
// excluded (ruling SY-2026-08-30d, attempt 3, rewriting SY-2026-08-30c's
// contract: SET EXCLUSION, never subtraction). The range total (CB7) and the
// per-month trend and the dashboard's budget-vs-actual chart (CB2) all use
// the SAME SIGNED negated net, -LivingOutflows(...).SumAmount(): a
// refund-dominant range or month must show as a negative (a credit), which
// math.Abs used to flip positive. That subtraction shape (an arithmetic
// subtraction of a separately-computed total from an already-summed figure)
// breaks whenever the REMAINDER itself nets a refund (e.g. an outflow-typed
// credit misclassified into Outflow, per the "type is inferred, not
// bank-supplied" ledger convention), independent of the flagged group's own
// sign -- Abs(S+F)-F (or any signed variant of it) is not equal to Abs(S) in
// general; only computing directly on the remaining transactions is.
//
// Nil-safe: nil outflows or nil/empty planExclusions still applies the HI
// filter, so `-LivingOutflows(outflows, nil).SumAmount()` reproduces pre-SY4
// master's `totalExpenses - healthcareTotal` byte-for-byte on
// all-outflows-negative data (and is MORE correct than that subtraction
// shape whenever HI itself nets a refund -- a side effect, not a new
// healthcareTotal computation; healthcareTotal/healthcareOutflows above are
// untouched by this function).
func LivingOutflows(outflows *models.TransactionSet, planExclusions map[string]models.MajorExpense) *models.TransactionSet {
	result := &models.TransactionSet{}
	if outflows == nil {
		return result
	}
	for _, t := range outflows.Transactions {
		if strings.EqualFold(t.Category, HealthInsuranceCategory) {
			continue
		}
		if _, ok := planExclusions[t.Hash]; ok {
			continue
		}
		result.Transactions = append(result.Transactions, t)
	}
	return result
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
//
// planExclusions (SY4) is the transaction-Hash -> flagged-def map from
// majorexpenses.ComputePlanSyncExclusions, computed by the caller over the
// FULL unfiltered transaction set -- same discipline as coverageStart, and
// the SAME map the what-if dashboard sync already excludes from its own
// living-expense average. nil/empty means "no exclusions", reproducing
// pre-SY4 behavior for every field exactly (LivingOutflows applies the HI
// filter identically either way).
//
// Ruling SY-2026-08-30d (attempt 3): flagged rows are removed from the
// outflow SET (via LivingOutflows) BEFORE the living-expenses figures below
// (LivingExpensesTotal, ActualMonthly, PerMonthDelta, CumulativeDelta,
// LivingExpensesTrend, and the combined cumulative walk's living share) are
// computed with the signed negated-net arithmetic (CB7) -- never an arithmetic
// subtraction of a separately-computed exclusion amount from an
// already-Abs'd total (attempts 1-2's shape, both wrong: attempt 1 used
// Abs on the flagged group's net, attempt 2 signed it but still subtracted
// from Abs(everything), which breaks whenever the REMAINDER itself nets a
// refund independent of the flagged group's sign). See LivingOutflows' doc.
// PlanExcludedTotal/PlanExcludedCount below are DISPLAY-ONLY annotation
// data, never fed back into any of these figures. TotalIncome/TotalExpenses/
// NetSavings/SavingsRate are computed above this point, from the FULL
// outflow set, and are untouched -- they reflect every dollar actually
// spent regardless of the flag.
func Calculate(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, budgetTarget, healthcareTarget float64, coverageStart time.Time, hasCoverage bool, planExclusions map[string]models.MajorExpense) *models.DashboardMetrics {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	totalIncome := income.SumAmount()
	// CB7 fix: totalExpenses is the SIGNED negated net of the range's whole
	// outflow set, not math.Abs -- the same signed-negated-net contract CB2
	// already gave every per-month figure fed by this function. An ordinary
	// range nets outflow-negative, so -SumAmount() is positive spend, same
	// as the old math.Abs. A REFUND-DOMINANT range -- one whose outflow-typed
	// rows net POSITIVE across the whole window -- must report NEGATIVE
	// expenses (a net credit); math.Abs flipped this sign and understated
	// NetSavings/SavingsRate below and broke the CombinedCumulativeBalance
	// partition invariant for exactly this case (see LivingOutflows' doc and
	// models.DashboardMetrics.CombinedCumulativeBalance's doc, both updated
	// by CB7). netSavings/savingsRate need no further change: they derive
	// correctly once totalExpenses is signed, and ADD the net refund instead
	// of subtracting an absolute value. CB7-2026-09-03c: computed via the
	// SignedNet helper (not inline -outflows.SumAmount()), which also
	// normalizes IEEE negative zero to +0 -- an exactly-cancelling range
	// would otherwise render "$-0.00" (encoding/json even serializes -0.0
	// as "-0").
	totalExpenses := SignedNet(outflows)
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
	// CB7 fix: same signed-negated-net contract as totalExpenses above --
	// a refund-dominant healthcare window (premium refunds exceeding
	// premiums paid) must be negative, not math.Abs'd positive.
	healthcareTotal := SignedNet(healthcareOutflows)
	var healthcareActual float64
	if coverageMonths > 0 {
		healthcareActual = healthcareTotal / coverageMonths
	}
	healthcarePerMonthDelta := healthcareActual - healthcareTarget
	healthcareCumulativeDelta := healthcareTotal - healthcareTarget*coverageMonths
	hasHealthcareTarget := healthcareTarget > 0 && coverageMonths > 0

	// Plan-sync exclusions (SY4, ruling SY-2026-08-30d): PlanExcludedTotal/
	// PlanExcludedCount below are DISPLAY-ONLY annotation data -- the SIGNED
	// net spend (matching the SY1 `Total += -t.Amount` convention in
	// whatif/sync.go: positive = net spend, negative = net refund) and row
	// count of the flagged group, for surfaces that want to annotate. They
	// are NEVER used in the living-expense arithmetic below (grep this
	// function for `PlanExcluded` outside this block and the final struct
	// literal -- there is none). Living instead uses SET EXCLUSION via
	// LivingOutflows (see its doc for why arithmetic subtraction from an
	// already-Abs'd total was wrong).
	planExcludedSet := PlanExcludedOutflows(outflows, planExclusions)
	planExcludedTotal := SignedNet(planExcludedSet)
	planExcludedCount := planExcludedSet.Len()

	// livingOutflows already excludes HealthInsuranceCategory rows (the
	// same rows healthcareOutflows above tracks) AND flagged rows -- the
	// signed negated-net (CB7) living arithmetic runs directly on it. This REPLACES
	// the pre-SY4 `totalExpenses - healthcareTotal` subtraction shape
	// entirely; healthcareTotal itself (computed above) is untouched and
	// used only by the Healthcare KPI fields below.
	livingOutflows := LivingOutflows(outflows, planExclusions)
	// CB7 fix: same signed-negated-net contract as totalExpenses above --
	// a refund-dominant living-expense window must be negative, not
	// math.Abs'd positive.
	livingTotal := SignedNet(livingOutflows)
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
	monthlyLiving := livingOutflows.GroupByMonth()
	// nonExcludedOutflows is the combined cumulative walk's basis below:
	// every outflow EXCEPT plan-sync-excluded rows -- HI stays in (the walk
	// nets living+healthcare together against CombinedTarget, matching its
	// pre-SY4 basis exactly aside from the plan exclusion). Built by
	// merging the two disjoint sets already classified above rather than a
	// third independent classifier.
	nonExcludedOutflows := &models.TransactionSet{
		Transactions: append(append([]models.Transaction{}, livingOutflows.Transactions...), healthcareOutflows.Transactions...),
	}
	monthlyNonExcluded := nonExcludedOutflows.GroupByMonth()

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

		// CB2 fix: expAmt is the SIGNED negated net of the month's outflow
		// bucket, not math.Abs. An ordinary month nets outflow-negative, so
		// -SumAmount() is positive expense, same as the old math.Abs. A
		// REFUND-DOMINANT month -- one whose outflow-typed rows net
		// POSITIVE -- must show as a NEGATIVE expense (a credit); math.Abs
		// flipped this sign. savingsTrend (incAmt-expAmt below) needs no
		// change: it derives correctly once expAmt is signed, and ADDS the
		// refund instead of subtracting it.
		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = SignedNet(exp)
		}

		// CB2 fix: same signed-negated-net contract as expAmt above.
		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = SignedNet(hc)
		}

		// Set exclusion (ruling SY-2026-08-30d; signed per CB2): the
		// signed negated net runs directly on livingOutflows' month
		// bucket, never an arithmetic subtraction from expAmt (which
		// breaks whenever the REMAINDER itself nets a refund).
		livingMonth := 0.0
		if lo, ok := monthlyLiving[m]; ok {
			livingMonth = SignedNet(lo)
		}

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
	// actual outflow spend, SIGNED (all outflows — living + healthcare —
	// matching CombinedCumulativeDelta's basis; CB1: a refund-dominant
	// month enters as a credit, never charged as spend -- see the spend
	// computation below). A month with no transactions still produces a
	// point: the target accrues, nothing is spent. See the field doc on
	// models.DashboardMetrics.CombinedCumulativeBalance for the resulting
	// invariant and its pre-filtered-TransactionSet precondition.
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

			// Set exclusion (ruling SY-2026-08-30d): spend is the SIGNED
			// negated net of nonExcludedOutflows' month bucket -- all
			// outflows except plan-sync-excluded rows (HI stays in; matches
			// CombinedCumulativeDelta's living+healthcare basis exactly).
			// Never an arithmetic subtraction from monthlyOutflows' |sum|.
			//
			// CB1 fix: an ordinary month's outflow-typed rows net negative
			// (SumAmount() < 0), so -SumAmount() is positive spend, same as
			// the old math.Abs. A REFUND-DOMINANT month -- one whose
			// outflow-typed rows net POSITIVE, e.g. a cruise refund larger
			// than the month's spending -- must enter the walk as a CREDIT,
			// not be charged as spend; -SumAmount() is then negative and
			// `running += accrual - spend` correctly ADDS the net refund to
			// the balance. math.Abs flipped this sign (KD ruling: month rows
			// are signed). This does not touch range-level totalExpenses'
			// own arithmetic (now also the signed negated net, CB7, line
			// ~377) -- per-month spends partition that range total exactly
			// for EVERY range now, including a wholly refund-dominant one,
			// since both sides of the partition use the same signed
			// convention (the "RANGE as a whole nets outflow-negative"
			// precondition CB1 documented here is removed by CB7; see
			// models.DashboardMetrics.CombinedCumulativeBalance's doc).
			spend := 0.0
			if bucket, ok := monthlyNonExcluded[cur.Format("2006-01")]; ok {
				spend = SignedNet(bucket)
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
		PlanExcludedTotal:              planExcludedTotal,
		PlanExcludedCount:              planExcludedCount,
	}
}

// planExclusions (SY4) is threaded straight through to both Calculate calls
// below unmodified: a transaction's flagged status is a fact about the
// ledger keyed by Hash, not something that varies between the current and
// comparison windows, so the SAME map applies to both. Without this, the
// "vs prior" deltas kpis.html renders (ActualMonthlyChange,
// CumulativeDeltaChange) would compare an exclusion-adjusted current figure
// against an unadjusted comparison figure -- a surface computing living
// actuals directly from Calculate, so it must consume the same map
// (criterion 3, split-classification rule).
func Comparison(data *models.TransactionSet, start, end time.Time, compType string, settings *models.WhatIfSettings, planExclusions map[string]models.MajorExpense) *models.PeriodComparison {
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
	currentMetrics := Calculate(currentFiltered, start, end, currentTarget, healthTarget, coverageStart, hasCoverage, planExclusions)
	compMetrics := Calculate(compFiltered, compStart, compEnd, compTarget, healthTarget, coverageStart, hasCoverage, planExclusions)

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

// SignedNet returns the negated signed sum of a transaction set (positive =
// net spend, negative = net refund), with IEEE negative zero normalized to
// +0 so an empty or exactly-cancelling window never renders "-0" / "$-0.00"
// (ruling CB7-2026-09-03c). This is the ONLY sanctioned way to derive a
// spend figure from a set; never write -ts.SumAmount() inline.
//
// Nil-safe in exactly the same sense SumAmount is, no more and no less:
// SumAmount ranges over ts.Transactions, so it panics on a literal nil
// *models.TransactionSet but returns 0 on a non-nil, empty one (the shape
// every filter/group helper in this package -- FilterByType,
// FilterByCategory, GroupByMonth's map values, PlanExcludedOutflows,
// LivingOutflows -- always returns, per their own nil-safety docs).
// SignedNet adds no additional nil guard beyond that: every call site in
// this package and in explorer/handlers.go passes one of those non-nil
// results, never a bare nil TransactionSet.
func SignedNet(ts *models.TransactionSet) float64 {
	v := -ts.SumAmount()
	if v == 0 {
		return 0
	}
	return v
}

// PercentChange returns the percent change from previous to current.
//
// CB3-E (no code change): the |previous| denominator is the deliberate
// signed-base convention, not an abs-per-transaction bug of the kind CB1-CB3
// fixed elsewhere. Dividing by the signed previous would flip the sign of
// the result whenever previous is negative, inverting "improved" and
// "worsened"; dividing by |previous| preserves the natural direction of
// change regardless of which side of zero previous sits on. Pinned by
// TestPercentChange_NegativeBase: PercentChange(-500, -1000) == 50 (current
// is less negative -- an improvement, reported as +50%) and
// PercentChange(-1500, -1000) == -50 (current is more negative -- worse,
// reported as -50%).
func PercentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return ((current - previous) / math.Abs(previous)) * 100
}
