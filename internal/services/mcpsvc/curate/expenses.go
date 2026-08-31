package curate

import (
	"context"
	"sort"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listExpensesInput struct {
	StartDate           string `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD; defaults to the first transaction on record"`
	EndDate             string `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD; defaults to the last transaction on record"`
	IncludeTransactions bool   `json:"include_transactions,omitempty" jsonschema:"return each expense's matched transactions with their hashes, not just the counts"`
	IncludeDeleted      bool   `json:"include_deleted,omitempty" jsonschema:"also return the soft-deleted expenses that can still be restored"`
}

type majorExpenseRow struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Keywords            []string      `json:"keywords"`
	ExpectedMin         float64       `json:"expected_min"`
	ExpectedMax         float64       `json:"expected_max"`
	Notes               string        `json:"notes,omitempty"`
	IsInternalTransfer  bool          `json:"is_internal_transfer"`
	ExcludeFromPlanSync bool          `json:"exclude_from_plan_sync"`
	Count               int           `json:"count"`
	PinnedCount         int           `json:"pinned_count"`
	Total               float64       `json:"total"`
	Transactions        []pinnableRow `json:"transactions,omitempty"`
}

type deletedExpenseRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DeletedAt    string `json:"deleted_at"`
	PinnedHashes int    `json:"pinned_hashes"`
}

type listExpensesOutput struct {
	Start          string              `json:"start"`
	End            string              `json:"end"`
	Expenses       []majorExpenseRow   `json:"expenses"`
	Deleted        []deletedExpenseRow `json:"deleted,omitempty"`
	TotalDeclared  float64             `json:"total_declared"`
	UnmatchedCount int                 `json:"unmatched_count"`
	UnmatchedTotal float64             `json:"unmatched_total"`
	Note           string              `json:"note,omitempty"`
}

func registerListExpenses(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_major_expenses",
		Description: "List the declared major expenses -- the user's own labels for spending they already " +
			"understand -- with how many transactions matched each one in the window and what they came to. " +
			"A transaction matches an expense by keyword, by amount range, or because the user pinned it there " +
			"explicitly; a pin overrides the other two. Only OUTFLOWS are matched, so income whose description " +
			"happens to contain a keyword is never counted. Window defaults to the full transaction history. " +
			"`total` and `total_declared` are NET SPEND and are normally POSITIVE: a refund inside a group " +
			"REDUCES its total, and a group whose refunds outweigh its purchases has a negative total. The " +
			"per-transaction `amount` under include_transactions is the opposite -- SIGNED exactly as stored, " +
			"so a purchase is negative and a refund positive, matching search_transactions. `unmatched_count` " +
			"and `unmatched_total` are the in-window outflows that matched nothing; that gap is why the app's " +
			"overall spending exceeds the declared total. Each returned transaction carries the `hash` that " +
			"pin_transactions needs; hashes come from date + lower-cased description + amount, so two " +
			"identical-looking transactions share one and pinning either pins both. Transactions the user has " +
			"already resolved as duplicates are excluded, matching every other aggregate in the app. This tool " +
			"reads only; upsert_major_expense and delete_major_expense are the writes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listExpensesInput) (res *mcp.CallToolResult, out listExpensesOutput, err error) {
		defer recoverToError("list_major_expenses", &err)

		v, err := deps.pageView(in.StartDate, in.EndDate)
		if err != nil {
			return nil, listExpensesOutput{}, err
		}

		rows := make([]majorExpenseRow, 0, len(v.Expenses))
		var totalDeclared float64
		for _, e := range v.Expenses {
			group := v.Match.Groups[e.ID]
			sorted := append([]models.Transaction(nil), group...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })

			var total float64
			pinned := 0
			txRows := make([]pinnableRow, 0, len(sorted))
			for _, t := range sorted {
				// Net spend: purchases are negative and refunds positive in
				// the classifier's Outflow convention, so negating the raw
				// amount makes spending positive and lets a refund subtract.
				total += -t.Amount
				if v.Match.PinnedHashes[t.Hash] {
					pinned++
				}
				if in.IncludeTransactions {
					txRows = append(txRows, rowFor(t, v.Match.PinnedHashes))
				}
			}
			totalDeclared += total

			keywords := e.Keywords
			if keywords == nil {
				keywords = []string{}
			}
			rows = append(rows, majorExpenseRow{
				ID:                  e.ID,
				Name:                e.Name,
				Keywords:            keywords,
				ExpectedMin:         e.ExpectedMin,
				ExpectedMax:         e.ExpectedMax,
				Notes:               e.Notes,
				IsInternalTransfer:  e.IsInternalTransfer,
				ExcludeFromPlanSync: e.ExcludeFromPlanSync,
				Count:               len(sorted),
				PinnedCount:         pinned,
				Total:               total,
				Transactions:        txRows,
			})
		}
		sort.Slice(rows, func(i, j int) bool {
			return strings.ToLower(strings.TrimSpace(rows[i].Name)) <
				strings.ToLower(strings.TrimSpace(rows[j].Name))
		})

		var unmatchedTotal float64
		for _, t := range v.Match.Unmatched {
			unmatchedTotal += -t.Amount
		}

		out = listExpensesOutput{
			Start:          formatDay(v.Start),
			End:            formatDay(v.End),
			Expenses:       rows,
			TotalDeclared:  totalDeclared,
			UnmatchedCount: len(v.Match.Unmatched),
			UnmatchedTotal: unmatchedTotal,
		}
		if len(rows) == 0 {
			out.Note = "no major expenses are declared yet; use upsert_major_expense to create one"
		}

		if in.IncludeDeleted {
			deleted, err := deps.Expenses.LoadDeletedMajorExpenses()
			if err != nil {
				return nil, listExpensesOutput{}, err
			}
			sort.Slice(deleted, func(i, j int) bool { return deleted[i].DeletedAt.After(deleted[j].DeletedAt) })
			for _, d := range deleted {
				out.Deleted = append(out.Deleted, deletedExpenseRow{
					ID:           d.Expense.ID,
					Name:         d.Expense.Name,
					DeletedAt:    d.DeletedAt.Format("2006-01-02"),
					PinnedHashes: len(d.PinnedHashes),
				})
			}
		}
		return nil, out, nil
	})
}
