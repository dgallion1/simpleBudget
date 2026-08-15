// Package dataloader reads the user's bank-export CSVs, normalizes
// per-bank column layouts into a unified TransactionSet, applies user
// aliases, near-duplicate decisions, Amazon enrichment, and major-expense
// name overrides, and persists side files (aliases, duplicate decisions,
// enrichment maps) back to the encrypted-at-rest storage layer.
package dataloader

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/classifier"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/storage"
)

const aliasFile = "aliases.json"

// DataLoader handles loading and preprocessing of financial data from CSV files.
//
// It is safe for concurrent use. Two mutexes with distinct jobs:
//
//   - stateMu guards the derived fields LoadData stamps and later callers
//     read. Held only across the assignment or the read, never across file
//     I/O.
//   - writeMu (see below) makes each load->modify->save sequence over a
//     JSON sidecar file one critical section.
//
// No method holds both. stateMu sites do no file I/O; writeMu sites touch no
// derived state.
type DataLoader struct {
	CSVDirectory string
	store        *storage.Storage

	// writeMu makes each load->modify->save sequence over a JSON sidecar
	// file ONE critical section. storage.Storage locks only around an
	// individual WriteFile -- and only with an RLock, which is shared -- so
	// it does nothing for a caller that reads, edits in memory and writes
	// back.
	//
	// NOT reentrant. The invariant, without exception: a public method takes
	// writeMu and then calls only *Locked helpers; a *Locked helper never
	// takes writeMu and never calls a public method.
	writeMu sync.Mutex

	// stateMu guards every field below it.
	stateMu               sync.RWMutex
	filteredTransferCount int
	enabledFiles          map[string]bool

	// Populated by every LoadData call. Read-only for callers.
	// The three lists partition the detected pairs: awaiting review,
	// settled as kept_winner, settled as kept_both.
	unresolvedDuplicates []DuplicatePair
	resolvedDuplicates   []DuplicatePair
	keptBothDuplicates   []DuplicatePair
}

// columnMappings maps common bank export column names to our standard names
var columnMappings = map[string][]string{
	"Date": {
		"date", "Date", "DATE",
		"transaction date", "Transaction Date", "TRANSACTION DATE",
		"posted date", "Posted Date", "POSTED DATE",
		"post date", "Post Date", "POST DATE",
		"trans date", "Trans Date", "TRANS DATE",
		"posting date", "Posting Date", "POSTING DATE",
	},
	"Description": {
		"description", "Description", "DESCRIPTION",
		"memo", "Memo", "MEMO",
		"details", "Details", "DETAILS",
		"payee", "Payee", "PAYEE",
		"name", "Name", "NAME",
		"transaction description", "Transaction Description",
		"merchant", "Merchant", "MERCHANT",
		"narrative", "Narrative", "NARRATIVE",
	},
	"Amount": {
		"amount", "Amount", "AMOUNT",
		"value", "Value", "VALUE",
		"transaction amount", "Transaction Amount", "TRANSACTION AMOUNT",
		"sum", "Sum", "SUM",
	},
	"Category": {
		"category", "Category", "CATEGORY",
		"type", "Type", "TYPE",
		"category name", "Category Name",
	},
	"Debit": {
		"debit", "Debit", "DEBIT",
		"withdrawal", "Withdrawal", "WITHDRAWAL",
		"withdrawals", "Withdrawals", "WITHDRAWALS",
		"money out", "Money Out", "MONEY OUT",
		"expense", "Expense", "EXPENSE",
	},
	"Credit": {
		"credit", "Credit", "CREDIT",
		"deposit", "Deposit", "DEPOSIT",
		"deposits", "Deposits", "DEPOSITS",
		"money in", "Money In", "MONEY IN",
		"income", "Income", "INCOME",
	},
	"Status": {
		"status", "Status", "STATUS",
		"transaction status", "Transaction Status", "TRANSACTION STATUS",
		"state", "State",
	},
}

// New creates a new DataLoader
func New(csvDirectory string, store *storage.Storage) *DataLoader {
	return &DataLoader{
		CSVDirectory: csvDirectory,
		enabledFiles: make(map[string]bool),
		store:        store,
	}
}

// normalizeColumnName maps a bank export column name to our standard name
func normalizeColumnName(col string) string {
	col = strings.TrimSpace(col)
	for standard, variants := range columnMappings {
		for _, variant := range variants {
			if col == variant {
				return standard
			}
		}
	}
	return col // Return original if no mapping found
}

