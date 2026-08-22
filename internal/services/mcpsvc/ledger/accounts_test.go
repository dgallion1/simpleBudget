package ledger

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/insights"
	"budget2/internal/services/mcpsvc/confirm"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"
)

// twoAccounts builds a checking + brokerage pair with one anchor each. The
// FilePatterns match the CSV basenames the tests write, so the loader stamps
// AccountIDs and transfer pairing can match across accounts.
func twoAccounts() []models.Account {
	return []models.Account{
		{
			ID:                  "checking",
			Name:                "USAA Checking",
			Institution:         "USAA",
			Kind:                models.AccountKindChecking,
			FilePatterns:        []string{"checking"},
			LowBalanceThreshold: 500,
			Anchors: []models.BalanceAnchor{{
				Date:   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
				Amount: 1200,
			}},
		},
		{
			ID:                  "brokerage",
			Name:                "Schwab Brokerage",
			Institution:         "Schwab",
			Kind:                models.AccountKindBrokerage,
			FilePatterns:        []string{"schwab"},
			LowBalanceThreshold: 0, // uses default 500
			Anchors: []models.BalanceAnchor{{
				Date:   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
				Amount: 50000,
			}},
		},
	}
}

// a checking CSV with one outbound transfer leg and one expense.
func checkingCSV() string {
	return "Date,Description,Amount,Status\n" +
		"2026-08-05,GROCERY STORE,-42.10,\n" +
		"2026-08-10,USAA FUNDS TRANSFER,-500.00,\n"
}

func brokerageCSV() string {
	return "Date,Description,Amount,Status\n" +
		"2026-08-10,USAA FUNDS TRANSFER,500.00,\n"
}

func TestGetAccountsReportsBalanceFreshnessAndLowFlag(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv", checkingCSV())
	writeCSV(t, dir, "schwab.csv", brokerageCSV())
	cs := connect(t, deps)

	out := decodeToolResult[getAccountsOutput](t, call(t, cs, "get_accounts", map[string]any{}))
	if out.Count != 2 {
		t.Fatalf("count = %d, want 2", out.Count)
	}
	if len(out.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(out.Accounts))
	}
	// ID-sorted: brokerage first, then checking.
	broker := out.Accounts[0]
	check := out.Accounts[1]
	if broker.ID != "brokerage" {
		t.Fatalf("accounts[0] = %q, want brokerage", broker.ID)
	}
	if check.ID != "checking" {
		t.Fatalf("accounts[1] = %q, want checking", check.ID)
	}

	// Checking: 1200 anchor - 42.10 - 500 = 657.90, above the 500 threshold.
	if !check.Available {
		t.Error("checking balance unavailable; an anchor exists")
	}
	if check.Balance != 657.90 {
		t.Errorf("checking balance = %.2f, want 657.90", check.Balance)
	}
	if check.LowBalance {
		t.Error("checking flagged low; 657.90 >= 500 threshold")
	}
	if check.Threshold != 500 {
		t.Errorf("checking threshold = %.2f, want 500", check.Threshold)
	}
	if check.AnchorDate != "2026-07-31" {
		t.Errorf("checking anchor_date = %q, want 2026-07-31", check.AnchorDate)
	}
	if check.Freshness != "2026-08-10" {
		t.Errorf("checking freshness = %q, want 2026-08-10", check.Freshness)
	}

	// Brokerage: 50000 + 500 = 50500.
	if !broker.Available {
		t.Error("brokerage balance unavailable; an anchor exists")
	}
	if broker.Balance != 50500 {
		t.Errorf("brokerage balance = %.2f, want 50500", broker.Balance)
	}
	if broker.Threshold != 500 {
		t.Errorf("brokerage threshold = %.2f, want default 500", broker.Threshold)
	}
}

