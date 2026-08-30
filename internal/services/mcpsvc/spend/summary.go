package spend

import (
	"context"
	"fmt"
	"sort"

	"budget2/internal/models"
	"budget2/internal/services/merchants"
	"budget2/internal/services/metrics"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultSummaryTopN is how many category/merchant rows summarize_spending
// returns when top_n is omitted.
const defaultSummaryTopN = 10

type summaryInput struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	TopN      int    `json:"top_n,omitempty" jsonschema:"how many categories and merchants to return, default 10"`
}

type namedAmount struct {
	Category string  `json:"category,omitempty"`
	Merchant string  `json:"merchant,omitempty"`
	Month    string  `json:"month,omitempty"`
	Amount   float64 `json:"amount"`
	Count    int     `json:"count,omitempty"`
}

type budgetView struct {
	LivingTarget     float64 `json:"living_monthly_target"`
	LivingActual     float64 `json:"living_monthly_actual"`
	LivingDelta      float64 `json:"living_monthly_delta"`
	HealthcareTarget float64 `json:"healthcare_monthly_target"`
	HealthcareActual float64 `json:"healthcare_monthly_actual"`
	HealthcareDelta  float64 `json:"healthcare_monthly_delta"`
	MonthsInRange    float64 `json:"months_in_range"`
	// CombinedCumulativeDelta is the net living+healthcare variance over the
	// whole window (actual minus target, summed across both categories) --
	// unlike LivingDelta/HealthcareDelta above, which are per-category
	// monthly averages, this is the combined total-dollar figure. Positive
	// means over budget.
	CombinedCumulativeDelta float64 `json:"combined_cumulative_delta"`
}

type summaryOutput struct {
	Start         string        `json:"start"`
	End           string        `json:"end"`
	TotalIncome   float64       `json:"total_income"`
	TotalExpenses float64       `json:"total_expenses"`
	NetSavings    float64       `json:"net_savings"`
	SavingsRate   float64       `json:"savings_rate"`
	ByCategory    []namedAmount `json:"by_category"`
	ByMerchant    []namedAmount `json:"by_merchant"`
	ByMonth       []namedAmount `json:"by_month"`
	Budget        *budgetView   `json:"budget,omitempty"`
}

