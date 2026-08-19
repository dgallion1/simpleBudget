package ledger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/insights"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// accountRow is one account in get_accounts' result.
type accountRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Institution string `json:"institution,omitempty"`
	Kind        string `json:"kind"`

	// Balance is the current balance rolled forward from the latest anchor
	// through the account's transactions. Available is false when there is
	// no anchor at or before today, in which case Balance is zero and MUST
	// NOT be rendered as a balance: an unknown balance and a zero balance
	// are different facts (GLOSSARY.md "BalanceAnchor").
	Balance   float64 `json:"balance"`
	Available bool    `json:"available"`

	// AnchorDate is the date of the anchor the balance was rolled forward
	// from, YYYY-MM-DD; empty when Available is false.
	AnchorDate string `json:"anchor_date,omitempty"`

	// LowBalance is true when the balance is available and strictly below
	// the account's low-balance threshold. False when unavailable -- an
	// unknown balance is not "low".
	LowBalance bool `json:"low_balance"`

	// Threshold is the low-balance threshold used: the account's own
	// LowBalanceThreshold, or the default when that is zero. Reported so the
	// model can label the flag.
	Threshold float64 `json:"threshold"`

	// Freshness is the latest transaction date for this account,
	// YYYY-MM-DD; empty when no transaction belongs to the account.
	Freshness string `json:"freshness,omitempty"`
}

type getAccountsOutput struct {
	Count    int          `json:"count"`
	Accounts []accountRow `json:"accounts"`
}

func registerGetAccounts(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_accounts",
		Description: "List the configured accounts with their current balance, freshness, and whether each is " +
			"below its low-balance threshold. An account is a named source of transactions persisted in the " +
			"accounts.json sidecar (see GLOSSARY.md \"Account\"); one CSV file maps to exactly one account by " +
			"filename pattern. The balance is rolled forward from the latest BalanceAnchor -- a user-entered " +
			"{date, amount} stating the balance as of the END of that day -- plus the account's transaction " +
			"amounts after it (GLOSSARY.md \"BalanceAnchor\"). An account with NO anchor reports " +
			"available=false and balance=0: that is UNAVAILABLE, not a zero balance -- do not present it as $0. " +
			"low_balance is true only when the balance is available and strictly below the threshold; an " +
			"unavailable balance is not \"low\". freshness is the latest transaction date for the account, so " +
			"a stale CSV masquerading as a healthy balance is visible. No aggregation or projection logic is " +
			"computed here; the figures come from the accounts service's BalanceAt and Freshness.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (res *mcp.CallToolResult, out getAccountsOutput, err error) {
		defer recoverToError("get_accounts", &err)

		if deps.Accounts == nil {
			return nil, getAccountsOutput{}, fmt.Errorf("no account store is configured on this server")
		}
		accts, err := deps.Accounts.LoadAccounts()
		if err != nil {
			return nil, getAccountsOutput{}, fmt.Errorf("cannot load accounts: %w", err)
		}

		ts, err := deps.load()
		if err != nil {
			return nil, getAccountsOutput{}, err
		}
		var txns []models.Transaction
		if ts != nil {
			txns = ts.Active().Transactions
		}

		now := time.Now()
		rows := make([]accountRow, 0, len(accts))
		for _, a := range sortedAccountsByID(accts) {
			bal, bErr := accounts.BalanceAt(a, txns, now)
			if bErr != nil {
				return nil, getAccountsOutput{}, fmt.Errorf("cannot compute balance for %s: %w", a.ID, bErr)
			}
			fresh, _ := accounts.Freshness(a, txns)
			threshold := thresholdFor(a)
			row := accountRow{
				ID:          a.ID,
				Name:        a.Name,
				Institution: a.Institution,
				Kind:        string(a.Kind),
				Balance:     round2(bal.Amount),
				Available:   bal.Available,
				Threshold:   threshold,
			}
			if bal.Available {
				row.AnchorDate = bal.AnchorDate.Format("2006-01-02")
				row.LowBalance = bal.Amount < threshold
			}
			if fresh.IsZero() {
				row.Freshness = ""
			} else {
				row.Freshness = fresh.Format("2006-01-02")
			}
			rows = append(rows, row)
		}

		return nil, getAccountsOutput{Count: len(rows), Accounts: rows}, nil
	})
}

// thresholdFor returns the account's LowBalanceThreshold, falling back to the
// accounts package's default when it is zero. Mirrors accounts.thresholdFor,
// which is unexported; the default is a public constant.
func thresholdFor(a models.Account) float64 {
	if a.LowBalanceThreshold > 0 {
		return a.LowBalanceThreshold
	}
	return accounts.DefaultLowBalanceThreshold
}

