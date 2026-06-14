package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/web"
)

// renderMajorExpensesVerdict renders the verdict band define with an arbitrary
// data map. It uses a plain map for TrackingVerdict (rather than importing the
// majorexpenses type) to avoid an import cycle: majorexpenses imports templates.
func renderMajorExpensesVerdict(t *testing.T, verdict map[string]any) string {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	html, err := r.RenderToString("major-expenses-verdict-bar", map[string]any{"TrackingVerdict": verdict})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return html
}

func TestRenderMajorExpenses_VerdictBar(t *testing.T) {
	t.Run("green well-tracked band", func(t *testing.T) {
		out := renderMajorExpensesVerdict(t, map[string]any{
			"Health": "green", "HasSpend": true, "TrackedPercent": 90.0,
			"DeclaredTotal": 9000.0, "UnmatchedTotal": 1000.0, "UnmatchedCount": 3,
		})
		for _, want := range []string{"verdict-green", "of spending tracked", "Declared", "Unmatched", `class="num`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q; got: %s", want, mtrunc(out, 700))
			}
		}
	})

	t.Run("red mostly-untracked band", func(t *testing.T) {
		out := renderMajorExpensesVerdict(t, map[string]any{
			"Health": "red", "HasSpend": true, "TrackedPercent": 30.0,
			"DeclaredTotal": 3000.0, "UnmatchedTotal": 7000.0, "UnmatchedCount": 25,
		})
		if !strings.Contains(out, "verdict-red") {
			t.Errorf("expected verdict-red; got: %s", mtrunc(out, 700))
		}
	})

	t.Run("neutral no-spend band", func(t *testing.T) {
		out := renderMajorExpensesVerdict(t, map[string]any{"Health": "neutral", "HasSpend": false})
		if !strings.Contains(out, "verdict-neutral") {
			t.Errorf("expected verdict-neutral; got: %s", mtrunc(out, 700))
		}
		if !strings.Contains(out, "No spending in this range") {
			t.Errorf("expected no-spend message; got: %s", mtrunc(out, 700))
		}
		if strings.Contains(out, "of spending tracked") {
			t.Errorf("did not expect tracked headline when HasSpend is false")
		}
	})
}

func mtrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
