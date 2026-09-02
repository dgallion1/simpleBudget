// Package dashboard serves the main landing page and its HTMX-driven
// partials: KPI tiles, budget tracking, monthly-variance and healthcare
// charts, and the income-vs-expense rollups. Reads transactions from the
// dataloader and renders via the templates package; holds no state of
// its own beyond the per-request request scope.
package dashboard

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"net/http"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/metrics"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
)

var (
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
	store         *storage.Storage
)

// Initialize sets up the dashboard package with required dependencies. The
// storage service is needed to read the accounts sidecar for the accounts
// card and the projection summary line (A8); a nil store is tolerated --
// buildAccountsCard returns an empty card -- so existing tests that wire
// only a loader still pass.
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, rm *retirement.SettingsManager, s *storage.Storage) {
	loader = l
	renderer = r
	retirementMgr = rm
	store = s
}

// currentBudgetSettings returns the active what-if settings used to
// derive the dashboard budget target, or nil if the settings manager
// is unset or the load fails. The dashboard treats a missing settings
// (or zero MonthlyLivingExpenses) as "no budget configured" and renders
// the fallback card; loading errors are non-fatal.
func currentBudgetSettings() *models.WhatIfSettings {
	if retirementMgr == nil {
		return nil
	}
	settings, err := retirementMgr.Load()
	if err != nil {
		return nil
	}
	return settings
}

// planSyncExclusions returns the plan-sync exclusion map (transaction Hash
// -> flagged major-expense def) for ts via
// majorexpenses.ComputePlanSyncExclusions -- the SAME classifier the what-if
// dashboard sync already uses to keep plan-modeled spend (e.g. a car loan
// flagged ExcludeFromPlanSync) out of its living-expense average (SY4;
// D-SY-b's full-Match-pass discipline lives in ComputePlanSyncExclusions
// itself, not re-implemented here). Callers must pass the FULL active
// transaction set, never a range-filtered one -- a transaction's flagged
// status is a fact about the ledger, independent of the selected window
// (same discipline as metrics.HealthcareCoverageStart). Best-effort: a
// missing loader or a major_expenses.json load failure returns nil ("no
// exclusions") rather than failing the dashboard, matching
// bucketMajorExpenses' own tolerance for the same file.
func planSyncExclusions(ts *models.TransactionSet) map[string]models.MajorExpense {
	if loader == nil {
		return nil
	}
	defs, err := loader.LoadMajorExpenses()
	if err != nil || len(defs) == 0 {
		return nil
	}
	pins, _ := loader.LoadTransactionPins()
	return majorexpenses.ComputePlanSyncExclusions(ts, defs, pins)
}

// dashboardCalcInputs bundles the plan-derived inputs metrics.Calculate
// needs beyond the transaction set itself: the living/healthcare budget
// targets, the healthcare coverage-start fact, and the plan-sync exclusion
// map. Assembled by gatherDashboardCalcInputs so handleDashboard and
// handleKPIDetail feed metrics.Calculate the IDENTICAL inputs for a given
// date range -- the card and the modal cannot drift apart, because there is
// only one place these five values are derived (KD1 design point 4).
type dashboardCalcInputs struct {
	target         float64
	healthTarget   float64
	coverageStart  time.Time
	hasCoverage    bool
	planExclusions map[string]models.MajorExpense
}

// gatherDashboardCalcInputs assembles dashboardCalcInputs for [startDate,
// endDate]. active must be the FULL active (post duplicate-resolution)
// transaction set, never a range-filtered one -- coverage start and
// plan-sync exclusions are lifetime facts about the ledger, independent of
// the selected window (same discipline metrics.HealthcareCoverageStart and
// planSyncExclusions themselves document). settings is passed in rather
// than reloaded here so a caller that also needs it for
// metrics.TargetProvenance/metrics.Comparison loads it exactly once.
func gatherDashboardCalcInputs(settings *models.WhatIfSettings, active *models.TransactionSet, startDate, endDate time.Time) dashboardCalcInputs {
	target, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
	coverageStart, hasCoverage := metrics.HealthcareCoverageStart(active)
	planExclusions := planSyncExclusions(active)
	return dashboardCalcInputs{
		target:         target,
		healthTarget:   healthTarget,
		coverageStart:  coverageStart,
		hasCoverage:    hasCoverage,
		planExclusions: planExclusions,
	}
}

// RegisterRoutes registers all dashboard routes
func RegisterRoutes(r chi.Router) {
	r.Get("/dashboard", handleDashboard)
	r.Get("/dashboard/kpis", handleKPIsPartial)
	r.Get("/dashboard/charts/data/{chartType}", handleChartData)
	r.Get("/dashboard/major-expense", handleMajorExpenseDrilldown)
	r.Get("/dashboard/kpi/{kpiType}", handleKPIDetail)
	r.Get("/dashboard/kpi/{kpiType}/month/{month}", handleKPIMonthDetail)
	r.Get("/dashboard/kpi/{kpiType}/export", handleKPIExport)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		log.Printf("Error loading data: %v", err)
		http.Error(w, "Error loading data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse date range from query params
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	comparison := r.URL.Query().Get("comparison")

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	startDate, endDate := resolveDateRange(startStr, endStr, minDate, maxDate)

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	settings := currentBudgetSettings()
	// See gatherDashboardCalcInputs: coverage start and plan-sync exclusions
	// are derived from the FULL active (post duplicate-resolution) set, never
	// the range-filtered one -- lifetime facts about the ledger, independent
	// of the selected window (single-source rule, ruling 2026-08-29a).
	calcInputs := gatherDashboardCalcInputs(settings, data.Active(), startDate, endDate)
	dashMetrics := metrics.Calculate(filtered, startDate, endDate, calcInputs.target, calcInputs.healthTarget, calcInputs.coverageStart, calcInputs.hasCoverage, calcInputs.planExclusions)
	// Provenance for the Monthly Living Expenses card's "Target $X" text —
	// base plan value, effective multiplier, and active phase, so the
	// number is explained rather than appearing out of nowhere next to the
	// What-If plan's own figure. See kpis.html.
	targetProvenance := metrics.TargetProvenance(settings, startDate, endDate)

	// Calculate period comparison if requested
	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = metrics.Comparison(data.Active(), startDate, endDate, comparison, settings, calcInputs.planExclusions)
	}

	// Accounts card (A8): per-account balance, freshness, low/stale/no-anchor
	// flags, and a checking/savings projection summary line. Uses the loader's
	// maxDate as the "as of" date so "data through Aug 12" and the balance are
	// consistent with what the rest of the dashboard shows. Reuses
	// accounts.BalanceAt / Freshness / Project; adds no balance or projection
	// logic of its own.
	asOf := maxDate
	if asOf.IsZero() {
		asOf = time.Now()
	}
	accountsCard := buildAccountsCard(store, data.Active().Transactions, asOf, detectRecurringForDashboard(data, asOf))

	// Unassigned-files banner (A8): surfaces how many transactions came from
	// CSVs matching no account, linking to /accounts. Files are never
	// silently dropped; this banner is what makes that visible. Read from
	// the loader's most recent load count (the same figure the accounts page
	// derives its unassigned file list from).
	unassignedCount := 0
	if loader != nil {
		unassignedCount = loader.UnassignedCount()
	}

	pageData := map[string]interface{}{
		"Title":            "Dashboard",
		"ActiveTab":        "dashboard",
		"Metrics":          dashMetrics,
		"TargetProvenance": targetProvenance,
		"BudgetVerdict":    BuildBudgetVerdict(dashMetrics),
		"PeriodComparison": periodComparison,
		"StartDate":        startDate.Format("2006-01-02"),
		"EndDate":          endDate.Format("2006-01-02"),
		"MinDate":          minDate.Format("2006-01-02"),
		"MaxDate":          maxDate.Format("2006-01-02"),
		"Comparison":       comparison,
		"AccountsCard":     accountsCard,
		"UnassignedCount":  unassignedCount,
	}

	templates.AttachDuplicateCount(pageData, loader)
	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Dashboard</h1><p>Templates not loaded. Check configuration.</p></body></html>"))
	}
}

func handleKPIsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	comparison := r.URL.Query().Get("comparison")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	settings := currentBudgetSettings()
	target, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
	// See handleDashboard: coverage start comes from the full active set,
	// never the range-filtered one.
	coverageStart, hasCoverage := metrics.HealthcareCoverageStart(data.Active())
	// SY4: see handleDashboard.
	planExclusions := planSyncExclusions(data.Active())
	dashMetrics := metrics.Calculate(filtered, startDate, endDate, target, healthTarget, coverageStart, hasCoverage, planExclusions)
	targetProvenance := metrics.TargetProvenance(settings, startDate, endDate)

	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = metrics.Comparison(data.Active(), startDate, endDate, comparison, settings, planExclusions)
	}

	partialData := map[string]interface{}{
		"Metrics":          dashMetrics,
		"TargetProvenance": targetProvenance,
		"BudgetVerdict":    BuildBudgetVerdict(dashMetrics),
		"PeriodComparison": periodComparison,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "kpis", partialData)
	} else if err := json.NewEncoder(w).Encode(partialData); err != nil {
		log.Printf("dashboard: encoding kpis JSON: %v", err)
	}
}

func handleChartData(w http.ResponseWriter, r *http.Request) {
	chartType := chi.URLParam(r, "chartType")

	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)

	var chartData interface{}

	switch chartType {
	case "major-expense":
		chartData = buildMajorExpenseChartData(filtered)
	case "spending-trend":
		chartData = buildSpendingTrendChartData(filtered)
	case "merchants":
		chartData = buildMerchantsChartData(filtered)
	case "cumulative":
		chartData = buildCumulativeChartData(filtered)
	case "budget-vs-actual":
		settings := currentBudgetSettings()
		livingTarget, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
		// See handleDashboard: coverage start comes from the full active
		// set, never the range-filtered one.
		coverageStart, hasCoverage := metrics.HealthcareCoverageStart(data.Active())
		// SY4: see handleDashboard.
		planExclusions := planSyncExclusions(data.Active())
		chartData = buildBudgetVsActualChartData(filtered, startDate, endDate, livingTarget, healthTarget, coverageStart, hasCoverage, planExclusions)
	default:
		http.Error(w, "Unknown chart type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(chartData); err != nil {
		log.Printf("dashboard: encoding chart JSON: %v", err)
	}
}

