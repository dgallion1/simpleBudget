package whatif

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
)

// CF1 (2026-09-18): every spending option card states, on its face directly
// under the option name, the Monthly Living Expenses setting (the slider's
// value) and the starting living budget (setting plus any boost or phase
// multiplier at the start). The headline figure is a simulated median and a
// reader could not relate it to the slider; the sentence used to live only
// in the collapsed "Supporting details" expander and only when the two
// figures differed.
func TestSpendingOptimizerRenderBudgetOnCardFace(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	rules := &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
	metrics := func(v float64) *models.SpendingRiskMetrics {
		return &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: v}
	}
	// Fractional-cent base (the live plan's value) with a $2,000 boost.
	current := models.SpendingCandidate{ID: "current", Kind: "current", Baseline: true, Qualifies: true, BaseMonthlyLivingExpenses: 7639.342517162467, StartingMonthlyLivingReal: 9639.342517162467, Metrics: metrics(8701.3)}
	planned8500 := models.SpendingCandidate{ID: "planned-8500", Kind: "planned", Qualifies: true, BaseMonthlyLivingExpenses: 8500, StartingMonthlyLivingReal: 8500, Guardrails: rules, Metrics: metrics(8500)}
	flex9000 := models.SpendingCandidate{ID: "flex-9000", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: 9000, StartingMonthlyLivingReal: 9500, Guardrails: rules, Metrics: metrics(9000)}
	// Fractional-cent searched candidate that is NOT a headline: it appears
	// only in the frontier table, whose base figure must be the same string
	// FormatMoney gives the card face for the same value.
	flexFrac := models.SpendingCandidate{ID: "flex-7639", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: 7639.342517162467, StartingMonthlyLivingReal: 7639.342517162467, Guardrails: rules, Metrics: metrics(7600)}
	result := &models.SpendingOptimizerResult{
		Request:           models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5},
		SelectionRuns:     1000,
		ValidationRuns:    1000,
		Candidates:        []models.SpendingCandidate{current, planned8500, flex9000, flexFrac},
		RecommendationIDs: []string{"planned-8500", "flex-9000"},
	}
	view, _, _, err := buildSpendingOptimizerView("card-face-fixture", result)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()

	const marker = "Monthly Living Expenses setting"
	if got := strings.Count(body, marker); got != 3 {
		t.Fatalf("%q occurrences = %d, want 3 (one per card)", marker, got)
	}
	// The old details-only sentence is gone everywhere.
	if strings.Contains(body, "starting monthly living budget") {
		t.Errorf("old details sentence still rendered")
	}

	articles := regexp.MustCompile(`(?s)<article[^>]*>.*?</article>`).FindAllString(body, -1)
	if len(articles) != 3 {
		t.Fatalf("cards = %d, want 3", len(articles))
	}
	want := map[string]string{
		"current":      "Monthly Living Expenses setting $7,639.34/mo · starting living budget $9,639.34/mo (scheduled phases or an early-spending boost apply at the start).",
		"planned-8500": "Monthly Living Expenses setting $8,500.00/mo · starting living budget $8,500.00/mo (the same at the start).",
		"flex-9000":    "Monthly Living Expenses setting $9,000.00/mo · starting living budget $9,500.00/mo (scheduled phases or an early-spending boost apply at the start).",
	}
	line := regexp.MustCompile(`(?s)<p data-spending-budget[^>]*>(.*?)</p>`)
	for _, card := range articles {
		idm := regexp.MustCompile(`data-spending-apply="([^"]+)"`).FindStringSubmatch(card)
		if idm == nil {
			t.Fatalf("card without apply id: %s", truncate(card, 300))
		}
		id := idm[1]
		m := line.FindStringSubmatch(card)
		if m == nil {
			t.Errorf("%s: no budget line on the card face", id)
			continue
		}
		if got := strings.TrimSpace(m[1]); got != want[id] {
			t.Errorf("%s budget line = %q, want %q", id, got, want[id])
		}
		// On the face: after the name, before the headline figure and the
		// details expander.
		h5 := strings.Index(card, "</h5>")
		pos := strings.Index(card, "<p data-spending-budget")
		details := strings.Index(card, "<details")
		figure := strings.Index(card, "/month")
		if !(h5 >= 0 && pos > h5 && details > pos && figure > pos) {
			t.Errorf("%s: budget line position h5=%d line=%d figure=%d details=%d, want h5 < line < figure < details", id, h5, pos, figure, details)
		}
		if d := card[details:]; strings.Contains(d, marker) {
			t.Errorf("%s: budget line also rendered inside Supporting details", id)
		}
	}
	// The frontier tables carry only the base figure, no budget line.
	for _, tbl := range regexp.MustCompile(`(?s)<table[^>]*data-spending-frontier.*?</table>`).FindAllString(body, -1) {
		if strings.Contains(tbl, marker) {
			t.Errorf("frontier table renders the card budget line")
		}
	}
	// One formatter: the current card's base equals its formatted value on
	// the face; the frontier rows for the searched candidates show the same
	// base string as their cards.
	for _, base := range []string{"$8,500.00", "$9,000.00", "$7,639.34"} {
		if !strings.Contains(body, `font-semibold">`+base+`<span`) {
			t.Errorf("frontier row base %s not found", base)
		}
	}
}

// Ruling 2026-09-18a: the differ/same wording is decided on the SAME
// rendered strings the card shows, so the sentence can never say "the same"
// over two different figures (or "differ" over two identical ones). The
// half-cent boundary 7639.035 vs 7639.04 is where a separate math.Round
// comparison disagreed with FormatMoney's %.2f.
func TestSpendingCardFaceBudgetWordingMatchesRenderedFigures(t *testing.T) {
	cases := []struct {
		base, start float64
		want        string
	}{
		{7639.035, 7639.04, "Monthly Living Expenses setting $7,639.03/mo · starting living budget $7,639.04/mo (scheduled phases or an early-spending boost apply at the start)."},
		{8000, 8000.004, "Monthly Living Expenses setting $8,000.00/mo · starting living budget $8,000.00/mo (the same at the start)."},
		{8000.004, 8000.006, "Monthly Living Expenses setting $8,000.00/mo · starting living budget $8,000.01/mo (scheduled phases or an early-spending boost apply at the start)."},
		{7639.342517162467, 9639.342517162467, "Monthly Living Expenses setting $7,639.34/mo · starting living budget $9,639.34/mo (scheduled phases or an early-spending boost apply at the start)."},
	}
	same := regexp.MustCompile(`setting (\$[\d,.]+)/mo · starting living budget (\$[\d,.]+)/mo \((the same|scheduled)`)
	for _, c := range cases {
		row := spendingOptimizerRow{Candidate: models.SpendingCandidate{BaseMonthlyLivingExpenses: c.base, StartingMonthlyLivingReal: c.start}}
		populateSpendingRowPlanEvidence(&row)
		if row.StartingBudgetEvidence != c.want {
			t.Errorf("(%v, %v) = %q, want %q", c.base, c.start, row.StartingBudgetEvidence, c.want)
		}
		m := same.FindStringSubmatch(row.StartingBudgetEvidence)
		if m == nil {
			t.Fatalf("unparseable sentence %q", row.StartingBudgetEvidence)
		}
		if (m[1] == m[2]) != (m[3] == "the same") {
			t.Errorf("(%v, %v): wording %q contradicts figures %s vs %s", c.base, c.start, m[3], m[1], m[2])
		}
	}
}
