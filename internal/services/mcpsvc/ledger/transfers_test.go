package ledger

import (
	"strings"
	"testing"
)

func TestGetTransfersReportsPairedAndExternal(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	// checking has the outbound leg of a paired transfer; schwab has the
	// inbound leg. The "usaa funds transfer" pattern backs the auto-pair.
	writeCSV(t, dir, "checking.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-05,GROCERY STORE,-42.10,\n"+
			"2026-08-10,USAA FUNDS TRANSFER,-500.00,\n")
	writeCSV(t, dir, "schwab.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-10,USAA FUNDS TRANSFER,500.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[getTransfersOutput](t, call(t, cs, "get_transfers", map[string]any{}))
	// The paired pair's two legs.
	if out.Count != 2 {
		t.Fatalf("count = %d, want 2 (the paired legs)", out.Count)
	}
	if out.PairedCount != 2 {
		t.Errorf("paired_count = %d, want 2 (one pair, two legs)", out.PairedCount)
	}
	// total_in / total_out net to zero for a balanced pair.
	if out.Net != 0 {
		t.Errorf("net = %.2f, want 0 for a balanced pair", out.Net)
	}
	// Each paired row reports its class, pair key, and counterparty.
	var sawCheckingOut, sawBrokerageIn bool
	for _, r := range out.Transfers {
		if r.Class != "paired" {
			t.Errorf("class = %q, want paired", r.Class)
		}
		if r.PairKey == "" {
			t.Error("paired transfer row has no pair_key")
		}
		if r.AccountID == "checking" {
			sawCheckingOut = true
			if r.Amount != -500 {
				t.Errorf("checking amount = %.2f, want -500", r.Amount)
			}
			if r.CounterpartyAccountID != "brokerage" {
				t.Errorf("checking counterparty = %q, want brokerage", r.CounterpartyAccountID)
			}
		}
		if r.AccountID == "brokerage" {
			sawBrokerageIn = true
			if r.Amount != 500 {
				t.Errorf("brokerage amount = %.2f, want 500", r.Amount)
			}
			if r.CounterpartyAccountID != "checking" {
				t.Errorf("brokerage counterparty = %q, want checking", r.CounterpartyAccountID)
			}
		}
	}
	if !sawCheckingOut {
		t.Error("did not see checking's outflow leg")
	}
	if !sawBrokerageIn {
		t.Error("did not see brokerage's inflow leg")
	}
}

func TestGetTransfersFiltersByAccount(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-10,USAA FUNDS TRANSFER,-500.00,\n")
	writeCSV(t, dir, "schwab.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-10,USAA FUNDS TRANSFER,500.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[getTransfersOutput](t, call(t, cs, "get_transfers", map[string]any{
		"account_id": "checking",
	}))
	if out.Count != 1 {
		t.Fatalf("count = %d, want 1 (checking's outflow leg only)", out.Count)
	}
	row := out.Transfers[0]
	if row.AccountID != "checking" {
		t.Errorf("account_id = %q, want checking", row.AccountID)
	}
	if row.Amount != -500 {
		t.Errorf("amount = %.2f, want -500 (signed outflow)", row.Amount)
	}
	if out.TotalOut != -500 {
		t.Errorf("total_out = %.2f, want -500", out.TotalOut)
	}
	if out.TotalIn != 0 {
		t.Errorf("total_in = %.2f, want 0", out.TotalIn)
	}
}

func TestGetTransfersFiltersByDateRange(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv",
		"Date,Description,Amount,Status\n"+
			"2026-07-15,USAA FUNDS TRANSFER,-300.00,\n"+
			"2026-08-10,USAA FUNDS TRANSFER,-500.00,\n")
	writeCSV(t, dir, "schwab.csv",
		"Date,Description,Amount,Status\n"+
			"2026-07-15,USAA FUNDS TRANSFER,300.00,\n"+
			"2026-08-10,USAA FUNDS TRANSFER,500.00,\n")
	cs := connect(t, deps)

	out := decodeToolResult[getTransfersOutput](t, call(t, cs, "get_transfers", map[string]any{
		"start_date": "2026-08-01",
		"end_date":   "2026-08-31",
	}))
	// Only the August pair's two legs.
	if out.Count != 2 {
		t.Fatalf("count = %d, want 2 (August legs only)", out.Count)
	}
	for _, r := range out.Transfers {
		if !strings.HasPrefix(r.Date, "2026-08") {
			t.Errorf("date = %q, want in August", r.Date)
		}
	}
}

func TestGetTransfersEmptyWhenNoTransfers(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv",
		"Date,Description,Amount,Status\n"+
			"2026-08-05,GROCERY STORE,-42.10,\n") // an expense, not a transfer
	cs := connect(t, deps)

	out := decodeToolResult[getTransfersOutput](t, call(t, cs, "get_transfers", map[string]any{}))
	if out.Count != 0 {
		t.Fatalf("count = %d, want 0 (no transfer rows)", out.Count)
	}
	if len(out.Transfers) != 0 {
		t.Errorf("transfers = %v, want nil or empty", out.Transfers)
	}
}

func TestGetTransfersRejectsUnknownAccountFilter(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, twoAccounts())
	writeCSV(t, dir, "checking.csv", checkingCSV())
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "get_transfers", map[string]any{
		"account_id": "nope",
	}))
	if !strings.Contains(msg, "no account with id") {
		t.Errorf("error = %q, want it to name the unknown account", msg)
	}
}
