package models

import (
	"testing"
	"time"
)

func TestComputeHash(t *testing.T) {
	tx := &Transaction{
		Date:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Description: "Coffee Shop",
		Amount:      -4.50,
	}
	hash := tx.ComputeHash()
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(hash) != 16 { // hex encoded 8 bytes
		t.Errorf("expected 16 char hash, got %d", len(hash))
	}

	// Same data produces same hash
	tx2 := &Transaction{
		Date:        time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		Description: "  Coffee Shop  ",
		Amount:      -4.50,
	}
	if tx2.ComputeHash() != hash {
		t.Error("same logical transaction should produce same hash")
	}

	// Different data produces different hash
	tx3 := &Transaction{
		Date:        time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		Description: "Coffee Shop",
		Amount:      -4.50,
	}
	if tx3.ComputeHash() == hash {
		t.Error("different date should produce different hash")
	}
}

func TestComputeDerivedFields(t *testing.T) {
	tx := &Transaction{
		Date: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC), // Friday, March 15 2024
	}
	tx.ComputeDerivedFields()

	if tx.Month != "2024-03" {
		t.Errorf("Month = %q, want 2024-03", tx.Month)
	}
	if tx.Year != 2024 {
		t.Errorf("Year = %d, want 2024", tx.Year)
	}
	if tx.Quarter != 1 {
		t.Errorf("Quarter = %d, want 1", tx.Quarter)
	}
	if tx.DayOfWeek != "Friday" {
		t.Errorf("DayOfWeek = %q, want Friday", tx.DayOfWeek)
	}
	if tx.DayOfMonth != 15 {
		t.Errorf("DayOfMonth = %d, want 15", tx.DayOfMonth)
	}
	if tx.Week == "" {
		t.Error("Week should not be empty")
	}
}

func TestTransaction_Label(t *testing.T) {
	tests := []struct {
		name string
		txn  Transaction
		want string
	}{
		{
			name: "DisplayName wins over both",
			txn:  Transaction{Description: "BANK", MajorExpenseName: "Mortgage", DisplayName: "Mort."},
			want: "Mort.",
		},
		{
			name: "MajorExpenseName beats Description",
			txn:  Transaction{Description: "BANK", MajorExpenseName: "Mortgage"},
			want: "Mortgage",
		},
		{
			name: "Description fallback when nothing else",
			txn:  Transaction{Description: "BANK"},
			want: "BANK",
		},
		{
			name: "all empty returns empty",
			txn:  Transaction{},
			want: "",
		},
		{
			name: "DisplayName wins even when only it is set",
			txn:  Transaction{DisplayName: "Friendly"},
			want: "Friendly",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.txn.Label(); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAbsAmount(t *testing.T) {
	tests := []struct {
		amount float64
		want   float64
	}{
		{-100.50, 100.50},
		{200.00, 200.00},
		{0, 0},
	}
	for _, tt := range tests {
		tx := &Transaction{Amount: tt.amount}
		if got := tx.AbsAmount(); got != tt.want {
			t.Errorf("AbsAmount(%f) = %f, want %f", tt.amount, got, tt.want)
		}
	}
}

func makeTestTransactions() []Transaction {
	return []Transaction{
		{Date: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC), Amount: 5000, Description: "Salary", Category: "Income", TransactionType: Income},
		{Date: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), Amount: -100, Description: "Grocery Store", Category: "Groceries", TransactionType: Outflow},
		{Date: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC), Amount: -50, Description: "Coffee Shop", DisplayName: "Morning Java", Category: "Dining", TransactionType: Outflow},
		{Date: time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), Amount: 5000, Description: "Salary", Category: "Income", TransactionType: Income},
		{Date: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC), Amount: -200, Description: "Electric Bill", Category: "", TransactionType: Outflow},
	}
}

func TestNewTransactionSet(t *testing.T) {
	txs := makeTestTransactions()
	ts := NewTransactionSet(txs)
	if ts.Len() != 5 {
		t.Errorf("Len() = %d, want 5", ts.Len())
	}
}

func TestFilterByType(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	income := ts.FilterByType(Income)
	if income.Len() != 2 {
		t.Errorf("income count = %d, want 2", income.Len())
	}

	outflow := ts.FilterByType(Outflow)
	if outflow.Len() != 3 {
		t.Errorf("outflow count = %d, want 3", outflow.Len())
	}
}

