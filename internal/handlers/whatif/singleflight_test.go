package whatif

// Tests for the singleflight coalescing in runAnalysisWithCache: concurrent
// cache-missing requests with the same settings hash must execute the
// expensive retirement.RunFull fan-out exactly once, later requests must be
// served from the cache, failed input building must not poison the cache,
// and a panicking leader must not wedge future requests.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
)

// swapRunFull replaces the runFullFn test seam for the duration of the test.
func swapRunFull(t *testing.T, fn func(*engine.Engine, engine.Input) *models.WhatIfAnalysis) {
	t.Helper()
	orig := runFullFn
	runFullFn = fn
	t.Cleanup(func() { runFullFn = orig })
}

// TestSingleflightConcurrentCalculateRunsFullAnalysisOnce fires 8 concurrent
// cache-missing POST /whatif/calculate requests and proves RunFull executed
// exactly once, then verifies a request after completion is served from the
// cache without recomputing.
func TestSingleflightConcurrentCalculateRunsFullAnalysisOnce(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	const n = 8
	var calls int32
	entered := make(chan struct{}, n)
	release := make(chan struct{})
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		entered <- struct{}{}
		<-release
		return retirement.RunFull(eng, in)
	})

	statuses := make([]int, n)
	bodies := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/calculate", nil)
			handleWhatIfCalculate(w, req)
			statuses[i] = w.Code
			bodies[i] = w.Body.String()
		}(i)
	}

	// Wait until the leader is inside RunFull, give the remaining requests
	// time to queue as singleflight waiters, then let the leader finish.
	// (Correctness does not depend on the sleep: the cache is written before
	// the in-flight call is unregistered, so a straggler that misses the
	// in-flight window hits the cache instead of recomputing.)
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("no request reached RunFull")
	}
	time.Sleep(200 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times for %d concurrent identical requests, want 1", got, n)
	}
	for i := 0; i < n; i++ {
		if statuses[i] != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200\nbody: %s", i, statuses[i], bodies[i])
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("request %d: body differs from request 0 (waiters must share the leader's result)", i)
		}
	}

	// A request AFTER completion must be served from the cache: RunFull
	// count stays at 1.
	w := httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("post-completion request: status = %d, want 200", w.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times after cache warm, want still 1", got)
	}
}

// TestSingleflightErrorResultNotCached verifies that a failed analysis (input
// building error) neither runs RunFull nor populates the cache as a success:
// the next good request computes normally.
func TestSingleflightErrorResultNotCached(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	var calls int32
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		return retirement.RunFull(eng, in)
	})

	bad := models.DefaultWhatIfSettings()
	bad.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "does-not-exist.json", TransitionAge: 70},
	}
	if _, err := runAnalysisWithCache(context.Background(), bad); err == nil {
		t.Fatal("expected error for missing chained scenario")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("RunFull executed %d times on failed input building, want 0", got)
	}

	// The failure must not have been cached as a success: a good request
	// computes, and only then does a repeat request hit the cache.
	good := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), good)
	if err != nil {
		t.Fatalf("good settings: unexpected error: %v", err)
	}
	if analysis == nil {
		t.Fatal("good settings: nil analysis")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times for first good request, want 1", got)
	}
	if _, err := runAnalysisWithCache(context.Background(), good); err != nil {
		t.Fatalf("cached good settings: unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times after cache warm, want still 1", got)
	}
}

