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

func TestBuildIncomeChartData_StacksSSOtherAndWithdrawals(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.ProjectionYears = 2

	months := make([]models.ProjectionMonth, 0, 24)
	for m := 0; m < 24; m++ {
		months = append(months, models.ProjectionMonth{
			Month:                     m,
			Year:                      float64(m) / 12.0,
			CumulativeInflation:       1.0 + 0.0025*float64(m), // ~3%/yr
			SocialSecurityIncome:      2000,
			TotalIncome:               2500, // SS 2000 + Other 500
			WithdrawalFromTaxDeferred: 1000,
			WithdrawalFromTaxable:     500,
			WithdrawalFromRoth:        0,
		})
	}
	projection := &models.ProjectionResult{Months: months, Survives: true}

	chartData := buildIncomeChartData(settings, projection, "nominal")
	traces, ok := chartData["data"].([]map[string]interface{})
	if !ok || len(traces) != 3 {
		t.Fatalf("expected 3 stacked traces, got %#v", chartData["data"])
	}
	for _, tr := range traces {
		if tr["stackgroup"] != "income" {
			t.Errorf("trace %q missing stackgroup, got %#v", tr["name"], tr["stackgroup"])
		}
	}

	ssTrace := traces[0]
	if ssTrace["name"] != "Social Security" {
		t.Fatalf("first trace should be Social Security, got %v", ssTrace["name"])
	}
	ssY := ssTrace["y"].([]float64)
	if len(ssY) != 2 {
		t.Fatalf("expected 2 yearly buckets, got %d", len(ssY))
	}
	// Year 0: 12 months × $2000 SS = $24,000
	if got, want := ssY[0], 24000.0; got != want {
		t.Errorf("year 0 SS = %v, want %v", got, want)
	}

	otherTrace := traces[1]
	otherY := otherTrace["y"].([]float64)
	// Year 0: 12 × $500 = $6,000
	if got, want := otherY[0], 6000.0; got != want {
		t.Errorf("year 0 Other = %v, want %v", got, want)
	}

	wTrace := traces[2]
	wY := wTrace["y"].([]float64)
	// Year 0: 12 × ($1000 + $500) = $18,000
	if got, want := wY[0], 18000.0; got != want {
		t.Errorf("year 0 Withdrawals = %v, want %v", got, want)
	}
}

func TestBuildIncomeChartData_DeflatesToTodayDollarsWhenReal(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.ProjectionYears = 1

	months := make([]models.ProjectionMonth, 0, 12)
	for m := 0; m < 12; m++ {
		months = append(months, models.ProjectionMonth{
			Month:                m,
			Year:                 float64(m) / 12.0,
			CumulativeInflation:  2.0, // double — today's dollars should be half
			SocialSecurityIncome: 2000,
			TotalIncome:          2000,
		})
	}
	projection := &models.ProjectionResult{Months: months, Survives: true}

	chartData := buildIncomeChartData(settings, projection, "real")
	traces := chartData["data"].([]map[string]interface{})
	ssY := traces[0]["y"].([]float64)
	// $2000/mo × 12 / 2.0 inflation = $12,000 in today's dollars
	if got, want := ssY[0], 12000.0; got != want {
		t.Errorf("real SS = %v, want %v", got, want)
	}

	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	if got := yaxis["title"]; got != "Annual Income (Today's Dollars)" {
		t.Errorf("real y-axis title = %v", got)
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

func TestBuildProjectionChartData_EventLabelsGetHeadroom(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.ProjectionYears = 15
	settings.IncomeSources = []models.IncomeSource{{Name: "Social Security", Amount: 2000, StartMonth: 24}}
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "nominal")

	traces := chartData["data"].([]map[string]interface{})
	if len(traces) < 2 {
		t.Fatalf("expected event trace, got %d traces", len(traces))
	}
	if clip, ok := traces[1]["cliponaxis"].(bool); !ok || clip {
		t.Errorf("event trace cliponaxis = %v, want false so labels can render at the top edge", traces[1]["cliponaxis"])
	}

	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	rng, ok := yaxis["range"].([]float64)
	if !ok || len(rng) != 2 {
		t.Fatalf("expected explicit y-axis range for headroom, got %#v", yaxis["range"])
	}
	maxBalance := 0.0
	for _, m := range projection.Months {
		if m.PortfolioBalance > maxBalance {
			maxBalance = m.PortfolioBalance
		}
	}
	if rng[0] != 0 || rng[1] <= maxBalance {
		t.Errorf("y range = %v, want [0, >%v] headroom above the peak balance", rng, maxBalance)
	}
}

func TestBuildProjectionChartData_NoEventsMeansAutoRange(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.IncomeSources = nil
	settings.HealthcarePersons = nil
	settings.ScenarioChain = nil
	// Past RMD start: CurrentAge alone doesn't drive RMD timing —
	// FirstRMDCalendarYear reads Persons[0].BirthMonth (see CLAUDE.md
	// gotcha), so both must move together or the RMD event resurfaces.
	settings.CurrentAge = 80
	settings.Persons[0].BirthMonth = models.BirthMonthForAge(settings.StartDate, 80)
	projection := sampleProjectionForChart()

	chartData := buildProjectionChartData(settings, projection, "nominal")
	layout := chartData["layout"].(map[string]interface{})
	yaxis := layout["yaxis"].(map[string]interface{})
	if _, present := yaxis["range"]; present {
		t.Errorf("without events the y-axis should keep Plotly auto-range")
	}
}
