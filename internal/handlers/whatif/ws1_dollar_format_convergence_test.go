package whatif

import (
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
)

// WS1 attempt-3 contract, R-FMT: ONE whole-dollar rounding rule (Go
// formatNumber's %.0f, round-half-EVEN at an exact .50) for every surface
// that shows a stored value in whole dollars — slider display span, its
// aria-valuetext, the Quick Adjust mirror's display+aria, and the
// Coverage Timeline. The permanent round-trip tests in
// ws1_generic_roundtrip_test.go inspect FORM INPUTS (name/step/min/max);
// none of them look at a plain display <span>, so a reintroduced
// formatDollars (half-up) on a single display span was invisible to that
// suite — confirmed by hand: reverting one healthcare display span to
// formatDollars left every ws1_generic_roundtrip_test.go and
// ws1_value_fidelity_test.go test passing. These tests close that gap
// directly, at exact .50 ties where the two rules disagree.
//
// Every fixture value below has an EVEN integer floor (612, 2150, 3450,
// 10736, 2437512), so half-even (stays) and half-up (rounds up) produce
// DIFFERENT strings — the same class of tie the WS1 attempt-2 hard stop
// was about (a stored 1800.50 showing "$1,801" on the slider and "$1,800"
// in the Coverage Timeline on the same screen).

// wholeDollarUp is a "\d,\d{3}" style thousands-grouped even-tie value with
// its half-up neighbour, e.g. ("$612", "$613").
type wholeDollarTie struct {
	even, up string
}

