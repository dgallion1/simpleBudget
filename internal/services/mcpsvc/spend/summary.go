package spend

import (
	"context"
	"math"
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
	CumulativeDelta  float64 `json:"cumulative_delta"`
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
// income would otherwise show up as a spurious "category".
func byCategoryRows(ts *models.TransactionSet, topN int) []namedAmount {
	totals := ts.FilterByType(models.Outflow).CategoryTotals()
	rows := make([]namedAmount, 0, len(totals))
	for cat, amt := range totals {
		rows = append(rows, namedAmount{Category: cat, Amount: round2(amt)})
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
// sums the signed amount (expenses negative), so the result is negated to
// report a positive dollar figure, matching search_transactions' convention
// that this tool's figures are always positive for expenses.
func byMonthRows(ts *models.TransactionSet) []namedAmount {
	totals := ts.FilterByType(models.Outflow).MonthlyTotals()
	months := make([]string, 0, len(totals))
	for m := range totals {
		months = append(months, m)
	}
	sort.Strings(months)
	rows := make([]namedAmount, 0, len(months))
	for _, m := range months {
		rows = append(rows, namedAmount{Month: m, Amount: round2(math.Abs(totals[m]))})
	}
	return rows
}

// byMerchantRows groups ts's outflows via merchants.GroupTransactions --
// the same fuzzy matching get_anomalies and get_price_creep already use, so
// "SAFEWAY #123" and "SAFEWAY #456" count as one merchant here too -- and
// returns each group's total (absolute dollars) and transaction count,
// sorted by amount descending, truncated to topN.
func byMerchantRows(ts *models.TransactionSet, topN int) []namedAmount {
	groups := merchants.GroupTransactions(ts.FilterByType(models.Outflow).Transactions)
	rows := make([]namedAmount, 0, len(groups))
	for _, txns := range groups {
		var total float64
		for _, t := range txns {
			total += math.Abs(t.Amount)
		}
		rows = append(rows, namedAmount{
			Merchant: merchants.DisplayLabel(txns),
			Amount:   round2(total),
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
		Description: "Totals for the transaction history over an optional date window: income, expenses, " +
			"net savings and savings rate, plus breakdowns by category, by merchant, and by month. Expense " +
			"figures here are POSITIVE dollar amounts (unlike search_transactions, which returns signed " +
			"amounts). Merchants are grouped by the same fuzzy matching used by get_anomalies, so " +
			"\"SAFEWAY #123\" and \"SAFEWAY #456\" count as one merchant. by_category and by_merchant are " +
			"limited to top_n entries (default 10) sorted by amount; by_month is always complete. The budget " +
			"block appears only when a retirement plan with a spending target is configured, and compares " +
			"actual monthly spending against that plan's target for this window, with healthcare tracked " +
			"separately from living expenses.",
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

		from, to := start, end
		if from == nil {
			min := ts.MinDate()
			from = &min
		}
		if to == nil {
			max := ts.MaxDate()
			to = &max
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

		m := metrics.Calculate(filtered, *from, *to, livingTarget, healthTarget)

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
		// unset target would read as a 100% overrun, so both must be
		// positive before the block is populated at all.
		if m.HasBudgetTarget || m.HasHealthcareTarget {
			out.Budget = &budgetView{
				LivingTarget:     round2(m.BudgetTarget),
				LivingActual:     round2(m.ActualMonthly),
				LivingDelta:      round2(m.PerMonthDelta),
				HealthcareTarget: round2(m.HealthcareTarget),
				HealthcareActual: round2(m.HealthcareActual),
				HealthcareDelta:  round2(m.HealthcarePerMonthDelta),
				MonthsInRange:    round2(m.MonthsInRange),
				CumulativeDelta:  round2(m.CombinedCumulativeDelta),
			}
		}

		return nil, out, nil
	})
}
