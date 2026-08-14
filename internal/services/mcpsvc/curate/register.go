// Package curate serves the Major Expenses page's data and edits over MCP:
// which declared expenses exist, which transactions matched them, what fell
// through as an exception, and the four writes that change any of that.
//
// It calls the same *dataloader.DataLoader methods the /major-expenses
// handlers call and recomputes the page's view with the page's own
// majorexpenses.Match options, so a tool answer and the page agree.
package curate

import (
	"fmt"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These mirror internal/handlers/majorexpenses/handlers.go's package
// constants. They are duplicated rather than exported from the handler
// package because a service must not import a handlers package; the
// duplication is pinned by TestThresholdsMatchThePage.
const (
	defaultUnknownThreshold = 100.0
	defaultNewWindowDays    = 30
)

// File names in the data directory that the write tools snapshot before
// changing. They mirror the unexported constants in
// internal/services/dataloader. Unused until the write tools (upsert/delete
// major expense, pin/unpin transactions) land in later tasks.
const (
	majorExpensesFile   = "major_expenses.json"
	transactionPinsFile = "transaction_pins.json"
	//lint:ignore U1000 consumed by delete/restore-major-expense in a later task
	deletedMajorExpensesFile = "deleted_major_expenses.json"
)

// TransactionSource loads the full transaction history. *dataloader.DataLoader
// satisfies it via LoadData, so no adapter is needed in production. The
// interface exists so tests can substitute a canned models.TransactionSet
// instead of driving real CSV parsing and classification.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// ExpenseStore reads and writes the declared major expenses and their
// soft-delete archive. *dataloader.DataLoader satisfies it. Tests use a real
// loader over t.TempDir() rather than a stub: these methods write JSON files
// whose on-disk layout is part of what is being tested.
type ExpenseStore interface {
	LoadMajorExpenses() ([]models.MajorExpense, error)
	AddMajorExpense(models.MajorExpense) ([]models.MajorExpense, error)
	UpdateMajorExpense(string, models.MajorExpense) ([]models.MajorExpense, error)
	ArchiveMajorExpense(string) error
	RestoreMajorExpense(string) error
	LoadDeletedMajorExpenses() ([]models.DeletedMajorExpense, error)
}

// PinStore reads and writes transaction_pins.json.
// *dataloader.DataLoader satisfies it.
type PinStore interface {
	LoadTransactionPins() (map[string]string, error)
	SetTransactionPins(map[string]string) (int, error)
}

// Deps is what the curation tools need. Store is optional and used only to
// turn a locked store into a clear message instead of a parse failure.
// Snapshots is required by the write tools, which abort rather than write
// without a backup; the read tools never touch it.
type Deps struct {
	Transactions TransactionSource
	Expenses     ExpenseStore
	Pins         PinStore
	Store        *storage.Storage
	Snapshots    *snapshot.Snapshotter
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

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	return d.Transactions.LoadData()
}

// parseWindowDate parses a YYYY-MM-DD date parameter. An empty value is not
// an error -- it means "no bound on this side" -- and returns a nil pointer.
// An unparseable value returns an error naming the offending field and value,
// meant to surface as a tool error rather than a panic.
//
// Duplicated from mcpsvc/spend rather than shared: the two packages are
// siblings and neither may import the other.
func parseWindowDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a valid date (want YYYY-MM-DD): %w", field, value, err)
	}
	return &t, nil
}

// view is the recomputed Major Expenses page state for one window.
type view struct {
	Start    time.Time
	End      time.Time
	Expenses []models.MajorExpense
	Pins     map[string]string
	Match    majorexpenses.MatchResult
}

// pageView reproduces internal/handlers/majorexpenses.buildPageData's
// pipeline exactly: resolve the window, drop transactions the user resolved
// as duplicates, narrow to the window, keep only outflows, then match.
//
// Two orderings here are load-bearing and neither is incidental.
//
// The unbounded window defaults come from the RAW set, BEFORE Active(),
// because buildPageData's parseRangeFromRequest reads MinDate/MaxDate off the
// raw set too. This deliberately differs from the six mcpsvc/spend tools,
// which default over active rows. The transactions analyzed are identical
// either way -- every active row falls inside both windows -- so the only
// thing at stake is the start/end reported back to the caller, and that has
// to be the page's, not a second opinion. Do not "fix" this to Active().
//
// The outflow filter comes before Match so that income whose description
// happens to contain a keyword cannot inflate a group's count or total.
func (d Deps) pageView(startDate, endDate string) (*view, error) {
	start, err := parseWindowDate("start_date", startDate)
	if err != nil {
		return nil, err
	}
	end, err := parseWindowDate("end_date", endDate)
	if err != nil {
		return nil, err
	}

	expenses, err := d.Expenses.LoadMajorExpenses()
	if err != nil {
		return nil, fmt.Errorf("load major expenses: %w", err)
	}
	if expenses == nil {
		expenses = []models.MajorExpense{}
	}

	ts, err := d.load()
	if err != nil {
		return nil, err
	}

	pins, err := d.Pins.LoadTransactionPins()
	if err != nil {
		return nil, fmt.Errorf("load transaction pins: %w", err)
	}

	// Window defaults off the raw set, matching parseRangeFromRequest. See
	// the note above: this is not the Active()-first defaulting the spend
	// tools use, and that is on purpose.
	from := ts.MinDate()
	if start != nil {
		from = *start
	}
	to := ts.MaxDate()
	if end != nil {
		to = *end
	}

	outflows := ts.Active().FilterByDateRange(from, to).FilterByType(models.Outflow)

	return &view{
		Start:    from,
		End:      to,
		Expenses: expenses,
		Pins:     pins,
		Match: majorexpenses.Match(outflows, expenses, majorexpenses.MatchOptions{
			UnknownLargeThreshold: defaultUnknownThreshold,
			NewMerchantWindow:     time.Duration(defaultNewWindowDays) * 24 * time.Hour,
			Pins:                  pins,
		}),
	}, nil
}

// pinnableRow is a transaction as the curation tools report it: the signed
// amount exactly as stored, plus the hash needed to act on it.
type pinnableRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Pinned      bool    `json:"pinned"`
}

func rowFor(t models.Transaction, pinned map[string]bool) pinnableRow {
	return pinnableRow{
		Hash:        t.Hash,
		Date:        t.Date.Format("2006-01-02"),
		Description: t.Label(),
		Category:    t.Category,
		Amount:      t.Amount,
		Pinned:      pinned[t.Hash],
	}
}

// formatDay renders a window bound for output. The zero time -- what MinDate
// returns for an empty ledger -- renders as empty rather than "0001-01-01".
func formatDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// Register adds the curation tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
	registerListExceptions(s, deps)
	registerPin(s, deps)
	registerUpsert(s, deps)
}