func TestFilterByDateRange(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	jan := ts.FilterByDateRange(start, end)
	if jan.Len() != 3 {
		t.Errorf("January count = %d, want 3", jan.Len())
	}

	// Inclusive boundaries
	exact := ts.FilterByDateRange(
		time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
	)
	if exact.Len() != 1 {
		t.Errorf("exact date count = %d, want 1", exact.Len())
	}
}

func TestFilterByCategory(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	groceries := ts.FilterByCategory("groceries") // case insensitive
	if groceries.Len() != 1 {
		t.Errorf("groceries count = %d, want 1", groceries.Len())
	}

	none := ts.FilterByCategory("nonexistent")
	if none.Len() != 0 {
		t.Errorf("nonexistent count = %d, want 0", none.Len())
	}
}

func TestFilterBySearch(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	// Search by description
	result := ts.FilterBySearch("salary")
	if result.Len() != 2 {
		t.Errorf("salary search = %d, want 2", result.Len())
	}

	// Search by display name
	result = ts.FilterBySearch("morning java")
	if result.Len() != 1 {
		t.Errorf("display name search = %d, want 1", result.Len())
	}

	// No match
	result = ts.FilterBySearch("xyz123")
	if result.Len() != 0 {
		t.Errorf("no match search = %d, want 0", result.Len())
	}
}

func TestTransactionSet_FilterBySearch_MatchesMajorExpenseName(t *testing.T) {
	ts := NewTransactionSet([]Transaction{
		{Description: "BOFA HOMELOANS 0123", MajorExpenseName: "Mortgage"},
		{Description: "Whole Foods", MajorExpenseName: "Groceries"},
		{Description: "Starbucks"},
	})
	got := ts.FilterBySearch("mortgage")
	if got.Len() != 1 {
		t.Fatalf("expected 1 match, got %d", got.Len())
	}
	if got.Transactions[0].Description != "BOFA HOMELOANS 0123" {
		t.Errorf("wrong row matched: %q", got.Transactions[0].Description)
	}
}

func TestSumAmount(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	sum := ts.SumAmount()
	expected := 5000.0 - 100 - 50 + 5000 - 200
	if sum != expected {
		t.Errorf("SumAmount() = %f, want %f", sum, expected)
	}
}

func TestSumAbsAmount(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	sum := ts.SumAbsAmount()
	expected := 5000.0 + 100 + 50 + 5000 + 200
	if sum != expected {
		t.Errorf("SumAbsAmount() = %f, want %f", sum, expected)
	}
}

func TestGroupByMonth(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	groups := ts.GroupByMonth()
	if len(groups) != 2 {
		t.Errorf("month groups = %d, want 2", len(groups))
	}
	if groups["2024-01"].Len() != 3 {
		t.Errorf("Jan count = %d, want 3", groups["2024-01"].Len())
	}
	if groups["2024-02"].Len() != 2 {
		t.Errorf("Feb count = %d, want 2", groups["2024-02"].Len())
	}
}

func TestGroupByCategory(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	groups := ts.GroupByCategory()

	// Empty category becomes "Uncategorized"
	if _, ok := groups["Uncategorized"]; !ok {
		t.Error("expected Uncategorized group for empty category")
	}
	if groups["Income"].Len() != 2 {
		t.Errorf("Income count = %d, want 2", groups["Income"].Len())
	}
}

func TestGroupByDate(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	groups := ts.GroupByDate()
	if len(groups) != 5 {
		t.Errorf("date groups = %d, want 5", len(groups))
	}
}

