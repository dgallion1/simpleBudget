package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestSpendingGraphRetainedEvidenceAndAuthorization(t *testing.T) {
	rm, _ := spendingFixture(t)
	c := spendingAccepted()
	id, _ := spendingSeed(t, rm, c)
	before := spendingBytes(t, rm)
	for _, mode := range []string{"real", "nominal"} {
		w := httptest.NewRecorder()
		handleSpendingOptimizerGraph(w, spendingPost(url.Values{"request_id": {id}, "candidate": {"graph-token"}, "display_dollars": {mode}}))
		if w.Code != 200 {
			t.Fatalf("graph %d %s", w.Code, w.Body.String())
		}
		var got spendingGraphPayload
		if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got.Candidate, c) || got.CandidateLabel != "Flexible spending at $10,000.00 per month" || got.ValidationSeed != "9223372036854775807" || got.DisplayDollars != mode || got.FloorMonthlyReal != 6000 {
			t.Fatalf("graph evidence differs %+v", got)
		}
		if mode == "nominal" && got.Simulated.Floor != nil {
			t.Fatal("nominal chart has real floor")
		}
		if mode == "real" && (got.Simulated.Floor == nil || *got.Simulated.Floor != 6000) {
			t.Fatal("real floor missing")
		}
	}
	w := httptest.NewRecorder()
	handleSpendingOptimizerGraph(w, spendingPost(url.Values{"request_id": {id}, "candidate": {"apply-token"}}))
	if w.Code != 409 {
		t.Fatal("apply token authorized graph")
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("graph saved")
	}
}
func TestSpendingGraphCancellationDuringCanonicalWork(t *testing.T) {
	rm, _ := spendingFixture(t)
	id, _ := spendingSeed(t, rm, spendingAccepted())
	before := spendingBytes(t, rm)
	w := httptest.NewRecorder()
	handleSpendingOptimizerGraphWithRunner(w, spendingPost(url.Values{"request_id": {id}, "candidate": {"graph-token"}}), func(in engine.Input) *models.ProjectionResult {
		handleCancelSpendingOptimizer(httptest.NewRecorder(), spendingPost(url.Values{"request_id": {id}}))
		return getEngine().Run(in)
	})
	if w.Code != 409 {
		t.Fatalf("cancelled graph published %d", w.Code)
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("graph saved")
	}
}

func TestSpendingGraphRejections(t *testing.T) {
	for _, mode := range []string{"expiry", "revision", "fingerprint", "manager", "scenario", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			id, p := spendingSeed(t, rm, spendingAccepted())
			var expected string
			w := httptest.NewRecorder()
			handleSpendingOptimizerGraphWithRunner(w, spendingPost(url.Values{"request_id": {id}, "candidate": {"graph-token"}}), func(in engine.Input) *models.ProjectionResult {
				projection := getEngine().Run(in)
				switch mode {
				case "expiry":
					p.expires = time.Now().Add(-time.Minute)
				case "revision":
					s, _ := rm.Load()
					s.DiscountRate++
					if e := rm.Save(s); e != nil {
						t.Fatal(e)
					}
				case "fingerprint":
					p.fingerprint = [32]byte{}
				case "manager":
					p.manager = nil
				case "scenario":
					if _, e := rm.CreateScenario("Other"); e != nil {
						t.Fatal(e)
					}
				case "cancel":
					handleCancelSpendingOptimizer(httptest.NewRecorder(), spendingPost(url.Values{"request_id": {id}}))
				}
				expected = spendingBytes(t, rm)
				return projection
			})
			if w.Code != 409 {
				t.Fatalf("stale graph %d %s", w.Code, w.Body.String())
			}
			if spendingBytes(t, rm) != expected {
				t.Fatal("graph wrote settings")
			}
		})
	}
}

