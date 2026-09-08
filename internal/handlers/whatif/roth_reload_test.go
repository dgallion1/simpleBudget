package whatif

import (
	"budget2/internal/models"
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func assertRothDocumentReload(t *testing.T, w *httptest.ResponseRecorder, fragment string) {
	t.Helper()
	destination, err := url.Parse(w.Header().Get("HX-Redirect"))
	if err != nil {
		t.Fatal(err)
	}
	if destination.Path != "/whatif" || destination.Query().Get("roth_applied") == "" || destination.Fragment != fragment {
		t.Fatalf("must reload the document, not navigate within it: %q", w.Header().Get("HX-Redirect"))
	}
}

func TestRothApplyForcesDocumentReload(t *testing.T) {
	rm, done := setupTestEnv(t)
	defer done()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.SocialSecurity = &models.SocialSecurityConfig{ClaimAge: 67, FRA: 67, FRABenefit: 2000}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	s, rev, err := rm.LoadContextWithRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	token, err := cacheRothRecommendation(rm, rm.ActiveFilename(), rev, s, &models.RothConversionConfig{}, 70, 0)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/whatif/roth-recommendations/apply", strings.NewReader(url.Values{"recommendation": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleApplyRothRecommendation(w, req)
	assertRothDocumentReload(t, w, "roth-recommendation-applied")
}

func TestFixedRothEditForcesDocumentReload(t *testing.T) {
	rm, done := setupTestEnv(t)
	defer done()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.RothConversion = &models.RothConversionConfig{Enabled: true, EndYear: 3, PerYearOverrides: map[int]float64{0: 12345}}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", strings.NewReader("enabled=on&annual_amount=27000&start_year=0&end_year=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, req)
	assertRothDocumentReload(t, w, "roth-fixed-applied")
}
