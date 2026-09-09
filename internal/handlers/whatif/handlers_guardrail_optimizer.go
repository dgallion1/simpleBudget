package whatif

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
)

type guardrailPreview struct {
	validationSeed int64
	validationRuns int
	manager        *retirement.SettingsManager
	scenario       string
	revision       int
	fingerprint    [32]byte
	request        models.GuardrailOptimizerRequest
	cancel         context.CancelFunc
	expires        time.Time
	tokens         map[string]*models.GuardrailConfig
	graphs         map[string]models.GuardrailOptimizerCandidate
}

var guardrailPreviews = struct {
	sync.Mutex
	entries map[string]*guardrailPreview
}{entries: make(map[string]*guardrailPreview)}

type guardrailOptimizerRow struct {
	Candidate  models.GuardrailOptimizerCandidate
	Token      string
	GraphToken string
}

func guardrailOptimizerError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<p id="guardrail-optimizer-error" role="alert" tabindex="-1" class="text-negative">%s</p>`, html.EscapeString(message))
}

func handleGuardrailOptimizer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		guardrailOptimizerError(w, "Invalid form data.", 400)
		return
	}
	manager := retirementMgr
	scenario := manager.ActiveFilename()
	settings, revision, err := manager.LoadContextWithRevision(r.Context())
	if err != nil {
		guardrailOptimizerError(w, "Could not load the plan.", 500)
		return
	}
	floor, e1 := strconv.ParseFloat(r.PostForm.Get("floor_monthly_real"), 64)
	target, e2 := strconv.ParseFloat(r.PostForm.Get("target_success_pct"), 64)
	if e1 != nil || math.IsNaN(floor) || math.IsInf(floor, 0) || floor <= 0 || floor > settings.MonthlyLivingExpenses {
		guardrailOptimizerError(w, "Minimum monthly living spending must be finite, positive, and no greater than starting monthly living expenses.", 400)
		return
	}
	if e2 != nil || math.IsNaN(target) || math.IsInf(target, 0) || target <= 0 || target >= 100 {
		guardrailOptimizerError(w, "Select a chance of keeping your minimum spending greater than 0% and less than 100%.", 400)
		return
	}
	if len(settings.ScenarioChain) > 0 {
		guardrailOptimizerError(w, "Chained scenarios are not supported by the guardrail optimizer.", 400)
		return
	}
	id := r.PostForm.Get("request_id")
	if len(id) < 8 || len(id) > 128 {
		guardrailOptimizerError(w, "Invalid search request. Run again.", 400)
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		guardrailOptimizerError(w, "Could not prepare the plan.", 500)
		return
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		guardrailOptimizerError(w, "Could not prepare the plan.", 500)
		return
	}
	req := models.GuardrailOptimizerRequest{FloorMonthlyReal: floor, TargetSuccessPct: target}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	entry := &guardrailPreview{manager: manager, scenario: scenario, revision: revision, fingerprint: sha256.Sum256(raw), request: req, cancel: cancel, expires: time.Now().Add(15 * time.Minute), tokens: make(map[string]*models.GuardrailConfig), graphs: make(map[string]models.GuardrailOptimizerCandidate)}
	guardrailPreviews.Lock()
	for key, p := range guardrailPreviews.entries {
		if time.Now().After(p.expires) {
			p.cancel()
			delete(guardrailPreviews.entries, key)
		}
	}
	if old := guardrailPreviews.entries[id]; old != nil || len(guardrailPreviews.entries) >= 128 {
		guardrailPreviews.Unlock()
		guardrailOptimizerError(w, "Search unavailable or already used. Run again.", 409)
		return
	}
	guardrailPreviews.entries[id] = entry
	guardrailPreviews.Unlock()
	result, err := analysis.OptimizeGuardrails(ctx, in, req)
	guardrailPreviews.Lock()
	defer guardrailPreviews.Unlock()
	if err != nil || ctx.Err() != nil || guardrailPreviews.entries[id] != entry {
		delete(guardrailPreviews.entries, id)
		guardrailOptimizerError(w, "Search cancelled or could not complete. Nothing was saved.", 409)
		return
	}
	entry.validationSeed = result.ValidationSeed
	entry.validationRuns = result.ValidationRuns
	recommended := make(map[string]bool)
	for _, key := range result.Recommendations {
		recommended[key] = true
	}
	rows := make([]guardrailOptimizerRow, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		var graphBytes [32]byte
		if _, err := rand.Read(graphBytes[:]); err != nil {
			delete(guardrailPreviews.entries, id)
			guardrailOptimizerError(w, "Could not retain graph preview. Nothing was saved.", 500)
			return
		}
		row := guardrailOptimizerRow{Candidate: c, GraphToken: hex.EncodeToString(graphBytes[:])}
		retained := c
		if c.Guardrails != nil {
			cfg := *c.Guardrails
			retained.Guardrails = &cfg
		}
		entry.graphs[row.GraphToken] = retained
		if recommended[c.ID] && c.Qualifies && !c.Baseline && c.Guardrails != nil {
			var bytes [32]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				delete(guardrailPreviews.entries, id)
				guardrailOptimizerError(w, "Could not retain preview. Nothing was saved.", 500)
				return
			}
			row.Token = hex.EncodeToString(bytes[:])
			cfg := *c.Guardrails
			entry.tokens[row.Token] = &cfg
		}
		rows = append(rows, row)
	}
	data := map[string]any{"Optimizer": result, "Rows": rows, "RequestID": id, "Target": strconv.FormatFloat(target, 'f', -1, 64)}
	w.Header().Set("Cache-Control", "no-store")
	if renderer != nil {
		if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer-results", data); err != nil {
			delete(guardrailPreviews.entries, id)
			guardrailOptimizerError(w, "Could not display results.", 500)
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}

func handleCancelGuardrailOptimizer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		guardrailOptimizerError(w, "Invalid request.", 400)
		return
	}
	guardrailPreviews.Lock()
	defer guardrailPreviews.Unlock()
	id := r.PostForm.Get("request_id")
	if p := guardrailPreviews.entries[id]; p != nil && p.manager == retirementMgr {
		p.cancel()
		delete(guardrailPreviews.entries, id)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func handleApplyGuardrailOptimizer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		guardrailOptimizerError(w, "Invalid request.", 400)
		return
	}
	guardrailPreviews.Lock()
	defer guardrailPreviews.Unlock()
	id := r.PostForm.Get("request_id")
	p := guardrailPreviews.entries[id]
	if p == nil || p.manager != retirementMgr || !time.Now().Before(p.expires) {
		guardrailOptimizerError(w, "Preview expired or cancelled. Run again. Nothing was saved.", 409)
		return
	}
	cfg := p.tokens[r.PostForm.Get("recommendation")]
	if cfg == nil {
		guardrailOptimizerError(w, "This recommendation is unavailable. Nothing was saved.", 409)
		return
	}
	s, rev, err := p.manager.LoadContextWithRevision(r.Context())
	if err != nil {
		guardrailOptimizerError(w, "Could not load the plan. Nothing was saved.", 500)
		return
	}
	raw, err := json.Marshal(s)
	if err != nil || rev != p.revision || p.scenario != p.manager.ActiveFilename() || sha256.Sum256(raw) != p.fingerprint {
		guardrailOptimizerError(w, "The plan changed. Run again. Nothing was saved.", 409)
		return
	}
	copyConfig := *cfg
	s.Guardrails = &copyConfig
	rev, err = p.manager.SaveWithRevisionIfScenario(s, p.scenario, p.revision)
	if err != nil {
		guardrailOptimizerError(w, "Could not save recommendation: "+err.Error(), statusForMutationError(err))
		return
	}
	delete(guardrailPreviews.entries, id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("HX-Redirect", fmt.Sprintf("/whatif?guardrail_applied=%d#guardrail-optimizer", rev))
}
