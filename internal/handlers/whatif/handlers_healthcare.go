package whatif

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
	"budget2/internal/services/retirement/completeness"
)

func handleWhatIfAddHealthcare(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderRetargetedError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}

	// Load is outside the AddHealthcarePerson write lock, so there is a
	// small TOCTOU window.  saveInternal's ComputeAges re-derives linked
	// name/age, and ValidatePersons catches orphaned links, so correctness
	// is preserved even if the settings change between this read and the
	// subsequent write.
	settingsState, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	personID := strings.TrimSpace(r.FormValue("person_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	age := 0
	if personID != "" {
		person := settingsState.FindPerson(personID)
		if person == nil {
			renderRetargetedError(w, "Selected person was not found", http.StatusBadRequest, "#whatif-add-healthcare-error")
			return
		}
		name = person.Name
		age = settingsState.PersonAge(personID)
	} else {
		if name == "" {
			name = "Person"
		}
		if strings.TrimSpace(r.FormValue("current_age")) != "" {
			age, err = parseFormInt(r, "current_age")
			if err != nil {
				renderRetargetedError(w, "Invalid age: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
				return
			}
		}
		if age == 0 {
			age = 65
		}
	}

	coverageType := r.FormValue("current_coverage")
	monthlyCost, err := parseFormFloat(r, "current_monthly_cost")
	if err != nil {
		renderRetargetedError(w, "Invalid monthly cost: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}
	if monthlyCost < 0 {
		renderRetargetedError(w, "Monthly cost cannot be negative", http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}
	preMedicareInflation, err := parseFormFloat(r, "pre_medicare_inflation")
	if err != nil {
		renderRetargetedError(w, "Invalid pre-Medicare inflation: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}
	medicareCost, err := parseFormFloat(r, "medicare_monthly_cost")
	if err != nil {
		renderRetargetedError(w, "Invalid Medicare cost: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}
	postMedicareInflation, err := parseFormFloat(r, "post_medicare_inflation")
	if err != nil {
		renderRetargetedError(w, "Invalid post-Medicare inflation: "+err.Error(), http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}

	if age < 0 || age > 120 {
		renderRetargetedError(w, "Age must be between 0 and 120", http.StatusBadRequest, "#whatif-add-healthcare-error")
		return
	}
	if coverageType == "" {
		if age >= 65 {
			coverageType = string(models.CoverageMedicare)
		} else {
			coverageType = string(models.CoverageACA)
		}
	}
	if monthlyCost == 0 {
		if coverageType == string(models.CoverageMedicare) {
			monthlyCost = 459
		} else {
			monthlyCost = 1100
		}
	}
	if preMedicareInflation == 0 {
		preMedicareInflation = 7.0
	}
	if medicareCost == 0 {
		medicareCost = 600
	}
	if postMedicareInflation == 0 {
		postMedicareInflation = 4.0
	}

	person := models.HealthcarePerson{
		ID:                    uuid.New().String(),
		Name:                  name,
		PersonID:              personID,
		CurrentAge:            age,
		CurrentCoverage:       models.CoverageType(coverageType),
		CurrentMonthlyCost:    monthlyCost,
		PreMedicareInflation:  preMedicareInflation,
		MedicareMonthlyCost:   medicareCost,
		PostMedicareInflation: postMedicareInflation,
		MedicareEligibleAge:   65,
	}

	settings, err := retirementMgr.AddHealthcarePerson(person)
	if err != nil {
		renderError(w, "Failed to add healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := buildResultsPartialData(settings, analysis, completeness.Check(settings))

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
func handleWhatIfUpdateHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Load is outside the UpdateHealthcarePerson write lock, so there is a
	// small TOCTOU window.  saveInternal's ComputeAges re-derives linked
	// name/age, and ValidatePersons catches orphaned links, so correctness
	// is preserved even if the settings change between this read and the
	// subsequent write.
	settingsState, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	existing := findHealthcarePerson(settingsState, id)

	updates := make(map[string]interface{})

	personIDSpecified := formHasKey(r, "person_id")
	if personIDSpecified {
		personID := strings.TrimSpace(r.FormValue("person_id"))
		updates["person_id"] = personID
		if personID != "" {
			person := settingsState.FindPerson(personID)
			if person == nil {
				renderError(w, "Selected person was not found", http.StatusBadRequest)
				return
			}
			updates["name"] = person.Name
			updates["current_age"] = settingsState.PersonAge(personID)
		} else {
			name := strings.TrimSpace(r.FormValue("name"))
			if name == "" {
				renderError(w, "Name is required when unlinking a healthcare entry", http.StatusBadRequest)
				return
			}
			updates["name"] = name

			ageValue := strings.TrimSpace(r.FormValue("current_age"))
			if ageValue == "" {
				renderError(w, "Age is required when unlinking a healthcare entry", http.StatusBadRequest)
				return
			}
			i, err := strconv.Atoi(ageValue)
			if err != nil {
				renderError(w, "Invalid age: must be an integer", http.StatusBadRequest)
				return
			}
			if i < 0 || i > 120 {
				renderError(w, "Age must be between 0 and 120", http.StatusBadRequest)
				return
			}
			updates["current_age"] = i
		}
	} else if existing == nil || !existing.IsLinked() {
		if v := r.FormValue("name"); v != "" {
			updates["name"] = v
		}
		if v := r.FormValue("current_age"); v != "" {
			i, err := strconv.Atoi(v)
			if err != nil {
				renderError(w, "Invalid age: must be an integer", http.StatusBadRequest)
				return
			}
			if i < 0 || i > 120 {
				renderError(w, "Age must be between 0 and 120", http.StatusBadRequest)
				return
			}
			updates["current_age"] = i
		}
	}
	if v := r.FormValue("current_coverage"); v != "" {
		updates["current_coverage"] = v
	}
	if v := r.FormValue("current_monthly_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid monthly cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "Monthly cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["current_monthly_cost"] = f
	}
	if v := r.FormValue("pre_medicare_inflation"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid pre-Medicare inflation: must be a number", http.StatusBadRequest)
			return
		}
		updates["pre_medicare_inflation"] = f
	}
	if v := r.FormValue("medicare_monthly_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid Medicare cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "Medicare cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["medicare_monthly_cost"] = f
	}
	if v := r.FormValue("post_medicare_inflation"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid post-Medicare inflation: must be a number", http.StatusBadRequest)
			return
		}
		updates["post_medicare_inflation"] = f
	}
	if v := r.FormValue("employer_coverage_years"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			renderError(w, "Invalid employer coverage years: must be an integer", http.StatusBadRequest)
			return
		}
		if i < 0 {
			renderError(w, "Employer coverage years cannot be negative", http.StatusBadRequest)
			return
		}
		updates["employer_coverage_years"] = i
	}
	if v := r.FormValue("aca_cost_after_employer"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid ACA cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "ACA cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["aca_cost_after_employer"] = f
	}

	settings, err := retirementMgr.UpdateHealthcarePerson(id, updates)
	if err != nil {
		renderError(w, "Failed to update healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := buildResultsPartialData(settings, analysis, completeness.Check(settings))

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
func handleWhatIfDeleteHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveHealthcarePerson(id)
	if err != nil {
		renderError(w, "Failed to remove healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := buildResultsPartialData(settings, analysis, completeness.Check(settings))

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
