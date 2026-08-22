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
// File format: {"<tx-identity>": "Amazon: <product label>"}, where the
// identity is a StableID for anything written since StableID landed and a
// legacy content hash for older entries.
// Lives alongside aliases.json so it shares the same encryption path
// (the file is encrypted at rest when storage encryption is on).
func (dl *DataLoader) amazonEnrichmentPath() string {
	return filepath.Join(dl.CSVDirectory, amazonEnrichmentFile)
}

// LoadAmazonEnrichment reads the hash->label map from disk. A missing
// file is not an error — enrichment is opt-in (users without Amazon
// data won't have generated this file).
func (dl *DataLoader) LoadAmazonEnrichment() (map[string]string, error) {
	tx, done := dl.beginWrite()
	defer done()
	path := dl.amazonEnrichmentPath()
	data, err := tx.ReadFile(path)
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
// CLI calls this after a successful matching pass. writeMu makes this
// call and LoadAmazonEnrichment each atomic against concurrent writers
// individually, but LoadAmazonEnrichment releases writeMu before
// returning, so a caller that reads then saves (cmd/enrich-amazon/main.go)
// still spans two acquisitions with a lost-update window between them --
// an atomic read-modify-write across that pair would need the *Locked
// split the other four files got.
//
// Legacy-hash keys that name a currently loaded row are rekeyed to that row's
// StableID on the way out, the same opportunistic migration the pins and
// duplicate-decision stores do. Keys naming no loaded row are written back
// untouched -- the row is probably just outside the loaded set.
func (dl *DataLoader) SaveAmazonEnrichment(m map[string]string) error {
	tx, done := dl.beginWrite()
	defer done()
	rekeyToStable(m, dl.stableIDIndex())
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return tx.WriteFile(dl.amazonEnrichmentPath(), data, 0644)
}

// applyAmazonEnrichment stamps Transaction.EnrichedDescription on each
// transaction whose identity appears in the enrichment map -- StableID
// first, legacy content hash second, so a map written before StableID
// existed keeps matching with no migration step. Failure to load is
// non-fatal: users without enrichment data see no behavior change.
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
		if label, _, ok := models.ResolveByIdentity(enrich, transactions[i]); ok {
			transactions[i].EnrichedDescription = label
		}
	}
	return transactions
}
