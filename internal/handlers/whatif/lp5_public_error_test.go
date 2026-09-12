package whatif

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func checkerFailure(s *models.WhatIfSettings) *models.WhatIfAnalysis {
	return &models.WhatIfAnalysis{Settings: s, CalculationError: "checker settlement failed", Projection: &models.ProjectionResult{CalculationError: "checker settlement failed", FinalBalance: 987654, Survives: true}, MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 91.23}}}
}
func checkerBody(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	b := w.Body.String()
	if !strings.Contains(strings.ToLower(b), "calculation") || strings.Contains(b, "987,654") || strings.Contains(b, "91.23") {
		t.Fatalf("failure precedence body=%s", b)
	}
}

func TestCheckerHTTPFailureColdPendingWarmPollRecovery(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	origFast := runFastFn
	t.Cleanup(func() { runFastFn = origFast })
	runFastFn = func(_ *engine.Engine, _ engine.Input) *models.WhatIfAnalysis { return checkerFailure(s) }

	// Cold fast response: explicit failure, no stale success and no loader implying pending success.
	w := httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	checkerBody(t, w)
	if strings.Contains(w.Body.String(), `id="whatif-async-loader"`) {
		t.Fatal("failed cold result rendered pending success loader")
	}

	// Warm failed cache and subsequent render.
	_, hash, err := buildEngineInput(s)
	if err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	cache.hash = hash
	cache.analysis = checkerFailure(s)
	cache.cachedAt = time.Now()
	cache.mu.Unlock()
	w = httptest.NewRecorder()
	handleWhatIfCalculate(w, httptest.NewRequest("POST", "/whatif/calculate", nil))
	checkerBody(t, w)

	// Poll must not merge a previous success into the current failure.
	w = httptest.NewRecorder()
	handleWhatIfPoll(w, httptest.NewRequest("GET", "/whatif/poll?since=-1", nil))
	checkerBody(t, w)

	// Full endpoint failure is explicit and becomes the current cached value.
	swapRunFull(t, func(_ *engine.Engine, _ engine.Input) *models.WhatIfAnalysis { return checkerFailure(s) })
	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()
	w = httptest.NewRecorder()
	handleWhatIfResultsFull(w, httptest.NewRequest("GET", "/whatif/results-full?hash="+hash, nil))
	checkerBody(t, w)

	// A changed revision/hash can recover to a valid analysis.
	s.ProjectionYears++
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	runFastFn = retirement.RunFast
	w = httptest.NewRecorder()
	handleWhatIfPoll(w, httptest.NewRequest("GET", "/whatif/poll?since=-1", nil))
	if strings.Contains(w.Body.String(), "checker settlement failed") {
		t.Fatal("changed revision/hash retained failed cached result")
	}
}

func TestCheckerDirectNoGuardrailAndSweepCalculationError(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	prior := 106000.0
	s.StartDate = "2026-01"
	s.ProjectionYears = 1
	s.CurrentAge = 75
	s.MonthlyLivingExpenses = 0
	s.Persons = []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}}
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.RMDTiming = models.RMDTimingStartOfYear
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 200, CashPercent: 100}, {ID: "broker", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100}, {ID: "ira", OwnerID: "older", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: prior, PriorDecemberValue: nil, StockPercent: 100}}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "ira"}}}
	if _, err := prepare.From(s); err != nil {
		t.Fatal(err)
	}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleWhatIfProjectionChartNoGuardrails(w, httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails", nil))
	if !strings.Contains(w.Body.String(), "calculationError") || strings.Contains(w.Body.String(), "987654") {
		t.Fatalf("no-guardrails did not return explicit empty error chart: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	handleWhatIfConversionSweep(w, httptest.NewRequest("POST", "/whatif/conversion-sweep", nil))
	if w.Code != 422 || !strings.Contains(strings.ToLower(w.Body.String()), "unavailable") {
		t.Fatalf("sweep silently accepted lifetime settings: code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLifetimeCalculationErrorAcrossPublicResultEndpoints(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	origFast := runFastFn
	t.Cleanup(func() { runFastFn = origFast })
	runFastFn = func(_ *engine.Engine, _ engine.Input) *models.WhatIfAnalysis { return checkerFailure(s) }
	for _, tc := range []struct {
		name, path string
		handler    func(http.ResponseWriter, *http.Request)
	}{
		{"projection chart", "/whatif/chart/projection", handleWhatIfProjectionChart},
		{"income chart", "/whatif/chart/income", handleWhatIfIncomeChart},
		{"trajectory", "/whatif/spending-trajectory", handleWhatIfSpendingTrajectory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, httptest.NewRequest("GET", tc.path, nil))
			body := strings.ToLower(w.Body.String())
			if !strings.Contains(body, "calculation") || strings.Contains(body, "987654") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}

	ss := httptest.NewRequest("POST", "/whatif/social-security", strings.NewReader("fra_benefit=1000&fra=67&claim_age=67"))
	ss.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ssW := httptest.NewRecorder()
	handleWhatIfSocialSecurity(ssW, ss)
	checkerBody(t, ssW)

	swapRunFull(t, func(_ *engine.Engine, _ engine.Input) *models.WhatIfAnalysis { return checkerFailure(s) })
	w := httptest.NewRecorder()
	handleWhatIfMonteCarlo(w, httptest.NewRequest("POST", "/whatif/montecarlo", nil))
	checkerBody(t, w)
}
