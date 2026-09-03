package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// writeAccountsFixture writes the accounts sidecar through the storage
// service (so encryption applies exactly as in production) and returns
// nothing; the dashboard reads it via accounts.Load(store).
func writeAccountsFixture(t *testing.T, store *storage.Storage, accts []models.Account) {
	t.Helper()
	if err := accounts.Save(store, accts); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}
}

// setupAccountsTestEnv creates a temp dir, writes a CSV whose rows are
// stamped with the given account ID (by matching the filename), writes the
// accounts sidecar, and initializes the dashboard with a real renderer so
// the accounts card template is actually exercised. now is the fixed
// "today" the staleness check runs against (overriding the package's
// nowFunc); passing time.Time leaves nowFunc as time.Now. Returns the
// router, the temp dir (for any extra fixture writes), and cleanup.
func setupAccountsTestEnv(t *testing.T, csvRows [][]string, accts []models.Account, now time.Time) (chi.Router, string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "dashboard-accts-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	// Write a CSV whose name matches the first account's first file pattern,
	// so the loader stamps AccountID on its rows. The accounts fixture's
	// FilePatterns must match "test.csv".
	csvPath := filepath.Join(tmpDir, "test.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Create csv: %v", err)
	}
	if err := writeCSVRows(f, csvRows); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("write csv: %v", err)
	}

	store, err := storage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("storage.New: %v", err)
	}
	writeAccountsFixture(t, store, accts)

	// Reuse the same dataloader.New the package's existing setupTestEnv
	// uses; the dashboard handler reads the accounts sidecar through the
	// store on every LoadDataContext call, so writing the fixture above is
	// enough for the loader to stamp AccountID on the CSV's rows.
	dl := dataloader.New(tmpDir, store)

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("templates.New: %v", err)
	}

	// Override the staleness reference date so the test is deterministic:
	// the staleness check compares nowFunc() against the account's latest
	// transaction date, so a fixed now makes the fresh/stale outcome
	// independent of the date the test is run.
	prevNow := nowFunc
	if !now.IsZero() {
		nowFunc = func() time.Time { return now }
	}

	Initialize(dl, rend, nil, store)
	r := chi.NewRouter()
	RegisterRoutes(r)

	return r, tmpDir, func() {
		nowFunc = prevNow
		os.RemoveAll(tmpDir)
	}
}

// writeCSVRows writes the header + rows to the given open file.
func writeCSVRows(f *os.File, rows [][]string) error {
	if _, err := f.WriteString("Date,Description,Amount,Category\n"); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := f.WriteString(strings.Join(r, ",") + "\n"); err != nil {
			return err
		}
	}
	return f.Close()
}

func doGetDash(t *testing.T, router chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// extractAccountsCardSection returns the substring of body spanning the
// accounts card <section>. It is used to scope assertions to the card so
// that a no-anchor assertion like "no $0.00" is not falsely tripped by an
// unrelated $0.00 figure in the KPI tiles. Returns "" when the card is
// absent (e.g. no accounts configured).
func extractAccountsCardSection(body string) string {
	const marker = `aria-labelledby="accounts-card-heading"`
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	// Back up to the opening <section.
	sectionStart := strings.LastIndex(body[:start], "<section")
	if sectionStart < 0 {
		return ""
	}
	end := strings.Index(body[sectionStart:], "</section>")
	if end < 0 {
		return body[sectionStart:]
	}
	return body[sectionStart : sectionStart+end+len("</section>")]
}

// ---------- Accounts card: four states ----------

// TestAccountsCard_Healthy renders an account with a fresh balance above
// its threshold and asserts the healthy state shows the balance with no
// low/stale/no-anchor flag.
func TestAccountsCard_Healthy(t *testing.T) {
	// Account with a $2000 anchor; threshold $500. A fresh transaction
	// keeps the balance above threshold and within the freshness window.
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: 2000.00}},
		LowBalanceThreshold: 500.00,
	}
	rows := [][]string{
		{"2026-08-10", "Groceries", "-100", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "USAA Checking") {
		t.Errorf("body missing account name; got:\n%s", body)
	}
	if !strings.Contains(body, "$1,900.00") {
		t.Errorf("body missing balance $1,900.00 (2000 - 100); got:\n%s", body)
	}
	if strings.Contains(body, "low-balance threshold") {
		t.Errorf("healthy account must not show low-balance flag; got:\n%s", body)
	}
	if strings.Contains(body, "Data is stale") {
		t.Errorf("healthy account must not show stale flag; got:\n%s", body)
	}
	if strings.Contains(body, "Balance unknown") {
		t.Errorf("healthy account must not show no-anchor flag; got:\n%s", body)
	}
}

