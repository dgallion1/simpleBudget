package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// TransactionType indicates whether a transaction is income or an outflow
type TransactionType string

const (
	Income  TransactionType = "Income"
	Outflow TransactionType = "Outflow"
)

// Transaction represents a single financial transaction
type Transaction struct {
	ID              string          `json:"id"`
	Date            time.Time       `json:"date"`
	Amount          float64         `json:"amount"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	TransactionType TransactionType `json:"transaction_type"`
	SourceFile      string          `json:"source_file"`
	Hash            string          `json:"hash"`

	// Derived fields (computed, not stored)
	Month      string `json:"month,omitempty"`       // "2024-01"
	Week       string `json:"week,omitempty"`        // "2024-W05"
	Year       int    `json:"year,omitempty"`
	Quarter    int    `json:"quarter,omitempty"`
	DayOfWeek  string `json:"day_of_week,omitempty"`
	DayOfMonth int    `json:"day_of_month,omitempty"`
}

// ComputeHash generates a unique hash for duplicate detection
func (t *Transaction) ComputeHash() string {
	dateStr := t.Date.Format("2006-01-02")
	desc := strings.ToLower(strings.TrimSpace(t.Description))
	amount := fmt.Sprintf("%.2f", t.Amount)

	input := fmt.Sprintf("%s|%s|%s", dateStr, desc, amount)
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:8])
}

// ComputeDerivedFields populates computed fields from Date
func (t *Transaction) ComputeDerivedFields() {
	t.Month = t.Date.Format("2006-01")
	year, week := t.Date.ISOWeek()
	t.Week = fmt.Sprintf("%d-W%02d", year, week)
	t.Year = t.Date.Year()
	t.Quarter = (int(t.Date.Month())-1)/3 + 1
	t.DayOfWeek = t.Date.Weekday().String()
	t.DayOfMonth = t.Date.Day()
}

// AbsAmount returns the absolute value of the amount
func (t *Transaction) AbsAmount() float64 {
	return math.Abs(t.Amount)
}

// TransactionSet wraps a slice with filtering/aggregation methods
type TransactionSet struct {
	Transactions []Transaction
}

// NewTransactionSet creates a new TransactionSet from a slice
func NewTransactionSet(transactions []Transaction) *TransactionSet {
	return &TransactionSet{Transactions: transactions}
}

// Len returns the number of transactions
func (ts *TransactionSet) Len() int {
	return len(ts.Transactions)
}

// FilterByType returns transactions of the specified type
func (ts *TransactionSet) FilterByType(tt TransactionType) *TransactionSet {
	result := &TransactionSet{}
	for _, t := range ts.Transactions {
		if t.TransactionType == tt {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}

// FilterByDateRange returns transactions within the date range (inclusive)
func (ts *TransactionSet) FilterByDateRange(start, end time.Time) *TransactionSet {
	result := &TransactionSet{}
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999999999, end.Location())

	for _, t := range ts.Transactions {
		if !t.Date.Before(startDay) && !t.Date.After(endDay) {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}

// FilterByCategory returns transactions matching the category
func (ts *TransactionSet) FilterByCategory(category string) *TransactionSet {
	result := &TransactionSet{}
	catLower := strings.ToLower(category)
	for _, t := range ts.Transactions {
		if strings.ToLower(t.Category) == catLower {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}

// FilterBySearch returns transactions matching the search term in description
func (ts *TransactionSet) FilterBySearch(search string) *TransactionSet {
	result := &TransactionSet{}
	searchLower := strings.ToLower(search)
	for _, t := range ts.Transactions {
		if strings.Contains(strings.ToLower(t.Description), searchLower) {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}

// SumAmount returns the sum of all transaction amounts
func (ts *TransactionSet) SumAmount() float64 {
	var sum float64
	for _, t := range ts.Transactions {
		sum += t.Amount
	}
	return sum
}

// SumAbsAmount returns the sum of absolute values
func (ts *TransactionSet) SumAbsAmount() float64 {
	var sum float64
	for _, t := range ts.Transactions {
		sum += math.Abs(t.Amount)
	}
	return sum
}

// GroupByMonth groups transactions by month
func (ts *TransactionSet) GroupByMonth() map[string]*TransactionSet {
	result := make(map[string]*TransactionSet)
	for _, t := range ts.Transactions {
		month := t.Date.Format("2006-01")
		if result[month] == nil {
			result[month] = &TransactionSet{}
		}
		result[month].Transactions = append(result[month].Transactions, t)
	}
	return result
}

// GroupByCategory groups transactions by category
func (ts *TransactionSet) GroupByCategory() map[string]*TransactionSet {
	result := make(map[string]*TransactionSet)
	for _, t := range ts.Transactions {
		cat := t.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		if result[cat] == nil {
			result[cat] = &TransactionSet{}
		}
		result[cat].Transactions = append(result[cat].Transactions, t)
	}
	return result
}

// GroupByDate groups transactions by date
func (ts *TransactionSet) GroupByDate() map[string]*TransactionSet {
	result := make(map[string]*TransactionSet)
	for _, t := range ts.Transactions {
		dateKey := t.Date.Format("2006-01-02")
		if result[dateKey] == nil {
			result[dateKey] = &TransactionSet{}
		}
		result[dateKey].Transactions = append(result[dateKey].Transactions, t)
	}
	return result
}

// SortByDate sorts transactions by date (ascending)
func (ts *TransactionSet) SortByDate() *TransactionSet {
	sorted := make([]Transaction, len(ts.Transactions))
	copy(sorted, ts.Transactions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})
	return &TransactionSet{Transactions: sorted}
}

// SortByDateDesc sorts transactions by date (descending)
func (ts *TransactionSet) SortByDateDesc() *TransactionSet {
	sorted := make([]Transaction, len(ts.Transactions))
	copy(sorted, ts.Transactions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.After(sorted[j].Date)
	})
	return &TransactionSet{Transactions: sorted}
}

// SortByAmount sorts transactions by amount (ascending)
func (ts *TransactionSet) SortByAmount() *TransactionSet {
	sorted := make([]Transaction, len(ts.Transactions))
	copy(sorted, ts.Transactions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Amount < sorted[j].Amount
	})
	return &TransactionSet{Transactions: sorted}
}

// MinDate returns the earliest transaction date
func (ts *TransactionSet) MinDate() time.Time {
	if len(ts.Transactions) == 0 {
		return time.Time{}
	}
	minDate := ts.Transactions[0].Date
	for _, t := range ts.Transactions[1:] {
		if t.Date.Before(minDate) {
			minDate = t.Date
		}
	}
	return minDate
}

// MaxDate returns the latest transaction date
func (ts *TransactionSet) MaxDate() time.Time {
	if len(ts.Transactions) == 0 {
		return time.Time{}
	}
	maxDate := ts.Transactions[0].Date
	for _, t := range ts.Transactions[1:] {
		if t.Date.After(maxDate) {
			maxDate = t.Date
		}
	}
	return maxDate
}

// Categories returns a sorted list of unique categories
func (ts *TransactionSet) Categories() []string {
	catMap := make(map[string]bool)
	for _, t := range ts.Transactions {
		cat := t.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		catMap[cat] = true
	}

	cats := make([]string, 0, len(catMap))
	for cat := range catMap {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return cats
}

// Paginate returns a slice of transactions for the given page
func (ts *TransactionSet) Paginate(page, perPage int) *TransactionSet {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 25
	}

	start := (page - 1) * perPage
	if start >= len(ts.Transactions) {
		return &TransactionSet{}
	}

	end := start + perPage
	if end > len(ts.Transactions) {
		end = len(ts.Transactions)
	}

	return &TransactionSet{Transactions: ts.Transactions[start:end]}
}

// TotalPages returns the number of pages for the given page size
func (ts *TransactionSet) TotalPages(perPage int) int {
	if perPage < 1 {
		perPage = 25
	}
	return (len(ts.Transactions) + perPage - 1) / perPage
}

// MonthlyTotals returns a map of month -> total amount
func (ts *TransactionSet) MonthlyTotals() map[string]float64 {
	result := make(map[string]float64)
	for _, t := range ts.Transactions {
		month := t.Date.Format("2006-01")
		result[month] += t.Amount
	}
	return result
}

// CategoryTotals returns a map of category -> total amount
func (ts *TransactionSet) CategoryTotals() map[string]float64 {
	result := make(map[string]float64)
	for _, t := range ts.Transactions {
		cat := t.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		result[cat] += math.Abs(t.Amount)
	}
	return result
}

// Copy creates a shallow copy of the TransactionSet
func (ts *TransactionSet) Copy() *TransactionSet {
	copied := make([]Transaction, len(ts.Transactions))
	copy(copied, ts.Transactions)
	return &TransactionSet{Transactions: copied}
}
