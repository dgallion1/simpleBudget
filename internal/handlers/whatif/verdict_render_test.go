package whatif

import (
	"strings"
	"testing"
)

func TestVerdictBar_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("green funded plan shows headline and figures", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictGreen, Headline: "Funded through 2064",
				Detail: "spending covered for all 38 years",
				MonthlyGap: -200, GapIsShortfall: false,
				RequiredRate: 0, SuccessRate: 85, HasMonteCarlo: true,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Funded through 2064", "spending covered for all 38 years", "85.0%", "verdict-green"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, truncate(out, 600))
			}
		}
	})

	t.Run("red plan shows shortfall styling and run-out headline", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictRed, Headline: "Funds run out in 2032",
				Detail: "covered for 6 of 38 years",
				MonthlyGap: 1601.38, GapIsShortfall: true,
				RequiredRate: 3.1, SuccessRate: 12, HasMonteCarlo: true,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Funds run out in 2032", "verdict-red", "1,601"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, truncate(out, 600))
			}
		}
	})

	t.Run("steady-state gap is labeled with the selected year", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictAmber, Headline: "Funded through 2064",
				Detail: "spending covered for all 38 years",
				MonthlyGap: 3400, GapIsShortfall: true, RequiredRate: 4.2,
				GapAtSteadyState: true, GapYear: 12,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Gap @ Yr 12") {
			t.Errorf("expected gap labeled with selected year; got: %s", truncate(out, 600))
		}
	})

	t.Run("today's gap keeps the plain label", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictGreen, Headline: "Funded through 2064",
				Detail: "spending covered for all 38 years",
				MonthlyGap: -200, GapIsShortfall: false, GapAtSteadyState: false,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Monthly Gap") {
			t.Errorf("expected plain 'Monthly Gap' label; got: %s", truncate(out, 600))
		}
		if strings.Contains(out, "Gap @ Yr") {
			t.Errorf("did not expect a year label when GapAtSteadyState is false")
		}
	})

	t.Run("no monte carlo hides the MC figure", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-verdict-bar", map[string]any{
			"Verdict": VerdictView{
				Health: VerdictGreen, Headline: "Funded through 2046",
				Detail: "spending covered for all 20 years", HasMonteCarlo: false,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, "Monte Carlo") {
			t.Errorf("did not expect Monte Carlo figure when HasMonteCarlo is false; got: %s", truncate(out, 600))
		}
	})
}
