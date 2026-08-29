#!/usr/bin/env bash
# Oracle for W2 (tier 3, escalated after two failed attempts) — ONE dollar
# string per value, everywhere. Server-rendered aria-valuetext, the visible
# display spans, and every whole-dollar preview derived from
# monthly_living_expenses must agree on a single canonical formatting rule:
# round half-away-from-zero to whole dollars, thousands separators, leading $.
# Run with cwd set to the tree under test.
#
# History this oracle exists to kill:
#   attempt 1: browser step-snapping made display/AT/saved values diverge.
#   attempt 2: three formatters — Go %.0f (half-even), JS Math.round
#              (half-away), JS toLocaleString (no rounding) — produced three
#              different strings for one value (7386.50).
# The canonical rule is pinned HERE, behaviorally, on the server-rendered
# output; the JS side must match it (checkers verify the live JS paths).
set -u
PKG=internal/handlers/whatif
PLANTED="$PKG/zz_oracle_w2_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package whatif

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
)

// zzCanonWhole is the pinned rule: half-away-from-zero to whole dollars,
// commas, leading $. (Living expenses are non-negative by construction.)
func zzCanonWhole(v float64) string {
	n := int64(math.Floor(v + 0.5))
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return "$" + b.String()
}

func zzSettings(v float64) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = v
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.1}},
	}
	return s
}

var zzAriaRe = regexp.MustCompile(`aria-valuetext="([^"]*)"`)

func zzDisplayAfter(t *testing.T, html, anchor string) string {
	t.Helper()
	idx := strings.Index(html, anchor)
	if idx < 0 {
		t.Fatalf("anchor %q not found", anchor)
	}
	m := regexp.MustCompile(`\$[\d,]+(?:\.\d+)?`).FindString(html[idx:])
	if m == "" {
		t.Fatalf("no dollar text after anchor %q", anchor)
	}
	return m
}

// Every server-rendered string for the living-expenses value must equal the
// canonical whole-dollar form — including the .50 ties %.0f gets wrong.
func TestOracleW2CanonicalValueStrings(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	values := []float64{7386, 7386.50, 44319.0 / 6.0, 7386.555, 10000.0 / 7.0}
	for _, v := range values {
		want := zzCanonWhole(v)
		s := zzSettings(v)
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("render portfolio-settings (v=%v): %v", v, err)
		}
		arias := zzAriaRe.FindAllStringSubmatch(out, -1)
		if len(arias) == 0 {
			t.Fatalf("v=%v: no aria-valuetext in portfolio-settings", v)
		}
		for _, a := range arias {
			if a[1] != want {
				t.Errorf("v=%v: portfolio-settings aria-valuetext = %q, want %q", v, a[1], want)
			}
		}
		if got := zzDisplayAfter(t, out, `id="monthly_living_expenses_display"`); got != want {
			t.Errorf("v=%v: display span = %q, want %q", v, got, want)
		}

		qa, err := renderer.RenderToString("whatif-quick-adjust-portfolio-content", map[string]any{
			"Settings": s,
		})
		if err != nil {
			t.Fatalf("render quick-adjust portfolio content (v=%v): %v", v, err)
		}
		for _, a := range zzAriaRe.FindAllStringSubmatch(qa, -1) {
			if a[1] != want {
				t.Errorf("v=%v: quick-adjust mirror aria-valuetext = %q, want %q", v, a[1], want)
			}
		}
		if idx := strings.Index(qa, `data-quick-adjust-display="monthly_living_expenses"`); idx >= 0 {
			if got := zzDisplayAfter(t, qa, `data-quick-adjust-display="monthly_living_expenses"`); got != want {
				t.Errorf("v=%v: quick-adjust display = %q, want %q", v, got, want)
			}
		} else {
			t.Fatalf("v=%v: quick-adjust display span not found", v)
		}
	}
}

// The per-phase dollar previews derive from the same value and must use the
// same rule — 7386 x 1.25 = 9232.50 rounds AWAY to $9,233, with separators,
// matching what the JS side computes.
func TestOracleW2PhaseDollarLabels(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := zzSettings(7386)
	s.SpendingPhaseConfig.Phases = []models.SpendingPhase{
		{Name: "Go-Go", StartAge: 0, Multiplier: 1.25},
	}
	out, err := renderer.RenderToString("whatif-quick-adjust-phases-content", map[string]any{
		"Settings": s,
	})
	if err != nil {
		t.Fatalf("render phases content: %v", err)
	}
	if !strings.Contains(out, "$9,233/mo") {
		t.Errorf("phase-dollar label: want %q in output, got: %s", "$9,233/mo", out)
	}
}
GO

