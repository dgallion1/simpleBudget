package templates

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"budget2/web"
)

// ============================================================
// U8 — one date range propagated across pages (§2c of the U-run spec).
//
// These are the "base-layout/nav render tests" the U8 task block points to:
// there was no existing suite for layouts/base.html's nav, so this file is
// it. It exercises the REAL base.html template (via the embedded FS, same
// as every handler) with page-data shapes that mirror what the real
// handlers pass: map[string]interface{} for the range-bearing pages
// (dashboard/explorer/insights/major-expenses, and also the
// no-range map pages whatif/duplicates/filemanager), and a bare struct for
// the pages that use their own pageData struct (accounts, transfers) with
// no StartDate/EndDate field at all.
// ============================================================

func newNavRenderer(t *testing.T) *Renderer {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}
	return renderer
}

// moneyGroupHrefRe finds the href attribute value for a given bare path
// (e.g. "/dashboard"), which may or may not carry a query string.
func moneyGroupHrefRe(path string) *regexp.Regexp {
	return regexp.MustCompile(`href="` + regexp.QuoteMeta(path) + `(\?[^"]*)?"`)
}

// TestBaseNav_PropagatesRangeOnMoneyGroupLinks proves acceptance (a): with a
// range set, the Dashboard/Explorer/Insights/Major-Expenses nav links (both
// desktop bar and mobile menu -- 4 links x 2 = 8 anchors) carry the exact
// current start/end, and the Plan/Setup links do not.
func TestBaseNav_PropagatesRangeOnMoneyGroupLinks(t *testing.T) {
	renderer := newNavRenderer(t)

	pageData := map[string]interface{}{
		"Title":                    "Dashboard",
		"ActiveTab":                "nav-test",
		"StartDate":                "2025-03-01",
		"EndDate":                  "2025-06-30",
		"Comparison":               "mom", // dashboard-only, must NOT propagate
		"UnresolvedDuplicateCount": 0,
	}

	html, err := renderer.RenderToString("base", pageData)
	if err != nil {
		t.Fatalf("RenderToString(base) error: %v", err)
	}

	wantStart := "start=2025-03-01"
	wantEnd := "end=2025-06-30"

	rangeLinks := []string{"/dashboard", "/explorer", "/insights", "/major-expenses"}
	for _, path := range rangeLinks {
		matches := moneyGroupHrefRe(path).FindAllStringSubmatch(html, -1)
		if len(matches) != 2 {
			t.Fatalf("%s: expected 2 anchors (desktop + mobile), found %d in: %v", path, len(matches), matches)
		}
		for _, m := range matches {
			if !strings.Contains(m[0], wantStart) || !strings.Contains(m[0], wantEnd) {
				t.Errorf("%s href %q does not carry both %q and %q", path, m[0], wantStart, wantEnd)
			}
		}
	}

	bareLinks := []string{"/whatif", "/accounts", "/transfers", "/filemanager"}
	for _, path := range bareLinks {
		matches := moneyGroupHrefRe(path).FindAllStringSubmatch(html, -1)
		if len(matches) == 0 {
			t.Fatalf("%s: expected at least one anchor, found none", path)
		}
		for _, m := range matches {
			if strings.Contains(m[0], "start=") || strings.Contains(m[0], "end=") {
				t.Errorf("%s href %q must NOT carry start/end, but does", path, m[0])
			}
			if m[1] != "" {
				t.Errorf("%s href %q must be bare (no query string), got query %q", path, m[0], m[1])
			}
		}
	}

	// (c) comparison/preset stay page-local: the value must never appear in
	// an href anywhere in the rendered nav.
	if strings.Contains(html, "comparison=mom") {
		t.Errorf("rendered nav propagates 'comparison', which must stay page-local: %s", html)
	}
}

// TestBaseNav_NoRangeSetRendersBareLinks proves acceptance criterion (b.iii):
// when the current page carries no StartDate/EndDate at all (e.g. it was
// never set, or the page is one of the no-range map pages), the four
// Money-group links render bare, with no query string.
func TestBaseNav_NoRangeSetRendersBareLinks(t *testing.T) {
	renderer := newNavRenderer(t)

	pageData := map[string]interface{}{
		"Title":                    "File Manager",
		"ActiveTab":                "nav-test",
		"UnresolvedDuplicateCount": 0,
		// No StartDate/EndDate key at all -- mirrors filemanager/whatif/duplicates.
	}

	html, err := renderer.RenderToString("base", pageData)
	if err != nil {
		t.Fatalf("RenderToString(base) error: %v", err)
	}

	for _, path := range []string{"/dashboard", "/explorer", "/insights", "/major-expenses"} {
		matches := moneyGroupHrefRe(path).FindAllStringSubmatch(html, -1)
		if len(matches) != 2 {
			t.Fatalf("%s: expected 2 anchors (desktop + mobile), found %d", path, len(matches))
		}
		for _, m := range matches {
			if m[1] != "" {
				t.Errorf("%s href %q should be bare with no range set, got query %q", path, m[0], m[1])
			}
		}
	}
}

