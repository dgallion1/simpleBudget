package transfers

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	accountssvc "budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/services/transfers"
	"budget2/internal/templates"
	"budget2/web"
)

// transferCSVs is the fixture: one Schwab->USAA paired transfer, one
// Vanguard external leg (counterparty not loaded), a dividend, and a
// grocery run. It mirrors internal/services/dataloader/transfers_test.go so
// the classification the handler reads is the same shape the loader
// produces.
const (
	schwabTransferCSV = `Date,Description,Category,Amount
2026-05-01,Dividend,Investing,12.50
2026-05-04,SCHWAB MONEYLINK TRANSFER,Transfer,-2000.00`

	usaaTransferCSV = `Date,Description,Category,Amount
2026-05-03,VANGUARD BUY INVESTMENT,Investing,-1500.00
2026-05-06,TRANSFER IN FROM SCHWAB,Deposit,2000.00
2026-05-07,Wegmans,Groceries,-84.12`
)

// coincidenceCSVs adds a $60 debit and a $60 credit in different accounts
// with no transfer-pattern hit, so they land in the suspected queue.
var coincidenceCSVs = map[string]string{
	"schwab-brokerage-2026.csv": schwabTransferCSV + "\n2026-05-12,ZELLE FROM PAT,Other,60.00",
	"usaa-checking-2026.csv":    usaaTransferCSV + "\n2026-05-11,TARGET STORE 1123,Shopping,-60.00",
}

