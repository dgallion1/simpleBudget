package dataloader

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
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

func TestLoadDuplicateDecisions_EmptyFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": "",
	})
	defer cleanup()
	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("empty file should not error, got: %v", err)
	}
	if got == nil {
		t.Error("empty file should return non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("empty file should return empty map, got %d entries", len(got))
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

func TestLookupDuplicateDecision_ExactKey(t *testing.T) {
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
	got, ok, err := loader.LookupDuplicateDecision("pairA")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for an exact-keyed entry")
	}
	if got.Outcome != DuplicateOutcomeKeptWinner {
		t.Errorf("outcome = %q, want %q", got.Outcome, DuplicateOutcomeKeptWinner)
	}
}

// TestLookupDuplicateDecision_LegacyAlias reuses the fixture recipe of
// TestApplyDuplicateDetection_LegacyPairKeyStillResolves (stable_id_test.go):
// a decision filed under the pair's pre-StableID content-hash key must still
// be found by a lookup keyed on the current StableID-derived key, via the
// alias index a load publishes.
func TestLookupDuplicateDecision_LegacyAlias(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")
	billPay.AccountID = "usaa-checking"
	check.AccountID = "usaa-checking"
	billPay.StableID = models.StableIDFor("usaa-checking", billPay.Date, -158043, 0)
	check.StableID = models.StableIDFor("usaa-checking", check.Date, -158043, 0)

	legacyKey := pairKey(billPay.Hash, check.Hash)
	doc := duplicateDecisionsDoc{Decisions: map[string]DuplicateDecision{
		legacyKey: {
			KeptHash:       billPay.Hash,
			SuppressedHash: check.Hash,
			Outcome:        DuplicateOutcomeKeptWinner,
			DecidedAt:      time.Now().UTC(),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": string(data),
	})
	defer cleanup()

	// Populate the alias index the same way a real load does.
	loader.applyDuplicateDetection([]models.Transaction{billPay, check})

	currentKey := pairKey(billPay.StableID, check.StableID)
	if currentKey == legacyKey {
		t.Fatal("fixture is degenerate: the StableID key equals the legacy key")
	}

	got, ok, err := loader.LookupDuplicateDecision(currentKey)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a legacy-keyed entry reached via its current key")
	}
	if got.Outcome != DuplicateOutcomeKeptWinner {
		t.Errorf("outcome = %q, want %q", got.Outcome, DuplicateOutcomeKeptWinner)
	}
}

// TestLookupDuplicateDecision_PrefersExactOverLegacy reuses the fixture
// recipe of TestApplyDuplicateDetection_LegacyPairKeyStillResolves
// (stable_id_test.go:362). When the decisions file holds entries under BOTH
// the pair's current (StableID-derived) key and its legacy (content-hash-
// derived) key, the exact-key entry must win: LookupDuplicateDecision checks
// the exact key before ranging over legacy aliases, so reversing that order
// would return the legacy entry instead.
func TestLookupDuplicateDecision_PrefersExactOverLegacy(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")
	billPay.AccountID = "usaa-checking"
	check.AccountID = "usaa-checking"
	billPay.StableID = models.StableIDFor("usaa-checking", billPay.Date, -158043, 0)
	check.StableID = models.StableIDFor("usaa-checking", check.Date, -158043, 0)

	legacyKey := pairKey(billPay.Hash, check.Hash)
	currentKey := pairKey(billPay.StableID, check.StableID)
	if currentKey == legacyKey {
		t.Fatal("fixture is degenerate: the StableID key equals the legacy key")
	}

	doc := duplicateDecisionsDoc{Decisions: map[string]DuplicateDecision{
		currentKey: {
			Outcome:   DuplicateOutcomeKeptBoth,
			DecidedAt: time.Now().UTC(),
		},
		legacyKey: {
			KeptHash:       billPay.Hash,
			SuppressedHash: check.Hash,
			Outcome:        DuplicateOutcomeKeptWinner,
			DecidedAt:      time.Now().UTC(),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": string(data),
	})
	defer cleanup()

	// Populate the alias index the same way a real load does.
	loader.applyDuplicateDetection([]models.Transaction{billPay, check})

	got, ok, err := loader.LookupDuplicateDecision(currentKey)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for the current-keyed entry")
	}
	if got.Outcome != DuplicateOutcomeKeptBoth {
		t.Errorf("outcome = %q, want %q (the current-key entry, not the legacy-key entry)",
			got.Outcome, DuplicateOutcomeKeptBoth)
	}
}

func TestLookupDuplicateDecision_NotFound(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	if err := loader.SaveDuplicateDecision("someOtherPair", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := loader.LookupDuplicateDecision("never-saved")
	if err != nil {
		t.Fatalf("lookup should not error for an absent key, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false, got decision %+v", got)
	}
}

func TestClearDuplicateDecision_Missing_NoError(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	if err := loader.ClearDuplicateDecision("never-saved"); err != nil {
		t.Errorf("clear of missing key should not error, got: %v", err)
	}
}
