package dataloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// helper to create a temp dir with files and return a loader + cleanup func
func setupTestDir(t *testing.T, files map[string]string) (string, *DataLoader, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "dataloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			os.RemoveAll(tmpDir)
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	store, err := storage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create storage: %v", err)
	}
	loader := New(tmpDir, store)
	return tmpDir, loader, func() { os.RemoveAll(tmpDir) }
}

func TestSetEnabledFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	loader.SetEnabledFiles([]string{"a.csv", "b.csv"})
	if !loader.enabledFiles["a.csv"] {
		t.Error("expected a.csv to be enabled")
	}
	if !loader.enabledFiles["b.csv"] {
		t.Error("expected b.csv to be enabled")
	}
	if loader.enabledFiles["c.csv"] {
		t.Error("expected c.csv to not be enabled")
	}

	// SetEnabledFiles replaces previous set
	loader.SetEnabledFiles([]string{"c.csv"})
	if loader.enabledFiles["a.csv"] {
		t.Error("expected a.csv to no longer be enabled")
	}
	if !loader.enabledFiles["c.csv"] {
		t.Error("expected c.csv to be enabled")
	}
}

func TestParseDate_AllFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected string // "2006-01-02" or "" for zero
	}{
		{"2024-01-15", "2024-01-15"},
		{"01/15/2024", "2024-01-15"},
		{"1/2/2024", "2024-01-02"},
		{"01-15-2024", "2024-01-15"},
		{"2024/01/15", "2024-01-15"},
		{"Jan 15, 2024", "2024-01-15"},
		{"January 15, 2024", "2024-01-15"},
		{"15 Jan 2024", "2024-01-15"},
		{"not-a-date", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseDate(tt.input)
			if tt.expected == "" {
				if !result.IsZero() {
					t.Errorf("parseDate(%q) = %v, want zero", tt.input, result)
				}
			} else {
				expected, _ := time.Parse("2006-01-02", tt.expected)
				if !result.Equal(expected) {
					t.Errorf("parseDate(%q) = %v, want %v", tt.input, result, expected)
				}
			}
		})
	}
}

func TestParseAmount_AllFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"100.00", 100.00},
		{"-50.00", -50.00},
		{"$100.00", 100.00},
		{"$1,234.56", 1234.56},
		{"(100.00)", -100.00},
		{"($100.00)", -100.00},
		{"  50.00  ", 50.00},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseAmount(tt.input)
			if result != tt.expected {
				t.Errorf("parseAmount(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFilterInternalTransfers(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	transactions := []models.Transaction{
		{Description: "Grocery Store", Amount: -50.00},
		{Description: "transfer to savings", Amount: -200.00},
		{Description: "Paycheck", Amount: 3000.00},
	}

	result := loader.filterInternalTransfers(transactions)
	if len(result) > len(transactions) {
		t.Error("filtered result should not be longer than input")
	}
	// Verify FilteredTransferCount is set correctly
	if loader.FilteredTransferCount != len(transactions)-len(result) {
		t.Errorf("FilteredTransferCount = %d, expected %d", loader.FilteredTransferCount, len(transactions)-len(result))
	}
}

func TestFilterInternalTransfers_NoTransfers(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	transactions := []models.Transaction{
		{Description: "Grocery Store", Amount: -50.00},
	}

	result := loader.filterInternalTransfers(transactions)
	if len(result) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result))
	}
	if loader.FilteredTransferCount != 0 {
		t.Errorf("expected 0 filtered, got %d", loader.FilteredTransferCount)
	}
}

func TestDeduplicateTransactions(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "abc123", Description: "Grocery"},
		{Hash: "abc123", Description: "Grocery"},
		{Hash: "def456", Description: "Gas"},
	}

	result := loader.deduplicateTransactions(transactions)
	if len(result) != 2 {
		t.Errorf("expected 2 unique transactions, got %d", len(result))
	}
}

func TestDeduplicateTransactions_NoDuplicates(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "aaa", Description: "A"},
		{Hash: "bbb", Description: "B"},
	}

	result := loader.deduplicateTransactions(transactions)
	if len(result) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(result))
	}
}

