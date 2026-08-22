package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"budget2/internal/services/storage"
)

const duplicateDecisionsFile = "duplicate_decisions.json"

// Outcome string constants for DuplicateDecision.Outcome.
const (
	DuplicateOutcomeKeptWinner = "kept_winner"
	DuplicateOutcomeKeptBoth   = "kept_both"
)

// DuplicateDecision records the user's resolution of a single
// near-duplicate candidate pair. Outcome controls how the loader
// applies it on subsequent loads:
//
//   - kept_winner: SuppressedHash is excluded from aggregations
//     via Transaction.Suppressed = true.
//   - kept_both: both transactions stay live; the pair is no longer
//     re-flagged as a candidate.
//
// KeptHash and SuppressedHash hold transaction identities: StableIDs for
// anything written since StableID landed, legacy content hashes for older
// entries. The loader indexes rows under both forms, so either resolves.
type DuplicateDecision struct {
	KeptHash       string    `json:"kept_hash,omitempty"`
	SuppressedHash string    `json:"suppressed_hash,omitempty"`
	Outcome        string    `json:"outcome"`
	DecidedAt      time.Time `json:"decided_at"`
}

// duplicateDecisionsDoc is the on-disk wire format. Keeping decisions
// nested under a "decisions" key leaves room for future top-level
// metadata (schema version, etc.) without a breaking change.
type duplicateDecisionsDoc struct {
	Decisions map[string]DuplicateDecision `json:"decisions"`
}

func (dl *DataLoader) duplicateDecisionsPath() string {
	return filepath.Join(dl.CSVDirectory, duplicateDecisionsFile)
}

// LoadDuplicateDecisions reads the pairKey → DuplicateDecision map
// from disk. Returns an empty map when the file is missing or empty;
// returns an error only when the file exists with non-empty contents
// that fail to parse. Callers can rely on a non-error result always
// being a usable map.
func (dl *DataLoader) LoadDuplicateDecisions() (map[string]DuplicateDecision, error) {
	tx, done := dl.beginWrite()
	defer done()
	return dl.loadDuplicateDecisionsLocked(tx)
}

// loadDuplicateDecisionsLocked is LoadDuplicateDecisions' body. Caller holds
// the sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) loadDuplicateDecisionsLocked(tx *storage.SharedTx) (map[string]DuplicateDecision, error) {
	path := dl.duplicateDecisionsPath()
	data, err := tx.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]DuplicateDecision), nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return make(map[string]DuplicateDecision), nil
	}
	var doc duplicateDecisionsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid duplicate_decisions file: %w", err)
	}
	if doc.Decisions == nil {
		return make(map[string]DuplicateDecision), nil
	}
	return doc.Decisions, nil
}

// SaveDuplicateDecision writes a decision keyed by pairKey, replacing
// any prior decision under the same key. Validates outcome and the
// hash invariants for kept_winner.
func (dl *DataLoader) SaveDuplicateDecision(pairKey string, decision DuplicateDecision) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	if err := validateDuplicateDecision(decision); err != nil {
		return err
	}
	tx, done := dl.beginWrite()
	defer done()
	decisions, err := dl.loadDuplicateDecisionsLocked(tx)
	if err != nil {
		return err
	}
	if decision.DecidedAt.IsZero() {
		decision.DecidedAt = time.Now().UTC()
	}
	// The panel still posts legacy hashes (it renders Transaction.Hash);
	// store the StableID of the row each names so the entry is durable.
	decision.KeptHash = dl.canonicalKey(decision.KeptHash)
	decision.SuppressedHash = dl.canonicalKey(decision.SuppressedHash)
	decisions[pairKey] = decision
	// A decision the user made before StableID existed lives under the
	// pair's old key; drop it now that the same pair has been re-decided
	// under the new one, or both would apply.
	for _, legacy := range dl.legacyPairKeysFor(pairKey) {
		delete(decisions, legacy)
	}
	return dl.writeDecisionsLocked(tx, decisions)
}

// ClearDuplicateDecision removes a decision. No-op if the key isn't
// present. Used by the review panel's "Undo" button.
func (dl *DataLoader) ClearDuplicateDecision(pairKey string) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	tx, done := dl.beginWrite()
	defer done()
	decisions, err := dl.loadDuplicateDecisionsLocked(tx)
	if err != nil {
		return err
	}
	// Undo has to reach the entry under whichever key it was filed: the
	// caller supplies the pair's current key, but a decision made before
	// StableID existed is still under the old one.
	keys := append([]string{pairKey}, dl.legacyPairKeysFor(pairKey)...)
	found := false
	for _, k := range keys {
		if _, ok := decisions[k]; ok {
			delete(decisions, k)
			found = true
		}
	}
	if !found {
		return nil
	}
	return dl.writeDecisionsLocked(tx, decisions)
}

// writeDecisionsLocked rekeys resolvable pre-StableID entries -- both the map
// key and the identities inside each decision -- and then marshals and
// persists the map. Entries naming a pair that is not in the current load are
// written back untouched: the rows are probably outside the loaded date range,
// not gone. Caller holds the sequence opened by beginWrite and passes its
// transaction.
func (dl *DataLoader) writeDecisionsLocked(tx *storage.SharedTx, decisions map[string]DuplicateDecision) error {
	rekeyToStable(decisions, dl.stablePairKeyIndex())
	if index := dl.stableIDIndex(); len(index) > 0 {
		for key, decision := range decisions {
			kept, keptOK := index[decision.KeptHash]
			suppressed, suppressedOK := index[decision.SuppressedHash]
			if !keptOK && !suppressedOK {
				continue
			}
			if keptOK {
				decision.KeptHash = kept
			}
			if suppressedOK {
				decision.SuppressedHash = suppressed
			}
			decisions[key] = decision
		}
	}
	return dl.writeDecisionsRawLocked(tx, decisions)
}

// writeDecisionsRawLocked marshals and persists the decisions map exactly as
// given. Caller holds the sequence opened by beginWrite and passes its
// transaction.
func (dl *DataLoader) writeDecisionsRawLocked(tx *storage.SharedTx, decisions map[string]DuplicateDecision) error {
	doc := duplicateDecisionsDoc{Decisions: decisions}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return tx.WriteFile(dl.duplicateDecisionsPath(), data, 0644)
}

func validateDuplicateDecision(d DuplicateDecision) error {
	switch d.Outcome {
	case DuplicateOutcomeKeptWinner:
		if d.KeptHash == "" || d.SuppressedHash == "" {
			return fmt.Errorf("kept_winner requires both kept_hash and suppressed_hash")
		}
	case DuplicateOutcomeKeptBoth:
		// kept_both ignores hashes; nothing to validate.
	default:
		return fmt.Errorf("unknown outcome %q (want %q or %q)",
			d.Outcome, DuplicateOutcomeKeptWinner, DuplicateOutcomeKeptBoth)
	}
	return nil
}