// buildColumnIndex creates a normalized column index from CSV headers
func buildColumnIndex(header []string) map[string]int {
	colIndex := make(map[string]int)
	for i, col := range header {
		normalized := normalizeColumnName(col)
		// Only set if not already set (first match wins)
		if _, exists := colIndex[normalized]; !exists {
			colIndex[normalized] = i
		}
	}
	return colIndex
}

// SetEnabledFiles sets which files should be loaded
func (dl *DataLoader) SetEnabledFiles(files []string) {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f] = true
	}
	dl.stateMu.Lock()
	dl.enabledFiles = set
	dl.stateMu.Unlock()
}

// enabledFilesSnapshot returns the current enabled-file set for one load to
// work against. A caller that rewrites the set mid-load therefore affects the
// next load, not this one -- which is both race-free and the behavior a user
// clicking "apply" expects.
func (dl *DataLoader) enabledFilesSnapshot() map[string]bool {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make(map[string]bool, len(dl.enabledFiles))
	for k, v := range dl.enabledFiles {
		out[k] = v
	}
	return out
}

// FilteredTransfers returns how many internal transfers the most recent load
// filtered out. Replaces the former exported FilteredTransferCount field,
// which could not be read safely while another goroutine was loading.
func (dl *DataLoader) FilteredTransfers() int {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return dl.filteredTransferCount
}

// LoadData loads and combines data from all CSV files in the directory
func (dl *DataLoader) LoadData() (*models.TransactionSet, error) {
	return dl.LoadDataContext(context.Background())
}

// LoadDataContext is LoadData with caller-supplied cancellation. It fails fast
// on entry and between CSV files if ctx is cancelled (e.g. the HTTP client
// disconnected), so an abandoned dashboard request stops loading promptly
// instead of parsing every file to completion.
func (dl *DataLoader) LoadDataContext(ctx context.Context) (*models.TransactionSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pattern := filepath.Join(dl.CSVDirectory, "*.csv")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("error finding CSV files: %w", err)
	}

	if len(files) == 0 {
		log.Printf("No CSV files found in %s - returning empty dataset", dl.CSVDirectory)
		return models.NewTransactionSet(nil), nil
	}

	log.Printf("Found %d CSV files in %s", len(files), dl.CSVDirectory)

	var allTransactions []models.Transaction

	enabled := dl.enabledFilesSnapshot()

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		filename := filepath.Base(file)

		// Skip if file list is set and this file is not enabled
		if len(enabled) > 0 && !enabled[filename] {
			log.Printf("Skipping disabled file: %s", filename)
			continue
		}

		transactions, err := dl.loadCSVFile(file)
		if err != nil {
			log.Printf("Warning: failed to load %s: %v", filename, err)
			continue
		}

		log.Printf("Loaded %d transactions from %s", len(transactions), filename)
		allTransactions = append(allTransactions, transactions...)
	}

	if len(allTransactions) == 0 {
		log.Printf("No transactions loaded from CSV files - returning empty dataset")
		return models.NewTransactionSet(nil), nil
	}

	// Preprocess: filter transfers, classify, deduplicate
	allTransactions = dl.filterInternalTransfers(allTransactions)
	allTransactions = classifier.ClassifyTransactions(allTransactions)
	allTransactions = dl.deduplicateTransactions(allTransactions)

	// Detect near-duplicate pairs and apply user decisions. Failure
	// modes are non-fatal: a corrupt decisions file still allows the
	// load to complete, just with all candidates marked unresolved.
	allTransactions = dl.applyDuplicateDetection(allTransactions)

	// Apply user-assigned aliases
	allTransactions = dl.applyAliases(allTransactions)

	// Stamp MajorExpenseName based on user-defined major expenses + pins
	allTransactions = dl.applyMajorExpenseNames(allTransactions)

	// Stamp EnrichedDescription from Amazon order data (no-op if file absent)
	allTransactions = dl.applyAmazonEnrichment(allTransactions)

	// Compute derived fields
	for i := range allTransactions {
		allTransactions[i].ComputeDerivedFields()
	}

	log.Printf("Total transactions after processing: %d", len(allTransactions))

	return models.NewTransactionSet(allTransactions), nil
}

