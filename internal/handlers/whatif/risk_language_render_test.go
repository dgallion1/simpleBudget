package whatif

import (
	"budget2/internal/models"
	"strings"
	"testing"
)

func TestBufferIllustration(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	out, err := renderer.RenderToString("whatif-sequence-risk", map[string]any{
		"Analysis": &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{
			Runs: 1000, SequenceRiskImpact: 12, SequenceRisk: &models.SequenceRiskBreakdown{
				RecommendedBuffer: 3, BufferAmount: 90000, AnnualExpenses: 60000, AnnualShortfall: 30000, AdjustedSpending: 3000,
				SafeWithdrawalDuringCrash: 30000, CrashedPortfolioValue: 1000000, CrashDrawdownPercent: 30, NaiveBufferAmount: 180000,
			}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Buffer illustration — not simulation-tested", "Illustrative withdrawal at 3%", "Illustrative monthly withdrawal at 4%", "This illustration uses total expenses and fixed withdrawal assumptions.", "It does not account for outside income, withdrawal taxes, or a separate cash allocation.", "<details", "<summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in rendered buffer", want)
		}
	}
	for _, absent := range []string{"Recommended Buffer", "Safe 3%", "Safe monthly spending", "is sufficient", "provides good protection"} {
		if strings.Contains(out, absent) {
			t.Errorf("unsupported promise %q remains", absent)
		}
	}
}
