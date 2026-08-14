package curate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxBulkPin caps a filter-driven pin. The Major Expenses page has no cap,
// but it has a human looking at the filtered list before clicking; a tool
// does not, so a filter wider than this is refused with its match count
// rather than applied.
const maxBulkPin = 200

type pinFilter struct {
	StartDate     string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate       string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Search        string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the transaction's description"`
	Category      string  `json:"category,omitempty" jsonschema:"exact category name"`
	MinAmount     float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include; 0 means unset"`
	MaxAmount     float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include; 0 means unset"`
	UnmatchedOnly bool    `json:"unmatched_only,omitempty" jsonschema:"restrict to transactions that currently match no declared major expense"`
}

type pinInput struct {
	ExpenseID string     `json:"expense_id,omitempty" jsonschema:"id of the major expense to pin to, from list_major_expenses; required unless unpin is true"`
	Hashes    []string   `json:"hashes,omitempty" jsonschema:"transaction hashes to act on, from list_exceptions, list_major_expenses or search_transactions; supply this or filter, not both"`
	Filter    *pinFilter `json:"filter,omitempty" jsonschema:"act on every outflow matching these conditions instead of named hashes; supply this or hashes, not both"`
	Unpin     bool       `json:"unpin,omitempty" jsonschema:"remove the pins instead of setting them, so the transactions fall back to keyword and amount matching; expense_id is then ignored"`
}

type pinOutput struct {
	ExpenseID    string   `json:"expense_id,omitempty"`
	ExpenseName  string   `json:"expense_name,omitempty"`
	Unpinned     bool     `json:"unpinned"`
	Matched      int      `json:"matched"`
	Changed      int      `json:"changed"`
	Hashes       []string `json:"hashes"`
	SnapshotPath string   `json:"snapshot_path,omitempty"`
	Note         string   `json:"note,omitempty"`
}

// resolveFilter returns the hashes of every in-window outflow the filter
// selects, in a deterministic order. It runs over the same view the read
// tools report, so what a caller saw in list_exceptions is what a filter here
// selects.
func (d Deps) resolveFilter(f pinFilter) ([]string, error) {
	v, err := d.pageView(f.StartDate, f.EndDate)
	if err != nil {
		return nil, err
	}

	candidates := make([]models.Transaction, 0)
	if f.UnmatchedOnly {
		candidates = append(candidates, v.Match.Unmatched...)
	} else {
		for _, group := range v.Match.Groups {
			candidates = append(candidates, group...)
		}
		candidates = append(candidates, v.Match.Unmatched...)
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, t := range candidates {
		if t.Hash == "" || seen[t.Hash] {
			continue
		}
		if f.Category != "" && !strings.EqualFold(t.Category, f.Category) {
			continue
		}
		if f.Search != "" && !strings.Contains(strings.ToLower(t.Label()), strings.ToLower(f.Search)) {
			continue
		}
		amt := t.AbsAmount()
		if f.MinAmount > 0 && amt < f.MinAmount {
			continue
		}
		if f.MaxAmount > 0 && amt > f.MaxAmount {
			continue
		}
		seen[t.Hash] = true
		out = append(out, t.Hash)
	}
	sort.Strings(out)
	return out, nil
}

func registerPin(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "pin_transactions",
		Description: "Attach transactions to a declared major expense, or detach them. THIS WRITES TO THE " +
			"USER'S DATA. A pin is a manual override: it wins over the expense's keywords and amount range, so " +
			"it is how a transaction that should belong to an expense but does not look like it gets counted " +
			"there. Target the transactions EITHER by naming `hashes` (from list_exceptions, " +
			"list_major_expenses or search_transactions) OR by giving a `filter`, never both. A filter selects " +
			"in-window outflows the same way the read tools report them, and is REFUSED if it selects more " +
			"than 200 -- narrow it and call again rather than expecting a partial write. Set `unpin` to true " +
			"to remove pins instead; the transactions then fall back to keyword and amount matching, and " +
			"expense_id is ignored. A hash is derived from date + lower-cased description + amount, so two " +
			"genuinely distinct transactions sharing all three share one hash and are pinned or unpinned " +
			"TOGETHER. `matched` is how many transactions were targeted and `changed` how many pins actually " +
			"differed, so changed can be smaller when some were already pinned where you asked. The pins file " +
			"is copied to a .bak before this session's first change to it; later changes in the same session " +
			"are not separately recoverable. An already-open Major Expenses page does NOT refresh itself -- it " +
			"shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pinInput) (res *mcp.CallToolResult, out pinOutput, err error) {
		defer recoverToError("pin_transactions", &err)

		hasHashes := len(in.Hashes) > 0
		hasFilter := in.Filter != nil
		if hasHashes == hasFilter {
			return nil, pinOutput{}, fmt.Errorf(
				"supply exactly one of hashes or filter: hashes names the transactions directly, filter selects them by condition")
		}

		expenseName := ""
		if !in.Unpin {
			if strings.TrimSpace(in.ExpenseID) == "" {
				return nil, pinOutput{}, fmt.Errorf("expense_id is required unless unpin is true")
			}
			expenses, err := deps.Expenses.LoadMajorExpenses()
			if err != nil {
				return nil, pinOutput{}, err
			}
			for _, e := range expenses {
				if e.ID == in.ExpenseID {
					expenseName = e.Name
					break
				}
			}
			if expenseName == "" {
				return nil, pinOutput{}, fmt.Errorf(
					"no major expense has id %q; call list_major_expenses for the current ids, or create one with upsert_major_expense", in.ExpenseID)
			}
		}

		var hashes []string
		if hasHashes {
			seen := make(map[string]bool, len(in.Hashes))
			for _, h := range in.Hashes {
				if h = strings.TrimSpace(h); h != "" && !seen[h] {
					seen[h] = true
					hashes = append(hashes, h)
				}
			}
		} else {
			hashes, err = deps.resolveFilter(*in.Filter)
			if err != nil {
				return nil, pinOutput{}, err
			}
			if len(hashes) > maxBulkPin {
				return nil, pinOutput{}, fmt.Errorf(
					"that filter selects %d transactions, over the %d limit for one call; narrow it (a tighter date range, a more specific search, or an amount bound) and call again",
					len(hashes), maxBulkPin)
			}
		}

		out = pinOutput{
			ExpenseID: in.ExpenseID, ExpenseName: expenseName, Unpinned: in.Unpin,
			Matched: len(hashes), Hashes: hashes,
		}
		if in.Unpin {
			out.ExpenseID = ""
		}
		if len(hashes) == 0 {
			out.Hashes = []string{}
			out.Note = "nothing was targeted, so nothing was written; the filter matched no in-window outflow"
			return nil, out, nil
		}

		// Before the write, never after: a failed snapshot must abort it.
		snapPath, err := deps.Snapshots.Ensure(transactionPinsFile, time.Now())
		if err != nil {
			return nil, pinOutput{}, err
		}
		out.SnapshotPath = snapPath

		target := in.ExpenseID
		if in.Unpin {
			target = "" // SetTransactionPins deletes on an empty expense id.
		}
		updates := make(map[string]string, len(hashes))
		for _, h := range hashes {
			updates[h] = target
		}
		changed, err := deps.Pins.SetTransactionPins(updates)
		if err != nil {
			return nil, pinOutput{}, err
		}
		out.Changed = changed
		if changed == 0 {
			out.Note = "every targeted transaction was already in that state; nothing changed"
		}
		return nil, out, nil
	})
}