// loadCSVFile loads transactions from a single CSV file
func (dl *DataLoader) loadCSVFile(filePath string) ([]models.Transaction, error) {
	file, err := dl.store.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1 // Allow variable number of fields
	reader.TrimLeadingSpace = true

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("error reading header: %w", err)
	}

	// Build normalized column index map
	colIndex := buildColumnIndex(header)

	// Check for Debit/Credit columns as alternative to Amount
	_, hasAmount := colIndex["Amount"]
	_, hasDebit := colIndex["Debit"]
	_, hasCredit := colIndex["Credit"]
	useDebitCredit := !hasAmount && (hasDebit || hasCredit)

	// Validate required columns
	if _, ok := colIndex["Date"]; !ok {
		return nil, fmt.Errorf("missing required column: Date (tried: %v)", columnMappings["Date"])
	}
	if _, ok := colIndex["Description"]; !ok {
		return nil, fmt.Errorf("missing required column: Description (tried: %v)", columnMappings["Description"])
	}
	if !hasAmount && !useDebitCredit {
		return nil, fmt.Errorf("missing required column: Amount or Debit/Credit (tried: %v)", columnMappings["Amount"])
	}

	if useDebitCredit {
		log.Printf("Using Debit/Credit columns instead of Amount for %s", filepath.Base(filePath))
	}

	var transactions []models.Transaction
	sourceFile := filepath.Base(filePath)
	lineNum := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Warning: error reading line %d: %v", lineNum+1, err)
			lineNum++
			continue
		}
		lineNum++

		t := models.Transaction{
			SourceFile: sourceFile,
		}

		// Parse Date
		if idx, ok := colIndex["Date"]; ok && idx < len(record) {
			dateStr := strings.TrimSpace(record[idx])
			t.Date = parseDate(dateStr)
			if t.Date.IsZero() {
				log.Printf("Warning: could not parse date '%s' on line %d", dateStr, lineNum)
				continue
			}
		}

		// Parse Amount (either from Amount column or Debit/Credit columns)
		if useDebitCredit {
			t.Amount = parseDebitCredit(record, colIndex)
		} else if idx, ok := colIndex["Amount"]; ok && idx < len(record) {
			amountStr := strings.TrimSpace(record[idx])
			t.Amount = parseAmount(amountStr)
		}

		// Parse Description
		if idx, ok := colIndex["Description"]; ok && idx < len(record) {
			t.Description = strings.TrimSpace(record[idx])
		}

		// Parse Category (optional)
		if idx, ok := colIndex["Category"]; ok && idx < len(record) {
			t.Category = strings.TrimSpace(record[idx])
		}

		// Parse Status (optional). Used by near-duplicate detection
		// to distinguish scheduled bill-pays from posted checks.
		if idx, ok := colIndex["Status"]; ok && idx < len(record) {
			t.Status = strings.TrimSpace(record[idx])
		}

		t.Hash = t.ComputeHash()
		transactions = append(transactions, t)
	}

	if usesCreditCardSignConvention(transactions) {
		log.Printf("Detected credit-card sign convention in %s; flipping signs to bank convention", filepath.Base(filePath))
		for i := range transactions {
			transactions[i].Amount = -transactions[i].Amount
			// Re-key on the post-flip amount so dedup, pins, and
			// enrichment all match the value the app actually uses.
			// Without this, the same transaction from a CC-convention
			// source and a bank-convention source hashes differently
			// and survives deduplicateTransactions.
			transactions[i].Hash = transactions[i].ComputeHash()
		}
	}

	return transactions, nil
}

// minSignConventionSample is the minimum number of non-zero amounts required
// before sign-convention auto-detection runs. Below this we don't have enough
// signal and the file is left untouched.
const minSignConventionSample = 10

// ccConventionPositiveThreshold is the share of non-zero amounts that must be
// positive for a file to be treated as a credit-card statement, when no
// bank-style signals are present.
const ccConventionPositiveThreshold = 0.7

// usesCreditCardSignConvention reports whether a parsed CSV's amounts follow
// credit-card statement convention (positive = charge, negative = payment) as
// opposed to bank convention (positive = deposit, negative = debit).
//
// A file is treated as a credit-card statement only when it lacks every
// bank-only signal (paychecks/dividends/interest, paper checks, wire and
// payroll transfers) AND its non-zero amounts are predominantly positive.
// This avoids flipping legitimate bank files that happen to have a positive
// month, while correctly normalizing CC exports whose sign convention is
// inverted relative to the rest of the system.
func usesCreditCardSignConvention(txns []models.Transaction) bool {
	if len(txns) < minSignConventionSample {
		return false
	}
	pos, neg := 0, 0
	for i := range txns {
		t := &txns[i]
		if hasBankOnlySignal(t) {
			return false
		}
		switch {
		case t.Amount > 0:
			pos++
		case t.Amount < 0:
			neg++
		}
	}
	nonZero := pos + neg
	if nonZero < minSignConventionSample {
		return false
	}
	return float64(pos)/float64(nonZero) >= ccConventionPositiveThreshold
}

