package whatif

import (
	"budget2/internal/models"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRothRecommendationApplyExactAndGuards(t *testing.T) {
	for _, mode := range []string{"schedule", "none", "fixed", "stale", "invalid", "expired"} {
		t.Run(mode, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, _, err := rm.LoadContextWithRevision(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			s.SocialSecurity = &models.SocialSecurityConfig{ClaimAge: 67, SpouseClaimAge: 62, FRABenefit: 2000, FRA: 67}
			if err := rm.Save(s); err != nil {
				t.Fatal(err)
			}
			s, rev, err := rm.LoadContextWithRevision(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(s)
			roth := &models.RothConversionConfig{Enabled: true, EndYear: 3, PerYearOverrides: map[int]float64{0: 12345.67, 1: 0, 2: 67890.12, 3: 42}}
			if mode == "none" {
				roth = &models.RothConversionConfig{}
			}
			if mode == "fixed" {
				roth = &models.RothConversionConfig{Enabled: true, AnnualAmount: 12345.67, EndYear: 3}
			}
			token, err := cacheRothRecommendation(rm, rm.ActiveFilename(), rev, s, roth, 70, 67)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invalid" {
				token = "forged"
			}
			if mode == "expired" {
				rothRecommendations.Lock()
				entry := rothRecommendations.entries[token]
				entry.expires = time.Now().Add(-time.Minute)
				rothRecommendations.entries[token] = entry
				rothRecommendations.Unlock()
			}
			if mode == "stale" {
				s.MonthlyLivingExpenses += 123
				if err := rm.Save(s); err != nil {
					t.Fatal(err)
				}
				before, _ = json.Marshal(s)
			}
			req := httptest.NewRequest("POST", "/whatif/roth-recommendations/apply", strings.NewReader(url.Values{"recommendation": {token}, "annual_amount": {"1"}, "primary_age": {"62"}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleApplyRothRecommendation(w, req)
			got, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "stale" || mode == "invalid" || mode == "expired" {
				after, _ := json.Marshal(got)
				if string(before) != string(after) {
					t.Fatal("rejected recommendation mutated settings")
				}
				if w.Code < 400 {
					t.Fatalf("expected rejection: %d %s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
				t.Fatalf("apply response: %d %s", w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(got.RothConversion, roth) {
				t.Fatalf("config mismatch: %#v", got.RothConversion)
			}
			if got.SocialSecurity.ClaimAge != 70 || got.SocialSecurity.SpouseClaimAge != 67 {
				t.Fatal("SS pair not saved")
			}
			got.RothConversion = s.RothConversion
			got.SocialSecurity = s.SocialSecurity
			after, _ := json.Marshal(got)
			if string(before) != string(after) {
				t.Fatal("unrelated settings mutated")
			}
		})
	}
}

func TestRothRecommendationCardRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.RothConversion = &models.RothConversionConfig{}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-roth-conversion", map[string]any{"Settings": s}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "Find recommendations") {
		t.Fatal("disabled card has no recommendations action")
	}
}