// sortedAccountsByID returns a copy in ascending ID order, matching the order
// accounts.MatchFile resolves in and the order the /accounts page shows.
func sortedAccountsByID(accts []models.Account) []models.Account {
	out := make([]models.Account, len(accts))
	copy(out, accts)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// --- get_balance_projection ------------------------------------------------

type balanceProjectionInput struct {
	AccountID string `json:"account_id" jsonschema:"the account ID to project, as reported by get_accounts"`
	AsOf      string `json:"as_of,omitempty" jsonschema:"the \"as of\" date the projection rolls forward from, YYYY-MM-DD; defaults to today"`
}

type balanceProjectionOutput struct {
	AccountID string `json:"account_id"`

	// Available is false when there is no anchor at or before as_of, in which
	// case no projection is produced: an unknown balance and a zero balance
	// are different facts (GLOSSARY.md "BalanceAnchor").
	Available bool `json:"available"`

	// AsOf is the date the projection rolls forward from, YYYY-MM-DD.
	AsOf string `json:"as_of,omitempty"`

	// Threshold is the low-balance threshold the projection used.
	Threshold float64 `json:"threshold,omitempty"`

	// Crossing is the first date in the 35-day window the projected balance
	// crosses strictly below the threshold, YYYY-MM-DD; empty when Available
	// is false or the balance never crosses.
	Crossing string `json:"crossing,omitempty"`

	// Minimum is the lowest projected balance over the window; zero when
	// unavailable. NOT clamped to the threshold.
	Minimum float64 `json:"minimum,omitempty"`

	// SuggestedTopUp is the shortfall (Threshold - Minimum) rounded up to
	// the nearest $100, never negative; zero when there is no crossing or
	// when unavailable.
	SuggestedTopUp float64 `json:"suggested_top_up,omitempty"`

	// ReferenceAmount is the median of confirmed inbound paired-transfer
	// amounts into this account, so the model can say "you usually move $X".
	// HasReference is false when no such history exists; ReferenceAmount is
	// then zero and MUST NOT be presented as a number to move.
	ReferenceAmount float64 `json:"reference_amount,omitempty"`
	HasReference    bool    `json:"has_reference"`
}

func registerGetBalanceProjection(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_balance_projection",
		Description: "The 35-day funding projection for one account: the first date the projected balance crosses " +
			"below the account's low-balance threshold, the minimum projected balance, a suggested top-up rounded " +
			"up to the nearest $100, and the median of confirmed inbound paired-transfer amounts as a reference " +
			"(\"you usually move $X\"). Advisory only -- nothing is written. The projection rolls the account's " +
			"current balance (from its latest BalanceAnchor plus transactions after it) forward one day at a " +
			"time, applying expected recurring items for THIS ACCOUNT ONLY. Report \"cannot project\" when " +
			"available is false: that means there is no anchor at or before the as-of date, and an unknown " +
			"balance is not a zero balance to roll forward (GLOSSARY.md \"BalanceAnchor\"). has_reference is " +
			"false when no confirmed inbound paired transfers exist; reference_amount is then zero and MUST NOT " +
			"be presented as a number to move. No projection or balance logic is computed here; the figures come " +
			"from the accounts service's Project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in balanceProjectionInput) (res *mcp.CallToolResult, out balanceProjectionOutput, err error) {
		defer recoverToError("get_balance_projection", &err)

		if deps.Accounts == nil {
			return nil, balanceProjectionOutput{}, fmt.Errorf("no account store is configured on this server")
		}
		id := strings.TrimSpace(in.AccountID)
		if id == "" {
			return nil, balanceProjectionOutput{}, fmt.Errorf("account_id is required; call get_accounts for the current IDs")
		}

		asOf := time.Now()
		asOfSupplied := false
		if s := strings.TrimSpace(in.AsOf); s != "" {
			t, pErr := time.Parse("2006-01-02", s)
			if pErr != nil {
				return nil, balanceProjectionOutput{}, fmt.Errorf("as_of %q is not a valid date (want YYYY-MM-DD): %w", s, pErr)
			}
			asOf = t
			asOfSupplied = true
		}

		accts, err := deps.Accounts.LoadAccounts()
		if err != nil {
			return nil, balanceProjectionOutput{}, fmt.Errorf("cannot load accounts: %w", err)
		}
		acct, ok := findAccount(accts, id)
		if !ok {
			return nil, balanceProjectionOutput{}, fmt.Errorf("no account with id %q; call get_accounts for the current IDs", id)
		}

		ts, err := deps.load()
		if err != nil {
			return nil, balanceProjectionOutput{}, err
		}
		active, recurring := activeSetAndRecurringForProjection(ts, asOf, asOfSupplied)
		var txns []models.Transaction
		if active != nil {
			txns = active.Transactions
		}

		proj, pErr := accounts.Project(acct, txns, asOf, recurring)
		if pErr != nil {
			return nil, balanceProjectionOutput{}, fmt.Errorf("cannot project %s: %w", id, pErr)
		}

		out = balanceProjectionOutput{
			AccountID:       id,
			Available:       proj.Available,
			Threshold:       round2(proj.Threshold),
			Minimum:         round2(proj.Minimum),
			SuggestedTopUp:  round2(proj.SuggestedTopUp),
			ReferenceAmount: round2(proj.ReferenceAmount),
			HasReference:    proj.HasReference,
		}
		if proj.Available {
			out.AsOf = asOf.Format("2006-01-02")
		}
		if !proj.Crossing.IsZero() {
			out.Crossing = proj.Crossing.Format("2006-01-02")
		}
		return nil, out, nil
	})
}

