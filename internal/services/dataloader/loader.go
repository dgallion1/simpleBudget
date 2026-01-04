package dataloader

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/classifier"
	"budget2/internal/services/storage"
)

// DataLoader handles loading and preprocessing of financial data from CSV files
type DataLoader struct {
	CSVDirectory          string
	FilteredTransferCount int
	enabledFiles          map[string]bool
	store                 *storage.Storage
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
	dl.enabledFiles = make(map[string]bool)
	for _, f := range files {
		dl.enabledFiles[f] = true
	}
}

// LoadData loads and combines data from all CSV files in the directory
func (dl *DataLoader) LoadData() (*models.TransactionSet, error) {
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

	for _, file := range files {
		filename := filepath.Base(file)

		// Skip if file list is set and this file is not enabled
		if len(dl.enabledFiles) > 0 && !dl.enabledFiles[filename] {
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
	defer file.Close()

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

		t.Hash = t.ComputeHash()
		transactions = append(transactions, t)
	}

	return transactions, nil
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

// filterInternalTransfers removes internal transfers to avoid double-counting
func (dl *DataLoader) filterInternalTransfers(transactions []models.Transaction) []models.Transaction {
	initialCount := len(transactions)
	var filtered []models.Transaction

	for _, t := range transactions {
		if !classifier.IsInternalTransfer(&t) {
			filtered = append(filtered, t)
		}
	}

	dl.FilteredTransferCount = initialCount - len(filtered)
	if dl.FilteredTransferCount > 0 {
		log.Printf("Filtered %d internal transfers", dl.FilteredTransferCount)
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

// GetFileInfo returns information about available CSV files
func (dl *DataLoader) GetFileInfo() ([]models.FileInfo, error) {
	pattern := filepath.Join(dl.CSVDirectory, "*.csv")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var infos []models.FileInfo

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

		enabled := true
		if len(dl.enabledFiles) > 0 {
			enabled = dl.enabledFiles[filename]
		}

		infos = append(infos, models.FileInfo{
			Name:         filename,
			Path:         file,
			Size:         info.Size(),
			Enabled:      enabled,
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

	content := string(data)
	lines := strings.Split(content, "\n")

	if len(lines) == 0 {
		return 0, time.Time{}, time.Time{}, nil
	}

	// Parse header
	headerReader := csv.NewReader(strings.NewReader(lines[0]))
	header, err := headerReader.Read()
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}

	dateIdx := -1
	for i, col := range header {
		if strings.TrimSpace(col) == "Date" {
			dateIdx = i
			break
		}
	}

	// Count transactions (lines - 1 for header)
	transCount := 0
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			transCount++
		}
	}

	// Get first and last transaction dates
	var minDate, maxDate time.Time
	if dateIdx >= 0 && len(lines) > 1 {
		// First transaction date
		for _, line := range lines[1:] {
			if strings.TrimSpace(line) == "" {
				continue
			}
			r := csv.NewReader(strings.NewReader(line))
			if record, err := r.Read(); err == nil && dateIdx < len(record) {
				minDate = parseDate(strings.TrimSpace(record[dateIdx]))
				if !minDate.IsZero() {
					break
				}
			}
		}

		// Last transaction date (work backwards)
		for i := len(lines) - 1; i > 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			r := csv.NewReader(strings.NewReader(line))
			if record, err := r.Read(); err == nil && dateIdx < len(record) {
				maxDate = parseDate(strings.TrimSpace(record[dateIdx]))
				if !maxDate.IsZero() {
					break
				}
			}
		}
	}

	// Swap if dates are reversed
	if !minDate.IsZero() && !maxDate.IsZero() && minDate.After(maxDate) {
		minDate, maxDate = maxDate, minDate
	}

	return transCount, minDate, maxDate, nil
}
