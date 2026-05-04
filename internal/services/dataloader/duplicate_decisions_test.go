package dataloader

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateDecisionsPath(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	want := filepath.Join(loader.CSVDirectory, "duplicate_decisions.json")
	if got := loader.duplicateDecisionsPath(); got != want {
		t.Errorf("duplicateDecisionsPath() = %q, want %q", got, want)
	}
}

func TestLoadDuplicateDecisions_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestLoadDuplicateDecisions_ValidFile(t *testing.T) {
	doc := duplicateDecisionsDoc{
		Decisions: map[string]DuplicateDecision{
			"key1": {
				KeptHash:       "ha",
				SuppressedHash: "hb",
				Outcome:        DuplicateOutcomeKeptWinner,
				DecidedAt:      time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC),
			},
		},
	}
	data, _ := json.Marshal(doc)
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": string(data),
	})
	defer cleanup()

	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d, ok := got["key1"]
	if !ok {
		t.Fatalf("missing key1 in %+v", got)
	}
	if d.KeptHash != "ha" || d.SuppressedHash != "hb" || d.Outcome != DuplicateOutcomeKeptWinner {
		t.Errorf("round-trip mismatch: %+v", d)
	}
}

func TestLoadDuplicateDecisions_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": "{{not json",
	})
	defer cleanup()
	if _, err := loader.LoadDuplicateDecisions(); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveDuplicateDecision_RoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	dec := DuplicateDecision{
		KeptHash:       "h1",
		SuppressedHash: "h2",
		Outcome:        DuplicateOutcomeKeptWinner,
		DecidedAt:      time.Now().UTC(),
	}
	if err := loader.SaveDuplicateDecision("pairA", dec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got["pairA"].KeptHash != "h1" {
		t.Errorf("expected h1, got %+v", got["pairA"])
	}
}

func TestSaveDuplicateDecision_KeptBoth(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	dec := DuplicateDecision{
		Outcome:   DuplicateOutcomeKeptBoth,
		DecidedAt: time.Now().UTC(),
	}
	if err := loader.SaveDuplicateDecision("pairB", dec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := loader.LoadDuplicateDecisions()
	if got["pairB"].Outcome != DuplicateOutcomeKeptBoth {
		t.Errorf("expected kept_both, got %+v", got["pairB"])
	}
}

func TestSaveDuplicateDecision_EmptyKeyRejected(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now()})
	if err == nil {
		t.Error("expected error for empty pair key")
	}
}

func TestSaveDuplicateDecision_UnknownOutcomeRejected(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("k", DuplicateDecision{Outcome: "weird"})
	if err == nil {
		t.Error("expected error for unknown outcome")
	}
}

func TestSaveDuplicateDecision_KeptWinnerRequiresBothHashes(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("k", DuplicateDecision{
		Outcome:   DuplicateOutcomeKeptWinner,
		KeptHash:  "h1",
		DecidedAt: time.Now(),
	})
	if err == nil {
		t.Error("expected error for kept_winner missing suppressed_hash")
	}
}

func TestClearDuplicateDecision(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	loader.SaveDuplicateDecision("k1", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now(),
	})
	loader.SaveDuplicateDecision("k2", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now(),
	})
	if err := loader.ClearDuplicateDecision("k1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := loader.LoadDuplicateDecisions()
	if _, ok := got["k1"]; ok {
		t.Error("k1 should be cleared")
	}
	if _, ok := got["k2"]; !ok {
		t.Error("k2 should remain")
	}
}

func TestClearDuplicateDecision_Missing_NoError(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	if err := loader.ClearDuplicateDecision("never-saved"); err != nil {
		t.Errorf("clear of missing key should not error, got: %v", err)
	}
}
