package engine

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// ws3ChainFixture is a minimal settings scenario, independent of
// richEngineScenario's SS/RMD/Roth machinery, whose portfolio reliably
// declines: a fixed negative return with no offsetting income source. Small
// FloorDropPct/high FloorCutPct means ANY decline fires a cut, so a fixture
// with guardrails enabled ALWAYS produces at least one cut in its first few
// years — the WS3 chain tests need that certainty, not just plausibility.
func ws3ChainFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "primary", Name: "Primary", BirthMonth: models.BirthMonthForAge(s.StartDate, 64), Role: models.PersonRolePrimary}}
	s.CurrentAge = 64
	s.PortfolioValue = 500_000
	s.MonthlyLivingExpenses = 4_000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.InvestmentReturn = -10
	s.ProjectionYears = 6
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.IncomeSources = nil
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	return s
}

func ws3GuardrailConfig() *models.GuardrailConfig {
	return &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 1000, CeilingRaisePct: 0,
		MinSpendingPct: 40, MaxSpendingPct: 200,
	}
}

// ws3OtherGuardrailConfig is deliberately VERY different from
// ws3GuardrailConfig (mirrors the oracle's other_g in make_fixture.py): if a
// chained step's OWN guardrail config were ever consulted (the D3' defect
// class), a step using this config in place of the primary's would cut on a
// completely different schedule and by a completely different amount —
// making any such leak trivially observable.
func ws3OtherGuardrailConfig() *models.GuardrailConfig {
	return &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 2, FloorCutPct: 30,
		CeilingRisePct: 50, CeilingRaisePct: 1,
		MinSpendingPct: 40, MaxSpendingPct: 200,
	}
}

// forceTransitionAtYear returns a ResolveChainTransition hook that fires
// chain[0] the instant currentYear reaches year, regardless of age — the
// same pattern engine_run_test.go's chain tests use, so the transition point
// is exact and independent of TransitionAge/CurrentAge arithmetic.
func forceTransitionAtYear(year int) func(int, int, *models.WhatIfSettings, []PreparedChainLink) (int, *models.WhatIfSettings) {
	return func(currentYear, nextChainIndex int, _ *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
		if nextChainIndex == 0 && currentYear >= year {
			return 1, chain[0].Settings.Settings()
		}
		return nextChainIndex, nil
	}
}

// assertGuardrailYearsMatch fails the test at any year where the chained
// run's guardrail figures diverge from the unchained control's — the D3'
// contract in one assertion: the VIEWED (primary) scenario's guardrail
// setting and config govern every year of the chain, so a chain that only
// changes a LATER step's guardrail configuration (or removes it, or enables
// a different one) must reproduce the unchained control byte-for-byte.
func assertGuardrailYearsMatch(t *testing.T, label string, got, want []models.ProjectionYearSummary) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: yearly summaries=%d, want %d", label, len(got), len(want))
	}
	for y := range want {
		g, w := got[y], want[y]
		if !gv2Close(g.GuardrailMultiplier, w.GuardrailMultiplier) {
			t.Errorf("%s: year %d GuardrailMultiplier=%v, want %v (the unchained-A figure)", label, y, g.GuardrailMultiplier, w.GuardrailMultiplier)
		}
		if !gv2Close(g.GuardrailCutTrigger, w.GuardrailCutTrigger) {
			t.Errorf("%s: year %d GuardrailCutTrigger=%v, want %v", label, y, g.GuardrailCutTrigger, w.GuardrailCutTrigger)
		}
		if !gv2Close(g.GuardrailRaiseTrigger, w.GuardrailRaiseTrigger) {
			t.Errorf("%s: year %d GuardrailRaiseTrigger=%v, want %v", label, y, g.GuardrailRaiseTrigger, w.GuardrailRaiseTrigger)
		}
		if !gv2Close(g.GuardrailPeak, w.GuardrailPeak) {
			t.Errorf("%s: year %d GuardrailPeak=%v, want %v", label, y, g.GuardrailPeak, w.GuardrailPeak)
		}
		if !gv2Close(g.GuardrailBaseline, w.GuardrailBaseline) {
			t.Errorf("%s: year %d GuardrailBaseline=%v, want %v", label, y, g.GuardrailBaseline, w.GuardrailBaseline)
		}
	}
}

