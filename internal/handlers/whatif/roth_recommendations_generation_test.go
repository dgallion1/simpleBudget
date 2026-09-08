package whatif

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

func TestRothRecommendationsGenerateReadOnlyAndApplyReload(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 60)
	s.ProjectionYears = 10
	s.PortfolioValue = 1200000
	s.TaxDeferredPercent = 80
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2400, FRA: 67, ClaimAge: 67}
	s.RothConversion = &models.RothConversionConfig{}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(rm.SettingsDir(), rm.ActiveFilename())
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	rev := rm.Revision()
	w := httptest.NewRecorder()
	handleRothRecommendations(w, httptest.NewRequest("POST", "/whatif/roth-recommendations", nil))
	if w.Code != 200 {
		t.Fatalf("generate: %d %s", w.Code, w.Body.String())
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) || rm.Revision() != rev {
		t.Fatal("generation wrote saved plan")
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-roth-revision="`+strconv.Itoa(rev)+`"`) {
		t.Fatalf("rendered recommendations lost generating revision %d", rev)
	}
	for _, text := range []string{"Roth and Social Security recommendations", "Apply Roth + SS", "MC median ending", "primary age"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing %q", text)
		}
	}
	tokens := regexp.MustCompile(`name="recommendation" value="([a-f0-9]+)"`).FindAllStringSubmatch(body, -1)
	if len(tokens) < 2 {
		t.Fatalf("expected ranked selections: %s", body)
	}
	token := tokens[0][1]
	rothRecommendations.Lock()
	expected := rothRecommendations.entries[token]
	rothRecommendations.Unlock()
	req := httptest.NewRequest("POST", "/whatif/roth-recommendations/apply", strings.NewReader(url.Values{"recommendation": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applied := httptest.NewRecorder()
	handleApplyRothRecommendation(applied, req)
	if !strings.Contains(applied.Header().Get("HX-Redirect"), "#roth-recommendation-applied") {
		t.Fatalf("apply: %d %s", applied.Code, applied.Body.String())
	}
	rm.InvalidateCache()
	got, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.RothConversion, &expected.roth) {
		t.Fatalf("disk reload changed config: %#v want %#v", got.RothConversion, expected.roth)
	}
	if got.SocialSecurity.ClaimAge != expected.primary || got.SocialSecurity.SpouseClaimAge != expected.spouse {
		t.Fatal("disk reload changed SS")
	}
	var disk models.WhatIfSettings
	raw, _ := os.ReadFile(file)
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(disk.RothConversion, &expected.roth) {
		t.Fatal("disk does not contain exact config")
	}
}

func TestRothRecommendationCacheBoundedAndInstanceLocal(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rothRecommendationLimit+2; i++ {
		if _, err := cacheRothRecommendation(rm, rm.ActiveFilename(), rm.Revision(), s, &models.RothConversionConfig{}, 67, 0); err != nil {
			t.Fatal(err)
		}
	}
	rothRecommendations.Lock()
	count := len(rothRecommendations.entries)
	rothRecommendations.Unlock()
	if count > rothRecommendationLimit {
		t.Fatalf("unbounded cache: %d", count)
	}
	token, err := cacheRothRecommendation(rm, rm.ActiveFilename(), rm.Revision(), s, &models.RothConversionConfig{}, 67, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, cleanup2 := setupTestEnv(t)
	defer cleanup2()
	before, _ := other.Load()
	rawBefore, _ := json.Marshal(before)
	req := httptest.NewRequest("POST", "/whatif/roth-recommendations/apply", strings.NewReader(url.Values{"recommendation": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleApplyRothRecommendation(w, req)
	after, _ := other.Load()
	rawAfter, _ := json.Marshal(after)
	if w.Code != 409 || string(rawBefore) != string(rawAfter) {
		t.Fatal("cross-instance token accepted or mutated settings")
	}
	// Expiration pruning occurs before insertion, independent of Apply.
	rothRecommendations.Lock()
	for key, entry := range rothRecommendations.entries {
		entry.expires = time.Now().Add(-time.Second)
		rothRecommendations.entries[key] = entry
	}
	rothRecommendations.Unlock()
	if _, err := cacheRothRecommendation(other, other.ActiveFilename(), other.Revision(), after, &models.RothConversionConfig{}, 67, 0); err != nil {
		t.Fatal(err)
	}
	rothRecommendations.Lock()
	count = len(rothRecommendations.entries)
	rothRecommendations.Unlock()
	if count != 1 {
		t.Fatalf("expired entries retained: %d", count)
	}
}