// handleMajorExpenseDrilldown returns the transactions covered by a
// single wedge in the Spending by Major Expense donut. The wedge name
// is taken from the "name" query param (URL-safe; major expense names
// can include arbitrary user text). Two synthetic wedges are honored:
// "Unmatched" returns outflows that didn't match any major expense, and
// "Other" returns the rolled-up tail beyond the donut display limit.
func handleMajorExpenseDrilldown(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")

	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	buckets, unmatched := bucketMajorExpenses(filtered)

	var txns []models.Transaction
	switch name {
	case "Unmatched":
		txns = unmatched
	case "Other":
		// "Other" must mirror buildMajorExpenseChartData's rolled-up
		// tail exactly (CB4-2026-09-02a): the donut-limit slice applies
		// to the POSITIVE buckets only, so a net-negative group sitting
		// at the sort tail is never folded into "Other" here either —
		// it belongs to the "credits" list, not this drilldown.
		positiveBuckets, _ := splitPositiveMajorExpenseBuckets(buckets)
		if len(positiveBuckets) > majorExpenseDonutLimit {
			for _, b := range positiveBuckets[majorExpenseDonutLimit:] {
				txns = append(txns, b.txns...)
			}
		}
	default:
		for _, b := range buckets {
			if b.name == name {
				txns = b.txns
				break
			}
		}
	}

	// Total/count/avg are computed BEFORE the display sort below (CB6-5):
	// float64 addition is not associative, so summing the same signed
	// amounts in a different order can differ in the last bit or two
	// (observed divergence ~7e-15 on a many-transaction bucket). For the
	// "default" (named-bucket) case, txns here IS bucketMajorExpenses'
	// b.txns — still in match.Groups' match order, the exact order its
	// bucket.total was summed in — so summing here first, before
	// sort.Slice reorders the slice in place for date display, pins this
	// Total to be bit-identical to that list-row figure. Summing after
	// the sort would silently drift the modal's Total from the list row
	// it's supposed to agree with.
	var total float64
	for _, t := range txns {
		// Signed net (CB3-A): Total = -(sum of signed amounts), matching
		// bucketMajorExpenses' list-row contract above so the drilldown
		// modal never disagrees with its own list row. Refunds (positive
		// Outflow amounts per classifier convention) reduce the total; a
		// refund-dominant group renders negative.
		total -= t.Amount
	}
	count := len(txns)
	var avgAmount float64
	if count > 0 {
		avgAmount = total / float64(count)
	}

	// Display order only, applied AFTER the totals above are pinned.
	sort.Slice(txns, func(i, j int) bool { return txns[i].Date.After(txns[j].Date) })

	partialData := map[string]interface{}{
		"Name":         name,
		"Transactions": txns,
		"Total":        total,
		"Count":        count,
		"AvgAmount":    avgAmount,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "major-expense-drilldown", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// kpiTitles maps a KPI tile's type to the heading its detail modal shows.
// Membership doubles as the set of valid kpiType path params for the month
// drill-down.
var kpiTitles = map[string]string{
	"income":       "Total Income",
	"expenses":     "Total Expenses",
	"savings":      "Net Savings",
	"savings-rate": "Savings Rate",
	"living":       "Monthly Living Expenses",
	"healthcare":   "Monthly Healthcare",
}

// monthKeyPattern matches a YYYY-MM month key of the form GroupByMonth emits.
var monthKeyPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func handleKPIDetail(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	income := filtered.FilterByType(models.Income)
	outflows := filtered.FilterByType(models.Outflow)

	// living/healthcare classify outflows through the SAME helpers the
	// dashboard card uses (single-source rule, ruling 2026-08-29a) and read
	// their "Per Month" figure from metrics.Calculate -- the fractional
	// MonthsBetween/ClippedHealthcareMonths divisor, never rows/len(months)
	// -- via gatherDashboardCalcInputs, the SAME helper handleDashboard
	// calls, so the card and this modal cannot drift apart (KD1 design
	// point 4). Gated to the two kinds that need it so the other four kinds'
	// request shape (and any loader I/O it implies) is unchanged.
	var monthlyLivingTotals, monthlyHealthcareTotals map[string]float64
	var classifiedCardPerMonth float64
	// healthcareNoCoverageInRange is ruling KD-2026-08-30c's zero-divisor
	// guard: when the coverage-clipped month count for [startDate, endDate]
	// is zero (no coverage, or coverage starts after the range ends),
	// HealthcareActual is 0 not because spend is zero but because the
	// divisor is -- so the modal must not render "$0.00" beside non-zero
	// classified rows. Computed via metrics.ClippedHealthcareMonths, the
	// SAME single-source helper metrics.Calculate itself calls internally
	// (see its coverageMonths line) -- not a re-derivation of coverage
	// logic, just reading the same fact metrics.Calculate already used to
	// produce classifiedCardPerMonth==0 in this state.
	var healthcareNoCoverageInRange bool
	if kpiType == "living" || kpiType == "healthcare" {
		settings := currentBudgetSettings()
		calcInputs := gatherDashboardCalcInputs(settings, data.Active(), startDate, endDate)
		cardMetrics := metrics.Calculate(filtered, startDate, endDate, calcInputs.target, calcInputs.healthTarget, calcInputs.coverageStart, calcInputs.hasCoverage, calcInputs.planExclusions)
		livingOutflows := metrics.LivingOutflows(outflows, calcInputs.planExclusions)
		healthcareOutflows := outflows.FilterByCategory(metrics.HealthInsuranceCategory)
		monthlyLivingTotals = classifiedMonthlyTotals(livingOutflows)
		monthlyHealthcareTotals = classifiedMonthlyTotals(healthcareOutflows)
		if kpiType == "living" {
			classifiedCardPerMonth = cardMetrics.ActualMonthly
		} else {
			classifiedCardPerMonth = cardMetrics.HealthcareActual
			healthcareNoCoverageInRange = metrics.ClippedHealthcareMonths(startDate, endDate, calcInputs.coverageStart, calcInputs.hasCoverage) <= 0
		}
	}

	// Group by month
	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

	// Collect all months
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

	// Calculate monthly summaries
	type MonthlyStat struct {
		Month    string
		Value    float64
		Income   float64
		Expenses float64
		Savings  float64
		Rate     float64
	}
	var monthlySummaries []MonthlyStat
	var values []float64

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		// CB2 fix: expAmt is the SIGNED negated net of the month's outflow
		// bucket, matching the classifiedMonthlyTotals kinds below (ruling
		// KD-2026-08-30d). An ordinary month nets outflow-negative, so
		// -SumAmount() is positive expense, same as the old math.Abs. A
		// REFUND-DOMINANT month -- one whose outflow-typed rows net
		// POSITIVE -- shows as a NEGATIVE expense (a credit); math.Abs
		// flipped this sign. DERIVED savings/rate below need no change:
		// they derive correctly once expAmt is signed.
		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = -exp.SumAmount()
		}

		savings := incAmt - expAmt
		rate := 0.0
		if incAmt > 0 {
			rate = (savings / incAmt) * 100
		}

		// Set exclusion (ruling SY-2026-08-30d) + signed rows, no per-month
		// Abs (ruling KD-2026-08-30d): classifiedMonthlyTotals negates the
		// classified month bucket's signed sum directly -- positive = net
		// spend, negative = net refund. A missing month key reads as 0
		// (Go's zero-value map lookup), matching the ok-checked pattern the
		// other kinds use.
		livingAmt := monthlyLivingTotals[m]
		healthcareAmt := monthlyHealthcareTotals[m]

		var value float64
		switch kpiType {
		case "income":
			value = incAmt
		case "expenses":
			value = expAmt
		case "savings":
			value = savings
		case "savings-rate":
			value = rate
		case "living":
			value = livingAmt
		case "healthcare":
			value = healthcareAmt
		}

		monthlySummaries = append(monthlySummaries, MonthlyStat{
			Month:    m,
			Value:    value,
			Income:   incAmt,
			Expenses: expAmt,
			Savings:  savings,
			Rate:     rate,
		})
		values = append(values, value)
	}

	// Calculate stats
	var sum, min, max, avg float64
	var minMonth, maxMonth string

	if len(values) > 0 {
		min = values[0]
		max = values[0]
		minMonth = monthlySummaries[0].Month
		maxMonth = monthlySummaries[0].Month

		for i, v := range values {
			sum += v
			if v < min {
				min = v
				minMonth = monthlySummaries[i].Month
			}
			if v > max {
				max = v
				maxMonth = monthlySummaries[i].Month
			}
		}
		avg = sum / float64(len(values))
	}

	// Amendment KD-2026-08-30a: living/healthcare show exactly ONE per-month
	// figure -- the card's own rate (classifiedCardPerMonth, computed above
	// via metrics.Calculate's fractional divisor), never the Total÷months
	// average the other four kinds use. This also becomes the "vs Avg"
	// column's comparison basis (the template's existing mechanism, just fed
	// the card figure instead of the arithmetic mean) rather than a second,
	// separately-rendered average stat.
	if kpiType == "living" || kpiType == "healthcare" {
		avg = classifiedCardPerMonth
	}

	// Calculate number of months for period breakdown
	numMonths := len(months)
	if numMonths == 0 {
		numMonths = 1
	}

	partialData := map[string]interface{}{
		"Type":                        kpiType,
		"Title":                       kpiTitles[kpiType],
		"Monthly":                     monthlySummaries,
		"Total":                       sum,
		"Average":                     avg,
		"Min":                         min,
		"Max":                         max,
		"MinMonth":                    minMonth,
		"MaxMonth":                    maxMonth,
		"NumMonths":                   numMonths,
		"IsRate":                      kpiType == "savings-rate",
		"IsSavings":                   kpiType == "savings",
		"HealthcareNoCoverageInRange": healthcareNoCoverageInRange,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "kpi-detail", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// transactionsInMonth returns the set's transactions falling in the given
// YYYY-MM month key, in load order.
func transactionsInMonth(ts *models.TransactionSet, month string) []models.Transaction {
	if set, ok := ts.GroupByMonth()[month]; ok {
		return set.Transactions
	}
	return []models.Transaction{}
}

// sumSigned adds amounts as stored, so a positive-amount outflow (a refund)
// reduces the total rather than inflating it.
func sumSigned(txns []models.Transaction) float64 {
	var sum float64
	for _, t := range txns {
		sum += t.Amount
	}
	return sum
}

// classifiedMonthlyTotals groups a classified set (living or healthcare
// outflows, already filtered via metrics.LivingOutflows / FilterByCategory)
// by month and returns each month's NEGATED signed sum -- positive means
// net spend, negative means net refund, and there is NO per-month Abs
// (ruling KD-2026-08-30d, matching the MCP by_month convention). Because
// the modal's Total tile is the sum of these same displayed values (one
// rounding path), a row's displayed figure and the Total it feeds always
// reconcile exactly; Total also equals Metrics.LivingExpensesTotal /
// HealthcareTotal whenever the range nets spend (the only divergence is a
// whole-range net refund, where Total honestly renders negative while the
// card figure is an Abs -- documented, accepted). A month absent from the
// classified set is simply absent from the returned map; callers read it
// with a plain map lookup, which yields 0 for those months. Shared by
// handleKPIDetail and handleKPIExport (K8) so the modal and its CSV export
// can never disagree about a month's total.
func classifiedMonthlyTotals(classified *models.TransactionSet) map[string]float64 {
	totals := make(map[string]float64)
	for m, set := range classified.GroupByMonth() {
		totals[m] = -set.SumAmount()
	}
	return totals
}

// handleKPIMonthDetail lists the transactions behind one row of the KPI
// detail modal's month table. The KPI's own date range is applied before the
// month is picked out, so a partially covered month (the first or last in the
// range) drills down to exactly the figure its row displays.
func handleKPIMonthDetail(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")
	month := chi.URLParam(r, "month")

	title, ok := kpiTitles[kpiType]
	if !ok {
		http.Error(w, "unknown KPI type", http.StatusBadRequest)
		return
	}
	if !monthKeyPattern.MatchString(month) {
		http.Error(w, "invalid month", http.StatusBadRequest)
		return
	}
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		http.Error(w, "invalid month", http.StatusBadRequest)
		return
	}

	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)

	isSavings := kpiType == "savings" || kpiType == "savings-rate"

	monthIncome := transactionsInMonth(filtered.FilterByType(models.Income), month)
	monthOutflow := transactionsInMonth(filtered.FilterByType(models.Outflow), month)

	// CB2 fix (amendment CB2-c, site 8): expenseTotal is the SIGNED negated
	// net of the month's outflow rows, matching the KD-signed living/
	// healthcare kinds in this SAME handler (ruling KD-2026-08-30d, "Living
	// Spent"/"Healthcare Spent" below) and handleKPIDetail's now-signed
	// expAmt. A refund-dominant month renders negative (a credit), keeping
	// this tile and the parent modal row agreeing exactly.
	incomeTotal := sumSigned(monthIncome)
	expenseTotal := -sumSigned(monthOutflow)

	var txns []models.Transaction
	var total float64
	var totalLabel string
	switch kpiType {
	case "income":
		txns = monthIncome
		expenseTotal = 0
		total, totalLabel = incomeTotal, "Total Income"
	case "expenses":
		txns = monthOutflow
		incomeTotal = 0
		total, totalLabel = expenseTotal, "Total Spent"
	case "living":
		// Classified set restricted to the month (single-source rule,
		// ruling 2026-08-29a) -- the SAME LivingOutflows helper the parent
		// modal and the dashboard card use, so this total equals the
		// parent row's figure exactly. Negated signed sum, NO per-month Abs
		// (ruling KD-2026-08-30d): a refund-dominant month renders negative,
		// matching the parent modal row (classifiedMonthlyTotals) exactly.
		monthLiving := transactionsInMonth(metrics.LivingOutflows(filtered.FilterByType(models.Outflow), planSyncExclusions(data.Active())), month)
		txns = monthLiving
		incomeTotal = 0
		expenseTotal = 0
		total, totalLabel = -sumSigned(monthLiving), "Living Spent"
	case "healthcare":
		// Negated signed sum, NO per-month Abs (ruling KD-2026-08-30d) --
		// see the "living" case above.
		monthHealthcare := transactionsInMonth(filtered.FilterByType(models.Outflow).FilterByCategory(metrics.HealthInsuranceCategory), month)
		txns = monthHealthcare
		incomeTotal = 0
		expenseTotal = 0
		total, totalLabel = -sumSigned(monthHealthcare), "Healthcare Spent"
	default:
		// Both savings KPIs are income minus expenses, so the drill-down
		// shows both sides -- and leaves transfers out, exactly as the
		// figure itself does.
		txns = append(append([]models.Transaction{}, monthIncome...), monthOutflow...)
		total, totalLabel = incomeTotal-expenseTotal, "Net"
	}

	sort.SliceStable(txns, func(i, j int) bool {
		return math.Abs(txns[i].Amount) > math.Abs(txns[j].Amount)
	})

	count := len(txns)
	var avgAmount float64
	if count > 0 {
		avgAmount = total / float64(count)
	}

	partialData := map[string]interface{}{
		"Type":         kpiType,
		"Title":        title,
		"Month":        month,
		"MonthLabel":   monthStart.Format("January 2006"),
		"Transactions": txns,
		"Count":        count,
		"Total":        total,
		"TotalLabel":   totalLabel,
		"AvgAmount":    avgAmount,
		"IncomeTotal":  incomeTotal,
		"ExpenseTotal": expenseTotal,
		"IsSavings":    isSavings,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "kpi-month-detail", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleKPIExport(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

	data, err := loader.LoadDataContext(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	income := filtered.FilterByType(models.Income)
	outflows := filtered.FilterByType(models.Outflow)

	// living/healthcare (K8): classify through the SAME helpers the modal
	// uses (single-source rule, ruling 2026-08-29a), then reduce to
	// per-month totals through classifiedMonthlyTotals -- the SAME shared
	// helper handleKPIDetail's month rows use -- so the CSV export and the
	// modal table can never disagree about a month's figure. planExclusions
	// comes from planSyncExclusions, the SAME helper handleKPIMonthDetail's
	// living case already calls for the same purpose; only the exclusion
	// map is needed here, not the full budget-target/coverage bundle
	// gatherDashboardCalcInputs assembles for the modal's "Per Month" tile.
	var monthlyLivingTotals, monthlyHealthcareTotals map[string]float64
	if kpiType == "living" || kpiType == "healthcare" {
		planExclusions := planSyncExclusions(data.Active())
		monthlyLivingTotals = classifiedMonthlyTotals(metrics.LivingOutflows(outflows, planExclusions))
		monthlyHealthcareTotals = classifiedMonthlyTotals(outflows.FilterByCategory(metrics.HealthInsuranceCategory))
	}

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

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

	// Build CSV
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header based on type. Living/healthcare column labels match the
	// modal table's own column header (kpi-detail.html).
	switch kpiType {
	case "income":
		_ = writer.Write([]string{"Month", "Income"})
	case "expenses":
		_ = writer.Write([]string{"Month", "Expenses"})
	case "savings":
		_ = writer.Write([]string{"Month", "Income", "Expenses", "Savings"})
	case "savings-rate":
		_ = writer.Write([]string{"Month", "Income", "Expenses", "Savings", "Savings Rate %"})
	case "living":
		_ = writer.Write([]string{"Month", "Living Expenses"})
	case "healthcare":
		_ = writer.Write([]string{"Month", "Healthcare"})
	}

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		// CB2 fix: expAmt is the SIGNED negated net of the month's outflow
		// bucket, matching monthlyLivingTotals/monthlyHealthcareTotals below
		// (ruling KD-2026-08-30d). A REFUND-DOMINANT month shows as a
		// NEGATIVE expense (a credit) in the CSV; math.Abs flipped this
		// sign. DERIVED savings/rate below need no change.
		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = -exp.SumAmount()
		}

		savings := incAmt - expAmt
		rate := 0.0
		if incAmt > 0 {
			rate = (savings / incAmt) * 100
		}

		switch kpiType {
		case "income":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt)})
		case "expenses":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", expAmt)})
		case "savings":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings)})
		case "savings-rate":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings), fmt.Sprintf("%.1f", rate)})
		case "living":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", monthlyLivingTotals[m])})
		case "healthcare":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", monthlyHealthcareTotals[m])})
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Printf("dashboard: building CSV export: %v", err)
		http.Error(w, "Failed to build export", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("%s_%s_to_%s.csv", kpiType, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write(buf.Bytes())
}

// Utility Functions

// resolveDateRange converts start/end query strings into time.Time values,
// defaulting to YTD start (clamped to data bounds) and maxDate end.
func resolveDateRange(startStr, endStr string, minDate, maxDate time.Time) (time.Time, time.Time) {
	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		// Default to YTD. Built in UTC because that is the calendar the
		// loader parses transaction dates on: a local midnight in a
		// negative-offset zone starts the window AFTER midnight UTC and
		// drops January 1 rows, while the filter beside it still reads
		// 01/01 and every drill-down (which posts explicit dates) counts
		// them.
		startDate = time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		// If YTD range starts after our data ends, default to all-time
		if !maxDate.IsZero() && startDate.After(maxDate) {
			startDate = minDate
		} else if startDate.Before(minDate) {
			startDate = minDate
		}
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}
	return startDate, endDate
}

