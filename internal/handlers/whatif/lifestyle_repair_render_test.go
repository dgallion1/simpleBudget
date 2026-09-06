package whatif

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
)

// Catch risk coloring on event shares and both SS delta axes.
func TestLifestyleEventSharesNeutral(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	a := &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 100, MarketCrashCount: 95, SpendingShockCount: 55}}}
	out, err := renderer.RenderToString("whatif-monte-carlo", map[string]any{"Analysis": a})
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Market Crashes", "Spending Shocks"} {
		re := regexp.MustCompile(`(?s)<div class="([^"]*)">\s*<p[^>]*>` + label + `</p>\s*<p class="([^"]*)">([0-9.]+)% of runs</p>`)
		m := re.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("missing event share %s", label)
		}
		for _, bad := range []string{"positive", "negative", "warning", "text-red", "text-green"} {
			if strings.Contains(m[1]+" "+m[2], bad) {
				t.Errorf("%s still classified: %s", label, m[0])
			}
		}
	}
}

func TestLifestyleSSSurvivalDeltasNeutral(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	options := []models.SSPortfolioOption{{ClaimAge: 62, SurvivalRate: 95, DeltaSurvivalRate: 20}, {ClaimAge: 70, SurvivalRate: 55, DeltaSurvivalRate: -20}}
	a := &models.WhatIfAnalysis{SocialSecurity: &models.SSComparisonAnalysis{Portfolio: &models.SSPortfolioAnalysis{PrimaryOptions: options, SpouseOptions: options, BaselineSurvivalRate: 75}}}
	out, err := renderer.RenderToString("whatif-social-security-results", map[string]any{"Analysis": a, "Settings": models.DefaultWhatIfSettings()})
	if err != nil {
		t.Fatal(err)
	}
	cells := regexp.MustCompile(`(?s)<td class="([^"]*)">\s*([+-]20\.0)%\s*</td>`).FindAllStringSubmatch(out, -1)
	if len(cells) != 4 {
		t.Fatalf("want both signs on both axes, got %d", len(cells))
	}
	for _, cell := range cells {
		for _, bad := range []string{"positive", "negative", "warning", "text-red", "text-green"} {
			if strings.Contains(cell[1], bad) {
				t.Errorf("survival delta classified: %s", cell[0])
			}
		}
	}
}