// hasBankOnlySignal returns true when a transaction carries a marker that
// only appears in bank-account exports (never in credit-card statements):
// classifiable income, paper checks, or wire/payroll transfer descriptions.
func hasBankOnlySignal(t *models.Transaction) bool {
	if classifier.IsPotentialIncome(t) {
		return true
	}
	descLower := strings.ToLower(strings.TrimSpace(t.Description))
	catLower := strings.ToLower(strings.TrimSpace(t.Category))

	if catLower == "check" {
		return true
	}
	if strings.HasPrefix(descLower, "check #") || strings.HasPrefix(descLower, "chk #") {
		return true
	}

	for _, p := range bankOnlyDescPatterns {
		if strings.Contains(descLower, p) {
			return true
		}
	}
	return false
}

// bankOnlyDescPatterns are description fragments that only appear in
// bank-account exports — never in credit-card statements.
var bankOnlyDescPatterns = []string{
	"wire transfer", "funds transfer",
	"direct deposit", "direct dep",
	"payroll",
}

// parseDebitCredit combines Debit and Credit columns into a single amount
// Credits are positive (income), Debits are negative (expenses)
func parseDebitCredit(record []string, colIndex map[string]int) float64 {
	var amount float64

	// Check for credit (positive/income)
	if idx, ok := colIndex["Credit"]; ok && idx < len(record) {
		creditStr := strings.TrimSpace(record[idx])
		if creditStr != "" {
			credit := parseAmount(creditStr)
			if credit != 0 {
				amount = abs(credit) // Credits are positive
			}
		}
	}

	// Check for debit (negative/expense)
	if idx, ok := colIndex["Debit"]; ok && idx < len(record) {
		debitStr := strings.TrimSpace(record[idx])
		if debitStr != "" {
			debit := parseAmount(debitStr)
			if debit != 0 {
				amount = -abs(debit) // Debits are negative
			}
		}
	}

	return amount
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// parseDate tries multiple date formats
func parseDate(s string) time.Time {
	formats := []string{
		"2006-01-02",
		"01/02/2006",
		"1/2/2006",
		"01-02-2006",
		"2006/01/02",
		"Jan 2, 2006",
		"January 2, 2006",
		"2 Jan 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// parseAmount parses an amount string, handling currency symbols and parentheses
func parseAmount(s string) float64 {
	// Remove currency symbols and spaces
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)

	// Handle parentheses for negative numbers: (100.00) -> -100.00
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = "-" + s[1:len(s)-1]
	}

	amount, _ := strconv.ParseFloat(s, 64)
	return amount
}

// filterInternalTransfers removes internal transfers to avoid double-counting.
// Two sources are consulted:
//
//  1. The hardcoded classifier.InternalTransferPatterns list (covers
//     common bank/broker descriptions out of the box).
//  2. User-declared major expenses flagged with IsInternalTransfer=true,
//     matched via majorexpenses.MatchTransaction (same keyword/amount
//     rules as the Major Expenses page). Lets every user filter their
//     own broker without code changes.
//
// Major-expense load failure is non-fatal — we log and proceed with just
// the hardcoded patterns so a corrupt major_expenses.json doesn't break
// CSV ingestion entirely.
func (dl *DataLoader) filterInternalTransfers(transactions []models.Transaction) []models.Transaction {
	initialCount := len(transactions)

	var transferDefs []models.MajorExpense
	if defs, err := dl.LoadMajorExpenses(); err != nil {
		log.Printf("Warning: could not load major expenses for transfer filtering: %v", err)
	} else {
		for _, d := range defs {
			if d.IsInternalTransfer {
				transferDefs = append(transferDefs, d)
			}
		}
	}

	var filtered []models.Transaction
	for _, t := range transactions {
		if classifier.IsInternalTransfer(&t) {
			continue
		}
		if len(transferDefs) > 0 {
			if _, ok := majorexpenses.MatchTransaction(t, transferDefs); ok {
				continue
			}
		}
		filtered = append(filtered, t)
	}

	count := initialCount - len(filtered)
	dl.stateMu.Lock()
	dl.filteredTransferCount = count
	dl.stateMu.Unlock()
	if count > 0 {
		log.Printf("Filtered %d internal transfers", count)
	}

	return filtered
}

// deduplicateTransactions removes duplicate transactions based on hash
func (dl *DataLoader) deduplicateTransactions(transactions []models.Transaction) []models.Transaction {
	seen := make(map[string]bool)
	var unique []models.Transaction

	for _, t := range transactions {
		if !seen[t.Hash] {
			seen[t.Hash] = true
			unique = append(unique, t)
		}
	}

	duplicatesRemoved := len(transactions) - len(unique)
	if duplicatesRemoved > 0 {
		log.Printf("Removed %d duplicate transactions", duplicatesRemoved)
	}

	return unique
}

// CountCSVFiles returns how many CSV files GetFileInfo would report, without
// the per-file scanCSVMetadata parse that dominates its cost. The inclusion
// rule is deliberately identical -- glob *.csv, drop anything that fails to
// stat -- so a caller that only needs the count never has to parse the whole
// ledger a second time to get it.
func (dl *DataLoader) CountCSVFiles() (int, error) {
	files, err := filepath.Glob(filepath.Join(dl.CSVDirectory, "*.csv"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

// GetFileInfo returns information about available CSV files
func (dl *DataLoader) GetFileInfo() ([]models.FileInfo, error) {
	pattern := filepath.Join(dl.CSVDirectory, "*.csv")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var infos []models.FileInfo

	enabled := dl.enabledFilesSnapshot()

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		filename := filepath.Base(file)

		// Quick scan to get transaction count and date range
		transCount, minD, maxD, err := dl.scanCSVMetadata(file)
		minDate := ""
		maxDate := ""

		if err == nil {
			if !minD.IsZero() {
				minDate = minD.Format("2006-01-02")
			}
			if !maxD.IsZero() {
				maxDate = maxD.Format("2006-01-02")
			}
		}

		fileEnabled := true
		if len(enabled) > 0 {
			fileEnabled = enabled[filename]
		}

		infos = append(infos, models.FileInfo{
			Name:         filename,
			Path:         file,
			Size:         info.Size(),
			Enabled:      fileEnabled,
			Transactions: transCount,
			MinDate:      minDate,
			MaxDate:      maxDate,
		})
	}

	return infos, nil
}

// scanCSVMetadata performs a fast scan of a CSV file to estimate metadata
func (dl *DataLoader) scanCSVMetadata(filePath string) (int, time.Time, time.Time, error) {
	// Read decrypted content via storage
	data, err := dl.store.ReadFile(filePath)
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	// Parse header
	header, err := reader.Read()
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}

	dateIdx := -1
	for i, col := range header {
		if normalizeColumnName(col) == "Date" {
			dateIdx = i
			break
		}
	}

	var minDate, maxDate time.Time
	transCount := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		transCount++

		if dateIdx >= 0 && dateIdx < len(record) {
			date := parseDate(record[dateIdx])
			if !date.IsZero() {
				if minDate.IsZero() || date.Before(minDate) {
					minDate = date
				}
				if maxDate.IsZero() || date.After(maxDate) {
					maxDate = date
				}
			}
		}
	}

	return transCount, minDate, maxDate, nil
}

// aliasPath returns the path to the aliases file
func (dl *DataLoader) aliasPath() string {
	return filepath.Join(dl.CSVDirectory, aliasFile)
}

// LoadAliases reads the hash->displayName mapping from disk
func (dl *DataLoader) LoadAliases() (map[string]string, error) {
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	return dl.loadAliasesLocked()
}

// loadAliasesLocked is LoadAliases' body. Caller holds writeMu.
func (dl *DataLoader) loadAliasesLocked() (map[string]string, error) {
	path := dl.aliasPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	aliases := make(map[string]string)
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("invalid aliases file: %w", err)
	}
	return aliases, nil
}

