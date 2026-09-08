package insights

import (
	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	insightssvc "budget2/internal/services/insights"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func sv1Date(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }

// SV1: "Review these transactions" lists the largest absolute amount first.
// Sign is ignored (a refund sorts by its magnitude); equal amounts keep the
// previous order (newest date, then hash, then finding type). The rendered
// preview and the "View all" block must follow the same sequence.
func TestSV1FindingsSortedByAbsAmountDesc(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	rows := []struct {
		hash, date string
		amount     float64
	}{
		{"h-small", "2026-08-20", -5},
		{"h-big", "2026-08-02", -500},
		{"h-mid", "2026-08-10", -50},
		{"h-refund", "2026-08-25", 20},
		{"h-tie-old", "2026-08-03", -75},
		{"h-tie-new", "2026-08-12", -75},
		{"h-tie-b", "2026-08-12", -75},
	}
	var tx []models.Transaction
	var flags []anomalies.Anomaly
	for _, r := range rows {
		tx = append(tx, models.Transaction{Date: sv1Date(r.date), Description: "Merchant " + r.hash, Category: "Dining", Amount: r.amount, TransactionType: models.Outflow, Hash: r.hash})
		flags = append(flags, anomalies.Anomaly{Hash: r.hash, Method: "mad_category", Severity: "high"})
	}
	ts := models.NewTransactionSet(tx)
	p := insightssvc.BuildPeriodContext(ts, sv1Date("2026-08-01"), sv1Date("2026-08-31"), sv1Date("2026-09-08"))

	// Criteria 1 and 2: |amount| descending; ties by newest date, then hash.
	// The three -75 rows: newest date first; h-tie-b and h-tie-new share the
	// date, so hash ascending decides.
	want := []string{"h-big", "h-tie-b", "h-tie-new", "h-tie-old", "h-mid", "h-refund", "h-small"}
	got := buildFindings(ts, p, flags, nil)
	if len(got) != len(want) {
		t.Fatalf("want %d findings, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Transaction.Hash != want[i] {
			t.Fatalf("order at %d: want %s got %s (full: %v)", i, want[i], got[i].Transaction.Hash, hashes(got))
		}
	}

}

// Criterion 3 against the REAL detectors: eleven price-creep groups whose
// latest charges are all different, so the preview must hold the five
// largest and the "View all" block must continue downward from there.
func TestSV1PreviewAndRemainingFollowAmountOrder(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	var tx []models.Transaction
	for i := 0; i < 11; i++ {
		for m := 1; m <= 6; m++ {
			amount := -10.0 * float64(i+1)
			if m >= 4 {
				amount *= 2
			}
			tx = append(tx, models.Transaction{Date: time.Date(2026, time.Month(m), 28, 12, 0, 0, 0, time.UTC), Description: fmt.Sprintf("Merchant%c Coffee", 'A'+i), Category: "Dining", Amount: amount, TransactionType: models.Outflow, Hash: fmt.Sprintf("g%d-m%d", i, m)})
		}
	}
	ts := models.NewTransactionSet(tx)
	p := insightssvc.BuildPeriodContext(ts, sv1Date("2026-06-01"), sv1Date("2026-06-28"), sv1Date("2026-09-08"))
	old := loader
	loader = nil
	defer func() { loader = old }()
	v := buildInvestigation(ts, &models.InsightsData{Period: &p})
	if len(v.Findings) != 11 || len(v.Preview) != 5 || len(v.Remaining) != 6 {
		t.Fatalf("fixture: %d findings, preview %d, remaining %d", len(v.Findings), len(v.Preview), len(v.Remaining))
	}
	var want []string
	for i := 10; i >= 0; i-- {
		want = append(want, fmt.Sprintf("g%d-m6", i))
	}
	all := append(append([]FindingView{}, v.Preview...), v.Remaining...)
	for _, got := range [][]string{hashes(v.Findings), hashes(all)} {
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("order: %v", got)
		}
	}
	html, err := renderer.RenderToString("insights-content", map[string]any{"Insights": models.InsightsData{Period: &p}, "Period": p, "Investigation": v, "StartDate": "2026-06-01", "EndDate": "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`data-finding-hash="([^"]+)"`)
	var rendered []string
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		rendered = append(rendered, m[1])
	}
	if strings.Join(rendered, ",") != strings.Join(want, ",") {
		t.Fatalf("rendered order: %v", rendered)
	}
	if !strings.Contains(html, `<details id="all-findings"`) {
		t.Fatal("expected the View all block for the six remaining findings")
	}

	// Criterion 4: the intro sentence names the order.
	if !strings.Contains(html, "11 findings in the selected period, largest amount first. A transaction can have more than one finding type.") {
		t.Fatal("intro sentence does not state the order")
	}
}

func hashes(fs []FindingView) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Transaction.Hash)
	}
	return out
}