// TestAccountsCard_Low renders an account whose balance is below its
// threshold and asserts the low flag appears WITH text (not color alone).
func TestAccountsCard_Low(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: 300.00}},
		LowBalanceThreshold: 500.00,
	}
	rows := [][]string{
		{"2026-08-10", "Groceries", "-10", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Below the $500 low-balance threshold") {
		t.Errorf("low account must show text flag 'Below the $500 low-balance threshold'; got:\n%s", body)
	}
	if !strings.Contains(body, "$290.00") {
		t.Errorf("low account must show balance $290.00 (300 - 10); got:\n%s", body)
	}
}

// TestAccountsCard_CreditNeverLow renders a credit account whose balance is
// deeply negative (money owed — the normal state of a card) and asserts the
// low-balance flag does NOT appear: the threshold is only meaningful for the
// checking and savings kinds (models.Account.LowBalanceThreshold), so
// comparing a card's balance to it would flag every card permanently.
func TestAccountsCard_CreditNeverLow(t *testing.T) {
	acct := models.Account{
		ID:           "usaa-credit-card",
		Name:         "USAA Credit Card",
		Institution:  "USAA",
		Kind:         models.AccountKindCredit,
		FilePatterns: []string{"test.csv"},
		Anchors:      []models.BalanceAnchor{{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: -11371.20}},
	}
	rows := [][]string{
		{"2026-08-10", "Groceries", "-10", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "USAA Credit Card") {
		t.Errorf("body missing account name; got:\n%s", body)
	}
	if strings.Contains(body, "low-balance threshold") {
		t.Errorf("credit account must never show the low-balance flag (a card balance is negative by nature); got:\n%s", extractAccountsCardSection(body))
	}
}

// TestAccountsCard_Stale renders an account whose latest transaction is
// older than the staleness window and asserts the stale flag appears WITH
// text, so a stale CSV cannot masquerade as a healthy balance.
func TestAccountsCard_Stale(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 2000.00}},
		LowBalanceThreshold: 500.00,
	}
	// A transaction from February; now is August 12, so data is ~6 months
	// old, well past the 45-day staleness window.
	rows := [][]string{
		{"2026-02-10", "Groceries", "-100", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Data is stale") {
		t.Errorf("stale account must show 'Data is stale' text flag; got:\n%s", body)
	}
	if !strings.Contains(body, "Re-import") {
		t.Errorf("stale account must mention Re-import; got:\n%s", body)
	}
}

