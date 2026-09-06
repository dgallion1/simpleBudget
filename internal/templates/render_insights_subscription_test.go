package templates

import (
	"html"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

func TestRenderInsightsSubscriptionInitial(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, major, description, initial, label string
	}{
		{"major expense", "Travel & Hobbies", "Other", "T", "Travel & Hobbies"},
		{"description", "", "Netflix", "N", "Netflix"},
		{"unicode major", "旅行", "Other", "旅", "旅行"},
		{"unicode description", "", "Éducation", "É", "Éducation"},
		{"emoji", "🎵 Music", "", "🎵", "🎵 Music"},
		{"empty", "", "", "?", "Unlabeled subscription"},
		{"long", strings.Repeat("TravelAndHobbies", 40), "", "T", strings.Repeat("TravelAndHobbies", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderer.RenderToString("insights-content", map[string]any{
				"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
				"MinDate":     "2024-01-01", "MaxDate": "2024-12-31",
				"StartDate": "2024-01-01", "EndDate": "2024-12-31",
				"Insights": models.InsightsData{Subscriptions: []models.RecurringPayment{{
					MajorExpenseName: tc.major, Description: tc.description,
					Frequency: "monthly", Amount: 12.34, AnnualCost: 148.08,
				}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			badge := regexp.MustCompile(`<span class="text-xs font-bold text-negative">([^<]*)</span>`).FindStringSubmatch(out)
			if len(badge) != 2 || html.UnescapeString(badge[1]) != tc.initial {
				t.Errorf("badge = %q, want %q", badge, tc.initial)
			}
			if !strings.Contains(out, ">"+html.EscapeString(tc.label)+"</p>") {
				t.Error("full subscription label missing")
			}
			for _, amount := range []string{">$12.34</p>", ">$148.08/yr</p>"} {
				if !strings.Contains(out, amount) {
					t.Errorf("missing rendered amount %s", amount)
				}
			}
		})
	}
}
