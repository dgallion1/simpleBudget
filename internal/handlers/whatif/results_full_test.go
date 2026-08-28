package whatif

// Tests for the fast-first results partial: mutating handlers render
// immediately from the cheap RunFast analysis on a cache miss (never
// invoking RunFull), and GET /whatif/results-full serves the expensive
// analysis behind that render's async loader, guarding against a settings
// change during the multi-second compute.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
)

// TestCalculateCacheMissRendersFastWithLoader proves a cache-missing
// POST /whatif/calculate never invokes RunFull: it renders the fast
// analysis immediately with the async loader and pending skeletons in
// place of the expensive cards.
func TestCalculateCacheMissRendersFastWithLoader(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, wantHash, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}

	var calls int32
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		return retirement.RunFull(eng, in)
	})

	w := httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, `id="whatif-async-loader"`) {
		t.Error("expected the async loader div in a cache-miss render")
	}
	// Extract the exact rendered hash and require EQUALITY with the
	// independently-computed dep-hash — a strings.Contains(body, "hash="+want)
	// substring check would still pass if the render appended a suffix (e.g.
	// analysisFastOrCached returning depHash+"x", since "hash=X" is a prefix
	// of "hash=Xx"). Equality is what actually pins render->endpoint
	// agreement.
	m := loaderHashRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("could not find the loader's hash in the rendered body:\n%s", body)
	}
	if m[1] != wantHash {
		t.Errorf("loader hash = %q, want %q (independently computed via buildEngineInput)", m[1], wantHash)
	}
	if !strings.Contains(body, "data-wf-pending") {
		t.Error("expected skeleton pending cards in place of the expensive sections")
	}
	// "scenarios with year-by-year" is unique to the real Monte Carlo card
	// (whatif-monte-carlo.html); the pending skeleton reuses the "Monte
	// Carlo Simulation" title text, so asserting on the heading alone would
	// not distinguish the two.
	if strings.Contains(body, "scenarios with year-by-year") {
		t.Error("the real Monte Carlo card must not render on a cache miss")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("RunFull executed %d times on a cache miss, want 0", got)
	}
}

// TestCalculateCacheHitRendersFull proves a warm cache still renders the
// full analysis synchronously, with no async loader.
func TestCalculateCacheHitRendersFull(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	in, hash, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}

	full := retirement.RunFast(getEngine(), in)
	full.MonteCarlo = &models.MonteCarloAnalysis{
		Stats:        &models.MonteCarloStats{Runs: 1000, SuccessRate: 87.5},
		Distribution: &models.MonteCarloDistribution{},
	}
	full.FailurePoints = &models.FailurePointAnalysis{BaselineSurvives: true}
	// HistoricalBacktest and SocialSecurity stay nil: both are
	// template-guarded (`{{if .Analysis.HistoricalBacktest}}` /
	// `{{if .Analysis.SocialSecurity}}`).

	cache.mu.Lock()
	cache.hash = hash
	cache.analysis = full
	cache.cachedAt = time.Now()
	cache.mu.Unlock()

	var calls int32
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		return retirement.RunFull(eng, in)
	})

	w := httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, `id="whatif-async-loader"`) {
		t.Error("a cache hit must not render the async loader")
	}
	if !strings.Contains(body, "scenarios with year-by-year") {
		t.Error("expected the real Monte Carlo card on a cache hit")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("RunFull executed %d times on a cache hit, want 0", got)
	}
}

// TestResultsFullStaleHashNoContent proves a stale hash never triggers the
// expensive fan-out: it answers 204 before computing anything.
func TestResultsFullStaleHashNoContent(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	var calls int32
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		return retirement.RunFull(eng, in)
	})

	w := httptest.NewRecorder()
	handleWhatIfResultsFull(w, httptest.NewRequest("GET", "/whatif/results-full?hash=deadbeef", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("RunFull executed %d times for a stale hash, want 0", got)
	}
}

// TestResultsFullRendersFullPartial proves the correct-hash path renders the
// full partial with no loader and no OOB swaps.
func TestResultsFullRendersFullPartial(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	in, hash, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}

	swapRunFull(t, func(eng *engine.Engine, _ engine.Input) *models.WhatIfAnalysis {
		full := retirement.RunFast(eng, in)
		full.MonteCarlo = &models.MonteCarloAnalysis{
			Stats:        &models.MonteCarloStats{Runs: 1000, SuccessRate: 90},
			Distribution: &models.MonteCarloDistribution{},
		}
		full.FailurePoints = &models.FailurePointAnalysis{BaselineSurvives: true}
		return full
	})

	w := httptest.NewRecorder()
	handleWhatIfResultsFull(w, httptest.NewRequest("GET", "/whatif/results-full?hash="+hash, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "scenarios with year-by-year") {
		t.Error("expected the real Monte Carlo card")
	}
	if strings.Contains(body, `id="whatif-async-loader"`) {
		t.Error("the full partial must not re-embed the async loader")
	}
	if strings.Contains(body, "hx-swap-oob") {
		t.Error("the async fetch must not carry OOB swaps")
	}
}

