package whatif

import (
	"budget2/internal/models"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func float64Pointer(value float64) *float64 { return &value }

func TestSpendingOptimizerRenderFormDecision(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	for _, tc := range []struct {
		name      string
		floor     any
		wantValue string
	}{
		{name: "minimum remains unchosen", floor: nil, wantValue: ""},
		{name: "saved positive minimum", floor: 6250.25, wantValue: "6250.25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			data := map[string]any{
				"SpendingOptimizerForm": map[string]any{
					"FloorMonthlyReal":           tc.floor,
					"NearTermYears":              5,
					"CurrentBaseMonthlyReal":     8000.0,
					"CurrentStartingMonthlyReal": 8500.0,
					"StartMonth":                 "2026-09",
					"ExpectedEndMonth":           "2056-08",
				},
			}
			if err := renderer.RenderPartial(w, "whatif-spending-optimizer", data); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			minimum := guardrailTestElement(t, body, "id", "spending-minimum")
			if value := guardrailTestAttribute(minimum, "value"); value != tc.wantValue {
				t.Fatalf("minimum value = %q, want %q", value, tc.wantValue)
			}
			for _, want := range []string{
				"How much can I spend?",
				"Minimum comfortable monthly living budget",
				"Required. Enter today's dollars; the minimum increases with inflation.",
				"Include basic living costs and a fun allowance.",
				"Healthcare, taxes, and other separately entered expenses are additional.",
				"Acceptable shortfall",
				`name="max_shortfall_pct"`,
				"Shown the same way as the funding-shortfall line in Simulated lifestyle outcomes",
				"Compare spending options",
				"Extra spending early",
				"When enabled, both extra-spending fields are required.",
				"Advanced search settings",
				"Lowest starting monthly living budget",
				"Highest starting monthly living budget",
				"Starting monthly living step",
				"today's US dollars per month",
				"Effective starting monthly living range",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
			shortfall := guardrailTestElement(t, body, "id", "spending-shortfall")
			if value := guardrailTestAttribute(shortfall, "value"); value != "5" {
				t.Fatalf("acceptable shortfall default = %q, want 5", value)
			}
			for _, forbidden := range []string{`value="7500"`, "guaranteed", "risk-free", "in every checked future"} {
				if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
					t.Errorf("new spending form contains forbidden copy %q", forbidden)
				}
			}
		})
	}
}

func TestSpendingOptimizerRenderSavedBoostStates(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	for _, tc := range []struct {
		name     string
		form     map[string]any
		wantCopy string
	}{
		{name: "active", form: map[string]any{"BoostActive": true}, wantCopy: "current starting budget includes this active extra amount"},
		{name: "expired", form: map[string]any{"BoostExpired": true}, wantCopy: "saved schedule has already ended and is inactive"},
		{name: "outside horizon", form: map[string]any{"BoostOutsideHorizon": true, "BoostActive": true}, wantCopy: "saved boost ends outside the modeled horizon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := map[string]any{
				"FloorMonthlyReal": 7000.0, "NearTermYears": 5,
				"CurrentBaseMonthlyReal": 8000.0, "CurrentStartingMonthlyReal": 8500.0,
				"StartMonth": "2026-09", "ExpectedEndMonth": "2056-08", "BoostStopMinimum": "2026-10",
				"BoostEnabled": true, "BoostMonthlyReal": 500.0, "BoostStopMonth": "2031-09",
			}
			for key, value := range tc.form {
				form[key] = value
			}
			w := httptest.NewRecorder()
			if err := renderer.RenderPartial(w, "whatif-spending-optimizer", map[string]any{"SpendingOptimizerForm": form}); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			if !strings.Contains(body, `id="spending-boost-enabled" name="boost_enabled" type="checkbox" value="on" checked`) {
				t.Fatal("saved boost was not initialized as enabled")
			}
			if !strings.Contains(body, "500.00") || !strings.Contains(body, "2031-09") || !strings.Contains(body, tc.wantCopy) {
				t.Fatalf("saved boost state missing from form: %s", body)
			}
		})
	}
}

