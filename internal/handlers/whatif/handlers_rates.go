package whatif

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

func handleWhatIfSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	startDate, persons, hasPersons, err := parsePersonsForm(r)
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if hasPersons && startDate == "" {
		renderError(w, "Projection start date is required", http.StatusBadRequest)
		return
	}

	updates := make(map[string]interface{})
	if msg := applySettingsFormSpec(r, updates); msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}
	if msg := applyProjectionTiming(r, updates); msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}
	if msg := applyRMDTiming(r, updates); msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}
	applySpouseSoleBeneficiary(r, updates)
	applyACAAdvanceCredits(r, updates)
	if msg := validateSettingsCrossFieldInvariants(r, updates); msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}
	clampPerAccountAllocations(updates)

	recalcAndRender(w, r, "Failed to save settings", func() (*models.WhatIfSettings, int, error) {
		if hasPersons {
			return retirementMgr.UpdateSettingsWithPersons(updates, startDate, persons)
		}
		return retirementMgr.UpdateSettings(updates)
	})
}
func handleWhatIfMonteCarlo(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-run the full analysis, which includes a fresh (auto-seeded) Monte
	// Carlo simulation. Deliberately uncached — the point is a re-roll —
	// but still coalesced via runFreshAnalysis so a double-click or two
	// racing tabs share one fan-out instead of stampeding two.
	in, depHash, err := buildEngineInput(settings)
	if err != nil {
		renderError(w, "Failed to build engine input: "+err.Error(), http.StatusInternalServerError)
		return
	}
	analysis, err := runFreshAnalysis(r.Context(), depHash, in)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}

// handleWhatIfSpendingPhases handles updates to spending phase configuration
func handleWhatIfSpendingPhases(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse enabled toggle
	enabled := checkboxOn(r, "enabled")

	// Load current settings to get existing phases as base
	currentSettings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Use current phases or defaults
	basePhases := models.DefaultSpendingPhases()
	if currentSettings.SpendingPhaseConfig != nil && len(currentSettings.SpendingPhaseConfig.Phases) > 0 {
		basePhases = currentSettings.SpendingPhaseConfig.Phases
	}

	// Build phases from form data without truncating higher-index phases.
	phases := []models.SpendingPhase{}
	phaseCount := len(basePhases)
	if maxSubmitted := maxSubmittedSpendingPhaseIndex(r.Form); maxSubmitted >= 0 && maxSubmitted+1 > phaseCount {
		phaseCount = maxSubmitted + 1
	}
	for i := 0; i < phaseCount; i++ {
		// Check if this phase exists in form data
		nameKey := fmt.Sprintf("phase_%d_name", i)
		multKey := fmt.Sprintf("phase_%d_multiplier", i)

		// If no multiplier field, phase doesn't exist
		if r.FormValue(multKey) == "" {
			// Use base phase if available
			if i < len(basePhases) {
				phase := basePhases[i]
				// Parse start age if provided
				if startAge, err := parseFormInt(r, fmt.Sprintf("phase_%d_start_age", i)); err == nil && startAge > 0 {
					phase.StartAge = startAge
				}
				phases = append(phases, phase)
			}
			continue
		}

		// Create phase from form data
		phase := models.SpendingPhase{}
		if i < len(basePhases) {
			phase = basePhases[i] // Start with base values
		}

		// Parse name if provided
		if name := r.FormValue(nameKey); name != "" {
			phase.Name = name
		}

		// Parse start age if provided (skip for phase 0 which always starts at 0)
		if i > 0 {
			if startAge, err := parseFormInt(r, fmt.Sprintf("phase_%d_start_age", i)); err == nil {
				phase.StartAge = startAge
			}
		}

		// Parse multiplier
		if mult, err := parseFormFloat(r, multKey); err == nil {
			phase.Multiplier = mult
		}

		// Parse description if provided
		if desc := r.FormValue(fmt.Sprintf("phase_%d_description", i)); desc != "" {
			phase.Description = desc
		}

		phases = append(phases, phase)
	}

	recalcAndRender(w, r, "Failed to save spending phases", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.UpdateSpendingPhases(enabled, phases)
		return settings, revisionUnreported, err
	})
}