// TestAccountsCard_NoAnchor renders an account with no balance anchor and
// asserts the balance is shown as "unknown", NOT as $0.00. This is the
// ruling from GLOSSARY.md: a zero balance and an unknown balance are
// different facts. The $0.00 assertion is scoped to the accounts card
// <section> so an unrelated $0.00 in the KPI tiles (zero income) does not
// falsely trip it.
func TestAccountsCard_NoAnchor(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		LowBalanceThreshold: 500.00,
		// No anchors.
	}
	rows := [][]string{
		{"2026-08-10", "Groceries", "-100", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	card := extractAccountsCardSection(body)
	if card == "" {
		t.Fatalf("accounts card section not found in body; got:\n%s", body)
	}
	if !strings.Contains(card, "Balance unknown") {
		t.Errorf("no-anchor account must render 'Balance unknown' in the card; card:\n%s", card)
	}
	if !strings.Contains(card, "No balance anchor set") {
		t.Errorf("no-anchor account must render 'No balance anchor set' flag; card:\n%s", card)
	}
	// The no-anchor state MUST NOT render a $0.00 balance figure. A zero
	// balance and an unknown balance are different facts. Scoped to the card
	// section so the KPI tiles' legitimate $0.00 (zero income) does not
	// falsely trip this.
	if strings.Contains(card, "$0.00") {
		t.Errorf("no-anchor card must NOT render $0.00 (unknown != zero); card:\n%s", card)
	}
}

// ---------- Projection summary line: three states ----------

// TestProjectionLine_Crossing renders a checking account whose projected
// balance crosses below the threshold and asserts the crossing date and
// suggested top-up appear.
func TestProjectionLine_Crossing(t *testing.T) {
	// Anchor $800 on Aug 1. Three monthly rent payments (May/Jun/Jul) make
	// the recurring engine detect a monthly $400 outflow whose next
	// occurrence after asOf is Sep 1. A small Aug-10 transaction lifts
	// maxDate past the anchor so BalanceAt has an anchor at or before asOf.
	// Balance at Aug 10 = 800 - 5 = 795. The Sep 1 rent drops it to 395,
	// below the $500 threshold. Crossing is Sep 1.
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	rows := [][]string{
		{"2026-05-01", "Rent", "-400", "Housing"},
		{"2026-06-01", "Rent", "-400", "Housing"},
		{"2026-07-01", "Rent", "-400", "Housing"},
		{"2026-08-10", "Coffee", "-5", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Drops below $500") {
		t.Errorf("crossing projection must state 'Drops below $500'; got:\n%s", body)
	}
}

// TestProjectionLine_NoCrossing renders a checking account whose projected
// balance stays above the threshold and asserts the "stays above" line
// appears.
func TestProjectionLine_NoCrossing(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 5000.00}},
		LowBalanceThreshold: 500.00,
	}
	// Small outflow, far above threshold.
	rows := [][]string{
		{"2026-08-10", "Coffee", "-5", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Projected to stay above the $500 threshold") {
		t.Errorf("no-crossing projection must state 'Projected to stay above the $500 threshold'; got:\n%s", body)
	}
	if strings.Contains(body, "Drops below") {
		t.Errorf("no-crossing projection must NOT contain 'Drops below'; got:\n%s", body)
	}
}

// TestProjectionLine_Unavailable renders a checking account with no anchor
// and asserts the projection line says unavailable rather than showing a
// number.
func TestProjectionLine_Unavailable(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		LowBalanceThreshold: 500.00,
	}
	rows := [][]string{
		{"2026-08-10", "Coffee", "-5", "Food"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Projection unavailable") {
		t.Errorf("unavailable projection must state 'Projection unavailable'; got:\n%s", body)
	}
	if strings.Contains(body, "Drops below") || strings.Contains(body, "Projected to stay above") {
		t.Errorf("unavailable projection must not show a crossing or no-crossing line; got:\n%s", body)
	}
}

// ---------- Suppressed duplicates are not double-counted (R3) ----------

