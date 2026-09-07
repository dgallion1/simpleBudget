package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDI3CumulativeCashFlowReporting(t *testing.T) {
	router, cleanup := setupTestEnv(t, [][]string{{"2026-01-12", "PAYCHECK", "10.006", "Income"}, {"2026-01-14", "Home purchase", "-0.004", "Home"}})
	defer cleanup()
	var out struct {
		Data []struct {
			Y []float64 `json:"y"`
		} `json:"data"`
	}
	body := doGet(t, router, "/dashboard/charts/data/cumulative?start=2026-01-01&end=2026-01-31").Body.Bytes()
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 || len(out.Data[0].Y) == 0 {
		t.Fatalf("missing chart: %s", body)
	}
	if got := out.Data[0].Y[len(out.Data[0].Y)-1]; got != 10.01 {
		t.Errorf("cash-flow chart ends at %v, displayed period difference is 10.01", got)
	}
}

// These real HTTP/renderer fixtures catch independent net rounding, stale
// selected-range navigation, and presenting empty observations as a verdict.
func TestDI3RenderedCashFlow(t *testing.T) {
	for _, tc := range []struct{ name, income, expense, net, explanation string }{
		{"fractional", "10.006", "-0.004", "$10.01", "Recorded income above spending"},
		{"boundary", "2.675", "-0.004", "$2.67", "Recorded income above spending"},
		{"shortfall", "10", "-20", "-$10.00", "Spending not covered by recorded income"},
		{"zero", "10.001", "-10.004", "$0.00", "Recorded income matches spending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, cleanup := setupTestEnvWithRenderer(t, [][]string{{"2026-01-12", "PAYCHECK", tc.income, "Income"}, {"2026-01-14", "Home purchase", tc.expense, "Home"}, {"2026-02-02", "PAYCHECK", "999", "Income"}})
			defer cleanup()
			for _, path := range []string{"/dashboard", "/dashboard/kpis"} {
				body := doGet(t, router, path+"?start=2026-01-01&end=2026-01-31").Body.String()
				for _, want := range []string{"Recorded income", "Net spending", "Cash-flow balance", "Spending versus plan", tc.net, tc.explanation, "does not measure portfolio withdrawals", "/insights?start=2026-01-01&amp;end=2026-01-31"} {
					if !strings.Contains(body, want) {
						t.Errorf("%s missing %q", path, want)
					}
				}
				if strings.Contains(body, "-$0.00") {
					t.Error("negative zero")
				}
				if got := extractAfterLabel(t, body, "Cash-flow balance"); got != tc.net {
					t.Errorf("displayed cash flow %s want %s", got, tc.net)
				}
			}
			csv := doGet(t, router, "/dashboard/kpi/savings/export?start=2026-01-01&end=2026-01-31").Body.String()
			if tc.name == "fractional" && !strings.Contains(csv, "2026-01,10.01,0.00,10.01") {
				t.Errorf("CSV %s", csv)
			}
			modal := doGet(t, router, "/dashboard/kpi/savings/month/2026-01?start=2026-01-01&end=2026-01-31").Body.String()
			if !strings.Contains(modal, tc.net) {
				t.Errorf("month modal missing %s", tc.net)
			}
		})
	}
}

func TestDI3MonthlyAdjustmentRendered(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, [][]string{{"2026-01-12", "Home purchase", "-0.004", "Home"}, {"2026-02-12", "Home purchase", "-0.004", "Home"}})
	defer cleanup()
	body := doGet(t, router, "/dashboard/kpi/expenses?start=2026-01-01&end=2026-02-28").Body.String()
	if strings.Count(body, "Rounding adjustment") != 1 || !strings.Contains(body, "$0.01") {
		t.Errorf("missing explicit period reconciliation: %s", body)
	}
}

func TestDI3EmptySelection(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, [][]string{{"2026-01-12", "Home purchase", "-100", "Home"}})
	defer cleanup()
	body := doGet(t, router, "/dashboard/kpis?start=2026-02-01&end=2026-02-28").Body.String()
	if !strings.Contains(body, "No transactions in this selected period") {
		t.Error("missing empty selection guidance")
	}
	if strings.Contains(body, "Recorded income matches spending") || strings.Contains(body, "verdict-green") {
		t.Error("empty selection has a verdict")
	}
}

func TestDI3HealthcareNoCoverage(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, healthcareNoCoverageFixtureRows())
	defer cleanup()
	body := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-01-31").Body.String()
	if !strings.Contains(body, "Monthly equivalent unavailable: no coverage in this range") {
		t.Error("missing no-coverage explanation")
	}
}
