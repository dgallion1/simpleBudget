package whatif

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// parseNamedAmount parses the name + amount fields shared by the Add forms.
// requiredMsg is the noun-specific "... name is required" message. A
// non-empty errMsg is the user-facing 400 body.
func parseNamedAmount(r *http.Request, requiredMsg string) (name string, amount float64, errMsg string) {
	name = strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return "", 0, requiredMsg
	}
	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		return "", 0, err.Error()
	}
	if amount < 0 {
		return "", 0, "Amount cannot be negative"
	}
	return name, amount, ""
}

// parseYearRange parses start_year and the optional end_year with the
// non-negative and ordering rules shared by the income/expense forms.
func parseYearRange(r *http.Request) (startYear int, endYear *int, errMsg string) {
	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		return 0, nil, "Invalid start year: " + err.Error()
	}
	if startYear < 0 {
		return 0, nil, "Start year cannot be negative"
	}
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			return 0, nil, "Invalid end year: " + err.Error()
		}
		if ey < 0 {
			return 0, nil, "End year cannot be negative"
		}
		if ey < startYear {
			return 0, nil, "End year cannot be before start year"
		}
		endYear = &ey
	}
	return startYear, endYear, ""
}

// checkboxOn reports whether a checkbox-style form field is set ("on" from a
// plain checkbox, "true" from a hidden-input pattern).
func checkboxOn(r *http.Request, key string) bool {
	v := r.FormValue(key)
	return v == "on" || v == "true"
}

func handleWhatIfAddIncome(w http.ResponseWriter, r *http.Request) {
	const target = "#whatif-add-income-error"
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, target)
		return
	}
	name, amount, msg := parseNamedAmount(r, "Income source name is required")
	if msg != "" {
		renderRetargetedError(w, msg, http.StatusBadRequest, target)
		return
	}
	startYear, endYear, msg := parseYearRange(r)
	if msg != "" {
		renderRetargetedError(w, msg, http.StatusBadRequest, target)
		return
	}

	source := models.IncomeSource{
		ID:         uuid.New().String(),
		Name:       name,
		Amount:     amount,
		Type:       models.IncomeFixed,
		StartMonth: startYear * 12,
		COLARate:   0,
	}
	if checkboxOn(r, "cola") {
		source.COLARate = 0.02 // 2% COLA
	}
	if endYear != nil {
		endMonth := *endYear * 12
		source.EndMonth = &endMonth
	}

	recalcAndRender(w, r, "Failed to add income source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.AddIncomeSource(source)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfUpdateIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	startYear, endYear, msg := parseYearRange(r)
	if msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}

	colaRate := 0.0
	if checkboxOn(r, "cola") {
		colaRate = 0.02 // 2% COLA
	}

	recalcAndRender(w, r, "Failed to update income source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.UpdateIncomeSource(id, startYear, endYear, colaRate)
		return settings, revisionUnreported, err
	})
}

// justRemovedIncome is the Undo toast's display-only signal: the ID/Name of
// an income source removed in THIS request. It is deliberately NOT read
// from settings.RemovedIncomeSources (the persistent removed-sources store,
// which can carry stale entries from past sessions or unrelated removals)
// -- see justRemovedIncomeExtra and ruling U-2026-09-05o. Never persisted;
// exists only in the response's template data.
type justRemovedIncome struct {
	ID   string
	Name string
}

// justRemovedIncomeExtra returns the renderRecalcWithExtra "extra" map for a
// successful income removal: {"JustRemovedIncome": &justRemovedIncome{...}}
// for the source RemoveIncomeSource just moved into RemovedIncomeSources.
// The toast template keys ONLY on this signal (never on the persistent
// list), so every OTHER mutation's response -- including the Undo/restore
// response itself -- renders the toast hidden by simply never setting this
// key (ruling U-2026-09-05o).
//
// Looks the id up in settings.RemovedIncomeSources (where RemoveIncomeSource
// just placed it) rather than trusting the pre-mutation form/URL value, so
// the toast's Name always matches what was actually removed. Returns nil
// (no signal, toast stays hidden) if the id is somehow not found there --
// degrading to "no toast" rather than a wrong one.
func justRemovedIncomeExtra(settings *models.WhatIfSettings, id string) map[string]interface{} {
	for _, src := range settings.RemovedIncomeSources {
		if src.ID == id {
			return map[string]interface{}{
				"JustRemovedIncome": &justRemovedIncome{ID: src.ID, Name: src.Name},
			}
		}
	}
	return nil
}

func handleWhatIfDeleteIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	settings, err := retirementMgr.RemoveIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to remove income source: "+err.Error(), statusForMutationError(err))
		return
	}
	renderRecalcWithExtra(w, r, settings, revisionUnreported, justRemovedIncomeExtra(settings, id))
}

func handleWhatIfRestoreIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to restore income source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RestoreIncomeSource(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfAddExpense(w http.ResponseWriter, r *http.Request) {
	const target = "#whatif-add-expense-error"
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, target)
		return
	}
	name, amount, msg := parseNamedAmount(r, "Expense name is required")
	if msg != "" {
		renderRetargetedError(w, msg, http.StatusBadRequest, target)
		return
	}
	startYear, endYear, msg := parseYearRange(r)
	if msg != "" {
		renderRetargetedError(w, msg, http.StatusBadRequest, target)
		return
	}

	source := models.ExpenseSource{
		ID:            uuid.New().String(),
		Name:          name,
		Amount:        amount,
		StartYear:     startYear,
		EndYear:       0, // Default to perpetual
		Inflation:     checkboxOn(r, "inflation"),
		Discretionary: checkboxOn(r, "discretionary"),
	}
	if endYear != nil {
		source.EndYear = *endYear
	}

	recalcAndRender(w, r, "Failed to add expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.AddExpenseSource(source)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfUpdateExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	startYear, endYear, msg := parseYearRange(r)
	if msg != "" {
		renderError(w, msg, http.StatusBadRequest)
		return
	}

	inflation := checkboxOn(r, "inflation")
	discretionary := checkboxOn(r, "discretionary")

	recalcAndRender(w, r, "Failed to update expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.UpdateExpenseSource(id, startYear, endYear, inflation, discretionary)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfDeleteExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to remove expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RemoveExpenseSource(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfRestoreExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to restore expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RestoreExpenseSource(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfAddBigTicket(w http.ResponseWriter, r *http.Request) {
	const target = "#whatif-add-bigticket-error"
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, target)
		return
	}
	name, amount, msg := parseNamedAmount(r, "Big ticket item name is required")
	if msg != "" {
		renderRetargetedError(w, msg, http.StatusBadRequest, target)
		return
	}

	year, err := parseFormInt(r, "year")
	if err != nil {
		renderRetargetedError(w, "Invalid year: "+err.Error(), http.StatusBadRequest, target)
		return
	}
	if year < 0 {
		renderRetargetedError(w, "Year cannot be negative", http.StatusBadRequest, target)
		return
	}

	itemType := models.BigTicketType(r.FormValue("type"))
	if itemType != models.BigTicketIncome && itemType != models.BigTicketExpense {
		itemType = models.BigTicketExpense
	}

	taxTreatment := models.TaxTreatment(r.FormValue("tax_treatment"))
	if taxTreatment != models.TaxNone && taxTreatment != models.TaxOrdinary && taxTreatment != models.TaxCapGains {
		taxTreatment = models.TaxNone
	}

	item := models.BigTicketItem{
		ID:           uuid.New().String(),
		Name:         name,
		Amount:       amount,
		Year:         year,
		Type:         itemType,
		TaxTreatment: taxTreatment,
		Notes:        r.FormValue("notes"),
	}

	recalcAndRender(w, r, "Failed to add big ticket item", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.AddBigTicketItem(item)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfDeleteBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to remove big ticket item", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RemoveBigTicketItem(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfRestoreBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to restore big ticket item", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RestoreBigTicketItem(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfAddOneTime(w http.ResponseWriter, r *http.Request) {
	const target = "#whatif-add-onetime-error"
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, target)
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		renderRetargetedError(w, "Description is required", http.StatusBadRequest, target)
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderRetargetedError(w, err.Error(), http.StatusBadRequest, target)
		return
	}
	if amount < 0 {
		renderRetargetedError(w, "Amount cannot be negative", http.StatusBadRequest, target)
		return
	}

	year, err := parseFormInt(r, "year")
	if err != nil {
		renderRetargetedError(w, "Invalid year: "+err.Error(), http.StatusBadRequest, target)
		return
	}
	if year < 0 {
		renderRetargetedError(w, "Year cannot be negative", http.StatusBadRequest, target)
		return
	}

	expense := models.OneTimeExpense{
		ID:          uuid.New().String(),
		Description: description,
		Year:        year,
		Amount:      amount,
	}

	// Validate the would-be new list against the invariants prepare.From
	// enforces on every recalc (malformed Amount/Year) BEFORE persisting, so a
	// bad row can never reach storage.
	current, err := retirementMgr.Load()
	if err != nil {
		renderRetargetedError(w, "Failed to add one-time expense: "+err.Error(), http.StatusInternalServerError, target)
		return
	}
	current.OneTimeExpenses = append(current.OneTimeExpenses, expense)
	if err := prepare.ValidateOneTimeExpenses(current); err != nil {
		renderRetargetedError(w, "Failed to add one-time expense: "+err.Error(), http.StatusBadRequest, target)
		return
	}

	// Beyond-horizon is a handler-only rejection, not a shared-validator rule:
	// at add time it's almost certainly a typo, so we reject it here for a
	// friendly UX message. But the invariant must not live in
	// ValidateOneTimeExpenses, because every other write path (settings
	// shrinking ProjectionYears, MCP apply_changes) runs that validator too,
	// and an existing entry going out of horizon there is a legitimate,
	// non-fatal user action (the engine treats it as dormant), not an error.
	if year >= current.ProjectionYears {
		renderRetargetedError(w, fmt.Sprintf("Year %d is beyond the %d-year projection horizon", year, current.ProjectionYears), http.StatusBadRequest, target)
		return
	}

	recalcAndRender(w, r, "Failed to add one-time expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.AddOneTimeExpense(expense)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfDeleteOneTime(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to remove one-time expense", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RemoveOneTimeExpense(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfPurgeIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to purge income source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.PurgeRemovedIncomeSource(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfPurgeExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to purge expense source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.PurgeRemovedExpenseSource(id)
		return settings, revisionUnreported, err
	})
}

func handleWhatIfPurgeBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to purge big ticket item", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.PurgeRemovedBigTicketItem(id)
		return settings, revisionUnreported, err
	})
}