// handleWhatIfAddPhase adds a new spending phase
func handleWhatIfAddPhase(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize phases if needed
	if settings.SpendingPhaseConfig == nil {
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases:  models.DefaultSpendingPhases(),
		}
	}

	// Find the last phase to determine new phase values
	phases := settings.SpendingPhaseConfig.Phases
	lastPhase := phases[len(phases)-1]

	// Create new phase 5 years after the last one, with 5% lower multiplier
	newMultiplier := lastPhase.Multiplier - 0.05
	if newMultiplier < 0.30 {
		newMultiplier = 0.30 // Floor at 30%
	}

	newPhase := models.SpendingPhase{
		Name:        fmt.Sprintf("Phase %d", len(phases)+1),
		StartAge:    lastPhase.StartAge + 5,
		Multiplier:  newMultiplier,
		Description: "Custom spending phase",
	}

	settings.SpendingPhaseConfig.Phases = append(settings.SpendingPhaseConfig.Phases, newPhase)

	saveAndRecalc(w, r, settings)
}

// handleWhatIfDeletePhase removes a spending phase by index
func handleWhatIfDeletePhase(w http.ResponseWriter, r *http.Request) {
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		renderError(w, "Invalid phase index", http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if settings.SpendingPhaseConfig == nil || len(settings.SpendingPhaseConfig.Phases) == 0 {
		renderError(w, "No phases to delete", http.StatusBadRequest)
		return
	}

	// Don't allow deleting the first phase or below minimum
	if index == 0 {
		renderError(w, "Cannot delete the first phase", http.StatusBadRequest)
		return
	}

	phases := settings.SpendingPhaseConfig.Phases
	if index < 0 || index >= len(phases) {
		renderError(w, "Phase index out of range", http.StatusBadRequest)
		return
	}

	// Minimum 2 phases
	if len(phases) <= 2 {
		renderError(w, "Must have at least 2 phases", http.StatusBadRequest)
		return
	}

	// Remove the phase at index
	settings.SpendingPhaseConfig.Phases = append(phases[:index], phases[index+1:]...)

	saveAndRecalc(w, r, settings)
}

// handleWhatIfResetPhases resets phases to defaults
func handleWhatIfResetPhases(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset to default phases
	settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: settings.SpendingPhaseConfig != nil && settings.SpendingPhaseConfig.Enabled,
		Phases:  models.DefaultSpendingPhases(),
	}

	saveAndRecalc(w, r, settings)
}

