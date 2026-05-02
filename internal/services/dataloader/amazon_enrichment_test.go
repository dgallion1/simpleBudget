package dataloader

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestLoadAmazonEnrichment_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	got, err := loader.LoadAmazonEnrichment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %d", len(got))
	}
}

func TestLoadAmazonEnrichment_ValidFile(t *testing.T) {
	data, _ := json.Marshal(map[string]string{
		"abc123": "Amazon: Coffee Beans",
		"def456": "Amazon: Filters +1 more",
	})
	_, loader, cleanup := setupTestDir(t, map[string]string{
		amazonEnrichmentFile: string(data),
	})
	defer cleanup()

	got, err := loader.LoadAmazonEnrichment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["abc123"] != "Amazon: Coffee Beans" {
		t.Errorf("got[abc123] = %q", got["abc123"])
	}
}

func TestLoadAmazonEnrichment_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		amazonEnrichmentFile: "not json{",
	})
	defer cleanup()

	if _, err := loader.LoadAmazonEnrichment(); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveAndLoadAmazonEnrichment_RoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	in := map[string]string{"h1": "Amazon: Item A", "h2": "Amazon: Item B"}
	if err := loader.SaveAmazonEnrichment(in); err != nil {
		t.Fatalf("save error: %v", err)
	}

	got, err := loader.LoadAmazonEnrichment()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(got) != 2 || got["h1"] != "Amazon: Item A" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestApplyAmazonEnrichment_StampsField(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"h1": "Amazon: Coffee"})
	_, loader, cleanup := setupTestDir(t, map[string]string{
		amazonEnrichmentFile: string(data),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "AMZN MKTP US"},
		{Hash: "h2", Description: "Walmart"},
	}
	got := loader.applyAmazonEnrichment(txns)

	if got[0].EnrichedDescription != "Amazon: Coffee" {
		t.Errorf("h1 EnrichedDescription = %q", got[0].EnrichedDescription)
	}
	if got[1].EnrichedDescription != "" {
		t.Errorf("h2 should not be stamped, got %q", got[1].EnrichedDescription)
	}
	// Confirm Label() picks up the enrichment.
	if got[0].Label() != "Amazon: Coffee" {
		t.Errorf("Label() = %q", got[0].Label())
	}
}

func TestApplyAmazonEnrichment_NoFileIsNoOp(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	txns := []models.Transaction{{Hash: "h1", Description: "AMZN"}}
	got := loader.applyAmazonEnrichment(txns)
	if got[0].EnrichedDescription != "" {
		t.Errorf("expected unchanged, got %q", got[0].EnrichedDescription)
	}
}