// The no-anchor trap: an account with no anchor MUST report available=false,
// not a zero balance. Reporting zero would let the model present an unknown
// balance as $0.
func TestGetAccountsReportsUnavailableNotZeroForNoAnchor(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
		// no anchors
	}})
	writeCSV(t, dir, "checking.csv", checkingCSV())
	cs := connect(t, deps)

	out := decodeToolResult[getAccountsOutput](t, call(t, cs, "get_accounts", map[string]any{}))
	if out.Count != 1 {
		t.Fatalf("count = %d, want 1", out.Count)
	}
	a := out.Accounts[0]
	if a.Available {
		t.Error("available = true for an account with no anchor; want false (unavailable, not $0)")
	}
	if a.Balance != 0 {
		t.Errorf("balance = %.2f, want 0 (unavailable reports zero amount but available=false)", a.Balance)
	}
	if a.LowBalance {
		t.Error("low_balance = true for an unavailable balance; an unknown balance is not \"low\"")
	}
	if a.AnchorDate != "" {
		t.Errorf("anchor_date = %q, want empty for unavailable", a.AnchorDate)
	}
}

func TestGetAccountsOnEmptyConfig(t *testing.T) {
	deps, _ := newDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[getAccountsOutput](t, call(t, cs, "get_accounts", map[string]any{}))
	if out.Count != 0 {
		t.Fatalf("count = %d, want 0 on an empty config", out.Count)
	}
	if len(out.Accounts) != 0 {
		t.Errorf("accounts = %v, want nil or empty", out.Accounts)
	}
}

// --- get_balance_projection ------------------------------------------------

func TestGetBalanceProjectionReportsCrossingAndTopUp(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv", checkingCSV())
	writeCSV(t, dir, "schwab.csv", brokerageCSV())
	cs := connect(t, deps)

	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "2026-08-10",
	}))
	if !out.Available {
		t.Fatal("projection unavailable; an anchor exists at 2026-07-31")
	}
	if out.AccountID != "checking" {
		t.Errorf("account_id = %q, want checking", out.AccountID)
	}
	if out.AsOf != "2026-08-10" {
		t.Errorf("as_of = %q, want 2026-08-10", out.AsOf)
	}
	if out.Threshold != 500 {
		t.Errorf("threshold = %.2f, want 500", out.Threshold)
	}
	// With no recurring items, the balance stays at 657.90 (1200 - 42.10 -
	// 500), above the 500 threshold, so there is no crossing and no top-up.
	if out.Crossing != "" {
		t.Errorf("crossing = %q, want empty (balance stays above threshold)", out.Crossing)
	}
	if out.SuggestedTopUp != 0 {
		t.Errorf("suggested_top_up = %.2f, want 0 (no crossing)", out.SuggestedTopUp)
	}
	if out.Minimum != 657.90 {
		t.Errorf("minimum = %.2f, want 657.90", out.Minimum)
	}
	if out.HasReference {
		t.Error("has_reference = true; checking has no inbound paired transfers")
	}
}

func TestGetBalanceProjectionReportsUnavailableForNoAnchor(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})
	writeCSV(t, dir, "checking.csv", checkingCSV())
	cs := connect(t, deps)

	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "2026-08-10",
	}))
	if out.Available {
		t.Error("available = true for an account with no anchor; want false (cannot project without a starting balance)")
	}
	if out.AsOf != "" {
		t.Errorf("as_of = %q, want empty when unavailable", out.AsOf)
	}
	if out.Crossing != "" {
		t.Errorf("crossing = %q, want empty when unavailable", out.Crossing)
	}
}

func TestGetBalanceProjectionRejectsUnknownAccount(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv", checkingCSV())
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "nope",
	}))
	if !strings.Contains(msg, "no account with id") {
		t.Errorf("error = %q, want it to name the unknown account", msg)
	}
}

func TestGetBalanceProjectionRejectsBadDate(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv", checkingCSV())
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "not-a-date",
	}))
	if !strings.Contains(msg, "not a valid date") {
		t.Errorf("error = %q, want it to name the bad date", msg)
	}
}

// --- Suppressed duplicates are not double-counted (R3) ---------------------

// newDepsCapturingLoader is newDeps, but also returns the concrete
// *dataloader.DataLoader so a test can call LoadData/SaveDuplicateDecision
// directly to set up a resolved near-duplicate pair before exercising the
// tools through the MCP client.
func newDepsCapturingLoader(t *testing.T) (Deps, string, *dataloader.DataLoader) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	deps := Deps{
		Transactions: loader,
		Accounts:     NewAccountStore(store),
		Transfers:    loader,
		Store:        store,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
		Confirm:      confirm.NewRegistry(5 * time.Minute),
	}
	return deps, dir, loader
}