func TestSpendingOptimizerRenderResultsEvidence(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	current := models.SpendingCandidate{
		ID: "current", Kind: "current", Baseline: true,
		BaseMonthlyLivingExpenses: 8000.004, StartingMonthlyLivingReal: 8500,
		Metrics: &models.SpendingRiskMetrics{
			Runs: 1000, MedianNearTermMonthlyReal: 8000.004,
			CutPaths: 0, FloorShortfallPaths: 0, UnpaidObligationPaths: 0,
		},
	}
	alternative := models.SpendingCandidate{
		ID: "flex-9000", Kind: "flexible", Qualifies: true,
		BaseMonthlyLivingExpenses: 9000, StartingMonthlyLivingReal: 9500,
		LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2031-09"},
		Guardrails:          &models.GuardrailConfig{Enabled: true, FloorCutPct: 5, CeilingRaisePct: 0, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000},
		Metrics: &models.SpendingRiskMetrics{
			Runs: 1000, MedianNearTermMonthlyReal: 9000.006,
			CutPaths: 250, EarlyCutPaths: 100,
			FloorShortfallPaths: 2, UnpaidObligationPaths: 1,
			DepletionPaths: 120, DepletionFloorFundedPaths: 90,
			MedianFirstCutMonth: float64Pointer(36), MedianMaxCutReal: float64Pointer(250),
			MedianMaxCutPct: float64Pointer(25), MedianMonthsBelowPlan: float64Pointer(18),
			LowestObservedMonthlyReal: 6000, LargestFloorGapReal: 1000,
			LowestObservedMonth: 90, LongestFloorShortfallMonths: 4,
		},
	}
	planned := alternative
	planned.ID, planned.Kind, planned.BaseMonthlyLivingExpenses, planned.StartingMonthlyLivingReal = "planned-8500", "planned", 8500, 9000
	planned.Metrics = &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8500, CutPaths: 0, FloorShortfallPaths: 0, UnpaidObligationPaths: 0}
	failed := alternative
	failed.ID, failed.Kind, failed.Qualifies, failed.BaseMonthlyLivingExpenses = "flex-9250", "flexible", false, 9250

	result := &models.SpendingOptimizerResult{
		Request:    models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, MaxShortfallPct: 5, NearTermYears: 5, SearchMinMonthlyReal: 7000, SearchMaxMonthlyReal: 9500, SearchStepMonthlyReal: 250},
		SearchRuns: 1000, SelectionRuns: 1000, ValidationRuns: 1000,
		EvaluatedCandidates: 4, EffectiveMinMonthlyReal: 7000, EffectiveMaxMonthlyReal: 9500,
		ResolutionMonthlyReal: 250, RangeLimited: true, HorizonMinYears: 20, HorizonMaxYears: 35,
		Candidates:        []models.SpendingCandidate{current, planned, alternative, failed},
		RecommendationIDs: []string{"planned-8500", "flex-9000"},
	}
	view, _, _, err := buildSpendingOptimizerView("render-fixture", result)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Current plan", "Follow planned spending", "Flexible spending",
		"$8,000.00", "$9,000.01", "$1,000.01 more per month than the current plan",
		"No cuts observed across 1,000 checked futures.",
		"Cuts occurred in 25.0% · 250 of 1,000 futures.",
		"Among those 250 futures, the median first cut was in plan year 3.",
		"Below the minimum in 0.2% · 2 of 1,000 futures",
		"at most 5.0% of checked futures",
		"0.2% · 2 / 1,000", "25.0% · 250 / 1,000", "0.0% · 0 / 1,000",
		"cut 5% after a 0% drop",
		"measured on 1,000 final validation futures",
		"Largest observed monthly gap: $1,000.00.",
		"Longest observed below-minimum episode: 4 consecutive months.",
		"These maxima may come from different futures.",
		"Dollar depth: median deepest monthly reduction $250.00.",
		"Percentage depth: median deepest reduction 25.00% of planned living.",
		"Duration: median 18 months below planned living.",
		"These are separate summaries across futures with a cut.",
		"The model recorded depletion in 12.0% · 120 of 1,000 futures; in 90 of those, your minimum stayed funded from that event through the end.",
		"The early-spending boost ends in 2031-09; this planned change is separate from below-plan cuts.",
		"Complete tested frontier",
		"Search reached the upper range", "portfolio-trigger spending rules",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The frontier must fit the card: no forced table width and no
	// constant or details-only columns forcing a horizontal scroll.
	for _, gone := range []string{"min-w-max", ">Final validation futures<", ">Minimum funded after depletion<", "(25.00%)"} {
		if strings.Contains(body, gone) {
			t.Errorf("unexpected %q", gone)
		}
	}
	// Two frontier tables of three columns each (budget, outcomes, actions)
	// is what fits the ~350px strategies card without a horizontal scroll.
	if got := strings.Count(body, `scope="col"`); got != 6 {
		t.Errorf("frontier column headers = %d, want 6", got)
	}
	for _, want := range []string{"Below minimum</span>", "Below-plan cuts</span>", "Near-term funded living</span>"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing stacked outcome label %q", want)
		}
	}
	if got := strings.Count(body, `data-spending-headline`); got != 2 {
		t.Errorf("headline count = %d, want 2", got)
	}
	if got := strings.Count(body, `data-spending-frontier="planned"`); got != 1 {
		t.Errorf("planned frontier count = %d, want 1", got)
	}
	if got := strings.Count(body, `data-spending-frontier="flexible"`); got != 1 {
		t.Errorf("flexible frontier count = %d, want 1", got)
	}
	if !strings.Contains(body, `data-spending-row="flex-9250"`) || strings.Contains(body, `data-spending-apply="flex-9250"`) {
		t.Error("failed frontier row must remain visible and graph-only")
	}
	if !strings.Contains(body, `data-spending-apply="planned-8500"`) || !strings.Contains(body, `data-spending-apply="flex-9000"`) {
		t.Error("every qualifying nonbaseline frontier row needs Apply")
	}
	for _, forbidden := range []string{"income alone", "guaranteed", "risk-free", "global optimum", "Guyton"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("results contain forbidden claim %q", forbidden)
		}
	}
	// The retained aggregates can come from different futures: the largest
	// dollar gap need not be the longest episode, and dollar/percentage cut
	// medians need not describe one cut. Never join them as one experience.
	for _, combined := range []string{"$1,000.00 for as many as 4", "$250.00 (25.00%"} {
		if strings.Contains(body, combined) {
			t.Errorf("independent evidence was combined as one experience: %q", combined)
		}
	}
}

