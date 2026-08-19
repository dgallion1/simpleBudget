package ledger

import (
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
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