// resolveDuplicatePair loads the ledger to discover the one candidate pair
// it expects to find, saves a kept_winner decision suppressing the bill-pay
// side, and fails the test if the pair (or exactly one) isn't found.
func resolveDuplicatePair(t *testing.T, loader *dataloader.DataLoader) {
	t.Helper()
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData (discovery): %v", err)
	}
	var pairKey, billHash, checkHash string
	for _, tr := range ts.Transactions {
		if strings.HasPrefix(strings.ToLower(tr.Description), "check") {
			checkHash = tr.Hash
		} else {
			billHash = tr.Hash
		}
		if tr.DuplicatePairKey != "" {
			pairKey = tr.DuplicatePairKey
		}
	}
	if pairKey == "" || billHash == "" || checkHash == "" {
		t.Fatalf("setup: did not detect the expected candidate pair (pairKey=%q bill=%q check=%q)", pairKey, billHash, checkHash)
	}
	if err := loader.SaveDuplicateDecision(pairKey, dataloader.DuplicateDecision{
		KeptHash:       checkHash,
		SuppressedHash: billHash,
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
	}); err != nil {
		t.Fatalf("SaveDuplicateDecision: %v", err)
	}
}

// duplicatePairAccount is a single checking account with a $2200 anchor and
// the default $500 threshold, used by the R3 fixtures below.
func duplicatePairAccount() models.Account {
	return models.Account{
		ID:                  "checking",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"checking"},
		LowBalanceThreshold: 500,
		Anchors: []models.BalanceAnchor{{
			Date:   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			Amount: 2200,
		}},
	}
}

// duplicatePairCSVs writes a bill-pay row and its matching posted-check row
// as two files (near_duplicates.go's candidate-pair heuristic): same
// amount, within the 7-day window, one description matching "Check #...".
// Single-counting the $900 debit leaves the balance at 1300 (above the 500
// threshold); double-counting it (the bug this test guards against) would
// leave it at 400 (below the threshold).
func duplicatePairCSVs(t *testing.T, dir string) {
	t.Helper()
	writeCSV(t, dir, "checking-a.csv", "Date,Description,Amount,Status\n"+
		"2026-08-01,Lucid,-900.00,Scheduled Bill Pay\n")
	writeCSV(t, dir, "checking-b.csv", "Date,Description,Amount,Status\n"+
		"2026-08-02,Check #55501,-900.00,Posted\n")
}

// TestGetAccountsCountsSuppressedDuplicateOnce is acceptance criterion 1 for
// get_accounts: a resolved duplicate pair's debit must be counted exactly
// once in the balance and the low_balance flag. Before R3 the tool read
// ts.Transactions (the raw, unfiltered set) instead of ts.Active(), so a
// resolved duplicate was still counted twice.
func TestGetAccountsCountsSuppressedDuplicateOnce(t *testing.T) {
	deps, dir, loader := newDepsCapturingLoader(t)
	seedAccounts(t, deps, []models.Account{duplicatePairAccount()})
	duplicatePairCSVs(t, dir)
	resolveDuplicatePair(t, loader)

	cs := connect(t, deps)
	out := decodeToolResult[getAccountsOutput](t, call(t, cs, "get_accounts", map[string]any{}))
	if out.Count != 1 {
		t.Fatalf("count = %d, want 1", out.Count)
	}
	a := out.Accounts[0]
	if !a.Available {
		t.Fatal("balance unavailable; an anchor exists")
	}
	if a.Balance != 1300 {
		t.Errorf("balance = %.2f, want 1300.00 (2200 - 900, debit counted once); a double-counted debit would report 400.00", a.Balance)
	}
	if a.LowBalance {
		t.Error("low_balance = true; 1300 >= the 500 threshold. A double-counted debit (400) would incorrectly trip this flag")
	}
}

