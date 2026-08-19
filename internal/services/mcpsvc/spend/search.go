package spend

import (
	"context"
	"fmt"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSearchPerPage = 50
	maxSearchPerPage     = 200
)

type searchInput struct {
	StartDate string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Category  string  `json:"category,omitempty" jsonschema:"exact category name; omit for all categories"`
	Search    string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the transaction's description, display name, major-expense name, or enriched description; the returned description field may differ from whichever of these matched"`
	Type      string  `json:"type,omitempty" jsonschema:"income, outflow, or transfer (expense is accepted as an alias for outflow); omit for all three"`
	MinAmount float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include; 0 means unset, not \"greater than zero\""`
	MaxAmount float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include; 0 means unset"`
	Page      int     `json:"page,omitempty" jsonschema:"1-based page number; defaults to 1"`
	PerPage   int     `json:"per_page,omitempty" jsonschema:"rows per page, default 50, maximum 200"`
}

type transactionRow struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
	// Hash is the identifier the curation tools use to pin this transaction
	// to a major expense. It is derived from date + lower-cased description +
	// amount, so two distinct transactions sharing all three share one hash
	// and are pinned together.
	Hash string `json:"hash"`
}

type searchOutput struct {
	Total        int              `json:"total"`
	Page         int              `json:"page"`
	PerPage      int              `json:"per_page"`
	TotalPages   int              `json:"total_pages"`
	SumAmount    float64          `json:"sum_amount"`
	Transactions []transactionRow `json:"transactions"`
}

// filterByAbsAmount keeps rows whose absolute amount is within [min, max].
// A zero bound is "unset": amounts are dollars and a zero-dollar row carries
// no information, so treating 0 as unset costs nothing and lets both bounds
// be optional in the schema.
func filterByAbsAmount(ts *models.TransactionSet, min, max float64) *models.TransactionSet {
	if min == 0 && max == 0 {
		return ts
	}
	kept := make([]models.Transaction, 0, ts.Len())
	for _, t := range ts.Transactions {
		amt := t.Amount
		if amt < 0 {
			amt = -amt
		}
		if min > 0 && amt < min {
			continue
		}
		if max > 0 && amt > max {
			continue
		}
		kept = append(kept, t)
	}
	return models.NewTransactionSet(kept)
}

