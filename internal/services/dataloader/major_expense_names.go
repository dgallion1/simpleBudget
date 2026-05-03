package dataloader

import (
	"log"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
)

// applyMajorExpenseNames stamps Transaction.MajorExpenseName on every
// transaction that maps to a declared major expense — either via an
// explicit user pin or by keyword/amount matching. Mirrors the logic
// used by services/majorexpenses.Match so the per-transaction label
// matches the group the Major Expenses page would put the row in.
//
// Failure modes are non-fatal: if either the defs file or the pins file
// is missing or unreadable, we log and return the input unchanged. This
// makes the feature opt-in: users who haven't configured Major Expenses
// see no behavior change.
func (dl *DataLoader) applyMajorExpenseNames(transactions []models.Transaction) []models.Transaction {
	defs, err := dl.LoadMajorExpenses()
	if err != nil {
		log.Printf("Warning: could not load major expenses for label stamping: %v", err)
		return transactions
	}
	if len(defs) == 0 {
		return transactions
	}

	pins, err := dl.LoadTransactionPins()
	if err != nil {
		log.Printf("Warning: could not load transaction pins for label stamping: %v", err)
		pins = nil
	}

	nameByID := make(map[string]string, len(defs))
	validIDs := make(map[string]bool, len(defs))
	for _, d := range defs {
		nameByID[d.ID] = d.Name
		validIDs[d.ID] = true
	}

	for i := range transactions {
		t := transactions[i]
		// Major Expenses is an outflow concept. Skip income so a paycheck
		// or refund whose description happens to contain an expense
		// keyword (e.g. "BOFA HOMELOANS REFUND") doesn't get stamped and
		// then surface as that expense via Transaction.Label().
		if t.TransactionType == models.Income {
			continue
		}
		// Pin wins when it points to an existing expense.
		if pins != nil && t.Hash != "" {
			if id, ok := pins[t.Hash]; ok && validIDs[id] {
				transactions[i].MajorExpenseName = nameByID[id]
				continue
			}
		}
		// Otherwise fall back to keyword/amount matching.
		if id, ok := majorexpenses.MatchTransaction(t, defs); ok {
			transactions[i].MajorExpenseName = nameByID[id]
		}
	}
	return transactions
}