// TestGetBalanceProjectionCountsSuppressedDuplicateOnce is acceptance
// criterion 1 for get_balance_projection: the funding projection must count
// a resolved duplicate's debit exactly once. Before R3 the tool read
// ts.Transactions raw instead of ts.Active().
func TestGetBalanceProjectionCountsSuppressedDuplicateOnce(t *testing.T) {
	deps, dir, loader := newDepsCapturingLoader(t)
	seedAccounts(t, deps, []models.Account{duplicatePairAccount()})
	duplicatePairCSVs(t, dir)
	resolveDuplicatePair(t, loader)

	cs := connect(t, deps)
	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "2026-08-10",
	}))
	if !out.Available {
		t.Fatal("projection unavailable; an anchor exists")
	}
	// No recurring items are detected (only one active transaction remains
	// after suppression), so the projected balance stays flat at 1300
	// across the window: above the 500 threshold, no crossing, no top-up.
	// A double-counted debit would put the balance at 400, below the
	// threshold, and wrongly report a crossing and a top-up.
	if out.Minimum != 1300 {
		t.Errorf("minimum = %.2f, want 1300.00 (2200 - 900, debit counted once); a double-counted debit would report 400.00", out.Minimum)
	}
	if out.Crossing != "" {
		t.Errorf("crossing = %q, want empty; a double-counted debit would wrongly cross the 500 threshold", out.Crossing)
	}
	if out.SuggestedTopUp != 0 {
		t.Errorf("suggested_top_up = %.2f, want 0; a double-counted debit would wrongly suggest a top-up", out.SuggestedTopUp)
	}
}

// --- R5: get_balance_projection must honour as_of for recurring detection --

// r5RecurringAccount is a single checking account used by the R5 fixtures
// below: a $3,700 anchor the day before the ledger starts, and the default
// $500 threshold.
func r5RecurringAccount() models.Account {
	return models.Account{
		ID:                  "checking",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"checking"},
		LowBalanceThreshold: 500,
		Anchors: []models.BalanceAnchor{{
			Date:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
			Amount: 3700,
		}},
	}
}

// TestGetBalanceProjectionHistoricalAsOfSchedulesFromPastEvidenceOnly is
// acceptance criterion 1 for R5: a historical as_of must schedule recurrence
// from evidence at or before that date only, and CAN schedule inside its own
// window.
//
// Fixture: three genuine monthly $900 "Rent" debits (Jan-Mar 2026) -- enough
// for strict monthly detection -- plus an unrelated one-off transaction five
// months later (2026-08-15) that exists purely to push the ledger's actual
// latest date far past Rent's last occurrence. as_of is historical
// (2026-04-01), 31 days after Rent's last occurrence -- well inside the
// 90-day monthly freshness window measured against as_of.
//
// Before this fix, recurringForProjection called insights.DetectRecurring,
// which (per its doc comment) falls back to the WHOLE active set's own
// MaxDate (2026-08-15, from the unrelated August row) as both the history
// truncation and the freshness "now". Measured against 2026-08-15, Rent's
// last occurrence is 167 days stale (167 > the 90-day monthly window), so
// the old code detected NO recurring items at all here and scheduled
// nothing: the balance would stay flat at 1000 and never cross the 500
// threshold. Fixed, DetectRecurringAt(ts, asOf) measures freshness against
// the requested 2026-04-01 (31 days stale, well inside the window), detects
// Rent, and schedules its next $900 occurrence inside the 35-day window --
// producing a real crossing this fixture would not otherwise have.
func TestGetBalanceProjectionHistoricalAsOfSchedulesFromPastEvidenceOnly(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{r5RecurringAccount()})
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n"+
		"2026-01-01,Rent,-900.00,\n"+
		"2026-02-01,Rent,-900.00,\n"+
		"2026-03-01,Rent,-900.00,\n"+
		"2026-08-15,Misc Fee,-20.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "2026-04-01",
	}))
	if !out.Available {
		t.Fatal("projection unavailable; an anchor exists at 2025-12-31")
	}
	if out.AsOf != "2026-04-01" {
		t.Errorf("as_of = %q, want 2026-04-01", out.AsOf)
	}
	// Starting balance at 2026-04-01: 3700 - 900*3 (Jan/Feb/Mar Rent) = 1000.
	// The unrelated 2026-08-15 row is after as_of and must not count.
	// Rent's next $900 occurrence (NextExpected 2026-04-01, rolled forward
	// past as_of at the engine's fixed 30-day monthly step) lands at
	// 2026-05-01, inside the 35-day window, dropping the balance to 100 --
	// below the 500 threshold. A non-empty schedule (this crossing) is
	// exactly what the old code failed to produce here.
	if out.Crossing != "2026-05-01" {
		t.Errorf("crossing = %q, want 2026-05-01 (the old code, using the ledger's actual max date for freshness, scheduled nothing here)", out.Crossing)
	}
	if out.Minimum != 100 {
		t.Errorf("minimum = %.2f, want 100.00", out.Minimum)
	}
	if out.SuggestedTopUp != 400 {
		t.Errorf("suggested_top_up = %.2f, want 400.00 (500 - 100, already a multiple of 100)", out.SuggestedTopUp)
	}
}

