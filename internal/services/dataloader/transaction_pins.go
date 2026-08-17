package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

const transactionPinsFile = "transaction_pins.json"

// transactionPinsPath returns the path to the transaction_pins.json file.
func (dl *DataLoader) transactionPinsPath() string {
	return filepath.Join(dl.CSVDirectory, transactionPinsFile)
}

// LoadTransactionPins reads the identity → MajorExpense.ID mapping from disk.
// Keys are StableIDs for everything written since StableID landed and legacy
// content hashes for older entries, so resolve a transaction against the
// result with models.ResolveByIdentity (or use PinFor) rather than indexing by
// hash alone. Returns an empty map if the file does not exist.
func (dl *DataLoader) LoadTransactionPins() (map[string]string, error) {
	tx, done := dl.beginWrite()
	defer done()
	return dl.loadTransactionPinsLocked(tx)
}

// loadTransactionPinsLocked is LoadTransactionPins' body. Caller holds the
// sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) loadTransactionPinsLocked(tx *storage.SharedTx) (map[string]string, error) {
	path := dl.transactionPinsPath()
	data, err := tx.ReadFile(path)
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

// writePinsRawLocked marshals and persists the pin map exactly as given.
// Caller holds the sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) writePinsRawLocked(tx *storage.SharedTx, pins map[string]string) error {
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	return tx.WriteFile(dl.transactionPinsPath(), data, 0644)
}

// writePinsLocked rekeys resolvable legacy-hash entries to StableID and then
// persists the map. This is the whole migration: no one-shot pass, just every
// save moving the entries it can identify, leaving the rest to be moved when
// their rows are next loaded. Caller holds the sequence opened by beginWrite
// and passes its transaction.
func (dl *DataLoader) writePinsLocked(tx *storage.SharedTx, pins map[string]string) error {
	rekeyToStable(pins, dl.stableIDIndex())
	return dl.writePinsRawLocked(tx, pins)
}

// WriteTransactionPins persists a pin map verbatim, replacing the whole file.
// Verbatim is the point: it is the one write path that does NOT rekey legacy
// hashes to StableID, so a caller (a test staging a legacy-keyed sidecar, a
// restore replaying a snapshot) can put exactly the bytes it means to on disk.
// Everything else should go through SetTransactionPin(s).
func (dl *DataLoader) WriteTransactionPins(pins map[string]string) error {
	tx, done := dl.beginWrite()
	defer done()
	if pins == nil {
		pins = make(map[string]string)
	}
	return dl.writePinsRawLocked(tx, pins)
}

// PinFor resolves a transaction's pinned major-expense ID, trying its StableID
// first and falling back to its legacy content Hash. The fallback is what lets
// a pins file written before StableID existed keep working untouched: no
// migration step, no user action, and the entry moves to its StableID the next
// time the file is written.
//
// A missing or unreadable pins file reports "not pinned" rather than an error
// -- pinning is opt-in, and every caller's fallback is keyword/amount matching.
func (dl *DataLoader) PinFor(t models.Transaction) (string, bool) {
	pins, err := dl.LoadTransactionPins()
	if err != nil {
		return "", false
	}
	id, _, ok := models.ResolveByIdentity(pins, t)
	return id, ok
}

// SetTransactionPin pins a transaction to a major-expense ID. The key may be
// either a StableID or a legacy content hash; a legacy hash that names a
// currently loaded row is normalized to that row's StableID, so a pin set from
// a hash-rendering UI and an unpin issued later against the same hash land on
// the same entry. An empty expenseID removes the pin.
func (dl *DataLoader) SetTransactionPin(hash, expenseID string) error {
	if hash == "" {
		return fmt.Errorf("transaction hash is required")
	}
	tx, done := dl.beginWrite()
	defer done()
	pins, err := dl.loadTransactionPinsLocked(tx)
	if err != nil {
		return fmt.Errorf("load transaction pins: %w", err)
	}
	key := dl.canonicalKey(hash)
	if key != hash {
		// Drop the legacy duplicate; the StableID entry is authoritative.
		delete(pins, hash)
	}
	if expenseID == "" {
		delete(pins, key)
	} else {
		pins[key] = expenseID
	}
	return dl.writePinsLocked(tx, pins)
}

// ClearTransactionPin removes the pin for a transaction hash. No-op if
// the hash isn't pinned.
//
// This delegates to the PUBLIC SetTransactionPin rather than opening a
// sequence itself: neither writeMu nor the storage shared hold is reentrant,
// and SetTransactionPin already opens one for the whole sequence, so opening a
// second here would deadlock.
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
	tx, done := dl.beginWrite()
	defer done()
	pins, err := dl.loadTransactionPinsLocked(tx)
	if err != nil {
		return 0, err
	}
	changed := 0
	// dirty covers edits that are cleanup rather than a change of the
	// user's intent -- collapsing a legacy-hash entry onto its StableID.
	// Those must still reach disk, but must not inflate the count callers
	// render as "N pins changed".
	dirty := false
	for hash, expenseID := range updates {
		if hash == "" {
			continue
		}
		// Same key normalization as SetTransactionPin: a legacy hash
		// naming a loaded row is written under that row's StableID.
		key := dl.canonicalKey(hash)
		pinned := false
		if _, ok := pins[key]; ok {
			pinned = true
		}
		if key != hash {
			if _, ok := pins[hash]; ok {
				pinned = true
				delete(pins, hash)
				dirty = true
			}
		}
		if expenseID == "" {
			if pinned {
				delete(pins, key)
				changed++
			}
			continue
		}
		if pins[key] != expenseID {
			pins[key] = expenseID
			changed++
		}
	}
	if changed == 0 && !dirty {
		return 0, nil
	}
	if err := dl.writePinsLocked(tx, pins); err != nil {
		return 0, err
	}
	return changed, nil
}

// PrunePinsForMissingExpenses drops pins whose target ID is not in the
// supplied list of valid expense IDs. Currently unused: no caller in this
// package or the handler layer invokes it -- ArchiveMajorExpense's
// per-expense pin detachment superseded the old DeleteMajorExpense ->
// PrunePinsForMissingExpenses flow. Retained as a defensive cleanup path.
//
// It opens its own sequence. Do not call it from DeleteMajorExpense or any
// other method in this package that already holds one -- neither writeMu nor
// the storage shared hold is reentrant, so nesting would deadlock.
func (dl *DataLoader) PrunePinsForMissingExpenses(validIDs map[string]bool) error {
	tx, done := dl.beginWrite()
	defer done()
	pins, err := dl.loadTransactionPinsLocked(tx)
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
	return dl.writePinsLocked(tx, pins)
}
