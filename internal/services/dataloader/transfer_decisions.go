package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"budget2/internal/services/storage"
	"budget2/internal/services/transfers"
)

const transferDecisionsFile = "transfer_decisions.json"

// transferDecisionsDoc is the on-disk wire format, keyed by pair key. The
// wrapping object matches every other list-shaped sidecar so a schema version
// or other top-level metadata can be added later without breaking old files.
type transferDecisionsDoc struct {
	Decisions map[string]transfers.Decision `json:"decisions"`
}

func (dl *DataLoader) transferDecisionsPath() string {
	return filepath.Join(dl.CSVDirectory, transferDecisionsFile)
}

// LoadTransferDecisions reads the pairKey -> Decision map from disk. A
// missing or empty file is not an error -- it is the normal starting state --
// so a non-error result is always a usable map. An error means the file
// exists with contents that do not parse.
//
// Entries are keyed on StableID pairs only. Unlike the older sidecars there
// is no legacy content-hash form to fall back to: this store did not exist
// before StableID did.
func (dl *DataLoader) LoadTransferDecisions() (map[string]transfers.Decision, error) {
	tx, done := dl.beginWrite()
	defer done()
	return dl.loadTransferDecisionsLocked(tx)
}

// loadTransferDecisionsLocked is LoadTransferDecisions' body. Caller holds the
// sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) loadTransferDecisionsLocked(tx *storage.SharedTx) (map[string]transfers.Decision, error) {
	data, err := tx.ReadFile(dl.transferDecisionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]transfers.Decision), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return make(map[string]transfers.Decision), nil
	}
	var doc transferDecisionsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid transfer_decisions file: %w", err)
	}
	if doc.Decisions == nil {
		return make(map[string]transfers.Decision), nil
	}
	return doc.Decisions, nil
}

// writeTransferDecisionsLocked marshals and persists the decisions map.
// Caller holds the sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) writeTransferDecisionsLocked(tx *storage.SharedTx, decisions map[string]transfers.Decision) error {
	data, err := json.MarshalIndent(transferDecisionsDoc{Decisions: decisions}, "", "  ")
	if err != nil {
		return err
	}
	return tx.WriteFile(dl.transferDecisionsPath(), data, 0644)
}
