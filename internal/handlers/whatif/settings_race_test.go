package whatif

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestWhatIfPhaseHandlersRaceAgainstPollingReader drives the two spending-phase
// handlers that publish a NEW slice header / struct pointer into the settings
// object (handleWhatIfAddPhase appends to
// settings.SpendingPhaseConfig.Phases; handleWhatIfResetPhases replaces
// settings.SpendingPhaseConfig outright) against a reader that does exactly
// what the 2s /whatif/poll path does on a 200: Load() and then json.Marshal
// the returned object (getSettingsHash / prepare.From both marshal it).
//
// Before the load-modify-save handlers were changed to mutate a private copy,
// Load() handed both goroutines the same *models.WhatIfSettings, the handlers
// mutated it in place outside any lock, and this test reported a data race
// under -race — a torn slice header observed mid-marshal can index past the
// backing array, not merely render a stale figure.
//
// Run with -race; without it the test only exercises the handlers.
func TestWhatIfPhaseHandlersRaceAgainstPollingReader(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	const writerIterations = 40

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reader: the polling path. Load() then marshal, continuously.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				settings, err := rm.Load()
				if err != nil {
					continue
				}
				if _, err := json.Marshal(settings); err != nil {
					return
				}
			}
		}()
	}

	// Writer: the load-modify-save handlers. Alternate add and reset so the
	// phase list does not grow without bound across iterations.
	for i := 0; i < writerIterations; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/phases/add", nil)
		handleWhatIfAddPhase(w, req)
		if w.Code != 200 {
			close(stop)
			wg.Wait()
			t.Fatalf("add phase iteration %d: status %d, body %s", i, w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/whatif/phases/reset", nil)
		handleWhatIfResetPhases(w, req)
		if w.Code != 200 {
			close(stop)
			wg.Wait()
			t.Fatalf("reset phases iteration %d: status %d, body %s", i, w.Code, w.Body.String())
		}
	}

	close(stop)
	wg.Wait()
}

// TestWhatIfPollHandlerRaceAgainstMutatingHandlers is the previous test's
// reader replaced by the actual /whatif/poll handler, so the render path
// (runAnalysisWithCache -> getSettingsHash -> json.Marshal, prepare.From,
// completeness.Check, BuildVerdict and the template data assembly) is
// exercised against a concurrent writer rather than a stand-in marshal.
//
// It guards the other half of the writers-copy invariant: writers no longer
// mutate the published object, so this fails if a READER starts mutating it.
// Iteration counts are lower because each poll that sees a new revision runs
// a full projection.
func TestWhatIfPollHandlerRaceAgainstMutatingHandlers(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	const writerIterations = 6

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			w := httptest.NewRecorder()
			// since=-2 can never equal a real revision, so every poll takes
			// the 200 branch that loads and renders.
			req := httptest.NewRequest("GET", "/whatif/poll?since=-2", nil)
			handleWhatIfPoll(w, req)
		}
	}()

	for i := 0; i < writerIterations; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/phases/add", nil)
		handleWhatIfAddPhase(w, req)
		if w.Code != 200 {
			close(stop)
			wg.Wait()
			t.Fatalf("add phase iteration %d: status %d, body %s", i, w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/whatif/social-security", strings.NewReader(
			"enabled=on&fra_benefit=3000&fra=67&claim_age=67&cola_rate=2.5"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSocialSecurity(w, req)
		if w.Code != 200 {
			close(stop)
			wg.Wait()
			t.Fatalf("social security iteration %d: status %d, body %s", i, w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/whatif/phases/reset", nil)
		handleWhatIfResetPhases(w, req)
		if w.Code != 200 {
			close(stop)
			wg.Wait()
			t.Fatalf("reset phases iteration %d: status %d, body %s", i, w.Code, w.Body.String())
		}
	}

	close(stop)
	wg.Wait()

	// The writers must still have persisted what they intended: a copy that
	// is saved but never re-read would make the whole suite above vacuous.
	final, err := rm.Load()
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if final.SocialSecurity == nil || final.SocialSecurity.FRABenefit != 3000 {
		t.Fatalf("social security did not persist through the copy: %+v", final.SocialSecurity)
	}
}