// Only rendered strings feed the arithmetic oracle; no production rounding helper is used.
func TestLifestyleDisplayedBudgetCentsReconcile(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	cases := []struct {
		name                              string
		expenses, income, rmd, taxes, gap float64
		adjustment                        string
	}{
		{"positive residual", 100.005, 40.005, 10.005, 5.005, 55, "+$0.02"},
		{"negative residual", 140.005, 100.005, 5.005, 10.005, 45, "-$0.01"},
		{"surplus positive residual", 100.005, 140.005, 10.005, 5.005, -45, "+$0.01"},
		{"surplus negative residual", 40.005, 100.005, 5.005, 10.005, -55, "-$0.02"},
		{"half-cent zero residual", 100.005, 40.005, 5.005, 10.005, 65, ""},
		{"balanced", 100, 95, 10, 5, 0, ""},
		{"below half cent", 100.0049, 40.0049, 10.0049, 5.0049, 55, ""},
		{"above half cent", 100.0051, 40.0051, 10.0051, 5.0051, 55, ""},
	}
	for _, tc := range cases {
		for _, future := range []bool{false, true} {
			mode := "current"
			if future {
				mode = "future"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				bf := &models.BudgetFitAnalysis{MonthlyExpenses: 200, MonthlyIncome: 50, MonthlyRMD: 10, MonthlyTaxes: 20, MonthlyGap: 160, HasSteadyState: true, SteadyStateYear: 12, SteadyStateExpenses: 300, SteadyStateIncome: 60, SteadyStateRMD: 20, SteadyStateTaxes: 30, SteadyStateGap: 250}
				// Informational subcomponents must not be counted again.
				bf.MonthlyIRMAA, bf.SteadyStateIRMAA = 2.005, 3.005
				bf.MonthlyNIIT, bf.SteadyStateNIIT = 1.005, 2.005
				if future {
					bf.SteadyStateExpenses, bf.SteadyStateIncome, bf.SteadyStateRMD, bf.SteadyStateTaxes, bf.SteadyStateGap = tc.expenses, tc.income, tc.rmd, tc.taxes, tc.gap
				} else {
					bf.MonthlyExpenses, bf.MonthlyIncome, bf.MonthlyRMD, bf.MonthlyTaxes, bf.MonthlyGap = tc.expenses, tc.income, tc.rmd, tc.taxes, tc.gap
				}
				originalCurrent, originalFuture := bf.MonthlyGap, bf.SteadyStateGap
				out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{"Analysis": &models.WhatIfAnalysis{BudgetFit: bf}, "Settings": models.DefaultWhatIfSettings()})
				if err != nil {
					t.Fatal(err)
				}
				blocks := strings.SplitN(out, `<div class="pt-4">`, 2)
				if len(blocks) != 2 {
					t.Fatal("missing selected-year block")
				}
				block := blocks[0]
				expensesLabel, incomeLabel, rmdLabel := "Expenses", "Income", "Required RMD"
				if future {
					block = blocks[1]
					expensesLabel, incomeLabel, rmdLabel = "Monthly Expenses", "Monthly Income", "Estimated RMD"
				}
				row := func(label string) string {
					t.Helper()
					m := regexp.MustCompile(`(?s)>` + regexp.QuoteMeta(label) + `</span>\s*<span[^>]*>([+-]?\$[0-9,.]+)`).FindStringSubmatch(block)
					if m == nil {
						t.Fatalf("missing row %s", label)
					}
					return m[1]
				}
				taxLabel := "Taxes &amp; Deductions"
				if future {
					taxLabel = "Estimated Taxes"
				}
				sum := lifestyleRenderedCents(t, row(expensesLabel)) + lifestyleRenderedCents(t, row(taxLabel)) - lifestyleRenderedCents(t, row(incomeLabel)) - lifestyleRenderedCents(t, row(rmdLabel))
				if !strings.Contains(block, ">Included in Monthly Expenses: IRMAA</span>") {
					t.Error("IRMAA must visibly identify its inclusion in expenses")
				}
				if tc.adjustment != "" {
					adjustment := row("Rounding adjustment")
					if adjustment != tc.adjustment {
						t.Errorf("adjustment %s want %s", adjustment, tc.adjustment)
					}
					sum += lifestyleRenderedCents(t, adjustment)
					direction, preposition := "Add", "to"
					if strings.HasPrefix(tc.adjustment, "-") {
						direction, preposition = "Subtract", "from"
					}
					wantARIA := `aria-label="` + direction + " " + tc.adjustment[1:] + " " + preposition + ` the displayed cash-flow total"`
					if !strings.Contains(block, wantARIA) {
						t.Errorf("missing sign-correct neutral announcement %s", wantARIA)
					}
				} else if strings.Contains(block, "Rounding adjustment") {
					t.Error("zero residual must omit adjustment")
				}
				if future {
					// Sum every visible monetary row in the selected-year breakdown.
					// Only visibly included subcomponents are informational; any other
					// peer row contributes, so an unlabeled IRMAA cannot escape the oracle.
					breakdown := strings.SplitN(block, `<div class="grid grid-cols-2 gap-4 text-center">`, 2)[0]
					rows := regexp.MustCompile(`(?s)<span[^>]*>([^<]+)</span>\s*<span[^>]*>([+-]?\$[0-9,.]+)`).FindAllStringSubmatch(breakdown, -1)
					if len(rows) < 6 {
						t.Fatalf("incomplete future breakdown: %d monetary rows", len(rows))
					}
					sum = 0
					for _, visible := range rows {
						label := strings.TrimSpace(visible[1])
						cents := lifestyleRenderedCents(t, visible[2])
						switch label {
						case "Monthly Income", "Estimated RMD":
							sum -= cents
						case "Includes NIIT", "Includes State Tax", "Included in Monthly Expenses: IRMAA":
							// The visible wording tells readers these amounts are included.
						default:
							sum += cents
						}
					}
				}
				gapMatch := regexp.MustCompile(`(?s)>Needed from portfolio after estimated taxes and RMDs</p>\s*<p[^>]*>\s*([+]?\$[0-9,.]+)`).FindStringSubmatch(block)
				if gapMatch == nil {
					t.Fatal("missing displayed gap")
				}
				gap := lifestyleRenderedCents(t, gapMatch[1])
				if strings.HasPrefix(gapMatch[1], "+") {
					gap = -gap
				}
				if sum != gap {
					t.Errorf("displayed components plus adjustment = %d cents, gap = %d", sum, gap)
				}
				if bf.MonthlyGap != originalCurrent || bf.SteadyStateGap != originalFuture {
					t.Error("render changed underlying gaps")
				}
			})
		}
	}
}

func lifestyleRenderedCents(t *testing.T, displayed string) int64 {
	t.Helper()
	amount := strings.NewReplacer("$", "", ",", "", ".", "", "+", "").Replace(displayed)
	cents, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		t.Fatalf("parse rendered money %q: %v", displayed, err)
	}
	return cents
}
