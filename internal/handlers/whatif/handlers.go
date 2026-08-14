// Package whatif serves the What-If retirement-projection page and all of
// its HTMX field handlers (income, expenses, rates, healthcare, taxes,
// guardrails, etc.). It composes services/retirement, the engine, and the
// prepare/completeness sub-packages to turn user form input into the
// projection chart, sustainability score, and scenario chain views.
package whatif

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/insights"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/completeness"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"budget2/internal/templates"
)

// sharedEngine is the package-level Engine used by what-if handlers.
// Engine.Run is stateless, so a single shared instance is safe.
var sharedEngine = engine.New()

// getEngine returns the shared what-if engine instance.
func getEngine() *engine.Engine { return sharedEngine }

// analysisCache caches expensive analysis results keyed by settings hash
type analysisCache struct {
	mu       sync.RWMutex
	hash     string
	analysis *models.WhatIfAnalysis
	cachedAt time.Time
}

var cache = &analysisCache{}

// runFullFn indirects retirement.RunFull so tests can count or gate the
// expensive analysis fan-out. Production never reassigns it.
var runFullFn = retirement.RunFull

// errAnalysisPanicked is returned to every request coalesced onto an
// analysis whose RunFull panicked. The panic value and stack are logged in
// runFullRecovered; all callers fail cleanly with a 500 and the client can
// simply retry, which recomputes because nothing was cached.
var errAnalysisPanicked = errors.New("analysis computation failed; please retry")

// analysisGroup coalesces concurrent identical analysis computations
// (x/sync singleflight): one flight executes the RunFull fan-out per key
// while the rest wait for its result. Keys are the settings dep-hash for
// cached analyses and "fresh:"+hash for the deliberately uncached Monte
// Carlo re-roll endpoint.
var analysisGroup singleflight.Group

// cachedAnalysis returns the cached analysis for depHash if it is still
// fresh. The returned pointer is shared across requests; handlers treat
// WhatIfAnalysis as read-only (this matches the pre-existing cache
// behavior, which also handed the same pointer to every request).
func cachedAnalysis(depHash string) (*models.WhatIfAnalysis, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if cache.hash == depHash && time.Since(cache.cachedAt) < 5*time.Minute {
		return cache.analysis, true
	}
	return nil, false
}

// runFullRecovered runs the RunFull fan-out, converting a panic into
// errAnalysisPanicked. singleflight must never see the flight fn panic:
// DoChan re-raises a flight panic on a bare goroutine, which would crash
// the process. The panic value and stack — including the worker frames
// analysis.ParallelIndexed logs at capture — are logged here, so a failed
// analysis stays diagnosable without chi's Recoverer in the loop.
func runFullRecovered(in engine.Input) (analysis *models.WhatIfAnalysis, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("whatif: analysis panicked: %v\n%s", r, debug.Stack())
			analysis, err = nil, errAnalysisPanicked
		}
	}()
	return runFullFn(getEngine(), in), nil
}

// awaitAnalysis waits for a singleflight result channel, honoring request
// cancellation: a waiter whose client disconnected (or timed out) returns
// ctx.Err() immediately instead of staying parked until the flight
// finishes. The flight itself keeps running to completion so its result
// still lands in the cache for the next request.
func awaitAnalysis(ctx context.Context, ch <-chan singleflight.Result) (*models.WhatIfAnalysis, error) {
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*models.WhatIfAnalysis), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// statusForMutationError maps a settings-mutation error to its HTTP status.
// It understands every typed error the SettingsManager returns — chain and
// scenario validation → 400, unknown scenario/item → 404, restore conflict →
// 409 — and falls back to 500. One policy for every mutating handler, so the
// same failure class cannot produce different statuses on different routes.
func statusForMutationError(err error) int {
	var chainErr *retirement.ScenarioChainValidationError
	if errors.As(err, &chainErr) {
		return http.StatusBadRequest
	}
	return statusForScenarioOperationError(err)
}

// revisionUnreported is the revision a caller passes to renderRecalc when it
// cannot say which revision its own write produced — either because it wrote
// nothing (a pure recalc) or because the mutation method it called does not
// report one.
//
// It makes renderRecalc omit the HX-Trigger, so the client keeps its older
// baseline and picks the change up on its next poll: one redundant render.
// The alternative — reading Revision() after the analysis fan-out — can hand
// the client a baseline that LEADS the state it was just sent, because a
// concurrent MCP write can bump the counter in between. That baseline makes
// every later poll answer 204 and freezes the page on pre-change figures with
// no recovery short of a reload, which is far worse than one extra render.
const revisionUnreported = 0