// mustNotShow fails the test if `up` appears anywhere in html as a
// standalone whole-dollar figure (not merely as a prefix of a
// cents-precision figure elsewhere, e.g. "$613.00" would still match here,
// which is correct — no surface in this test set renders cents).
func assertNoUp(t *testing.T, html, label string, tie wholeDollarTie) {
	t.Helper()
	upPattern := regexp.MustCompile(regexp.QuoteMeta(tie.up) + `(?:[^0-9]|$)`)
	if upPattern.MatchString(html) {
		idx := upPattern.FindStringIndex(html)
		t.Errorf("%s: found half-up %q (want only half-even %q); context: %s",
			label, tie.up, tie.even, truncate(html[max0(idx[0]-80):idx[1]+40], 200))
	}
	if !strings.Contains(html, tie.even) {
		t.Errorf("%s: expected half-even %q not found anywhere in output", label, tie.even)
	}
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// TestWS1WholeDollarConvergence_HealthcareCard: current_monthly_cost,
// medicare_monthly_cost and aca_cost_after_employer at even-floor .50 ties,
// on an employer-coverage person whose Coverage Timeline renders all
// three fields in the "Employer -> ACA -> Medicare" branch — the exact
// branch WS1.2's checker-second found split from the slider/QA spans.
func TestWS1WholeDollarConvergence_HealthcareCard(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                    "hc-conv",
		Name:                  "Jordan",
		CurrentAge:            55,
		CurrentCoverage:       models.CoverageEmployer,
		CurrentMonthlyCost:    612.50, // even floor: half-even "$612", half-up "$613"
		EmployerCoverageYears: 3,
		ACACostAfterEmployer:  3450.50, // even floor: "$3,450" vs "$3,451"
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150.50, // even floor: "$2,150" vs "$2,151"
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}
	settings := models.DefaultWhatIfSettings()

	out, err := renderer.RenderToString("whatif-healthcare-person", map[string]any{
		"Settings": settings,
		"Person":   person,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Coverage Timeline") {
		t.Fatalf("fixture must exercise the Coverage Timeline branch; got: %s", truncate(out, 2000))
	}

	ties := map[string]wholeDollarTie{
		"current_monthly_cost":    {"$612", "$613"},
		"medicare_monthly_cost":   {"$2,150", "$2,151"},
		"aca_cost_after_employer": {"$3,450", "$3,451"},
	}
	for field, tie := range ties {
		assertNoUp(t, out, "whatif-healthcare-person ("+field+")", tie)
	}
}

// TestWS1WholeDollarConvergence_QuickAdjustHealthcare: the same three
// fields, rendered through the Quick Adjust healthcare tab (a separate
// template from the in-card one), against the SAME ties.
func TestWS1WholeDollarConvergence_QuickAdjustHealthcare(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.HealthcarePersons = []models.HealthcarePerson{{
		ID:                    "hc-conv-qa",
		Name:                  "Jordan",
		CurrentAge:            55,
		CurrentCoverage:       models.CoverageEmployer,
		CurrentMonthlyCost:    612.50,
		EmployerCoverageYears: 3,
		ACACostAfterEmployer:  3450.50,
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150.50,
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}}

	out, err := renderer.RenderToString("whatif-quick-adjust-healthcare-content", map[string]any{
		"Settings": settings,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	ties := []wholeDollarTie{{"$612", "$613"}, {"$2,150", "$2,151"}, {"$3,450", "$3,451"}}
	for _, tie := range ties {
		assertNoUp(t, out, "whatif-quick-adjust-healthcare-content", tie)
	}
}

// TestWS1WholeDollarConvergence_PortfolioAndQuickAdjust: portfolio_value
// and monthly_living_expenses at even-floor .50 ties, checked across BOTH
// the in-card portfolio-settings render and the Quick Adjust portfolio
// tab — these two templates are the ones the WS1.2k hard stop named
// explicitly (master's own pre-existing living-expenses split).
func TestWS1WholeDollarConvergence_PortfolioAndQuickAdjust(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 2437512.50      // even floor: "$2,437,512" vs "$2,437,513"
	s.MonthlyLivingExpenses = 10736.50 // even floor: "$10,736" vs "$10,737"

	portfolioTie := wholeDollarTie{"$2,437,512", "$2,437,513"}
	livingTie := wholeDollarTie{"$10,736", "$10,737"}

	out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
		"Settings":                s,
		"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
	})
	if err != nil {
		t.Fatalf("RenderToString(whatif-portfolio-settings): %v", err)
	}
	assertNoUp(t, out, "whatif-portfolio-settings (portfolio_value)", portfolioTie)
	assertNoUp(t, out, "whatif-portfolio-settings (monthly_living_expenses)", livingTie)

	qaOut, err := renderer.RenderToString("whatif-quick-adjust-portfolio-content", map[string]any{
		"Settings": s,
	})
	if err != nil {
		t.Fatalf("RenderToString(whatif-quick-adjust-portfolio-content): %v", err)
	}
	assertNoUp(t, qaOut, "whatif-quick-adjust-portfolio-content (portfolio_value)", portfolioTie)
	assertNoUp(t, qaOut, "whatif-quick-adjust-portfolio-content (monthly_living_expenses)", livingTie)
}

// TestWS1WholeDollarConvergence_TimelineEmployerDirectToMedicare covers the
// Coverage Timeline's OTHER employer branch — "Employer -> Medicare"
// directly (no ACA segment), rendered when employer coverage runs past
// Medicare eligibility. WS1.3 checker-tests (F5/F6) found this branch (and
// its 3-way sibling covered by TestWS1WholeDollarConvergence_HealthcareCard
// above) could regress to a half-up formatter with nothing catching it —
// each Timeline branch needs its OWN tie fixture, not just one of the two.
func TestWS1WholeDollarConvergence_TimelineEmployerDirectToMedicare(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                    "hc-conv-direct",
		Name:                  "Robin",
		CurrentAge:            63,
		CurrentCoverage:       models.CoverageEmployer,
		CurrentMonthlyCost:    1800.50, // even floor: "$1,800" vs "$1,801"
		EmployerCoverageYears: 2,       // 63+2 = 65 >= MedicareEligibleAge: the DIRECT branch
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   612.50, // even floor: "$612" vs "$613"
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}
	settings := models.DefaultWhatIfSettings()

	out, err := renderer.RenderToString("whatif-healthcare-person", map[string]any{
		"Settings": settings,
		"Person":   person,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Coverage Timeline") || !strings.Contains(out, "Employer $") {
		t.Fatalf("fixture must exercise the direct Employer->Medicare Timeline branch; got: %s", truncate(out, 2000))
	}
	if strings.Contains(out, "ACA $") {
		t.Fatalf("fixture rendered the 3-way branch, not the direct one; got: %s", truncate(out, 2000))
	}

	assertNoUp(t, out, "whatif-healthcare-person Timeline (current_monthly_cost, direct branch)", wholeDollarTie{"$1,800", "$1,801"})
	assertNoUp(t, out, "whatif-healthcare-person Timeline (medicare_monthly_cost, direct branch)", wholeDollarTie{"$612", "$613"})
}

// TestWS1WholeDollarConvergence_TimelineACAToMedicare covers the Coverage
// Timeline's THIRD branch — a non-employer, non-Medicare person (ACA
// coverage here) counting down to Medicare eligibility — the
// "{{.CurrentCoverage.Label}} $... / Medicare $..." branch, distinct from
// both employer branches above.
func TestWS1WholeDollarConvergence_TimelineACAToMedicare(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                    "hc-conv-aca-timeline",
		Name:                  "Casey",
		CurrentAge:            60,
		CurrentCoverage:       models.CoverageACA,
		CurrentMonthlyCost:    3450.50, // even floor: "$3,450" vs "$3,451"
		PreMedicareInflation:  7.25,
		MedicareMonthlyCost:   2150.50, // even floor: "$2,150" vs "$2,151"
		PostMedicareInflation: 4.15,
		MedicareEligibleAge:   65,
	}
	settings := models.DefaultWhatIfSettings()

	out, err := renderer.RenderToString("whatif-healthcare-person", map[string]any{
		"Settings": settings,
		"Person":   person,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Coverage Timeline") || !strings.Contains(out, "ACA $") {
		t.Fatalf("fixture must exercise the ACA->Medicare Timeline branch; got: %s", truncate(out, 2000))
	}

	assertNoUp(t, out, "whatif-healthcare-person Timeline (current_monthly_cost, ACA->Medicare branch)", wholeDollarTie{"$3,450", "$3,451"})
	assertNoUp(t, out, "whatif-healthcare-person Timeline (medicare_monthly_cost, ACA->Medicare branch)", wholeDollarTie{"$2,150", "$2,151"})
}

// TestWS1WholeDollarConvergence_PhaseDollarLabels covers the phase-dollar
// preview label at BOTH its render sites — the in-card
// "whatif-spending-phases" card and the Quick Adjust "phases" tab — at an
// even-floor .50 tie (multiplier x living-expenses). WS1.3 checker-tests
// (F2/F3) found this label could regress to a half-up formatter (Go
// formatDollars, or an inline JS Math.round bypassing formatWholeDollars)
// with nothing at the template level catching it.
func TestWS1WholeDollarConvergence_PhaseDollarLabels(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7202 // 7202 x 0.25 = 1800.50 -- even floor
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 0.25, Description: "Active retirement"}},
	}
	tie := wholeDollarTie{"$1,800", "$1,801"}

	inCard, err := renderer.RenderToString("whatif-spending-phases", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString(whatif-spending-phases): %v", err)
	}
	if !strings.Contains(inCard, `data-quick-adjust-display-format="phase-dollar"`) {
		t.Fatalf("fixture must render the in-card phase-dollar label; got: %s", truncate(inCard, 2000))
	}
	assertNoUp(t, inCard, "whatif-spending-phases (phase-dollar)", tie)

	qa, err := renderer.RenderToString("whatif-quick-adjust-phases-content", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString(whatif-quick-adjust-phases-content): %v", err)
	}
	if !strings.Contains(qa, `data-quick-adjust-display-format="phase-dollar"`) {
		t.Fatalf("fixture must render the Quick Adjust phase-dollar label; got: %s", truncate(qa, 2000))
	}
	assertNoUp(t, qa, "whatif-quick-adjust-phases-content (phase-dollar)", tie)
}