// findAccount returns the account with the given ID.
func findAccount(accts []models.Account, id string) (models.Account, bool) {
	for _, a := range accts {
		if a.ID == id {
			return a, true
		}
	}
	return models.Account{}, false
}

// activeSetAndRecurringForProjection is get_balance_projection's single
// branch point on whether as_of was supplied, factored out of the handler
// so the branch itself -- not just the two functions it dispatches to -- is
// directly unit-testable.
//
// Only an EXPLICIT as_of truncates the active set and threads a reference
// date into recurring detection. When as_of is absent this takes the pre-R5
// path verbatim -- the full active set and insights.DetectRecurring's own
// MaxDate fallback (via recurringForProjectionDefault) -- so the default (no
// as_of) response cannot drift from established behaviour.
//
// When as_of IS supplied, the active set, the recurring detection, and the
// reference-amount median must all evaluate "as of" the requested date, not
// the ledger's raw or actual-latest date. This is what lets
// DetectRecurringAt's referenceDate (passed as asOf, via
// recurringForProjection) do its freshness job against the date the caller
// asked for.
func activeSetAndRecurringForProjection(ts *models.TransactionSet, asOf time.Time, asOfSupplied bool) (*models.TransactionSet, []models.RecurringPayment) {
	if ts == nil {
		return nil, nil
	}
	all := ts.Active()
	if !asOfSupplied {
		return all, recurringForProjectionDefault(all)
	}
	// Normalise to a day boundary using the dayOf convention
	// (internal/services/accounts/balance.go:201-203) before truncating, so
	// this boundary and BalanceAt's day-boundary comparison inside
	// accounts.Project agree. asOf already parses to UTC midnight
	// (time.Parse("2006-01-02", ...)), so this is a no-op in practice; it is
	// here so the boundary is correct by construction rather than by the
	// parse format's coincidence.
	asOfDay := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, asOf.Location())
	active := all.FilterByDateRange(all.MinDate(), asOfDay)
	return active, recurringForProjection(active, asOf)
}

// recurringForProjection builds the recurring-payment list the projection
// consumes from the loaded ledger. It runs the same insights detection the
// spend get_recurring tool uses, so the projection's expected recurring
// outflows agree with what the app shows.
//
// asOf is threaded through to insights.DetectRecurringAt as the reference
// date, matching the dashboard's detectRecurringForDashboard. DetectRecurringAt's
// referenceDate does two jobs (see its doc comment): it truncates the
// history detection runs over, and it is the freshness "now" each candidate
// series is checked against. Calling the DetectRecurring shorthand instead
// (referenceDate zero) would fall back to ts's own MaxDate for both jobs --
// wrong for a historical as_of (which would let detection see, and schedule
// against, transactions after the requested date) and wrong for a future
// as_of (which would judge freshness against the ledger's actual latest
// date instead of the date the caller asked about).
func recurringForProjection(ts *models.TransactionSet, asOf time.Time) []models.RecurringPayment {
	// The recurring engine lives in internal/services/insights and is the
	// shared source every consumer of recurring payments uses. This is a
	// read; nothing is written.
	if ts == nil || ts.Len() == 0 {
		return nil
	}
	return insights.DetectRecurringAt(ts, asOf)
}

// recurringForProjectionDefault is the as_of-absent counterpart of
// recurringForProjection. It calls the DetectRecurring shorthand (a zero
// referenceDate), which falls back to ts's own MaxDate for both the
// detection-history truncation and the freshness reference -- the pre-R5
// behaviour for get_balance_projection's default (no as_of) response. This
// exists so that path is preserved by construction rather than by
// coincidentally passing time.Now() through DetectRecurringAt, which would
// differ from MaxDate whenever the ledger's newest row is not dated today
// (the normal case for an imported CSV).
func recurringForProjectionDefault(ts *models.TransactionSet) []models.RecurringPayment {
	if ts == nil || ts.Len() == 0 {
		return nil
	}
	return insights.DetectRecurring(ts)
}
