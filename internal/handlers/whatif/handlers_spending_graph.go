package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	budgettemplates "budget2/internal/templates"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Series values use DisplayDollars exclusively. Annual pointwise percentiles
// are never described as one coherent path; Worst is one retained observed path.
type spendingGraphSeries struct {
	Label                                    string `json:"label"`
	Years                                    []int  `json:"years"`
	PathCounts                               []int  `json:"path_counts,omitempty"`
	LivingP10, LivingP50, LivingP90          []float64
	PortfolioP10, PortfolioP50, PortfolioP90 []float64
	Floor                                    *float64 `json:"floor,omitempty"`
	FloorNote                                string   `json:"floor_note,omitempty"`
}
type spendingGraphPayload struct {
	Candidate                  models.SpendingCandidate        `json:"candidate"`
	CandidateLabel             string                          `json:"candidate_label"`
	Request                    models.SpendingOptimizerRequest `json:"request"`
	FloorMonthlyReal           float64                         `json:"floor_monthly_real"`
	CurrentBaseMonthlyReal     float64                         `json:"current_base_monthly_real"`
	CurrentStartingMonthlyReal float64                         `json:"current_starting_monthly_real"`
	SearchSeed                 string                          `json:"search_seed"`
	SelectionSeed              string                          `json:"selection_seed"`
	ValidationSeed             string                          `json:"validation_seed"`
	ValidationRuns             int                             `json:"validation_runs"`
	DisplayDollars             string                          `json:"display_dollars"`
	BaseCaseLabel              string                          `json:"base_case_label"`
	BaseCaseChart              map[string]interface{}          `json:"base_case_chart"`
	FundingTimeline            *models.SpendingFundingTimeline `json:"funding_timeline"`
	Simulated                  spendingGraphSeries             `json:"simulated"`
	Worst                      spendingGraphSeries             `json:"worst"`
}

func spendingGraphCandidateLabel(candidate models.SpendingCandidate) string {
	class := "Selected spending option"
	switch candidate.Kind {
	case "current":
		class = "Current plan"
	case "planned":
		class = "Follow planned spending"
	case "flexible":
		class = "Flexible spending"
	}
	return fmt.Sprintf("%s at %s per month", class, budgettemplates.FormatMoney(candidate.BaseMonthlyLivingExpenses))
}

func handleSpendingOptimizerGraph(w http.ResponseWriter, r *http.Request) {
	handleSpendingOptimizerGraphWithRunner(w, r, getEngine().Run)
}
func handleSpendingOptimizerGraphWithRunner(w http.ResponseWriter, r *http.Request, run func(engine.Input) *models.ProjectionResult) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(message string, status int) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	if err := r.ParseForm(); err != nil {
		fail("Invalid graph request.", 400)
		return
	}
	id := r.PostForm.Get("request_id")
	spendingPreviews.Lock()
	p := spendingPreviews.entries[id]
	if p == nil || p.active || p.applying || p.result == nil {
		spendingPreviews.Unlock()
		fail("Graph unavailable. Run again.", 409)
		return
	}
	c, ok := p.graphs[r.PostForm.Get("candidate")]
	if !ok {
		spendingPreviews.Unlock()
		fail("Graph unavailable. Run again.", 409)
		return
	}
	candidate := cloneSpendingCandidate(c)
	request := p.request
	request.LivingSpendingBoost = models.CloneLivingSpendingBoost(request.LivingSpendingBoost)
	searchSeed, selectionSeed, validationSeed, runs := p.result.SearchSeed, p.result.SelectionSeed, p.result.ValidationSeed, p.result.ValidationRuns
	spendingPreviews.Unlock()
	settings, err := loadSpendingSnapshot(r.Context(), p)
	if err != nil {
		fail("The plan changed or preview expired. Run again.", spendingSnapshotStatus(err))
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		fail("Could not prepare graph.", 500)
		return
	}
	in.Hooks = retirement.DefaultHooks()
	currentStart := engine.LivingExpensesAtMonth(in.Prepared.Settings(), 0)
	in, err = analysis.PrepareSpendingCandidate(in, candidate)
	if err != nil {
		fail("Could not prepare candidate graph.", 500)
		return
	}
	projection := run(in)
	if projection == nil {
		fail("Could not project candidate.", 500)
		return
	}
	fundingTimeline, err := analysis.BuildSpendingFundingTimeline(in, projection)
	if err != nil {
		fail("Could not prepare funding timeline.", 500)
		return
	}
	addProjectedSSFundingMarkers(fundingTimeline, in.Prepared.Settings())
	mode := "real"
	if r.PostForm.Get("display_dollars") == "nominal" {
		mode = "nominal"
	}
	payload := spendingGraphPayload{Candidate: candidate, CandidateLabel: spendingGraphCandidateLabel(candidate), Request: request, FloorMonthlyReal: request.FloorMonthlyReal, CurrentBaseMonthlyReal: settings.MonthlyLivingExpenses, CurrentStartingMonthlyReal: currentStart, SearchSeed: strconv.FormatInt(searchSeed, 10), SelectionSeed: strconv.FormatInt(selectionSeed, 10), ValidationSeed: strconv.FormatInt(validationSeed, 10), ValidationRuns: runs, DisplayDollars: mode, BaseCaseLabel: "Base case — canonical configured assumptions", BaseCaseChart: buildProjectionChartData(in.Prepared.Settings(), projection, mode), FundingTimeline: fundingTimeline}
	payload.Simulated, payload.Worst = spendingEvidenceSeries(candidate, request.FloorMonthlyReal, mode)
	body, err := json.Marshal(payload)
	if err != nil {
		fail("Could not render graph.", 500)
		return
	}
	// Canonical work and serialization never block registry cancellation. Revalidate
	// both saved identity and the registry entry before emitting the buffered response.
	if _, err := loadSpendingSnapshot(r.Context(), p); err != nil {
		fail("The plan changed or preview expired. Run again.", spendingSnapshotStatus(err))
		return
	}
	spendingPreviews.Lock()
	publish := spendingPreviews.entries[id] == p && !p.applying && time.Now().Before(p.expires) && r.Context().Err() == nil
	spendingPreviews.Unlock()
	// Authorization is checked before delivery; no client write may hold the
	// registry lock, including the stale-error response.
	if !publish {
		fail("Graph cancelled or expired. Run again.", 409)
		return
	}
	_, _ = w.Write(body)
}