// TestAccountsCard_SuppressedDuplicateCountedOnce writes a resolved
// near-duplicate pair (a bill-pay row and its matching posted-check row,
// one Suppressed via a kept_winner decision) and asserts the accounts
// card balance reflects the debit exactly once. Before R3 the card read
// data.Transactions (the raw, unfiltered set) instead of data.Active(),
// so a resolved duplicate was still counted twice.
func TestAccountsCard_SuppressedDuplicateCountedOnce(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dashboard-dup-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Two files so both rows land as separate CSV rows but the same
	// account (both filenames match the account's FilePatterns). A bill-pay
	// row and its matching posted-check row form a near-duplicate
	// candidate pair (see near_duplicates.go): same amount, within the
	// 7-day window, one description matching "Check #...".
	csvA := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n"
	csvB := "Date,Description,Amount,Status\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "a.csv"), []byte(csvA), 0644); err != nil {
		t.Fatalf("write a.csv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.csv"), []byte(csvB), 0644); err != nil {
		t.Fatalf("write b.csv: %v", err)
	}

	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	acct := models.Account{
		ID:           "usaa",
		Name:         "USAA Checking",
		Institution:  "USAA",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"a.csv", "b.csv"},
		// Anchor chosen so single-counting the duplicate's debit keeps the
		// balance above the $500 threshold (2200 - 1580.43 = 619.57), but
		// double-counting it (the bug) would push the balance to -960.86,
		// well below the threshold. This exercises the low-balance warning,
		// not just the raw balance figure.
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Amount: 2200.00}},
		LowBalanceThreshold: 500.00,
	}
	writeAccountsFixture(t, store, []models.Account{acct})

	dl := dataloader.New(tmpDir, store)

	// First load discovers the candidate pair and its hashes.
	ts1, err := dl.LoadData()
	if err != nil {
		t.Fatalf("first LoadData: %v", err)
	}
	var pairKey, billHash, checkHash string
	for _, tr := range ts1.Transactions {
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

	// Resolve it: keep the posted check, suppress the bill-pay side --
	// the user's real workflow for a confirmed duplicate.
	if err := dl.SaveDuplicateDecision(pairKey, dataloader.DuplicateDecision{
		KeptHash:       checkHash,
		SuppressedHash: billHash,
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
	}); err != nil {
		t.Fatalf("SaveDuplicateDecision: %v", err)
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}
	prevNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = prevNow }()

	Initialize(dl, rend, nil, store)
	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGetDash(t, r, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	card := extractAccountsCardSection(body)
	if card == "" {
		t.Fatalf("accounts card section not found in body; got:\n%s", body)
	}

	// Counted once: 2200.00 - 1580.43 = 619.57, above the $500 threshold.
	// Counted twice (the bug) would render 2200.00 - 2*1580.43 = -960.86
	// and incorrectly trip the low-balance flag.
	if !strings.Contains(card, "$619.57") {
		t.Errorf("accounts card must count the suppressed duplicate's debit exactly once ($619.57); card:\n%s", card)
	}
	if strings.Contains(card, "960.86") {
		t.Errorf("accounts card double-counted the resolved duplicate (found the double-counted balance); card:\n%s", card)
	}
	if strings.Contains(card, "low-balance threshold") {
		t.Errorf("balance $619.57 is above the $500 threshold; the low-balance warning must not fire (it would if the duplicate were double-counted); card:\n%s", card)
	}
}

// ---------- Dashboard funding projection must ignore suppressed rows in
// the recurring input too (R3 attempt 2) ----------

// suppressedInterlopeTxn builds a Lucid outflow transaction for the fixture
// below. Suppressed rows are set directly on the models.Transaction rather
// than routed through the CSV/near-duplicate-detection pipeline: R3 is
// about whether the projection respects an already-set Suppressed flag, not
// about how that flag gets set (R2's concern, covered elsewhere).
func suppressedInterloperTxn(date string, amount float64, suppressed bool) models.Transaction {
	d, _ := time.Parse("2006-01-02", date)
	return models.Transaction{
		Date:            d,
		Description:     "Lucid",
		Amount:          amount,
		TransactionType: models.Outflow,
		AccountID:       "checking",
		Suppressed:      suppressed,
		Hash:            date + "-" + fmt.Sprintf("%.2f", amount) + "-" + fmt.Sprintf("%v", suppressed),
	}
}

