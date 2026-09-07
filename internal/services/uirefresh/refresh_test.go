package uirefresh

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSignalsAreIsolatedAndReadsAreFresh(t *testing.T) {
	a, b := New(), New()
	read := func(c *Coordinator) State {
		t.Helper()
		w := httptest.NewRecorder()
		c.ServeHTTP(w, httptest.NewRequest("GET", "/api/ui-refresh", nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("response: %d %v", w.Code, w.Header())
		}
		var s State
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	initial := read(a)
	if initial.Epoch == "" || initial.Epoch == read(b).Epoch {
		t.Fatal("epochs not unique")
	}
	for i := uint64(1); i <= 3; i++ {
		got := a.Request()
		if got.Revision != i || got.Epoch != initial.Epoch || read(a) != got {
			t.Fatalf("stale signal: %+v", got)
		}
		if read(b).Revision != 0 {
			t.Fatal("cross-instance signal")
		}
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("POST", "/api/ui-refresh", nil))
	if w.Code != 405 || read(a).Revision != 3 {
		t.Fatal("route must be read only")
	}
}