// TestChainPrimaryGovernsOnToOffTransition is acceptance criterion 1 / the
// oracle's a-to-b variant, under D3': A (guardrails enabled) chains into B
// (no guardrails) at year 3. Per D3', B's own (missing) guardrail setting is
// never consulted — the VIEWED (primary) scenario, A, governs the WHOLE
// chain, so every year (including every year after the transition) must
// match an UNCHAINED run of A exactly. Before this fix (attempt 2's D3), the
// chained run's post-transition years fell back to the multiplier=1.0
// "plan" default instead.
func TestChainPrimaryGovernsOnToOffTransition(t *testing.T) {
	a := ws3ChainFixture()
	a.Guardrails = ws3GuardrailConfig()

	b := ws3ChainFixture()
	b.Guardrails = nil

	chained := New().Run(Input{
		Prepared: prepare.MustFrom(t, a),
		Chain: []PreparedChainLink{{
			ScenarioFilename: "b.json",
			TransitionAge:    a.CurrentAge + 3,
			Settings:         prepare.MustFrom(t, b),
		}},
		Hooks: Hooks{ResolveChainTransition: forceTransitionAtYear(3)},
	})
	unchained := New().Run(Input{Prepared: prepare.MustFrom(t, a)})

	cutSomewhere := false
	for _, y := range unchained.YearlySummaries {
		if y.GuardrailMultiplier < 1.0 {
			cutSomewhere = true
		}
	}
	if !cutSomewhere {
		t.Fatalf("fixture defect: expected the unchained control to show at least one guardrail cut: %#v", unchained.YearlySummaries)
	}

	assertGuardrailYearsMatch(t, "a-to-b", chained.YearlySummaries, unchained.YearlySummaries)

	// The oracle's own check: chart/table trigger figures must never be a
	// fabricated $0 once guardrails govern (they are constant, driven by
	// the primary's config, for the whole chain).
	for y, ys := range chained.YearlySummaries {
		if ys.GuardrailCutTrigger <= 0 {
			t.Errorf("a-to-b: year %d GuardrailCutTrigger=%v, want > 0 (governed by the primary throughout the chain)", y, ys.GuardrailCutTrigger)
		}
		if ys.GuardrailRaiseTrigger <= 0 {
			t.Errorf("a-to-b: year %d GuardrailRaiseTrigger=%v, want > 0", y, ys.GuardrailRaiseTrigger)
		}
	}
}

// TestChainPrimaryConfigIgnoresChainedStepConfig is acceptance criterion 2 /
// the oracle's a-to-y variant: A (guardrails enabled) chains into Y
// (guardrails ALSO enabled, but a VERY DIFFERENT config) at year 3. D3'
// says the chained step's own guardrail config — even when the step turns
// guardrails on with a config of its own — is never consulted: the whole
// chain still matches an unchained run of A exactly. This is the direct
// kill for "evaluating with the active step's config instead of the
// primary's": if the active step's Guardrails ever leaked in, Y's very
// different FloorDropPct/FloorCutPct would produce a visibly different
// multiplier schedule after year 3.
func TestChainPrimaryConfigIgnoresChainedStepConfig(t *testing.T) {
	a := ws3ChainFixture()
	a.Guardrails = ws3GuardrailConfig()

	y := ws3ChainFixture()
	y.Guardrails = ws3OtherGuardrailConfig()

	chained := New().Run(Input{
		Prepared: prepare.MustFrom(t, a),
		Chain: []PreparedChainLink{{
			ScenarioFilename: "y.json",
			TransitionAge:    a.CurrentAge + 3,
			Settings:         prepare.MustFrom(t, y),
		}},
		Hooks: Hooks{ResolveChainTransition: forceTransitionAtYear(3)},
	})
	unchained := New().Run(Input{Prepared: prepare.MustFrom(t, a)})

	assertGuardrailYearsMatch(t, "a-to-y", chained.YearlySummaries, unchained.YearlySummaries)
}