// byCategoryRows returns expense totals by category over ts (already
// window-filtered), sorted by amount descending, truncated to topN. Only
// outflows count -- CategoryTotals sums every transaction it's given, so
// income would otherwise show up as a spurious "category". Amounts are
// summed SIGNED and negated here (rather than using CategoryTotals, which
// sums math.Abs per transaction) so this matches total_expenses'
// (metrics.Calculate's) convention: a positive-amount refund SUBTRACTS from
// a category's total instead of adding to it. See Finding 1 in the Phase 2
// review -- CategoryTotals' own math.Abs-per-transaction convention is still
// correct for its other (non-MCP) callers and is left unchanged.
//
// Known cosmetic artifact (left as-is): when a category's refunds exactly
// offset its spend, round2(-amt) can produce float64 negative zero, which
// encoding/json renders as the literal "-0" rather than "0". Same applies
// to byMonthRows/byMerchantRows below. Not fixed here because doing so is a
// computation change, not a documentation one, and the two are worth
// keeping separate in this pass.
func byCategoryRows(ts *models.TransactionSet, topN int) []namedAmount {
	outflows := ts.FilterByType(models.Outflow)
	totals := make(map[string]float64, outflows.Len())
	for _, t := range outflows.Transactions {
		cat := t.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		totals[cat] += t.Amount
	}
	rows := make([]namedAmount, 0, len(totals))
	for cat, amt := range totals {
		rows = append(rows, namedAmount{Category: cat, Amount: round2(-amt)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Amount != rows[j].Amount {
			return rows[i].Amount > rows[j].Amount
		}
		return rows[i].Category < rows[j].Category
	})
	if len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

// byMonthRows returns expense totals by month over ts (already
// window-filtered), sorted chronologically, never truncated. MonthlyTotals
// sums the signed amount (expenses negative, positive-amount refunds
// positive), and the negated (not math.Abs'd) total is reported: a month
// whose refunds exceed its spending is a real net-negative month, and
// math.Abs would flip its sign to look like ordinary positive spending. See
// Finding 1 in the Phase 2 review.
func byMonthRows(ts *models.TransactionSet) []namedAmount {
	totals := ts.FilterByType(models.Outflow).MonthlyTotals()
	months := make([]string, 0, len(totals))
	for m := range totals {
		months = append(months, m)
	}
	sort.Strings(months)
	rows := make([]namedAmount, 0, len(months))
	for _, m := range months {
		rows = append(rows, namedAmount{Month: m, Amount: round2(-totals[m])})
	}
	return rows
}

// byMerchantRows groups ts's outflows via merchants.GroupTransactions --
// the same fuzzy matching get_anomalies and get_price_creep already use, so
// "SAFEWAY #123" and "SAFEWAY #456" count as one merchant here too -- and
// returns each group's total and transaction count, sorted by amount
// descending, truncated to topN. The total is summed SIGNED and negated
// (not math.Abs per transaction) so a positive-amount refund subtracts
// rather than adds, matching total_expenses' convention. See Finding 1 in
// the Phase 2 review.
func byMerchantRows(ts *models.TransactionSet, topN int) []namedAmount {
	groups := merchants.GroupTransactions(ts.FilterByType(models.Outflow).Transactions)
	rows := make([]namedAmount, 0, len(groups))
	for _, txns := range groups {
		var total float64
		for _, t := range txns {
			total += t.Amount
		}
		rows = append(rows, namedAmount{
			Merchant: merchants.DisplayLabel(txns),
			Amount:   round2(-total),
			Count:    len(txns),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Amount != rows[j].Amount {
			return rows[i].Amount > rows[j].Amount
		}
		return rows[i].Merchant < rows[j].Merchant
	})
	if len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

// registerSummary adds summarize_spending to s.
func registerSummary(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "summarize_spending",
		Description: "Totals for the transaction history over an optional date window (default: the full " +
			"ledger, i.e. its earliest through latest transaction; a ledger with no transactions after " +
			"excluding resolved duplicates is a tool error rather than a fabricated 0001-01-01 window, " +
			"unless start_date and end_date are both given explicitly): total_income, total_expenses, " +
			"net_savings, and savings_rate (a PERCENTAGE, not a fraction -- 47 means 47%, not 4700%; it is " +
			"NEGATIVE whenever expenses exceed income, which is the ordinary case for a household living " +
			"off savings in retirement, not an error -- but it is DEFINED AS 0 whenever total_income is zero " +
			"or less, rather than negative or NaN, so a window with NO income reads savings_rate 0.0 even " +
			"alongside heavy spending; a 0 there means \"no income in this window\", NOT \"broke even\"), " +
			"plus breakdowns by category, by merchant, and by " +
			"month. by_category, by_merchant, and by_month are expenses only -- income is not broken out by " +
			"category/merchant/month, only in the top-level total_income. Amounts in by_category, " +
			"by_merchant, and by_month, and total_expenses itself, are normally POSITIVE dollar figures, but " +
			"can be NEGATIVE: refunds/credits are recorded in this ledger as positive-amount outflows, so " +
			"they subtract from whichever category/merchant/month they fall in, and if a category's, " +
			"merchant's, or month's refunds exceed its spend in this window that row goes negative rather " +
			"than clamping at zero. All four figures share this identical signed-sum-then-negate " +
			"convention, but do NOT assume summing a breakdown reproduces total_expenses: by_category and " +
			"by_merchant are each truncated to top_n (see below), so their sums only match total_expenses " +
			"when this window has top_n or fewer categories/merchants -- with more, the truncated sum is " +
			"necessarily LESS than total_expenses. by_month is never truncated, so summing it always matches " +
			"total_expenses in MAGNITUDE, but total_expenses is always non-negative (it is an absolute value) " +
			"while summing by_month can be negative -- if this window's refunds exceed its spending OVERALL " +
			"(not just in one category/merchant/month), total_expenses and the sum of by_month are the " +
			"negation of each other, not equal. (This differs from search_transactions, which returns every amount signed, " +
			"expenses negative -- the opposite sign convention.) Transactions the user has already marked " +
			"as a resolved duplicate are excluded, matching the dashboard and " +
			"get_anomalies/get_price_creep/search_transactions. Merchants are grouped by the same fuzzy-" +
			"matching ALGORITHM get_anomalies and get_price_creep use, so \"SAFEWAY #123\" and " +
			"\"SAFEWAY #456\" count as one merchant here too; the merchant name returned is lower-cased and " +
			"may not match any single transaction's description verbatim. The resulting GROUPS can still " +
			"differ from get_anomalies'/get_price_creep's groups, and from this tool's own groups in a " +
			"different window, because grouping runs over whatever transactions are in scope here (this " +
			"window's outflows, including positive-amount refunds) versus get_anomalies'/get_price_creep's " +
			"full active-outflow, negative-amount-only history -- the same merchant can get a different " +
			"display label or cluster boundary between tools, or between two calls to this tool with " +
			"different windows. count (number of transactions folded in) is populated for by_merchant only, " +
			"not by_category or by_month. by_category and by_merchant are limited to top_n entries (default " +
			"10) sorted by amount descending; by_month is always complete, sorted chronologically. The " +
			"budget block appears only when a retirement plan with a nonzero living or healthcare spending " +
			"target is configured -- it is omitted entirely, not zeroed, otherwise -- and compares actual " +
			"monthly spending against that plan's target for this window, with healthcare tracked " +
			"separately from living expenses; combined_cumulative_delta is the two categories' net dollar " +
			"variance over the whole window (positive = over budget). living_monthly_actual EXCLUDES the " +
			"\"Health Insurance\" category -- living is total expenses MINUS healthcare, so the two never " +
			"double-count the same premium, and living_monthly_actual + healthcare_monthly_actual is the " +
			"window's whole outflow pace. living_monthly_target is PHASE-ADJUSTED: when the plan has " +
			"spending phases enabled, each calendar month in the window contributes its own multiplier and " +
			"the target is their average, so it may not equal the plan's raw monthly living-expense " +
			"number, and a window straddling a phase boundary gets a blended target. " +
			"healthcare_monthly_target is deliberately NOT phase-adjusted -- it is today's planned premium " +
			"(premiums rise with age rather than falling with spending phases). months_in_range is the " +
			"window's inclusive DAY count divided by 30.4375 (the average calendar month), not a count of " +
			"whole months -- so it is fractional, and a short window yields a fraction well below 1.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in summaryInput) (res *mcp.CallToolResult, out summaryOutput, err error) {
		defer recoverToError("summarize_spending", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, summaryOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, summaryOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, summaryOutput{}, err
		}
		// Suppressed rows are near-duplicates the user has already resolved
		// (dataloader's duplicate-resolution flow); this tool wraps the same
		// metrics.Calculate the dashboard uses, so leaving them in would
		// make this tool's totals, savings rate, and budget verdict
		// contradict the dashboard screen for the same window. Excluding
		// them here (before MinDate/MaxDate) also keeps the default window
		// itself active-only, consistent with search_transactions.
		ts = ts.Active()

		// Coverage start is a lifetime fact about the ledger -- derived from
		// the full active set here, before the window filter below, never
		// from the range-filtered set (see metrics.HealthcareCoverageStart's
		// doc).
		coverageStart, hasCoverage := metrics.HealthcareCoverageStart(ts)

		from, to := start, end
		if from == nil || to == nil {
			// ts.MinDate()/MaxDate() are the zero time on an empty (post-
			// suppression) ledger; defaulting from them would silently
			// report the window as starting/ending 0001-01-01 instead of
			// surfacing that there is nothing to default from. Mirrors
			// get_trends' identical guard. Two fully explicit dates are
			// still a legitimate (if empty) request against an empty
			// ledger, so only the defaulting path needs this check.
			if ts.MaxDate().IsZero() {
				return nil, summaryOutput{}, fmt.Errorf(
					"cannot default the summary window: the ledger has no transactions (after excluding " +
						"suppressed rows); pass start_date and end_date explicitly")
			}
			if from == nil {
				min := ts.MinDate()
				from = &min
			}
			if to == nil {
				max := ts.MaxDate()
				to = &max
			}
		}
		filtered := ts.FilterByDateRange(*from, *to)

		topN := in.TopN
		if topN <= 0 {
			topN = defaultSummaryTopN
		}

		// Settings failing to load (or being unwired) is not fatal: the
		// totals below are still a correct answer, they just can't be
		// compared to a plan. living/healthTarget stay 0, which
		// metrics.Calculate and the HasBudgetTarget/HasHealthcareTarget
		// gate below both read as "no target set".
		var livingTarget, healthTarget float64
		if deps.Settings != nil {
			if settings, sErr := deps.Settings.Load(); sErr == nil {
				livingTarget, healthTarget = metrics.BudgetTargets(settings, *from, *to)
			}
		}

		m := metrics.Calculate(filtered, *from, *to, livingTarget, healthTarget, coverageStart, hasCoverage)

		out = summaryOutput{
			Start:         from.Format("2006-01-02"),
			End:           to.Format("2006-01-02"),
			TotalIncome:   round2(m.TotalIncome),
			TotalExpenses: round2(m.TotalExpenses),
			NetSavings:    round2(m.NetSavings),
			SavingsRate:   round2(m.SavingsRate),
			ByCategory:    byCategoryRows(filtered, topN),
			ByMerchant:    byMerchantRows(filtered, topN),
			ByMonth:       byMonthRows(filtered),
		}

		// A zero target means "unset" throughout this codebase (see
		// metrics.BudgetTargets); reporting a budget block against an
		// unset target would read as a 100% overrun, so EITHER target being
		// positive is enough to populate the block (a household can have a
		// living target with no healthcare target configured, or vice
		// versa) -- hence the || below, not &&.
		if m.HasBudgetTarget || m.HasHealthcareTarget {
			// HealthcareTarget/Actual/PerMonthDelta on m are populated
			// unconditionally by metrics.Calculate (HasHealthcareTarget is
			// the gate, not a zeroing of the fields themselves -- see its
			// doc) -- so a plan can have a healthcare target CONFIGURED
			// (m.HealthcareTarget > 0) while HasHealthcareTarget is false
			// because coverageMonths is 0 (no qualifying Health Insurance
			// transactions, or the window ends before coverage starts).
			// Copying those fields unconditionally would report a phantom
			// healthcare_monthly_target/delta (e.g. target:1000,
			// delta:-1000) for a category the dashboard correctly omits
			// entirely (kpis.html gates its Healthcare KPI on the same
			// flag). Zero them here to match that suppression; living
			// fields are unaffected -- HasBudgetTarget/LivingTarget/
			// Actual/Delta carry their own independent gate.
			var healthcareTarget, healthcareActual, healthcareDelta float64
			if m.HasHealthcareTarget {
				healthcareTarget = round2(m.HealthcareTarget)
				healthcareActual = round2(m.HealthcareActual)
				healthcareDelta = round2(m.HealthcarePerMonthDelta)
			}
			out.Budget = &budgetView{
				LivingTarget:            round2(m.BudgetTarget),
				LivingActual:            round2(m.ActualMonthly),
				LivingDelta:             round2(m.PerMonthDelta),
				HealthcareTarget:        healthcareTarget,
				HealthcareActual:        healthcareActual,
				HealthcareDelta:         healthcareDelta,
				MonthsInRange:           round2(m.MonthsInRange),
				CombinedCumulativeDelta: round2(m.CombinedCumulativeDelta),
			}
		}

		return nil, out, nil
	})
}