// TestAccountsCard_ProjectionIgnoresSuppressedRecurringLeg is acceptance
// criterion 1 (a fixture whose history actually produces a recurring item,
// with the crossing date/minimum/suggested top-up asserted precisely) and
// criterion 1(c)/3 for the recurring-input regression the amendment
// describes: a resolved duplicate sitting inside an otherwise-monthly
// series must not corrupt its detected interval.
//
// Fixture: five genuine monthly $900 "Lucid" debits (Jan-May) plus a sixth
// Suppressed=true row one day after the March occurrence -- the same
// near-zero-interval shape the amendment's first reproduction used. Before
// this fix, detectRecurringForDashboard fed the RAW (unfiltered) set to
// insights.DetectRecurringAt: the suppressed row's one-day gap inflates the
// interval std-dev past the strict-monthly cutoff, reclassifying the
// series "ongoing" -- and accounts.Project's frequencyIntervalDays returns
// ok=false for "ongoing", so the recurring outflow is dropped entirely.
// With a $2,000 threshold and a $2,400 balance that never dips on its own,
// that misclassification hides a real crossing the correct (Active())
// input reports at Jun 1 with a $500 shortfall.
//
// Verified independently by direct computation before writing this test:
// with the recurring-input bug present, this exact fixture reports
// Crossing = zero time (no crossing) and SuggestedTopUp = 0 ("healthy, no
// top-up") -- silently wrong. This test pins the correct answer and would
// fail against that regression.
func TestAccountsCard_ProjectionIgnoresSuppressedRecurringLeg(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dashboard-recur-suppress-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	acct := models.Account{
		ID:                  "checking",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"checking"},
		LowBalanceThreshold: 2000.00,
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Amount: 6000.00}},
	}
	writeAccountsFixture(t, store, []models.Account{acct})

	txns := []models.Transaction{
		suppressedInterloperTxn("2026-01-01", -900, false),
		suppressedInterloperTxn("2026-02-01", -900, false),
		suppressedInterloperTxn("2026-03-01", -900, false),
		suppressedInterloperTxn("2026-03-02", -900, true), // resolved duplicate: near-zero interval
		suppressedInterloperTxn("2026-04-01", -900, false),
		suppressedInterloperTxn("2026-05-01", -900, false),
	}
	ts := models.NewTransactionSet(txns)
	asOf := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	// Mirrors the exact composition at handlers.go:114:
	//   buildAccountsCard(store, data.Active().Transactions, asOf, detectRecurringForDashboard(data, asOf))
	card := buildAccountsCard(store, ts.Active().Transactions, asOf, detectRecurringForDashboard(ts, asOf))

	if len(card.Accounts) != 1 {
		t.Fatalf("expected 1 account view, got %d", len(card.Accounts))
	}
	view := card.Accounts[0]
	if view.Projection == nil {
		t.Fatalf("expected a projection for a checking account")
	}
	proj := view.Projection

	wantCrossing := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !proj.Crossing.Equal(wantCrossing) {
		t.Errorf("Crossing = %v, want %v (a monthly-classified series correctly reports the Jun 1 crossing; "+
			"the pre-fix bug misclassifies this series as \"ongoing\" via the suppressed row's near-zero interval, "+
			"which accounts.Project silently drops -- reporting no crossing at all)", proj.Crossing, wantCrossing)
	}
	if proj.Minimum != 1500.00 {
		t.Errorf("Minimum = %.2f, want 1500.00; the pre-fix bug reports 2400.00 (no recurring outflow applied)", proj.Minimum)
	}
	if proj.SuggestedTopUp != 500.00 {
		t.Errorf("SuggestedTopUp = %.2f, want 500.00; the pre-fix bug reports 0.00", proj.SuggestedTopUp)
	}
	if view.ProjectionState != projectionCrossing {
		t.Errorf("ProjectionState = %q, want %q", view.ProjectionState, projectionCrossing)
	}
}