// TestGetBalanceProjectionFutureAsOfEvaluatesFreshnessAgainstRequestedDate
// is acceptance criterion 2 for R5: a future as_of must evaluate recurrence
// freshness against the REQUESTED date, not the ledger's actual maximum.
//
// Fixture: the same three monthly $900 "Rent" debits (Jan-Mar 2026), with
// no later transactions at all -- so the ledger's actual max date IS Rent's
// own last occurrence (2026-03-01). as_of is far in the future (2026-08-01),
// 153 days after Rent's last occurrence -- well outside the 90-day monthly
// freshness window measured against as_of.
//
// Before this fix, DetectRecurring measured freshness against the ledger's
// own MaxDate, which here equals Rent's last occurrence itself (0 days
// stale) -- so the old code would judge Rent "fresh" and keep scheduling it
// indefinitely into any future as_of, regardless of how stale the series
// truly is relative to the requested date. Fixed, DetectRecurringAt(ts,
// asOf) measures the 153-day gap against the requested 2026-08-01 and
// correctly excludes Rent as no longer active: the balance stays flat and
// never crosses.
func TestGetBalanceProjectionFutureAsOfEvaluatesFreshnessAgainstRequestedDate(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{r5RecurringAccount()})
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n"+
		"2026-01-01,Rent,-900.00,\n"+
		"2026-02-01,Rent,-900.00,\n"+
		"2026-03-01,Rent,-900.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      "2026-08-01",
	}))
	if !out.Available {
		t.Fatal("projection unavailable; an anchor exists at 2025-12-31")
	}
	if out.AsOf != "2026-08-01" {
		t.Errorf("as_of = %q, want 2026-08-01", out.AsOf)
	}
	// Starting balance at 2026-08-01: 3700 - 900*3 = 1000. Rent is stale
	// (153 days since its last occurrence) relative to the requested
	// 2026-08-01, so it must not be scheduled: the balance stays flat.
	if out.Crossing != "" {
		t.Errorf("crossing = %q, want empty; Rent is 153 days stale relative to the requested as_of and must not be scheduled (the old code, judging freshness against its own last occurrence, would wrongly keep it active)", out.Crossing)
	}
	if out.Minimum != 1000 {
		t.Errorf("minimum = %.2f, want 1000.00 (flat -- no recurring item applies)", out.Minimum)
	}
	if out.SuggestedTopUp != 0 {
		t.Errorf("suggested_top_up = %.2f, want 0 (no crossing)", out.SuggestedTopUp)
	}
}

