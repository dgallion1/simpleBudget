package models

import "time"

// AccountKind classifies an account by what it holds. The enum is closed:
// anything outside these five values is a data error, not an extension
// point. See GLOSSARY.md ("Account kind").
//
// Two behavioral consequences (LB1 added the second):
//  1. AccountKindCredit forces the credit-card sign convention for every
//     CSV file that belongs to the account, overriding the >=70%-positive
//     heuristic in the dataloader. Every other kind leaves that heuristic
//     exactly as it is.
//  2. Only checking and savings compare against LowBalanceThreshold —
//     accounts.LowBalanceApplies gates the low-balance flag and the
//     funding projection on every surface (a credit balance is negative
//     by nature, a brokerage balance is not spendable cash; comparing
//     either to a cash floor would flag it permanently).
type AccountKind string

const (
	AccountKindChecking  AccountKind = "checking"
	AccountKindSavings   AccountKind = "savings"
	AccountKindBrokerage AccountKind = "brokerage"
	AccountKindCredit    AccountKind = "credit"
	AccountKindOther     AccountKind = "other"
)

// BalanceAnchor is a user-entered statement of an account's balance as of
// the END of Date. A transaction dated on the anchor day is therefore
// already reflected in Amount and must not be added again when rolling the
// balance forward. Amount is in bank convention (positive = money you have).
type BalanceAnchor struct {
	Date   time.Time `json:"date"`
	Amount float64   `json:"amount"`
	Note   string    `json:"note,omitempty"`
}

// Account is a named source of transactions, persisted in the
// data/accounts.json sidecar through internal/services/storage (so age
// encryption applies exactly as it does for every other sidecar).
//
// One CSV file maps to exactly one account, matched by filename against
// FilePatterns; first match wins, with accounts considered in ascending ID
// order so the result does not depend on slice order. A file matching no
// account leaves its rows' AccountID empty — unassigned, never dropped.
type Account struct {
	// ID is a stable slug, e.g. "usaa-checking". Transactions and
	// persisted user decisions reference it, so it must be unique and
	// should not be rewritten casually.
	ID string `json:"id"`

	// Name is the user-facing label, e.g. "USAA Checking". Required.
	Name string `json:"name"`

	// Institution is the bank/broker, e.g. "USAA". Optional.
	Institution string `json:"institution,omitempty"`

	Kind AccountKind `json:"kind"`

	// FilePatterns are matched case-insensitively against a CSV basename:
	// path.Match glob first, plain substring as a fallback.
	FilePatterns []string `json:"file_patterns,omitempty"`

	// Anchors are kept sorted by Date.
	Anchors []BalanceAnchor `json:"anchors,omitempty"`

	// LowBalanceThreshold is the balance below which the account is
	// flagged. Zero means "use the default"; only meaningful for the
	// checking and savings kinds.
	LowBalanceThreshold float64 `json:"low_balance_threshold,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
