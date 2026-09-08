package whatif

import (
	"budget2/internal/services/retirement"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Graph authorization is deliberately separate from Apply authorization: every
// returned candidate can be inspected, but only recommendations can be saved.
func handleGuardrailOptimizerGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(message string, code int) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	if err := r.ParseForm(); err != nil {
		fail("Invalid graph request.", 400)
		return
	}
	guardrailPreviews.Lock()
	defer guardrailPreviews.Unlock()
	p := guardrailPreviews.entries[r.PostForm.Get("request_id")]
	if p == nil || p.manager != retirementMgr || !time.Now().Before(p.expires) {
		fail("Graph preview expired or cancelled. Run optimizer again.", 409)
		return
	}
	candidate, ok := p.graphs[r.PostForm.Get("candidate")]
	if !ok {
		fail("This graph preview is unavailable. Run optimizer again.", 409)
		return
	}
	settings, revision, err := p.manager.LoadContextWithRevision(r.Context())
	if err != nil {
		fail("Could not load the plan.", 500)
		return
	}
	raw, err := json.Marshal(settings)
	if err != nil || revision != p.revision || p.scenario != p.manager.ActiveFilename() || sha256.Sum256(raw) != p.fingerprint {
		fail("The plan changed. Run optimizer again.", 409)
		return
	}
	clone := *settings
	clone.Guardrails = nil
	if candidate.Guardrails != nil {
		cfg := *candidate.Guardrails
		clone.Guardrails = &cfg
	}
	in, _, err := buildEngineInput(&clone)
	if err != nil {
		fail("Could not prepare graph preview.", 500)
		return
	}
	in.Hooks = retirement.DefaultHooks()
	projection := getEngine().Run(in)
	if r.Context().Err() != nil {
		return
	}
	mode := normalizeDisplayDollars(r.PostForm.Get("display_dollars"))
	_ = json.NewEncoder(w).Encode(map[string]any{"candidate": candidate, "success_text": fmt.Sprintf("%.2f", candidate.Metrics.FloorSuccessPct), "target": p.request.TargetSuccessPct, "display_dollars": mode, "chart": buildProjectionChartData(&clone, projection, mode)})
}
