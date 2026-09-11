package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCurrentMonthFormToggleAndRender(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, enabled := range []bool{true, false} {
		vals := url.Values{
			"start_date": {"2020-01"}, "person_id[]": {"primary"},
			"person_name[]": {"Alex"}, "person_birth_month[]": {"1960-09"}, "person_role[]": {"primary"},
		}
		if enabled {
			vals.Set("use_current_month", "true")
		}
		req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(vals))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("save: %d: %s", w.Code, w.Body.String())
		}
		s, err := rm.Load()
		if err != nil {
			t.Fatal(err)
		}
		want := "2020-01"
		if enabled {
			want = time.Now().Format("2006-01")
		}
		if s.StartDate != want || s.UseCurrentMonth != enabled {
			t.Fatalf("toggle %v: date %s mode %v", enabled, s.StartDate, s.UseCurrentMonth)
		}
		out, err := renderer.RenderToString("whatif-rate-assumptions", map[string]interface{}{"Settings": s})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Use current month") || strings.Contains(out, "readonly") != enabled {
			t.Fatal("form does not reflect selected mode")
		}
	}
}
