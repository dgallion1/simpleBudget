package whatif

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
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

func handleWhatIfDeleteIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	recalcAndRender(w, r, "Failed to remove income source", func() (*models.WhatIfSettings, int, error) {
		settings, err := retirementMgr.RemoveIncomeSource(id)
		return settings, revisionUnreported, err
	})
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
