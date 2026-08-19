package dataloader

import (
	"fmt"
	"log"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/transfers"
)

// classifyTransfers is the load pipeline's transfer-classification stage. It
// replaces filterInternalTransfers, which DROPPED every row whose description
// matched a transfer pattern: a matched transfer disappeared from the ledger
// and an unmatched one was counted twice. Nothing is dropped here. Matching
// rows are typed models.Transfer and stay exactly where they are; income and
// expense totals exclude them by type filter rather than by absence.
//
// Two inputs beyond the rows themselves, both non-fatal on failure so a
// corrupt sidecar degrades classification instead of breaking CSV ingestion:
//
//  1. The user's IsInternalTransfer-flagged major expenses, which extend the
//     hardcoded pattern list to their own broker without a code change.
//  2. transfer_decisions.json, the confirm/reject verdicts from the review
//     queue.
func (dl *DataLoader) classifyTransfers(transactions []models.Transaction) []models.Transaction {
	var transferDefs []models.MajorExpense
	if defs, err := dl.LoadMajorExpenses(); err != nil {
		log.Printf("Warning: could not load major expenses for transfer classification: %v", err)
	} else {
		for _, d := range defs {
			if d.IsInternalTransfer {
				transferDefs = append(transferDefs, d)
			}
		}
	}

	decisions, err := dl.LoadTransferDecisions()
	if err != nil {
		log.Printf("Warning: could not load transfer decisions: %v", err)
		decisions = nil
	}

	result := transfers.Classify(transactions, transferDefs, decisions)
	dl.setTransferState(result.Classified(), result.Suspected)

	if result.Classified() > 0 {
		log.Printf("Classified %d transactions as transfers (%d paired, %d external)",
			result.Classified(), result.Paired, result.External)
	}
	if n := len(result.Suspected); n > 0 {
		log.Printf("%d suspected transfer pairs awaiting review", n)
	}

	return result.Transactions
}

// setTransferState records what the load that just finished classified. Every
// LoadDataContext exit path goes through it, so a load that finds no files
// clears the previous load's numbers instead of leaving them stale.
func (dl *DataLoader) setTransferState(classified int, suspected []transfers.Suspected) {
	dl.stateMu.Lock()
	dl.filteredTransferCount = classified
	dl.suspectedTransfers = suspected
	dl.stateMu.Unlock()
}

// SuspectedTransfers returns the review queue from the most recent load:
// cross-account, opposite-sign, equal-amount matches inside the pairing
// window that no transfer pattern backs, plus candidates that tied on date
// distance. These are suggestions only. They are never auto-paired, because
// coincidentally equal amounts are common and pairing one on a guess would
// silently erase real income or real spending.
//
// The returned slice is a copy, safe to hold while another load runs.
func (dl *DataLoader) SuspectedTransfers() []transfers.Suspected {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	if len(dl.suspectedTransfers) == 0 {
		return nil
	}
	out := make([]transfers.Suspected, len(dl.suspectedTransfers))
	copy(out, dl.suspectedTransfers)
	return out
}

// ResolveTransfer persists the user's verdict on one suspected pair to
// data/transfer_decisions.json. A confirmed pair is paired on the next load
// whether or not a pattern backs it; a rejected pair is never suggested or
// auto-paired again.
//
// pairKey must name a pair from the most recent load's SuspectedTransfers --
// that is where the two StableIDs the decision records come from. An unknown
// key is an error rather than a silently stored no-op entry.
func (dl *DataLoader) ResolveTransfer(pairKey string, v transfers.Verdict) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	switch v {
	case transfers.VerdictConfirm, transfers.VerdictReject:
	default:
		return fmt.Errorf("unknown verdict %q (want %q or %q)",
			v, transfers.VerdictConfirm, transfers.VerdictReject)
	}

	ids, ok := dl.suspectedIdentities(pairKey)
	if !ok {
		return fmt.Errorf("no suspected transfer pair with key %q", pairKey)
	}

	tx, done := dl.beginWrite()
	defer done()
	decisions, err := dl.loadTransferDecisionsLocked(tx)
	if err != nil {
		return err
	}
	decisions[pairKey] = transfers.Decision{
		PairKey:   pairKey,
		StableIDs: ids,
		Verdict:   v,
		DecidedAt: time.Now().UTC(),
	}
	return dl.writeTransferDecisionsLocked(tx, decisions)
}

// suspectedIdentities returns the two legs' identities for a queued pair.
func (dl *DataLoader) suspectedIdentities(pairKey string) ([2]string, bool) {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	for _, s := range dl.suspectedTransfers {
		if s.PairKey == pairKey {
			return [2]string{transfers.Identity(s.Left), transfers.Identity(s.Right)}, true
		}
	}
	return [2]string{}, false
}
