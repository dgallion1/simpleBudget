package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const transactionPinsFile = "transaction_pins.json"

// transactionPinsPath returns the path to the transaction_pins.json file.
func (dl *DataLoader) transactionPinsPath() string {
	return filepath.Join(dl.CSVDirectory, transactionPinsFile)
}

// LoadTransactionPins reads the hash → MajorExpense.ID mapping from disk.
// Returns an empty map if the file does not exist.
func (dl *DataLoader) LoadTransactionPins() (map[string]string, error) {
	path := dl.transactionPinsPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	pins := make(map[string]string)
	if err := json.Unmarshal(data, &pins); err != nil {
		return nil, fmt.Errorf("invalid transaction_pins file: %w", err)
	}
	return pins, nil
}

// SetTransactionPin pins a transaction (by hash) to a major-expense ID.
// An empty expenseID removes the pin.
func (dl *DataLoader) SetTransactionPin(hash, expenseID string) error {
	if hash == "" {
		return fmt.Errorf("transaction hash is required")
	}
	pins, err := dl.LoadTransactionPins()
	if err != nil {
		return fmt.Errorf("load transaction pins: %w", err)
	}
	if expenseID == "" {
		delete(pins, hash)
	} else {
		pins[hash] = expenseID
	}
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.transactionPinsPath(), data, 0644)
}

// ClearTransactionPin removes the pin for a transaction hash. No-op if
// the hash isn't pinned.
func (dl *DataLoader) ClearTransactionPin(hash string) error {
	return dl.SetTransactionPin(hash, "")
}

// SetTransactionPins writes many hash → expense-ID pins in one disk
// round-trip. Existing pins for hashes not in the input map are left
// untouched; pins in the input map with an empty expenseID are removed.
// Empty hashes are silently skipped so callers don't have to filter
// upstream. Returns the number of pins actually changed.
func (dl *DataLoader) SetTransactionPins(updates map[string]string) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	pins, err := dl.LoadTransactionPins()
	if err != nil {
		return 0, err
	}
	changed := 0
	for hash, expenseID := range updates {
		if hash == "" {
			continue
		}
		if expenseID == "" {
			if _, ok := pins[hash]; ok {
				delete(pins, hash)
				changed++
			}
			continue
		}
		if pins[hash] != expenseID {
			pins[hash] = expenseID
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := dl.store.WriteFile(dl.transactionPinsPath(), data, 0644); err != nil {
		return 0, err
	}
	return changed, nil
}

// PrunePinsForMissingExpenses drops pins whose target ID is not in the
// supplied list of valid expense IDs. Used by DeleteMajorExpense and on
// startup to prevent orphaned pins from quietly hiding transactions.
func (dl *DataLoader) PrunePinsForMissingExpenses(validIDs map[string]bool) error {
	pins, err := dl.LoadTransactionPins()
	if err != nil {
		return err
	}
	changed := false
	for hash, id := range pins {
		if !validIDs[id] {
			delete(pins, hash)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.transactionPinsPath(), data, 0644)
}
