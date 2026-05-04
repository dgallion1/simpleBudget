package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
// from disk. Returns an empty map if the file does not exist.
func (dl *DataLoader) LoadDuplicateDecisions() (map[string]DuplicateDecision, error) {
	path := dl.duplicateDecisionsPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]DuplicateDecision), nil
		}
		return nil, err
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
	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		return err
	}
	if decision.DecidedAt.IsZero() {
		decision.DecidedAt = time.Now().UTC()
	}
	decisions[pairKey] = decision
	return dl.writeDecisions(decisions)
}

// ClearDuplicateDecision removes a decision. No-op if the key isn't
// present. Used by the review panel's "Undo" button.
func (dl *DataLoader) ClearDuplicateDecision(pairKey string) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		return err
	}
	if _, ok := decisions[pairKey]; !ok {
		return nil
	}
	delete(decisions, pairKey)
	return dl.writeDecisions(decisions)
}

func (dl *DataLoader) writeDecisions(decisions map[string]DuplicateDecision) error {
	doc := duplicateDecisionsDoc{Decisions: decisions}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.duplicateDecisionsPath(), data, 0644)
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