func registerSearch(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "search_transactions",
		Description: "Search the transaction history. Every filter is optional and they combine with AND: " +
			"start_date/end_date (inclusive, YYYY-MM-DD), category (exact), search (case-insensitive substring " +
			"matched against the transaction's description, its user-assigned display name, its major-expense " +
			"name, or any enriched description -- NOT against the `description` field in the results below, " +
			"which is the display label shown in the app and may differ from whichever field actually " +
			"matched), type (income, outflow, or transfer; \"expense\" is also accepted as an alias for " +
			"outflow; omit for all three), and min_amount/max_amount (compared against the ABSOLUTE dollar " +
			"amount, so min_amount 100 matches a -150.00 expense; 0 means the bound is unset, so there is " +
			"no way to express \"strictly greater than zero\"). Amounts in the result are SIGNED, but NOT " +
			"uniformly by type: an ordinary expense (type outflow) is negative, but a refund or credit is a " +
			"POSITIVE amount that is still typed outflow -- so filtering type=outflow does not mean every " +
			"amount returned is negative, and sum_amount over an outflow-only search nets those refunds " +
			"against spend rather than totaling raw spend. Results are newest-first " +
			"and paginated: `total` is the full number of matching rows, not the number returned, so check it " +
			"against the rows you received before concluding you have seen everything. Default 50 rows per " +
			"page, maximum 200. sum_amount is the signed sum over ALL matches, not just this page, and on " +
			"the default path (no type filter) it EXCLUDES Transfer rows even though they are still listed " +
			"(`total` and the page include them): a Transfer is neither income nor expense -- money moving " +
			"between the user's own accounts -- so netting it into a signed total would make this sum " +
			"disagree with summarize_spending's net_savings for the same window; with an explicit type " +
			"filter the filtered set is already one type, so the exclusion only matters on the default path. " +
			"Each row carries a `hash`, which is what pin_transactions uses to attach that transaction to a " +
			"major expense; the hash is derived from date + lower-cased description + amount, so two genuinely " +
			"distinct transactions that share all three share one hash and pinning either pins both. " +
			"Transactions the user has already marked as a resolved duplicate are excluded, matching every " +
			"other aggregate in the app (the dashboard, get_anomalies, get_price_creep, summarize_spending) " +
			"so sums here agree with those tools for the same window.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (res *mcp.CallToolResult, out searchOutput, err error) {
		defer recoverToError("search_transactions", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, searchOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, searchOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, searchOutput{}, err
		}
		// Suppressed rows are near-duplicates the user has already resolved
		// (dataloader's duplicate-resolution flow); every other aggregate in
		// the app -- the dashboard, get_anomalies, get_price_creep,
		// summarize_spending -- excludes them, so a search sum that included
		// them would silently disagree with all of those for the same
		// window. Excluded here for the same reason, not filtered later, so
		// MinDate/MaxDate fallbacks below are also active-only.
		ts = ts.Active()

		if start != nil || end != nil {
			from, to := start, end
			if from == nil {
				min := ts.MinDate()
				from = &min
			}
			if to == nil {
				max := ts.MaxDate()
				to = &max
			}
			ts = ts.FilterByDateRange(*from, *to)
		}
		if in.Category != "" {
			ts = ts.FilterByCategory(in.Category)
		}
		if in.Search != "" {
			ts = ts.FilterBySearch(in.Search)
		}
		switch strings.ToLower(in.Type) {
		case "":
		case "income":
			ts = ts.FilterByType(models.Income)
		case "outflow", "expense":
			ts = ts.FilterByType(models.Outflow)
		case "transfer":
			ts = ts.FilterByType(models.Transfer)
		default:
			return nil, searchOutput{}, fmt.Errorf("type %q is not recognized; use \"income\", \"outflow\", or \"transfer\"", in.Type)
		}
		ts = filterByAbsAmount(ts, in.MinAmount, in.MaxAmount)

		total := ts.Len()
		// sum_amount is the signed sum over the filtered rows. On the default
		// path (no type filter) Transfer rows are EXCLUDED from the sum even
		// though they are still listed (total and the page include them): a
		// Transfer is neither income nor expense -- money moving between the
		// user's own accounts -- so netting it into a signed total would make
		// this sum disagree with summarize_spending's net_savings for the same
		// window. With an explicit type filter the filtered set is already one
		// type, so the sum covers whatever that filter selected (including
		// transfer).
		var sum float64
		if in.Type == "" {
			sum = ts.FilterByType(models.Income).SumAmount() +
				ts.FilterByType(models.Outflow).SumAmount()
		} else {
			sum = ts.SumAmount()
		}

		perPage := in.PerPage
		if perPage <= 0 {
			perPage = defaultSearchPerPage
		}
		if perPage > maxSearchPerPage {
			perPage = maxSearchPerPage
		}
		page := in.Page
		if page <= 0 {
			page = 1
		}

		sorted := ts.SortByDateDesc()
		totalPages := sorted.TotalPages(perPage)
		paged := sorted.Paginate(page, perPage)

		rows := make([]transactionRow, 0, paged.Len())
		for _, t := range paged.Transactions {
			rows = append(rows, transactionRow{
				Date:        t.Date.Format("2006-01-02"),
				Description: t.Label(),
				Category:    t.Category,
				Amount:      t.Amount,
				Type:        string(t.TransactionType),
				Hash:        t.Hash,
			})
		}

		return nil, searchOutput{
			Total:        total,
			Page:         page,
			PerPage:      perPage,
			TotalPages:   totalPages,
			SumAmount:    round0(sum*100) / 100,
			Transactions: rows,
		}, nil
	})
}
