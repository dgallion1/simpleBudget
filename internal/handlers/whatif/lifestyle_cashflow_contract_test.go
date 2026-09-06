package whatif

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
)

func lifestyleContractRenderedCents(t *testing.T, block, label string) int64 {
	t.Helper()
	m := regexp.MustCompile(`(?s)>` + regexp.QuoteMeta(label) + `</(?:span|p)>\s*<(?:span|p)[^>]*>\s*([+-]?\$[0-9,.]+)`).FindStringSubmatch(block)
	if m == nil {
		t.Fatalf("missing rendered row %q", label)
	}
	raw := strings.NewReplacer("$", "", ",", "", ".", "", "+", "").Replace(m[1])
	got, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", m[1], err)
	}
	return got
}

func lifestyleContractDisplayedGap(t *testing.T, block string) int64 {
	t.Helper()
	m := regexp.MustCompile(`(?s)>Needed from portfolio after estimated taxes and RMDs</p>\s*<p[^>]*>\s*([+]?\$[0-9,.]+)`).FindStringSubmatch(block)
	if m == nil {
		t.Fatal("missing displayed gap")
	}
	got := lifestyleContractMoneyStringCents(t, m[1])
	if strings.HasPrefix(m[1], "+") {
		return -got
	}
	return got
}

func lifestyleContractMoneyStringCents(t *testing.T, displayed string) int64 {
	t.Helper()
	raw := strings.NewReplacer("$", "", ",", "", ".", "", "+", "").Replace(displayed)
	got, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", displayed, err)
	}
	return got
}

func TestLifestyleVisibleIRMAANotDoubleCounted(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	bf := &models.BudgetFitAnalysis{
		MonthlyExpenses: 102.005, MonthlyIncome: 40.005, MonthlyRMD: 10.005,
		MonthlyTaxes: 5.005, MonthlyIRMAA: 2.005, MonthlyNIIT: 1.005,
		MonthlyGap:     102.005 - 40.005 - 10.005 + 5.005,
		HasSteadyState: true, SteadyStateYear: 12,
		SteadyStateExpenses: 203.005, SteadyStateIncome: 80.005, SteadyStateRMD: 20.005,
		SteadyStateTaxes: 11.005, SteadyStateIRMAA: 3.005, SteadyStateNIIT: 2.005,
		SteadyStateGap: 203.005 - 80.005 - 20.005 + 11.005,
	}
	out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		"Settings": models.DefaultWhatIfSettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.SplitN(out, `<div class="pt-4">`, 2)
	if len(blocks) != 2 {
		t.Fatal("missing current/future blocks")
	}
	current, future := blocks[0], blocks[1]

	currentSum := lifestyleContractRenderedCents(t, current, "Expenses") +
		lifestyleContractRenderedCents(t, current, "Taxes &amp; Deductions") -
		lifestyleContractRenderedCents(t, current, "Income") -
		lifestyleContractRenderedCents(t, current, "Required RMD") +
		lifestyleContractRenderedCents(t, current, "Rounding adjustment")
	if currentSum != lifestyleContractDisplayedGap(t, current) {
		t.Errorf("current visible totals plus adjustment = %d cents, gap = %d; IRMAA is included in both Expenses and Taxes & Deductions", currentSum, lifestyleContractDisplayedGap(t, current))
	}

	futureSum := lifestyleContractRenderedCents(t, future, "Monthly Expenses") +
		lifestyleContractRenderedCents(t, future, "Estimated Taxes") -
		lifestyleContractRenderedCents(t, future, "Monthly Income") -
		lifestyleContractRenderedCents(t, future, "Estimated RMD") +
		lifestyleContractRenderedCents(t, future, "Rounding adjustment")
	if strings.Contains(future, ">Estimated IRMAA</span>") {
		futureSum += lifestyleContractRenderedCents(t, future, "Estimated IRMAA")
	} else if !strings.Contains(future, "Included in Monthly Expenses: IRMAA") {
		t.Fatal("IRMAA row is neither additive nor explicitly marked as included in expenses")
	}
	if futureSum != lifestyleContractDisplayedGap(t, future) {
		t.Errorf("future visible component rows plus adjustment = %d cents, gap = %d; Estimated IRMAA is also already included in Monthly Expenses", futureSum, lifestyleContractDisplayedGap(t, future))
	}
}

func TestLifestyleSurplusAdjustmentLabel(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	bf := &models.BudgetFitAnalysis{
		MonthlyExpenses: 100.005, MonthlyIncome: 140.005,
		MonthlyRMD: 10.005, MonthlyTaxes: 5.005, MonthlyGap: -45,
	}
	out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		"Settings": models.DefaultWhatIfSettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ">Surplus<") {
		t.Fatal("fixture did not render as surplus")
	}
	if strings.Contains(out, `aria-label="Add $0.01 to the funding need"`) {
		t.Fatal("surplus adjustment is announced as an addition to a funding need")
	}
}