func TestSpendingOptimizerRenderNoUnconditionalMoreClaim(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	current := models.SpendingCandidate{ID: "current", Kind: "current", Baseline: true, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8000}}
	for _, tc := range []struct {
		name           string
		currentMetrics *models.SpendingRiskMetrics
		alternative    float64
	}{
		{name: "unavailable current", currentMetrics: nil, alternative: 9000},
		{name: "equal", currentMetrics: current.Metrics, alternative: 8000},
		{name: "lower", currentMetrics: current.Metrics, alternative: 7500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := current
			baseline.Metrics = tc.currentMetrics
			flexible := models.SpendingCandidate{ID: "flexible", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: tc.alternative, Guardrails: &models.GuardrailConfig{Enabled: true}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: tc.alternative}}
			result := &models.SpendingOptimizerResult{Request: models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5}, Candidates: []models.SpendingCandidate{baseline, flexible}, RecommendationIDs: []string{"flexible"}}
			view, _, _, err := buildSpendingOptimizerView("neutral-label", result)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			if strings.Contains(body, "More spending") || strings.Contains(body, "more per month") {
				t.Fatalf("neutral/lower evidence rendered a positive-more claim: %s", body)
			}
			if !strings.Contains(body, "Flexible spending") {
				t.Fatal("neutral flexible-spending label missing")
			}
		})
	}
}

