package whatif

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
)

func handleListScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios, err := retirementMgr.ListScenarios()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(scenarios)
}
func handleCreateScenario(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderRetargetedError(w, "Scenario name is required", http.StatusBadRequest, "#whatif-scenario-error")
		return
	}

	if _, err := retirementMgr.CreateScenario(name); err != nil {
		if status := statusForScenarioOperationError(err); status != http.StatusInternalServerError {
			renderRetargetedError(w, err.Error(), status, "#whatif-scenario-error")
			return
		}
		renderError(w, "Failed to create scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
func handleSwitchScenario(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(r.FormValue("filename"))
	if filename == "" {
		renderRetargetedError(w, "Scenario filename is required", http.StatusBadRequest, "#whatif-scenario-error")
		return
	}

	if err := retirementMgr.SwitchScenario(filename); err != nil {
		if status := statusForScenarioOperationError(err); status != http.StatusInternalServerError {
			renderRetargetedError(w, err.Error(), status, "#whatif-scenario-error")
			return
		}
		renderError(w, "Failed to switch scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
func handleDeleteScenario(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(chi.URLParam(r, "filename"))
	if filename == "" {
		renderRetargetedError(w, "Scenario filename is required", http.StatusBadRequest, "#whatif-scenario-error")
		return
	}

	if err := retirementMgr.DeleteScenario(filename); err != nil {
		if status := statusForScenarioOperationError(err); status != http.StatusInternalServerError {
			renderRetargetedError(w, err.Error(), status, "#whatif-scenario-error")
			return
		}
		renderError(w, "Failed to delete scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
func handleRenameScenario(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(chi.URLParam(r, "filename"))
	if filename == "" {
		renderRetargetedError(w, "Scenario filename is required", http.StatusBadRequest, "#whatif-scenario-error")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderRetargetedError(w, "New scenario name is required", http.StatusBadRequest, "#whatif-scenario-error")
		return
	}

	if err := retirementMgr.RenameScenario(filename, name); err != nil {
		if status := statusForScenarioOperationError(err); status != http.StatusInternalServerError {
			renderRetargetedError(w, err.Error(), status, "#whatif-scenario-error")
			return
		}
		renderError(w, "Failed to rename scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
func handleWhatIfUpdateChain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	scenarioFiles := r.Form["chain_scenario[]"]
	ageStrings := r.Form["chain_age[]"]

	if len(scenarioFiles) != len(ageStrings) {
		renderError(w, "Mismatched chain scenario and age counts", http.StatusBadRequest)
		return
	}

	chain := make([]models.ScenarioChainLink, 0, len(scenarioFiles))
	for i := range scenarioFiles {
		if scenarioFiles[i] == "" {
			continue
		}
		age, err := strconv.Atoi(ageStrings[i])
		if err != nil {
			renderError(w, fmt.Sprintf("Invalid age at position %d: %s", i+1, ageStrings[i]), http.StatusBadRequest)
			return
		}
		chain = append(chain, models.ScenarioChainLink{
			ScenarioFilename: scenarioFiles[i],
			TransitionAge:    age,
		})
	}

	sort.Slice(chain, func(i, j int) bool {
		return chain[i].TransitionAge < chain[j].TransitionAge
	})

	// ValidateScenarioChain below reads settings.CurrentAge, so Load's copy
	// must carry the json:"-" fields that a bare prepare.DeepCopy would drop —
	// a zeroed CurrentAge would silently change which transition ages this
	// endpoint accepts. Load uses prepare.Clone for exactly this reason.
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	activeFilename := retirementMgr.ActiveFilename()
	if err := retirementMgr.ValidateScenarioChain(chain, settings, activeFilename); err != nil {
		renderError(w, "Invalid chain: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings.ScenarioChain = chain
	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Full page reload so both the chain card and results update
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
func handleWhatIfDeleteChainLink(w http.ResponseWriter, r *http.Request) {
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		renderError(w, "Invalid index", http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if index < 0 || index >= len(settings.ScenarioChain) {
		renderError(w, "Index out of range", http.StatusBadRequest)
		return
	}

	settings.ScenarioChain = append(settings.ScenarioChain[:index], settings.ScenarioChain[index+1:]...)

	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Full page reload so both the chain card and results update
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