func TestLoadData_NoCsvFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadData_WithCsvFiles(t *testing.T) {
	csv1 := "Date,Description,Amount,Category\n2024-01-15,Grocery Store,-50.00,Groceries\n2024-01-16,Paycheck,3000.00,Income"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"bank1.csv": csv1,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadData_WithEnabledFiles(t *testing.T) {
	csv1 := "Date,Description,Amount\n2024-01-15,Grocery,-50.00"
	csv2 := "Date,Description,Amount\n2024-01-16,Gas,-30.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"enabled.csv":  csv1,
		"disabled.csv": csv2,
	})
	defer cleanup()

	loader.SetEnabledFiles([]string{"enabled.csv"})
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadData_BadCsvFileSkipped(t *testing.T) {
	badCSV := "Foo,Bar\nabc,def"
	goodCSV := "Date,Description,Amount\n2024-01-15,Grocery,-50.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"bad.csv":  badCSV,
		"good.csv": goodCSV,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadData_AllBadFiles(t *testing.T) {
	badCSV := "Foo,Bar\nabc,def"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"bad.csv": badCSV,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet for empty data")
	}
}

func TestLoadData_WithDuplicates(t *testing.T) {
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"file1.csv": csv,
		"file2.csv": csv,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadData_WithAliases(t *testing.T) {
	csv := "Date,Description,Amount\n2024-01-15,UGLY_NAME,-50.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	// First load to get the hash
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestLoadCSVFile_EmptyFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"empty.csv": "",
	})
	defer cleanup()

	_, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "empty.csv"))
	if err == nil {
		t.Error("expected error for empty CSV file")
	}
}

func TestLoadCSVFile_UnparsableDate(t *testing.T) {
	csv := "Date,Description,Amount\nnot-a-date,Grocery,-50.00\n2024-01-15,Gas,-30.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	txns, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txns) != 1 {
		t.Errorf("expected 1 transaction (bad date skipped), got %d", len(txns))
	}
}

func TestLoadCSVFile_NonexistentFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	_, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "nonexistent.csv"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadCSVFile_WithCategory(t *testing.T) {
	csv := "Date,Description,Amount,Category\n2024-01-15,Grocery,-50.00,Food"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	txns, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txns) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txns))
	}
	if txns[0].Category != "Food" {
		t.Errorf("expected category 'Food', got %q", txns[0].Category)
	}
	if txns[0].SourceFile != "test.csv" {
		t.Errorf("expected source file 'test.csv', got %q", txns[0].SourceFile)
	}
}

func TestLoadCSVFile_DebitCreditFormat(t *testing.T) {
	csv := "Posted Date,Details,Debit,Credit\n2024-01-15,Grocery Store,50.00,\n2024-01-16,Paycheck,,3000.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	txns, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}
	if txns[0].Amount != -50.00 {
		t.Errorf("expected debit -50.00, got %v", txns[0].Amount)
	}
	if txns[1].Amount != 3000.00 {
		t.Errorf("expected credit 3000.00, got %v", txns[1].Amount)
	}
}

func TestGetFileInfo_NoFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	infos, err := loader.GetFileInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected 0 file infos, got %d", len(infos))
	}
}

func TestGetFileInfo_WithFiles(t *testing.T) {
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00\n2024-03-20,Gas,-30.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	infos, err := loader.GetFileInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 file info, got %d", len(infos))
	}
	info := infos[0]
	if info.Name != "test.csv" {
		t.Errorf("expected name 'test.csv', got %q", info.Name)
	}
	if info.Transactions != 2 {
		t.Errorf("expected 2 transactions, got %d", info.Transactions)
	}
	if info.MinDate != "2024-01-15" {
		t.Errorf("expected min date '2024-01-15', got %q", info.MinDate)
	}
	if info.MaxDate != "2024-03-20" {
		t.Errorf("expected max date '2024-03-20', got %q", info.MaxDate)
	}
	if !info.Enabled {
		t.Error("expected file to be enabled by default")
	}
	if info.Size == 0 {
		t.Error("expected non-zero file size")
	}
}

func TestGetFileInfo_WithEnabledFiles(t *testing.T) {
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"a.csv": csv,
		"b.csv": csv,
	})
	defer cleanup()

	loader.SetEnabledFiles([]string{"a.csv"})
	infos, err := loader.GetFileInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, info := range infos {
		if info.Name == "a.csv" && !info.Enabled {
			t.Error("expected a.csv to be enabled")
		}
		if info.Name == "b.csv" && info.Enabled {
			t.Error("expected b.csv to be disabled")
		}
	}
}

func TestGetFileInfo_ScanError(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"empty.csv": "",
	})
	defer cleanup()

	infos, err := loader.GetFileInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}
	if infos[0].MinDate != "" {
		t.Errorf("expected empty MinDate for scan error, got %q", infos[0].MinDate)
	}
}

