package curate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultExceptionLimit = 50
	maxExceptionLimit     = 200
)

type listExceptionsInput struct {
	Bucket    string  `json:"bucket,omitempty" jsonschema:"which bucket to return: unmatched, anomalous, or new_merchants; omit for all three"`
	StartDate string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Search    string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the row's description"`
	MinAmount float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include; 0 means unset, not \"greater than zero\""`
	MaxAmount float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include; 0 means unset"`
	Limit     int     `json:"limit,omitempty" jsonschema:"rows per bucket, default 50, maximum 200; each bucket still reports its full total"`
}

type exceptionRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	// OverThreshold is set on unmatched rows at or above the notable-amount
	// threshold reported alongside the buckets.
	OverThreshold bool `json:"over_threshold,omitempty"`
	// The remaining fields are populated for anomalous rows only.
	MajorExpenseID   string  `json:"major_expense_id,omitempty"`
	MajorExpenseName string  `json:"major_expense_name,omitempty"`
	ExpectedMin      float64 `json:"expected_min,omitempty"`
	ExpectedMax      float64 `json:"expected_max,omitempty"`
	// FirstSeen is populated for new_merchants rows only.
	FirstSeen string `json:"first_seen,omitempty"`
}

type bucketView struct {
	Total    int            `json:"total"`
	Returned int            `json:"returned"`
	Rows     []exceptionRow `json:"rows"`
}

type listExceptionsOutput struct {
	Start                 string      `json:"start"`
	End                   string      `json:"end"`
	Threshold             float64     `json:"threshold"`
	NewMerchantWindowDays int         `json:"new_merchant_window_days"`
	Unmatched             *bucketView `json:"unmatched,omitempty"`
	Anomalous             *bucketView `json:"anomalous,omitempty"`
	NewMerchants          *bucketView `json:"new_merchants,omitempty"`
	Note                  string      `json:"note,omitempty"`
}

// keepRow applies the text and amount filters shared by all three buckets.
func keepRow(r exceptionRow, search string, min, max float64) bool {
	if search != "" && !strings.Contains(strings.ToLower(r.Description), strings.ToLower(search)) {
		return false
	}
	amt := r.Amount
	if amt < 0 {
		amt = -amt
	}
	if min > 0 && amt < min {
		return false
	}
	if max > 0 && amt > max {
		return false
	}
	return true
}

// buildBucket filters, reports the true match count, and returns at most
// limit rows. Total is the number that matched, not the number returned, so a
// caller cannot mistake a truncated list for the whole bucket.
func buildBucket(rows []exceptionRow, in listExceptionsInput, limit int) *bucketView {
	kept := make([]exceptionRow, 0, len(rows))
	for _, r := range rows {
		if keepRow(r, in.Search, in.MinAmount, in.MaxAmount) {
			kept = append(kept, r)
		}
	}
	total := len(kept)
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return &bucketView{Total: total, Returned: len(kept), Rows: kept}
}

