package approval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/go-chi/chi/v5"
)

// router wires the real routes over a fresh registry, so tests exercise the
// same URL shape cmd/server serves.
func router(t *testing.T) (*chi.Mux, *confirm.Approvals) {
	t.Helper()
	a := confirm.NewApprovals(time.Minute)
	Initialize(a)
	t.Cleanup(func() { Initialize(nil) })

	r := chi.NewRouter()
	RegisterPublicRoutes(r)
	return r, a
}

func get(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func post(t *testing.T, r http.Handler, path, decision string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("decision="+decision))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// The page must show what is at stake. A confirmation page that does not
// state the consequence is the same theater as a token.
func TestPageShowsTheConsequences(t *testing.T) {
	r, a := router(t)
	p, _ := a.Create("restore_backup", "a.zip", "Restore a.zip?",
		"ANY FILE THE ARCHIVE DOES NOT CONTAIN WOULD BE DELETED")

	rec := get(t, r, "/mcp/approve/"+p.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"restore_backup", "Restore a.zip?", "WOULD BE DELETED"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not mention %q:\n%s", want, body)
		}
	}
	// Both answers must be offered. A page with only an approve button is a
	// dark pattern, not a consent flow.
	if !strings.Contains(body, `value="approve"`) || !strings.Contains(body, `value="decline"`) {
		t.Error("page does not offer both answers")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("page is cacheable; a back button could re-serve an answered request")
	}
}

func TestApprovingReleasesTheWaitingTool(t *testing.T) {
	r, a := router(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")

	got := make(chan confirm.Decision, 1)
	go func() {
		d, _ := a.Await(context.Background(), p)
		got <- d
	}()

	if rec := post(t, r, "/mcp/approve/"+p.ID, "approve"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	select {
	case d := <-got:
		if d != confirm.Approved {
			t.Errorf("Decision = %v, want Approved", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting tool was never released")
	}
}

func TestDecliningReleasesTheWaitingToolWithARefusal(t *testing.T) {
	r, a := router(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")

	got := make(chan confirm.Decision, 1)
	go func() {
		d, _ := a.Await(context.Background(), p)
		got <- d
	}()

	post(t, r, "/mcp/approve/"+p.ID, "decline")
	select {
	case d := <-got:
		if d != confirm.Refused {
			t.Errorf("Decision = %v, want Refused", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting tool was never released")
	}
}

// Anything that is not an explicit approval is a refusal. A submit with no
// decision at all -- a stray form post, a crafted request -- must not approve.
func TestAnythingButApproveIsARefusal(t *testing.T) {
	for _, value := range []string{"", "yes", "true", "APPROVE", "nonsense"} {
		t.Run("value="+value, func(t *testing.T) {
			r, a := router(t)
			p, _ := a.Create("restore_backup", "a.zip", "t", "d")

			got := make(chan confirm.Decision, 1)
			go func() {
				d, _ := a.Await(context.Background(), p)
				got <- d
			}()

			post(t, r, "/mcp/approve/"+p.ID, value)
			select {
			case d := <-got:
				if d != confirm.Refused {
					t.Errorf("decision=%q was treated as %v, want Refused", value, d)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the waiting tool was never released")
			}
		})
	}
}

func TestUnknownOrAnsweredRequestsAreGone(t *testing.T) {
	r, a := router(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", "d")

	if rec := get(t, r, "/mcp/approve/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}
	post(t, r, "/mcp/approve/"+p.ID, "approve")

	// A second submit of the same form must not flip the answer.
	rec := post(t, r, "/mcp/approve/"+p.ID, "decline")
	if rec.Code != http.StatusNotFound {
		t.Errorf("re-submitted id: status = %d, want 404", rec.Code)
	}
	if rec := get(t, r, "/mcp/approve/"+p.ID); rec.Code != http.StatusNotFound {
		t.Errorf("answered id: status = %d, want 404", rec.Code)
	}
}

// A page served with no registry configured must say nothing is pending, not
// panic and take the server down.
func TestWithoutARegistryTheePageIsHarmless(t *testing.T) {
	Initialize(nil)
	r := chi.NewRouter()
	RegisterPublicRoutes(r)

	if rec := get(t, r, "/mcp/approve/anything"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if rec := post(t, r, "/mcp/approve/anything", "approve"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// The detail text is attacker-influenced only by the server itself, but it is
// rendered into HTML: if that ever stops being escaped, an archive name is a
// scripting vector.
func TestDetailIsEscaped(t *testing.T) {
	r, a := router(t)
	p, _ := a.Create("restore_backup", "a.zip", "t", `<script>alert(1)</script>`)

	body := get(t, r, "/mcp/approve/"+p.ID).Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("detail was rendered as live HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("detail was not escaped into the page at all:\n%s", body)
	}
}
