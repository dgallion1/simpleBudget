// Package spend serves spending analysis over MCP: what the ledger says, as
// opposed to what the retirement projection assumes.
package spend

import (
	"context"
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TransactionSource loads the full transaction history. *dataloader.DataLoader
// satisfies it via its existing LoadData method, so no adapter is needed in
// production. The interface exists so tests can substitute a canned
// models.TransactionSet directly -- constructing exact peer groups and
// planted anomalies through real CSV parsing, classification, and near-duplicate
// detection would be indirect and brittle.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// Deps is what the spending tools need. Store is optional and used only to
// turn a locked store into a clear message instead of a parse failure.
type Deps struct {
	Transactions TransactionSource
	Store        *storage.Storage
}

func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	return d.Transactions.LoadData()
}

// Register adds the spending tools to s.
func Register(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_anomalies",
		Description: "Flag unusual expense transactions: amounts far outside a merchant's or category's " +
			"typical range (mad_merchant, mad_category), or an outsized first-ever charge from a brand-new " +
			"merchant (new_merchant). Detection ALWAYS runs over the COMPLETE transaction history -- peer-group " +
			"baselines and each merchant's first-ever occurrence never change with the window -- start_date and " +
			"end_date only filter which already-detected flags are RETURNED, so a narrow window will not " +
			"chronically re-flag a long-standing recurring bill as \"new\" merely because its true first " +
			"occurrence predates the window. Only expenses are considered (outflows: TransactionType == Outflow " +
			"AND Amount < 0); the returned amount is the transaction's signed amount, so expenses are negative. " +
			"Both date params are optional, inclusive, YYYY-MM-DD; an invalid date is a tool error.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in anomaliesInput) (res *mcp.CallToolResult, out anomaliesOutput, err error) {
		defer recoverToError("get_anomalies", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, anomaliesOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, anomaliesOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, anomaliesOutput{}, err
		}

		rows := anomalyRows(ts, start, end)
		return nil, anomaliesOutput{
			Count:     len(rows),
			Window:    anomaliesWindow{Start: nilableString(in.StartDate), End: nilableString(in.EndDate)},
			Anomalies: rows,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_price_creep",
		Description: "Find recurring merchant charges whose amount has drifted upward over their full history: " +
			"for each merchant with at least 6 occurrences, compares the median of its first 3 charges to the " +
			"median of its last 3 and reports it when the increase exceeds 5%. Always runs over the COMPLETE " +
			"transaction history -- there is no window parameter, because the whole point is a merchant's amount " +
			"across its full lifetime, not one period. Only expenses are considered (outflows: TransactionType " +
			"== Outflow AND Amount < 0); the returned amounts are absolute (positive) dollar figures.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ priceCreepInput) (res *mcp.CallToolResult, out priceCreepOutput, err error) {
		defer recoverToError("get_price_creep", &err)

		ts, err := deps.load()
		if err != nil {
			return nil, priceCreepOutput{}, err
		}

		rows := priceCreepRows(ts)
		return nil, priceCreepOutput{Count: len(rows), Items: rows}, nil
	})
}