func registerListExceptions(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_exceptions",
		Description: "List the transactions the Major Expenses page flags for attention, in three buckets. " +
			"`unmatched`: in-window outflows that matched no declared major expense, biggest first -- these are " +
			"the spending the user has not labelled yet, and `over_threshold` marks the ones at or above the " +
			"notable-amount threshold reported in `threshold`. `anomalous`: transactions that DID match a " +
			"declared expense but whose amount fell outside that expense's own expected range, with the range " +
			"included so the gap is visible; an explicitly pinned transaction is never called anomalous, " +
			"because the user has already said it belongs. `new_merchants`: descriptions never seen before the " +
			"trailing window reported in `new_merchant_window_days`, counted relative to the last transaction " +
			"in range rather than today. Only OUTFLOWS are considered; income is not an exception. Amounts are " +
			"SIGNED exactly as stored, so a purchase is negative and a refund positive. Every row carries the " +
			"`hash` that pin_transactions needs; hashes come from date + lower-cased description + amount, so " +
			"two identical-looking transactions share one and pinning either pins both. search/min_amount/" +
			"max_amount narrow every bucket you asked for; min_amount and max_amount compare against the " +
			"ABSOLUTE amount, and 0 means the bound is unset. Each bucket's `total` is the full number of " +
			"matches and `returned` is how many rows came back, so check them against each other before " +
			"concluding you have seen everything. Transactions the user has already resolved as duplicates are " +
			"excluded. This tool reads only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listExceptionsInput) (res *mcp.CallToolResult, out listExceptionsOutput, err error) {
		defer recoverToError("list_exceptions", &err)

		bucket := strings.ToLower(strings.TrimSpace(in.Bucket))
		switch bucket {
		case "", "unmatched", "anomalous", "new_merchants":
		default:
			return nil, listExceptionsOutput{}, fmt.Errorf(
				"bucket %q is not recognized; use \"unmatched\", \"anomalous\", \"new_merchants\", or omit it for all three", in.Bucket)
		}

		limit := in.Limit
		if limit <= 0 {
			limit = defaultExceptionLimit
		}
		if limit > maxExceptionLimit {
			limit = maxExceptionLimit
		}

		v, err := deps.pageView(in.StartDate, in.EndDate)
		if err != nil {
			return nil, listExceptionsOutput{}, err
		}

		out = listExceptionsOutput{
			Start:                 formatDay(v.Start),
			End:                   formatDay(v.End),
			Threshold:             v.Match.Exceptions.Threshold,
			NewMerchantWindowDays: v.Match.Exceptions.NewWindowDays,
		}

		matched := 0
		if bucket == "" || bucket == "unmatched" {
			// Biggest-first by absolute amount, matching the page: the rows
			// worth labelling are at the top, and the long sub-threshold tail
			// follows.
			unmatched := append([]models.Transaction(nil), v.Match.Unmatched...)
			sort.Slice(unmatched, func(i, j int) bool {
				return unmatched[i].AbsAmount() > unmatched[j].AbsAmount()
			})
			rows := make([]exceptionRow, 0, len(unmatched))
			for _, t := range unmatched {
				r := exceptionRow{
					Hash: t.Hash, Date: t.Date.Format("2006-01-02"), Description: t.Label(),
					Category: t.Category, Amount: t.Amount,
				}
				r.OverThreshold = v.Match.Exceptions.Threshold > 0 && t.AbsAmount() >= v.Match.Exceptions.Threshold
				rows = append(rows, r)
			}
			out.Unmatched = buildBucket(rows, in, limit)
			matched += out.Unmatched.Total
		}
		if bucket == "" || bucket == "anomalous" {
			rows := make([]exceptionRow, 0, len(v.Match.Exceptions.Anomalous))
			for _, a := range v.Match.Exceptions.Anomalous {
				rows = append(rows, exceptionRow{
					Hash: a.Transaction.Hash, Date: a.Transaction.Date.Format("2006-01-02"),
					Description: a.Transaction.Label(), Category: a.Transaction.Category,
					Amount: a.Transaction.Amount, MajorExpenseID: a.MajorExpenseID,
					MajorExpenseName: a.MajorExpenseName, ExpectedMin: a.ExpectedMin, ExpectedMax: a.ExpectedMax,
				})
			}
			out.Anomalous = buildBucket(rows, in, limit)
			matched += out.Anomalous.Total
		}
		if bucket == "" || bucket == "new_merchants" {
			rows := make([]exceptionRow, 0, len(v.Match.Exceptions.NewMerchants))
			for _, n := range v.Match.Exceptions.NewMerchants {
				rows = append(rows, exceptionRow{
					Hash: n.Transaction.Hash, Date: n.Transaction.Date.Format("2006-01-02"),
					Description: n.Transaction.Label(), Category: n.Transaction.Category,
					Amount: n.Transaction.Amount, FirstSeen: n.FirstSeen.Format("2006-01-02"),
				})
			}
			out.NewMerchants = buildBucket(rows, in, limit)
			matched += out.NewMerchants.Total
		}

		if matched == 0 {
			out.Note = "nothing matched; either the filters are too narrow, the window holds no outflows, or every in-window outflow is already matched to a declared major expense"
		}
		return nil, out, nil
	})
}