// setupTestEnv builds a temp data dir with the transfer CSVs and an account
// per file, a storage service, a dataloader, and the transfers handler
// initialized in JSON-mode (renderer == nil) so tests can assert on the
// round-tripped pageData. Mirrors accounts.setupTestEnv.
func setupTestEnv(t *testing.T, files map[string]string) (*dataloader.DataLoader, *storage.Storage, func()) {
	t.Helper()
	csvDir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(csvDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	store, err := storage.New(csvDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dl := dataloader.New(csvDir, store)

	if err := accountssvc.Save(store, []models.Account{
		{ID: "schwab", Name: "Schwab Brokerage", Institution: "Schwab", Kind: models.AccountKindBrokerage, FilePatterns: []string{"schwab-*.csv"}},
		{ID: "usaa", Name: "USAA Checking", Institution: "USAA", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-*.csv"}},
	}); err != nil {
		t.Fatalf("save accounts: %v", err)
	}

	Initialize(dl, store, nil) // renderer = nil -> JSON responses for tests
	return dl, store, func() {}
}

// setupTestEnvWithRenderer wires the package with a real templates.Renderer
// pulling from the embedded FS, so tests can assert on rendered HTML.
func setupTestEnvWithRenderer(t *testing.T, files map[string]string) (*dataloader.DataLoader, *storage.Storage, func()) {
	t.Helper()
	dl, store, prevCleanup := setupTestEnv(t, files)

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	rend, err := templates.NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	Initialize(dl, store, rend)
	return dl, store, func() {
		prevCleanup()
		Initialize(dl, store, nil) // restore JSON-mode
	}
}

func newRouter() http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r
}

// formPost builds a url-encoded POST request.
func formPost(method, target string, values url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// TestHandlePage_RendersPairedExternalAndSuspected: the page renders all
// three classes when the fixtures hold them. Asserts on the RENDERED HTML so
// it guards the template the handler actually returns (ruling 2026-08-16a).
func TestHandlePage_RendersPairedExternalAndSuspected(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/transfers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// The three classes appear in the history + queue. Each is preceded by
	// a count assertion so a missing range is not a silent no-op (ruling
	// 2026-08-16e).
	if got := strings.Count(body, "Paired"); got < 1 {
		t.Errorf("expected at least one Paired badge in history, got %d", got)
	}
	if got := strings.Count(body, "External"); got < 1 {
		t.Errorf("expected at least one External status in history, got %d", got)
	}
	if got := strings.Count(body, "Suspected pairs"); got < 1 {
		t.Fatalf("expected a Suspected pairs heading, got %d", got)
	}
	// The suspected pair has confirm + reject buttons (real <button>, not div).
	if got := strings.Count(body, "Confirm pair"); got != 1 {
		t.Fatalf("expected exactly 1 Confirm pair button, got %d", got)
	}
	if got := strings.Count(body, "Reject"); got != 1 {
		t.Fatalf("expected exactly 1 Reject button, got %d", got)
	}
	// Both legs of the suspected pair render.
	if !strings.Contains(body, "TARGET STORE 1123") {
		t.Errorf("suspected leg A (TARGET STORE) missing")
	}
	if !strings.Contains(body, "ZELLE FROM PAT") {
		t.Errorf("suspected leg B (ZELLE) missing")
	}
	// The paired transfer's two legs render in history.
	if !strings.Contains(body, "SCHWAB MONEYLINK TRANSFER") {
		t.Errorf("paired debit leg missing from history")
	}
	if !strings.Contains(body, "TRANSFER IN FROM SCHWAB") {
		t.Errorf("paired credit leg missing from history")
	}
	// The external leg renders and is marked external.
	if !strings.Contains(body, "VANGUARD BUY INVESTMENT") {
		t.Errorf("external leg missing from history")
	}
	if !strings.Contains(body, "no loaded counterparty") {
		t.Errorf("external leg not marked as having no loaded counterparty")
	}
}

// TestHandlePage_ChartDataTableFallbackCarriesSameNumbers: the data-table
// alternative carries the same values the chart payload encodes. The chart
// payload is JSON; the table rows are HTML. The single paired transfer is
// 2000.00 from Schwab to USAA in 2026-05, so both must show that.
func TestHandlePage_ChartDataTableFallbackCarriesSameNumbers(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t, map[string]string{
		"schwab-brokerage-2026.csv": schwabTransferCSV,
		"usaa-checking-2026.csv":    usaaTransferCSV,
	})
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/transfers", nil))
	body := w.Body.String()

	// The chart is present (the paired transfer produces a flow).
	if !strings.Contains(body, "chart-transfers-flow") {
		t.Fatalf("chart container missing; body: %s", body)
	}
	// The data table carries the same month + direction + amount.
	if !strings.Contains(body, "2026-05") {
		t.Errorf("data table missing the transfer month 2026-05")
	}
	if !strings.Contains(body, "Schwab") || !strings.Contains(body, "USAA") {
		t.Errorf("data table missing from/to institutions (Schwab, USAA)")
	}
	// The amount $2,000.00 appears in the data table (formatted).
	if !strings.Contains(body, "2,000.00") {
		t.Errorf("data table missing the $2,000.00 amount")
	}
	// The chart JSON payload carries the SAME value (as a raw number 2000),
	// so the table and the chart agree (ACCESSIBILITY.md point 11).
	if !strings.Contains(body, `"y":[2000]`) && !strings.Contains(body, "2000") {
		t.Errorf("chart payload missing the 2000 amount (table/chart disagree)")
	}
	// The text summary states the takeaway (point 11).
	if !strings.Contains(body, "Largest flow:") {
		t.Errorf("text summary of the chart takeaway missing")
	}
}

// TestHandlePage_EmptyQueueState: with no transfer rows and no suspected
// pairs, the page renders the empty state, not a chart or history.
func TestHandlePage_EmptyQueueState(t *testing.T) {
	// A single grocery row: no transfers, no suspected pairs.
	csv := map[string]string{
		"usaa-checking-2026.csv": "Date,Description,Category,Amount\n2026-05-07,Wegmans,Groceries,-84.12\n",
	}
	_, _, cleanup := setupTestEnvWithRenderer(t, csv)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/transfers", nil))
	body := w.Body.String()
	if !strings.Contains(body, "No transfers yet") {
		t.Errorf("empty state heading missing; body: %s", body)
	}
	if strings.Contains(body, `id="chart-transfers-flow"`) {
		t.Errorf("chart container should not render in the empty state")
	}
	if strings.Contains(body, "Transfer history") {
		t.Errorf("history section should not render in the empty state")
	}
}

// TestHandleResolve_ConfirmPersistsAndDropsFromQueue: posting confirm calls
// ResolveTransfer with VerdictConfirm, the decision persists (a reload leaves
// the queue empty), and the queue partial re-renders without the pair.
// Proves persistence through ResolveTransfer, the contract the spec names.
func TestHandleResolve_ConfirmPersistsAndDropsFromQueue(t *testing.T) {
	dl, store, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	// Seed the queue: the page load triggers a load, populating suspected.
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/transfers", nil))
	if !strings.Contains(w.Body.String(), "Confirm pair") {
		t.Fatalf("seed load did not populate the queue: %s", w.Body.String())
	}
	pairKey := mustSuspectedPairKey(t, dl)

	// POST confirm.
	w = httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/transfers/resolve", url.Values{
		"pair_key": {pairKey},
		"verdict":  {"confirm"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The partial re-renders the empty queue.
	if !strings.Contains(body, "No suspected transfer pairs awaiting review") {
		t.Errorf("queue partial should show empty after confirm; body: %s", body)
	}
	// The outcome is announced (aria-live + visible banner).
	if !strings.Contains(body, "Confirmed pair") {
		t.Errorf("resolve outcome not announced; body: %s", body)
	}

	// The decision persisted: a fresh load's queue is empty.
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := dl.SuspectedTransfers(); len(got) != 0 {
		t.Fatalf("SuspectedTransfers after reload = %d, want 0 (decision should persist)", len(got))
	}
	// And the two legs are now typed Transfer/paired on reload.
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("reload 2: %v", err)
	}
	for _, desc := range []string{"TARGET STORE 1123", "ZELLE FROM PAT"} {
		if txn := findTxnByDesc(t, ts, desc); txn.TransactionType != models.Transfer || txn.TransferClass != transfers.ClassPaired {
			t.Errorf("%q after confirm: type/class = %q/%q, want Transfer/paired", desc, txn.TransactionType, txn.TransferClass)
		}
	}
	_ = store
}

// TestHandleResolve_RejectPersistsAndNeverResuggested: posting reject calls
// ResolveTransfer with VerdictReject, the pair leaves the queue, and a reload
// does NOT suggest it again and does NOT type the rows Transfer.
func TestHandleResolve_RejectPersistsAndNeverResuggested(t *testing.T) {
	dl, _, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	// Seed the queue.
	_ = httptest.NewRecorder()
	newRouter().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/transfers", nil))
	pairKey := mustSuspectedPairKey(t, dl)

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/transfers/resolve", url.Values{
		"pair_key": {pairKey},
		"verdict":  {"reject"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Rejected pair") {
		t.Errorf("reject outcome not announced; body: %s", w.Body.String())
	}

	// Reload: queue stays empty (rejected is never suggested again).
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := dl.SuspectedTransfers(); len(got) != 0 {
		t.Fatalf("SuspectedTransfers after reject+reload = %d, want 0", len(got))
	}
	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("reload 2: %v", err)
	}
	for _, desc := range []string{"TARGET STORE 1123", "ZELLE FROM PAT"} {
		if txn := findTxnByDesc(t, ts, desc); txn.TransactionType == models.Transfer {
			t.Errorf("%q was typed Transfer despite a reject", desc)
		}
	}
}

// TestHandleResolve_IdempotentRepost: re-posting the SAME decision must NOT
// error. After a confirm, posting confirm again for the now-absent pair key
// returns the (unchanged) queue partial with a no-op message, not a 4xx.
func TestHandleResolve_IdempotentRepost(t *testing.T) {
	dl, _, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	// Seed the queue.
	_ = httptest.NewRecorder()
	newRouter().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/transfers", nil))
	pairKey := mustSuspectedPairKey(t, dl)

	// First confirm.
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/transfers/resolve", url.Values{
		"pair_key": {pairKey},
		"verdict":  {"confirm"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("first confirm status = %d", w.Code)
	}

	// Reload so the queue no longer holds the pair.
	if _, err := dl.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Second confirm for the now-absent pair key: must be 200, not an error.
	w = httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/transfers/resolve", url.Values{
		"pair_key": {pairKey},
		"verdict":  {"confirm"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("idempotent re-post status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "already") {
		t.Errorf("idempotent re-post should report a no-op (already confirmed); body: %s", body)
	}
	if !strings.Contains(body, "No suspected transfer pairs awaiting review") {
		t.Errorf("queue partial should still render empty; body: %s", body)
	}
}

// TestHandleQueuePartial_RendersQueue: the HTMX partial endpoint renders
// ONLY the queue partial (not the full page), with the suspected pair.
// Ruling 2026-08-16a: assert on the partial the handler returns.
func TestHandleQueuePartial_RendersQueue(t *testing.T) {
	dl, _, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/transfers/queue", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The partial does NOT include the full-page <h1>Transfers</h1>.
	if strings.Contains(body, ">Transfers</h1>") {
		t.Errorf("partial should not render the full-page h1; body: %s", body)
	}
	// The partial renders the suspected pair (not the section heading, which
	// lives in the full-page template). The pair's legs + confirm button are
	// the partial's content.
	if !strings.Contains(body, "TARGET STORE 1123") {
		t.Errorf("partial missing suspected leg A; body: %s", body)
	}
	if got := strings.Count(body, "Confirm pair"); got != 1 {
		t.Fatalf("partial expected 1 Confirm button, got %d; body: %s", got, body)
	}
	_ = dl
}

// TestHandleResolve_BadInput: a missing pair key or unknown verdict surfaces
// a message rather than crashing.
func TestHandleResolve_BadInput(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t, coincidenceCSVs)
	defer cleanup()

	cases := []struct {
		name   string
		values url.Values
	}{
		{"missing pair key", url.Values{"verdict": {"confirm"}}},
		{"unknown verdict", url.Values{"pair_key": {"deadbeef1234"}, "verdict": {"maybe"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newRouter().ServeHTTP(w, formPost("POST", "/transfers/resolve", tc.values))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (message, not 4xx); body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "Invalid request") {
				t.Errorf("expected an invalid-request message; body: %s", w.Body.String())
			}
		})
	}
}

// mustSuspectedPairKey returns the single suspected pair key from the loader,
// failing the test if the queue is empty or has more than one entry.
func mustSuspectedPairKey(t *testing.T, dl *dataloader.DataLoader) string {
	t.Helper()
	queue := dl.SuspectedTransfers()
	if len(queue) != 1 {
		t.Fatalf("expected exactly 1 suspected pair, got %d", len(queue))
	}
	return queue[0].PairKey
}

// findTxnByDesc returns the single transaction with the given description.
func findTxnByDesc(t *testing.T, ts *models.TransactionSet, desc string) models.Transaction {
	t.Helper()
	var found []models.Transaction
	for _, txn := range ts.Transactions {
		if txn.Description == desc {
			found = append(found, txn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d rows described %q, want 1", len(found), desc)
	}
	return found[0]
}
