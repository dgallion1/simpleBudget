package whatif

import (
	"encoding/json"
	"net/http"
	"path/filepath"
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
