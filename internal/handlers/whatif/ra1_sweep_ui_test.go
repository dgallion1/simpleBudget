package whatif

import (
	"budget2/internal/models"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRA1AdversarialSweepRefresh(t *testing.T) {
	rm, done := setupTestEnvWithRenderer(t)
	defer done()
	s := sweepScenarioSettings()
	s.RothConversion = &models.RothConversionConfig{Enabled: true, EndYear: 3, PerYearOverrides: map[int]float64{0: 12345.67, 1: 42}}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	card := httptest.NewRecorder()
	if err := renderer.RenderPartial(card, "whatif-roth-conversion", map[string]any{"Settings": s}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(card.Body.String(), "Saved year-by-year") {
		t.Fatal("fixture lacks schedule")
	}
	savedRenderer := renderer
	renderer = nil
	form := sweepApplyForm(t)
	renderer = savedRenderer
	w := submitSweepForm(form)
	saved, _ := rm.Load()
	if saved.RothConversion.PerYearOverrides != nil {
		t.Fatal("not cleared")
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("HX-Redirect") == "" && !strings.Contains(w.Body.String(), "roth-conversion-card") {
		t.Fatalf("saved fixed %v but response has no redirect/card replacement; trigger=%s", saved.RothConversion.AnnualAmount, w.Header().Get("HX-Trigger"))
	}
}