func TestSpendingOptimizerRenderNilOrZeroSampleMetricsUnavailable(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, metrics := range []*models.SpendingRiskMetrics{nil, {Runs: 0, MedianNearTermMonthlyReal: 8000}} {
		result := &models.SpendingOptimizerResult{
			Request: models.SpendingOptimizerRequest{FloorMonthlyReal: 7000},
			Candidates: []models.SpendingCandidate{
				{ID: "current", Kind: "current", Baseline: true, Metrics: metrics},
				{ID: "zero-sample", Kind: "flexible", Metrics: &models.SpendingRiskMetrics{Runs: 0, DepletionPaths: 0, DepletionFloorFundedPaths: 0}},
			},
		}
		view, _, _, err := buildSpendingOptimizerView("unavailable-metrics", result)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Unavailable") {
			t.Fatal("missing or zero-sample metrics must render Unavailable")
		}
		if strings.Contains(body, "0.00%") || strings.Contains(body, "0 / 0") || view.CurrentMetricsAvailable {
			t.Fatal("missing or zero-sample metrics fabricated percentage evidence")
		}
		if !strings.Contains(body, `data-spending-row="zero-sample"`) {
			t.Fatal("zero-sample nonbaseline frontier row missing")
		}
	}
}

func TestSpendingOptimizerManualRulesAllowZeroAndFloorAboveBase(t *testing.T) {
	for _, stopMonth := range []string{"2025-01", "2035-01"} {
		t.Run(stopMonth, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			s.StartDate = "2030-01"
			s.MonthlyLivingExpenses = 8000
			s.PortfolioValue = 1234567
			s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1500, StopMonth: stopMonth}
			s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 20, CeilingRaisePct: 5, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 9000}
			if err := rm.Save(s); err != nil {
				t.Fatal(err)
			}
			form := url.Values{
				"enabled": {"on"}, "floor_drop_pct": {"20"}, "floor_cut_pct": {"0"},
				"ceiling_rise_pct": {"20"}, "ceiling_raise_pct": {"0"},
				"min_spending_pct": {"100"}, "max_spending_pct": {"100"},
				"min_monthly_spending_real": {"9000"},
			}
			r := httptest.NewRequest("POST", "/whatif/guardrails", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleWhatIfGuardrails(w, r)
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			got, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got.Guardrails.FloorCutPct != 0 || got.Guardrails.CeilingRaisePct != 0 || got.Guardrails.MinMonthlySpendingReal != 9000 {
				t.Fatalf("planned policy was coerced: %#v", got.Guardrails)
			}
			if got.MonthlyLivingExpenses != 8000 || got.PortfolioValue != 1234567 || got.LivingSpendingBoost == nil || got.LivingSpendingBoost.MonthlyReal != 1500 || got.LivingSpendingBoost.StopMonth != stopMonth {
				t.Fatalf("ordinary save changed unrelated spending settings: %#v", got)
			}
		})
	}
}

