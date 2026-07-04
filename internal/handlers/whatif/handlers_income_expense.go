package whatif

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
)

func handleWhatIfAddIncome(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, "#whatif-add-income-error")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderRetargetedError(w, "Income source name is required", http.StatusBadRequest, "#whatif-add-income-error")
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderRetargetedError(w, err.Error(), http.StatusBadRequest, "#whatif-add-income-error")
		return
	}
	if amount < 0 {
		renderRetargetedError(w, "Amount cannot be negative", http.StatusBadRequest, "#whatif-add-income-error")
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderRetargetedError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest, "#whatif-add-income-error")
		return
	}
	if startYear < 0 {
		renderRetargetedError(w, "Start year cannot be negative", http.StatusBadRequest, "#whatif-add-income-error")
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderRetargetedError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest, "#whatif-add-income-error")
			return
		}
		if ey < 0 {
			renderRetargetedError(w, "End year cannot be negative", http.StatusBadRequest, "#whatif-add-income-error")
			return
		}
		if ey < startYear {
			renderRetargetedError(w, "End year cannot be before start year", http.StatusBadRequest, "#whatif-add-income-error")
			return
		}
		endYearPtr = &ey
	}

	cola := r.FormValue("cola") == "on" || r.FormValue("cola") == "true"

	source := models.IncomeSource{
		ID:         uuid.New().String(),
		Name:       name,
		Amount:     amount,
		Type:       models.IncomeFixed,
		StartMonth: startYear * 12,
		COLARate:   0,
	}

	if cola {
		source.COLARate = 0.02 // 2% COLA
	}

	if endYearPtr != nil {
		endMonth := *endYearPtr * 12
		source.EndMonth = &endMonth
	}

	settings, err := retirementMgr.AddIncomeSource(source)
	if err != nil {
		renderError(w, "Failed to add income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfUpdateIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

	cola := r.FormValue("cola") == "on" || r.FormValue("cola") == "true"

	colaRate := 0.0
	if cola {
		colaRate = 0.02 // 2% COLA
	}

	settings, err := retirementMgr.UpdateIncomeSource(id, startYear, endYearPtr, colaRate)
	if err != nil {
		renderError(w, "Failed to update income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfDeleteIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to remove income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfRestoreIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to restore income source: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfAddExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderRetargetedError(w, "Expense name is required", http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderRetargetedError(w, err.Error(), http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}
	if amount < 0 {
		renderRetargetedError(w, "Amount cannot be negative", http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderRetargetedError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}
	if startYear < 0 {
		renderRetargetedError(w, "Start year cannot be negative", http.StatusBadRequest, "#whatif-add-expense-error")
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderRetargetedError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest, "#whatif-add-expense-error")
			return
		}
		if ey < 0 {
			renderRetargetedError(w, "End year cannot be negative", http.StatusBadRequest, "#whatif-add-expense-error")
			return
		}
		if ey < startYear {
			renderRetargetedError(w, "End year cannot be before start year", http.StatusBadRequest, "#whatif-add-expense-error")
			return
		}
		endYearPtr = &ey
	}

	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"
	discretionary := r.FormValue("discretionary") == "on" || r.FormValue("discretionary") == "true"

	source := models.ExpenseSource{
		ID:            uuid.New().String(),
		Name:          name,
		Amount:        amount,
		StartYear:     startYear,
		EndYear:       0, // Default to perpetual
		Inflation:     inflation,
		Discretionary: discretionary,
	}
	if endYearPtr != nil {
		source.EndYear = *endYearPtr
	}

	settings, err := retirementMgr.AddExpenseSource(source)
	if err != nil {
		renderError(w, "Failed to add expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfUpdateExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"
	discretionary := r.FormValue("discretionary") == "on" || r.FormValue("discretionary") == "true"

	settings, err := retirementMgr.UpdateExpenseSource(id, startYear, endYearPtr, inflation, discretionary)
	if err != nil {
		renderError(w, "Failed to update expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfDeleteExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveExpenseSource(id)
	if err != nil {
		renderError(w, "Failed to remove expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfRestoreExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreExpenseSource(id)
	if err != nil {
		renderError(w, "Failed to restore expense: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfAddBigTicket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, "#whatif-add-bigticket-error")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderRetargetedError(w, "Big ticket item name is required", http.StatusBadRequest, "#whatif-add-bigticket-error")
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderRetargetedError(w, err.Error(), http.StatusBadRequest, "#whatif-add-bigticket-error")
		return
	}
	if amount < 0 {
		renderRetargetedError(w, "Amount cannot be negative", http.StatusBadRequest, "#whatif-add-bigticket-error")
		return
	}

	year, err := parseFormInt(r, "year")
	if err != nil {
		renderRetargetedError(w, "Invalid year: "+err.Error(), http.StatusBadRequest, "#whatif-add-bigticket-error")
		return
	}
	if year < 0 {
		renderRetargetedError(w, "Year cannot be negative", http.StatusBadRequest, "#whatif-add-bigticket-error")
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

	notes := r.FormValue("notes")

	item := models.BigTicketItem{
		ID:           uuid.New().String(),
		Name:         name,
		Amount:       amount,
		Year:         year,
		Type:         itemType,
		TaxTreatment: taxTreatment,
		Notes:        notes,
	}

	settings, err := retirementMgr.AddBigTicketItem(item)
	if err != nil {
		renderError(w, "Failed to add big ticket item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfDeleteBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to remove big ticket item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
func handleWhatIfRestoreBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to restore big ticket item: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}

func handleWhatIfPurgeIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to purge income source: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}

func handleWhatIfPurgeExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedExpenseSource(id)
	if err != nil {
		renderError(w, "Failed to purge expense source: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}

func handleWhatIfPurgeBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to purge big ticket item: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderWhatIfResults(w, settings, analysis)
}
