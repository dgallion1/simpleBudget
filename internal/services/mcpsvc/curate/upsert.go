package curate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type upsertInput struct {
	ID                 string   `json:"id,omitempty" jsonschema:"id of an existing expense to edit, from list_major_expenses; omit to create a new one"`
	Name               *string  `json:"name,omitempty" jsonschema:"display name, required when creating; max 200 characters"`
	Keywords           []string `json:"keywords,omitempty" jsonschema:"case-insensitive substrings matched against a transaction's description; omit to leave unchanged, pass an empty list to clear"`
	ExpectedMin        *float64 `json:"expected_min,omitempty" jsonschema:"low end of the expected amount, as a positive dollar figure; with expected_max equal it matches that exact amount, and a wider range flags anything outside it as anomalous"`
	ExpectedMax        *float64 `json:"expected_max,omitempty" jsonschema:"high end of the expected amount, as a positive dollar figure"`
	Notes              *string  `json:"notes,omitempty" jsonschema:"free-text note shown with the expense"`
	IsInternalTransfer *bool    `json:"is_internal_transfer,omitempty" jsonschema:"treat matches as money moving between the user's own accounts, dropping them from spending totals instead of counting them as spending"`
	PinHash            string   `json:"pin_hash,omitempty" jsonschema:"a transaction hash to pin to this expense in the same call, so the transaction that prompted the expense is matched even if the keywords would not have caught it"`
}

type upsertOutput struct {
	ID           string          `json:"id"`
	Created      bool            `json:"created"`
	Expense      majorExpenseRow `json:"expense"`
	Pinned       bool            `json:"pinned"`
	SnapshotPath string          `json:"snapshot_path,omitempty"`
}

// trimKeywords drops blank entries, matching the page's splitAndTrim. A
// non-nil but fully blank list still clears the keywords.
func trimKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	for _, k := range in {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func registerUpsert(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "upsert_major_expense",
		Description: "Create a declared major expense, or edit an existing one. THIS WRITES TO THE USER'S " +
			"DATA. Omit `id` to create; give an `id` from list_major_expenses to edit. On an edit, every field " +
			"you omit KEEPS ITS CURRENT VALUE -- to clear the keywords pass an explicitly empty list, and to " +
			"clear an amount bound pass 0. A definition is valid in exactly three shapes: at least one keyword " +
			"(an amount range is then optional and only flags anomalies); no keywords but BOTH expected_min " +
			"and expected_max set, which matches by amount alone and is how fixed-dollar charges with varying " +
			"descriptions are captured (setting them equal matches that one amount); or no keywords and no " +
			"range at all, a pin-only target you attach transactions to with pin_transactions. Setting only " +
			"one bound without a keyword is refused. expected_min/expected_max are POSITIVE dollar figures " +
			"even though the transactions they match are negative. is_internal_transfer marks the entry as " +
			"money moving between the user's own accounts: its matches are dropped from spending totals " +
			"instead of counted as spending, which changes what every other tool reports, so do not set it " +
			"unless the user says the money did not leave their household. pin_hash pins one transaction to " +
			"the expense in the same call, which is how you make sure the charge that prompted the expense is " +
			"matched even when the keywords would have missed it. The definitions file is copied to a .bak " +
			"before this session's first change to it. An already-open Major Expenses page does NOT refresh " +
			"itself -- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in upsertInput) (res *mcp.CallToolResult, out upsertOutput, err error) {
		defer recoverToError("upsert_major_expense", &err)

		existing, err := deps.Expenses.LoadMajorExpenses()
		if err != nil {
			return nil, upsertOutput{}, err
		}

		var (
			target models.MajorExpense
			create = strings.TrimSpace(in.ID) == ""
			found  bool
		)
		if !create {
			for _, e := range existing {
				if e.ID == in.ID {
					target, found = e, true
					break
				}
			}
			if !found {
				return nil, upsertOutput{}, fmt.Errorf(
					"no major expense has id %q; call list_major_expenses for the current ids, or omit id to create a new expense", in.ID)
			}
		}

		// Apply only what the caller actually sent. UpdateMajorExpense copies
		// every one of these fields from its argument, so anything not merged
		// in here would be blanked on disk.
		if in.Name != nil {
			target.Name = strings.TrimSpace(*in.Name)
		}
		if in.Keywords != nil {
			target.Keywords = trimKeywords(in.Keywords)
		}
		if in.ExpectedMin != nil {
			target.ExpectedMin = *in.ExpectedMin
		}
		if in.ExpectedMax != nil {
			target.ExpectedMax = *in.ExpectedMax
		}
		if in.Notes != nil {
			target.Notes = strings.TrimSpace(*in.Notes)
		}
		if in.IsInternalTransfer != nil {
			target.IsInternalTransfer = *in.IsInternalTransfer
		}

		// The page's own rules, shared rather than restated, so a definition
		// the Major Expenses form would refuse is refused here identically.
		if err := majorexpenses.Validate(target); err != nil {
			return nil, upsertOutput{}, err
		}

		// Before the write, never after: a failed snapshot must abort it. A
		// create is the one case where the definitions file may legitimately
		// not exist yet, and Ensure treats a missing source as an error, so a
		// first-ever create skips the snapshot -- there is no prior state to
		// recover.
		var snapPath string
		if len(existing) > 0 {
			snapPath, err = deps.Snapshots.Ensure(majorExpensesFile, time.Now())
			if err != nil {
				return nil, upsertOutput{}, err
			}
		}

		if create {
			target.ID = uuid.New().String()
			if _, err := deps.Expenses.AddMajorExpense(target); err != nil {
				return nil, upsertOutput{}, err
			}
		} else if _, err := deps.Expenses.UpdateMajorExpense(target.ID, target); err != nil {
			return nil, upsertOutput{}, err
		}

		out = upsertOutput{
			ID:      target.ID,
			Created: create,
			Expense: majorExpenseRow{
				ID: target.ID, Name: target.Name, Keywords: trimKeywords(target.Keywords),
				ExpectedMin: target.ExpectedMin, ExpectedMax: target.ExpectedMax,
				Notes: target.Notes, IsInternalTransfer: target.IsInternalTransfer,
			},
			SnapshotPath: snapPath,
		}
		if out.Expense.Keywords == nil {
			out.Expense.Keywords = []string{}
		}

		// Pin failure does not roll back the create, matching the page's
		// create-and-pin affordance: the definition is the durable part and
		// the pin can be reapplied with pin_transactions.
		if h := strings.TrimSpace(in.PinHash); h != "" {
			if _, err := deps.Pins.SetTransactionPins(map[string]string{h: target.ID}); err == nil {
				out.Pinned = true
			}
		}
		return nil, out, nil
	})
}