// TestResultsFullDetectsSettingsChangeDuringCompute proves the post-compute
// hash re-check: if settings change while the flight runs, the response
// must be 204 rather than clobbering the newer render with stale figures.
func TestResultsFullDetectsSettingsChangeDuringCompute(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, preHash, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}

	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		mutated, err := rm.Load()
		if err != nil {
			t.Fatalf("Load during flight: %v", err)
		}
		mutated.MonthlyLivingExpenses += 500
		if err := rm.Save(mutated); err != nil {
			t.Fatalf("Save during flight: %v", err)
		}
		return retirement.RunFull(eng, in)
	})

	w := httptest.NewRecorder()
	handleWhatIfResultsFull(w, httptest.NewRequest("GET", "/whatif/results-full?hash="+preHash, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (settings changed while the flight ran)", w.Code)
	}
}

// TestChartEndpointsDoNotBlockOnCacheMiss proves the chart JSON endpoints
// serve the projection from the fast path on a cache miss instead of
// blocking behind (or triggering) the expensive fan-out.
func TestChartEndpointsDoNotBlockOnCacheMiss(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	var calls int32
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		return retirement.RunFull(eng, in)
	})

	cases := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/whatif/chart/projection", handleWhatIfProjectionChart},
		{"/whatif/chart/income", handleWhatIfIncomeChart},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		tc.handler(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200. body: %s", tc.path, w.Code, w.Body.String())
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s: invalid JSON: %v", tc.path, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("RunFull executed %d times for chart endpoints on a cache miss, want 0", got)
	}
}

// TestPollCacheMissRendersPending proves a stale poll on a cold cache
// renders the fast analysis with the async loader instead of blocking for
// ~7s behind the full fan-out.
func TestPollCacheMissRendersPending(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, wantHash, err := buildEngineInput(settings)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/whatif/poll?since=%d", rm.Revision()-1), nil)
	handleWhatIfPoll(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="whatif-async-loader"`) {
		t.Error("expected the async loader on a cache-miss poll render")
	}
	// Pin the poll render's loader hash to the independently-computed
	// dep-hash via exact equality (not substring containment — see the
	// comment in TestCalculateCacheMissRendersFastWithLoader for why
	// strings.Contains is insufficient here).
	m := loaderHashRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("could not find the loader's hash in the rendered body:\n%s", body)
	}
	if m[1] != wantHash {
		t.Errorf("loader hash = %q, want %q (independently computed via buildEngineInput)", m[1], wantHash)
	}
}

// loaderHashRE extracts the hash query param from the async loader's
// rendered hx-get target, e.g. hx-get="/whatif/results-full?hash=abc123".
var loaderHashRE = regexp.MustCompile(`hx-get="/whatif/results-full\?hash=([^"]+)"`)

// TestPendingLoaderHashRoundTripsToFullResults pins render->endpoint hash
// agreement end-to-end: it extracts the hash from the pending render's
// loader div (rather than computing it independently, as the tests above
// do) and confirms GET /whatif/results-full accepts exactly that value,
// regardless of how either side happens to compute a dep-hash. This is the
// mutation the checker found uncaught: a corrupted AsyncHash (e.g.
// analysisFastOrCached returning depHash+"x") renders a loader whose own
// GET immediately 204s instead of ever reaching the full partial.
func TestPendingLoaderHashRoundTripsToFullResults(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// Cache miss: POST /whatif/calculate renders the fast analysis with the
	// pending loader.
	w := httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	m := loaderHashRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("could not find loader hash in rendered body:\n%s", body)
	}
	extractedHash := m[1]
	if extractedHash == "" {
		t.Fatal("extracted loader hash is empty")
	}

	// Stub the expensive fan-out so the follow-up GET is cheap, as the other
	// results-full tests in this file do.
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		full := retirement.RunFast(eng, in)
		full.MonteCarlo = &models.MonteCarloAnalysis{
			Stats:        &models.MonteCarloStats{Runs: 1000, SuccessRate: 90},
			Distribution: &models.MonteCarloDistribution{},
		}
		full.FailurePoints = &models.FailurePointAnalysis{BaselineSurvives: true}
		return full
	})

	w2 := httptest.NewRecorder()
	handleWhatIfResultsFull(w2, httptest.NewRequest("GET", "/whatif/results-full?hash="+extractedHash, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the loader's own hash must be accepted by results-full). body: %s", w2.Code, w2.Body.String())
	}
	fullBody := w2.Body.String()
	if !strings.Contains(fullBody, "scenarios with year-by-year") {
		t.Error("expected the real Monte Carlo card in the round-tripped full partial")
	}
	if strings.Contains(fullBody, `id="whatif-async-loader"`) {
		t.Error("the full partial must not re-embed the async loader")
	}
}
