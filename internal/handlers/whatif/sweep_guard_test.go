package whatif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Fetch the actual endpoint envelope: missing or mismatched guard metadata
// must fail the same test that exercises the persistence boundary.
func sweepApplyForm(t *testing.T) url.Values {
	t.Helper()
	w := httptest.NewRecorder()
	handleWhatIfConversionSweep(w, httptest.NewRequest(http.MethodPost, "/whatif/conversion-sweep", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sweep status %d: %s", w.Code, w.Body.String())
	}
	var data struct {
		ExpectedScenario string
		ExpectedRevision int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.ExpectedScenario == "" {
		t.Fatal("sweep omitted scenario identity")
	}
	return url.Values{
		"apply_source":      {"conversion-sweep"},
		"annual_amount":     {"50000"},
		"enabled":           {"on"},
		"start_year":        {"0"},
		"end_year":          {"2"},
		"expected_scenario": {data.ExpectedScenario},
		"expected_revision": {strconv.Itoa(data.ExpectedRevision)},
	}
}

func submitSweepForm(form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/whatif/roth-conversion", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, r)
	return w
}

func TestConversionSweepRejectsStaleScenarioAndRevision(t *testing.T) {
	for _, switchScenario := range []bool{false, true} {
		name := "same scenario edit"
		if switchScenario {
			name = "scenario switch"
		}
		t.Run(name, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s := sweepScenarioSettings()
			if err := rm.Save(s); err != nil {
				t.Fatal(err)
			}
			form := sweepApplyForm(t)
			if switchScenario {
				if _, err := rm.CreateScenario("Other scenario"); err != nil {
					t.Fatal(err)
				}
			}
			edited, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			edited.RothConversion.AnnualAmount = 10000
			edited.DiscountRate = 4.25
			if err := rm.Save(edited); err != nil {
				t.Fatal(err)
			}
			beforeRevision := rm.Revision()
			w := submitSweepForm(form)
			if w.Code != http.StatusConflict {
				t.Fatalf("stale apply status %d, want 409: %s", w.Code, w.Body.String())
			}
			after, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			if after.RothConversion.AnnualAmount != 10000 || after.DiscountRate != 4.25 || rm.Revision() != beforeRevision {
				t.Fatal("stale sweep changed the saved plan")
			}
			if w.Header().Get("HX-Retarget") != "#conversion-sweep-panel" {
				t.Fatal("conflict must be shown in the sweep panel")
			}
		})
	}
}

func TestConversionSweepApplyRequiresGuardsAndRefreshesThem(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	if err := rm.Save(sweepScenarioSettings()); err != nil {
		t.Fatal(err)
	}
	form := sweepApplyForm(t)
	for _, key := range []string{"expected_scenario", "expected_revision"} {
		value := form.Get(key)
		form.Del(key)
		before := rm.Revision()
		if w := submitSweepForm(form); w.Code != http.StatusBadRequest {
			t.Fatalf("missing %s status %d", key, w.Code)
		}
		if rm.Revision() != before {
			t.Fatal("missing guard wrote settings")
		}
		form.Set(key, value)
	}
	form.Set("annual_amount", "75000")
	w := submitSweepForm(form)
	if w.Code != http.StatusOK {
		t.Fatalf("fresh apply status %d: %s", w.Code, w.Body.String())
	}
	var data struct {
		ExpectedScenario string
		ExpectedRevision int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.ExpectedScenario != rm.ActiveFilename() || data.ExpectedRevision != rm.Revision() {
		t.Fatal("applied table has stale guards")
	}
	saved, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.RothConversion.AnnualAmount != 75000 {
		t.Fatal("fresh apply did not persist")
	}
	// A second Apply from the newly rendered table remains usable.
	form.Set("expected_revision", strconv.Itoa(data.ExpectedRevision))
	form.Set("annual_amount", "25000")
	if w := submitSweepForm(form); w.Code != http.StatusOK {
		t.Fatalf("refreshed apply status %d", w.Code)
	}
}