// TestGetBalanceProjectionDefaultAsOfUnchanged is acceptance criterion 3 for
// R5: with no as_of supplied, the output must be unchanged from current
// behaviour. The fixture deliberately has NO merchant with 3+ occurrences,
// so insights.DetectRecurring/DetectRecurringAt both return nil regardless
// of which reference date freshness is measured against -- this pins the
// default (no as_of, asOf defaults to time.Now()) path so it cannot drift,
// independent of what day the suite happens to run.
func TestGetBalanceProjectionDefaultAsOfUnchanged(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:                  "checking",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"checking"},
		LowBalanceThreshold: 500,
		Anchors: []models.BalanceAnchor{{
			Date:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			Amount: 1000,
		}},
	}})
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n"+
		"2020-02-01,Coffee Shop,-20.00,\n"+
		"2020-03-01,Groceries,-50.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
	}))
	wantAsOf := time.Now().Format("2006-01-02")
	if !out.Available {
		t.Fatal("projection unavailable; an anchor exists at 2020-01-01")
	}
	if out.AsOf != wantAsOf {
		t.Errorf("as_of = %q, want today (%q) when as_of is not supplied", out.AsOf, wantAsOf)
	}
	if out.Threshold != 500 {
		t.Errorf("threshold = %.2f, want 500", out.Threshold)
	}
	// 1000 anchor - 20 - 50 = 930, and stays flat: no recurring merchant has
	// 3+ occurrences, so nothing is scheduled.
	if out.Minimum != 930 {
		t.Errorf("minimum = %.2f, want 930.00", out.Minimum)
	}
	if out.Crossing != "" {
		t.Errorf("crossing = %q, want empty (930 stays above the 500 threshold)", out.Crossing)
	}
	if out.SuggestedTopUp != 0 {
		t.Errorf("suggested_top_up = %.2f, want 0", out.SuggestedTopUp)
	}
	if out.HasReference {
		t.Error("has_reference = true; no paired transfers exist in this fixture")
	}
}

// r5ReferenceTransferAccounts returns a checking + brokerage pair used by the
// R5 attempt-2 reference-amount fixtures below. The anchor sits 90 days
// before "now" so it is at or before every transaction date the fixtures
// write, regardless of which TZ the test process runs under.
func r5ReferenceTransferAccounts(anchorDate time.Time) []models.Account {
	anchor := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
	return []models.Account{
		{
			ID:                  "checking",
			Name:                "USAA Checking",
			Institution:         "USAA",
			Kind:                models.AccountKindChecking,
			FilePatterns:        []string{"checking"},
			LowBalanceThreshold: 500,
			Anchors:             []models.BalanceAnchor{{Date: anchor, Amount: 5000}},
		},
		{
			ID:           "brokerage",
			Name:         "Schwab Brokerage",
			Institution:  "Schwab",
			Kind:         models.AccountKindBrokerage,
			FilePatterns: []string{"schwab"},
			Anchors:      []models.BalanceAnchor{{Date: anchor, Amount: 50000}},
		},
	}
}

// TestGetBalanceProjectionDefaultAsOfDoesNotTruncateReferenceAmount is
// evidence-requirement 1 for R5 attempt 2: a default-path (no as_of) fixture
// that actually exercises the reference-amount difference attempt 1 missed.
// Attempt 1's own pin (TestGetBalanceProjectionDefaultAsOfUnchanged, above)
// has no transfers at all, so it could not catch attempt 1 silently routing
// the default path through the same as_of truncation used for an explicit
// as_of.
//
// Fixture: three confirmed inbound paired transfers into "checking" --
// 100.00 and 200.00 dated in the past, and 900.00 dated ten days from now.
// medianInboundPairedTransfer (projection.go) applies no date filter of its
// own, so whatever set reaches it is its only filter.
//
//   - Correct (this fix): the default path takes the pre-R5 path verbatim --
//     no truncation -- so all three legs are visible and the median of
//     100/200/900 is 200.00.
//   - Attempt 1's bug: routing the default path through
//     all.FilterByDateRange(all.MinDate(), time.Now()) silently drops the
//     future 900.00 leg, leaving a median of 100/200 = 150.00.
func TestGetBalanceProjectionDefaultAsOfDoesNotTruncateReferenceAmount(t *testing.T) {
	deps, dir := newDeps(t)
	now := time.Now()
	past1 := now.AddDate(0, 0, -60)
	past2 := now.AddDate(0, 0, -30)
	future := now.AddDate(0, 0, 10)

	seedAccounts(t, deps, r5ReferenceTransferAccounts(now.AddDate(0, 0, -90)))

	fmtDate := func(d time.Time) string { return d.Format("2006-01-02") }
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n"+
		fmtDate(past1)+",USAA FUNDS TRANSFER,100.00,\n"+
		fmtDate(past2)+",USAA FUNDS TRANSFER,200.00,\n"+
		fmtDate(future)+",USAA FUNDS TRANSFER,900.00,\n")
	writeCSV(t, dir, "schwab.csv", "Date,Description,Amount,Status\n"+
		fmtDate(past1)+",USAA FUNDS TRANSFER,-100.00,\n"+
		fmtDate(past2)+",USAA FUNDS TRANSFER,-200.00,\n"+
		fmtDate(future)+",USAA FUNDS TRANSFER,-900.00,\n")

	cs := connect(t, deps)
	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
	}))

	if !out.HasReference {
		t.Fatal("has_reference = false, want true: three confirmed inbound paired transfers exist")
	}
	if out.ReferenceAmount != 200.00 {
		t.Errorf("reference_amount = %.2f, want 200.00 (median of the untruncated 100/200/900; "+
			"a default path that truncates at time.Now() would wrongly report 150.00, dropping the "+
			"future-dated 900.00 leg)", out.ReferenceAmount)
	}
}

