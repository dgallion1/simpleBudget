package dashboard

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestDashboardVerdictBar_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	t.Run("over budget shows red band and over-budget headline", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthRed, HasTarget: true, IsOver: true,
				Delta: 2500, SpentTotal: 12500, TargetTotal: 10000,
				Months: 3, NetSavings: 2000, SavingsRate: 20, TotalIncome: 10000,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-red", "over budget", "Spent", "Target", `class="num`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
	})

	t.Run("under budget shows green band and under-budget headline", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthGreen, HasTarget: true, IsUnder: true,
				Delta: -800, SpentTotal: 9200, TargetTotal: 10000, Months: 3, NetSavings: 1500,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-green", "under budget"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
	})

	t.Run("no target shows neutral band and a link to set a budget", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthNeutral, HasTarget: false, NetSavings: 500,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-neutral", "No budget set", "/whatif"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 700))
			}
		}
		if strings.Contains(out, "over budget") || strings.Contains(out, "under budget") {
			t.Errorf("did not expect a budget delta headline when HasTarget is false")
		}
		for _, notWant := range []string{"Living:", "Healthcare:", "You could spend", "No headroom", "of room comes from"} {
			if strings.Contains(out, notWant) {
				t.Errorf("did not expect %q in output when HasTarget is false; got: %s", notWant, trunc(out, 900))
			}
		}
		// Byte-identity regression (W4 fix 4a): the decomposition/sentence
		// block is entirely gated behind {{- if $v.HasTarget}}. Before the
		// trim marker, the whitespace-only line preceding that {{if}} stayed
		// in the output even when the block was skipped, so the closing
		// </div> of the *headline* block (".text-lg font-bold" — where the
		// artifact actually sat) and the flex-1 container's closing </div>
		// were separated by a line of bare spaces, instead of directly
		// abutting as master renders them. The prior version of this guard
		// anchored one level away (the flex-1-to-next-section boundary),
		// which is unaffected by the {{- trim}} either way and passes
		// whether or not the artifact is present — a tautology. Anchor on
		// the actual location, and generally reject any whitespace-only
		// line anywhere in the output.
		if !strings.Contains(out, "</div>\n    </div>\n\n    <div class=\"flex items-center gap-6 ml-auto\">") {
			t.Errorf("expected the headline block's closing </div> to directly abut the flex-1 container's closing </div> (byte-identical to master, no artifact line between), got: %s", trunc(out, 900))
		}
	})

	t.Run("mixed case: living over is decomposed and explained, not netted away", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthGreen, HasTarget: true, IsUnder: true,
				Delta: -9660.19, SpentTotal: 1000, TargetTotal: 2000, Months: 7.8,
				Living:     BucketFigure{Configured: true, Delta: 1804.41, Status: "over"},
				Healthcare: BucketFigure{Configured: true, Delta: -11464.60, Status: "under"},
				Sentence: VerdictSentence{
					Kind: SentenceMixed, UnderRoom: 9660.19, UnderBucket: "healthcare",
					OverAmount: 1804.41, OverBucket: "living",
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{
			"Living:", "$1,804.41", "over",
			"Healthcare:", "$11,464.60", "under",
			"Your", "$9,660.19", "of room comes from healthcare running under plan",
			"living spending is already", "over its target.",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 1400))
			}
		}
		if strings.Contains(out, "You could spend up to") || strings.Contains(out, "No headroom") {
			t.Errorf("did not expect the both/over sentences in the mixed case; got: %s", trunc(out, 1400))
		}
	})

	t.Run("both buckets at-or-under target: headroom sentence, no per-bucket blame", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthGreen, HasTarget: true, IsUnder: true,
				Delta: -800, SpentTotal: 9200, TargetTotal: 10000, Months: 3,
				Living:     BucketFigure{Configured: true, Delta: -500, Status: "under"},
				Healthcare: BucketFigure{Configured: true, Delta: -300, Status: "under"},
				Sentence:   VerdictSentence{Kind: SentenceBothAtOrUnder, UnderRoom: 800},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"Living:", "$500.00", "under", "Healthcare:", "$300.00", "You could spend up to", "$800.00", "extra this period and stay on plan."} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 1400))
			}
		}
	})

	t.Run("total over: no-headroom sentence, no per-bucket sentence", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthRed, HasTarget: true, IsOver: true,
				Delta: 1100, SpentTotal: 12000, TargetTotal: 10000, Months: 3,
				Living:     BucketFigure{Configured: true, Delta: 600, Status: "over"},
				Healthcare: BucketFigure{Configured: true, Delta: 500, Status: "over"},
				Sentence:   VerdictSentence{Kind: SentenceOver, OverAmount: 1100},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"No headroom", "$1,100.00", "over plan for this period."} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, trunc(out, 1400))
			}
		}
		if strings.Contains(out, "You could spend up to") || strings.Contains(out, "of room comes from") {
			t.Errorf("did not expect the both/mixed sentences in the over case; got: %s", trunc(out, 1400))
		}
	})

	t.Run("single bucket configured: only the configured bucket is shown", func(t *testing.T) {
		out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{
			"BudgetVerdict": BudgetVerdictView{
				Health: models.HealthGreen, HasTarget: true, IsUnder: true,
				Delta: -200, SpentTotal: 800, TargetTotal: 1000, Months: 1,
				Living:     BucketFigure{Configured: true, Delta: -200, Status: "under"},
				Healthcare: BucketFigure{}, // not configured
				Sentence:   VerdictSentence{Kind: SentenceBothAtOrUnder, UnderRoom: 200},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Living:") {
			t.Errorf("expected the configured Living bucket in output; got: %s", trunc(out, 1400))
		}
		if strings.Contains(out, "Healthcare:") {
			t.Errorf("did not expect the unconfigured Healthcare bucket in output; got: %s", trunc(out, 1400))
		}
	})
}

// TestBudgetKPICardAndVerdictBanner_AgreeWithinDeadBand guards ruling
// 2026-08-29a: a threshold applied to a figure must live in one
// classification source consumed by every surface that renders it. A
// within-$1 living delta must render "on target" in both the verdict
// banner's decomposition line and the Budget KPI card's per-bucket row —
// never "on target" in one and a colored "over"/"under" in the other.
func TestBudgetKPICardAndVerdictBanner_AgreeWithinDeadBand(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	m := &models.DashboardMetrics{
		MonthsInRange:             1,
		LivingExpensesTotal:       2000.60,
		ActualMonthly:             2000.60,
		BudgetTarget:              2000,
		PerMonthDelta:             0.60,
		CumulativeDelta:           0.60, // within the $1 dead-band: neither over nor under
		HasBudgetTarget:           true,
		HealthcareActual:          300,
		HealthcareTotal:           300,
		HealthcareTarget:          300,
		HealthcarePerMonthDelta:   0,
		HealthcareCumulativeDelta: 0,
		HasHealthcareTarget:       true,
		CombinedTarget:            2300,
		CombinedActualMonthly:     2300.60,
		CombinedCumulativeDelta:   0.60,
		HasCombinedTarget:         true,
		LivingTargetTotal:         2000,
		HealthcareTargetTotal:     300,
	}
	verdict := BuildBudgetVerdict(m)

	if verdict.Living.Status != "on target" {
		t.Fatalf("fixture setup: verdict.Living.Status = %q, want %q", verdict.Living.Status, "on target")
	}

	bannerOut, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": verdict})
	if err != nil {
		t.Fatalf("RenderToString(dashboard-verdict-bar): %v", err)
	}
	cardOut, err := renderer.RenderToString("kpis", map[string]any{"Metrics": m, "BudgetVerdict": verdict})
	if err != nil {
		t.Fatalf("RenderToString(kpis): %v", err)
	}

	if !strings.Contains(bannerOut, "Living: <span class=\"num\">$0.60</span> on target") {
		t.Errorf("expected banner decomposition to read \"on target\" for Living; got: %s", trunc(bannerOut, 1200))
	}
	if !strings.Contains(cardOut, "on target") {
		t.Errorf("expected Budget card Living row to read \"on target\"; got: %s", trunc(cardOut, 2000))
	}
	for _, notWant := range []string{`$0.60</span> over`, `$0.60</span> under`} {
		if strings.Contains(cardOut, notWant) {
			t.Errorf("did not expect Budget card Living row to classify the within-dead-band delta as over/under; got: %s", trunc(cardOut, 2000))
		}
	}
}

// singleBucketMetrics builds DashboardMetrics with only one of
// Living/Healthcare configured, mirroring the tier-3 oracle's fixture shape.
func singleBucketMetrics(hasLiving, hasHC bool) *models.DashboardMetrics {
	m := &models.DashboardMetrics{MonthsInRange: 1}
	combined := 0.0
	if hasLiving {
		m.BudgetTarget = 2000
		m.LivingTargetTotal = 2000
		m.HasBudgetTarget = true
		m.LivingExpensesTotal = 1500
		m.ActualMonthly = 1500
		m.PerMonthDelta = -500
		m.CumulativeDelta = -500
		combined += 2000
	}
	if hasHC {
		m.HealthcareTarget = 300
		m.HealthcareTargetTotal = 300
		m.HasHealthcareTarget = true
		m.HealthcareTotal = 260
		m.HealthcareActual = 260
		m.HealthcarePerMonthDelta = -40
		m.HealthcareCumulativeDelta = -40
		combined += 300
	}
	m.CombinedTarget = combined
	m.HasCombinedTarget = combined > 0
	return m
}

// TestBudgetKPICard_BucketRowAbsentWhenUnconfigured guards the W4 attempt-2
// defect: a phantom "Health: $0.00 of $0.00 on target" (or Living-equivalent)
// row rendered on the Budget KPI card whenever only one bucket had a target
// configured. A bucket's row must be gated on BudgetVerdict.<Bucket>.Configured
// on both the card and the banner, so an unconfigured bucket renders no row
// at all rather than a degenerate $0.00 one.
func TestBudgetKPICard_BucketRowAbsentWhenUnconfigured(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	cases := []struct {
		name             string
		hasLiving, hasHC bool
	}{
		{"living-only", true, false},
		{"healthcare-only", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := singleBucketMetrics(c.hasLiving, c.hasHC)
			v := BuildBudgetVerdict(m)

			bannerOut, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
			if err != nil {
				t.Fatalf("RenderToString(dashboard-verdict-bar): %v", err)
			}
			kpisOut, err := renderer.RenderToString("kpis", map[string]any{"Metrics": m, "BudgetVerdict": v})
			if err != nil {
				t.Fatalf("RenderToString(kpis): %v", err)
			}

			if c.hasLiving {
				if !strings.Contains(bannerOut, "Living:") {
					t.Errorf("expected banner Living entry when configured; got: %s", trunc(bannerOut, 900))
				}
				if !strings.Contains(kpisOut, "Living: <span") {
					t.Errorf("expected card Living row when configured; got: %s", trunc(kpisOut, 2000))
				}
			} else {
				if strings.Contains(bannerOut, "Living:") {
					t.Errorf("did not expect banner Living entry when unconfigured; got: %s", trunc(bannerOut, 900))
				}
				// Card-only occurrence count: subtract the banner's own copy
				// (the kpis template embeds the banner).
				cardCount := strings.Count(kpisOut, "Living: <span") - strings.Count(bannerOut, "Living: <span")
				if cardCount != 0 {
					t.Errorf("did not expect a phantom card Living row when unconfigured; got %d occurrences in: %s", cardCount, trunc(kpisOut, 2000))
				}
			}

			if c.hasHC {
				if !strings.Contains(bannerOut, "Healthcare:") {
					t.Errorf("expected banner Healthcare entry when configured; got: %s", trunc(bannerOut, 900))
				}
				if !strings.Contains(kpisOut, "Health: <span") {
					t.Errorf("expected card Health row when configured; got: %s", trunc(kpisOut, 2000))
				}
			} else {
				if strings.Contains(bannerOut, "Healthcare:") {
					t.Errorf("did not expect banner Healthcare entry when unconfigured; got: %s", trunc(bannerOut, 900))
				}
				if strings.Contains(kpisOut, "Health: <span") {
					t.Errorf("did not expect a phantom card Health row when unconfigured; got: %s", trunc(kpisOut, 2000))
				}
			}
		})
	}
}

// TestVerdictRender_NoNegativeZero guards the W4 attempt-2 "$-0.00" defect:
// an exactly-zero combined delta (both buckets precisely on target) must
// never surface a negative-zero dollar figure on either surface.
func TestVerdictRender_NoNegativeZero(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	m := &models.DashboardMetrics{
		MonthsInRange:             1,
		BudgetTarget:              2000,
		LivingTargetTotal:         2000,
		HasBudgetTarget:           true,
		LivingExpensesTotal:       2000,
		ActualMonthly:             2000,
		PerMonthDelta:             0,
		CumulativeDelta:           0,
		HealthcareTarget:          300,
		HealthcareTargetTotal:     300,
		HasHealthcareTarget:       true,
		HealthcareTotal:           300,
		HealthcareActual:          300,
		HealthcarePerMonthDelta:   0,
		HealthcareCumulativeDelta: 0,
		CombinedTarget:            2300,
		CombinedActualMonthly:     2300,
		CombinedCumulativeDelta:   0,
		HasCombinedTarget:         true,
	}
	v := BuildBudgetVerdict(m)

	bannerOut, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
	if err != nil {
		t.Fatalf("RenderToString(dashboard-verdict-bar): %v", err)
	}
	kpisOut, err := renderer.RenderToString("kpis", map[string]any{"Metrics": m, "BudgetVerdict": v})
	if err != nil {
		t.Fatalf("RenderToString(kpis): %v", err)
	}

	if strings.Contains(bannerOut, "$-0.00") {
		t.Errorf("banner renders $-0.00: %s", trunc(bannerOut, 1400))
	}
	if strings.Contains(kpisOut, "$-0.00") {
		t.Errorf("kpis card renders $-0.00: %s", trunc(kpisOut, 2200))
	}
}

// bothBucketsMetrics builds DashboardMetrics with both Living and Healthcare
// configured, given the two per-bucket cumulative deltas, mirroring the
// tier-3 oracle's fixture shape (2000 living target + 300 healthcare target).
func bothBucketsMetrics(livingDelta, hcDelta float64) *models.DashboardMetrics {
	return &models.DashboardMetrics{
		MonthsInRange:             1,
		BudgetTarget:              2000,
		LivingTargetTotal:         2000,
		HasBudgetTarget:           true,
		LivingExpensesTotal:       2000 + livingDelta,
		ActualMonthly:             2000 + livingDelta,
		PerMonthDelta:             livingDelta,
		CumulativeDelta:           livingDelta,
		HealthcareTarget:          300,
		HealthcareTargetTotal:     300,
		HasHealthcareTarget:       true,
		HealthcareTotal:           300 + hcDelta,
		HealthcareActual:          300 + hcDelta,
		HealthcarePerMonthDelta:   hcDelta,
		HealthcareCumulativeDelta: hcDelta,
		CombinedTarget:            2300,
		CombinedActualMonthly:     2300 + livingDelta + hcDelta,
		CombinedCumulativeDelta:   livingDelta + hcDelta,
		HasCombinedTarget:         true,
	}
}

// TestBudgetKPICard_TintMatchesClassification guards ruling 2026-08-29c: the
// Budget KPI card's container tint (border-negative/bg-negative-soft for
// over, border-positive/bg-positive-soft for under — U6 renamed these from
// the rose/emerald hue literals) and its headline ("over" / "under" / "On
// budget") must be driven by the SAME dead-banded BudgetVerdict
// classification the banner uses ($v.IsOver / $v.IsUnder) — not a bare-sign
// comparison against the raw delta. Two OTHER KPI cards (Total Expenses,
// Monthly Healthcare) are permanently negative-tinted and Total Income is
// permanently positive-tinted; those are the static baselines this test
// subtracts out. Counts use the "border-X bg-X-soft" pair (the card
// container), not bare "bg-X-soft" alone, because U6 also uses bg-X-soft on
// each card's icon circle — the container and icon shades used to be
// numerically distinct (50 vs 100) but the token sweep intentionally
// consolidated them onto one soft shade.
func TestBudgetKPICard_TintMatchesClassification(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	const staticNegative = 2 // Total Expenses + Monthly Healthcare cards
	const staticPositive = 1 // Total Income card

	render := func(t *testing.T, m *models.DashboardMetrics) string {
		t.Helper()
		v := BuildBudgetVerdict(m)
		out, err := renderer.RenderToString("kpis", map[string]any{"Metrics": m, "BudgetVerdict": v})
		if err != nil {
			t.Fatalf("RenderToString(kpis): %v", err)
		}
		return out
	}

	t.Run("dead-band: on budget, no danger tint", func(t *testing.T) {
		out := render(t, bothBucketsMetrics(0.60, 0))
		if !strings.Contains(out, "On budget") {
			t.Errorf("expected headline \"On budget\"; got: %s", trunc(out, 2500))
		}
		if n := strings.Count(out, "border-negative bg-negative-soft"); n != staticNegative {
			t.Errorf("dead-band: %d occurrence(s) of the negative card tint, want exactly the %d static cards (Budget card must not be negative-tinted while on-budget)", n, staticNegative)
		}
	})

	t.Run("clearly over: danger tint and over headline", func(t *testing.T) {
		out := render(t, bothBucketsMetrics(501.0, 0))
		if !strings.Contains(out, `</span> over</p>`) {
			t.Errorf("expected headline to end in \"over\"; got: %s", trunc(out, 2500))
		}
		if n := strings.Count(out, "border-negative bg-negative-soft"); n < staticNegative+1 {
			t.Errorf("over: negative card tint count = %d, want >= %d (static cards + the Budget card)", n, staticNegative+1)
		}
	})

	t.Run("clearly under: healthy tint, no danger tint", func(t *testing.T) {
		out := render(t, bothBucketsMetrics(-501.0, 0))
		if !strings.Contains(out, `</span> under</p>`) {
			t.Errorf("expected headline to end in \"under\"; got: %s", trunc(out, 2500))
		}
		if n := strings.Count(out, "border-positive bg-positive-soft"); n < staticPositive+1 {
			t.Errorf("under: positive card tint count = %d, want >= %d (static Income card + the Budget card)", n, staticPositive+1)
		}
		if n := strings.Count(out, "border-negative bg-negative-soft"); n != staticNegative {
			t.Errorf("under: negative card tint count = %d, want exactly the %d static cards", n, staticNegative)
		}
	})
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