// renderRecalc re-runs the (cached) analysis for settings and renders the
// standard results partial — the render tail every mutating what-if handler
// shares.
//
// revision must be the revision the caller's own write produced (see
// SettingsManager.SaveWithRevision), or revisionUnreported.
func renderRecalc(w http.ResponseWriter, r *http.Request, settings *models.WhatIfSettings, revision int) {
	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if revision != revisionUnreported {
		if trigger, err := json.Marshal(map[string]int{"whatif:revision": revision}); err == nil {
			w.Header().Set("HX-Trigger", string(trigger))
		}
	}
	renderWhatIfResults(w, settings, analysis)
}

// recalcAndRender is the shared tail of a mutating what-if handler: apply
// the mutation, then recalc + render. failMsg prefixes the mutation error;
// its status comes from statusForMutationError.
//
// mutate returns the revision its own write produced, or revisionUnreported if
// the SettingsManager method it calls does not report one.
func recalcAndRender(w http.ResponseWriter, r *http.Request, failMsg string, mutate func() (*models.WhatIfSettings, int, error)) {
	settings, revision, err := mutate()
	if err != nil {
		renderError(w, failMsg+": "+err.Error(), statusForMutationError(err))
		return
	}
	renderRecalc(w, r, settings, revision)
}

// saveAndRecalc is recalcAndRender for the load-modify-save handlers, which
// mutate a loaded settings object in place and persist it via Save.
//
// Mutating it in place is safe because Load hands every caller a private copy;
// the manager's cached object, which the /whatif/poll path marshals without
// holding a lock, is never the one a handler holds. Anything that reintroduces
// a shared-pointer accessor reintroduces the race these handlers used to
// carry: a slice-header mutation (spending phases) or a fresh pointer could be
// read torn, not merely stale.
func saveAndRecalc(w http.ResponseWriter, r *http.Request, settings *models.WhatIfSettings) {
	recalcAndRender(w, r, "Failed to save settings", func() (*models.WhatIfSettings, int, error) {
		revision, err := retirementMgr.SaveWithRevision(settings)
		if err != nil {
			return nil, 0, err
		}
		return settings, revision, nil
	})
}

func statusForScenarioOperationError(err error) int {
	var validationErr *retirement.ScenarioValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest
	}

	var notFoundErr *retirement.ScenarioNotFoundError
	if errors.As(err, &notFoundErr) {
		return http.StatusNotFound
	}

	var conflictErr *retirement.ScenarioConflictError
	if errors.As(err, &conflictErr) {
		return http.StatusConflict
	}

	return http.StatusInternalServerError
}

// getSettingsHash generates a hash of the settings for cache key
func getSettingsHash(settings *models.WhatIfSettings) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for shorter key
}

// buildEngineInput resolves prepared settings (and chain, if any) for
// the given top-level WhatIfSettings, and returns the engine.Input that
// the orchestrator and engine consume, along with a cache hash.
func buildEngineInput(settings *models.WhatIfSettings) (engine.Input, string, error) {
	hashData := getSettingsHash(settings)

	prepared, err := prepare.From(settings)
	if err != nil {
		return engine.Input{}, "", fmt.Errorf("prepare primary settings: %w", err)
	}

	if len(settings.ScenarioChain) == 0 {
		return engine.Input{Prepared: prepared}, hashData, nil
	}

	chain := make([]engine.PreparedChainLink, 0, len(settings.ScenarioChain))
	for _, link := range settings.ScenarioChain {
		linked, err := retirementMgr.LoadScenarioSettings(link.ScenarioFilename)
		if err != nil {
			return engine.Input{}, "", fmt.Errorf("failed to load chained scenario %s: %w", link.ScenarioFilename, err)
		}

		linkedHash := getSettingsHash(linked)
		hashData += linkedHash

		linkedPrepared, err := prepare.From(linked)
		if err != nil {
			return engine.Input{}, "", fmt.Errorf("prepare chained scenario %s: %w", link.ScenarioFilename, err)
		}

		chain = append(chain, engine.PreparedChainLink{
			ScenarioFilename: link.ScenarioFilename,
			TransitionAge:    link.TransitionAge,
			Settings:         linkedPrepared,
		})
	}

	combined := sha256.Sum256([]byte(hashData))
	combinedHash := fmt.Sprintf("%x", combined[:8])

	return engine.Input{Prepared: prepared, Chain: chain}, combinedHash, nil
}

