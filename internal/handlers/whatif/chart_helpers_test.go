package whatif

import (
	"testing"

	"budget2/internal/models"
)

func sampleProjectionForChart() *models.ProjectionResult {
	months := make([]models.ProjectionMonth, 0, 180)
	for m := 0; m < 180; m++ {
		months = append(months, models.ProjectionMonth{
			Month:                m,
			Year:                 float64(m) / 12.0,
			PortfolioBalance:     1_000_000 - float64(m)*2_000,
			PortfolioBalanceReal: 950_000 - float64(m)*1_800,
		})
	}
	return &models.ProjectionResult{
		Months:       months,
		FinalBalance: months[len(months)-1].PortfolioBalance,
		Survives:     true,
	}
}

func TestBuildProjectionChartData_UsesRealBalancesWhenRequested(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.ProjectionYears = 15
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "real")
	traces, ok := chartData["data"].([]map[string]interface{})
	if !ok || len(traces) == 0 {
		t.Fatalf("expected chart traces, got %#v", chartData["data"])
	}

	yValues, ok := traces[0]["y"].([]float64)
	if !ok || len(yValues) == 0 {
		t.Fatalf("expected balance series, got %#v", traces[0]["y"])
	}
	if yValues[0] != projection.Months[0].PortfolioBalanceReal {
		t.Fatalf("expected first real balance %.2f, got %.2f", projection.Months[0].PortfolioBalanceReal, yValues[0])
	}

	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	if got := yaxis["title"]; got != "Balance (Today's Dollars)" {
		t.Fatalf("expected real y-axis title, got %#v", got)
	}
}

func TestBuildProjectionChartData_AddsKeyEventMarkers(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.ProjectionYears = 15
	settings.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: "downsize-plan.json", TransitionAge: 75}}
	settings.IncomeSources = []models.IncomeSource{
		{Name: "Social Security", Amount: 2000, StartMonth: 24},
		{Name: "Pension", Amount: 1500, StartMonth: 12},
	}
	settings.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                "Pat",
			CurrentAge:          62,
			CurrentCoverage:     models.CoverageACA,
			CurrentMonthlyCost:  1000,
			MedicareMonthlyCost: 600,
			MedicareEligibleAge: 65,
		},
	}
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "nominal")
	traces := chartData["data"].([]map[string]interface{})
	if len(traces) < 2 {
		t.Fatalf("expected event marker trace, got %d traces", len(traces))
	}

	eventTrace := traces[1]
	text, ok := eventTrace["text"].([]string)
	if !ok {
		t.Fatalf("expected event text labels, got %#v", eventTrace["text"])
	}

	expected := []string{
		"Pension starts",
		"Social Security starts",
		"Medicare: Pat",
		"RMD starts",
		"Scenario: Downsize Plan",
	}
	for _, label := range expected {
		found := false
		for _, actual := range text {
			if actual == label {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected event label %q in %#v", label, text)
		}
	}
}
