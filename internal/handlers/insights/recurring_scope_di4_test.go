package insights

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	svc "budget2/internal/services/insights"
)

// Promoted from TestCheckerDI4RecurringLimitWording in
// /tmp/DI4-checker-tests.xGOWIK/internal/handlers/insights/zz_checker_di4_test.go.
// Keep the real 22-merchant fixture; also assert detector count and every name
// so truncation or duplicated rows cannot make the scope check pass.
func TestCheckerDI4RecurringLimitWording(t *testing.T) {
	names := []string{"Alder", "Birch", "Cedar", "Dogwood", "Elm", "Fir", "Ginkgo", "Hazel", "Ivy", "Juniper", "Kapok", "Larch", "Maple", "Nutmeg", "Oak", "Pine", "Quince", "Rowan", "Spruce", "Teak", "Electric Company", "Netflix"}
	var csv strings.Builder
	csv.WriteString("Date,Description,Amount,Category\n")
	for _, name := range names {
		amount := -100
		if name == "Netflix" {
			amount = -10
		}
		for m := 6; m <= 8; m++ {
			fmt.Fprintf(&csv, "2026-%02d-15,%s,%d,Household\n", m, name, amount)
		}
	}
	cleanup := setupTestLoaderWithRenderer(t, csv.String())
	defer cleanup()
	data, err := loader.LoadData()
	if err != nil {
		t.Fatal(err)
	}
	detected := svc.DetectRecurringAt(data.Active(), di4Date("2026-08-28"))
	if len(detected) != 22 {
		t.Fatalf("real detector returned %d series, want 22", len(detected))
	}

	for _, kind := range []string{"full", "HX", "standalone"} {
		t.Run(kind, func(t *testing.T) {
			path := "/insights"
			if kind == "standalone" {
				path += "/recurring"
			}
			r := httptest.NewRequest("GET", path+"?start=2026-08-01&end=2026-08-28", nil)
			if kind == "HX" {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			if kind == "standalone" {
				handleRecurringPartial(w, r)
			} else {
				handleInsights(w, r)
			}
			if w.Code != 200 {
				t.Fatalf("response status %d, want 200", w.Code)
			}
			body := w.Body.String()
			if kind != "standalone" {
				start := strings.Index(body, "id=\"insights-recurring\"")
				end := strings.Index(body, "id=\"insights-supporting\"")
				if start < 0 || end < start {
					t.Fatal("missing recurring/supporting sections")
				}
				body = body[start:end]
			}
			rows := len(regexp.MustCompile(`<tr>\s*<td`).FindAllString(body, -1))
			if rows != 22 {
				t.Errorf("retained rows=%d, want 22", rows)
			}
			for _, name := range names {
				merchant := "<span>" + strings.ToLower(name) + "</span>"
				if count := strings.Count(body, merchant); count != 1 {
					t.Errorf("merchant %q rendered %d times, want exactly once", name, count)
				}
			}
			t.Logf("real detector=%d; rendered retained rows=%d; checked all 22 merchant names", len(detected), rows)
			if strings.Contains(body, "up to 20 series") || strings.Contains(body, "detector limit: 20 series") {
				t.Errorf("falsely claims detector limit 20 while rendering %d series", rows)
			}
		})
	}
}