// runAnalysisWithCache runs full analysis, using cache when available.
// Concurrent cache-missing requests with the same settings hash are
// coalesced (singleflight): one flight executes the RunFull fan-out while
// the rest wait for its result, so two tabs or a slider drag racing the
// debounce no longer each spin up ~3x NumCPU goroutines. ctx cancellation
// releases a waiting request without cancelling the flight (its result is
// still cached for the next request).
func runAnalysisWithCache(ctx context.Context, settings *models.WhatIfSettings) (*models.WhatIfAnalysis, error) {
	in, depHash, err := buildEngineInput(settings)
	if err != nil {
		return nil, err
	}

	if cached, ok := cachedAnalysis(depHash); ok {
		return cached, nil
	}

	ch := analysisGroup.DoChan(depHash, func() (any, error) {
		// Re-check the cache inside the flight: a previous flight for the
		// same hash may have completed and cached between our fast-path
		// check and this flight starting.
		if cached, ok := cachedAnalysis(depHash); ok {
			return cached, nil
		}
		analysis, err := runFullRecovered(in)
		if err != nil {
			return nil, err
		}

		cache.mu.Lock()
		cache.hash = depHash
		cache.analysis = analysis
		cache.cachedAt = time.Now()
		cache.mu.Unlock()

		return analysis, nil
	})
	return awaitAnalysis(ctx, ch)
}

// runFreshAnalysis runs an UNCACHED RunFull — the Monte Carlo re-roll
// endpoint, whose whole point is a fresh auto-seeded simulation — while
// still coalescing concurrent identical requests: a double-click or two
// racing tabs share one fresh run instead of stampeding two full fan-outs.
// Deliberately neither reads nor writes the analysis cache, and uses a
// distinct key namespace so a re-roll never satisfies (or is satisfied by)
// a cached-analysis flight for the same settings.
func runFreshAnalysis(ctx context.Context, depHash string, in engine.Input) (*models.WhatIfAnalysis, error) {
	ch := analysisGroup.DoChan("fresh:"+depHash, func() (any, error) {
		return runFullRecovered(in)
	})
	return awaitAnalysis(ctx, ch)
}

// buildResultsPartialData constructs the standard partial data map for
// whatif-results rendering. Always includes Verdict so that templates that
// render the verdict bar (added in the tab-workspace redesign) do not error
// on a missing field.
func buildResultsPartialData(settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis, findings interface{}) map[string]interface{} {
	activeFilename := "whatif.json"
	if retirementMgr != nil {
		activeFilename = retirementMgr.ActiveFilename()
	}
	return map[string]interface{}{
		"Settings":       settings,
		"Analysis":       analysis,
		"Verdict":        BuildVerdict(analysis, settings),
		"ActiveFilename": activeFilename,
		"Findings":       findings,
	}
}

// renderWhatIfResults renders the results partial plus the out-of-band swaps
// that resync the left column. Used by every user-initiated mutation.
func renderWhatIfResults(w http.ResponseWriter, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	renderResultsTemplate(w, "whatif-results-with-oob", settings, analysis)
}

// renderWhatIfResultsOnly renders the results column alone, with no OOB swaps.
// The background poll uses this: it must not rewrite a left-column control the
// user may be typing into or dragging.
func renderWhatIfResultsOnly(w http.ResponseWriter, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	renderResultsTemplate(w, "whatif-results", settings, analysis)
}