func TestSortByDate(t *testing.T) {
	txs := []Transaction{
		{Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	ts := NewTransactionSet(txs)
	sorted := ts.SortByDate()

	if sorted.Transactions[0].Date.Month() != 1 {
		t.Error("first should be January")
	}
	if sorted.Transactions[2].Date.Month() != 3 {
		t.Error("last should be March")
	}
	// Original not mutated
	if ts.Transactions[0].Date.Month() != 3 {
		t.Error("original should be unchanged")
	}
}

func TestSortByDateDesc(t *testing.T) {
	txs := []Transaction{
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	ts := NewTransactionSet(txs)
	sorted := ts.SortByDateDesc()

	if sorted.Transactions[0].Date.Month() != 3 {
		t.Error("first should be March")
	}
	if sorted.Transactions[2].Date.Month() != 1 {
		t.Error("last should be January")
	}
}

func TestSortByAmount(t *testing.T) {
	txs := []Transaction{
		{Amount: 100},
		{Amount: -50},
		{Amount: 200},
	}
	ts := NewTransactionSet(txs)
	sorted := ts.SortByAmount()

	if sorted.Transactions[0].Amount != -50 {
		t.Errorf("first = %f, want -50", sorted.Transactions[0].Amount)
	}
	if sorted.Transactions[2].Amount != 200 {
		t.Errorf("last = %f, want 200", sorted.Transactions[2].Amount)
	}
}

func TestMinMaxDate(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	min := ts.MinDate()
	if min != time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC) {
		t.Errorf("MinDate = %v, want 2024-01-10", min)
	}

	max := ts.MaxDate()
	if max != time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC) {
		t.Errorf("MaxDate = %v, want 2024-02-15", max)
	}

	// Empty set
	empty := NewTransactionSet(nil)
	if !empty.MinDate().IsZero() {
		t.Error("MinDate on empty should be zero")
	}
	if !empty.MaxDate().IsZero() {
		t.Error("MaxDate on empty should be zero")
	}

	// Min not first element — triggers the Date.Before branch
	reversed := []Transaction{
		{Date: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	rts := NewTransactionSet(reversed)
	if rts.MinDate() != time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Error("MinDate should find earliest even when not first")
	}
}

func TestCategories(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	cats := ts.Categories()

	// Should include Uncategorized for empty category
	found := false
	for _, c := range cats {
		if c == "Uncategorized" {
			found = true
		}
	}
	if !found {
		t.Error("expected Uncategorized in categories")
	}

	// Should be sorted
	for i := 1; i < len(cats); i++ {
		if cats[i] < cats[i-1] {
			t.Error("categories not sorted")
		}
	}
}

func TestPaginate(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())

	// Normal pagination
	page1 := ts.Paginate(1, 2)
	if page1.Len() != 2 {
		t.Errorf("page1 len = %d, want 2", page1.Len())
	}

	page3 := ts.Paginate(3, 2)
	if page3.Len() != 1 {
		t.Errorf("page3 len = %d, want 1", page3.Len())
	}

	// Beyond range
	page99 := ts.Paginate(99, 2)
	if page99.Len() != 0 {
		t.Errorf("page99 len = %d, want 0", page99.Len())
	}

	// Invalid inputs default
	page0 := ts.Paginate(0, 0)
	if page0.Len() == 0 {
		t.Error("page 0/0 should default to page 1 with 25 per page")
	}
}

func TestTotalPages(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions()) // 5 items

	if tp := ts.TotalPages(2); tp != 3 {
		t.Errorf("TotalPages(2) = %d, want 3", tp)
	}
	if tp := ts.TotalPages(5); tp != 1 {
		t.Errorf("TotalPages(5) = %d, want 1", tp)
	}
	if tp := ts.TotalPages(0); tp != 1 { // defaults to 25
		t.Errorf("TotalPages(0) = %d, want 1", tp)
	}
}

func TestMonthlyTotals(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	totals := ts.MonthlyTotals()

	if len(totals) != 2 {
		t.Errorf("monthly totals count = %d, want 2", len(totals))
	}
	janExpected := 5000.0 - 100.0 - 50.0
	if totals["2024-01"] != janExpected {
		t.Errorf("Jan total = %f, want %f", totals["2024-01"], janExpected)
	}
}

func TestCategoryTotals(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	totals := ts.CategoryTotals()

	if totals["Income"] != 10000 {
		t.Errorf("Income total = %f, want 10000", totals["Income"])
	}
	if totals["Groceries"] != 100 {
		t.Errorf("Groceries total = %f, want 100", totals["Groceries"])
	}
	// Empty category -> Uncategorized
	if totals["Uncategorized"] != 200 {
		t.Errorf("Uncategorized total = %f, want 200", totals["Uncategorized"])
	}
}

func TestCopy(t *testing.T) {
	ts := NewTransactionSet(makeTestTransactions())
	cp := ts.Copy()

	if cp.Len() != ts.Len() {
		t.Error("copy should have same length")
	}

	// Modifying copy should not affect original
	cp.Transactions[0].Amount = 999999
	if ts.Transactions[0].Amount == 999999 {
		t.Error("modifying copy should not affect original")
	}
}