// SaveAlias sets or removes an alias for a transaction hash
func (dl *DataLoader) SaveAlias(hash, displayName string) error {
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	aliases, err := dl.loadAliasesLocked()
	if err != nil {
		return fmt.Errorf("load aliases: %w", err)
	}
	if displayName == "" {
		delete(aliases, hash)
	} else {
		aliases[hash] = displayName
	}
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.aliasPath(), data, 0644)
}

// applyAliases sets DisplayName on transactions that have aliases
func (dl *DataLoader) applyAliases(transactions []models.Transaction) []models.Transaction {
	aliases, err := dl.LoadAliases()
	if err != nil {
		log.Printf("Warning: could not load aliases: %v", err)
		return transactions
	}
	if len(aliases) == 0 {
		return transactions
	}
	for i := range transactions {
		if name, ok := aliases[transactions[i].Hash]; ok {
			transactions[i].DisplayName = name
		}
	}
	return transactions
}

// UnresolvedDuplicateCount returns the number of candidate pairs that
// have not yet been resolved by the user. Recomputed on every LoadData.
func (dl *DataLoader) UnresolvedDuplicateCount() int {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return len(dl.unresolvedDuplicates)
}

// UnresolvedDuplicates returns the candidate pairs awaiting user
// review, in detection order.
func (dl *DataLoader) UnresolvedDuplicates() []DuplicatePair {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make([]DuplicatePair, len(dl.unresolvedDuplicates))
	copy(out, dl.unresolvedDuplicates)
	return out
}