func addProjectedSSFundingMarkers(timeline *models.SpendingFundingTimeline, settings *models.WhatIfSettings) {
	if timeline == nil {
		return
	}
	for _, entry := range retirement.ProjectedSSEntries(settings) {
		if entry.MonthlyAmount <= 0 || entry.StartMonth < 0 || entry.StartMonth >= len(timeline.Months) {
			continue
		}
		timeline.Markers = append(timeline.Markers, models.SpendingFundingMarker{
			Month:         entry.StartMonth,
			CalendarMonth: timeline.Months[entry.StartMonth].CalendarMonth,
			Kind:          "social_security_start",
			Label:         entry.Label + " starts",
		})
	}
	sort.SliceStable(timeline.Markers, func(i, j int) bool {
		if timeline.Markers[i].Month != timeline.Markers[j].Month {
			return timeline.Markers[i].Month < timeline.Markers[j].Month
		}
		if timeline.Markers[i].Kind != timeline.Markers[j].Kind {
			return timeline.Markers[i].Kind < timeline.Markers[j].Kind
		}
		return timeline.Markers[i].Label < timeline.Markers[j].Label
	})
}

func spendingEvidenceSeries(c models.SpendingCandidate, floor float64, mode string) (spendingGraphSeries, spendingGraphSeries) {
	simulated := spendingGraphSeries{Label: "Simulated annual outcomes — pointwise percentiles"}
	worst := spendingGraphSeries{Label: "Lowest observed living path — one retained simulated future"}
	if mode == "real" {
		simulated.Floor = &floor
		worst.Floor = &floor
	} else {
		note := "Minimum line omitted: path-specific inflation is not retained. The minimum is defined in today's dollars."
		simulated.FloorNote = note
		worst.FloorNote = note
	}
	for _, year := range c.SimulationYears {
		living, portfolio := year.LivingReal, year.PortfolioReal
		if mode == "nominal" {
			living, portfolio = year.LivingNominal, year.PortfolioNominal
		}
		simulated.Years = append(simulated.Years, year.Year)
		simulated.PathCounts = append(simulated.PathCounts, year.Paths)
		simulated.LivingP10 = append(simulated.LivingP10, living.P10)
		simulated.LivingP50 = append(simulated.LivingP50, living.P50)
		simulated.LivingP90 = append(simulated.LivingP90, living.P90)
		simulated.PortfolioP10 = append(simulated.PortfolioP10, portfolio.P10)
		simulated.PortfolioP50 = append(simulated.PortfolioP50, portfolio.P50)
		simulated.PortfolioP90 = append(simulated.PortfolioP90, portfolio.P90)
	}
	for _, year := range c.WorstPathYears {
		living, portfolio := year.LivingReal, year.PortfolioReal
		if mode == "nominal" {
			living, portfolio = year.LivingNominal, year.PortfolioNominal
		}
		worst.Years = append(worst.Years, year.Year)
		worst.LivingP50 = append(worst.LivingP50, living)
		worst.PortfolioP50 = append(worst.PortfolioP50, portfolio)
	}
	return simulated, worst
}
