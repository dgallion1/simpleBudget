package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// spendingRealApply runs a genuine search-and-apply round trip through the
// router-registered handlers (handleSpendingOptimizer, handleApplySpendingOptimizer)
// with the real optimizer, exactly like TestSpendingOptimizerRealPreviewApply,
// and returns the manager, the published optimizer result, the applied row,
// and the settings as saved.
func spendingRealApply(t *testing.T) (*retirement.SettingsManager, models.SpendingOptimizerResult, spendingOptimizerRow, *models.WhatIfSettings) {
	t.Helper()
	rm, s := spendingFixture(t)
	s.ProjectionYears = 1
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	handleSpendingOptimizer(w, spendingPost(url.Values{"request_id": {"applied-evidence-preview"}, "floor_monthly_real": {"6000"}, "search_min_monthly_real": {"8000"}, "search_max_monthly_real": {"8100"}, "search_step_monthly_real": {"100"}, "seed": {"777777"}, "boost_enabled": {"on"}, "boost_monthly_real": {"1000"}, "boost_stop_month": {"2027-09"}}))
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var view spendingOptimizerView
	if e := json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	var chosen spendingOptimizerRow
	for _, row := range view.Rows {
		if row.Token != "" && !row.Headline {
			chosen = row
			break
		}
	}
	if chosen.Token == "" {
		t.Fatal("no qualifying non-headline option to apply")
	}
	apply := httptest.NewRecorder()
	handleApplySpendingOptimizer(apply, spendingPost(url.Values{"request_id": {view.RequestID}, "recommendation": {chosen.Token}}))
	if apply.Code != 200 {
		t.Fatalf("apply %d %s", apply.Code, apply.Body.String())
	}
	got, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	return rm, *view.Optimizer, chosen, got
}

// SP2 (a): a real Apply round trip through the router persists
// AppliedSpendingEvidence -- the applied candidate, the preview's exact
// seeds/validation-run count, an applied_at date, and a settings_hash that
// verifies -- in the SAME revision increment as SP1's SpendingSearch fields.
func TestAppliedSpendingEvidenceSavedInSameRevisionAsSearchPreferences(t *testing.T) {
	_, optimizer, chosen, got := spendingRealApply(t)
	if got.SpendingSearch == nil {
		t.Fatal("SP1 field missing from the same save")
	}
	evidence := got.AppliedSpendingEvidence
	if evidence == nil {
		t.Fatal("applied spending evidence not saved")
	}
	if !reflect.DeepEqual(evidence.Candidate, chosen.Candidate) {
		t.Fatalf("evidence candidate %#v want %#v", evidence.Candidate, chosen.Candidate)
	}
	if evidence.SearchSeed != optimizer.SearchSeed || evidence.SelectionSeed != optimizer.SelectionSeed || evidence.ValidationSeed != optimizer.ValidationSeed || evidence.ValidationRuns != optimizer.ValidationRuns {
		t.Fatalf("evidence seeds %#v want search=%d selection=%d validation=%d runs=%d", evidence, optimizer.SearchSeed, optimizer.SelectionSeed, optimizer.ValidationSeed, optimizer.ValidationRuns)
	}
	if evidence.AppliedAt == "" {
		t.Fatal("applied_at not recorded")
	}
	if !appliedSpendingEvidenceFresh(got) {
		t.Fatal("settings_hash does not verify against the settings it was saved with")
	}
}

// SP2 (c): the applied-graph endpoint reproduces the shared graph core's
// output byte-for-byte from the persisted candidate/request/seeds -- the
// same core the live preview endpoint calls with a live preview's inputs.
func TestAppliedSpendingGraphByteIdenticalToSharedCore(t *testing.T) {
	_, _, _, got := spendingRealApply(t)
	evidence := got.AppliedSpendingEvidence
	if evidence == nil {
		t.Fatal("applied spending evidence not saved")
	}
	for _, mode := range []string{"real", "nominal"} {
		t.Run(mode, func(t *testing.T) {
			w := httptest.NewRecorder()
			handleAppliedSpendingGraph(w, spendingPost(url.Values{"display_dollars": {mode}}))
			if w.Code != 200 {
				t.Fatalf("applied graph %d %s", w.Code, w.Body.String())
			}
			request := evidence.Request
			request.LivingSpendingBoost = models.CloneLivingSpendingBoost(request.LivingSpendingBoost)
			direct, err := buildSpendingGraphPayload(got, cloneSpendingCandidate(evidence.Candidate), request, evidence.SearchSeed, evidence.SelectionSeed, evidence.ValidationSeed, evidence.ValidationRuns, mode, getEngine().Run)
			if err != nil {
				t.Fatal(err)
			}
			directBody, err := json.Marshal(direct)
			if err != nil {
				t.Fatal(err)
			}
			if w.Body.String() != string(directBody) {
				t.Fatalf("applied graph payload differs from direct shared-core call\nendpoint: %s\ndirect:   %s", w.Body.String(), directBody)
			}
		})
	}
}

// SP2 (d): once the plan changes after Apply, the settings_hash guard fires
// and the endpoint answers 409 with an honest message -- never stale evidence.
func TestAppliedSpendingGraphStaleAfterSettingsEdit(t *testing.T) {
	rm, _, _, _ := spendingRealApply(t)
	before := spendingBytes(t, rm)
	s, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	s.DiscountRate++
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	if before == spendingBytes(t, rm) {
		t.Fatal("test setup did not actually edit settings")
	}
	w := httptest.NewRecorder()
	handleAppliedSpendingGraph(w, spendingPost(url.Values{}))
	if w.Code != 409 {
		t.Fatalf("stale graph %d %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.ToLower(body["error"]), "changed") {
		t.Fatalf("dishonest/missing staleness message: %#v", body)
	}
}

// SP2: before any plan has ever been applied, the endpoint is a plain 409 --
// never a panic on a nil evidence pointer.
func TestAppliedSpendingGraphNilEvidence(t *testing.T) {
	spendingFixture(t)
	w := httptest.NewRecorder()
	handleAppliedSpendingGraph(w, spendingPost(url.Values{}))
	if w.Code != 409 {
		t.Fatalf("nil-evidence graph %d %s", w.Code, w.Body.String())
	}
}
