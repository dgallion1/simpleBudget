package ledger

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/handlers/dashboard"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// parityAccount is the single checking account the R3 dashboard/MCP parity
// fixture below uses: a $4,600 anchor the day before the ledger starts, and
// the default-ish $500 threshold.
func parityAccount() models.Account {
	return models.Account{
		ID:                  "checking",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"checking"},
		LowBalanceThreshold: 500,
		Anchors: []models.BalanceAnchor{{
			Date:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			Amount: 4600,
		}},
	}
}

// resolveTaggedDuplicatePair is resolveDuplicatePair, but keys off the
// DuplicatePairKey the near-duplicate detector actually tags onto the two
// members of the detected pair, rather than "the last non-check row seen" --
// resolveDuplicatePair's approach only works when the fixture has exactly
// one non-check row. The parity fixture below has several, so this variant
// is needed to pick out the correct bill-pay leg.
func resolveTaggedDuplicatePair(t *testing.T, loader *dataloader.DataLoader) {
	t.Helper()
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData (discovery): %v", err)
	}
	var pairKey, billHash, checkHash string
	for _, tr := range ts.Transactions {
		if tr.DuplicatePairKey == "" {
			continue
		}
		pairKey = tr.DuplicatePairKey
		if strings.HasPrefix(strings.ToLower(tr.Description), "check") {
			checkHash = tr.Hash
		} else {
			billHash = tr.Hash
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

// TestDashboardAndMCPProjectionAgree is acceptance criterion 2 for R3
// attempt 2: the dashboard and MCP paths must return the same funding
// projection for the same data, including when a resolved duplicate sits
// inside the recurring series that drives it. It also re-covers criterion
// 3 (balance, low-balance, and funding-projection each count the debit
// exactly once) through the real dataloader/duplicate-decision pipeline,
// rather than a hand-built TransactionSet, on both paths at once.
//
// Fixture: four monthly $900 "Lucid" bill-pay debits (Feb-May), and the May
// occurrence has a matching posted-check row one day later for the same
// $950 that gets resolved as a duplicate (bill-pay side suppressed, check
// side kept) -- the real workflow near_duplicates.go recognizes. After
// resolution, three genuine $900 occurrences remain (Feb/Mar/Apr), which is
// enough for the recurring engine's strict-monthly detection (minimum 3) to
// fire; the fourth (suppressed) row must not extend or otherwise skew that
// series.
//
// Verified independently before writing this test: reverting the R3
// attempt-2 fix (detectRecurringForDashboard filtering to ts.Active())
// changes the dashboard's rendered crossing date from Jun 1 to May 31
// while the MCP path (already fixed in attempt 1) stays at Jun 1 -- the two
// paths disagree exactly as the amendment describes. This test pins
// agreement and would fail against that regression.
func TestDashboardAndMCPProjectionAgree(t *testing.T) {
	deps, dir, loader := newDepsCapturingLoader(t)
	seedAccounts(t, deps, []models.Account{parityAccount()})

	writeCSV(t, dir, "checking-a.csv", "Date,Description,Amount,Status\n"+
		"2026-02-01,Lucid,-900.00,Scheduled Bill Pay\n"+
		"2026-03-01,Lucid,-900.00,Scheduled Bill Pay\n"+
		"2026-04-01,Lucid,-900.00,Scheduled Bill Pay\n"+
		"2026-05-01,Lucid,-950.00,Scheduled Bill Pay\n")
	writeCSV(t, dir, "checking-b.csv", "Date,Description,Amount,Status\n"+
		"2026-05-02,Check #55501,-950.00,Posted\n")

	resolveTaggedDuplicatePair(t, loader)

	asOf := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)

	// --- MCP side ---
	cs := connect(t, deps)
	balOut := decodeToolResult[balanceProjectionOutput](t, call(t, cs, "get_balance_projection", map[string]any{
		"account_id": "checking",
		"as_of":      asOf.Format("2006-01-02"),
	}))
	if !balOut.Available {
		t.Fatal("MCP projection unavailable; an anchor exists")
	}
	acctOut := decodeToolResult[getAccountsOutput](t, call(t, cs, "get_accounts", map[string]any{}))
	if acctOut.Count != 1 {
		t.Fatalf("MCP get_accounts count = %d, want 1", acctOut.Count)
	}
	mcpAccount := acctOut.Accounts[0]

	// Balance and low-balance: the debit is counted exactly once (2026-05-02
	// AnchorDate 4600, minus three $900 debits, minus the kept $950 check =
	// 950; above the $500 threshold. Double-counting the suppressed row
	// would report 0.00 and trip low_balance).
	if mcpAccount.Balance != 950.00 {
		t.Errorf("MCP balance = %.2f, want 950.00", mcpAccount.Balance)
	}
	if mcpAccount.LowBalance {
		t.Error("MCP low_balance = true, want false (950.00 is above the 500 threshold)")
	}

	// Funding projection: three genuine monthly occurrences remain after
	// suppression, correctly detected, giving a real Jun 1 crossing.
	if balOut.Crossing != "2026-06-01" {
		t.Errorf("MCP crossing = %q, want \"2026-06-01\"", balOut.Crossing)
	}
	if balOut.Minimum != 50.00 {
		t.Errorf("MCP minimum = %.2f, want 50.00", balOut.Minimum)
	}
	if balOut.SuggestedTopUp != 500.00 {
		t.Errorf("MCP suggested_top_up = %.2f, want 500.00", balOut.SuggestedTopUp)
	}

	// --- Dashboard side, same store/loader ---
	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}
	dashStore, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dashboard.Initialize(loader, rend, nil, dashStore)
	r := chi.NewRouter()
	dashboard.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "$950.00") {
		t.Errorf("dashboard must show the balance counted once ($950.00); body:\n%s", body)
	}
	if strings.Contains(body, "low-balance threshold") {
		t.Errorf("dashboard must not show the low-balance warning (950.00 is above the 500 threshold); body:\n%s", body)
	}
	if !strings.Contains(body, "Drops below $500 around Jun 1, 2026") {
		t.Errorf("dashboard crossing must agree with the MCP path (Jun 1, 2026); body:\n%s", body)
	}
	if !strings.Contains(body, "Suggested top-up: $500.") {
		t.Errorf("dashboard suggested top-up must agree with the MCP path ($500); body:\n%s", body)
	}
}