// renderResultsTemplate computes the shared results partial data (Completeness
// findings included so every recalc handler reports them identically) and
// renders it under the given template name, falling back to JSON when no
// renderer is configured.
func renderResultsTemplate(w http.ResponseWriter, name string, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	partialData := buildResultsPartialData(settings, analysis, completeness.Check(settings))
	if renderer != nil {
		_ = renderer.RenderPartial(w, name, partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func normalizeDisplayDollars(raw string) string {
	if raw == "real" {
		return "real"
	}
	return "nominal"
}

type projectionChartEvent struct {
	Year  float64
	Label string
}

func projectionValueAtYear(projection *models.ProjectionResult, year float64, displayDollars string) float64 {
	if projection == nil || len(projection.Months) == 0 {
		return 0
	}

	index := int(math.Round(year * 12))
	if index < 0 {
		index = 0
	}
	if index >= len(projection.Months) {
		index = len(projection.Months) - 1
	}

	month := projection.Months[index]
	if displayDollars == "real" {
		return month.PortfolioBalanceReal
	}
	return month.PortfolioBalance
}

func humanizeScenarioFilename(filename string) string {
	name := strings.TrimSuffix(filename, ".json")
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	return cases.Title(language.English).String(name)
}

// parseProjectionStartYear extracts the year from a "YYYY-MM" StartDate.
// Falls back to the current calendar year on parse failure (mirrors
// retirement.parseStartYear, which is unexported in that package).
func parseProjectionStartYear(startDate string) int {
	if startDate == "" {
		return time.Now().Year()
	}
	t, err := time.Parse("2006-01", startDate)
	if err != nil {
		return time.Now().Year()
	}
	return t.Year()
}

func buildProjectionChartEvents(settings *models.WhatIfSettings, projection *models.ProjectionResult) []projectionChartEvent {
	if settings == nil || projection == nil {
		return nil
	}

	maxYear := 0.0
	if len(projection.Months) > 0 {
		maxYear = projection.Months[len(projection.Months)-1].Year
	}

	events := make([]projectionChartEvent, 0, 8)
	seen := map[string]struct{}{}
	appendEvent := func(year float64, label string) {
		if year <= 0 || year > maxYear {
			return
		}
		key := fmt.Sprintf("%.2f:%s", year, label)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		events = append(events, projectionChartEvent{Year: year, Label: label})
	}

	for _, link := range settings.ScenarioChain {
		appendEvent(float64(link.TransitionAge-settings.CurrentAge), "Scenario: "+humanizeScenarioFilename(link.ScenarioFilename))
	}

	for _, source := range settings.IncomeSources {
		year := float64(source.StartMonth) / 12.0
		lowerName := strings.ToLower(source.Name)
		switch {
		case strings.Contains(lowerName, "social security"):
			appendEvent(year, "Social Security starts")
		case strings.Contains(lowerName, "pension"):
			appendEvent(year, "Pension starts")
		}
	}

	for _, person := range settings.HealthcarePersons {
		if hasTransition, yearsUntil, _, _ := person.GetTransitionInfo(); hasTransition {
			appendEvent(float64(yearsUntil), fmt.Sprintf("Medicare: %s", person.Name))
		}
	}

	// F-078: use calendar-year arithmetic so late-year births land on the
	// right offset. FirstRMDCalendarYear knows about BirthMonth; floor'd
	// age subtraction does not.
	startYear := parseProjectionStartYear(settings.StartDate)
	firstRMDYear := engine.FirstRMDCalendarYear(settings)
	if firstRMDYear > startYear {
		appendEvent(float64(firstRMDYear-startYear), "RMD starts")
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Year == events[j].Year {
			return events[i].Label < events[j].Label
		}
		return events[i].Year < events[j].Year
	})

	return events
}

func buildProjectionChartData(settings *models.WhatIfSettings, projection *models.ProjectionResult, displayDollars string) map[string]interface{} {
	displayDollars = normalizeDisplayDollars(displayDollars)
	if projection == nil {
		projection = &models.ProjectionResult{}
	}

	years := make([]float64, 0)
	balances := make([]float64, 0)
	maxBalance := 0.0
	for _, m := range projection.Months {
		years = append(years, m.Year)
		balance := m.PortfolioBalance
		if displayDollars == "real" {
			balance = m.PortfolioBalanceReal
		}
		balances = append(balances, balance)
		if balance > maxBalance {
			maxBalance = balance
		}
	}

	fillColor := "rgba(34, 197, 94, 0.3)"
	lineColor := "#22c55e"
	if !projection.Survives {
		fillColor = "rgba(239, 68, 68, 0.3)"
		lineColor = "#ef4444"
	}

	traces := []map[string]interface{}{
		{
			"type":      "scatter",
			"mode":      "lines",
			"name":      "Portfolio Balance",
			"x":         years,
			"y":         balances,
			"fill":      "tozeroy",
			"fillcolor": fillColor,
			"line": map[string]interface{}{
				"color": lineColor,
				"width": 2,
			},
		},
	}

	events := buildProjectionChartEvents(settings, projection)
	if len(events) > 0 {
		eventX := make([]float64, 0, len(events))
		eventY := make([]float64, 0, len(events))
		eventText := make([]string, 0, len(events))
		yearOffsets := map[float64]int{}
		for _, event := range events {
			offsetCount := yearOffsets[event.Year]
			yearOffsets[event.Year] = offsetCount + 1
			y := projectionValueAtYear(projection, event.Year, displayDollars)
			if y <= 0 {
				y = maxBalance * 0.05
			} else {
				y *= 1.02 + 0.05*float64(offsetCount)
			}
			eventX = append(eventX, event.Year)
			eventY = append(eventY, y)
			eventText = append(eventText, event.Label)
		}

		traces = append(traces, map[string]interface{}{
			"type":         "scatter",
			"mode":         "markers+text",
			"name":         "Key events",
			"x":            eventX,
			"y":            eventY,
			"text":         eventText,
			"textposition": "top center",
			"marker": map[string]interface{}{
				"color":  "#f59e0b",
				"size":   9,
				"symbol": "diamond",
			},
			"hoverinfo":  "skip",
			"cliponaxis": false,
		})
	}

	dtick := 5
	if settings != nil && settings.ProjectionYears <= 12 {
		dtick = 1
	} else if settings != nil && settings.ProjectionYears <= 24 {
		dtick = 2
	}

	yAxisTitle := "Balance ($)"
	title := "Portfolio Projection"
	if displayDollars == "real" {
		yAxisTitle = "Balance (Today's Dollars)"
		title = "Portfolio Projection In Today's Dollars"
	}

	yaxis := map[string]interface{}{
		"title":      yAxisTitle,
		"tickformat": "$,.0f",
	}
	if len(events) > 0 && maxBalance > 0 {
		// Headroom so top-of-curve event labels don't clip at the plot edge.
		yaxis["range"] = []float64{0, maxBalance * 1.18}
	}

	return map[string]interface{}{
		"data": traces,
		"layout": map[string]interface{}{
			"title": title,
			"xaxis": map[string]interface{}{
				"title":    "Years",
				"tickmode": "linear",
				"tick0":    0,
				"dtick":    dtick,
			},
			"yaxis": yaxis,
			"legend": map[string]interface{}{
				"orientation": "h",
			},
		},
	}
}

// renderError renders an HTML error fragment for HTMX requests
func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	body := fmt.Sprintf(`<div class="p-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg">
		<div class="flex items-center">
			<svg class="w-5 h-5 text-red-500 dark:text-red-400 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
			</svg>
			<span class="text-red-700 dark:text-red-300 font-medium">Error</span>
		</div>
		<p class="mt-2 text-sm text-red-600 dark:text-red-400">%s</p>
	</div>`, html.EscapeString(message))
	_, _ = w.Write([]byte(body))
}

func renderRetargetedError(w http.ResponseWriter, message string, statusCode int, target string) {
	w.Header().Set("HX-Retarget", target)
	w.Header().Set("HX-Reswap", "innerHTML")
	renderError(w, message, statusCode)
}

// parseFormFloat parses a float64 from form data, returning an error if invalid
func parseFormFloat(r *http.Request, key string) (float64, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, nil
	}
	return strconv.ParseFloat(v, 64)
}

