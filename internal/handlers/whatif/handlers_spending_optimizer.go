package whatif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	budgettemplates "budget2/internal/templates"
)

// Immutable identity and evidence are retained until expiry or explicit consumption.
// active, applying, result and token maps are protected by spendingPreviews.
// operation serializes cancellation with an Apply already reserved for this preview.
type spendingPreview struct {
	operation        sync.Mutex
	manager          *retirement.SettingsManager
	scenario         string
	revision         int
	fingerprint      [32]byte
	expires          time.Time
	cancel           context.CancelFunc
	active, applying bool
	request          models.SpendingOptimizerRequest
	result           *models.SpendingOptimizerResult
	tokens           map[string]models.SpendingCandidate
	graphs           map[string]models.SpendingCandidate
}

var spendingPreviews = struct {
	sync.Mutex
	entries map[string]*spendingPreview
}{entries: make(map[string]*spendingPreview)}

type spendingOptimizerRow struct {
	NearTermMonthlyReal                                                     float64
	ComparisonAvailable                                                     bool
	MetricsAvailable                                                        bool
	Candidate                                                               models.SpendingCandidate
	Token, GraphToken                                                       string
	Headline                                                                bool
	NearTermDifferenceReal                                                  float64
	NearTermComparison                                                      string
	CutCount, EarlyCutCount, FloorFailureCount, UnpaidCount, DepletionCount string
	StillBelowPlanCount, RecoveredCount                                     string
	CutEvidence, CutTimingEvidence, CutDepthEvidence                        string
	MinimumEvidence, UnpaidEvidence, DepletionEvidence                      string
	PlannedChangesEvidence, StartingBudgetEvidence, LifetimeEvidence        string
}
type spendingOptimizerView struct {
	CurrentMetricsAvailable                                                        bool
	Optimizer                                                                      *models.SpendingOptimizerResult
	Request                                                                        models.SpendingOptimizerRequest
	RequestID                                                                      string
	Rows, PlannedRows, FlexibleRows, Headlines                                     []spendingOptimizerRow
	Current                                                                        *spendingOptimizerRow
	CurrentBaseMonthlyReal, CurrentStartingMonthlyReal, CurrentNearTermMonthlyReal float64
}
type spendingOptimizerRunner func(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error)

