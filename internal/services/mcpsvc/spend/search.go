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
	Search    string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the description"`
	Type      string  `json:"type,omitempty" jsonschema:"income or outflow; omit for both"`
	MinAmount float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include"`
	MaxAmount float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include"`
	Page      int     `json:"page,omitempty" jsonschema:"1-based page number; defaults to 1"`
	PerPage   int     `json:"per_page,omitempty" jsonschema:"rows per page, default 50, maximum 200"`
}

type transactionRow struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
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
			"of the description), type (income or outflow), and min_amount/max_amount (compared against the " +
			"ABSOLUTE dollar amount, so min_amount 100 matches a -150.00 expense). Amounts in the result are " +
			"SIGNED -- expenses are negative. Results are newest-first and paginated: `total` is the full number " +
			"of matching rows, not the number returned, so check it against the rows you received before " +
			"concluding you have seen everything. Default 50 rows per page, maximum 200. sum_amount is the " +
			"signed sum over ALL matches, not just this page.",
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
		default:
			return nil, searchOutput{}, fmt.Errorf("type %q is not recognized; use \"income\" or \"outflow\"", in.Type)
		}
		ts = filterByAbsAmount(ts, in.MinAmount, in.MaxAmount)

		total := ts.Len()
		sum := ts.SumAmount()

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
