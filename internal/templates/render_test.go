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
		// The verdict band requires a typed Health; this test doesn't
		// exercise it, so use a neutral no-data verdict.
		"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
		"MinDate":     "2024-01-01",
		"MaxDate":     "2024-12-31",
		"StartDate":   "2024-01-01",
		"EndDate":     "2024-12-31",
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

func TestRenderProjectionBreakdownCard(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	html, err := renderer.RenderToString("whatif-projection-breakdown", map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{
			ProjectionExplainability: &models.ProjectionExplainability{
				YearlySummaries: []models.ProjectionYearSummary{
					{
						Year:              0,
						StartingBalance:   1_000_000,
						Growth:            40_000,
						GrossIncome:       60_000,
						Taxes:             9_000,
						Expenses:          55_000,
						Withdrawals:       4_000,
						EndingBalance:     1_041_000,
						EndingBalanceReal: 1_010_000,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Year-by-Year Projection") {
		t.Fatalf("expected projection breakdown title, got %q", html)
	}
	if !strings.Contains(html, "Gross cash in includes income, dividends, and portfolio distributions before taxes.") {
		t.Fatalf("expected projection breakdown cash-flow note, got %q", html)
	}
	if !strings.Contains(html, "Portfolio Out") {
		t.Fatalf("expected projection breakdown withdrawal label, got %q", html)
	}
	if !strings.Contains(html, "$1,041,000") {
		t.Fatalf("expected ending balance in rendered html, got %q", html)
	}
}

func TestRenderProjectionChartCardIncludesModelAssumptions(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	settings := models.DefaultWhatIfSettings()
	settings.ProjectionTiming = models.ProjectionTimingMidMonth

	html, err := renderer.RenderToString("whatif-projection-chart", map[string]any{
		"Settings": settings,
		"Analysis": &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{
				FinalBalance: 1_250_000,
				Survives:     true,
			},
			ProjectionExplainability: &models.ProjectionExplainability{
				FinalBalanceReal:        900_000,
				InflationLossPercent:    28.0,
				TaxShareOfGrossCashFlow: 12.5,
				CumulativeInflation:     1.72,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Model assumptions") {
		t.Fatalf("expected model assumptions summary, got %q", html)
	}
	if !strings.Contains(html, "Average-cost basis for taxable sales") {
		t.Fatalf("expected taxable basis assumption, got %q", html)
	}
	if !strings.Contains(html, "mid-month") {
		t.Fatalf("expected timing assumption in rendered html, got %q", html)
	}
}