// TestChainPrimaryOffSuppressesChainedStepGuardrails is acceptance
// criterion 3 / the oracle's b-to-a variant: B (no guardrails) chains into A
// (guardrails enabled) at year 3. D3' replaces the old D3 "guardrails can
// act after the transition" rule: since the VIEWED (primary) scenario, B,
// has no guardrails, NOTHING governs for the whole chain — not even the
// portion running under A's settings. No multiplier in any year, no
// GuardrailEvents, and every trigger/peak/baseline figure stays zero.
func TestChainPrimaryOffSuppressesChainedStepGuardrails(t *testing.T) {
	b := ws3ChainFixture()
	b.Guardrails = nil

	a := ws3ChainFixture()
	a.Guardrails = ws3GuardrailConfig()

	proj := New().Run(Input{
		Prepared: prepare.MustFrom(t, b),
		Chain: []PreparedChainLink{{
			ScenarioFilename: "a.json",
			TransitionAge:    b.CurrentAge + 3,
			Settings:         prepare.MustFrom(t, a),
		}},
		Hooks: Hooks{ResolveChainTransition: forceTransitionAtYear(3)},
	})

	if len(proj.YearlySummaries) != b.ProjectionYears {
		t.Fatalf("yearly summaries=%d, want %d", len(proj.YearlySummaries), b.ProjectionYears)
	}
	for y, ys := range proj.YearlySummaries {
		if ys.GuardrailMultiplier != 1.0 {
			t.Errorf("year %d: GuardrailMultiplier=%v, want 1.0 (primary B has no guardrails, so nothing governs anywhere in the chain)", y, ys.GuardrailMultiplier)
		}
		if ys.GuardrailCutTrigger != 0 || ys.GuardrailRaiseTrigger != 0 {
			t.Errorf("year %d: CutTrigger=%v RaiseTrigger=%v, want 0/0 (no chart trigger lines when the primary has no guardrails)", y, ys.GuardrailCutTrigger, ys.GuardrailRaiseTrigger)
		}
		if ys.GuardrailPeak != 0 || ys.GuardrailBaseline != 0 {
			t.Errorf("year %d: Peak=%v Baseline=%v, want 0/0", y, ys.GuardrailPeak, ys.GuardrailBaseline)
		}
	}
	if len(proj.GuardrailEvents) != 0 {
		t.Errorf("expected no guardrail events with the primary's guardrails off, got %#v", proj.GuardrailEvents)
	}
}

// TestChainFloorAppliesPrimaryMinMonthlySpendingReal is the permanent test
// for "the spending floor from the active step": primary A sets a real
// spending floor high enough to always bind; the chained-into step Z has
// guardrails enabled too (so the "does guardrails govern" question is
// identically true either way — this isolates the floor VALUE specifically)
// but its own MinMonthlySpendingReal is 0 (would not bind at all if it were
// ever consulted). Every month, before AND after the transition, must be
// floored at A's value.
func TestChainFloorAppliesPrimaryMinMonthlySpendingReal(t *testing.T) {
	const primaryFloor = 6_000 // > MonthlyLivingExpenses (4,000) even before any cut, so it always binds.

	a := ws3ChainFixture()
	a.Guardrails = ws3GuardrailConfig()
	a.Guardrails.MinMonthlySpendingReal = primaryFloor

	z := ws3ChainFixture()
	z.Guardrails = ws3GuardrailConfig()
	z.Guardrails.MinMonthlySpendingReal = 0

	proj := New().Run(Input{
		Prepared: prepare.MustFrom(t, a),
		Chain: []PreparedChainLink{{
			ScenarioFilename: "z.json",
			TransitionAge:    a.CurrentAge + 3,
			Settings:         prepare.MustFrom(t, z),
		}},
		Hooks: Hooks{ResolveChainTransition: forceTransitionAtYear(3)},
	})

	for i, month := range proj.Months {
		if !gv2Close(month.AdjustedLivingExpenses, primaryFloor) {
			t.Errorf("month %d: AdjustedLivingExpenses=%v, want the PRIMARY's floor %v (the chained step's own floor of 0 must be ignored)", i, month.AdjustedLivingExpenses, float64(primaryFloor))
		}
	}
}