// buildBudgetVsActualChartData renders a two-panel Plotly chart showing
// monthly Living + Healthcare actuals stacked against a combined budget
// target line (top panel) and the per-month running cumulative balance
// (bottom panel). Balance uses the savings convention: positive = ahead
// of budget (saved), negative = behind (overspent), opposite of the
// CombinedCumulativeDelta KPI field. Returns a payload with empty data
// when the combined target is 0 so the front end can branch on
// len(data)==0 to show its "Set a budget in What-If →" empty state.
//
// coverageStart/hasCoverage (from metrics.HealthcareCoverageStart, applied
// to the full active set -- never the range-filtered ts) clip the
// healthcare target's contribution to actual coverage via
// metrics.ClippedHealthcareMonths, the single clipping helper every
// healthcare-target accrual site goes through (split-classification rule,
// ruling 2026-08-29a). Living's contribution is never clipped.
//
// planExclusions (SY4, from planSyncExclusions applied to the full active
// set) is passed to metrics.LivingOutflows -- the SAME helper
// metrics.Calculate uses -- so both the Living bar values and the
// cumulative-balance walk's spend term run the ordinary |sum| arithmetic
// directly on the ALREADY-excluded transaction set (ruling SY-2026-08-30d:
// set exclusion, never an arithmetic subtraction of a separately-computed
// exclusion amount), never re-implementing the HI-first ordering locally.
func buildBudgetVsActualChartData(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, livingTarget, healthcareTarget float64, coverageStart time.Time, hasCoverage bool, planExclusions map[string]models.MajorExpense) map[string]interface{} {
	// rawCombinedTarget gates emptiness the same way metrics.Calculate's
	// CombinedTarget/HasCombinedTarget do: the raw monthly-rate sum,
	// unaffected by coverage timing -- it answers "is a target configured
	// at all", not "is it accruing this window".
	rawCombinedTarget := livingTarget + healthcareTarget
	if rawCombinedTarget <= 0 {
		return map[string]interface{}{
			"data":   []map[string]interface{}{},
			"layout": map[string]interface{}{},
		}
	}

	// monthsInRange/coverageMonths feed the dashed target line's prorated
	// monthly average -- (livingTarget*monthsInRange +
	// healthcareTarget*coverageMonths) / monthsInRange -- via the same
	// clipping helper metrics.Calculate uses, over the full [rangeStart,
	// rangeEnd] window (not per calendar month).
	monthsInRange := metrics.MonthsBetween(rangeStart, rangeEnd)
	coverageMonths := metrics.ClippedHealthcareMonths(rangeStart, rangeEnd, coverageStart, hasCoverage)
	combinedTarget := rawCombinedTarget
	if monthsInRange > 0 {
		combinedTarget = (livingTarget*monthsInRange + healthcareTarget*coverageMonths) / monthsInRange
	}

	outflows := ts.FilterByType(models.Outflow)
	healthcareOutflows := outflows.FilterByCategory(metrics.HealthInsuranceCategory)
	livingOutflows := metrics.LivingOutflows(outflows, planExclusions)
	monthlyOutflows := outflows.GroupByMonth()
	monthlyHealthcare := healthcareOutflows.GroupByMonth()
	monthlyLiving := livingOutflows.GroupByMonth()
	// nonExcludedOutflows is the cumulative-balance walk's spend basis:
	// every outflow EXCEPT plan-sync-excluded rows -- HI stays in, since
	// the walk nets living+healthcare together against combinedTarget.
	// Ruling SY-2026-08-30e: master's identity livingMonth = expAmt - hcAmt
	// let the walk sum |living|+|hc| and have it cancel back to the
	// month's true combined |sum| exactly; once livingMonth became an
	// INDEPENDENT |LivingOutflows bucket| (ruling SY-2026-08-30d), that
	// cancellation broke -- |a|+|b| != |a+b| whenever a and b diverge in
	// sign. The walk must merge the two buckets (living-remainder rows +
	// HI rows, already classified above -- not a third classifier) and
	// take ONE signed negation of the combined sum (CB1: -sum, not Abs --
	// a refund-dominant combined bucket must enter the walk as a credit,
	// never be charged as spend), mirroring metrics.go's own combined-walk
	// fix exactly.
	nonExcludedOutflows := &models.TransactionSet{
		Transactions: append(append([]models.Transaction{}, livingOutflows.Transactions...), healthcareOutflows.Transactions...),
	}
	monthlyNonExcluded := nonExcludedOutflows.GroupByMonth()

	monthSet := make(map[string]bool)
	for m := range monthlyOutflows {
		monthSet[m] = true
	}
	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	livingValues := make([]float64, 0, len(months))
	healthcareValues := make([]float64, 0, len(months))
	cumulativeValues := make([]float64, 0, len(months))

	var running float64
	for _, m := range months {
		// CB2 fix: hcAmt is the SIGNED negated net of the month's
		// healthcare bucket, not math.Abs. An ordinary month nets
		// outflow-negative, so -SumAmount() is positive spend, same as the
		// old math.Abs. A REFUND-DOMINANT month -- one whose outflow-typed
		// rows net POSITIVE -- shows as a NEGATIVE bar (a credit); math.Abs
		// flipped this sign.
		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = -hc.SumAmount()
		}
		// Set exclusion (ruling SY-2026-08-30d; signed per CB2): the
		// signed negated net runs directly on livingOutflows' month bucket
		// (HI and flagged rows already removed), never an arithmetic
		// subtraction from the month's raw outflow total -- that shape
		// breaks whenever the REMAINDER itself nets a refund, independent
		// of the flagged group's own sign.
		livingMonth := 0.0
		if lo, ok := monthlyLiving[m]; ok {
			livingMonth = -lo.SumAmount()
		}

		livingValues = append(livingValues, livingMonth)
		healthcareValues = append(healthcareValues, hcAmt)

		// Per-month accrual: living's share stays the flat monthly rate
		// (unchanged from master); healthcare's share clips to actual
		// coverage via the same helper metrics.Calculate uses, applied to
		// this calendar month's own segment and normalized to a fraction
		// of that month (1.0 = fully covered, 0.0 = not covered yet,
		// fractional when coverage starts mid-month). A full-coverage
		// fixture (coverageStart before every month) always yields
		// fraction 1.0, reproducing master's flat-monthly-target sum.
		monthStart, _ := time.Parse("2006-01", m)
		monthStart = time.Date(monthStart.Year(), monthStart.Month(), 1, 0, 0, 0, 0, rangeStart.Location())
		monthEnd := monthStart.AddDate(0, 1, 0).AddDate(0, 0, -1)
		monthLen := metrics.MonthsBetween(monthStart, monthEnd)
		healthcareFraction := 0.0
		if monthLen > 0 {
			healthcareFraction = metrics.ClippedHealthcareMonths(monthStart, monthEnd, coverageStart, hasCoverage) / monthLen
		}
		monthTarget := livingTarget + healthcareTarget*healthcareFraction

		// Set exclusion (ruling SY-2026-08-30e): spend is the SIGNED
		// negated net of the merged non-excluded bucket's sum for this
		// month -- NEVER livingMonth+hcAmt (two independent Abs values do
		// not recombine to the month's true combined signed sum once each
		// is its own bucket; see nonExcludedOutflows' doc above).
		//
		// CB1 fix: mirrors metrics.go's combined-walk fix exactly (both
		// surfaces move together -- see plan_exclusions_chart_walk_test.go
		// for the chart-vs-metrics equality this depends on). An ordinary
		// month's non-excluded outflows net negative, so -SumAmount() is
		// positive spend, same as the old math.Abs. A REFUND-DOMINANT
		// month -- non-excluded outflows netting POSITIVE, e.g. a cruise
		// refund larger than the month's spending -- must enter the walk
		// as a CREDIT (running += monthTarget - spend then ADDS the net
		// refund), not be charged as spend. math.Abs flipped this sign.
		spend := 0.0
		if bucket, ok := monthlyNonExcluded[m]; ok {
			spend = -bucket.SumAmount()
		}

		running += monthTarget - spend
		cumulativeValues = append(cumulativeValues, running)
	}

	livingTrace := map[string]interface{}{
		"type": "bar",
		"name": "Living",
		"x":    months,
		"y":    livingValues,
		"marker": map[string]interface{}{
			"color": "#9ca3af", // gray — matches Living card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	healthcareTrace := map[string]interface{}{
		"type": "bar",
		"name": "Healthcare",
		"x":    months,
		"y":    healthcareValues,
		"marker": map[string]interface{}{
			"color": "#e11d48", // rose — matches Healthcare card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	cumulativeTrace := map[string]interface{}{
		"type": "scatter",
		"mode": "lines+markers",
		"name": "Cumulative balance",
		"x":    months,
		"y":    cumulativeValues,
		"line": map[string]interface{}{
			"color": "#6366f1",
			"width": 2,
		},
		"fill":      "tozeroy",
		"fillcolor": "rgba(99, 102, 241, 0.2)",
		"xaxis":     "x2",
		"yaxis":     "y2",
	}

	layout := map[string]interface{}{
		"barmode":    "stack",
		"showlegend": true,
		"legend": map[string]interface{}{
			"orientation": "h",
			"y":           1.12,
		},
		"grid": map[string]interface{}{
			"rows":    2,
			"columns": 1,
			"pattern": "independent",
		},
		"xaxis": map[string]interface{}{
			"showticklabels": false,
		},
		"yaxis": map[string]interface{}{
			"title":  map[string]interface{}{"text": "Monthly $"},
			"domain": []float64{0.55, 1.0},
		},
		"xaxis2": map[string]interface{}{
			"anchor": "y2",
		},
		"yaxis2": map[string]interface{}{
			"title":         map[string]interface{}{"text": "Cumulative balance $"},
			"domain":        []float64{0.0, 0.42},
			"zeroline":      true,
			"zerolinecolor": "#6b7280",
			"zerolinewidth": 2,
		},
		"shapes": []map[string]interface{}{
			{
				// Combined target line on top subplot
				"type": "line",
				"xref": "paper",
				"x0":   0,
				"x1":   1,
				"yref": "y",
				"y0":   combinedTarget,
				"y1":   combinedTarget,
				"line": map[string]interface{}{
					"color": "#6b7280",
					"width": 2,
					"dash":  "dash",
				},
			},
		},
		"annotations": []map[string]interface{}{
			{
				"xref":      "paper",
				"yref":      "y",
				"x":         1,
				"xanchor":   "right",
				"y":         combinedTarget,
				"yanchor":   "bottom",
				"text":      fmt.Sprintf("Target $%.0f", combinedTarget),
				"showarrow": false,
				"font": map[string]interface{}{
					"color": "#6b7280",
					"size":  11,
				},
			},
		},
	}

	return map[string]interface{}{
		"data":   []map[string]interface{}{livingTrace, healthcareTrace, cumulativeTrace},
		"layout": layout,
	}
}

// majorExpenseDonutLimit caps the number of individual wedges shown in
// the Spending by Major Expense donut. Anything past this is rolled
// into a single "Other" wedge with a text breakdown beneath the chart.
// The drilldown handler reads the same constant so clicking "Other"
// returns exactly the rolled-up tail.
const majorExpenseDonutLimit = 8

// majorExpenseBucket is one wedge candidate: a named major expense plus
// every outflow that landed in it (after pin overrides + keyword/amount
// matching) and the pre-summed absolute total used for sorting.
type majorExpenseBucket struct {
	name  string
	txns  []models.Transaction
	total float64
}

// bucketMajorExpenses groups outflows by user-declared major expense
// and returns the sorted (descending by total) bucket list plus the
// unmatched outflows. Both the chart builder and the drilldown handler
// call this so they always agree on which transactions belong to which
// wedge — including pin overrides.
//
// This is the SHARED SOURCE (design ruling CB4-2026-09-02a): it returns
// COMPLETE data — every matched group with at least one transaction,
// regardless of the sign of its total. A group can legitimately net to
// zero or negative (refunds exceeding spending in the window); that is
// real, meaningful data and the drilldown for it is still useful. Any
// surface that cannot display a non-positive figure (e.g. a pie wedge)
// applies that constraint itself, locally, documented at the call site —
// it must not creep back in here and hide data from every consumer.
func bucketMajorExpenses(ts *models.TransactionSet) (buckets []majorExpenseBucket, unmatched []models.Transaction) {
	if ts == nil {
		return nil, nil
	}
	outflows := ts.FilterByType(models.Outflow)

	// Best-effort load — empty config (or no loader during unit tests)
	// just means everything goes in "Unmatched", which is a perfectly
	// fine empty state.
	var expenses []models.MajorExpense
	var pins map[string]string
	if loader != nil {
		expenses, _ = loader.LoadMajorExpenses()
		pins, _ = loader.LoadTransactionPins()
	}

	match := majorexpenses.Match(outflows, expenses, majorexpenses.MatchOptions{Pins: pins})

	expenseByID := make(map[string]models.MajorExpense, len(expenses))
	for _, e := range expenses {
		expenseByID[e.ID] = e
	}

	for id, txns := range match.Groups {
		name := expenseByID[id].Name
		if name == "" {
			continue // expense was deleted between Match and lookup; skip
		}
		var total float64
		for _, t := range txns {
			// Net spend: refunds (positive Outflow amounts per classifier
			// convention) reduce the bucket instead of inflating it via
			// AbsAmount. Mirrors the Major Expenses page.
			total += -t.Amount
		}
		// Include every matched group with >=1 transaction, whatever the
		// sign of its total (CB4-2026-09-02a). match.Groups only ever
		// holds groups that received at least one transaction, so no
		// separate "has transactions" check is needed here.
		buckets = append(buckets, majorExpenseBucket{name: name, txns: txns, total: total})
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].total > buckets[j].total })
	return buckets, match.Unmatched
}