// parseFormInt parses an int from form data, returning an error if invalid
func parseFormInt(r *http.Request, key string) (int, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

func parseSSClaimAge(r *http.Request, key string) int {
	age, err := parseFormInt(r, key)
	if err != nil || age < 62 || age > 70 {
		return 0
	}
	return age
}

func formValues(r *http.Request, key string) []string {
	if values, ok := r.Form[key+"[]"]; ok {
		return values
	}
	if values, ok := r.Form[key]; ok {
		return values
	}
	return nil
}

func formHasKey(r *http.Request, key string) bool {
	if _, ok := r.Form[key]; ok {
		return true
	}
	if _, ok := r.Form[key+"[]"]; ok {
		return true
	}
	return false
}

func parsePersonsForm(r *http.Request) (string, []models.Person, bool, error) {
	startDate := strings.TrimSpace(r.FormValue("start_date"))
	personIDs := formValues(r, "person_id")
	names := formValues(r, "person_name")
	birthMonths := formValues(r, "person_birth_month")
	roles := formValues(r, "person_role")

	hasPersons := startDate != "" || len(personIDs) > 0 || len(names) > 0 || len(birthMonths) > 0 || len(roles) > 0
	if !hasPersons {
		return "", nil, false, nil
	}

	rowCount := len(names)
	switch {
	case rowCount == 0:
		return "", nil, true, fmt.Errorf("at least one person is required")
	case len(personIDs) != rowCount, len(birthMonths) != rowCount, len(roles) != rowCount:
		return "", nil, true, fmt.Errorf("person rows are misaligned")
	}

	persons := make([]models.Person, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		id := strings.TrimSpace(personIDs[i])
		if id == "" {
			id = uuid.New().String()
		}
		role := models.PersonRole(strings.TrimSpace(roles[i]))
		switch role {
		case models.PersonRolePrimary, models.PersonRoleSpouse, models.PersonRoleOther:
		default:
			return "", nil, true, fmt.Errorf("invalid role for person row %d", i+1)
		}

		persons = append(persons, models.Person{
			ID:         id,
			Name:       strings.TrimSpace(names[i]),
			BirthMonth: strings.TrimSpace(birthMonths[i]),
			Role:       role,
		})
	}

	return startDate, persons, true, nil
}

func findHealthcarePerson(settings *models.WhatIfSettings, id string) *models.HealthcarePerson {
	for i := range settings.HealthcarePersons {
		if settings.HealthcarePersons[i].ID == id {
			return &settings.HealthcarePersons[i]
		}
	}
	return nil
}

// parseRequiredFormFloat parses a required float64 from form data
func parseRequiredFormFloat(r *http.Request, key string) (float64, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, fmt.Errorf("missing required field: %s", key)
	}
	val, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be a number", key)
	}
	return val, nil
}

