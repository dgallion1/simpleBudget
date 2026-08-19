// Package ledger serves the account and transfer state over MCP: the
// configured accounts with their balances, the A5 funding projection, the
// transfer flows (paired and external), and the two mutating tools that
// record a balance anchor or resolve a suspected transfer pair.
//
// It reads through the same *dataloader.DataLoader, *storage.Storage and
// *accounts sidecar the HTTP handlers use, and writes through the same
// confirm-token flow the guarded admin tools use, so a tool answer and a
// page cannot disagree about the user's money.
package ledger

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"
	"budget2/internal/services/transfers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// accountsFile is the sidecar set_balance_anchor snapshots before writing,
// mirroring the unexported constant in internal/services/accounts.
const accountsFile = accounts.AccountsFile

// transferDecisionsFile is the sidecar resolve_transfer snapshots before
// writing, mirroring the unexported constant in internal/services/dataloader.
const transferDecisionsFile = "transfer_decisions.json"

// TransactionSource loads the full transaction history.
// *dataloader.DataLoader satisfies it via LoadData. The interface exists so
// tests can substitute a canned models.TransactionSet.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// AccountStore reads and writes the accounts sidecar. accounts.Load/Save
// over a *storage.Storage satisfy it; tests can substitute a fake.
type AccountStore interface {
	LoadAccounts() ([]models.Account, error)
	SaveAccounts([]models.Account) error
}

// TransferSource exposes the suspected-transfer review queue and the
// resolver. *dataloader.DataLoader satisfies both.
type TransferSource interface {
	SuspectedTransfers() []transfers.Suspected
	ResolveTransfer(pairKey string, v transfers.Verdict) error
}

// Deps is what the ledger tools need. Every field may be nil except
// Accounts: a nil dependency makes the tools that need it fail their own
// call with a named error rather than being absent from the tool list,
// matching how plan, curate and admin behave.
type Deps struct {
	Transactions TransactionSource
	Accounts     AccountStore
	Transfers    TransferSource
	Store        *storage.Storage

	// Snapshots copies the sidecar a guarded tool is about to write, so an
	// unwanted change has a recovery path. A nil snapshotter makes the
	// guarded tools refuse rather than write without a backup.
	Snapshots *snapshot.Snapshotter

	// Confirm mints and redeems the two-step tokens the guarded tools
	// require. A nil registry makes those tools refuse rather than run
	// unguarded.
	Confirm *confirm.Registry

	// Approvals holds the out-of-band approval requests a human answers in a
	// browser, and must be the SAME instance the /mcp/approve route serves.
	// Nil (or an empty BaseURL) drops the guarded tools to the in-client form
	// prompt, and then to the token alone.
	Approvals *confirm.Approvals

	// BaseURL is the origin a human can reach this server at, used to build
	// the approval URL. Empty disables browser approval for the same reason a
	// nil Approvals does.
	BaseURL string
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	if d.Transactions == nil {
		return nil, fmt.Errorf("transaction source is not configured on this server")
	}
	return d.Transactions.LoadData()
}

// recoverToError converts a panic into an error so a bad definition fails one
// tool call instead of terminating the session. The go-sdk dispatches every
// tool call on its own goroutine with no recover of its own, so this must run
// via a defer inside each handler closure.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// Register adds the ledger tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerGetAccounts(s, deps)
	registerGetBalanceProjection(s, deps)
	registerGetTransfers(s, deps)
	registerSetBalanceAnchor(s, deps)
	registerResolveTransfer(s, deps)
}

// storageAccountStore adapts a *storage.Storage to the AccountStore
// interface, reading and writing the accounts sidecar through the accounts
// package so the tool and the /accounts page can never disagree about what an
// account is.
type storageAccountStore struct {
	store *storage.Storage
}

// NewAccountStore returns an AccountStore over s. Nil s yields nil; the
// caller should guard as usual.
func NewAccountStore(s *storage.Storage) AccountStore {
	if s == nil {
		return nil
	}
	return storageAccountStore{store: s}
}

func (s storageAccountStore) LoadAccounts() ([]models.Account, error) {
	return accounts.Load(s.store)
}

// SaveAccounts persists an already-computed account list verbatim, through
// accounts.Mutate rather than accounts.Save directly, so this call is also
// one held section against every other Mutate sequence and a restore's
// exclusive hold — even though it discards the section's own load and
// always saves the list the caller already built.
func (s storageAccountStore) SaveAccounts(a []models.Account) error {
	return accounts.Mutate(s.store, func([]models.Account) ([]models.Account, error) {
		return a, nil
	})
}

// Compile-time interface checks.
var (
	_ TransactionSource = (*dataloader.DataLoader)(nil)
	_ TransferSource    = (*dataloader.DataLoader)(nil)
)
