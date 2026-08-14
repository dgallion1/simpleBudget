package curate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deleteInput struct {
	ID      string `json:"id" jsonschema:"id of the major expense, from list_major_expenses (or from the deleted list when restoring)"`
	Restore bool   `json:"restore,omitempty" jsonschema:"bring a previously deleted expense back instead of deleting one"`
}

type deleteOutput struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Restored      bool     `json:"restored"`
	PinsDetached  int      `json:"pins_detached,omitempty"`
	PinsRestored  int      `json:"pins_restored,omitempty"`
	SnapshotPaths []string `json:"snapshot_paths"`
	Note          string   `json:"note,omitempty"`
}

// countPinsTo returns how many transactions are currently pinned to id.
func countPinsTo(pins map[string]string, id string) int {
	n := 0
	for _, target := range pins {
		if target == id {
			n++
		}
	}
	return n
}

func registerDelete(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_major_expense",
		Description: "Delete a declared major expense, or restore one that was deleted. THIS WRITES TO THE " +
			"USER'S DATA. Deleting is a SOFT delete, the same one the Major Expenses page performs: the " +
			"definition is moved to an archive along with a record of every transaction pinned to it, so " +
			"calling this again with restore set to true brings both back. Nothing about the transactions " +
			"themselves changes -- they simply stop being grouped under this expense, so they reappear as " +
			"unmatched in list_exceptions and the app's unmatched spending total grows by their sum. A " +
			"restore reattaches a captured pin only if that transaction is not currently pinned somewhere " +
			"else, so a hash reassigned in the meantime keeps its newer pin. Use list_major_expenses with " +
			"include_deleted to see what can be restored. The affected files are each copied to a .bak before " +
			"this session's first change to them. An already-open Major Expenses page does NOT refresh itself " +
			"-- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (res *mcp.CallToolResult, out deleteOutput, err error) {
		defer recoverToError("delete_major_expense", &err)

		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, deleteOutput{}, fmt.Errorf("id is required; call list_major_expenses for the current ids")
		}

		// Resolve the name and the pin count from the side the operation
		// reads FROM, before anything is written.
		var name string
		if in.Restore {
			deleted, err := deps.Expenses.LoadDeletedMajorExpenses()
			if err != nil {
				return nil, deleteOutput{}, err
			}
			for _, d := range deleted {
				if d.Expense.ID == id {
					name = d.Expense.Name
					break
				}
			}
			if name == "" {
				return nil, deleteOutput{}, fmt.Errorf(
					"no deleted major expense has id %q; call list_major_expenses with include_deleted to see what can be restored", id)
			}
		} else {
			active, err := deps.Expenses.LoadMajorExpenses()
			if err != nil {
				return nil, deleteOutput{}, err
			}
			for _, e := range active {
				if e.ID == id {
					name = e.Name
					break
				}
			}
			if name == "" {
				return nil, deleteOutput{}, fmt.Errorf(
					"no active major expense has id %q; call list_major_expenses for the current ids", id)
			}
		}

		pinsBefore, err := deps.Pins.LoadTransactionPins()
		if err != nil {
			return nil, deleteOutput{}, err
		}

		// Before the write, never after: a failed snapshot must abort it.
		// Archive and restore each rewrite all three files, so all three are
		// backed up; a missing one has no prior state to lose, so its absence
		// is not fatal here the way it is for a single-file write.
		var paths []string
		now := time.Now()
		for _, f := range []string{majorExpensesFile, deletedMajorExpensesFile, transactionPinsFile} {
			p, err := deps.Snapshots.Ensure(f, now)
			if err != nil {
				continue
			}
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			return nil, deleteOutput{}, fmt.Errorf(
				"refusing to write: none of %s, %s or %s could be copied to a backup first",
				majorExpensesFile, deletedMajorExpensesFile, transactionPinsFile)
		}

		if in.Restore {
			if err := deps.Expenses.RestoreMajorExpense(id); err != nil {
				return nil, deleteOutput{}, err
			}
		} else if err := deps.Expenses.ArchiveMajorExpense(id); err != nil {
			return nil, deleteOutput{}, err
		}

		pinsAfter, err := deps.Pins.LoadTransactionPins()
		if err != nil {
			return nil, deleteOutput{}, err
		}

		out = deleteOutput{ID: id, Name: name, Restored: in.Restore, SnapshotPaths: paths}
		if in.Restore {
			out.PinsRestored = countPinsTo(pinsAfter, id) - countPinsTo(pinsBefore, id)
			if out.PinsRestored < 0 {
				out.PinsRestored = 0
			}
		} else {
			out.PinsDetached = countPinsTo(pinsBefore, id) - countPinsTo(pinsAfter, id)
			if out.PinsDetached < 0 {
				out.PinsDetached = 0
			}
			out.Note = "deleted expenses are recoverable: call this again with restore set to true, or use the Major Expenses page"
		}
		return nil, out, nil
	})
}