func TestScanCSVMetadata(t *testing.T) {
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00\n2024-03-20,Gas,-30.00\nbad-date,Unknown,-10.00"

	tmpDir, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	count, minDate, maxDate, err := loader.scanCSVMetadata(filepath.Join(tmpDir, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
	expectedMin, _ := time.Parse("2006-01-02", "2024-01-15")
	expectedMax, _ := time.Parse("2006-01-02", "2024-03-20")
	if !minDate.Equal(expectedMin) {
		t.Errorf("expected min date %v, got %v", expectedMin, minDate)
	}
	if !maxDate.Equal(expectedMax) {
		t.Errorf("expected max date %v, got %v", expectedMax, maxDate)
	}
}

func TestScanCSVMetadata_NoDateColumn(t *testing.T) {
	csv := "Description,Amount\nGrocery,-50.00"

	tmpDir, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	count, minDate, maxDate, err := loader.scanCSVMetadata(filepath.Join(tmpDir, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
	if !minDate.IsZero() {
		t.Error("expected zero min date when no date column")
	}
	if !maxDate.IsZero() {
		t.Error("expected zero max date when no date column")
	}
}

func TestScanCSVMetadata_NonexistentFile(t *testing.T) {
	tmpDir, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	_, _, _, err := loader.scanCSVMetadata(filepath.Join(tmpDir, "nonexistent.csv"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestScanCSVMetadata_EmptyFile(t *testing.T) {
	tmpDir, loader, cleanup := setupTestDir(t, map[string]string{
		"empty.csv": "",
	})
	defer cleanup()

	_, _, _, err := loader.scanCSVMetadata(filepath.Join(tmpDir, "empty.csv"))
	if err == nil {
		t.Error("expected error for empty CSV")
	}
}

func TestAliasPath(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	expected := filepath.Join(loader.CSVDirectory, "aliases.json")
	if loader.aliasPath() != expected {
		t.Errorf("aliasPath() = %q, want %q", loader.aliasPath(), expected)
	}
}

func TestLoadAliases_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	aliases, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected empty aliases, got %d", len(aliases))
	}
}

func TestLoadAliases_ValidFile(t *testing.T) {
	aliasData := map[string]string{"hash1": "Grocery Store", "hash2": "Gas Station"}
	data, _ := json.Marshal(aliasData)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": string(data),
	})
	defer cleanup()

	aliases, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aliases["hash1"] != "Grocery Store" {
		t.Errorf("expected 'Grocery Store', got %q", aliases["hash1"])
	}
	if aliases["hash2"] != "Gas Station" {
		t.Errorf("expected 'Gas Station', got %q", aliases["hash2"])
	}
}

func TestLoadAliases_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": "not valid json{{{",
	})
	defer cleanup()

	_, err := loader.LoadAliases()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveAlias_NewAlias(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	err := loader.SaveAlias("hash1", "My Store")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aliases, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}
	if aliases["hash1"] != "My Store" {
		t.Errorf("expected 'My Store', got %q", aliases["hash1"])
	}
}

func TestSaveAlias_UpdateExisting(t *testing.T) {
	aliasData := map[string]string{"hash1": "Old Name"}
	data, _ := json.Marshal(aliasData)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": string(data),
	})
	defer cleanup()

	err := loader.SaveAlias("hash1", "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aliases, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aliases["hash1"] != "New Name" {
		t.Errorf("expected 'New Name', got %q", aliases["hash1"])
	}
}

func TestSaveAlias_RemoveAlias(t *testing.T) {
	aliasData := map[string]string{"hash1": "Name"}
	data, _ := json.Marshal(aliasData)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": string(data),
	})
	defer cleanup()

	err := loader.SaveAlias("hash1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aliases, err := loader.LoadAliases()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := aliases["hash1"]; exists {
		t.Error("expected hash1 to be removed")
	}
}

func TestApplyAliases_WithAliases(t *testing.T) {
	aliasData := map[string]string{"hash1": "Friendly Name"}
	data, _ := json.Marshal(aliasData)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": string(data),
	})
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "hash1", Description: "UGLY_NAME_123"},
		{Hash: "hash2", Description: "No Alias"},
	}

	result := loader.applyAliases(transactions)
	if result[0].DisplayName != "Friendly Name" {
		t.Errorf("expected DisplayName 'Friendly Name', got %q", result[0].DisplayName)
	}
	if result[1].DisplayName != "" {
		t.Errorf("expected empty DisplayName, got %q", result[1].DisplayName)
	}
}

