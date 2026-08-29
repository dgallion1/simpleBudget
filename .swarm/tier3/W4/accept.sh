#!/usr/bin/env bash
# Oracle for W4 (tier 3, escalated after two failed attempts) — the budget
# banner and the Budget KPI card are ONE surface: same bucket presence, same
# classification, no "$-0.00", no known-failing text class on the on-target
# row. Ruling 2026-08-29a. Run with cwd set to the tree under test.
#
# History this oracle exists to kill:
#   attempt 1: banner dead-band vs card bare-sign ($0.60 both clean and red).
#   attempt 2: phantom "Health: $0.00 of $0.00 on target" card row when only
#              living is configured; on-target branch class fails AA on
#              rose-50; "$-0.00" reachable in the headroom sentence.
# NOTE: the kpis template EMBEDS the verdict bar, so the kpis render contains
# the banner too — card-only presence is computed by count difference.
set -u
PKG=internal/handlers/dashboard
PLANTED="$PKG/zz_oracle_w4_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package dashboard

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func zzMetrics(livingDelta, hcDelta float64, hasLiving, hasHC bool) *models.DashboardMetrics {
	m := &models.DashboardMetrics{MonthsInRange: 1}
	combined := 0.0
	if hasLiving {
		m.BudgetTarget = 2000
		m.LivingTargetTotal = 2000
		m.HasBudgetTarget = true
		m.LivingExpensesTotal = 2000 + livingDelta
		m.ActualMonthly = 2000 + livingDelta
		m.PerMonthDelta = livingDelta
		m.CumulativeDelta = livingDelta
		combined += 2000
	}
	if hasHC {
		m.HealthcareTarget = 300
		m.HealthcareTargetTotal = 300
		m.HasHealthcareTarget = true
		m.HealthcareTotal = 300 + hcDelta
		m.HealthcareActual = 300 + hcDelta
		m.HealthcarePerMonthDelta = hcDelta
		m.HealthcareCumulativeDelta = hcDelta
		combined += 300
	}
	m.CombinedTarget = combined
	m.HasCombinedTarget = combined > 0
	m.CombinedActualMonthly = combined + livingDelta + hcDelta
	m.CombinedCumulativeDelta = livingDelta + hcDelta
	return m
}

func zzRenderBoth(t *testing.T, m *models.DashboardMetrics) (banner, kpis string) {
	t.Helper()
	v := BuildBudgetVerdict(m)
	banner, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
	if err != nil {
		t.Fatalf("render banner: %v", err)
	}
	kpis, err = renderer.RenderToString("kpis", map[string]any{"Metrics": m, "BudgetVerdict": v})
	if err != nil {
		t.Fatalf("render kpis: %v", err)
	}
	return banner, kpis
}

const (
	zzLivingNeedle = `Living: <span`
	zzHealthNeedle = `Health: <span` // card-only: the banner says "Healthcare:"
)

// cardLivingCount: occurrences in the kpis render beyond those contributed
// by the embedded banner.
func zzCardLivingCount(banner, kpis string) int {
	return strings.Count(kpis, zzLivingNeedle) - strings.Count(banner, zzLivingNeedle)
}

// A bucket appears in the card's Budget rows iff it is configured — the same
// rule the banner's decomposition applies. No phantom $0.00 rows.
func TestOracleW4BucketPresenceParity(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()
	cases := []struct {
		name             string
		hasLiving, hasHC bool
	}{
		{"both", true, true},
		{"living-only", true, false},
		{"healthcare-only", false, true},
	}
	for _, c := range cases {
		banner, kpis := zzRenderBoth(t, zzMetrics(-500, -40, c.hasLiving, c.hasHC))
		wantL, wantH := 0, 0
		if c.hasLiving {
			wantL = 1
		}
		if c.hasHC {
			wantH = 1
		}
		if got := zzCardLivingCount(banner, kpis); got != wantL {
			t.Errorf("%s: card Living rows = %d, want %d", c.name, got, wantL)
		}
		if got := strings.Count(kpis, zzHealthNeedle); got != wantH {
			t.Errorf("%s: card Health rows = %d, want %d", c.name, got, wantH)
		}
		if bannerL := strings.Contains(banner, "Living:"); bannerL != c.hasLiving {
			t.Errorf("%s: banner Living presence = %v, want %v", c.name, bannerL, c.hasLiving)
		}
		if bannerH := strings.Contains(banner, "Healthcare:"); bannerH != c.hasHC {
			t.Errorf("%s: banner Healthcare presence = %v, want %v", c.name, bannerH, c.hasHC)
		}
	}
}