func spendingOptimizerError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<p id="spending-optimizer-error" role="alert" tabindex="-1" class="text-negative">%s</p>`, html.EscapeString(message))
}
func spendingErrorStatus(err error) int {
	if errors.Is(err, analysis.ErrInvalidSpendingRequest) {
		return 400
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 409
	}
	return 500
}
func parseSpendingRequest(r *http.Request) (models.SpendingOptimizerRequest, error) {
	var req models.SpendingOptimizerRequest
	if err := r.ParseForm(); err != nil {
		return req, fmt.Errorf("invalid form data")
	}
	parse := func(key string, required bool) (float64, error) {
		raw := strings.TrimSpace(r.PostForm.Get(key))
		if raw == "" && !required {
			return 0, nil
		}
		n, e := strconv.ParseFloat(raw, 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
			return 0, fmt.Errorf("%s must be finite and positive", key)
		}
		return n, nil
	}
	var err error
	for _, field := range []struct {
		key      string
		dst      *float64
		required bool
	}{{"floor_monthly_real", &req.FloorMonthlyReal, true}, {"search_min_monthly_real", &req.SearchMinMonthlyReal, false}, {"search_max_monthly_real", &req.SearchMaxMonthlyReal, false}, {"search_step_monthly_real", &req.SearchStepMonthlyReal, false}} {
		*field.dst, err = parse(field.key, field.required)
		if err != nil {
			return req, err
		}
	}
	if raw := strings.TrimSpace(r.PostForm.Get("near_term_years")); raw != "" {
		req.NearTermYears, err = strconv.Atoi(raw)
		if err != nil || req.NearTermYears <= 0 {
			return req, fmt.Errorf("near-term years must be a positive integer")
		}
	}
	if raw := strings.TrimSpace(r.PostForm.Get("seed")); raw != "" {
		req.Seed, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return req, fmt.Errorf("seed must be a signed 64-bit decimal integer")
		}
	}
	if enabled := r.PostForm.Get("boost_enabled"); enabled != "" && enabled != "false" && enabled != "0" {
		amount, e := parse("boost_monthly_real", true)
		if e != nil {
			return req, e
		}
		req.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: amount, StopMonth: strings.TrimSpace(r.PostForm.Get("boost_stop_month"))}
	}
	return req, nil
}

// Prepare is a cheap, read-only form validation step. It does not select a seed.
func handlePrepareSpendingOptimizer(w http.ResponseWriter, r *http.Request) {
	req, err := parseSpendingRequest(r)
	if err != nil {
		spendingOptimizerError(w, err.Error(), 400)
		return
	}
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		spendingOptimizerError(w, "Could not load the plan.", 500)
		return
	}
	if len(settings.ScenarioChain) > 0 {
		spendingOptimizerError(w, "Chained scenarios are not supported.", 400)
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		spendingOptimizerError(w, "Could not prepare the plan.", 500)
		return
	}
	req, err = analysis.NormalizeSpendingRequest(in, req)
	if err != nil {
		spendingOptimizerError(w, err.Error(), spendingErrorStatus(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	currentStarting := engine.LivingExpensesAtMonth(in.Prepared.Settings(), 0)
	minimumNote := ""
	if req.FloorMonthlyReal > currentStarting {
		minimumNote = "Your minimum is above the current starting budget. The search will evaluate higher budgets."
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"request": req, "current_base_monthly_real": settings.MonthlyLivingExpenses,
		"current_starting_monthly_real": currentStarting, "minimum_note": minimumNote,
	})
}
func handleSpendingOptimizer(w http.ResponseWriter, r *http.Request) {
	handleSpendingOptimizerWithRunner(w, r, analysis.OptimizeSpending)
}
func handleSpendingOptimizerWithRunner(w http.ResponseWriter, r *http.Request, run spendingOptimizerRunner) {
	req, err := parseSpendingRequest(r)
	if err != nil {
		spendingOptimizerError(w, err.Error(), 400)
		return
	}
	id := r.PostForm.Get("request_id")
	if len(id) < 8 || len(id) > 128 {
		spendingOptimizerError(w, "Invalid search request. Run again.", 400)
		return
	}
	manager := retirementMgr
	scenario := manager.ActiveFilename()
	settings, revision, err := manager.LoadContextWithRevision(r.Context())
	if err != nil {
		spendingOptimizerError(w, "Could not load the plan.", 500)
		return
	}
	if len(settings.ScenarioChain) > 0 {
		spendingOptimizerError(w, "Chained scenarios are not supported.", 400)
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		spendingOptimizerError(w, "Could not prepare the plan.", 500)
		return
	}
	in.Hooks = retirement.DefaultHooks()
	raw, err := json.Marshal(settings)
	if err != nil {
		spendingOptimizerError(w, "Could not prepare the plan.", 500)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	p := &spendingPreview{manager: manager, scenario: scenario, revision: revision, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(15 * time.Minute), cancel: cancel, active: true, request: req, tokens: make(map[string]models.SpendingCandidate), graphs: make(map[string]models.SpendingCandidate)}
	spendingPreviews.Lock()
	busy := false
	for key, old := range spendingPreviews.entries {
		if !time.Now().Before(old.expires) && !old.applying {
			old.cancel()
			delete(spendingPreviews.entries, key)
			continue
		}
		if old.manager == manager && old.scenario == scenario && old.active {
			busy = true
		}
	}
	if busy || spendingPreviews.entries[id] != nil || len(spendingPreviews.entries) >= 128 {
		spendingPreviews.Unlock()
		spendingOptimizerError(w, "Search unavailable or already running. Run again.", 409)
		return
	}
	spendingPreviews.entries[id] = p
	spendingPreviews.Unlock()
	result, err := run(ctx, in, req)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Search incomplete. Nothing was saved: "+err.Error(), spendingErrorStatus(err))
		return
	}
	if result == nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Search returned no results. Nothing was saved.", 500)
		return
	}
	// Copy the complete result so neither a runner nor response data can mutate authorization.
	raw, err = json.Marshal(result)
	var retained models.SpendingOptimizerResult
	if err == nil {
		err = json.Unmarshal(raw, &retained)
	}
	if err != nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Could not retain results.", 500)
		return
	}
	view, graphs, tokens, err := buildSpendingOptimizerView(id, &retained)
	if err != nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Could not retain results.", 500)
		return
	}
	// Render into a buffer outside the registry lock; a canceled search publishes nothing.
	var body bytes.Buffer
	contentType := "application/json"
	if renderer != nil {
		contentType = "text/html; charset=utf-8"
		recorder := &spendingResponseBuffer{headers: make(http.Header)}
		err = renderer.RenderPartial(recorder, "whatif-spending-optimizer-results", view)
		body.Write(recorder.Bytes())
	} else {
		err = json.NewEncoder(&body).Encode(view)
	}
	if err != nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Could not display results.", 500)
		return
	}
	if _, err = loadSpendingSnapshot(ctx, p); err != nil {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "The plan changed or search was cancelled. Run again.", spendingSnapshotStatus(err))
		return
	}
	spendingPreviews.Lock()
	publish := spendingPreviews.entries[id] == p && ctx.Err() == nil && time.Now().Before(p.expires)
	if publish {
		p.active = false
		p.result = &retained
		p.request = retained.Request
		p.graphs = graphs
		p.tokens = tokens
	} else if spendingPreviews.entries[id] == p {
		delete(spendingPreviews.entries, id)
	}
	spendingPreviews.Unlock()
	// The state transition above authorizes this buffered response. Delivery can
	// block; subsequent cancellation must still revoke its tokens immediately.
	if !publish {
		spendingOptimizerError(w, "Search cancelled. Nothing was saved.", 409)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body.Bytes())
}
func discardSpendingPreview(id string, p *spendingPreview) {
	spendingPreviews.Lock()
	defer spendingPreviews.Unlock()
	if spendingPreviews.entries[id] == p {
		p.cancel()
		delete(spendingPreviews.entries, id)
	}
}
func spendingOpaqueToken() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func cloneSpendingCandidate(c models.SpendingCandidate) models.SpendingCandidate {
	c.LivingSpendingBoost = models.CloneLivingSpendingBoost(c.LivingSpendingBoost)
	if c.Guardrails != nil {
		cfg := *c.Guardrails
		c.Guardrails = &cfg
	}
	// Metrics contain optional pointers; JSON cloning keeps all nested evidence independent.
	raw, _ := json.Marshal(c)
	var out models.SpendingCandidate
	_ = json.Unmarshal(raw, &out)
	return out
}
func spendingCandidateCanApply(c models.SpendingCandidate) bool {
	return c.Qualifies && !c.Baseline && c.Kind != "current" && c.Guardrails != nil && c.Guardrails.Enabled
}
func buildSpendingOptimizerView(id string, result *models.SpendingOptimizerResult) (spendingOptimizerView, map[string]models.SpendingCandidate, map[string]models.SpendingCandidate, error) {
	view := spendingOptimizerView{Optimizer: result, Request: result.Request, RequestID: id}
	graphs := make(map[string]models.SpendingCandidate)
	tokens := make(map[string]models.SpendingCandidate)
	heads := make(map[string]bool)
	for _, id := range result.RecommendationIDs {
		heads[id] = true
	}
	for _, c := range result.Candidates {
		if c.Baseline {
			view.CurrentBaseMonthlyReal = c.BaseMonthlyLivingExpenses
			view.CurrentStartingMonthlyReal = c.StartingMonthlyLivingReal
			if c.Metrics != nil && c.Metrics.Runs > 0 {
				view.CurrentMetricsAvailable = true
				view.CurrentNearTermMonthlyReal = math.Round(c.Metrics.MedianNearTermMonthlyReal*100) / 100
			}
		}
	}
	headlineCount := 0
	for _, c := range result.Candidates {
		row := spendingOptimizerRow{Candidate: cloneSpendingCandidate(c), Headline: heads[c.ID]}
		if row.Headline {
			if headlineCount >= 2 {
				row.Headline = false
			} else {
				headlineCount++
			}
		}
		var err error
		row.GraphToken, err = spendingOpaqueToken()
		if err != nil {
			return view, nil, nil, err
		}
		graphs[row.GraphToken] = cloneSpendingCandidate(c)
		if spendingCandidateCanApply(c) {
			row.Token, err = spendingOpaqueToken()
			if err != nil {
				return view, nil, nil, err
			}
			tokens[row.Token] = cloneSpendingCandidate(c)
		}
		if m := c.Metrics; m != nil {
			row.MetricsAvailable = m.Runs > 0
			row.NearTermMonthlyReal = math.Round(m.MedianNearTermMonthlyReal*100) / 100
			row.ComparisonAvailable = view.CurrentMetricsAvailable
			row.NearTermDifferenceReal = (math.Round(m.MedianNearTermMonthlyReal*100) - math.Round(view.CurrentNearTermMonthlyReal*100)) / 100
			switch {
			case !row.ComparisonAvailable:
				row.NearTermDifferenceReal = 0
				row.NearTermComparison = "Current-plan comparison unavailable"
			case row.NearTermDifferenceReal > 0:
				row.NearTermComparison = fmt.Sprintf("%s more per month than the current plan", budgettemplates.FormatMoney(row.NearTermDifferenceReal))
			case row.NearTermDifferenceReal < 0:
				row.NearTermComparison = fmt.Sprintf("%s less per month than the current plan", budgettemplates.FormatMoney(-row.NearTermDifferenceReal))
			default:
				row.NearTermComparison = "Same near-term funded living as the current plan"
			}
			count := func(n, d int) string { return fmt.Sprintf("%d / %d", n, d) }
			row.CutCount = count(m.CutPaths, m.Runs)
			row.EarlyCutCount = count(m.EarlyCutPaths, m.Runs)
			row.FloorFailureCount = count(m.FloorShortfallPaths, m.Runs)
			row.UnpaidCount = count(m.UnpaidObligationPaths, m.Runs)
			row.DepletionCount = count(m.DepletionPaths, m.Runs)
			row.StillBelowPlanCount = count(m.CutPathsStillBelowPlan, m.CutPaths)
			row.RecoveredCount = count(m.CutPaths-m.CutPathsStillBelowPlan, m.CutPaths)
			populateSpendingRowEvidence(&row, m)
		}
		populateSpendingRowPlanEvidence(&row)
		view.Rows = append(view.Rows, row)
		if row.Headline {
			view.Headlines = append(view.Headlines, row)
		}
		switch {
		case c.Baseline:
			current := row
			view.Current = &current
		case c.Kind == "planned":
			view.PlannedRows = append(view.PlannedRows, row)
		case c.Kind == "flexible":
			view.FlexibleRows = append(view.FlexibleRows, row)
		}
	}
	return view, graphs, tokens, nil
}

func formatSpendingCount(value int) string {
	raw := strconv.Itoa(value)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}

func spendingObservedPercent(observed, total int) string {
	if total <= 0 {
		return "Unavailable"
	}
	return fmt.Sprintf("%.2f%%", float64(observed)*100/float64(total))
}

func populateSpendingRowEvidence(row *spendingOptimizerRow, metrics *models.SpendingRiskMetrics) {
	if row == nil || metrics == nil || metrics.Runs <= 0 {
		return
	}
	runs := formatSpendingCount(metrics.Runs)
	if metrics.CutPaths == 0 {
		row.CutEvidence = fmt.Sprintf("No cuts observed across %s checked futures.", runs)
		row.CutTimingEvidence = "Cut timing and depth are unavailable because no below-plan cuts were observed."
	} else {
		cuts := formatSpendingCount(metrics.CutPaths)
		row.CutEvidence = fmt.Sprintf("Cuts occurred in %s of %s futures (%s).", cuts, runs, spendingObservedPercent(metrics.CutPaths, metrics.Runs))
		if metrics.MedianFirstCutMonth != nil && *metrics.MedianFirstCutMonth > 0 {
			year := (int(math.Round(*metrics.MedianFirstCutMonth))-1)/12 + 1
			row.CutTimingEvidence = fmt.Sprintf("Among those %s futures, the median first cut was in plan year %d.", cuts, year)
		} else {
			row.CutTimingEvidence = fmt.Sprintf("First-cut timing is unavailable among the %s futures with a cut.", cuts)
		}
		parts := make([]string, 0, 3)
		if metrics.MedianMaxCutReal != nil {
			parts = append(parts, fmt.Sprintf("Dollar depth: median deepest monthly reduction %s.", budgettemplates.FormatMoney(*metrics.MedianMaxCutReal)))
		}
		if metrics.MedianMaxCutPct != nil {
			parts = append(parts, fmt.Sprintf("Percentage depth: median deepest reduction %.2f%% of planned living.", *metrics.MedianMaxCutPct))
		}
		if metrics.MedianMonthsBelowPlan != nil {
			parts = append(parts, fmt.Sprintf("Duration: median %.0f months below planned living.", *metrics.MedianMonthsBelowPlan))
		}
		if len(parts) == 0 {
			row.CutDepthEvidence = fmt.Sprintf("Cut depth and duration are unavailable among the %s futures with a cut.", cuts)
		} else {
			row.CutDepthEvidence = "Among futures with a cut: " + strings.Join(parts, " ") + " These are separate summaries across futures with a cut."
		}
	}
	row.MinimumEvidence = fmt.Sprintf("Below the minimum in %s of %s futures", formatSpendingCount(metrics.FloorShortfallPaths), runs)
	if metrics.FloorShortfallPaths > 0 {
		row.MinimumEvidence += fmt.Sprintf(". Largest observed monthly gap: %s. Longest observed below-minimum episode: %d consecutive months. These maxima may come from different futures.", budgettemplates.FormatMoney(metrics.LargestFloorGapReal), metrics.LongestFloorShortfallMonths)
	} else {
		row.MinimumEvidence += "."
	}
	row.UnpaidEvidence = fmt.Sprintf("Other configured obligations went unpaid in %s of %s futures.", formatSpendingCount(metrics.UnpaidObligationPaths), runs)
	row.DepletionEvidence = fmt.Sprintf("The model recorded depletion in %s of %s futures; in %s of those, your minimum stayed funded from that event through the end.", formatSpendingCount(metrics.DepletionPaths), runs, formatSpendingCount(metrics.DepletionFloorFundedPaths))
	row.LifetimeEvidence = fmt.Sprintf("Median lifetime funded living spending: %s in today's dollars.", budgettemplates.FormatMoney(metrics.FloorEvidence.MedianLifetimeFundedLivingReal))
}

func populateSpendingRowPlanEvidence(row *spendingOptimizerRow) {
	if row == nil {
		return
	}
	candidate := row.Candidate
	if math.Round(candidate.StartingMonthlyLivingReal*100) != math.Round(candidate.BaseMonthlyLivingExpenses*100) {
		row.StartingBudgetEvidence = fmt.Sprintf("The starting monthly living budget is %s; the base living-expense setting is %s because scheduled phases or an active early-spending boost apply.", budgettemplates.FormatMoney(candidate.StartingMonthlyLivingReal), budgettemplates.FormatMoney(candidate.BaseMonthlyLivingExpenses))
	}
	if candidate.LivingSpendingBoost != nil {
		row.PlannedChangesEvidence = fmt.Sprintf("The early-spending boost ends in %s; this planned change is separate from below-plan cuts. Existing spending phases remain in force and are also shown separately from below-plan cuts.", candidate.LivingSpendingBoost.StopMonth)
	} else {
		row.PlannedChangesEvidence = "Existing spending phases remain in force. Their scheduled changes are separate from below-plan cuts."
	}
}

var errSpendingPreviewStale = errors.New("spending preview changed or expired")

func loadSpendingSnapshot(ctx context.Context, p *spendingPreview) (*models.WhatIfSettings, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if p.manager != retirementMgr || !time.Now().Before(p.expires) || p.scenario != p.manager.ActiveFilename() {
		return nil, errSpendingPreviewStale
	}
	s, rev, err := p.manager.LoadContextWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if rev != p.revision || sha256.Sum256(raw) != p.fingerprint || p.scenario != p.manager.ActiveFilename() || len(s.ScenarioChain) > 0 {
		return nil, errSpendingPreviewStale
	}
	return s, nil
}
func spendingSnapshotStatus(err error) int {
	if errors.Is(err, errSpendingPreviewStale) {
		return 409
	}
	return spendingErrorStatus(err)
}
func handleCancelSpendingOptimizer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		spendingOptimizerError(w, "Invalid request.", 400)
		return
	}
	id := r.PostForm.Get("request_id")
	spendingPreviews.Lock()
	p := spendingPreviews.entries[id]
	spendingPreviews.Unlock()
	if p != nil {
		p.operation.Lock()
		spendingPreviews.Lock()
		if spendingPreviews.entries[id] == p && p.manager == retirementMgr {
			p.cancel()
			delete(spendingPreviews.entries, id)
		}
		spendingPreviews.Unlock()
		p.operation.Unlock()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
func handleApplySpendingOptimizer(w http.ResponseWriter, r *http.Request) {
	handleApplySpendingOptimizerWithHook(w, r, nil)
}

// beforeSave permits deterministic tests of the manager's final scenario/revision guard.
func handleApplySpendingOptimizerWithHook(w http.ResponseWriter, r *http.Request, beforeSave func()) {
	if err := r.ParseForm(); err != nil {
		spendingOptimizerError(w, "Invalid request.", 400)
		return
	}
	id := r.PostForm.Get("request_id")
	spendingPreviews.Lock()
	p := spendingPreviews.entries[id]
	spendingPreviews.Unlock()
	if p == nil {
		spendingOptimizerError(w, "Preview expired or cancelled. Run again.", 409)
		return
	}
	p.operation.Lock()
	defer p.operation.Unlock()
	spendingPreviews.Lock()
	c, ok := p.tokens[r.PostForm.Get("recommendation")]
	if spendingPreviews.entries[id] != p || p.active || p.applying || !ok || !spendingCandidateCanApply(c) {
		spendingPreviews.Unlock()
		spendingOptimizerError(w, "This recommendation is unavailable. Nothing was saved.", 409)
		return
	}
	p.applying = true
	spendingPreviews.Unlock()
	defer func() { spendingPreviews.Lock(); p.applying = false; spendingPreviews.Unlock() }()
	s, err := loadSpendingSnapshot(r.Context(), p)
	if err != nil {
		if spendingSnapshotStatus(err) == 409 {
			discardSpendingPreview(id, p)
		}
		spendingOptimizerError(w, "The plan changed or preview expired. Nothing was saved.", spendingSnapshotStatus(err))
		return
	}
	s.MonthlyLivingExpenses = c.BaseMonthlyLivingExpenses
	s.LivingSpendingBoost = models.CloneLivingSpendingBoost(c.LivingSpendingBoost)
	cfg := *c.Guardrails
	s.Guardrails = &cfg
	if beforeSave != nil {
		beforeSave()
	}
	if r.Context().Err() != nil || p.manager != retirementMgr || !time.Now().Before(p.expires) {
		discardSpendingPreview(id, p)
		spendingOptimizerError(w, "Apply cancelled or preview expired. Nothing was saved.", 409)
		return
	}
	revision, err := p.manager.SaveWithRevisionIfScenario(s, p.scenario, p.revision)
	if err != nil {
		status := statusForMutationError(err)
		if status == 409 {
			discardSpendingPreview(id, p)
		}
		if status != 409 {
			status = 500
		}
		spendingOptimizerError(w, "Could not save spending plan: "+err.Error(), status)
		return
	}
	discardSpendingPreview(id, p)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("HX-Redirect", fmt.Sprintf("/whatif?spending_applied=%d#spending-optimizer", revision))
}

// Advanced range fields stay blank/automatic; prepare returns their canonical defaults.
func spendingOptimizerFormData(s *models.WhatIfSettings, projection *models.ProjectionResult) map[string]any {
	// An absent saved floor is an unchosen required input, not the base budget.
	var floor any
	if s.Guardrails != nil && s.Guardrails.MinMonthlySpendingReal > 0 {
		floor = s.Guardrails.MinMonthlySpendingReal
	}
	actual := s.MonthlyLivingExpenses
	if projection != nil && len(projection.Months) > 0 {
		actual = projection.Months[0].PlannedLivingExpenses
	}
	data := map[string]any{"FloorMonthlyReal": floor, "NearTermYears": 5, "CurrentBaseMonthlyReal": s.MonthlyLivingExpenses, "CurrentStartingMonthlyReal": actual, "Chained": len(s.ScenarioChain) > 0}
	start, err := models.ParseYearMonth(s.StartDate)
	if err == nil {
		data["StartMonth"] = start.Format("2006-01")
		data["BoostStopMinimum"] = start.AddDate(0, 1, 0).Format("2006-01")
		if s.ProjectionYears > 0 {
			end := start.AddDate(0, s.ProjectionYears*12-1, 0)
			data["ExpectedEndMonth"] = end.Format("2006-01")
			if boost := s.LivingSpendingBoost; boost != nil {
				if stop, stopErr := models.ParseYearMonth(boost.StopMonth); stopErr == nil {
					data["BoostOutsideHorizon"] = stop.After(end)
				}
			}
		}
	}
	if boost := s.LivingSpendingBoost; boost != nil {
		data["BoostEnabled"] = true
		data["BoostMonthlyReal"] = boost.MonthlyReal
		data["BoostStopMonth"] = boost.StopMonth
		if stop, stopErr := models.ParseYearMonth(boost.StopMonth); stopErr == nil && err == nil {
			data["BoostExpired"] = !stop.After(start)
			data["BoostActive"] = stop.After(start)
		}
	}
	if floorValue, ok := floor.(float64); ok {
		data["MinimumAboveCurrent"] = floorValue > actual
	}
	return data
}
func spendingAppliedAnnouncement(r *http.Request, s *models.WhatIfSettings) string {
	if rev, err := strconv.Atoi(r.URL.Query().Get("spending_applied")); err != nil || rev <= 0 {
		return ""
	}
	message := fmt.Sprintf("Saved spending plan: base living expenses $%.2f per month and the complete portfolio-trigger spending rules.", s.MonthlyLivingExpenses)
	if b := s.LivingSpendingBoost; b != nil {
		message += fmt.Sprintf(" Extra living spending of $%.2f per month stops in %s.", b.MonthlyReal, b.StopMonth)
	}
	return message
}

// Buffer partial output until a final preview identity check authorizes publication.
type spendingResponseBuffer struct {
	bytes.Buffer
	headers http.Header
}

func (b *spendingResponseBuffer) Header() http.Header { return b.headers }
func (b *spendingResponseBuffer) WriteHeader(int)     {}
