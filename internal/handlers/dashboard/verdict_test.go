package dashboard

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestBuildBudgetVerdict(t *testing.T) {
	t.Run("nil metrics is neutral with no target", func(t *testing.T) {
		v := BuildBudgetVerdict(nil)
		if v.Health != models.HealthNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasTarget {
			t.Errorf("HasTarget = true, want false")
		}
		// Eps is set unconditionally, even on the nil-metrics early return —
		// the sparkline dead band must match the card's onBudgetEps
		// regardless of whether a target is configured.
		if v.Eps != 1.0 {
			t.Errorf("Eps = %v, want 1.0", v.Eps)
		}
	})

	t.Run("no combined target is neutral", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: false, TotalIncome: 5000, NetSavings: 1000}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasTarget {
			t.Errorf("HasTarget = true, want false")
		}
		// Income/savings figures are still carried for the band.
		if v.TotalIncome != 5000 || v.NetSavings != 1000 {
			t.Errorf("income/savings = (%v,%v), want (5000,1000)", v.TotalIncome, v.NetSavings)
		}
	})

	t.Run("under budget is green", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: -500,
			LivingExpensesTotal:     8000, HealthcareTotal: 1500,
			LivingTargetTotal: 8500, HealthcareTargetTotal: 1500,
			MonthsInRange: 3,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if !v.IsUnder || v.IsOver {
			t.Errorf("IsUnder/IsOver = (%v,%v), want (true,false)", v.IsUnder, v.IsOver)
		}
		if v.SpentTotal != 9500 {
			t.Errorf("SpentTotal = %v, want 9500", v.SpentTotal)
		}
		if v.TargetTotal != 10000 {
			t.Errorf("TargetTotal = %v, want 10000", v.TargetTotal)
		}
		if v.Eps != 1.0 {
			t.Errorf("Eps = %v, want 1.0", v.Eps)
		}
	})

	t.Run("on budget is green", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: true, CombinedCumulativeDelta: 0.5, LivingTargetTotal: 10000}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if v.IsOver || v.IsUnder {
			t.Errorf("IsOver/IsUnder = (%v,%v), want both false (on budget)", v.IsOver, v.IsUnder)
		}
	})

	t.Run("slightly over budget is amber", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 500, // 5% of 10000 target
			LivingTargetTotal:       10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
		if !v.IsOver {
			t.Errorf("IsOver = false, want true")
		}
	})

	t.Run("well over budget is red", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 2500, // 25% of 10000 target
			LivingTargetTotal:       10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
	})

	t.Run("over budget with zero target total is red, not amber", func(t *testing.T) {
		// HasCombinedTarget can be true while the living+healthcare target
		// total is zero (combined delta spans other categories). A real
		// overage must not silently downgrade to amber via the missing ratio.
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 5000,
			LivingTargetTotal:       0,
			HealthcareTargetTotal:   0,
		}
		if v := BuildBudgetVerdict(m); v.Health != models.HealthRed {
			t.Errorf("Health = %q, want red (over budget, zero target denominator)", v.Health)
		}
	})

	t.Run("exactly at the amber/red ratio boundary is amber", func(t *testing.T) {
		// Delta/TargetTotal == overAmberPct (10%) → not > → amber (pins > vs >=).
		m := &models.DashboardMetrics{
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 1000, // exactly 10% of 10000
			LivingTargetTotal:       10000,
		}
		if v := BuildBudgetVerdict(m); v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber at exactly 10%%", v.Health)
		}
	})

	t.Run("within the on-budget dead-band is green and neither over nor under", func(t *testing.T) {
		// Delta == onBudgetEps (1.0): not > eps, not < -eps → on budget.
		m := &models.DashboardMetrics{HasCombinedTarget: true, CombinedCumulativeDelta: 1.0, LivingTargetTotal: 10000}
		v := BuildBudgetVerdict(m)
		if v.Health != models.HealthGreen || v.IsOver || v.IsUnder {
			t.Errorf("at eps: health=%q over=%v under=%v, want green/false/false", v.Health, v.IsOver, v.IsUnder)
		}
	})

	t.Run("headroom decomposition: live case, living over masked by healthcare under", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   -9660.19,
			HasBudgetTarget:           true,
			CumulativeDelta:           1804.41, // living over
			HasHealthcareTarget:       true,
			HealthcareCumulativeDelta: -11464.60, // healthcare under
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if !v.IsUnder {
			t.Fatalf("IsUnder = false, want true (total is under)")
		}
		if v.Living.Status != "over" || v.Living.Delta != 1804.41 {
			t.Errorf("Living = %+v, want over/1804.41", v.Living)
		}
		if v.Healthcare.Status != "under" || v.Healthcare.Delta != -11464.60 {
			t.Errorf("Healthcare = %+v, want under/-11464.60", v.Healthcare)
		}
		if v.Sentence.Kind != SentenceMixed {
			t.Fatalf("Sentence.Kind = %q, want mixed", v.Sentence.Kind)
		}
		if v.Sentence.OverBucket != "living" || v.Sentence.UnderBucket != "healthcare" {
			t.Errorf("Sentence buckets = (%q over, %q under), want (living, healthcare)", v.Sentence.OverBucket, v.Sentence.UnderBucket)
		}
		if v.Sentence.OverAmount != 1804.41 {
			t.Errorf("Sentence.OverAmount = %v, want 1804.41", v.Sentence.OverAmount)
		}
		if v.Sentence.UnderRoom != 9660.19 {
			t.Errorf("Sentence.UnderRoom = %v, want 9660.19", v.Sentence.UnderRoom)
		}
	})

	t.Run("headroom decomposition: mirrored mixed case, healthcare over masked by living under", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   -1500,
			HasBudgetTarget:           true,
			CumulativeDelta:           -2000, // living under
			HasHealthcareTarget:       true,
			HealthcareCumulativeDelta: 500, // healthcare over
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Sentence.Kind != SentenceMixed {
			t.Fatalf("Sentence.Kind = %q, want mixed", v.Sentence.Kind)
		}
		if v.Sentence.OverBucket != "healthcare" || v.Sentence.UnderBucket != "living" {
			t.Errorf("Sentence buckets = (%q over, %q under), want (healthcare, living)", v.Sentence.OverBucket, v.Sentence.UnderBucket)
		}
		if v.Sentence.OverAmount != 500 || v.Sentence.UnderRoom != 1500 {
			t.Errorf("Sentence over/room = (%v,%v), want (500,1500)", v.Sentence.OverAmount, v.Sentence.UnderRoom)
		}
	})

	t.Run("headroom decomposition: both buckets at-or-under target", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   -800,
			HasBudgetTarget:           true,
			CumulativeDelta:           -500,
			HasHealthcareTarget:       true,
			HealthcareCumulativeDelta: -300,
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Sentence.Kind != SentenceBothAtOrUnder {
			t.Fatalf("Sentence.Kind = %q, want both", v.Sentence.Kind)
		}
		if v.Sentence.UnderRoom != 800 {
			t.Errorf("Sentence.UnderRoom = %v, want 800", v.Sentence.UnderRoom)
		}
	})

	t.Run("headroom decomposition: total over yields the over sentence, not per-bucket", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   1100,
			HasBudgetTarget:           true,
			CumulativeDelta:           600,
			HasHealthcareTarget:       true,
			HealthcareCumulativeDelta: 500,
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Sentence.Kind != SentenceOver {
			t.Fatalf("Sentence.Kind = %q, want over", v.Sentence.Kind)
		}
		if v.Sentence.OverAmount != 1100 {
			t.Errorf("Sentence.OverAmount = %v, want 1100", v.Sentence.OverAmount)
		}
	})

	t.Run("headroom decomposition: living exactly at the $1 boundary counts as on target", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   -1000.01,
			HasBudgetTarget:           true,
			CumulativeDelta:           0.99, // within onBudgetEps → on target, not over
			HasHealthcareTarget:       true,
			HealthcareCumulativeDelta: -1001,
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if v.Living.Status != "on target" {
			t.Errorf("Living.Status = %q, want %q", v.Living.Status, "on target")
		}
		if v.Sentence.Kind != SentenceBothAtOrUnder {
			t.Errorf("Sentence.Kind = %q, want both (living at boundary is not 'over')", v.Sentence.Kind)
		}
	})

	t.Run("no target set renders no decomposition and no sentence", func(t *testing.T) {
		m := &models.DashboardMetrics{HasCombinedTarget: false}
		v := BuildBudgetVerdict(m)
		if v.Living.Configured || v.Healthcare.Configured {
			t.Errorf("Living/Healthcare configured = (%v,%v), want (false,false) with no target", v.Living.Configured, v.Healthcare.Configured)
		}
		if v.Sentence.Kind != SentenceNone {
			t.Errorf("Sentence.Kind = %q, want empty (no sentence)", v.Sentence.Kind)
		}
	})

	t.Run("only living configured: healthcare's degenerate raw delta is ignored, treated as at-target", func(t *testing.T) {
		m := &models.DashboardMetrics{
			HasCombinedTarget:         true,
			CombinedCumulativeDelta:   -200,
			HasBudgetTarget:           true,
			CumulativeDelta:           -200,
			HasHealthcareTarget:       false,
			HealthcareCumulativeDelta: 5000, // degenerate value per metrics comment; must be ignored
			LivingTargetTotal:         10000,
		}
		v := BuildBudgetVerdict(m)
		if !v.Living.Configured {
			t.Errorf("Living.Configured = false, want true")
		}
		if v.Healthcare.Configured {
			t.Errorf("Healthcare.Configured = true, want false (no healthcare target)")
		}
		if v.Healthcare.Status != "on target" || v.Healthcare.Delta != 0 {
			t.Errorf("Healthcare = %+v, want on-target/0 despite degenerate raw delta", v.Healthcare)
		}
		if v.Sentence.Kind != SentenceBothAtOrUnder {
			t.Errorf("Sentence.Kind = %q, want both (missing bucket treated as at-target)", v.Sentence.Kind)
		}
		if v.Sentence.UnderRoom != 200 {
			t.Errorf("Sentence.UnderRoom = %v, want 200", v.Sentence.UnderRoom)
		}
	})
}