// zzCardLivingSegment returns the card's Living row region (the LAST
// occurrence in the kpis render — the banner's copy comes first).
func zzCardLivingSegment(t *testing.T, kpis string) string {
	t.Helper()
	ci := strings.LastIndex(kpis, zzLivingNeedle)
	if ci < 0 {
		t.Fatal("card has no Living row")
	}
	end := ci + 400
	if end > len(kpis) {
		end = len(kpis)
	}
	return kpis[ci:end]
}

// Same classification wording on both surfaces at and around the dead-band.
func TestOracleW4DeadBandParity(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()
	for _, d := range []float64{-1.01, -1.00, -0.99, -0.60, 0.60, 0.99, 1.00, 1.01} {
		m := zzMetrics(d, 0, true, true)
		v := BuildBudgetVerdict(m)
		status := v.Living.Status
		banner, kpis := zzRenderBoth(t, m)
		bi := strings.Index(banner, "Living:")
		if bi < 0 {
			t.Fatalf("d=%v: banner has no Living entry", d)
		}
		bend := bi + 220
		if bend > len(banner) {
			bend = len(banner)
		}
		if bseg := banner[bi:bend]; !strings.Contains(bseg, "</span> "+status) {
			t.Errorf("d=%v: banner Living segment lacks %q: %s", d, "</span> "+status, bseg)
		}
		cseg := zzCardLivingSegment(t, kpis)
		if !strings.Contains(cseg, "</span> "+status) {
			t.Errorf("d=%v: card Living row lacks %q: %s", d, "</span> "+status, cseg)
		}
		for _, other := range []string{"over", "under", "on target"} {
			if other != status && strings.Contains(cseg, "</span> "+other) {
				t.Errorf("d=%v: card Living row shows %q, verdict says %q: %s", d, other, status, cseg)
			}
		}
	}
}

// An exactly-zero combined delta must never render $-0.00 anywhere.
func TestOracleW4NoNegativeZero(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()
	banner, kpis := zzRenderBoth(t, zzMetrics(0, 0, true, true))
	if strings.Contains(banner, "$-0.00") {
		t.Errorf("banner renders $-0.00")
	}
	if strings.Contains(kpis, "$-0.00") {
		t.Errorf("kpis renders $-0.00")
	}
}

// The no-target banner carries none of the headroom machinery.
func TestOracleW4NoTargetHasNoHeadroomText(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()
	banner, _ := zzRenderBoth(t, zzMetrics(0, 0, false, false))
	for _, s := range []string{"Living:", "Healthcare:", "headroom", "stay on plan", "on target"} {
		if strings.Contains(banner, s) {
			t.Errorf("no-target banner contains %q", s)
		}
	}
}

// The card's on-target row must not use light-mode gray-400/gray-500 (both
// measured under 4.5:1 on the card's tinted backgrounds). The dark: variants
// are fine and must not trip this.
func TestOracleW4OnTargetRowContrastClasses(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()
	_, kpis := zzRenderBoth(t, zzMetrics(0.60, 0, true, true))
	seg := zzCardLivingSegment(t, kpis)
	if !strings.Contains(seg, "on target") {
		t.Fatalf("fixture: Living row is not on target: %s", seg)
	}
	for _, bad := range []string{`"text-gray-500`, ` text-gray-500`, `"text-gray-400`, ` text-gray-400`} {
		if strings.Contains(seg, bad) {
			t.Errorf("on-target row uses %q (fails AA on tinted card backgrounds): %s", bad, seg)
		}
	}
}
GO

