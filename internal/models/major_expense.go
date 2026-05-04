package models

import "time"

// MajorExpense represents a user-declared expense the user understands
// well enough to use as a baseline for grouping transactions and flagging
// unusual amounts.
type MajorExpense struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Keywords    []string `json:"keywords"`
	ExpectedMin float64  `json:"expected_min"`
	ExpectedMax float64  `json:"expected_max"`
	Notes       string   `json:"notes,omitempty"`
	// IsInternalTransfer marks this entry as an internal-transfer filter
	// rather than a real expense. When true, transactions matched by its
	// keywords (and/or amount range) are dropped at load time alongside
	// the hardcoded transfer patterns, so they don't inflate spending
	// charts. The entry is hidden from spending rollups but still shown
	// in the Major Expenses list so the user can see what's being
	// filtered and edit it.
	IsInternalTransfer bool      `json:"is_internal_transfer,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// MajorExpenseStore is the persisted shape of major_expenses.json. The
// wrapping struct keeps the file's top level an object so additional
// configuration can be added later without breaking older files.
type MajorExpenseStore struct {
	Expenses []MajorExpense `json:"expenses"`
}

// DeletedMajorExpense is an archived MajorExpense plus the pin hashes
// that pointed at it at the moment of deletion. Restoring the expense
// reinserts it into the active list and re-applies the captured pins
// (skipping any hash that has since been pinned to a different expense).
type DeletedMajorExpense struct {
	Expense      MajorExpense `json:"expense"`
	DeletedAt    time.Time    `json:"deleted_at"`
	PinnedHashes []string     `json:"pinned_hashes"`
}

// DeletedMajorExpenseStore is the persisted shape of
// deleted_major_expenses.json.
type DeletedMajorExpenseStore struct {
	Deleted []DeletedMajorExpense `json:"deleted"`
}

// ExceptionUnknownLargeTxn flags a transaction that does not match any
// declared major expense and exceeds the user's notable-amount threshold.
type ExceptionUnknownLargeTxn struct {
	Transaction Transaction `json:"transaction"`
}

// ExceptionAnomalousAmount flags a transaction that matches a declared
// major expense but whose amount falls outside the expected range.
type ExceptionAnomalousAmount struct {
	MajorExpenseID   string      `json:"major_expense_id"`
	MajorExpenseName string      `json:"major_expense_name"`
	Transaction      Transaction `json:"transaction"`
	ExpectedMin      float64     `json:"expected_min"`
	ExpectedMax      float64     `json:"expected_max"`
}

// ExceptionNewMerchant flags a transaction whose normalized description
// has never been seen before the recent window.
type ExceptionNewMerchant struct {
	Description string      `json:"description"`
	FirstSeen   time.Time   `json:"first_seen"`
	Transaction Transaction `json:"transaction"`
}

// ExceptionsReport bundles all three exception flavors plus the
// thresholds used to compute them so the UI can show what was applied.
type ExceptionsReport struct {
	UnknownLarge  []ExceptionUnknownLargeTxn `json:"unknown_large"`
	Anomalous     []ExceptionAnomalousAmount `json:"anomalous"`
	NewMerchants  []ExceptionNewMerchant     `json:"new_merchants"`
	Threshold     float64                    `json:"threshold"`
	NewWindowDays int                        `json:"new_window_days"`
}