// handleWhatIfRothConversion handles Roth conversion configuration updates
func handleWhatIfRothConversion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize RothConversion if nil
	if settings.RothConversion == nil {
		settings.RothConversion = &models.RothConversionConfig{}
	}

	// Parse enabled checkbox (unchecked means not present in form)
	settings.RothConversion.Enabled = r.FormValue("enabled") == "on"

	// Parse numeric fields
	if amount, err := parseFormFloat(r, "annual_amount"); err == nil {
		if amount < 0 {
			renderError(w, "Annual conversion amount cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.AnnualAmount = amount
	}

	if startYear, err := parseFormInt(r, "start_year"); err == nil {
		if startYear < 0 {
			renderError(w, "Start year cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.StartYear = startYear
	}

	if endYear, err := parseFormInt(r, "end_year"); err == nil {
		if endYear < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.EndYear = endYear
	}
	if settings.RothConversion.EndYear != 0 && settings.RothConversion.EndYear < settings.RothConversion.StartYear {
		renderError(w, "End year cannot be earlier than start year", http.StatusBadRequest)
		return
	}

	// The conversion-sweep panel's "Apply" buttons (T16) post here — the same
	// route and mutation semantics as the standalone Roth Conversion form
	// above — but need the sweep table re-rendered afterward, not the
	// standard what-if results column, so the "current" marker moves to the
	// applied row. The standalone form never sends apply_source, so this
	// branch changes nothing for any other caller of this handler.
	if r.FormValue("apply_source") == conversionSweepApplySource {
		saveAndRenderConversionSweep(w, r, settings)
		return
	}

	// Save settings
	saveAndRecalc(w, r, settings)
}
func maxSubmittedSpendingPhaseIndex(form map[string][]string) int {
	maxIndex := -1
	for key := range form {
		if !strings.HasPrefix(key, "phase_") {
			continue
		}
		rest := strings.TrimPrefix(key, "phase_")
		indexText, _, ok := strings.Cut(rest, "_")
		if !ok {
			continue
		}
		index, err := strconv.Atoi(indexText)
		if err != nil {
			continue
		}
		if index > maxIndex {
			maxIndex = index
		}
	}
	return maxIndex
}
func handleWhatIfSocialSecurity(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if settings.SocialSecurity == nil {
		settings.SocialSecurity = &models.SocialSecurityConfig{}
	}

	if fraBenefit, err := parseFormFloat(r, "fra_benefit"); err == nil {
		settings.SocialSecurity.FRABenefit = fraBenefit
	}

	if fra, err := parseFormInt(r, "fra"); err == nil && fra >= 62 && fra <= 70 {
		settings.SocialSecurity.FRA = fra
	} else if settings.SocialSecurity.FRA == 0 {
		settings.SocialSecurity.FRA = 67
	}
	settings.SocialSecurity.ClaimAge = parseSSClaimAge(r, "claim_age")

	if colaRate, err := parseFormFloat(r, "cola_rate"); err == nil {
		settings.SocialSecurity.COLARate = colaRate / 100.0
		settings.SocialSecurity.COLARateSet = true // F-026: user explicitly submitted a value
	} else if settings.SocialSecurity.COLARate == 0 && !settings.SocialSecurity.COLARateSet {
		settings.SocialSecurity.COLARate = 0.02
	}

	if spouseBenefit, err := parseFormFloat(r, "spouse_fra_benefit"); err == nil {
		settings.SocialSecurity.SpouseFRABenefit = spouseBenefit
	}

	if spouseFRA, err := parseFormInt(r, "spouse_fra"); err == nil && spouseFRA >= 62 && spouseFRA <= 70 {
		settings.SocialSecurity.SpouseFRA = spouseFRA
	}
	settings.SocialSecurity.SpouseClaimAge = parseSSClaimAge(r, "spouse_claim_age")

	// Clear config if no benefit entered
	if settings.SocialSecurity.FRABenefit <= 0 {
		settings.SocialSecurity = nil
	}

	saveAndRecalc(w, r, settings)
}
func handleWhatIfGlidePath(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	enabled := checkboxOn(r, "enabled")

	if enabled {
		if settings.GlidePath == nil {
			settings.GlidePath = &models.GlidePathConfig{}
		}
		settings.GlidePath.Enabled = true

		applyClampedFloatFields(r, []clampedFloatField{
			{"start_stock_pct", 0, 100, &settings.GlidePath.StartStockPct},
			{"end_stock_pct", 0, 100, &settings.GlidePath.EndStockPct},
		})
		if v, err := parseFormInt(r, "transition_years"); err == nil {
			settings.GlidePath.TransitionYears = max(1, min(50, v))
		}
	} else {
		if settings.GlidePath != nil {
			settings.GlidePath.Enabled = false
		}
	}

	saveAndRecalc(w, r, settings)
}
func handleWhatIfGuardrails(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	enabled := checkboxOn(r, "enabled")

	if enabled {
		if settings.Guardrails == nil {
			settings.Guardrails = &models.GuardrailConfig{
				FloorDropPct:    20,
				FloorCutPct:     10,
				CeilingRisePct:  20,
				CeilingRaisePct: 10,
				MinSpendingPct:  75,
				MaxSpendingPct:  120,
			}
		}
		settings.Guardrails.Enabled = true

		applyClampedFloatFields(r, []clampedFloatField{
			{"floor_drop_pct", 1, 50, &settings.Guardrails.FloorDropPct},
			{"floor_cut_pct", 1, 50, &settings.Guardrails.FloorCutPct},
			{"ceiling_rise_pct", 1, 100, &settings.Guardrails.CeilingRisePct},
			{"ceiling_raise_pct", 1, 50, &settings.Guardrails.CeilingRaisePct},
			{"min_spending_pct", 50, 100, &settings.Guardrails.MinSpendingPct},
			{"max_spending_pct", 100, 200, &settings.Guardrails.MaxSpendingPct},
		})
	} else {
		if settings.Guardrails != nil {
			settings.Guardrails.Enabled = false
		}
	}

	saveAndRecalc(w, r, settings)
}

// handleWhatIfTaxOptimize runs the Tax Optimizer on demand. This is an
// explicit user-triggered endpoint; it is NOT called during the normal
// HTMX recalc path because the optimizer's cost (~38 ms) is too high
// for interactive slider drags.
func handleWhatIfTaxOptimize(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	in, _, err := buildEngineInput(settings)
	if err != nil {
		renderError(w, "Failed to build engine input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	taxOptimizer := retirement.RunTaxOptimizer(getEngine(), in)

	analysis := &models.WhatIfAnalysis{
		Settings:     settings,
		TaxOptimizer: taxOptimizer,
	}

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-tax-optimizer-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