// TestGetBalanceProjectionDefaultAsOfReferenceAmountStableAcrossTimezones is
// evidence-requirement 2 for R5 attempt 2: it pins the today/tomorrow edge
// and must produce the SAME result regardless of the process's local
// timezone.
//
// Fixture: two confirmed inbound paired transfers dated in the past, plus a
// third dated exactly tomorrow (Local calendar date, "now"+1 day) -- the
// tightest possible edge for a time.Now()-based truncation bug, since
// "tomorrow" in one timezone can already be "today" in another at the same
// instant.
//
//   - Correct (this fix): the default path never truncates, so all three
//     legs count and the median is 200.00 in every timezone.
//   - Attempt 1's bug: FilterByDateRange built its end boundary in
//     time.Now()'s Location (Local) while tx.Date is UTC, so whether the
//     "tomorrow" leg fell inside the truncation window depended on the
//     process's local offset from UTC at the moment the test ran --
//     observed as reference_amount 200.00 under America/New_York and 150.00
//     under Asia/Tokyo for this same fixture shape.
//
// Run this test under at least two TZ values, e.g.:
//
//	TZ=America/New_York go test ./internal/services/mcpsvc/ledger/... -run TestGetBalanceProjectionDefaultAsOfReferenceAmountStableAcrossTimezones -v
//	TZ=Asia/Tokyo        go test ./internal/services/mcpsvc/ledger/... -run TestGetBalanceProjectionDefaultAsOfReferenceAmountStableAcrossTimezones -v
//
// Both must report reference_amount = 200.00.
func TestGetBalanceProjectionDefaultAsOfReferenceAmountStableAcrossTimezones(t *testing.T) {
	deps, dir := newDeps(t)
	now := time.Now()
	past1 := now.AddDate(0, 0, -60)
	past2 := now.AddDate(0, 0, -30)
	tomorrow := now.AddDate(0, 0, 1)

	seedAccounts(t, deps, r5ReferenceTransferAccounts(now.AddDate(0, 0, -90)))

	fmtDate := func(d time.Time) string { return d.Format("2006-01-02") }
	writeCSV(t, dir, "checking.csv", "Date,Description,Amount,Status\n"+
		fmtDate(past1)+",USAA FUNDS TRANSFER,100.00,\n"+
		fmtDate(past2)+",USAA FUNDS TRANSFER,200.00,\n"+
		fmtDate(tomorrow)+",USAA FUNDS TRANSFER,900.00,\n")
	writeCSV(t, dir, "schwab.csv", "Date,Description,Amount,Status\n"+
		fmtDate(past1)+",USAA FUNDS TRANSFER,-100.00,\n"+
		fmtDate(past2)+",USAA FUNDS TRANSFER,-200.00,\n"+
		fmtDate(tomorrow)+",USAA FUNDS TRANSFER,-900.00,\n")

	cs := connect(t, deps)
	out := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
	}))

	if !out.HasReference {
		t.Fatalf("has_reference = false (TZ=%s), want true", time.Local.String())
	}
	if out.ReferenceAmount != 200.00 {
		t.Errorf("reference_amount = %.2f (TZ=%s), want 200.00 in every timezone -- a TZ-dependent "+
			"result means the default path is truncating again and the truncation boundary disagrees "+
			"with BalanceAt's", out.ReferenceAmount, time.Local.String())
	}
}

