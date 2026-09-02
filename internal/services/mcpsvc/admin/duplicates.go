package admin

import (
	"context"
	"fmt"
	"math"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type duplicatesInput struct {
	IncludeResolved bool `json:"include_resolved,omitempty" jsonschema:"also return the pairs the user has already settled, both kept_winner and kept_both"`
}

// duplicateRow is one side of a candidate pair. amount is SIGNED exactly as
// stored, matching search_transactions and the curate tools.
type duplicateRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status,omitempty"`
}

type duplicatePairOut struct {
	PairKey string       `json:"pair_key"`
	Left    duplicateRow `json:"left"`
	Right   duplicateRow `json:"right"`
}

// duplicatesOutput reports the three buckets separately rather than folding
// the settled ones together: resolved_count is kept_winner only, because that
// is the only outcome that removed an amount from the user's totals, while
// kept_both_count is settled-but-changed-nothing. Both are undoable, so both
// must be listed or undo_resolve has keys it will accept that no tool can
// name.
type duplicatesOutput struct {
	UnresolvedCount int                `json:"unresolved_count"`
	Unresolved      []duplicatePairOut `json:"unresolved"`
	ResolvedCount   int                `json:"resolved_count"`
	Resolved        []duplicatePairOut `json:"resolved,omitempty"`
	KeptBothCount   int                `json:"kept_both_count"`
	KeptBoth        []duplicatePairOut `json:"kept_both,omitempty"`
	Note            string             `json:"note,omitempty"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func rowFor(t models.Transaction) duplicateRow {
	return duplicateRow{
		Hash:        t.Hash,
		Date:        t.Date.Format("2006-01-02"),
		Description: t.Label(),
		Amount:      round2(t.Amount),
		Status:      t.Status,
	}
}

// pairsFrom shapes detection results for output. Order is detection order,
// which is stable for a given ledger.
func pairsFrom(pairs []dataloader.DuplicatePair) []duplicatePairOut {
	out := make([]duplicatePairOut, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, duplicatePairOut{
			PairKey: p.Key,
			Left:    rowFor(p.Left),
			Right:   rowFor(p.Right),
		})
	}
	return out
}

func registerListDuplicates(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_duplicates",
		Description: "List pairs of transactions that look like the same payment recorded twice -- a scheduled " +
			"bill pay and a posted check for the identical amount within 7 days, a scheduled bill pay or " +
			"recurring autopay that instead settles as an ordinary posted ACH/autopay charge (not a check) " +
			"within 7 days, or a Pending charge that re-appears Posted with a rewritten description within " +
			"5 days -- and are waiting for the user to decide between them. This is the queue behind the " +
			"app's Duplicates page. Each pair has a " +
			"pair_key, which is what resolve_duplicates and undo_resolve take; the two sides are called left and " +
			"right and the order carries NO meaning about which is the real charge. amount is the SIGNED amount " +
			"exactly as stored (an expense is negative), matching search_transactions for the same transaction. " +
			"Both sides are still LIVE and both are still counted in every spending total until the user " +
			"resolves the pair, so an unresolved queue means the app is currently double-counting those " +
			"amounts. Set include_resolved to also see the pairs already settled: resolved holds the " +
			"kept_winner decisions, whose losing side is excluded from every total, and kept_both holds the " +
			"pairs the user confirmed were two genuinely different payments, which changed no total but is " +
			"still a recorded decision. Both lists are undoable via undo_resolve, and kept_both appears here " +
			"and nowhere else -- it is the only way to recover such a pair_key. This tool reads only -- it " +
			"changes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in duplicatesInput) (res *mcp.CallToolResult, out duplicatesOutput, err error) {
		defer recoverToError("list_duplicates", &err)

		if deps.Duplicates == nil {
			return nil, duplicatesOutput{}, fmt.Errorf("no data loader is configured on this server")
		}
		// Detection results are recomputed and cached by LoadData; without
		// this the tool reports whatever the last page request left behind.
		if _, err := deps.load(); err != nil {
			return nil, duplicatesOutput{}, err
		}

		unresolved := pairsFrom(deps.Duplicates.UnresolvedDuplicates())
		out = duplicatesOutput{
			UnresolvedCount: len(unresolved),
			Unresolved:      unresolved,
		}
		resolved := deps.Duplicates.ResolvedDuplicates()
		out.ResolvedCount = len(resolved)
		keptBoth := deps.Duplicates.KeptBothDuplicates()
		out.KeptBothCount = len(keptBoth)
		if in.IncludeResolved {
			out.Resolved = pairsFrom(resolved)
			out.KeptBoth = pairsFrom(keptBoth)
		}
		if out.UnresolvedCount == 0 {
			out.Note = "nothing is waiting for review; no candidate pairs were detected, or the user has already decided every one"
		}
		return nil, out, nil
	})
}
