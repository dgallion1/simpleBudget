package duplicates

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