func TestApplyAliases_NoAliasFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "hash1", Description: "Grocery"},
	}

	result := loader.applyAliases(transactions)
	if len(result) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result))
	}
	if result[0].DisplayName != "" {
		t.Error("expected empty DisplayName when no aliases file")
	}
}

func TestApplyAliases_InvalidAliasFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": "broken json{{{",
	})
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "hash1", Description: "Grocery"},
	}

	// Should return original transactions without error
	result := loader.applyAliases(transactions)
	if len(result) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result))
	}
}

func TestApplyAliases_EmptyAliases(t *testing.T) {
	aliasData := map[string]string{}
	data, _ := json.Marshal(aliasData)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": string(data),
	})
	defer cleanup()

	transactions := []models.Transaction{
		{Hash: "hash1", Description: "Grocery"},
	}

	result := loader.applyAliases(transactions)
	if len(result) != 1 {
		t.Errorf("expected 1 transaction, got %d", len(result))
	}
}

func TestNormalizeColumnName_TrimSpace(t *testing.T) {
	result := normalizeColumnName("  Date  ")
	if result != "Date" {
		t.Errorf("normalizeColumnName with spaces = %q, want 'Date'", result)
	}
}

func TestLoadData_InvalidDirectory(t *testing.T) {
	store, _ := storage.New("/tmp")
	loader := New("/nonexistent/path/that/does/not/exist", store)

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == nil {
		t.Fatal("expected non-nil TransactionSet")
	}
}

func TestParseDebitCredit_CreditOnly(t *testing.T) {
	record := []string{"2024-01-01", "Deposit", "100.00"}
	colIndex := map[string]int{"Date": 0, "Description": 1, "Credit": 2}
	result := parseDebitCredit(record, colIndex)
	if result != 100.00 {
		t.Errorf("expected 100.00, got %v", result)
	}
}

func TestParseDebitCredit_DebitOnlyNoCredit(t *testing.T) {
	record := []string{"2024-01-01", "Purchase", "50.00"}
	colIndex := map[string]int{"Date": 0, "Description": 1, "Debit": 2}
	result := parseDebitCredit(record, colIndex)
	if result != -50.00 {
		t.Errorf("expected -50.00, got %v", result)
	}
}

func TestParseDebitCredit_IndexOutOfBounds(t *testing.T) {
	record := []string{"2024-01-01"}
	colIndex := map[string]int{"Date": 0, "Credit": 5, "Debit": 6}
	result := parseDebitCredit(record, colIndex)
	if result != 0 {
		t.Errorf("expected 0 for out-of-bounds, got %v", result)
	}
}

func TestFilterInternalTransfers_UserFlaggedMajorExpense(t *testing.T) {
	// A user-declared major expense flagged as IsInternalTransfer should
	// drop matching transactions just like the hardcoded patterns do.
	store := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{
			ID:                 "tx-1",
			Name:               "Brokerage funding",
			Keywords:           []string{"my custom broker"},
			IsInternalTransfer: true,
		},
	}}
	data, _ := json.Marshal(store)
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(data),
	})
	defer cleanup()

	transactions := []models.Transaction{
		{Description: "Grocery Store", Amount: -50.00},
		{Description: "MY CUSTOM BROKER ACH", Amount: -1000.00}, // should be dropped
		{Description: "Paycheck", Amount: 3000.00},
	}
	result := loader.filterInternalTransfers(transactions)
	if len(result) != 2 {
		t.Fatalf("expected 2 surviving txns, got %d: %+v", len(result), result)
	}
	for _, txn := range result {
		if txn.Description == "MY CUSTOM BROKER ACH" {
			t.Error("flagged-major-expense transaction should have been filtered out")
		}
	}
	if loader.FilteredTransferCount != 1 {
		t.Errorf("FilteredTransferCount = %d, want 1", loader.FilteredTransferCount)
	}
}

func TestFilterInternalTransfers_NonFlaggedMajorExpenseDoesNotFilter(t *testing.T) {
	// A regular major expense (IsInternalTransfer=false) must NOT cause
	// matching transactions to be dropped — that would silently remove
	// real spending. Bug guard: this is the contract that distinguishes
	// the new flag from the existing major-expense matching system.
	store := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{
			ID:       "tx-2",
			Name:     "Groceries",
			Keywords: []string{"wegmans"},
			// IsInternalTransfer intentionally not set
		},
	}}
	data, _ := json.Marshal(store)
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(data),
	})
	defer cleanup()

	transactions := []models.Transaction{
		{Description: "WEGMANS GROCERY", Amount: -75.00},
	}
	result := loader.filterInternalTransfers(transactions)
	if len(result) != 1 {
		t.Errorf("regular major-expense match must not be filtered; got %d surviving txns", len(result))
	}
	if loader.FilteredTransferCount != 0 {
		t.Errorf("FilteredTransferCount = %d, want 0", loader.FilteredTransferCount)
	}
}