// TestSpendingOptimizerBrowserFixture writes only server-rendered fixtures when
// the standalone Playwright oracle supplies SPENDING_BROWSER_FIXTURE. The
// browser test never authors financial answer markup in JavaScript.
func TestSpendingOptimizerBrowserFixture(t *testing.T) {
	path := os.Getenv("SPENDING_BROWSER_FIXTURE")
	if path == "" {
		t.Skip("fixture output requested only by spending_optimizer_browser.cjs")
	}
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := httptest.NewRecorder()
	formData := map[string]any{"SpendingOptimizerForm": map[string]any{
		"FloorMonthlyReal": nil, "NearTermYears": 5,
		"CurrentBaseMonthlyReal": 8000.0, "CurrentStartingMonthlyReal": 8500.0,
		"StartMonth": "2026-09", "ExpectedEndMonth": "2056-08",
	}}
	if err := renderer.RenderPartial(form, "whatif-spending-optimizer", formData); err != nil {
		t.Fatal(err)
	}

	current := models.SpendingCandidate{ID: "current", Kind: "current", Baseline: true, BaseMonthlyLivingExpenses: 8000, StartingMonthlyLivingReal: 8500, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8000}}
	nonHeadline := models.SpendingCandidate{ID: "planned-8250", Kind: "planned", Qualifies: true, BaseMonthlyLivingExpenses: 8250, StartingMonthlyLivingReal: 8750, Guardrails: &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8250}}
	planned := models.SpendingCandidate{ID: "planned-8500", Kind: "planned", Qualifies: true, BaseMonthlyLivingExpenses: 8500, StartingMonthlyLivingReal: 9000, Guardrails: &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8500}}
	flexible := models.SpendingCandidate{ID: "flex-9000", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: 9000, StartingMonthlyLivingReal: 9500, LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2027-03"}, Guardrails: &models.GuardrailConfig{Enabled: true, FloorCutPct: 5, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 9000, CutPaths: 250, MedianFirstCutMonth: float64Pointer(36), DepletionPaths: 120, DepletionFloorFundedPaths: 90}}
	failed := flexible
	failed.ID, failed.Qualifies, failed.BaseMonthlyLivingExpenses = "flex-9250", false, 9250
	result := &models.SpendingOptimizerResult{Request: models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5, SearchMinMonthlyReal: 7000, SearchMaxMonthlyReal: 9500, SearchStepMonthlyReal: 250}, SearchRuns: 1000, SelectionRuns: 1000, ValidationRuns: 1000, EffectiveMinMonthlyReal: 7000, EffectiveMaxMonthlyReal: 9500, ResolutionMonthlyReal: 250, Candidates: []models.SpendingCandidate{current, nonHeadline, planned, flexible, failed}, RecommendationIDs: []string{"planned-8500", "flex-9000"}}
	view, _, _, err := buildSpendingOptimizerView("browser-fixture", result)
	if err != nil {
		t.Fatal(err)
	}
	results := httptest.NewRecorder()
	if err := renderer.RenderPartial(results, "whatif-spending-optimizer-results", view); err != nil {
		t.Fatal(err)
	}
	renderResult := func(value *models.SpendingOptimizerResult, id string) string {
		t.Helper()
		v, _, _, buildErr := buildSpendingOptimizerView(id, value)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		out := httptest.NewRecorder()
		if renderErr := renderer.RenderPartial(out, "whatif-spending-optimizer-results", v); renderErr != nil {
			t.Fatal(renderErr)
		}
		return out.Body.String()
	}
	nilResult := &models.SpendingOptimizerResult{Request: result.Request, Candidates: []models.SpendingCandidate{{ID: "current", Kind: "current", Baseline: true}, {ID: "zero-sample", Kind: "flexible", Metrics: &models.SpendingRiskMetrics{}}}}
	failingCurrent := current
	failingCurrent.Metrics = &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8000, CutPaths: 0, FloorShortfallPaths: 5, LargestFloorGapReal: 1000, LongestFloorShortfallMonths: 6}
	noResult := &models.SpendingOptimizerResult{Request: result.Request, SelectionRuns: 1000, ValidationRuns: 1000, EffectiveMinMonthlyReal: 7000, EffectiveMaxMonthlyReal: 9500, ResolutionMonthlyReal: 250, RangeLimited: true, Candidates: []models.SpendingCandidate{failingCurrent, failed}}

	fundingMonths := make([]models.SpendingFundingMonth, 12)
	calendarStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	for month := range fundingMonths {
		fundingMonths[month] = models.SpendingFundingMonth{
			Month: month, CalendarMonth: calendarStart.AddDate(0, month, 0).Format("2006-01"),
			TaxDeferredWithdrawalReal: 2000, TaxesPaidReal: 500, PlannedLivingReal: 9500, FundedLivingReal: 9000, HealthcareReal: 300,
		}
		if month >= 3 {
			fundingMonths[month].OtherConfiguredIncomeReal = 1000
		}
		if month >= 6 {
			fundingMonths[month].SocialSecurityReal = 1800
			fundingMonths[month].PlannedLivingReal = 9000
			fundingMonths[month].FundedLivingReal = 8900
		}
	}
	graph := spendingGraphPayload{
		Candidate: flexible, CandidateLabel: spendingGraphCandidateLabel(flexible), Request: result.Request, FloorMonthlyReal: 7000,
		ValidationRuns: 1000, ValidationSeed: "9223372036854775700", DisplayDollars: "real",
		BaseCaseLabel: "Base case — canonical configured assumptions",
		BaseCaseChart: map[string]interface{}{"data": []map[string]interface{}{{"name": "Portfolio Balance", "x": []int{0, 1}, "y": []float64{500000, 450000}, "line": map[string]string{}}}, "layout": map[string]interface{}{"xaxis": map[string]interface{}{}, "yaxis": map[string]interface{}{}}},
		FundingTimeline: &models.SpendingFundingTimeline{
			Title: "Income and withdrawals over time — base case", StartMonth: "2026-09", ObservedEndMonth: "2027-08", ExpectedEndMonth: "2056-08", ExpectedMonths: 360, ObservedMonths: 12, Complete: false, EndReason: "base_case_projection_ended",
			AccountingNote: "Configured income and account withdrawals are shown without double counting transfers.", CoverageNote: "This view does not certify dependable-income coverage.", AvailabilityNote: "Later base-case months are unavailable; separate Monte Carlo minimum evidence covers complete modeled horizons.",
			Months:         fundingMonths,
			Markers:        []models.SpendingFundingMarker{{Month: 3, CalendarMonth: "2026-12", Kind: "configured_income_start", Label: "Pension starts"}, {Month: 6, CalendarMonth: "2027-03", Kind: "social_security_start", Label: "Social Security starts"}, {Month: 6, CalendarMonth: "2027-03", Kind: "living_spending_boost_stop", Label: "Early-spending boost stops"}},
			AnnualAverages: []models.SpendingFundingAnnualAverage{{CalendarYear: 2026, ObservedMonths: 4, Partial: true, OtherConfiguredIncomeReal: 250, TaxDeferredWithdrawalReal: 2000, TaxesPaidReal: 500, PlannedLivingReal: 9500, FundedLivingReal: 9000, HealthcareReal: 300}, {CalendarYear: 2027, ObservedMonths: 8, Partial: true, SocialSecurityReal: 1350, OtherConfiguredIncomeReal: 1000, TaxDeferredWithdrawalReal: 2000, TaxesPaidReal: 500, PlannedLivingReal: 9125, FundedLivingReal: 8925, HealthcareReal: 300}},
		},
		Simulated: spendingGraphSeries{Label: "Simulated annual outcomes — pointwise percentiles", Years: []int{1, 2}, PathCounts: []int{1000, 900}, LivingP10: []float64{7000, 7100}, LivingP50: []float64{9000, 9100}, LivingP90: []float64{10000, 10100}, PortfolioP10: []float64{100000, 90000}, PortfolioP50: []float64{500000, 450000}, PortfolioP90: []float64{900000, 850000}, Floor: float64Pointer(7000)},
		Worst:     spendingGraphSeries{Label: "Lowest observed living path — one retained simulated future", Years: []int{1, 2}, LivingP50: []float64{7000, 6000}, PortfolioP50: []float64{100000, 0}, Floor: float64Pointer(7000)},
	}
	graph.Candidate.Metrics.LowestObservedMonth = 18
	graph.Candidate.Metrics.LowestObservedMonthlyReal = 6000

	payload, err := json.Marshal(map[string]any{"form": form.Body.String(), "results": results.Body.String(), "nil_results": renderResult(nilResult, "nil-browser-fixture"), "no_results": renderResult(noResult, "no-browser-fixture"), "graph": graph})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