// TestBaseNav_EmptyRangeStringsRenderBareLinks covers the half-set case: a
// StartDate/EndDate key present but empty (e.g. resolveDateRange failed to
// produce a window) must not produce "?start=&end=".
func TestBaseNav_EmptyRangeStringsRenderBareLinks(t *testing.T) {
	renderer := newNavRenderer(t)

	pageData := map[string]interface{}{
		"Title":                    "Explorer",
		"ActiveTab":                "nav-test",
		"StartDate":                "",
		"EndDate":                  "",
		"UnresolvedDuplicateCount": 0,
	}

	html, err := renderer.RenderToString("base", pageData)
	if err != nil {
		t.Fatalf("RenderToString(base) error: %v", err)
	}

	if strings.Contains(html, "start=") || strings.Contains(html, "end=") {
		t.Errorf("empty StartDate/EndDate must not produce a query string, got: %s", html)
	}
}

// TestBaseNav_StructPageDataRendersBareLinks proves the accounts/transfers
// shape (a struct pageData with no StartDate/EndDate field, per
// internal/handlers/accounts and internal/handlers/transfers) renders the
// Money-group nav links bare and does not error the template execution --
// this is the case a naive ".StartDate" field-access implementation of
// withRange would break.
type navTestStructPageData struct {
	Title                    string
	ActiveTab                string
	UnresolvedDuplicateCount int
}

func TestBaseNav_StructPageDataRendersBareLinks(t *testing.T) {
	renderer := newNavRenderer(t)

	pageData := navTestStructPageData{
		Title:     "Accounts",
		ActiveTab: "nav-test",
	}

	html, err := renderer.RenderToString("base", pageData)
	if err != nil {
		t.Fatalf("RenderToString(base) with struct pageData error: %v", err)
	}

	for _, path := range []string{"/dashboard", "/explorer", "/insights", "/major-expenses"} {
		matches := moneyGroupHrefRe(path).FindAllStringSubmatch(html, -1)
		if len(matches) != 2 {
			t.Fatalf("%s: expected 2 anchors (desktop + mobile), found %d", path, len(matches))
		}
		for _, m := range matches {
			if m[1] != "" {
				t.Errorf("%s href %q should be bare for a struct pageData with no range field, got query %q", path, m[0], m[1])
			}
		}
	}
}

// TestWithRange_Unit covers extractDateRange/withRange directly across the
// shapes real handlers use.
func TestWithRange_Unit(t *testing.T) {
	tests := []struct {
		name string
		data interface{}
		want string
	}{
		{
			name: "map with both dates set",
			data: map[string]interface{}{"StartDate": "2025-01-01", "EndDate": "2025-01-31"},
			want: "/explorer?end=2025-01-31&start=2025-01-01",
		},
		{
			name: "map missing keys",
			data: map[string]interface{}{"ActiveTab": "filemanager"},
			want: "/explorer",
		},
		{
			name: "map with empty-string dates",
			data: map[string]interface{}{"StartDate": "", "EndDate": "2025-01-31"},
			want: "/explorer",
		},
		{
			name: "struct without the field",
			data: navTestStructPageData{Title: "Accounts"},
			want: "/explorer",
		},
		{
			name: "pointer to struct without the field",
			data: &navTestStructPageData{Title: "Accounts"},
			want: "/explorer",
		},
		{
			name: "nil",
			data: nil,
			want: "/explorer",
		},
		{
			name: "non-string StartDate value on a map is ignored, not a panic",
			data: map[string]interface{}{"StartDate": 12345, "EndDate": "2025-01-31"},
			want: "/explorer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withRange("/explorer", tt.data)
			if got != tt.want {
				t.Errorf("withRange(%q, %#v) = %q, want %q", "/explorer", tt.data, got, tt.want)
			}
		})
	}
}
