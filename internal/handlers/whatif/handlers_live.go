package whatif

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"budget2/internal/services/retirement/overrides"
)

// appIdentity is the literal reported by GET /whatif/state. A client compares
// it before writing: /api/health returns only {"status":"ok"} and cannot
// distinguish this server from anything else listening on the same port.
const appIdentity = "budget2"

type stateResponse struct {
	App         string `json:"app"`
	SettingsDir string `json:"settings_dir"`
	Active      string `json:"active"`
	Revision    int    `json:"revision"`
}

// handleWhatIfState reports which plan this server is serving and how many
// times it has changed. The settings directory is absolute so a client can
// compare it against its own resolved path without guessing about relative
// bases -- a mismatch means reads and writes would land on different plans.
func handleWhatIfState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}
	dir, err := filepath.Abs(retirementMgr.SettingsDir())
	if err != nil {
		http.Error(w, "resolving settings directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(stateResponse{
		App:         appIdentity,
		SettingsDir: dir,
		Active:      retirementMgr.ActiveFilename(),
		Revision:    retirementMgr.Revision(),
	})
}

type applyResponse struct {
	Scenario string              `json:"scenario"`
	Applied  overrides.Overrides `json:"applied"`
	Revision int                 `json:"revision"`
}

// applyRequest is the POST /whatif/apply body: the override set, plus the
// required expectation about which scenario is being written.
//
// expected_scenario is what makes the write safe rather than merely audited. A
// client snapshots the scenario the server reports active, then POSTs; if the
// browser switches scenario in between, the write would otherwise land on a
// plan that was never backed up, and there is no undo. Sending the expectation
// moves the comparison inside the manager's write lock, where it can still
// prevent the write. It is required (issue #24): omitting it used to skip the
// guard entirely and made the guarantee an unenforced client convention.
type applyRequest struct {
	overrides.Overrides
	ExpectedScenario string `json:"expected_scenario,omitempty"`
}

// handleWhatIfApply writes a sparse override set to the active scenario.
//
// This exists instead of the MCP posting the existing forms because
// parseFormFloat returns (0, nil) for an absent key, so inside the
// non-spec-driven handlers "field absent" and "field is zero" are
// indistinguishable. A partial post to /whatif/roth-conversion disables
// conversions; one to /whatif/social-security deletes the config outright.
//
// expected_scenario is required: a request with it missing or blank is
// rejected with 400 before any load or write. See applyRequest's doc for why
// the field exists; making it required closes the gap where a caller that
// forgot to send it got an unguarded write instead of a 400.
func handleWhatIfApply(w http.ResponseWriter, r *http.Request) {
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}

	var req applyRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid overrides JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.ExpectedScenario) == "" {
		http.Error(w, "expected_scenario is required: send the active scenario filename "+
			"reported by GET /whatif/state's \"active\" field", http.StatusBadRequest)
		return
	}

	_, scenario, revision, err := retirementMgr.ApplyOverrides(req.Overrides, req.ExpectedScenario)
	if err != nil {
		// Validation errors name their field; surface them verbatim. Save-time
		// failures (ValidatePersons, validateChainInternal) land here too, and
		// fall through to statusForMutationError's 400/404/409/500 mapping --
		// which is also what turns an expected_scenario mismatch
		// (*ScenarioConflictError) into 409 Conflict.
		status := statusForMutationError(err)
		var ve *overrides.ValidationError
		if errors.As(err, &ve) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// scenario comes from ApplyOverrides, read under the lock that wrote it.
	// Reading ActiveFilename() here instead would name whatever is active
	// *now*, which a concurrent switch can make a different file than the one
	// just written -- and a client comparing the two would raise a collision
	// alarm about a write that was in fact fine.
	_ = json.NewEncoder(w).Encode(applyResponse{
		Scenario: scenario,
		Applied:  req.Overrides,
		Revision: revision,
	})
}

// handleWhatIfPoll is the page's change detector. It returns 204 when nothing
// has changed since the caller's baseline -- htmx performs no swap on 204, so
// the common case costs one integer comparison and runs no analysis, which is
// what makes a 2s poll acceptable.
//
// The comparison is inequality, never `revision > since`: the counter is
// in-memory, so a tab held across a server restart has a baseline above the
// fresh counter and must still re-render.
func handleWhatIfPoll(w http.ResponseWriter, r *http.Request) {
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}

	// An absent or malformed `since` must never equal a real revision, or a
	// bad parameter would collide with a fresh counter (revision starts at 0
	// on every server start) and silently suppress a render instead of
	// showing fresh figures. -1 is not a value Revision() ever returns.
	since := -1
	if v, err := strconv.Atoi(r.URL.Query().Get("since")); err == nil {
		since = v
	}

	current := retirementMgr.Revision()
	if since == current {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	analysis, pendingHash, err := analysisFastOrCached(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The client advances its baseline from this header. It cannot come from
	// the body: the response swaps into #whatif-results and never touches the
	// polling element's own attributes.
	trigger, err := json.Marshal(map[string]int{"whatif:revision": current})
	if err == nil {
		w.Header().Set("HX-Trigger", string(trigger))
	}
	renderWhatIfResultsOnly(w, settings, analysis, pendingHash)
}
