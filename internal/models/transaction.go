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

// TransactionType indicates whether a transaction is income, an outflow,
// or a movement between the user's own accounts.
type TransactionType string

const (
	Income  TransactionType = "Income"
	Outflow TransactionType = "Outflow"
	// Transfer is money moving between the user's own accounts. It is
	// neither income nor expense and is excluded from both: every
	// aggregation filters by type, so a Transfer row falls out of Total
	// Income and Total Expenses without a formula change. Unlike the
	// drop-on-load filter it replaces, a Transfer row stays visible in
	// the ledger. See GLOSSARY.md ("Transfer", "Internal transfer").
	Transfer TransactionType = "Transfer"
)

// Transaction represents a single financial transaction
type Transaction struct {
	ID                  string          `json:"id"`
	Date                time.Time       `json:"date"`
	Amount              float64         `json:"amount"`
	Description         string          `json:"description"`
	DisplayName         string          `json:"display_name,omitempty"`         // User-assigned alias
	MajorExpenseName    string          `json:"major_expense_name,omitempty"`   // Derived; stamped at load time, not persisted to source CSVs
	EnrichedDescription string          `json:"enriched_description,omitempty"` // Derived from external sources (e.g. Amazon order data); stamped at load time
	Category            string          `json:"category"`
	TransactionType     TransactionType `json:"transaction_type"`
	SourceFile          string          `json:"source_file"`
	Hash                string          `json:"hash"`

	// AccountID is the ID of the Account whose CSV this row came from,
	// stamped at load time by matching the file's basename against each
	// account's FilePatterns. Empty means unassigned -- the file matched
	// no account. Unassigned rows load normally and are counted
	// (DataLoader.UnassignedCount); they are never dropped.
	AccountID string `json:"account_id,omitempty"`

	// StableID is the description-independent identity every user
	// decision (pins, near-duplicate resolutions, Amazon enrichment) is
	// keyed on: `accountID|YYYY-MM-DD|amount-in-cents|n`. See
	// StableIDFor and GLOSSARY.md. Stamped at load time, after the sign
	// flip and account attribution, so the amount it encodes is the one
	// the app actually uses. Hash remains for the legacy fallback.
	StableID string `json:"stable_id,omitempty"`

	// Status is the bank-reported lifecycle marker (e.g. "Posted",
	// "Scheduled Bill Pay"). Optional; populated when the source CSV
	// has a Status column. Used by near-duplicate detection.
	Status string `json:"status,omitempty"`

	// Suppressed is true when the user has resolved a near-duplicate
	// pair and chose to drop this side from totals/aggregations.
	// The transaction stays in the explorer view for audit/undo.
	Suppressed bool `json:"suppressed,omitempty"`

	// DuplicatePairKey is non-empty when this transaction is part of
	// an unresolved near-duplicate candidate pair. Used to render
	// "possible duplicate" badges and link to the review panel.
	DuplicatePairKey string `json:"duplicate_pair_key,omitempty"`

	// TransferClass qualifies a TransactionType of Transfer:
	// "paired" (the counterparty leg is loaded and linked through
	// TransferPairKey) or "external" (the counterparty account's CSV is
	// not loaded, e.g. a Vanguard contribution). A non-transfer row
	// carries "". See GLOSSARY.md ("TransferClass").
	TransferClass string `json:"transfer_class,omitempty"`

	// TransferPairKey is shared by exactly the two legs of one paired
	// transfer, so either leg resolves the pair. Empty on external
	// transfers and on non-transfer rows. See GLOSSARY.md
	// ("TransferPairKey").
	TransferPairKey string `json:"transfer_pair_key,omitempty"`

	// Derived fields (computed, not stored)
	Month      string `json:"month,omitempty"` // "2024-01"
	Week       string `json:"week,omitempty"`  // "2024-W05"
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

// StableIDFor builds the durable identity of one transaction:
//
//	accountID|YYYY-MM-DD|amount-in-cents|occurrence
//
// e.g. "usaa-checking|2025-05-04|-1234|0". occurrence is the 0-based index
// among rows sharing the first three components, counted in file row order,
// so several same-amount rows on one account and one day stay distinguishable
// without the description.
//
// The description is deliberately absent: it is the one part of a bank export
// that changes without the underlying transaction changing, and keying user
// decisions on it means a reformatted description silently orphans them.
// accountID is "file:<basename>" for rows whose file matched no account.
// amountCents is the post-sign-normalization amount, so a credit-kind flip is
// reflected in the identity.
func StableIDFor(accountID string, date time.Time, amountCents int64, occurrence int) string {
	return fmt.Sprintf("%s|%s|%d|%d", accountID, date.Format("2006-01-02"), amountCents, occurrence)
}

// AmountCents converts a transaction amount to integer cents, the unit
// StableIDFor encodes. Rounding (rather than truncating) keeps values like
// 12.34 off the 1233-cent cliff float64 would otherwise put them on.
func AmountCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// ResolveByIdentity looks a transaction up in a map keyed by transaction
// identity, trying its StableID first and falling back to its legacy content
// Hash. It returns the value, the key that matched, and whether one did.
//
// This is the single read path for every identity-keyed sidecar store
// (transaction pins, duplicate decisions, Amazon enrichment). The fallback is
// what lets a sidecar written before StableID existed keep working with no
// migration step: entries are rekeyed opportunistically when their store is
// next written, never in a one-shot pass.
func ResolveByIdentity[V any](m map[string]V, t Transaction) (V, string, bool) {
	var zero V
	if len(m) == 0 {
		return zero, "", false
	}
	if t.StableID != "" {
		if v, ok := m[t.StableID]; ok {
			return v, t.StableID, true
		}
	}
	if t.Hash != "" {
		if v, ok := m[t.Hash]; ok {
			return v, t.Hash, true
		}
	}
	return zero, "", false
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

// Label returns the user-facing name for a transaction.
// Precedence: DisplayName (per-txn alias) -> EnrichedDescription
// (per-txn external data, e.g. Amazon product) -> MajorExpenseName
// (rule-based group name) -> Description (bank text). Per-transaction
// signals win over rule-based grouping because they're strictly more
// specific; grouping/aggregation reads MajorExpenseName directly so
// it's unaffected by this ordering.
func (t Transaction) Label() string {
	switch {
	case t.DisplayName != "":
		return t.DisplayName
	case t.EnrichedDescription != "":
		return t.EnrichedDescription
	case t.MajorExpenseName != "":
		return t.MajorExpenseName
	default:
		return t.Description
	}
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

// FilterBySearch returns transactions matching the search term in
// description, display name, major-expense name, or enriched description.
func (ts *TransactionSet) FilterBySearch(search string) *TransactionSet {
	result := &TransactionSet{}
	searchLower := strings.ToLower(search)
	for _, t := range ts.Transactions {
		if strings.Contains(strings.ToLower(t.Description), searchLower) ||
			(t.DisplayName != "" && strings.Contains(strings.ToLower(t.DisplayName), searchLower)) ||
			(t.MajorExpenseName != "" && strings.Contains(strings.ToLower(t.MajorExpenseName), searchLower)) ||
			(t.EnrichedDescription != "" && strings.Contains(strings.ToLower(t.EnrichedDescription), searchLower)) {
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

// Active returns a new TransactionSet with Suppressed transactions
// filtered out. Aggregation/reporting call sites should use this to
// avoid double-counting near-duplicate pairs the user has resolved.
// The explorer keeps the raw slice so users can see and undo
// suppressions. Safe on a nil receiver.
func (ts *TransactionSet) Active() *TransactionSet {
	if ts == nil {
		return &TransactionSet{}
	}
	result := &TransactionSet{}
	for _, t := range ts.Transactions {
		if !t.Suppressed {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}
