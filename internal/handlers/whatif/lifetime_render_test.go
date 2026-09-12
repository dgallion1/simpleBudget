package whatif

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
)

func renderedMoneyAfter(t *testing.T, html, label string) float64 {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(label) + `(?s).*?<td[^>]*>\s*\$?(-?[0-9,]+\.[0-9]{2})`)
	m := re.FindStringSubmatch(html)
	if len(m) != 2 {
		t.Fatalf("money after %q not found", label)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLifetimeRenderedHouseholdAndAccountEquationsUseDisplayedStrings(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "cash", Name: "Named reserve"}}}
	y := models.AggregateLifetimeYear([]models.LifetimeMonthOutcome{{Year: 2026, Month: 1, ExternalIncome: .004, Accounts: []models.AccountReconciliation{{AccountID: "cash", AccountName: "Named reserve", Opening: .004, Deposits: .004, Closing: .008}}}})
	a := &models.WhatIfAnalysis{Settings: s, Projection: &models.ProjectionResult{LifetimeYearSummaries: []models.LifetimeYearSummary{y}}}
	out, err := renderer.RenderToString("whatif-lifetime-year", buildResultsPartialData(s, a, nil))
	if err != nil {
		t.Fatal(err)
	}
	opening := renderedMoneyAfter(t, out, "Opening household wealth")
	income := renderedMoneyAfter(t, out, "External gross income")
	employer := renderedMoneyAfter(t, out, "Employer additions")
	ret := renderedMoneyAfter(t, out, "Investment return")
	consumption := renderedMoneyAfter(t, out, "Consumption paid")
	tax := renderedMoneyAfter(t, out, "Actual taxes paid")
	adjustment := renderedMoneyAfter(t, out, "Rounding adjustment")
	closing := renderedMoneyAfter(t, out, "Closing household wealth")
	if math.Abs(opening+income+employer+ret-consumption-tax+adjustment-closing) > .000001 {
		t.Fatalf("rendered household equation fails: %.2f+%.2f+%.2f+%.2f-%.2f-%.2f+%.2f != %.2f", opening, income, employer, ret, consumption, tax, adjustment, closing)
	}
	row := regexp.MustCompile(`Named reserve(?s).*?<td[^>]*>\$?([0-9,]+\.[0-9]{2})</td><td[^>]*>\$?([0-9,]+\.[0-9]{2})</td><td[^>]*>\$?([0-9,]+\.[0-9]{2})</td><td[^>]*>\$?([0-9,]+\.[0-9]{2})</td><td[^>]*>.*?\$?(-?[0-9,]+\.[0-9]{2}).*?</td><td[^>]*>\$?([0-9,]+\.[0-9]{2})</td>`).FindStringSubmatch(out)
	if len(row) != 7 {
		t.Fatalf("named account rendered row not parsed: %s", out)
	}
	values := make([]float64, 6)
	for i := range values {
		values[i], err = strconv.ParseFloat(strings.ReplaceAll(row[i+1], ",", ""), 64)
		if err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(values[0]+values[1]-values[2]+values[3]+values[4]-values[5]) > .000001 {
		t.Fatalf("rendered account equation fails: %v", values)
	}
}
