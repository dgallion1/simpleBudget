package dataloader

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"budget2/internal/models"
)

const amazonEnrichmentFile = "amazon_enrichment.json"

// amazonEnrichmentPath is the on-disk location of the enrichment map.
// File format: {"<tx-hash>": "Amazon: <product label>"}.
// Lives alongside aliases.json so it shares the same encryption path
// (the file is encrypted at rest when storage encryption is on).
func (dl *DataLoader) amazonEnrichmentPath() string {
	return filepath.Join(dl.CSVDirectory, amazonEnrichmentFile)
}

// LoadAmazonEnrichment reads the hash->label map from disk. A missing
// file is not an error — enrichment is opt-in (users without Amazon
// data won't have generated this file).
func (dl *DataLoader) LoadAmazonEnrichment() (map[string]string, error) {
	path := dl.amazonEnrichmentPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	out := make(map[string]string)
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid amazon enrichment file: %w", err)
	}
	return out, nil
}

// SaveAmazonEnrichment writes the full map to disk (overwrites). The
// CLI calls this after a successful matching pass.
func (dl *DataLoader) SaveAmazonEnrichment(m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.amazonEnrichmentPath(), data, 0644)
}

// applyAmazonEnrichment stamps Transaction.EnrichedDescription on each
// transaction whose hash appears in the enrichment map. Failure to load
// is non-fatal: users without enrichment data see no behavior change.
func (dl *DataLoader) applyAmazonEnrichment(transactions []models.Transaction) []models.Transaction {
	enrich, err := dl.LoadAmazonEnrichment()
	if err != nil {
		log.Printf("Warning: could not load amazon enrichment: %v", err)
		return transactions
	}
	if len(enrich) == 0 {
		return transactions
	}
	for i := range transactions {
		if label, ok := enrich[transactions[i].Hash]; ok {
			transactions[i].EnrichedDescription = label
		}
	}
	return transactions
}
