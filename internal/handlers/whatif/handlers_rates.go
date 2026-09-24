package whatif

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/prepare"
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
	if hasPersons {
		// D7: a removed person who is still referenced by a healthcare entry
		// (healthcare_persons[].person_id) must be refused with a message
		// naming that entry, next to the form -- not the generic
		// "healthcare_persons: person_id ... not found" prepare.ValidatePersons
		// returns deep inside the save, which statusForMutationError has no
		// special case for and therefore maps to 500. Checked here, before
		// any write is attempted, so a refusal never touches disk.
		//
		// TOCTOU note: this reads current settings outside
		// SettingsManager's write lock, the same as
		// handleWhatIfUpdateHealthcare's existing load-then-save. A
		// concurrent edit landing in the gap is still caught by
		// ValidatePersons inside the save's lock -- just without this
		// handler's friendlier message -- so correctness never depends on
		// this check running.
		settingsState, err := retirementMgr.Load()
		if err != nil {
			renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if msg := personRemovalBlockedByHealthcare(settingsState, persons); msg != "" {
			renderError(w, msg, http.StatusBadRequest)
			return
		}
		updates["use_current_month"] = r.FormValue("use_current_month") == "true"
	}
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

	if hasPersons {
		// "Never a 500": a person row a direct POST could still submit with
		// a name but no birth month (the browser's own `required` on that
		// field -- D7 -- stops this in a real browser, but nothing stops a
		// raw request) reaches prepare.ValidatePersons deep inside the
		// save, which returns a TYPED error: prepare.PersonBirthMonthError
		// and PersonBirthMonthAfterStartError each carry the failing row's
		// PersonID, while prepare.InvalidStartDateError concerns no row (a
		// bad StartDate is not a per-person problem) and is matched by
		// type alone. statusForMutationError has no case for any of the
		// three and falls through to 500.
		// personsSaveErrorMessage below maps that to 400 with a clear
		// message, identifying a failing PERSON ONLY via errors.As plus
		// PersonID looked up in this request's own persons slice -- never
		// by parsing Error()'s text (ruling 2026-09-24i).
		settings, revision, err := retirementMgr.UpdateSettingsWithPersons(updates, startDate, persons)
		if err != nil {
			if msg, ok := personsSaveErrorMessage(err, persons); ok {
				renderError(w, msg, http.StatusBadRequest)
				return
			}
			renderError(w, "Failed to save settings: "+err.Error(), statusForMutationError(err))
			return
		}
		renderRecalc(w, r, settings, revision)
		return
	}

	recalcAndRender(w, r, "Failed to save settings", func() (*models.WhatIfSettings, int, error) {
		return retirementMgr.UpdateSettings(updates)
	})
}

// findPersonByID returns a pointer to the submitted person with the given
// ID, or nil. The sole lookup personsSaveErrorMessage may use to identify a
// failing row -- see its own doc comment (ruling 2026-09-24i).
func findPersonByID(persons []models.Person, id string) *models.Person {
	for i := range persons {
		if persons[i].ID == id {
			return &persons[i]
		}
	}
	return nil
}

