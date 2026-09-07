package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

func TestDI1OtherRecurringVisible(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	payment := models.RecurringPayment{Description: "Unknown Merchant", MajorExpenseName: "Household", Classification: "other", ClassificationReason: "Recurring pattern without positive subscription or bill evidence", Amount: 20, AnnualCost: 240, Frequency: "monthly"}
	out, err := renderer.RenderToString("insights-content", map[string]any{
		"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
		"Insights":    models.InsightsData{OtherRecurring: []models.RecurringPayment{payment}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Other recurring spending", ">Unknown Merchant</span>", "Household", payment.ClassificationReason, "$240.00", "$20.00"} {
		if !strings.Contains(out, text) {
			t.Errorf("missing visible recurring evidence %q", text)
		}
	}
}
