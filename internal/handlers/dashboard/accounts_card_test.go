package dashboard

import (
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