// ResolvedDuplicates returns the kept_winner pairs the user has
// already resolved, sourced from the most recent load. The Left side
// is the kept transaction; Right is the suppressed one.
//
// kept_both pairs are deliberately NOT here -- the Duplicates page reads
// this list and renders Right as the suppressed side, which a kept_both
// pair has none of. See KeptBothDuplicates.
func (dl *DataLoader) ResolvedDuplicates() []DuplicatePair {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make([]DuplicatePair, len(dl.resolvedDuplicates))
	copy(out, dl.resolvedDuplicates)
	return out
}

// KeptBothDuplicates returns the pairs the user settled as kept_both, in
// detection order. Both sides are live and untagged, so the pair affects
// nothing about the ledger -- but the decision is still recorded and is
// still undoable, and without this accessor such a pair appears in no
// list at all and its pair_key becomes unrecoverable.
func (dl *DataLoader) KeptBothDuplicates() []DuplicatePair {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make([]DuplicatePair, len(dl.keptBothDuplicates))
	copy(out, dl.keptBothDuplicates)
	return out
}

// applyDuplicateDetection runs near-duplicate detection, loads any
// saved decisions, and stamps Suppressed / DuplicatePairKey on the
// transactions accordingly. Caches unresolved/resolved pairs on the
// loader for handlers to read.
func (dl *DataLoader) applyDuplicateDetection(txns []models.Transaction) []models.Transaction {
	var unresolved []DuplicatePair
	var resolved []DuplicatePair
	var keptBoth []DuplicatePair

	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) == 0 {
		dl.stateMu.Lock()
		dl.unresolvedDuplicates = unresolved
		dl.resolvedDuplicates = resolved
		dl.keptBothDuplicates = keptBoth
		dl.stateMu.Unlock()
		return txns
	}

	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		log.Printf("Warning: could not load duplicate decisions: %v", err)
		decisions = nil
	}

	// Build hash → index lookup once.
	idxByHash := make(map[string]int, len(txns))
	for i, t := range txns {
		idxByHash[t.Hash] = i
	}

	for _, pair := range pairs {
		decision, isResolved := decisions[pair.Key]
		if !isResolved {
			// Tag both sides for badge rendering.
			if i, ok := idxByHash[pair.Left.Hash]; ok {
				txns[i].DuplicatePairKey = pair.Key
			}
			if i, ok := idxByHash[pair.Right.Hash]; ok {
				txns[i].DuplicatePairKey = pair.Key
			}
			unresolved = append(unresolved, pair)
			continue
		}
		switch decision.Outcome {
		case DuplicateOutcomeKeptWinner:
			if i, ok := idxByHash[decision.SuppressedHash]; ok {
				txns[i].Suppressed = true
			}
			// Keep the user-side roles in the resolved list: Left = kept.
			leftKept := pair.Left
			rightSuppressed := pair.Right
			if pair.Left.Hash == decision.SuppressedHash {
				leftKept, rightSuppressed = pair.Right, pair.Left
			}
			resolved = append(resolved, DuplicatePair{
				Key:   pair.Key,
				Left:  leftKept,
				Right: rightSuppressed,
			})
		case DuplicateOutcomeKeptBoth:
			// Both transactions stay live and untagged -- nothing to stamp.
			// The pair is still recorded so the decision remains visible
			// and undoable rather than vanishing from every list.
			keptBoth = append(keptBoth, pair)
		}
	}

	dl.stateMu.Lock()
	dl.unresolvedDuplicates = unresolved
	dl.resolvedDuplicates = resolved
	dl.keptBothDuplicates = keptBoth
	dl.stateMu.Unlock()
	return txns
}