func TestSpendingGraphTimelineUsesExactSelectedCandidateAndBenefitMarkers(t *testing.T) {
	rm, s := spendingFixture(t)
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2400, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true}
	s.IncomeSources = []models.IncomeSource{
		{ID: "manual-ss", Name: "Social Security", Amount: 9999, StartMonth: 0},
		{ID: "pension", Name: "Pension", Amount: 1200, StartMonth: 6},
	}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	rm.InvalidateCache()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	alternative := spendingAccepted()
	id, preview := spendingSeed(t, rm, alternative)
	current := models.SpendingCandidate{
		ID:                        "current",
		Kind:                      "current",
		BaseMonthlyLivingExpenses: s.MonthlyLivingExpenses,
		StartingMonthlyLivingReal: 6900,
		LivingSpendingBoost:       models.CloneLivingSpendingBoost(s.LivingSpendingBoost),
		Guardrails:                s.Guardrails,
		Baseline:                  true,
	}
	spendingPreviews.Lock()
	preview.graphs["current-graph"] = current
	spendingPreviews.Unlock()

	get := func(token string) spendingGraphPayload {
		t.Helper()
		w := httptest.NewRecorder()
		handleSpendingOptimizerGraph(w, spendingPost(url.Values{"request_id": {id}, "candidate": {token}}))
		if w.Code != 200 {
			t.Fatalf("graph %s: %d %s", token, w.Code, w.Body.String())
		}
		var payload spendingGraphPayload
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	alternativeGraph := get("graph-token")
	currentGraph := get("current-graph")
	if alternativeGraph.FundingTimeline == nil || currentGraph.FundingTimeline == nil {
		t.Fatal("funding timeline missing")
	}
	if got := alternativeGraph.FundingTimeline.Months[0].PlannedLivingReal; got != 9000 {
		t.Fatalf("alternative month-zero planned living = %.2f, want 9000", got)
	}
	if got := currentGraph.FundingTimeline.Months[0].PlannedLivingReal; got != 6900 {
		t.Fatalf("current month-zero planned living = %.2f, want 6900", got)
	}
	if alternativeGraph.FundingTimeline.Months[23].SocialSecurityReal != 0 || alternativeGraph.FundingTimeline.Months[24].SocialSecurityReal <= 0 {
		t.Fatalf("SS claim timing month23/month24 = %.2f/%.2f", alternativeGraph.FundingTimeline.Months[23].SocialSecurityReal, alternativeGraph.FundingTimeline.Months[24].SocialSecurityReal)
	}
	if alternativeGraph.FundingTimeline.Months[0].OtherConfiguredIncomeReal != 0 || alternativeGraph.FundingTimeline.Months[6].OtherConfiguredIncomeReal <= 0 {
		t.Fatal("configured pension timing is not from candidate settings")
	}
	wantMarkers := []models.SpendingFundingMarker{
		{Month: 6, CalendarMonth: "2027-03", Kind: "configured_income_start", Label: "Pension starts"},
		{Month: 24, CalendarMonth: "2028-09", Kind: "social_security_start", Label: "Your Social Security starts"},
		{Month: 120, CalendarMonth: "2036-09", Kind: "living_spending_boost_stop", Label: "Early-spending boost stops"},
	}
	if !reflect.DeepEqual(alternativeGraph.FundingTimeline.Markers, wantMarkers) {
		t.Fatalf("markers = %+v, want %+v", alternativeGraph.FundingTimeline.Markers, wantMarkers)
	}
	if annual := alternativeGraph.FundingTimeline.AnnualAverages; len(annual) < 2 || annual[0].CalendarYear != 2026 || annual[0].ObservedMonths != 4 || !annual[0].Partial || annual[1].CalendarYear != 2027 || annual[1].ObservedMonths != 12 || annual[1].Partial {
		t.Fatalf("calendar-year averages = %+v", annual)
	}
}

func TestSpendingGraphTimelineReportsTruncatedCanonicalEvidenceWithoutFutureMarkers(t *testing.T) {
	rm, s := spendingFixture(t)
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2400, FRA: 67, ClaimAge: 67}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	rm.InvalidateCache()
	id, _ := spendingSeed(t, rm, spendingAccepted())
	w := httptest.NewRecorder()
	handleSpendingOptimizerGraphWithRunner(w, spendingPost(url.Values{"request_id": {id}, "candidate": {"graph-token"}}), func(in engine.Input) *models.ProjectionResult {
		projection := getEngine().Run(in)
		projection.Months = projection.Months[:12]
		depletionMonth := 11
		projection.DepletionMonth = &depletionMonth
		projection.Survives = false
		return projection
	})
	if w.Code != 200 {
		t.Fatalf("graph %d %s", w.Code, w.Body.String())
	}
	var got spendingGraphPayload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	timeline := got.FundingTimeline
	if timeline == nil || timeline.Complete || timeline.EndReason != "base_case_projection_ended" || timeline.ObservedMonths != 12 || timeline.ExpectedMonths != 144 || timeline.ObservedEndMonth != "2027-08" || timeline.AvailabilityNote == "" {
		t.Fatalf("truncated timeline = %+v", timeline)
	}
	for _, marker := range timeline.Markers {
		if marker.Kind == "social_security_start" || marker.Kind == "living_spending_boost_stop" {
			t.Fatalf("future marker included in truncated evidence: %+v", marker)
		}
	}
}

func TestSpendingGraphTimelineReplayAfterApplyIsRejected(t *testing.T) {
	rm, _ := spendingFixture(t)
	id, _ := spendingSeed(t, rm, spendingAccepted())
	apply := httptest.NewRecorder()
	handleApplySpendingOptimizer(apply, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
	if apply.Code != 200 {
		t.Fatalf("apply %d %s", apply.Code, apply.Body.String())
	}
	graph := httptest.NewRecorder()
	handleSpendingOptimizerGraph(graph, spendingPost(url.Values{"request_id": {id}, "candidate": {"graph-token"}}))
	if graph.Code != 409 {
		t.Fatalf("post-Apply graph replay = %d, want 409", graph.Code)
	}
}
