package dataloader

import (
	"bufio"
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
)

// DataLoader handles loading and preprocessing of financial data from CSV files
type DataLoader struct {
	CSVDirectory          string
	FilteredTransferCount int
	enabledFiles          map[string]bool
}

// New creates a new DataLoader
func New(csvDirectory string) *DataLoader {
	return &DataLoader{
		CSVDirectory: csvDirectory,
		enabledFiles: make(map[string]bool),
	}
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
		return nil, fmt.Errorf("no CSV files found in %s", dl.CSVDirectory)
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
		return nil, fmt.Errorf("no transactions loaded from CSV files")
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
	file, err := os.Open(filePath)
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

	// Build column index map
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	// Validate required columns
	requiredCols := []string{"Date", "Amount", "Description"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, fmt.Errorf("missing required column: %s", col)
		}
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

		// Parse Amount
		if idx, ok := colIndex["Amount"]; ok && idx < len(record) {
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
	file, err := os.Open(filePath)
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}
	defer file.Close()

	info, _ := file.Stat()
	fileSize := info.Size()

	reader := csv.NewReader(file)
	header, err := reader.Read()
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

	// Count lines approximately (simple newline count)
	// Reset to after header
	_, _ = file.Seek(0, 0)
	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	transCount := lineCount - 1
	if transCount < 0 {
		transCount = 0
	}

	// Get first transaction date
	var minDate, maxDate time.Time
	if dateIdx >= 0 {
		_, _ = file.Seek(0, 0)
		r2 := csv.NewReader(file)
		_, _ = r2.Read() // skip header
		if first, err := r2.Read(); err == nil && dateIdx < len(first) {
			minDate = parseDate(strings.TrimSpace(first[dateIdx]))
		}

		// Get last transaction date by reading the end of the file
		if fileSize > 4096 {
			_, _ = file.Seek(-4096, 2)
		} else {
			_, _ = file.Seek(0, 0)
		}

		// Read the last chunk and find the last complete CSV line
		lastChunk := make([]byte, 4096)
		n, _ := file.Read(lastChunk)
		lastLines := strings.Split(string(lastChunk[:n]), "\n")

		// Work backwards from the second to last element (last might be empty or partial)
		for i := len(lastLines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lastLines[i])
			if line == "" {
				continue
			}
			r3 := csv.NewReader(strings.NewReader(line))
			if record, err := r3.Read(); err == nil && dateIdx < len(record) {
				maxDate = parseDate(strings.TrimSpace(record[dateIdx]))
				if !maxDate.IsZero() {
					break
				}
			}
		}
	}

	// Fallback if min/max date logic failed or file is sorted differently
	// If minDate is actually after maxDate, swap them
	if !minDate.IsZero() && !maxDate.IsZero() && minDate.After(maxDate) {
		minDate, maxDate = maxDate, minDate
	}

	return transCount, minDate, maxDate, nil
}