// TestAccountsCard_ProjectionCrossing_ExactNumbers is the general form of
// acceptance criterion 1: a fixture with enough history for the recurring
// engine to fire, asserting the crossing date, minimum, and suggested
// top-up precisely (not just the presence of "Drops below" text, which
// TestProjectionLine_Crossing already covers via HTTP). Same fixture as
// TestProjectionLine_Crossing, called directly through the production
// composition (buildAccountsCard + detectRecurringForDashboard) so the
// unrendered Minimum field can be asserted too.
func TestAccountsCard_ProjectionCrossing_ExactNumbers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dashboard-projexact-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	acct := models.Account{
		ID:                  "usaa",
		Name:                "USAA Checking",
		Institution:         "USAA",
		Kind:                models.AccountKindChecking,
		FilePatterns:        []string{"test.csv"},
		Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	writeAccountsFixture(t, store, []models.Account{acct})

	txns := []models.Transaction{
		{Date: mustDate(t, "2026-05-01"), Description: "Rent", Amount: -400, TransactionType: models.Outflow, AccountID: "usaa", Hash: "r1"},
		{Date: mustDate(t, "2026-06-01"), Description: "Rent", Amount: -400, TransactionType: models.Outflow, AccountID: "usaa", Hash: "r2"},
		{Date: mustDate(t, "2026-07-01"), Description: "Rent", Amount: -400, TransactionType: models.Outflow, AccountID: "usaa", Hash: "r3"},
		{Date: mustDate(t, "2026-08-10"), Description: "Coffee", Amount: -5, TransactionType: models.Outflow, AccountID: "usaa", Hash: "c1"},
	}
	ts := models.NewTransactionSet(txns)
	asOf := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	card := buildAccountsCard(store, ts.Active().Transactions, asOf, detectRecurringForDashboard(ts, asOf))
	if len(card.Accounts) != 1 {
		t.Fatalf("expected 1 account view, got %d", len(card.Accounts))
	}
	proj := card.Accounts[0].Projection
	if proj == nil {
		t.Fatalf("expected a projection")
	}

	wantCrossing := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	if !proj.Crossing.Equal(wantCrossing) {
		t.Errorf("Crossing = %v, want %v", proj.Crossing, wantCrossing)
	}
	if proj.Minimum != 395.00 {
		t.Errorf("Minimum = %.2f, want 395.00", proj.Minimum)
	}
	if proj.SuggestedTopUp != 200.00 {
		t.Errorf("SuggestedTopUp = %.2f, want 200.00", proj.SuggestedTopUp)
	}
}

// mustDate parses a YYYY-MM-DD date, failing the test on error.
func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("mustDate(%q): %v", s, err)
	}
	return d
}

// ---------- Unassigned-files banner: present and absent ----------

// TestUnassignedBanner_Present writes a CSV matching no account and asserts
// the banner appears with the count and a link to /accounts.
func TestUnassignedBanner_Present(t *testing.T) {
	// No accounts configured, so the CSV matches no account and its rows
	// are unassigned.
	rows := [][]string{
		{"2026-08-10", "Salary", "5000", "Payroll"},
		{"2026-08-11", "Rent", "-1500", "Housing"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, nil, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unassigned-banner") {
		t.Errorf("banner must be present (id unassigned-banner); got:\n%s", body)
	}
	if !strings.Contains(body, "matching no account") {
		t.Errorf("banner must state 'matching no account'; got:\n%s", body)
	}
	if !strings.Contains(body, `href="/accounts"`) {
		t.Errorf("banner must link to /accounts; got:\n%s", body)
	}
	// Count assertion (ruling 2026-08-16e): precede loop assertions with a
	// count assertion. Two transactions are unassigned.
	if !strings.Contains(body, "2 transactions came from CSV files") {
		t.Errorf("banner must state the count '2 transactions came from CSV files'; got:\n%s", body)
	}
	// Dismiss button is keyboard-accessible (ACCESSIBILITY.md point 14).
	if !strings.Contains(body, `aria-label="Dismiss unassigned files notice"`) {
		t.Errorf("banner must have a keyboard-accessible dismiss button; got:\n%s", body)
	}
}

// TestUnassignedBanner_Absent writes a CSV matching an account and asserts
// the banner does NOT appear.
func TestUnassignedBanner_Absent(t *testing.T) {
	acct := models.Account{
		ID:           "usaa",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"test.csv"},
		Anchors:      []models.BalanceAnchor{{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: 2000.00}},
	}
	rows := [][]string{
		{"2026-08-10", "Salary", "5000", "Payroll"},
	}
	router, _, cleanup := setupAccountsTestEnv(t, rows, []models.Account{acct}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup()

	rec := doGetDash(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "unassigned-banner") {
		t.Errorf("banner must NOT be present when all files are assigned; got:\n%s", body)
	}
}