// TestSingleflightPanicFailsAllCoalescedRequests verifies the panic
// policy: a RunFull panic is converted to errAnalysisPanicked for every
// request coalesced onto the flight (the flight fn must never panic into
// x/sync singleflight — DoChan would re-raise it on a bare goroutine and
// crash the process), nothing is cached, and a subsequent request
// recomputes.
func TestSingleflightPanicFailsAllCoalescedRequests(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	var calls int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		if atomic.AddInt32(&calls, 1) == 1 {
			entered <- struct{}{}
			<-release
			panic("boom")
		}
		return &models.WhatIfAnalysis{}
	})

	settings := models.DefaultWhatIfSettings()

	leaderErr := make(chan error, 1)
	go func() {
		_, err := runAnalysisWithCache(context.Background(), settings)
		leaderErr <- err
	}()
	<-entered

	waiterErr := make(chan error, 1)
	go func() {
		_, err := runAnalysisWithCache(context.Background(), settings)
		waiterErr <- err
	}()
	time.Sleep(100 * time.Millisecond) // let the waiter queue on the flight
	close(release)

	if err := <-leaderErr; err != errAnalysisPanicked {
		t.Fatalf("leader error = %v, want errAnalysisPanicked", err)
	}
	if err := <-waiterErr; err != errAnalysisPanicked {
		t.Fatalf("waiter error = %v, want errAnalysisPanicked", err)
	}

	// Nothing was cached from the panicked run: a fresh request recomputes
	// and succeeds.
	if _, err := runAnalysisWithCache(context.Background(), settings); err != nil {
		t.Fatalf("request after panic: unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("RunFull executed %d times, want 2 (panicked run + recompute)", got)
	}
}

// TestSingleflightWaiterHonorsContextCancellation: a waiter whose request
// context is cancelled must return promptly with the context error instead
// of staying parked until the (possibly wedged) flight finishes; the flight
// itself keeps running and its result is still cached.
func TestSingleflightWaiterHonorsContextCancellation(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	var calls int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		entered <- struct{}{}
		<-release
		return retirement.RunFull(eng, in)
	})

	settings := models.DefaultWhatIfSettings()

	leaderDone := make(chan error, 1)
	go func() {
		_, err := runAnalysisWithCache(context.Background(), settings)
		leaderDone <- err
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	waiterErr := make(chan error, 1)
	go func() {
		_, err := runAnalysisWithCache(ctx, settings)
		waiterErr <- err
	}()
	time.Sleep(100 * time.Millisecond) // let the waiter queue on the flight
	cancel()

	select {
	case err := <-waiterErr:
		if err != context.Canceled {
			t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled waiter stayed parked on the flight")
	}

	// The flight was NOT cancelled: it completes and caches, so the next
	// request is served without recomputing.
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader: unexpected error: %v", err)
	}
	if _, err := runAnalysisWithCache(context.Background(), settings); err != nil {
		t.Fatalf("post-completion request: unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times, want 1 (cancellation must not kill or duplicate the flight)", got)
	}
}

// TestMonteCarloRerollCoalescesConcurrentRequests: the Monte Carlo re-roll
// endpoint bypasses the cache by design (fresh seed), but concurrent
// identical requests must still share ONE RunFull fan-out — and must not
// be served from (or poison) the settings-hash cache.
func TestMonteCarloRerollCoalescesConcurrentRequests(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	const n = 6
	var calls int32
	entered := make(chan struct{}, n)
	release := make(chan struct{})
	swapRunFull(t, func(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
		atomic.AddInt32(&calls, 1)
		entered <- struct{}{}
		<-release
		return retirement.RunFull(eng, in)
	})

	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
			handleWhatIfMonteCarlo(w, req)
			statuses[i] = w.Code
		}(i)
	}

	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("no request reached RunFull")
	}
	time.Sleep(200 * time.Millisecond) // let the rest queue as waiters
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("RunFull executed %d times for %d concurrent re-roll requests, want 1", got, n)
	}
	for i, code := range statuses {
		if code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, code)
		}
	}

	// A re-roll AFTER the flight completes must recompute (no caching):
	// that is the endpoint's contract.
	w := httptest.NewRecorder()
	handleWhatIfMonteCarlo(w, httptest.NewRequest("POST", "/whatif/montecarlo", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("post-completion re-roll: status = %d, want 200", w.Code)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("RunFull executed %d times after sequential re-roll, want 2 (re-rolls must never be cached)", got)
	}
}
