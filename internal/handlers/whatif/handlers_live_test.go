package whatif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleWhatIfState_ReportsIdentityAndState(t *testing.T) {
	_, settingsDir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfState(w, httptest.NewRequest("GET", "/whatif/state", nil))

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got struct {
		App         string `json:"app"`
		SettingsDir string `json:"settings_dir"`
		Active      string `json:"active"`
		Revision    int    `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.App != "budget2" {
		t.Errorf("app = %q, want budget2", got.App)
	}
	if !filepath.IsAbs(got.SettingsDir) {
		t.Errorf("settings_dir %q is not absolute", got.SettingsDir)
	}
	wantAbs, _ := filepath.Abs(settingsDir)
	if got.SettingsDir != wantAbs {
		t.Errorf("settings_dir = %q, want %q", got.SettingsDir, wantAbs)
	}
	if got.Active == "" {
		t.Error("active is empty")
	}
}
