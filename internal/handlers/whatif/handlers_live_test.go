package whatif

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
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

func postApply(t *testing.T, body string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handleWhatIfApply(w, req)
	return w.Result()
}

func TestHandleWhatIfApply_PersistsAndBumpsRevision(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	before := rm.Revision()
	resp := postApply(t, `{"monthly_living_expenses": 4200}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got struct {
		Scenario string `json:"scenario"`
		Revision int    `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Revision <= before {
		t.Fatalf("revision did not advance: %d -> %d", before, got.Revision)
	}
	if got.Scenario == "" {
		t.Error("scenario is empty")
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.MonthlyLivingExpenses != 4200 {
		t.Fatalf("value did not persist: %v", settings.MonthlyLivingExpenses)
	}
}

// The trap that ruled out posting /whatif/roth-conversion: that handler reads
// Enabled from a checkbox, so a partial post disables the conversions it was
// meant to size.
func TestHandleWhatIfApply_PositiveRothAmountLeavesConversionsEnabled(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 25000, StartYear: 1, EndYear: 10}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"roth_conversion_amount": 50000}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.RothConversion == nil || !after.RothConversion.Enabled {
		t.Fatal("a positive amount must leave conversions enabled")
	}
	if after.RothConversion.AnnualAmount != 50000 {
		t.Fatalf("amount = %v, want 50000", after.RothConversion.AnnualAmount)
	}
	if after.RothConversion.StartYear != 1 || after.RothConversion.EndYear != 10 {
		t.Fatalf("the window was clobbered: %+v", after.RothConversion)
	}
}

// The documented-but-surprising semantics: amount 0 DISABLES. Asserting it so
// the behavior is pinned rather than discovered.
func TestHandleWhatIfApply_ZeroRothAmountDisablesConversions(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 25000}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"roth_conversion_amount": 0}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.RothConversion != nil && after.RothConversion.Enabled {
		t.Fatal("amount 0 must disable conversions -- see overrides.go Apply")
	}
}

// The trap that ruled out posting /whatif/social-security: that handler nils
// the entire config when FRABenefit <= 0, which a partial post always triggers.
func TestHandleWhatIfApply_SpouseClaimAgeLeavesSocialSecurityIntact(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 67, SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"spouse_claim_age": 65}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.SocialSecurity == nil {
		t.Fatal("the Social Security config was deleted by a partial write")
	}
	if after.SocialSecurity.SpouseClaimAge != 65 {
		t.Fatalf("spouse claim age = %d, want 65", after.SocialSecurity.SpouseClaimAge)
	}
	if after.SocialSecurity.ClaimAge != 67 {
		t.Fatalf("the primary claim age was reset to %d", after.SocialSecurity.ClaimAge)
	}
	if after.SocialSecurity.FRABenefit != 3000 {
		t.Fatalf("FRABenefit was reset to %v", after.SocialSecurity.FRABenefit)
	}
}

func TestHandleWhatIfApply_RejectsUnwritableAndInvalid(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	for _, tc := range []struct{ name, body, wantIn string }{
		{"healthcare_inflation", `{"healthcare_inflation": 6}`, "healthcare_inflation"},
		{"absurd return", `{"investment_return": 900}`, "investment_return"},
		{"claim age", `{"social_security_claim_age": 40}`, "social_security_claim_age"},
		{"roth window only", `{"roth_conversion_start_year": 2}`, "roth_conversion_amount"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postApply(t, tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tc.wantIn) {
				t.Fatalf("error %q does not name %q", body, tc.wantIn)
			}
		})
	}
}

func TestHandleWhatIfApply_RejectsMalformedJSON(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	if resp := postApply(t, `{"monthly_living_expenses":`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
