// Package admin serves the app's housekeeping state over MCP: what the
// storage, backup and settings layers are doing, which CSV files are loaded,
// and the near-duplicate review queue the /duplicates page owns.
//
// It calls the same *dataloader.DataLoader, *storage.Storage,
// *retirement.SettingsManager and *backup.Service instances the HTTP handlers
// use, so a tool answer and a page cannot disagree about the app's state.
package admin

import (
	"context"
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/backup"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/restore"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// duplicateDecisionsFile is the sidecar the write tools snapshot before
// changing. It mirrors the unexported constant in
// internal/services/dataloader.
const duplicateDecisionsFile = "duplicate_decisions.json"

// TransactionSource loads the full transaction history.
// *dataloader.DataLoader satisfies it via LoadData. The interface exists so
// tests can substitute a canned models.TransactionSet.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// FileLister reports the CSV inventory. *dataloader.DataLoader satisfies it.
//
// CountCSVFiles answers the same question as len(GetFileInfo()) without
// parsing every CSV to do it; get_status wants only the count and is
// advertised as the cheap first-resort probe, so it must not pay for the
// full inventory.
type FileLister interface {
	GetFileInfo() ([]models.FileInfo, error)
	CountCSVFiles() (int, error)
}

// DuplicateSource exposes the near-duplicate detection results cached by the
// most recent LoadData. *dataloader.DataLoader satisfies it.
//
// Every caller must LoadData FIRST: these four return the previous load's
// results, and on a freshly constructed loader they return nothing at all.
//
// The three list methods partition the detected pairs. A pair settled as
// kept_both is in KeptBothDuplicates and nowhere else -- it is absent from
// ResolvedDuplicates by design, so list_duplicates must read it separately
// or the pair_key is unrecoverable once the model loses context.
type DuplicateSource interface {
	UnresolvedDuplicates() []dataloader.DuplicatePair
	ResolvedDuplicates() []dataloader.DuplicatePair
	KeptBothDuplicates() []dataloader.DuplicatePair
	UnresolvedDuplicateCount() int
}

// DecisionStore reads and writes duplicate_decisions.json.
// *dataloader.DataLoader satisfies it.
type DecisionStore interface {
	LoadDuplicateDecisions() (map[string]dataloader.DuplicateDecision, error)
	SaveDuplicateDecision(string, dataloader.DuplicateDecision) error
	ClearDuplicateDecision(string) error

	// LookupDuplicateDecision finds the decision filed for a pair key,
	// checking legacy pre-StableID aliases when the current key itself
	// carries nothing. See the dataloader implementation's doc comment.
	LookupDuplicateDecision(string) (dataloader.DuplicateDecision, bool, error)
}

// SettingsSource reports the retirement planner's state.
// *retirement.SettingsManager satisfies it.
type SettingsSource interface {
	Revision() int
	ActiveScenario() string
	SettingsDir() string
}

// BackupService runs and reports on data-directory snapshots.
// *backup.Service satisfies it.
//
// List reports the archives on disk newest first. It is what gives the model
// a name to hand to restore_backup: before it existed the archives were
// visible only to the Backup Status page, so a restore tool would have had
// nothing to address.
type BackupService interface {
	BackupDir() string
	DataDir() string
	Enabled() bool
	Meta() (backup.Meta, error)
	Snapshot(ctx context.Context) error
	List() ([]backup.Archive, error)
}

// RestoreService rewrites the data directory from a named archive in the
// backup directory. *restore.Service satisfies it.
//
// It is the same instance the /restore HTTP handler uses, so a restore driven
// from a tool and one driven from the browser take the same snapshot hold and
// the same settings-rewrite gate.
type RestoreService interface {
	FromArchive(ctx context.Context, name string) (restore.Result, error)
}

// Deps is what the housekeeping tools need. Every field may be nil except
// Transactions: a nil dependency makes the tools that need it fail their own
// call with a named error rather than being absent from the tool list, which
// matches how plan and curate behave.
type Deps struct {
	Transactions TransactionSource
	Files        FileLister
	Duplicates   DuplicateSource
	Decisions    DecisionStore
	Store        *storage.Storage
	Settings     SettingsSource
	Backups      BackupService
	Restores     RestoreService
	Snapshots    *snapshot.Snapshotter

	// Confirm mints and redeems the two-step tokens guarded tools require. A
	// nil registry makes those tools refuse rather than run unguarded.
	Confirm *confirm.Registry

	// Approvals holds the out-of-band approval requests a human answers in a
	// browser, and must be the SAME instance the /mcp/approve route serves --
	// two registries would mean a person approving a request no tool is
	// waiting on. Nil (or an empty BaseURL) drops the guarded tools to the
	// in-client form prompt, and then to the token alone.
	Approvals *confirm.Approvals

	// BaseURL is the origin a human can reach this server at, used to build
	// the approval URL. Empty disables browser approval for the same reason a
	// nil Approvals does.
	BaseURL string

	// Shutdown stops the server. It is a func, never a direct os.Exit call,
	// because a test that invokes the real thing kills the test binary. Nil
	// means this server has no shutdown path wired.
	Shutdown func()
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

// locked reports whether the store is encrypted and not currently unlocked,
// which is the one condition under which nothing can read the ledger.
func (d Deps) locked() bool {
	return d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked()
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.locked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	if d.Transactions == nil {
		return nil, fmt.Errorf("transaction source is not configured on this server")
	}
	return d.Transactions.LoadData()
}

// Register adds the housekeeping tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerStatus(s, deps)
	registerFiles(s, deps)
	registerListDuplicates(s, deps)
	registerResolve(s, deps)
	registerUndo(s, deps)
	registerRunBackup(s, deps)
	registerListBackups(s, deps)
	registerRestoreBackup(s, deps)
	registerShutdown(s, deps)
}