func TestClassifyBucket(t *testing.T) {
	cases := []struct {
		name       string
		delta      float64
		configured bool
		wantStatus string
		wantDelta  float64
	}{
		{"over", 500, true, "over", 500},
		{"under", -500, true, "under", -500},
		{"on target zero", 0, true, "on target", 0},
		{"on target at positive boundary", 1.0, true, "on target", 1.0},
		{"on target at negative boundary", -1.0, true, "on target", -1.0},
		{"just over the boundary", 1.01, true, "over", 1.01},
		{"just under the boundary", -1.01, true, "under", -1.01},
		{"unconfigured with a large raw delta is on target with zero delta", 99999, false, "on target", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := classifyBucket(c.delta, c.configured)
			if f.Configured != c.configured {
				t.Errorf("Configured = %v, want %v", f.Configured, c.configured)
			}
			if f.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", f.Status, c.wantStatus)
			}
			if f.Delta != c.wantDelta {
				t.Errorf("Delta = %v, want %v", f.Delta, c.wantDelta)
			}
		})
	}
}

func TestClassifyVerdictSentence(t *testing.T) {
	t.Run("total over always yields the over sentence regardless of bucket makeup", func(t *testing.T) {
		living := classifyBucket(600, true)
		healthcare := classifyBucket(500, true)
		s := classifyVerdictSentence(living, healthcare, true, 1100)
		if s.Kind != SentenceOver || s.OverAmount != 1100 {
			t.Errorf("got %+v, want Kind=over OverAmount=1100", s)
		}
	})

	t.Run("both at-or-under yields the both sentence with total headroom", func(t *testing.T) {
		living := classifyBucket(-500, true)
		healthcare := classifyBucket(-300, true)
		s := classifyVerdictSentence(living, healthcare, false, -800)
		if s.Kind != SentenceBothAtOrUnder || s.UnderRoom != 800 {
			t.Errorf("got %+v, want Kind=both UnderRoom=800", s)
		}
	})

	t.Run("living over while total is under names living as the over bucket", func(t *testing.T) {
		living := classifyBucket(1804.41, true)
		healthcare := classifyBucket(-11464.60, true)
		s := classifyVerdictSentence(living, healthcare, false, -9660.19)
		if s.Kind != SentenceMixed || s.OverBucket != "living" || s.UnderBucket != "healthcare" {
			t.Errorf("got %+v, want Kind=mixed OverBucket=living UnderBucket=healthcare", s)
		}
		if s.OverAmount != 1804.41 || s.UnderRoom != 9660.19 {
			t.Errorf("got OverAmount=%v UnderRoom=%v, want 1804.41/9660.19", s.OverAmount, s.UnderRoom)
		}
	})

	t.Run("healthcare over while total is under names healthcare as the over bucket", func(t *testing.T) {
		living := classifyBucket(-2000, true)
		healthcare := classifyBucket(500, true)
		s := classifyVerdictSentence(living, healthcare, false, -1500)
		if s.Kind != SentenceMixed || s.OverBucket != "healthcare" || s.UnderBucket != "living" {
			t.Errorf("got %+v, want Kind=mixed OverBucket=healthcare UnderBucket=living", s)
		}
	})

	t.Run("unconfigured bucket can never be named the over bucket", func(t *testing.T) {
		living := classifyBucket(-200, true)
		healthcare := classifyBucket(5000, false) // unconfigured; forced on-target
		s := classifyVerdictSentence(living, healthcare, false, -200)
		if s.Kind != SentenceBothAtOrUnder {
			t.Errorf("Kind = %q, want both (unconfigured bucket must not drive mixed)", s.Kind)
		}
	})

	// W4 attempt-2 defect: an exact +0.0 totalDelta negates to IEEE-754
	// negative zero, and "-0.0 < 0" is false, so a strict "< 0" clamp let
	// -0.0 slip through as UnderRoom — formatMoney then renders "$-0.00" in
	// the "both at-or-under" sentence. room must always land on the
	// positive-zero literal, never -0.0.
	t.Run("both buckets exactly on target never yields a negative-zero UnderRoom", func(t *testing.T) {
		living := classifyBucket(0, true)
		healthcare := classifyBucket(0, true)
		s := classifyVerdictSentence(living, healthcare, false, 0)
		if s.Kind != SentenceBothAtOrUnder {
			t.Fatalf("Kind = %q, want both", s.Kind)
		}
		if s.UnderRoom != 0 {
			t.Errorf("UnderRoom = %v, want 0", s.UnderRoom)
		}
		if math.Signbit(s.UnderRoom) {
			t.Errorf("UnderRoom is negative zero (-0.0); formatMoney would render it as \"$-0.00\"")
		}
	})
}
