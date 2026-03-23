package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

func TestRenderInsightsContentAllowsBlankSubscriptionDescriptions(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	html, err := renderer.RenderToString("insights-content", map[string]any{
		"MinDate":   "2024-01-01",
		"MaxDate":   "2024-12-31",
		"StartDate": "2024-01-01",
		"EndDate":   "2024-12-31",
		"Insights": models.InsightsData{
			Subscriptions: []models.RecurringPayment{
				{
					Description: "",
					Frequency:   "monthly",
					Amount:      12.34,
					AnnualCost:  148.08,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Unlabeled subscription") {
		t.Fatalf("expected fallback subscription label, got %q", html)
	}
	if !strings.Contains(html, ">?<") {
		t.Fatalf("expected fallback subscription initial, got %q", html)
	}
}
