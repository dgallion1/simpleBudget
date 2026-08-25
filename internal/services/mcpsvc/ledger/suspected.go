package ledger

import (
	"context"
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/transfers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// suspectedLegRow is one leg of a suspected pair, mirroring transferRow's
// field naming (get_transfers.go) so the two tools read the same to a model.
type suspectedLegRow struct {
	// Date is the transaction date, YYYY-MM-DD.
	Date string `json:"date"`
	// Description is the transaction's display label (Label()).
	Description string `json:"description"`
	// AccountID is the account this leg belongs to.
	AccountID string `json:"account_id"`
	// Amount is SIGNED in bank convention: positive = money received into
	// this account, negative = money sent out of this account. Matches
	// get_transfers' sign convention.
	Amount float64 `json:"amount"`
	// Category is the source CSV's free-text category, if any.
	Category string `json:"category,omitempty"`
}

// suspectedPairRow is one candidate pair awaiting the user's confirm/reject.
type suspectedPairRow struct {
	// PairKey is what resolve_transfer's pair_key parameter takes.
	PairKey string `json:"pair_key"`
	// Reason is why the classifier would not auto-pair this candidate:
	// "amount_match" (a cross-account, opposite-sign, equal-amount match
	// with no transfer pattern on either leg) or "ambiguous" (several
	// pattern-backed candidates tied on date distance, so no single
	// counterparty was determinable). Mirrors transfers.ReasonAmountMatch /
	// transfers.ReasonAmbiguous.
	Reason string          `json:"reason"`
	Left   suspectedLegRow `json:"left"`
	Right  suspectedLegRow `json:"right"`
}

type getSuspectedTransfersOutput struct {
	Count int                `json:"count"`
	Pairs []suspectedPairRow `json:"pairs"`
}

func registerGetSuspectedTransfers(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_suspected_transfers",
		Description: "The transfer review queue: candidate pairs the classifier found but would not auto-pair. " +
			"A suspected pair is two cross-account, opposite-sign, equal-amount rows inside the pairing window " +
			"that no transfer pattern backs -- coincidentally equal amounts are common, so these are only ever " +
			"SUGGESTED, never auto-paired (GLOSSARY.md \"Internal transfer\"). reason is \"amount_match\" (no " +
			"pattern hit on either leg) or \"ambiguous\" (several pattern-backed candidates tied on date " +
			"distance, so no single counterparty was determinable). Each pair's pair_key is exactly what " +
			"resolve_transfer's pair_key parameter takes -- this is the tool to call before resolve_transfer, " +
			"never invent or guess a key. resolve_transfer's confirm verdict is the load-bearing one: it silently " +
			"erases real income or real spending from the totals if the rows were not actually a transfer, so " +
			"read left/right here and think before calling it. An empty result (count 0) means nothing is " +
			"currently awaiting review -- that is a normal answer, not an error. amount is SIGNED in bank " +
			"convention, same as get_transfers: positive = money received into that leg's account, negative = " +
			"money sent out. This is READ-ONLY; it neither resolves nor changes anything. The browser's " +
			"/transfers page (the \"Suspected pairs\" section) shows this same queue to a human.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (res *mcp.CallToolResult, out getSuspectedTransfersOutput, err error) {
		defer recoverToError("get_suspected_transfers", &err)

		if deps.Transfers == nil {
			return nil, getSuspectedTransfersOutput{}, fmt.Errorf("no transfer source is configured on this server")
		}

		// Trigger a load first, for exactly the reasons the comment in
		// resolve.go records: SuspectedTransfers() reflects only the most
		// recent load, so a fresh server would report an empty queue and a
		// stale cache would mislead; load() also reports a locked store as
		// such rather than letting ciphertext surface as a parse error.
		if _, err := deps.load(); err != nil {
			return nil, getSuspectedTransfersOutput{}, err
		}

		suspected := deps.Transfers.SuspectedTransfers()
		pairs := make([]suspectedPairRow, 0, len(suspected))
		for _, sp := range suspected {
			pairs = append(pairs, suspectedPairRow{
				PairKey: sp.PairKey,
				Reason:  sp.Reason,
				Left:    suspectedLegRowFor(sp.Left),
				Right:   suspectedLegRowFor(sp.Right),
			})
		}

		return nil, getSuspectedTransfersOutput{Count: len(pairs), Pairs: pairs}, nil
	})
}

// suspectedLegRowFor renders one leg of a transfers.Suspected pair.
func suspectedLegRowFor(t models.Transaction) suspectedLegRow {
	return suspectedLegRow{
		Date:        t.Date.Format("2006-01-02"),
		Description: t.Label(),
		AccountID:   t.AccountID,
		Amount:      round2(t.Amount),
		Category:    t.Category,
	}
}

// Compile-time check that the reason strings this file documents still match
// the package they are mirrored from.
var (
	_ = transfers.ReasonAmountMatch
	_ = transfers.ReasonAmbiguous
)