// assertGuardrailEventsMatch fails the test at any index where the chained
// run's GuardrailEvents diverge from the unchained control's — count,
// order, and every field. WS3 attempt-3 checker-tests (2026-09-24) found
// that TestChainFloorAppliesPrimaryMinMonthlySpendingReal only exercises
// the THIRD floorAdjustedLiving call site (the one that produces
// AdjustedLivingExpenses); the two EVENT call sites in StepMonth — the
// "before"/"after" floorAdjustedLiving calls used to decide whether a
// GuardrailEvent fires and what MonthlySpendingBefore/After it records —
// were untested, so a mutant sourcing the floor from the ACTIVE (chained)
// step at ONLY those two sites survived the whole suite even though the
// deterministic Year-by-Year table stayed correct.
func assertGuardrailEventsMatch(t *testing.T, label string, got, want []models.GuardrailEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: GuardrailEvents count=%d, want %d\n got=%#v\nwant=%#v", label, len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Year != w.Year || g.Type != w.Type {
			t.Errorf("%s: event %d Year/Type=%d/%s, want %d/%s", label, i, g.Year, g.Type, w.Year, w.Type)
		}
		if !gv2Close(g.Multiplier, w.Multiplier) {
			t.Errorf("%s: event %d Multiplier=%v, want %v", label, i, g.Multiplier, w.Multiplier)
		}
		if !gv2Close(g.PreviousMultiplier, w.PreviousMultiplier) {
			t.Errorf("%s: event %d PreviousMultiplier=%v, want %v", label, i, g.PreviousMultiplier, w.PreviousMultiplier)
		}
		if !gv2Close(g.MonthlySpendingBefore, w.MonthlySpendingBefore) {
			t.Errorf("%s: event %d MonthlySpendingBefore=%v, want %v", label, i, g.MonthlySpendingBefore, w.MonthlySpendingBefore)
		}
		if !gv2Close(g.MonthlySpendingAfter, w.MonthlySpendingAfter) {
			t.Errorf("%s: event %d MonthlySpendingAfter=%v, want %v", label, i, g.MonthlySpendingAfter, w.MonthlySpendingAfter)
		}
	}
}

// TestChainGuardrailEventsMatchUnchainedWhenPrimaryFloorBinds is the
// permanent test for the WS3 attempt-3 checker-tests gap: primary A's real
// spending floor (3,500) binds after a cut; A chains, at year 3, into step
// Z whose OWN guardrail config is very different AND whose own floor
// (1,000) is LOWER and would not bind at the same point. Per D3', Z's
// config — including its floor — is never consulted, so the chained run's
// GuardrailEvents (and therefore the rendered Guardrail Events list and the
// GuardrailChartSummary caption, which read the identical field — see the
// handlers-package counterpart of this test) must exactly match an
// unchained run of A alone.
func TestChainGuardrailEventsMatchUnchainedWhenPrimaryFloorBinds(t *testing.T) {
	a := ws3ChainFixture()
	a.Guardrails = ws3GuardrailConfig()
	a.Guardrails.MinMonthlySpendingReal = 3000 // < MonthlyLivingExpenses (4,000); the guaranteed yearly cuts push spending below it starting exactly at year 3, the transition year.

	z := ws3ChainFixture()
	z.Guardrails = ws3OtherGuardrailConfig()
	z.Guardrails.MinMonthlySpendingReal = 1000 // lower, and paired with a very different config — must never be consulted.

	chained := New().Run(Input{
		Prepared: prepare.MustFrom(t, a),
		Chain: []PreparedChainLink{{
			ScenarioFilename: "z.json",
			TransitionAge:    a.CurrentAge + 3,
			Settings:         prepare.MustFrom(t, z),
		}},
		Hooks: Hooks{ResolveChainTransition: forceTransitionAtYear(3)},
	})
	unchained := New().Run(Input{Prepared: prepare.MustFrom(t, a)})

	if len(unchained.GuardrailEvents) == 0 {
		t.Fatalf("fixture defect: expected the unchained control to record at least one guardrail event: %#v", unchained.YearlySummaries)
	}
	postTransitionEvent := false
	for _, e := range unchained.GuardrailEvents {
		if e.Year >= 3 {
			postTransitionEvent = true
		}
	}
	if !postTransitionEvent {
		t.Fatalf("fixture defect: expected at least one event at/after year 3 (where the chain transitions): %#v", unchained.GuardrailEvents)
	}

	assertGuardrailEventsMatch(t, "chained vs unchained-A", chained.GuardrailEvents, unchained.GuardrailEvents)
}
