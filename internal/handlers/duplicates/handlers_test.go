package duplicates

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

func TestGetDuplicates_RendersUnresolvedTab(t *testing.T) {
	// Initialize wires loader (nil-safe path) and renderer (we use raw
	// template-free fallback so the test doesn't depend on web embed).
	Initialize(nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/duplicates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Duplicates") {
		t.Errorf("body missing 'Duplicates' marker: %s", body)
	}
}

func newLoaderWithFixture(t *testing.T) (*dataloader.DataLoader, func()) {
	t.Helper()
	tmp, err := os.MkdirTemp("", "dup_handlers")
	if err != nil {
		t.Fatalf("tmpdir: %v", err)
	}
	csv := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	if err := os.WriteFile(filepath.Join(tmp, "bank.csv"), []byte(csv), 0644); err != nil {
		os.RemoveAll(tmp)
		t.Fatalf("write csv: %v", err)
	}
	store, err := storage.New(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		t.Fatalf("storage: %v", err)
	}
	return dataloader.New(tmp, store), func() { os.RemoveAll(tmp) }
}

func TestResolve_KeptWinner_PersistsAndRedirects(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("first load: %v", err)
	}
	pairs := loader.UnresolvedDuplicates()
	if len(pairs) != 1 {
		t.Fatalf("expected 1 unresolved pair, got %d", len(pairs))
	}
	left, right := pairs[0].Left, pairs[0].Right

	form := url.Values{}
	form.Set("pair_key", pairs[0].Key)
	form.Set("outcome", dataloader.DuplicateOutcomeKeptWinner)
	form.Set("kept_hash", left.Hash)
	form.Set("suppressed_hash", right.Hash)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/duplicates" {
		t.Errorf("Location = %q, want /duplicates", got)
	}

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("UnresolvedDuplicateCount after resolve = %d, want 0",
			loader.UnresolvedDuplicateCount())
	}
}

func TestResolve_KeptBoth_StopsFlagging(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)
	loader.LoadData()
	pairs := loader.UnresolvedDuplicates()

	form := url.Values{}
	form.Set("pair_key", pairs[0].Key)
	form.Set("outcome", dataloader.DuplicateOutcomeKeptBoth)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("kept_both should clear unresolved count, got %d",
			loader.UnresolvedDuplicateCount())
	}
}

func TestResolve_BadPairKey_ReturnsBadRequest(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)

	form := url.Values{}
	form.Set("outcome", dataloader.DuplicateOutcomeKeptBoth)
	// pair_key intentionally omitted

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUndo_ClearsDecisionAndReflagsOnReload(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)
	loader.LoadData()
	pairs := loader.UnresolvedDuplicates()
	pk := pairs[0].Key

	loader.SaveDuplicateDecision(pk, dataloader.DuplicateDecision{
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
		KeptHash:       pairs[0].Left.Hash,
		SuppressedHash: pairs[0].Right.Hash,
	})
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Fatalf("setup: expected resolved, got %d unresolved",
			loader.UnresolvedDuplicateCount())
	}

	form := url.Values{}
	form.Set("pair_key", pk)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/undo",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 1 {
		t.Errorf("after undo, expected 1 unresolved, got %d",
			loader.UnresolvedDuplicateCount())
	}
}