// personsSaveErrorMessage recognizes an error prepare.ValidatePersons
// returns and translates it into a clear, TRUTHFUL, user-facing 400
// message for each distinct shape that function can return --
// distinguishing a malformed persons submission, which a direct POST can
// still produce past the browser's own `required`/JS guards, from any
// other save failure (still left to the existing generic mapping, which is
// a genuine 500 for those). ok is false for anything else.
//
// The three per-row/per-date shapes -- prepare.InvalidStartDateError,
// prepare.PersonBirthMonthError, prepare.PersonBirthMonthAfterStartError --
// are identified ONLY via errors.As, and a failing PERSON only by that
// typed error's PersonID, looked up in persons (the request's OWN
// submitted slice, never reparsed from the error). This is the fix for the
// third failure of this class (ruling 2026-09-24g/h/i): regexing the
// error's Go-escaped (%q) text, or matching by Name, both break on a name
// that collides with another row, or that contains a quote, backslash, tab
// or other character %q or a "[^\"]*" capture handles unfaithfully.
// Wording below is built from that row's SUBMITTED Name and BirthMonth,
// printed with %s -- never %q, never any other Go escaping -- so it is
// reproduced verbatim; renderError's own html.EscapeString call escapes it
// for HTML at render, exactly once.
func personsSaveErrorMessage(err error, persons []models.Person) (string, bool) {
	if err == nil {
		return "", false
	}

	// The PROJECTION start date itself doesn't parse. Must never be
	// labelled "person data" -- it isn't about any person.
	var startErr *prepare.InvalidStartDateError
	if errors.As(err, &startErr) {
		return "Invalid projection start date: " + startErr.Err.Error(), true
	}

	// Missing OR unparseable birth month -- decided from the FAILING row's
	// own submitted BirthMonth, found by ID.
	var birthErr *prepare.PersonBirthMonthError
	if errors.As(err, &birthErr) {
		if p := findPersonByID(persons, birthErr.PersonID); p != nil {
			if strings.TrimSpace(p.BirthMonth) == "" {
				return fmt.Sprintf("%s needs a birth month", p.Name), true
			}
			return fmt.Sprintf(`%s's birth month "%s" isn't a valid month (use YYYY-MM)`, p.Name, p.BirthMonth), true
		}
		// The failing ID isn't in this request's own submitted persons --
		// should not happen (the error came from validating this same
		// slice), but never guess a name or a value without evidence.
		return "Invalid person data: " + err.Error(), true
	}

	// A birth month WAS entered; it is simply later than the plan's start
	// date. Must never say "needs a birth month" -- that would be false.
	var afterErr *prepare.PersonBirthMonthAfterStartError
	if errors.As(err, &afterErr) {
		if p := findPersonByID(persons, afterErr.PersonID); p != nil {
			return fmt.Sprintf("%s's birth month (%s) is after the plan's start date (%s)", p.Name, p.BirthMonth, afterErr.StartDate), true
		}
		return "Invalid person data: " + err.Error(), true
	}

	// Every other prepare.ValidatePersons shape (at least one person
	// required, id required, duplicate id, name required, invalid role,
	// wrong primary/spouse count): no dedicated wording is needed to be
	// TRUTHFUL -- the underlying message already says exactly what is
	// wrong. Recognized only by its "persons:"/"start_date:" prefix so an
	// unrelated save failure (a disk error, say) still falls through to
	// the caller's generic 500 mapping.
	msg := err.Error()
	if !strings.HasPrefix(msg, "persons:") && !strings.HasPrefix(msg, "start_date:") {
		return "", false
	}
	return "Invalid person data: " + msg, true
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

	// This is the deliberately uncached Monte Carlo re-roll: it always
	// returns the full analysis, never the fast path, so pendingHash is "".
	renderWhatIfResults(w, settings, analysis, "")
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

	hadSchedule := len(settings.RothConversion.PerYearOverrides) > 0

	// Parse enabled checkbox (unchecked means not present in form)
	settings.RothConversion.Enabled = r.FormValue("enabled") == "on"

	// Parse numeric fields
	if amount, err := parseFormFloat(r, "annual_amount"); err == nil {
		if amount < 0 {
			renderError(w, "Annual conversion amount cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.AnnualAmount = amount
		settings.RothConversion.PerYearOverrides = nil
	}

	if startYear, err := parseFormInt(r, "start_year"); err == nil {
		if startYear < 0 {
			renderError(w, "Start year cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.StartYear = startYear
		settings.RothConversion.PerYearOverrides = nil
	}

	if endYear, err := parseFormInt(r, "end_year"); err == nil {
		if endYear < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		settings.RothConversion.EndYear = endYear
		settings.RothConversion.PerYearOverrides = nil
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
		saveAndRenderConversionSweep(w, r, settings, hadSchedule)
		return
	}

	// Replacing a saved schedule must also replace the card's schedule view.
	if hadSchedule && len(settings.RothConversion.PerYearOverrides) == 0 {
		revision, err := retirementMgr.SaveWithRevision(settings)
		if err != nil {
			renderError(w, "Failed to save fixed conversions: "+err.Error(), statusForMutationError(err))
			return
		}
		redirectAfterRothChange(w, revision, "roth-fixed-applied")
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
		// R-EXACT': the form carries COLA x100 (formatExactScaled, 15
		// significant digits, e.g. "1.45"); dividing a parsed float by 100
		// is NOT guaranteed bit-identical to parsing the equivalent shifted
		// decimal directly (IEEE division rounds relative to the ALREADY-
		// rounded dividend, not the true decimal) — a sweep of 0.0001-step
		// percentages found ~26% disagree at the last bit. Rounding the
		// quotient to 15 significant digits (comfortably more than any
		// stored COLA needs) removes that drift in every case tested, so a
		// stored COLA with <=15 significant digits round-trips exactly
		// through render -> submit -> save.
		settings.SocialSecurity.COLARate = roundToSignificantDigits(colaRate/100.0, 15)
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
		// D6: the checkbox no longer auto-submits on a bare tick (it only
		// reveals the fields client-side; see toggleGlidePathFields in
		// whatif-rate-assumptions.js) -- only the "Apply Glide Path" button
		// posts enabled=on. This is the server-side backstop for that
		// contract: an enable with any of the three fields missing is
		// rejected rather than silently keeping (or, via parseFormFloat's
		// blank-parses-as-0 behavior, zeroing) whatever GlidePath config is
		// already on disk -- including the live plan's leftover
		// {enabled:false, start 0, end 0, transition_years 1}. Reactivating
		// THAT unedited runs 0% stocks after year 1 (reviewer: End Balance
		// $3.05M -> $0.71M).
		if strings.TrimSpace(r.FormValue("start_stock_pct")) == "" ||
			strings.TrimSpace(r.FormValue("end_stock_pct")) == "" ||
			strings.TrimSpace(r.FormValue("transition_years")) == "" {
			renderError(w, "Start %, End %, and Years are all required to apply a glide path", http.StatusBadRequest)
			return
		}

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

		if value, present := r.PostForm["min_monthly_spending_real"]; present && len(value) > 0 {
			floor, err := strconv.ParseFloat(value[0], 64)
			if err != nil || math.IsNaN(floor) || math.IsInf(floor, 0) || floor < 0 {
				renderError(w, "Minimum monthly living spending must be finite and at least zero.", http.StatusBadRequest)
				return
			}
			settings.Guardrails.MinMonthlySpendingReal = floor
		}
		minPct := 50.0
		if settings.Guardrails.MinMonthlySpendingReal > 0 {
			minPct = 0
		}
		applyClampedFloatFields(r, []clampedFloatField{
			{"floor_drop_pct", 1, 50, &settings.Guardrails.FloorDropPct},
			{"floor_cut_pct", 0, 50, &settings.Guardrails.FloorCutPct},
			{"ceiling_rise_pct", 1, 100, &settings.Guardrails.CeilingRisePct},
			{"ceiling_raise_pct", 0, 50, &settings.Guardrails.CeilingRaisePct},
			{"min_spending_pct", minPct, 100, &settings.Guardrails.MinSpendingPct},
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
// HTMX recalc path because the optimizer's cost (~10 s on real data once
// Monte Carlo ranking is included; the deterministic pass alone was
// ~38 ms) is far too high for interactive slider drags. The button that
// triggers it shows a spinner and disables itself for the duration.
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
