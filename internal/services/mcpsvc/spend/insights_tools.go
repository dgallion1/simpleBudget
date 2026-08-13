package spend

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerAnomalies adds get_anomalies to s.
func registerAnomalies(s *mcp.Server, deps Deps) {
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
}

// registerPriceCreep adds get_price_creep to s.
func registerPriceCreep(s *mcp.Server, deps Deps) {
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
