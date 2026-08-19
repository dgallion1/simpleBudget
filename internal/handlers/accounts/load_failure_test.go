package accounts

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
	accountssvc "budget2/internal/services/accounts"
)

// Attempt 3 (2026-08-19) regression coverage: collapsing load+edit+save
// into one accounts.Mutate closure merged a LOAD failure into the same
// "data.Error = err.Error(); renderList(...)" (HTTP 200) branch a
// SAVE/validation failure takes. At HEAD, handleDelete, handleAddAnchor
// and handleDeleteAnchor each returned 500 with "Failed to load accounts: "
// + err.Error() on a load failure specifically. These tests pin that a
// load failure is still a 500 with the byte-identical message, while a
// save/write failure remains a 200 with the error surfaced inline
// (unchanged HEAD behaviour, not to be "fixed" back to 500).

// corruptAccountsJSON writes invalid JSON to accounts.json inside store's
// base dir, the same fixture accounts_test.go uses to make Load fail.
func corruptAccountsJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, accountssvc.AccountsFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("corrupt accounts.json: %v", err)
	}
}

// wantLoadFailureMessage is the byte-identical prefix the brief pins.
const wantLoadFailurePrefix = "Failed to load accounts: "

func assertLoadFailure500(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, wantLoadFailurePrefix) {
		t.Errorf("body does not contain %q; got:\n%s", wantLoadFailurePrefix, body)
	}
	// The message must carry decode's own error text verbatim, not a
	// generic replacement.
	if !strings.Contains(body, "invalid accounts file") {
		t.Errorf("body does not carry the underlying decode error; got:\n%s", body)
	}
}

func TestHandleDelete_LoadFailureIs500(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
	})
	corruptAccountsJSON(t, store.BaseDir())

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{"confirm": {"yes"}}))
	assertLoadFailure500(t, w)
}

func TestHandleAddAnchor_LoadFailureIs500(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking},
	})
	corruptAccountsJSON(t, store.BaseDir())

	form := url.Values{
		"anchor_date":   {"2026-08-01"},
		"anchor_amount": {"4210.55"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	assertLoadFailure500(t, w)
}

func TestHandleDeleteAnchor_LoadFailureIs500(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking, Anchors: []models.BalanceAnchor{
			{Date: parseDate("2026-08-01"), Amount: 500},
		}},
	})
	corruptAccountsJSON(t, store.BaseDir())

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor/2026-08-01/delete", url.Values{}))
	assertLoadFailure500(t, w)
}

// --- save/write failure: unchanged at 200 with the error surfaced inline ---
//
// These pin that the fix above did NOT also flip the save/validation-error
// path (which was already 200 at HEAD, via the direct accounts.Save call)
// to 500. A read-only store directory makes accounts.json's write fail
// while the load (an already-present, readable accounts.json) succeeds, so
// the Mutate closure runs to completion and only the save fails -- the
// same shape a Validate failure would take.

// makeStoreDirReadOnly chmods dir to 0500 (no write) so a subsequent write
// through the store fails with permission denied, and restores 0755 on
// cleanup so t.TempDir can still remove it. Skips under root, where
// permission bits do not block writes.
func makeStoreDirReadOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission fixtures do not block root")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod 0500 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestHandleDelete_SaveFailureStays200WithInlineError(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
	})
	makeStoreDirReadOnly(t, store.BaseDir())

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{"confirm": {"yes"}}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (save failure surfaces inline); body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	errMsg, _ := body["Error"].(string)
	if errMsg == "" {
		t.Errorf("expected an inline Error on a save failure, got none; body=%v", body)
	}
	if strings.Contains(errMsg, wantLoadFailurePrefix) {
		t.Errorf("a save failure must not carry the load-failure message; got %q", errMsg)
	}
}

func TestHandleAddAnchor_SaveFailureStays200WithInlineError(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking},
	})
	makeStoreDirReadOnly(t, store.BaseDir())

	form := url.Values{
		"anchor_date":   {"2026-08-01"},
		"anchor_amount": {"4210.55"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (save failure surfaces inline); body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	errMsg, _ := body["Error"].(string)
	if errMsg == "" {
		t.Errorf("expected an inline Error on a save failure, got none; body=%v", body)
	}
	if strings.Contains(errMsg, wantLoadFailurePrefix) {
		t.Errorf("a save failure must not carry the load-failure message; got %q", errMsg)
	}
}

func TestHandleDeleteAnchor_SaveFailureStays200WithInlineError(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking, Anchors: []models.BalanceAnchor{
			{Date: parseDate("2026-08-01"), Amount: 500},
		}},
	})
	makeStoreDirReadOnly(t, store.BaseDir())

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor/2026-08-01/delete", url.Values{}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (save failure surfaces inline); body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	errMsg, _ := body["Error"].(string)
	if errMsg == "" {
		t.Errorf("expected an inline Error on a save failure, got none; body=%v", body)
	}
	if strings.Contains(errMsg, wantLoadFailurePrefix) {
		t.Errorf("a save failure must not carry the load-failure message; got %q", errMsg)
	}
}
