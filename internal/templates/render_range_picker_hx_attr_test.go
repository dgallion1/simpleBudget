package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/web"
)

// The range-picker's inline layout splices .InputHxAttrs mid-<input> so a page
// (Insights) can wire HTMX auto-refresh on manual date edits. That value must
// be a template.HTMLAttr (via safeHTMLAttr) — a template.HTML (safeHTML) in
// attribute context is rewritten to the literal "ZgotmplZ" by html/template,
// which silently dropped the hx-* attributes and broke Insights' date-edit
// auto-refresh. This pins that the hx attributes render and ZgotmplZ does not.
func TestRangePickerInputHxAttrsRenderInAttributeContext(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	out, err := r.RenderToString("shared/range-picker", map[string]any{
		"Layout": "inline", "StartID": "s", "StartName": "start", "StartValue": "2025-01-01",
		"EndID": "e", "EndName": "end", "EndValue": "2025-03-01",
		"MinDate": "2024-01-01", "MaxDate": "2025-12-31",
		"InputHxAttrs": safeHTMLAttr(`hx-get="/insights" hx-trigger="change"`),
		"Presets":      []map[string]any{},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, "ZgotmplZ") {
		t.Errorf("date input rendered ZgotmplZ; InputHxAttrs must be template.HTMLAttr (safeHTMLAttr), not template.HTML:\n%s", out)
	}
	if !strings.Contains(out, `hx-get="/insights"`) || !strings.Contains(out, `hx-trigger="change"`) {
		t.Errorf("date input missing the spliced hx-* attributes (HTMX auto-refresh would be dead):\n%s", out)
	}
}