go test -count=1 -run 'TestOracleW2' ./internal/handlers/whatif/
ck "01-canonical-strings" 0 "$?"

# Structural single-formatter contract in the shared JS: exactly one
# whole-dollar formatting function; no stray toLocaleString outside it.
QAS=web/templates/components/whatif/quick-adjust-scripts.html
DEFS=$(grep -c 'function formatWholeDollars(' "$QAS" || true)
ck "02-js-one-formatter-defined" 1 "$DEFS"
STRAYS=$(grep -o 'toLocaleString' "$QAS" | wc -l | tr -d ' ')
ck "03-js-single-toLocaleString" 1 "$STRAYS"
PS=web/templates/components/whatif/portfolio-settings.html
# Bare .toLocaleString() is the whole-dollar formatter that must route
# through formatWholeDollars; the cents-formatting note update
# (toLocaleString(undefined, {...})) is a different, allowed format.
PS_BARE=$(grep -o 'toLocaleString()' "$PS" | wc -l | tr -d ' ')
ck "04a-portfolio-no-bare-toLocaleString" 0 "$PS_BARE"
PS_CALLS=$(grep -c 'formatWholeDollars(' "$PS" || true)
if (( PS_CALLS >= 1 )); then echo "CHECK 04b-portfolio-uses-shared-formatter: PASS"; PASSN=$((PASSN+1));
else echo "CHECK 04b-portfolio-uses-shared-formatter: FAIL (want >=1 call, got $PS_CALLS)"; FAILN=$((FAILN+1)); fi

# Attempt-4 additions (user ruling 2026-08-29c): locale-pinned formatting.
# A locale-less toLocaleString (bare or with `undefined` as the locale)
# resolves to the BROWSER's locale and rewrites the server's canonical
# string on load (de-DE: $7,386 -> $7.386). Every dollar-formatting
# toLocaleString in the whatif templates must pass a fixed locale.
LOCALELESS=$(grep -o 'toLocaleString()' web/templates/components/whatif/*.html | wc -l | tr -d ' ')
ck "08-no-bare-toLocaleString-anywhere" 0 "$LOCALELESS"
UNDEF=$(grep -o 'toLocaleString(undefined' web/templates/components/whatif/*.html | wc -l | tr -d ' ')
ck "09-no-undefined-locale" 0 "$UNDEF"
RA=$(grep -o 'toLocaleString' web/templates/components/whatif/rate-assumptions.html | wc -l | tr -d ' ')
ck "10-rate-assumptions-routes-through-shared-formatter" 0 "$RA"

# Attempt-4 addition: the spending-phases phase-dollar label was proven
# correct but unguarded (mutating it to formatNumber left everything
# green). Guard it here.
cat > "$PKG/zz_oracle_w2b_test.go" <<'GO2'
package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestOracleW2SpendingPhasesDollarLabel(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.25}},
	}
	out, err := renderer.RenderToString("whatif-spending-phases", map[string]any{
		"Settings": s,
	})
	if err != nil {
		t.Fatalf("render whatif-spending-phases: %v", err)
	}
	if !strings.Contains(out, "$9,233/mo") {
		t.Errorf("spending-phases phase-dollar label: want %q (half-away of 9232.50, comma-grouped), output: %s", "$9,233/mo", out)
	}
}
GO2
cleanup() { rm -f "$PLANTED" "$PKG/zz_oracle_w2b_test.go"; }
trap cleanup EXIT
go test -count=1 -run 'TestOracleW2SpendingPhases' ./internal/handlers/whatif/
ck "11-spending-phases-label-guarded" 0 "$?"

go test -count=1 ./internal/handlers/whatif/ >/dev/null 2>&1
ck "05-package-suite" 0 "$?"
go build ./... >/dev/null 2>&1; ck "06-build" 0 "$?"
go vet ./...   >/dev/null 2>&1; ck "07-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 )) || exit 1
echo "ORACLE PASS"