// TestActiveSetAndRecurringForProjectionDefaultMeasuresFreshnessAgainstLedgerMaxDate
// is the remaining piece of evidence-requirement 1 for R5 attempt 2: it
// isolates bug 1 (the default path's recurring reference date) at the unit
// level, directly against activeSetAndRecurringForProjection -- the exact
// branch point get_balance_projection wires in -- rather than through the
// full round trip.
//
// The round trip cannot observe this bug on the default path: ledger task
// R12 (explicitly out of scope here -- see the R5 brief) means a
// time.Now()-derived as_of never schedules ANY recurring occurrence at all
// (accounts.Project's byDay map keys are time.Time values built from
// different Locations -- UTC from tx.Date, Local from time.Now() -- so the
// lookup never hits, regardless of whether detection included the series).
// Testing through get_balance_projection's Crossing/Minimum fields would
// therefore pass or fail on R12's bug, not on this one -- exactly the
// confound the brief warns against. Calling activeSetAndRecurringForProjection
// directly sidesteps accounts.Project entirely and tests only what R5
// attempt 2 changed: which reference date reaches insights.DetectRecurring*,
// INCLUDING the wiring decision of which branch runs (not just the two
// functions the branches dispatch to).
//
// Fixture: three genuine monthly $900 "Rent" debits spaced exactly 30 days
// apart, the LAST one 210 days before "now" -- and nothing newer in the
// ledger, so the ledger's actual MaxDate equals that last occurrence.
//
//   - Correct (this fix): the as_of-absent branch calls
//     recurringForProjectionDefault, i.e. insights.DetectRecurring(ts), whose
//     zero reference date falls back to ts's own MaxDate (Rent's last
//     occurrence itself) -- 0 days stale, well inside the 90-day monthly
//     window -- so Rent IS detected.
//   - Attempt 1's bug: routing the same ts through
//     insights.DetectRecurringAt(ts, time.Now()) judges freshness against
//     "now", 210 days after Rent's last occurrence -- outside the 90-day
//     window -- so Rent is wrongly excluded.
func TestActiveSetAndRecurringForProjectionDefaultMeasuresFreshnessAgainstLedgerMaxDate(t *testing.T) {
	now := time.Now()
	last := now.AddDate(0, 0, -210)
	mid := now.AddDate(0, 0, -240)
	first := now.AddDate(0, 0, -270)

	mkRent := func(d time.Time) models.Transaction {
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		return models.Transaction{
			Date: day, Description: "Rent", Amount: -900.00, AccountID: "checking",
			TransactionType: models.Outflow,
		}
	}
	ts := models.NewTransactionSet([]models.Transaction{mkRent(first), mkRent(mid), mkRent(last)})

	_, got := activeSetAndRecurringForProjection(ts, now, false /* asOfSupplied */)
	if len(got) != 1 {
		t.Fatalf("activeSetAndRecurringForProjection(ts, now, false): got %d recurring payments, want 1 "+
			"(Rent, fresh relative to the ledger's own MaxDate)", len(got))
	}
	if got[0].Description != "rent" {
		t.Errorf("recurring[0].Description = %q, want \"rent\"", got[0].Description)
	}

	// The bug attempt 1 introduced, isolated: threading time.Now() through
	// DetectRecurringAt instead of the MaxDate-fallback shorthand wrongly
	// excludes the same series as stale.
	buggy := insights.DetectRecurringAt(ts, now)
	if len(buggy) != 0 {
		t.Fatalf("setup check failed: insights.DetectRecurringAt(ts, time.Now()) returned %d payments, "+
			"want 0 -- this fixture is supposed to demonstrate the two reference dates disagree", len(buggy))
	}
}
