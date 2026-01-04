package dataloader

import (
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/services/storage"
)

func TestNormalizeColumnName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Date variations
		{"Date", "Date"},
		{"date", "Date"},
		{"DATE", "Date"},
		{"Transaction Date", "Date"},
		{"transaction date", "Date"},
		{"Posted Date", "Date"},
		{"Posting Date", "Date"},

		// Description variations
		{"Description", "Description"},
		{"description", "Description"},
		{"Memo", "Description"},
		{"memo", "Description"},
		{"Details", "Description"},
		{"Payee", "Description"},
		{"Merchant", "Description"},
		{"Narrative", "Description"},

		// Amount variations
		{"Amount", "Amount"},
		{"amount", "Amount"},
		{"Value", "Amount"},
		{"Transaction Amount", "Amount"},
		{"Sum", "Amount"},

		// Category variations
		{"Category", "Category"},
		{"category", "Category"},
		{"Type", "Category"},

		// Debit variations
		{"Debit", "Debit"},
		{"debit", "Debit"},
		{"Withdrawal", "Debit"},
		{"Money Out", "Debit"},

		// Credit variations
		{"Credit", "Credit"},
		{"credit", "Credit"},
		{"Deposit", "Credit"},
		{"Money In", "Credit"},

		// Unknown columns should pass through unchanged
		{"Unknown Column", "Unknown Column"},
		{"Balance", "Balance"},
		{"Account", "Account"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeColumnName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeColumnName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildColumnIndex(t *testing.T) {
	tests := []struct {
		name     string
		header   []string
		expected map[string]int
	}{
		{
			name:   "standard columns",
			header: []string{"Date", "Description", "Amount", "Category"},
			expected: map[string]int{
				"Date":        0,
				"Description": 1,
				"Amount":      2,
				"Category":    3,
			},
		},
		{
			name:   "bank format with different names",
			header: []string{"Transaction Date", "Memo", "Value"},
			expected: map[string]int{
				"Date":        0,
				"Description": 1,
				"Amount":      2,
			},
		},
		{
			name:   "debit credit format",
			header: []string{"Posted Date", "Details", "Debit", "Credit"},
			expected: map[string]int{
				"Date":        0,
				"Description": 1,
				"Debit":       2,
				"Credit":      3,
			},
		},
		{
			name:   "mixed case",
			header: []string{"date", "description", "amount"},
			expected: map[string]int{
				"Date":        0,
				"Description": 1,
				"Amount":      2,
			},
		},
		{
			name:   "first match wins",
			header: []string{"Date", "Transaction Date", "Amount"},
			expected: map[string]int{
				"Date":   0,
				"Amount": 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildColumnIndex(tt.header)
			for key, expectedIdx := range tt.expected {
				if idx, ok := result[key]; !ok {
					t.Errorf("expected key %q not found in result", key)
				} else if idx != expectedIdx {
					t.Errorf("result[%q] = %d, want %d", key, idx, expectedIdx)
				}
			}
		})
	}
}

func TestParseDebitCredit(t *testing.T) {
	tests := []struct {
		name     string
		record   []string
		colIndex map[string]int
		expected float64
	}{
		{
			name:     "credit only",
			record:   []string{"2024-01-01", "Deposit", "", "100.00"},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: 100.00,
		},
		{
			name:     "debit only",
			record:   []string{"2024-01-01", "Purchase", "50.00", ""},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: -50.00,
		},
		{
			name:     "debit with currency symbol",
			record:   []string{"2024-01-01", "Purchase", "$75.50", ""},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: -75.50,
		},
		{
			name:     "credit with currency symbol",
			record:   []string{"2024-01-01", "Refund", "", "$25.00"},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: 25.00,
		},
		{
			name:     "both empty",
			record:   []string{"2024-01-01", "Unknown", "", ""},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: 0.00,
		},
		{
			name:     "debit already negative",
			record:   []string{"2024-01-01", "Purchase", "-50.00", ""},
			colIndex: map[string]int{"Date": 0, "Description": 1, "Debit": 2, "Credit": 3},
			expected: -50.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDebitCredit(tt.record, tt.colIndex)
			if result != tt.expected {
				t.Errorf("parseDebitCredit() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLoadCSVWithFlexibleColumns(t *testing.T) {
	// Create temp directory for test files
	tmpDir, err := os.MkdirTemp("", "dataloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name            string
		csvContent      string
		expectedCount   int
		expectedAmount  float64 // amount of first transaction
		expectError     bool
		errorContains   string
	}{
		{
			name: "standard format",
			csvContent: `Date,Description,Amount,Category
2024-01-15,Grocery Store,-50.00,Groceries
2024-01-16,Paycheck,3000.00,Income`,
			expectedCount:  2,
			expectedAmount: -50.00,
		},
		{
			name: "bank format with Transaction Date and Memo",
			csvContent: `Transaction Date,Memo,Value
2024-01-15,Grocery Store,-50.00
2024-01-16,Paycheck,3000.00`,
			expectedCount:  2,
			expectedAmount: -50.00,
		},
		{
			name: "debit credit format",
			csvContent: `Posted Date,Details,Debit,Credit
2024-01-15,Grocery Store,50.00,
2024-01-16,Paycheck,,3000.00`,
			expectedCount:  2,
			expectedAmount: -50.00,
		},
		{
			name: "lowercase headers",
			csvContent: `date,description,amount
2024-01-15,Grocery Store,-50.00`,
			expectedCount:  1,
			expectedAmount: -50.00,
		},
		{
			name: "missing date column",
			csvContent: `Description,Amount
Grocery Store,-50.00`,
			expectError:   true,
			errorContains: "Date",
		},
		{
			name: "missing description column",
			csvContent: `Date,Amount
2024-01-15,-50.00`,
			expectError:   true,
			errorContains: "Description",
		},
		{
			name: "missing amount and debit/credit",
			csvContent: `Date,Description
2024-01-15,Grocery Store`,
			expectError:   true,
			errorContains: "Amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test CSV file
			csvPath := filepath.Join(tmpDir, "test.csv")
			if err := os.WriteFile(csvPath, []byte(tt.csvContent), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			store, _ := storage.New(tmpDir)
			loader := New(tmpDir, store)
			transactions, err := loader.loadCSVFile(csvPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorContains)
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(transactions) != tt.expectedCount {
				t.Errorf("got %d transactions, want %d", len(transactions), tt.expectedCount)
			}

			if len(transactions) > 0 && transactions[0].Amount != tt.expectedAmount {
				t.Errorf("first transaction amount = %v, want %v", transactions[0].Amount, tt.expectedAmount)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
