package whatif

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
)

func TestConversionSweepRenderedGuardsRoundTrip(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleWhatIfConversionSweep(w, httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sweep status %d", w.Code)
	}
	forms := regexp.MustCompile("(?s)<form[^>]*>.*?</form>").FindAllString(w.Body.String(), -1)
	if len(forms) == 0 {
		t.Fatal("no Apply forms rendered")
	}
	for _, f := range forms {
		values := url.Values{}
		for _, m := range regexp.MustCompile("<input[^>]*name=\"([^\"]+)\"[^>]*value=\"([^\"]*)\"").FindAllStringSubmatch(f, -1) {
			values.Set(m[1], html.UnescapeString(m[2]))
		}
		if values.Get("expected_scenario") != rm.ActiveFilename() || values.Get("expected_revision") == "" {
			t.Fatalf("Apply form omitted snapshot identity: %s", f)
		}
		if values.Get("annual_amount") != "75000" {
			continue
		}
		result := submitSweepForm(values)
		if result.Code != http.StatusOK {
			t.Fatalf("rendered Apply status %d: %s", result.Code, result.Body.String())
		}
		saved, err := rm.Load()
		if err != nil {
			t.Fatal(err)
		}
		if saved.RothConversion.AnnualAmount != 75000 {
			t.Fatal("rendered form did not save its displayed amount")
		}
		return
	}
	t.Fatal("no 75000 Apply form rendered")
}
