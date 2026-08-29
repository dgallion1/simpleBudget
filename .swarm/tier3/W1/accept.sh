#!/usr/bin/env bash
# Oracle for W1 (tier 3, escalated after two failed attempts) — the Living
# Expenses phase sub-rows must satisfy the DISPLAYED-sum identity for every
# reachable input, not just fixture-friendly ones. Ruling 2026-08-29b.
# Run with cwd set to the tree under test.
#
# History this oracle exists to kill:
#   attempt 1: adjustment computed from unrounded floats; independent %.2f
#              rendering broke the displayed identity (7386.555 case).
#   attempt 2: base/adjustment rounded via math.Round(x*100)/100 but the row
#              total still rendered via %.2f of the raw engine value; the two
#              rounding algorithms disagree at FP ties (9999.99/7 x0.5 case).
# The property, stated once: parse the THREE rendered dollar strings of the
# Living Expenses row; centsBase + centsAdjustment == centsTotal. Always.
set -u
PKG=internal/templates
PLANTED="$PKG/zz_oracle_w1_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package templates

import (
	"io/fs"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"budget2/web"
)

var zzOracleDollarRe = regexp.MustCompile(`([+-]?)\$([\d,]+\.\d{2})`)

func zzOracleCentsAfter(t *testing.T, html, label string) (int64, bool) {
	t.Helper()
	idx := strings.Index(html, label)
	if idx < 0 {
		return 0, false
	}
	m := zzOracleDollarRe.FindStringSubmatch(html[idx:])
	if m == nil {
		t.Fatalf("label %q present but no dollar figure follows", label)
	}
	num := strings.ReplaceAll(m[2], ",", "")
	parts := strings.SplitN(num, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", num, err)
	}
	frac, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", num, err)
	}
	cents := whole*100 + frac
	if m[1] == "-" {
		cents = -cents
	}
	return cents, true
}

func zzOracleRender(t *testing.T, base, mult float64) string {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = base
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: mult}},
	}
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	result := analysis.BudgetFit(in, nil)
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	html, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Settings": s,
		"Analysis": &models.WhatIfAnalysis{BudgetFit: result},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return html
}

func zzOracleAssertIdentity(t *testing.T, base, mult float64) {
	t.Helper()
	html := zzOracleRender(t, base, mult)
	total, ok := zzOracleCentsAfter(t, html, "Living Expenses")
	if !ok {
		t.Fatalf("base=%v mult=%v: no Living Expenses row rendered", base, mult)
	}
	bc, ok := zzOracleCentsAfter(t, html, "Base (slider setting)")
	if !ok {
		t.Fatalf("base=%v mult=%v: sub-rows missing (multiplier != 1.0, phases enabled)", base, mult)
	}
	// The adjustment sub-row label carries the phase name; anchor on it.
	ac, ok := zzOracleCentsAfter(t, html, "Go-Go phase")
	if !ok {
		t.Fatalf("base=%v mult=%v: adjustment sub-row missing", base, mult)
	}
	if bc+ac != total {
		t.Errorf("base=%v mult=%v: DISPLAYED %d + %d = %d cents, want displayed total %d",
			base, mult, bc, ac, bc+ac, total)
	}
}

// The two checker counterexamples plus the original fixture, verbatim.
func TestOracleW1KnownCounterexamples(t *testing.T) {
	zzOracleAssertIdentity(t, 7386.555, 1.1)          // attempt-1 class
	zzOracleAssertIdentity(t, 9999.99/7.0, 0.5)       // attempt-2 class (FP tie)
	zzOracleAssertIdentity(t, 0.01, 1.5)              // attempt-2 toy tie
	zzOracleAssertIdentity(t, 44319.00/6.0, 1.1)      // .50 remainder
	zzOracleAssertIdentity(t, 7386, 1.1)              // the live plan's shape
}

// Deterministic sweep over Sync-from-Dashboard-shaped bases (total/months)
// and plausible UI multipliers. Seeded — never time- or entropy-dependent.
func TestOracleW1SweepDisplayedIdentity(t *testing.T) {
	rng := rand.New(rand.NewSource(20260829))
	mults := []float64{0.5, 0.8, 0.9, 1.05, 1.1, 1.15, 1.25, 1.5}
	for i := 0; i < 400; i++ {
		totalCents := rng.Int63n(20000000) + 100 // $1.00 .. $200,000.00
		months := rng.Int63n(23) + 1             // 1..23 months
		base := (float64(totalCents) / 100.0) / float64(months)
		zzOracleAssertIdentity(t, base, mults[i%len(mults)])
	}
}

// Exact figures for the live plan's integer base must be unchanged.
func TestOracleW1LivePlanFigures(t *testing.T) {
	html := zzOracleRender(t, 7386, 1.1)
	for _, want := range []string{"$8,124.60", "$7,386.00", "$738.60"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered panel missing %q", want)
		}
	}
}

// Sub-rows must be absent when phases are disabled or the multiplier is 1.0.
func TestOracleW1AbsenceCases(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386.555
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false,
		Phases: []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.1}}}
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	result := analysis.BudgetFit(in, nil)
	for _, item := range result.ExpenseBreakdown {
		if len(item.SubItems) != 0 {
			t.Errorf("phases disabled: %q has sub-items", item.Name)
		}
	}
	html := zzOracleRender(t, 7386.555, 1.0)
	if strings.Contains(html, "Base (slider setting)") {
		t.Error("multiplier 1.0: sub-rows rendered")
	}
}
GO

go test -count=1 -run 'TestOracleW1' ./internal/templates/
ck "01-displayed-identity" 0 "$?"

go test -count=1 ./internal/services/retirement/analysis/ ./internal/templates/ >/dev/null 2>&1
ck "02-package-suites" 0 "$?"

go build ./... >/dev/null 2>&1; ck "03-build" 0 "$?"
go vet ./...   >/dev/null 2>&1; ck "04-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 )) || exit 1
echo "ORACLE PASS"