var (
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
)

// Initialize sets up the whatif package with required dependencies
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, rm *retirement.SettingsManager) {
	loader = l
	renderer = r
	retirementMgr = rm
}

// RegisterRoutes registers all whatif routes
func RegisterRoutes(r chi.Router) {
	r.Get("/whatif", handleWhatIf)
	r.Post("/whatif/calculate", handleWhatIfCalculate)
	r.Post("/whatif/settings", handleWhatIfSettings)
	r.Post("/whatif/income", handleWhatIfAddIncome)
	r.Put("/whatif/income/{id}", handleWhatIfUpdateIncome)
	r.Delete("/whatif/income/{id}", handleWhatIfDeleteIncome)
	r.Post("/whatif/income/{id}/restore", handleWhatIfRestoreIncome)
	r.Delete("/whatif/income/{id}/purge", handleWhatIfPurgeIncome)
	r.Post("/whatif/expense", handleWhatIfAddExpense)
	r.Put("/whatif/expense/{id}", handleWhatIfUpdateExpense)
	r.Delete("/whatif/expense/{id}", handleWhatIfDeleteExpense)
	r.Post("/whatif/expense/{id}/restore", handleWhatIfRestoreExpense)
	r.Delete("/whatif/expense/{id}/purge", handleWhatIfPurgeExpense)
	r.Post("/whatif/healthcare", handleWhatIfAddHealthcare)
	r.Put("/whatif/healthcare/{id}", handleWhatIfUpdateHealthcare)
	r.Delete("/whatif/healthcare/{id}", handleWhatIfDeleteHealthcare)
	r.Post("/whatif/spending-phases", handleWhatIfSpendingPhases)
	r.Post("/whatif/spending-phases/add", handleWhatIfAddPhase)
	r.Delete("/whatif/spending-phases/{index}", handleWhatIfDeletePhase)
	r.Post("/whatif/spending-phases/reset", handleWhatIfResetPhases)
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
	r.Get("/whatif/chart/projection/no-guardrails", handleWhatIfProjectionChartNoGuardrails)
	r.Get("/whatif/chart/income", handleWhatIfIncomeChart)
	r.Post("/whatif/sync", handleWhatIfSync)
	r.Post("/whatif/montecarlo", handleWhatIfMonteCarlo)
	r.Post("/whatif/roth-conversion", handleWhatIfRothConversion)
	r.Post("/whatif/bigticket", handleWhatIfAddBigTicket)
	r.Delete("/whatif/bigticket/{id}", handleWhatIfDeleteBigTicket)
	r.Post("/whatif/bigticket/{id}/restore", handleWhatIfRestoreBigTicket)
	r.Delete("/whatif/bigticket/{id}/purge", handleWhatIfPurgeBigTicket)
	r.Get("/whatif/scenarios", handleListScenarios)
	r.Post("/whatif/scenarios", handleCreateScenario)
	r.Post("/whatif/scenarios/switch", handleSwitchScenario)
	r.Delete("/whatif/scenarios/{filename}", handleDeleteScenario)
	r.Put("/whatif/scenarios/{filename}", handleRenameScenario)
	r.Get("/whatif/state", handleWhatIfState)
	r.Post("/whatif/apply", handleWhatIfApply)
	r.Get("/whatif/poll", handleWhatIfPoll)
	r.Post("/whatif/chain", handleWhatIfUpdateChain)
	r.Delete("/whatif/chain/{index}", handleWhatIfDeleteChainLink)
	r.Post("/whatif/social-security", handleWhatIfSocialSecurity)
	r.Post("/whatif/glide-path", handleWhatIfGlidePath)
	r.Post("/whatif/guardrails", handleWhatIfGuardrails)
	r.Post("/whatif/tax-optimize", handleWhatIfTaxOptimize)
}

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		log.Printf("Error loading what-if settings: %v", err)
		settings = models.DefaultWhatIfSettings()
	}

	// Run full analysis (with caching)
	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	scenarios, _ := retirementMgr.ListScenarios()
	activeScenario := retirementMgr.ActiveScenario()
	activeFilename := retirementMgr.ActiveFilename()

	findings := completeness.Check(settings)

	pageData := map[string]interface{}{
		"Title":          "What-If Analysis",
		"ActiveTab":      "whatif",
		"Settings":       settings,
		"Analysis":       analysis,
		"Verdict":        BuildVerdict(analysis, settings),
		"Scenarios":      scenarios,
		"ActiveScenario": activeScenario,
		"ActiveFilename": activeFilename,
		"Findings":       findings,
	}

	templates.AttachDuplicateCount(pageData, loader)
	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>What-If Analysis</h1><p>Templates not loaded.</p></body></html>"))
	}
}