func TestFilterInternalTransfers_WithTransfers(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	// Use a known internal transfer pattern from the classifier
	transactions := []models.Transaction{
		{Description: "Grocery Store", Amount: -50.00},
		{Description: "USAA funds transfer credit", Amount: -200.00},
		{Description: "Internal Transfer to Savings", Amount: -500.00},
		{Description: "Paycheck", Amount: 3000.00},
	}

	result := loader.filterInternalTransfers(transactions)
	if loader.FilteredTransferCount == 0 {
		t.Error("expected some transfers to be filtered")
	}
	if len(result)+loader.FilteredTransferCount != len(transactions) {
		t.Errorf("filtered count mismatch: %d result + %d filtered != %d total",
			len(result), loader.FilteredTransferCount, len(transactions))
	}
}

func TestLoadCSVFile_MalformedRow(t *testing.T) {
	// Create a CSV with a row that has a bare quote to trigger a parse error
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00\n\"unclosed quote\n2024-01-16,Gas,-30.00"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	txns, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// At least the first valid row should be parsed
	if len(txns) < 1 {
		t.Error("expected at least 1 transaction from valid rows")
	}
}

func TestScanCSVMetadata_MalformedRow(t *testing.T) {
	// CSV with a malformed row that causes a read error mid-file
	csv := "Date,Description,Amount\n2024-01-15,Grocery,-50.00\n\"unclosed\n2024-01-16,Gas,-30.00"

	tmpDir, loader, cleanup := setupTestDir(t, map[string]string{
		"test.csv": csv,
	})
	defer cleanup()

	count, _, _, err := loader.scanCSVMetadata(filepath.Join(tmpDir, "test.csv"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have parsed at least the valid rows
	if count < 1 {
		t.Error("expected at least 1 row counted")
	}
}

func TestSaveAlias_LoadError(t *testing.T) {
	// Create a loader pointing to a directory with an invalid aliases file
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"aliases.json": "invalid json{{{",
	})
	defer cleanup()

	err := loader.SaveAlias("hash1", "Name")
	if err == nil {
		t.Error("expected error when LoadAliases fails")
	}
}

func TestLoadAliases_ReadError(t *testing.T) {
	// Create a directory (not a file) at the aliases.json path to trigger
	// a non-NotExist error
	tmpDir, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	// Create a directory where the file should be - this causes a read error
	// that is not os.IsNotExist
	aliasDir := filepath.Join(tmpDir, "aliases.json")
	if err := os.MkdirAll(aliasDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	_, err := loader.LoadAliases()
	if err == nil {
		t.Error("expected error when aliases.json is a directory")
	}
}

func TestLoadData_GlobError(t *testing.T) {
	// filepath.Glob returns an error only for bad patterns (e.g., with unmatched brackets)
	// We can trigger this by using a directory name with a bad glob character
	tmpDir, err := os.MkdirTemp("", "dataloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a directory with a name that causes Glob to fail
	badDir := filepath.Join(tmpDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("failed to create bad dir: %v", err)
	}

	store, _ := storage.New(badDir)
	loader := New(badDir, store)

	_, err = loader.LoadData()
	if err == nil {
		t.Error("expected error from bad glob pattern")
	}
}

func TestGetFileInfo_StatError(t *testing.T) {
	tmpDir, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	// Create a symlink to a nonexistent target - Glob will find it, Stat will fail
	csvPath := filepath.Join(tmpDir, "broken.csv")
	os.Symlink("/nonexistent/target", csvPath)

	infos, err := loader.GetFileInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The broken symlink should be skipped (stat error -> continue)
	if len(infos) != 0 {
		t.Errorf("expected 0 infos (broken symlink skipped), got %d", len(infos))
	}
}

func TestGetFileInfo_GlobError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dataloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	badDir := filepath.Join(tmpDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("failed to create bad dir: %v", err)
	}

	store, _ := storage.New(badDir)
	loader := New(badDir, store)

	_, err = loader.GetFileInfo()
	if err == nil {
		t.Error("expected error from bad glob pattern")
	}
}
