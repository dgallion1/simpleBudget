// Package main is the enrich-amazon CLI: it reads an Amazon order-history
// export, matches each shipment against the user's bank transactions, and
// writes data/amazon_enrichment.json — a hash→label map that the
// dataloader applies on every load.
//
// Usage:
//
//	enrich-amazon --amazon-dir ~/amazon-export [--data-dir ./data] [--dry-run]
//
// The enrichment file lives alongside aliases.json so it inherits the
// same encryption-at-rest path. Re-run safely: each run regenerates the
// full map from current Amazon data + current transactions.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"

	"budget2/internal/services/amazon"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

func main() {
	var (
		amazonDir  string
		dataDir    string
		windowDays int
		dryRun     bool
		preview    int
	)
	flag.StringVar(&amazonDir, "amazon-dir", "", "directory containing Amazon order export (required)")
	flag.StringVar(&dataDir, "data-dir", "data", "budget2 data directory (where bank CSVs live)")
	flag.IntVar(&windowDays, "window", 5, "match window in days around ship date")
	flag.BoolVar(&dryRun, "dry-run", false, "report matches without writing the enrichment file")
	flag.IntVar(&preview, "preview", 10, "number of sample matches to print")
	flag.Parse()

	if amazonDir == "" {
		fmt.Fprintln(os.Stderr, "error: --amazon-dir is required")
		flag.Usage()
		os.Exit(2)
	}

	if err := run(amazonDir, dataDir, windowDays, dryRun, preview); err != nil {
		log.Fatalf("enrich-amazon: %v", err)
	}
}

func run(amazonDir, dataDir string, windowDays int, dryRun bool, previewN int) error {
	shipments, err := amazon.LoadDir(amazonDir)
	if err != nil {
		return fmt.Errorf("load amazon dir: %w", err)
	}
	fmt.Printf("Loaded %d shipments from %s\n", len(shipments), amazonDir)
	if len(shipments) == 0 {
		return fmt.Errorf("no shipments found — check --amazon-dir path")
	}

	store, err := storage.New(dataDir)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	if store.IsEncrypted() && !store.IsUnlocked() {
		creds, err := readCredentials()
		if err != nil {
			return fmt.Errorf("read credentials: %w", err)
		}
		if err := store.Unlock(creds); err != nil {
			return fmt.Errorf("unlock storage: %w", err)
		}
	}
	loader := dataloader.New(dataDir, store)

	ts, err := loader.LoadData()
	if err != nil {
		return fmt.Errorf("load transactions: %w", err)
	}
	fmt.Printf("Loaded %d transactions from %s\n", ts.Len(), dataDir)

	opts := amazon.MatchOptions{WindowDays: windowDays}
	matches := amazon.Match(ts.Transactions, shipments, opts)

	// Second pass: try Order-ID-in-description for whatever's left.
	// Carry forward Order IDs already attributed in pass 1 so a second
	// transaction whose description happens to contain one of them
	// doesn't reuse the same shipment's product label.
	already := make(map[string]bool, len(matches))
	consumedOrders := make(map[string]bool)
	for _, m := range matches {
		already[m.TxHash] = true
		for _, id := range m.OrderIDs {
			consumedOrders[id] = true
		}
	}
	descMatches := amazon.MatchByDescription(ts.Transactions, shipments, already, consumedOrders, opts)
	matches = append(matches, descMatches...)

	enrichment := make(map[string]string, len(matches))
	for _, m := range matches {
		if m.TxHash == "" {
			continue
		}
		enrichment[m.TxHash] = m.Label
	}

	// Count amazon-relevant transactions for context
	var amzTxCount int
	for _, t := range ts.Transactions {
		if isAmazonDesc(t.Description) {
			amzTxCount++
		}
	}

	fmt.Printf("\nMatch summary:\n")
	fmt.Printf("  Amazon-keyword transactions: %d\n", amzTxCount)
	fmt.Printf("  Matched (amount+window):     %d\n", len(matches)-len(descMatches))
	fmt.Printf("  Matched (order-id in desc):  %d\n", len(descMatches))
	fmt.Printf("  Total enriched:              %d\n", len(enrichment))
	if amzTxCount > 0 {
		fmt.Printf("  Coverage:                    %.1f%%\n", 100*float64(len(enrichment))/float64(amzTxCount))
	}

	if previewN > 0 && len(matches) > 0 {
		fmt.Printf("\nSample matches:\n")
		samples := make([]amazon.MatchResult, len(matches))
		copy(samples, matches)
		sort.Slice(samples, func(i, j int) bool { return samples[i].Label < samples[j].Label })
		n := previewN
		if n > len(samples) {
			n = len(samples)
		}
		for i := 0; i < n; i++ {
			fmt.Printf("  %s\n", samples[i].Label)
		}
	}

	if dryRun {
		fmt.Println("\n--dry-run: not writing enrichment file")
		return nil
	}

	if err := loader.SaveAmazonEnrichment(enrichment); err != nil {
		return fmt.Errorf("save enrichment: %w", err)
	}
	fmt.Printf("\nWrote enrichment file (%d entries)\n", len(enrichment))
	return nil
}

// isAmazonDesc duplicates the matcher's heuristic for the summary count.
// Kept private here so the matcher's internals stay unexported.
func isAmazonDesc(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "amazon") || strings.Contains(low, "amzn")
}

// readCredentials retrieves the storage password from BUDGET2_PASSWORD or
// prompts for it on a TTY. Returns an empty string for auth methods that
// don't need credentials (age, YubiKey) — Storage.Unlock handles those
// even when given an empty input.
func readCredentials() (string, error) {
	if env := os.Getenv("BUDGET2_PASSWORD"); env != "" {
		return env, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("storage is locked: set BUDGET2_PASSWORD or run interactively")
	}
	fmt.Fprint(os.Stderr, "Storage password (or empty for age/YubiKey): ")
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}