# Attempt-4 addition (user ruling 2026-08-29c): the Budget card's container
# tint and headline must consume the SAME classification as everything else
# rendering CombinedCumulativeDelta. At $0.60 (inside the $1 dead-band) the
# card previously painted rose while its own headline said "On budget".
cat > "$PKG/zz_oracle_w4b_test.go" <<'GO2'
package dashboard

import (
	"strings"
	"testing"
)

// html/template strips HTML comments, so these assertions use full-render
// class counts. The container tint pair (bg-rose-50 / border-rose-200 /
// bg-emerald-50) is used only by the Budget card container and the banner
// band; the per-month KPI cards use text-* colors on different figures and
// are deliberately out of scope (per the attempt-3 adjudication: per-month
// vs cumulative are different figures).
func TestOracleW4TintMatchesClassification(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	// Dead-band: $0.60 over in total -> headline "On budget", and NO danger
	// tint anywhere (banner is healthy at 0.60/2300 too).
	_, kpis := zzRenderBoth(t, zzMetrics(0.60, 0, true, true))
	if !strings.Contains(kpis, "On budget") {
		t.Errorf("dead-band: headline is not 'On budget'")
	}
	// Two KPI cards (Total Expenses, Monthly Healthcare) are PERMANENTLY
	// rose-tinted; the static baseline is exactly 2 of each tint class.
	// The Budget card and the embedded banner are the only dynamic users.
	for _, rose := range []string{"bg-rose-50", "border-rose-200"} {
		if n := strings.Count(kpis, rose); n != 2 {
			t.Errorf("dead-band: %d occurrence(s) of %q, want exactly the 2 static cards (Budget card/banner must not be rose while the classification says on-budget)", n, rose)
		}
	}

	// Clearly over: the danger tint must appear and the headline must say over.
	_, kpisOver := zzRenderBoth(t, zzMetrics(501.0, 0, true, true))
	if !strings.Contains(kpisOver, "</span> over</p>") {
		t.Errorf("over: headline lacks 'over'")
	}
	if n := strings.Count(kpisOver, "bg-rose-50"); n < 3 {
		t.Errorf("over: bg-rose-50 count = %d, want >= 3 (2 static cards + the Budget card)", n)
	}

	// Clearly under: healthy tint present, no danger tint.
	_, kpisUnder := zzRenderBoth(t, zzMetrics(-501.0, 0, true, true))
	if !strings.Contains(kpisUnder, "</span> under</p>") {
		t.Errorf("under: headline lacks 'under'")
	}
	if n := strings.Count(kpisUnder, "bg-emerald-50"); n < 2 {
		t.Errorf("under: bg-emerald-50 count = %d, want >= 2 (static income card + the Budget card)", n)
	}
	if n := strings.Count(kpisUnder, "bg-rose-50"); n != 2 {
		t.Errorf("under: bg-rose-50 count = %d, want exactly the 2 static cards", n)
	}
}
GO2
cleanup() { rm -f "$PLANTED" "$PKG/zz_oracle_w4b_test.go"; }
trap cleanup EXIT
go test -count=1 -run 'TestOracleW4Tint' ./internal/handlers/dashboard/
ck "00-tint-single-source-and-trim" 0 "$?"

go test -count=1 -run 'TestOracleW4' ./internal/handlers/dashboard/
ck "01-parity-and-cleanliness" 0 "$?"

go test -count=1 ./internal/handlers/dashboard/ >/dev/null 2>&1
ck "02-package-suite" 0 "$?"
go build ./... >/dev/null 2>&1; ck "03-build" 0 "$?"
go vet ./...   >/dev/null 2>&1; ck "04-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 )) || exit 1
echo "ORACLE PASS"
