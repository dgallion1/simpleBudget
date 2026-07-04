package dashboard

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestDashboardVerdictBar_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	t.Run("over budget shows red band and over-budget headline", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthRed, HasTarget: true, IsOver: true,
				Delta: 2500, SpentTotal: 12500, TargetTotal: 10000,
				Months: 3, NetSavings: 2000, SavingsRate: 20, TotalIncome: 10000,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-red", "over budget", "Spent", "Target", `class="num`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
	})

	t.Run("under budget shows green band and under-budget headline", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthGreen, HasTarget: true, IsUnder: true,
				Delta: -800, SpentTotal: 9200, TargetTotal: 10000, Months: 3, NetSavings: 1500,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-green", "under budget"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
	})

	t.Run("no target shows neutral band and a link to set a budget", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthNeutral, HasTarget: false, NetSavings: 500,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-neutral", "No budget set", "/whatif"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
		if strings.Contains(out, "over budget") || strings.Contains(out, "under budget") {
			t.Errorf("did not expect a budget delta headline when HasTarget is false")
		}
	})
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
