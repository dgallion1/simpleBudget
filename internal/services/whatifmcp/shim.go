package whatifmcp

import (
	"context"
	"fmt"
	"math"

	"budget2/internal/services/retirement/overrides"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Overrides, round0, Source and NewSource are a temporary compiling shim.
// view.go, months.go, overrides.go and scenarios.go moved to
// internal/services/mcpsvc/plan in Task 2 of the app-wide-mcp-phase-1 plan,
// but live.go, insights.go and their tests stay in this package until Tasks
// 4 and 5 take what they need -- and those files still reference the types
// that moved. This file exists only to keep them compiling in the meantime;
// it is deleted (not migrated) once Task 5 removes insights.go's use of
// Source and Task 4/5 remove live.go.

// Overrides is the shared sparse settings vocabulary, aliased the same way
// overrides.go (now mcpsvc/plan/overrides.go) aliased it.
type Overrides = overrides.Overrides

// round0 rounds a currency amount to whole dollars. See view.go (now
// mcpsvc/plan/view.go) for the original.
func round0(v float64) float64 { return math.Round(v) }

// Source is a trimmed stand-in for the type that used to live in
// scenarios.go (now mcpsvc/plan/scenarios.go, rebuilt there onto the
// server's shared *retirement.SettingsManager). Only the fields
// insights.go's Transactions method still reads survive here; List, Load
// and the settings-manager-backed lookups are plan.Source's job now.
type Source struct {
	settingsDir string
	store       *storage.Storage
	txSource    TransactionSource
}

func NewSource(settingsDir string, store *storage.Storage) *Source {
	return &Source{settingsDir: settingsDir, store: store}
}

// recoverToError converts a panic into an error so a bad scenario fails one
// tool call instead of terminating the session. Copied from the deleted
// server.go for the same reason as the rest of this file.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// NewServer registers only the two insight tools server.go used to serve
// alongside the planner tools -- get_anomalies and get_price_creep are the
// only ones insights_test.go still exercises through this package. live and
// snaps are accepted (insights_test.go passes nil, nil) but unused; the
// planner tools they backed moved to mcpsvc/plan and are not reconstructed
// here.
func NewServer(src *Source, live *Client, snaps *Snapshotter) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "whatif", Version: "v0.1.0"}, nil)

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

		ts, err := src.Transactions()
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

		ts, err := src.Transactions()
		if err != nil {
			return nil, priceCreepOutput{}, err
		}

		rows := priceCreepRows(ts)
		return nil, priceCreepOutput{Count: len(rows), Items: rows}, nil
	})

	return s
}