func handleWhatIfCalculate(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// A pure recalculation: nothing was written, so there is no revision of
	// this request's own to hand the client as a baseline.
	renderRecalc(w, r, settings, revisionUnreported)
}
func handleWhatIfProjectionChart(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	displayDollars := normalizeDisplayDollars(r.URL.Query().Get("display_dollars"))
	chartData := buildProjectionChartData(settings, analysis.Projection, displayDollars)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chartData)
}

func handleWhatIfIncomeChart(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	displayDollars := normalizeDisplayDollars(r.URL.Query().Get("display_dollars"))
	chartData := buildIncomeChartData(settings, analysis.Projection, displayDollars)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chartData)
}

// buildIncomeChartData aggregates monthly projection rows into yearly
// income buckets (Social Security, other ordinary income, portfolio
// withdrawals) and returns a stacked-area Plotly trace set. When
// displayDollars == "real", values are deflated to today's dollars
// using each month's cumulative inflation factor.
func buildIncomeChartData(settings *models.WhatIfSettings, projection *models.ProjectionResult, displayDollars string) map[string]interface{} {
	displayDollars = normalizeDisplayDollars(displayDollars)
	if projection == nil {
		projection = &models.ProjectionResult{}
	}

	// Accumulate per-year buckets keyed by integer year index.
	type yearBucket struct {
		ss          float64
		other       float64
		withdrawals float64
	}
	yearBuckets := map[int]*yearBucket{}
	yearOrder := []int{}

	for _, m := range projection.Months {
		yi := int(m.Year)
		bucket, ok := yearBuckets[yi]
		if !ok {
			bucket = &yearBucket{}
			yearBuckets[yi] = bucket
			yearOrder = append(yearOrder, yi)
		}

		ss := m.SocialSecurityIncome
		other := m.TotalIncome - m.SocialSecurityIncome
		if other < 0 {
			other = 0
		}
		withdrawals := m.WithdrawalFromTaxDeferred + m.WithdrawalFromTaxable + m.WithdrawalFromRoth

		if displayDollars == "real" && m.CumulativeInflation > 0 {
			ss /= m.CumulativeInflation
			other /= m.CumulativeInflation
			withdrawals /= m.CumulativeInflation
		}

		bucket.ss += ss
		bucket.other += other
		bucket.withdrawals += withdrawals
	}

	years := make([]int, 0, len(yearOrder))
	ssSeries := make([]float64, 0, len(yearOrder))
	otherSeries := make([]float64, 0, len(yearOrder))
	withdrawSeries := make([]float64, 0, len(yearOrder))
	for _, yi := range yearOrder {
		b := yearBuckets[yi]
		years = append(years, yi)
		ssSeries = append(ssSeries, b.ss)
		otherSeries = append(otherSeries, b.other)
		withdrawSeries = append(withdrawSeries, b.withdrawals)
	}

	traces := []map[string]interface{}{
		{
			"type":          "scatter",
			"mode":          "lines",
			"name":          "Social Security",
			"x":             years,
			"y":             ssSeries,
			"stackgroup":    "income",
			"fillcolor":     "rgba(245, 158, 11, 0.5)",
			"line":          map[string]interface{}{"color": "#f59e0b", "width": 1},
			"hovertemplate": "Year %{x}<br>SS: $%{y:,.0f}<extra></extra>",
		},
		{
			"type":          "scatter",
			"mode":          "lines",
			"name":          "Other Income",
			"x":             years,
			"y":             otherSeries,
			"stackgroup":    "income",
			"fillcolor":     "rgba(34, 197, 94, 0.5)",
			"line":          map[string]interface{}{"color": "#22c55e", "width": 1},
			"hovertemplate": "Year %{x}<br>Other: $%{y:,.0f}<extra></extra>",
		},
		{
			"type":          "scatter",
			"mode":          "lines",
			"name":          "Withdrawals",
			"x":             years,
			"y":             withdrawSeries,
			"stackgroup":    "income",
			"fillcolor":     "rgba(59, 130, 246, 0.5)",
			"line":          map[string]interface{}{"color": "#3b82f6", "width": 1},
			"hovertemplate": "Year %{x}<br>Withdrawals: $%{y:,.0f}<extra></extra>",
		},
	}

	dtick := 5
	if settings != nil && settings.ProjectionYears <= 12 {
		dtick = 1
	} else if settings != nil && settings.ProjectionYears <= 24 {
		dtick = 2
	}

	yAxisTitle := "Annual Income ($)"
	title := "Yearly Income by Source"
	if displayDollars == "real" {
		yAxisTitle = "Annual Income (Today's Dollars)"
		title = "Yearly Income by Source — Today's Dollars"
	}

	return map[string]interface{}{
		"data": traces,
		"layout": map[string]interface{}{
			"title": title,
			"xaxis": map[string]interface{}{
				"title":    "Year",
				"tickmode": "linear",
				"tick0":    0,
				"dtick":    dtick,
			},
			"yaxis": map[string]interface{}{
				"title":      yAxisTitle,
				"tickformat": "$,.0f",
			},
			"hovermode": "x unified",
			"legend": map[string]interface{}{
				"orientation": "h",
			},
		},
	}
}