// splitPositiveMajorExpenseBuckets partitions an already total-descending
// bucket list (bucketMajorExpenses' output) into the positive-total
// buckets and the zero/negative-total ones. Because the input is sorted
// descending, every positive bucket sorts strictly before every
// non-positive one, so the positive buckets are always a contiguous
// prefix — a single linear scan finds the split point.
//
// Both buildMajorExpenseChartData (the donut) and
// handleMajorExpenseDrilldown's "Other" lookup call this so they agree
// on exactly which buckets are display-eligible; see CB4-2026-09-02a.
func splitPositiveMajorExpenseBuckets(buckets []majorExpenseBucket) (positive, credits []majorExpenseBucket) {
	for i, b := range buckets {
		if b.total <= 0 {
			return buckets[:i], buckets[i:]
		}
	}
	return buckets, nil
}

// buildMajorExpenseChartData renders a pie chart of outflow spending
// grouped by user-declared major expense for the date-filtered window.
// Transactions that don't match any major expense are bucketed under
// "Unmatched" so the totals add up to the period's total outflows.
//
// To keep the donut readable when many small buckets exist, only the
// top majorExpenseDonutLimit matched buckets are kept as individual
// wedges; the rest are rolled into a single "Other" wedge. The list
// of rolled-up items is returned alongside the chart in the "smaller"
// field so the client can render a text breakdown.
//
// Pie geometry cannot render a wedge with a zero or negative value, so
// this is where that constraint is applied (CB4-2026-09-02a) — locally,
// on top of bucketMajorExpenses' complete data. Net-negative and
// zero-total buckets are excluded from the donut, the "Other" rollup,
// and "smaller" entirely (folding a negative into "Other" would
// understate that wedge's true size); they are returned instead in the
// "credits" field, a flat {name, amount} list — no percent, since a
// percentage of a negative figure against a positive grand total is
// meaningless. The donut-limit slice is applied to the positive buckets
// ONLY, after the sign filter, so negative buckets at the sort tail can
// never eat a display slot or leak into "Other".
func buildMajorExpenseChartData(ts *models.TransactionSet) map[string]interface{} {
	buckets, unmatchedTxns := bucketMajorExpenses(ts)
	positiveBuckets, creditBuckets := splitPositiveMajorExpenseBuckets(buckets)

	var unmatchedTotal float64
	for _, t := range unmatchedTxns {
		// Net spend, consistent with bucketMajorExpenses.
		unmatchedTotal += -t.Amount
	}

	type wedge struct {
		name string
		val  float64
	}
	display := make([]wedge, 0, len(positiveBuckets)+2)
	var rolledUp []wedge
	if len(positiveBuckets) > majorExpenseDonutLimit {
		for _, b := range positiveBuckets[:majorExpenseDonutLimit] {
			display = append(display, wedge{b.name, b.total})
		}
		var otherTotal float64
		for _, b := range positiveBuckets[majorExpenseDonutLimit:] {
			rolledUp = append(rolledUp, wedge{b.name, b.total})
			otherTotal += b.total
		}
		display = append(display, wedge{"Other", otherTotal})
	} else {
		for _, b := range positiveBuckets {
			display = append(display, wedge{b.name, b.total})
		}
	}

	if unmatchedTotal > 0 {
		display = append(display, wedge{"Unmatched", unmatchedTotal})
	}

	labels := make([]string, len(display))
	values := make([]float64, len(display))
	var grandTotal float64
	for i, w := range display {
		labels[i] = w.name
		values[i] = w.val
		grandTotal += w.val
	}

	out := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":   "pie",
				"labels": labels,
				"values": values,
				"hole":   0.4,
			},
		},
	}

	if len(rolledUp) > 0 {
		smaller := make([]map[string]interface{}, 0, len(rolledUp))
		for _, w := range rolledUp {
			pct := 0.0
			if grandTotal > 0 {
				pct = w.val / grandTotal * 100
			}
			// One decimal for ≥ 1%, two decimals for < 1% so that a
			// 0.45% slice does not display as "0%".
			if pct < 1 {
				pct = math.Round(pct*100) / 100
			} else {
				pct = math.Round(pct*10) / 10
			}
			smaller = append(smaller, map[string]interface{}{
				"name":    w.name,
				"amount":  w.val,
				"percent": pct,
			})
		}
		out["smaller"] = smaller
	}

	// Unmatched follows the same completeness contract as matched groups
	// (CB5, extending CB4-2026-09-02a): a positive total is a wedge
	// (appended above); a net-refund or zero total WITH transactions must
	// not vanish — it joins the credits list, after the matched credit
	// buckets. The pre-CB4 `> 0` wedge guard used to be Unmatched's only
	// path into the payload, silently dropping a refund-dominant window.
	unmatchedCredit := len(unmatchedTxns) > 0 && unmatchedTotal <= 0

	if len(creditBuckets) > 0 || unmatchedCredit {
		credits := make([]map[string]interface{}, 0, len(creditBuckets)+1)
		for _, b := range creditBuckets {
			credits = append(credits, map[string]interface{}{
				"name":   b.name,
				"amount": b.total,
			})
		}
		if unmatchedCredit {
			credits = append(credits, map[string]interface{}{
				"name":   "Unmatched",
				"amount": unmatchedTotal,
			})
		}
		out["credits"] = credits
	}

	return out
}

func buildSpendingTrendChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)
	monthlyOutflowSets := outflows.GroupByMonth()

	// Build sorted month list and per-month signed negated totals
	var months []string
	for m := range monthlyOutflowSets {
		months = append(months, m)
	}
	sort.Strings(months)

	// CB2 fix (amendment CB2-c, site 9): the SIGNED negated net of each
	// month's outflow bucket, not math.Abs. A refund-dominant month is
	// negative (a credit), so the %-change below can swing PAST zero into
	// negative territory instead of being clamped to a smaller positive
	// decrease. The `prev > 0` guard just below is UNCHANGED: a
	// refund-dominant month used as the BASE (prev <= 0) still renders 0%
	// -- an honest degradation, since "percent change off a credit" has no
	// sensible sign convention, not a defect this fix addresses.
	monthlyTotals := make(map[string]float64, len(months))
	for _, m := range months {
		monthlyTotals[m] = -monthlyOutflowSets[m].SumAmount()
	}

	// Need at least 2 months to show change
	if len(months) < 2 {
		return map[string]interface{}{
			"data":   []map[string]interface{}{},
			"layout": map[string]interface{}{},
		}
	}

	// Calculate month-over-month percentage change
	var changeMonths []string
	var changeValues []float64
	var colors []string
	var currAmounts []float64
	var prevAmounts []float64

	for i := 1; i < len(months); i++ {
		prev := monthlyTotals[months[i-1]]
		curr := monthlyTotals[months[i]]

		var pctChange float64
		if prev > 0 {
			pctChange = ((curr - prev) / prev) * 100
		}

		changeMonths = append(changeMonths, months[i])
		changeValues = append(changeValues, pctChange)
		currAmounts = append(currAmounts, curr)
		prevAmounts = append(prevAmounts, prev)
		if pctChange <= 0 {
			colors = append(colors, "#22c55e") // green = spending decreased
		} else {
			colors = append(colors, "#ef4444") // red = spending increased
		}
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "bar",
				"name": "Spending Change",
				"x":    changeMonths,
				"y":    changeValues,
				"customdata": func() [][]float64 {
					var cd [][]float64
					for i := range currAmounts {
						cd = append(cd, []float64{currAmounts[i], prevAmounts[i]})
					}
					return cd
				}(),
				"marker": map[string]interface{}{
					"color": colors,
				},
				"hovertemplate": "<b>%{x}</b><br>Spent: $%{customdata[0]:,.0f}<br>Prior: $%{customdata[1]:,.0f}<br>Change: %{y:+.1f}%<extra></extra>",
			},
		},
		"layout": map[string]interface{}{
			"yaxis": map[string]interface{}{
				"title":      "Change (%)",
				"ticksuffix": "%",
			},
		},
	}
}

func buildMerchantsChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)

	// Group by description (merchant). Signed net (CB3-B): refunds
	// (positive Outflow amounts per classifier convention) net against
	// the merchant instead of inflating it via AbsAmount; a net-refund
	// merchant renders a negative bar. Ordering stays by total desc.
	merchantTotals := make(map[string]float64)
	for _, t := range outflows.Transactions {
		merchantTotals[t.Label()] -= t.Amount
	}

	// Sort by value
	type merchVal struct {
		name string
		val  float64
	}
	var sorted []merchVal
	for name, val := range merchantTotals {
		sorted = append(sorted, merchVal{name, val})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].val > sorted[j].val
	})

	// Take top 10
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}

	// Reverse for horizontal bar chart
	var labels []string
	var values []float64
	for i := len(sorted) - 1; i >= 0; i-- {
		labels = append(labels, sorted[i].name)
		values = append(values, sorted[i].val)
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":        "bar",
				"orientation": "h",
				"x":           values,
				"y":           labels,
				"marker": map[string]string{
					"color": "#8b5cf6",
				},
			},
		},
	}
}

func buildCumulativeChartData(ts *models.TransactionSet) map[string]interface{} {
	sorted := ts.SortByDate()
	daily := sorted.GroupByDate()

	// Sort dates
	var dates []string
	for d := range daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var dateLabels []string
	var cumulative []float64
	var runningTotal float64

	for _, d := range dates {
		var dayTotal float64
		for _, t := range daily[d].Transactions {
			// A transfer is neither income nor expense (GLOSSARY:
			// "Transfer"). This loop's else branch is the only
			// income/expense consumer in the app that is not a
			// FilterByType call, so without this skip every transfer
			// leg would be subtracted from cumulative cash flow --
			// and a paired transfer would be subtracted twice.
			if t.TransactionType == models.Transfer {
				continue
			}
			// Signed accumulation (CB3-C): the classifier normalizes
			// purchase amounts negative and keeps refunds (non-income
			// credits) positive, so a single `+= t.Amount` is correct
			// for every non-Transfer row -- income adds, a purchase
			// subtracts, and an outflow-typed refund correctly ADDS to
			// cash flow. This also reshapes the Income branch: a
			// negative-amount Income row (an income reversal/chargeback)
			// now correctly SUBTRACTS from cash flow instead of being
			// forced positive by AbsAmount.
			dayTotal += t.Amount
		}
		runningTotal += dayTotal
		dateLabels = append(dateLabels, d)
		cumulative = append(cumulative, runningTotal)
	}

	// Determine line color based on final value
	lineColor := "#22c55e"
	fillColor := "rgba(34, 197, 94, 0.1)"
	if runningTotal < 0 {
		lineColor = "#ef4444"
		fillColor = "rgba(239, 68, 68, 0.1)"
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "scatter",
				"mode": "lines",
				"name": "Cumulative Balance",
				"x":    dateLabels,
				"y":    cumulative,
				"line": map[string]interface{}{
					"color": lineColor,
					"width": 2,
				},
				"fill":      "tozeroy",
				"fillcolor": fillColor,
			},
		},
		"layout": map[string]interface{}{
			"yaxis": map[string]interface{}{
				"title": "Cumulative ($)",
			},
		},
	}
}
