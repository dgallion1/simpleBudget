package main

import (
	"budget2/internal/services/uirefresh"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDI6MountedRefreshRouteAndLayout(t *testing.T) {
	router := newMCPRouter(t)
	read := func() uirefresh.State {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/ui-refresh", nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("route: %d %v", w.Code, w.Header())
		}
		var state uirefresh.State
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := read()
	pageRefresh.Request()
	after := read()
	if after.Epoch != before.Epoch || after.Revision != before.Revision+1 {
		t.Fatalf("stale route: %+v", after)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/dashboard", nil))
	if w.Code != 200 || strings.Count(w.Body.String(), `src="/static/js/page-refresh.js"`) != 1 {
		t.Fatalf("layout missing refresh script: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/static/js/page-refresh.js", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/api/ui-refresh") {
		t.Fatal("refresh script not served")
	}
}