func handleWhatIfProjectionChartNoGuardrails(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Build a copy with guardrails forced off.
	clone := *settings
	clone.Guardrails = nil

	// Use buildEngineInput + DefaultHooks so SS optimizer income and chain
	// transitions match the guardrails-on path. The endpoint is "without
	// guardrails", not "without SS or chain".
	in, _, err := buildEngineInput(&clone)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	in.Hooks = retirement.DefaultHooks()
	projection := getEngine().Run(in)

	displayDollars := normalizeDisplayDollars(r.URL.Query().Get("display_dollars"))
	chartData := buildProjectionChartData(&clone, projection, displayDollars)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chartData)
}

func handleWhatIfSync(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync expenses and income from dashboard
	if err := syncSettingsFromDashboard(settings); err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	saveAndRecalc(w, r, settings)
}

// syncSettingsFromDashboard updates settings with values from dashboard data
func syncSettingsFromDashboard(settings *models.WhatIfSettings) error {
	data, err := loader.LoadData()
	if err != nil {
		return err
	}

	// Calculate average monthly values from last 12 months
	now := time.Now()
	yearAgo := now.AddDate(-1, 0, 0)
	filtered := data.Active().FilterByDateRange(yearAgo, now)
	outflows := filtered.FilterByType(models.Outflow)

	months := 12.0
	if filtered.MinDate().After(yearAgo) {
		months = now.Sub(filtered.MinDate()).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}

	// Calculate and set average monthly expenses.
	// Signed sum + math.Abs so refunds reduce the total instead of inflating it.
	totalExpenses := math.Abs(outflows.SumAmount())
	settings.MonthlyLivingExpenses = totalExpenses / months

	// Use insights income pattern detection for individual income sources
	incomePatterns := insights.IncomePatterns(filtered)

	// Remove old auto-detected sources (prefixed with "insights-" or old "dashboard-income")
	// Keep user-added sources (no special prefix)
	// BUT preserve user modifications (EndMonth, StartMonth, COLARate, Type) from existing insights sources
	userSources := make([]models.IncomeSource, 0)
	existingMods := make(map[string]models.IncomeSource)

	for _, src := range settings.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") || src.ID == "dashboard-income" {
			// Save user modifications for this auto-detected source
			existingMods[src.ID] = src
		} else {
			userSources = append(userSources, src)
		}
	}

	// Convert detected income patterns to income sources
	for _, pattern := range incomePatterns {
		// Only include regular income patterns (skip one-time or irregular)
		if !pattern.IsRegular {
			continue
		}

		// Convert to monthly amount based on frequency
		monthlyAmount := pattern.AvgAmount
		switch pattern.Frequency {
		case "weekly":
			monthlyAmount = pattern.AvgAmount * 52 / 12
		case "biweekly":
			monthlyAmount = pattern.AvgAmount * 26 / 12
			// monthly is already correct
		}

		// Create a stable ID from the description
		id := "insights-" + strings.ToLower(strings.ReplaceAll(pattern.Description, " ", "-"))

		newSource := models.IncomeSource{
			ID:     id,
			Name:   cases.Title(language.English).String(pattern.Description),
			Amount: monthlyAmount,
			Type:   models.IncomeFixed,
		}

		// Preserve user modifications from existing source with same ID
		if existing, ok := existingMods[id]; ok {
			newSource.EndMonth = existing.EndMonth
			newSource.StartMonth = existing.StartMonth
			newSource.COLARate = existing.COLARate
			newSource.InflationAdjusted = existing.InflationAdjusted
			// Preserve Type only if user changed it from default
			if existing.Type != "" && existing.Type != models.IncomeFixed {
				newSource.Type = existing.Type
			}
		}

		userSources = append(userSources, newSource)
	}

	settings.IncomeSources = userSources

	return nil
}
