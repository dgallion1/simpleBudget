package dashboard

// CB3 oracle probe. Copied by accept.sh into internal/handlers/dashboard/
// as zz_cb3_oracle_test.go for the oracle run, then removed. One test per
// contract in .swarm/CB3-RUN-SPEC.md.

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/internal/services/metrics"
)

func cb3Feq(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func cb3Txn(desc string, amount float64, date time.Time, tt models.TransactionType, category string) models.Transaction {
	return models.Transaction{Description: desc, Amount: amount, Date: date, TransactionType: tt, Category: category}
}

// CB3-A: major-expense drilldown modal Total via the Unmatched path (no
// defs configured in setupTestEnv, so every row lands in unmatched).
// Signed contract: Total = -(sum of signed amounts) = -(-300+100) = 200;
// per-transaction abs gives 400.
func TestCB3Oracle_DrilldownTotalSigned(t *testing.T) {
	rows := [][]string{
		{"2025-03-05", "Groceries", "-300", "Food"},
		{"2025-03-12", "Carnival Cruise Lines", "100", "Travel"},
	}
	router, cleanup := setupTestEnv(t, rows)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=Unmatched")
	if rec.Code != http.StatusOK {
		t.Fatalf("drilldown status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tot, ok := result["Total"].(float64)
	if !ok {
		t.Fatalf("harness error: Total missing; keys %v", keysOfCB3(result))
	}
	if cnt, _ := result["Count"].(float64); cnt != 2 {
		t.Fatalf("harness error: Count = %v, want 2 (both rows in Unmatched)", result["Count"])
	}
	if !cb3Feq(tot, 200) {
		t.Errorf("drilldown Total = %.2f, want 200 (-(signed sum)); per-txn abs gives 400", tot)
	}
}

// CB3-B: merchant chart nets refunds per merchant.
func TestCB3Oracle_MerchantTotalsSigned(t *testing.T) {
	mar := func(d int) time.Time { return time.Date(2025, 3, d, 0, 0, 0, 0, time.UTC) }
	ts := models.NewTransactionSet([]models.Transaction{
		cb3Txn("Store A", -300, mar(3), models.Outflow, "Shopping"),
		cb3Txn("Store A", 100, mar(9), models.Outflow, "Shopping"),
		cb3Txn("Store B", -50, mar(4), models.Outflow, "Shopping"),
	})
	chart := buildMerchantsChartData(ts)
	b, _ := json.Marshal(chart)
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("harness error: unmarshal: %v", err)
	}
	xs, ys := cb3FindXY(decoded)
	if xs == nil {
		t.Fatalf("harness error: no x/y trace in merchants chart: %.300s", string(b))
	}
	found := false
	for i, label := range ys {
		if label == "Store A" {
			found = true
			if !cb3Feq(xs[i], 200) {
				t.Errorf("Store A total = %.2f, want 200 (signed net: 300 spend - 100 refund); abs gives 400", xs[i])
			}
		}
	}
	if !found {
		t.Fatalf("harness error: Store A not in labels %v", ys)
	}
}

// CB3-C: the cash-flow direction bug — a refund DAY must INCREASE the
// running total. Income +1000 (day 1), outflow -300 (day 2), outflow-typed
// refund +100 (day 3): running must end 800; the abs bug ends 600.
func TestCB3Oracle_CashFlowRefundDirection(t *testing.T) {
	mar := func(d int) time.Time { return time.Date(2025, 3, d, 0, 0, 0, 0, time.UTC) }
	ts := models.NewTransactionSet([]models.Transaction{
		cb3Txn("Salary", 1000, mar(1), models.Income, "Payroll"),
		cb3Txn("Groceries", -300, mar(2), models.Outflow, "Food"),
		cb3Txn("Carnival Cruise Lines", 100, mar(3), models.Outflow, "Travel"),
	})
	chart := buildCumulativeChartData(ts)
	b, _ := json.Marshal(chart)
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("harness error: unmarshal: %v", err)
	}
	series := cb3AllYSeries(decoded)
	ok := false
	for _, s := range series {
		if len(s) == 3 && cb3Feq(s[0], 1000) && cb3Feq(s[1], 700) {
			ok = true
			if !cb3Feq(s[2], 800) {
				t.Errorf("cash flow after refund day = %.2f, want 800 (refund ADDS cash); the abs bug gives 600", s[2])
			}
		}
	}
	if !ok {
		t.Fatalf("harness error: no [1000 700 *] cumulative series found: %v", series)
	}
}

// CB3-D: insights aggregates signed.
func TestCB3Oracle_InsightsSigned(t *testing.T) {
	mar := func(d int) time.Time { return time.Date(2025, 3, d, 0, 0, 0, 0, time.UTC) }
	cur := models.NewTransactionSet([]models.Transaction{
		cb3Txn("Groceries", -300, mar(3), models.Outflow, "Food"),
		cb3Txn("Carnival Cruise Lines", 800, mar(9), models.Outflow, "Travel"),
	})
	v := insights.SpendingVelocity(cur, cur)
	if v == nil {
		t.Fatalf("harness error: SpendingVelocity nil")
	}
	if v.DailyAverage >= 0 {
		t.Errorf("DailyAverage = %.2f, want negative on a refund-dominant period (net +500 credit); abs gives positive", v.DailyAverage)
	}
	if v.MonthProjection >= 0 {
		t.Errorf("MonthProjection = %.2f, want negative on a refund-dominant period", v.MonthProjection)
	}

	defs := []models.MajorExpense{{ID: "cruise", Name: "Cruise", Keywords: []string{"carnival"}}}
	trends := insights.MajorExpenseTrends(models.NewTransactionSet([]models.Transaction{
		cb3Txn("Carnival Cruise Lines", -300, mar(3), models.Outflow, "Travel"),
		cb3Txn("Carnival Cruise Lines", 100, mar(9), models.Outflow, "Travel"),
	}), defs, nil, mar(1), mar(31))
	foundCruise := false
	for _, tr := range trends {
		if tr.Category == "Cruise" {
			foundCruise = true
			if !cb3Feq(tr.CurrentAmount, 200) {
				t.Errorf("Cruise period total = %.2f, want 200 (signed net); abs gives 400", tr.CurrentAmount)
			}
		}
	}
	if !foundCruise {
		t.Fatalf("harness error: Cruise not in MajorExpenseTrends: %+v", trends)
	}
}

// CB3-E: PercentChange contract pinned for signed bases — no code change.
func TestCB3Oracle_PercentChangeSignedBasePinned(t *testing.T) {
	if got := metrics.PercentChange(-500, -1000); !cb3Feq(got, 50) {
		t.Errorf("PercentChange(-500,-1000) = %.2f, want 50 (|previous| denominator preserves direction)", got)
	}
	if got := metrics.PercentChange(-1500, -1000); !cb3Feq(got, -50) {
		t.Errorf("PercentChange(-1500,-1000) = %.2f, want -50", got)
	}
}

// CB3-c (attempt 2): the inline trend classifier must be sign-consistent,
// and the two attempt-1 mutation survivors get direct oracle guards.
func TestCB3Oracle_TrendClassifierSignConsistent(t *testing.T) {
	mar := func(m time.Month, d int) time.Time { return time.Date(2025, m, d, 0, 0, 0, 0, time.UTC) }
	defs := []models.MajorExpense{{ID: "cruise", Name: "Cruise", Keywords: []string{"carnival"}}}

	// The real-ledger shape: previous period net-refund (-628-like), current 0.
	trendsA := insights.MajorExpenseTrends(models.NewTransactionSet([]models.Transaction{
		cb3Txn("Carnival Cruise Lines", -100, mar(time.March, 3), models.Outflow, "Travel"),
		cb3Txn("Carnival Cruise Lines", 728, mar(time.March, 9), models.Outflow, "Travel"),
	}), defs, nil, mar(time.April, 1), mar(time.April, 30))
	for _, tr := range trendsA {
		if tr.Category != "Cruise" {
			continue
		}
		if !cb3Feq(tr.PreviousAmount, -628) || !cb3Feq(tr.ChangeAmount, 628) {
			t.Fatalf("harness error: prev=%.2f change=%.2f, want -628/+628", tr.PreviousAmount, tr.ChangeAmount)
		}
		if tr.Direction != "up" {
			t.Errorf("Direction = %q with ChangeAmount +628, want \"up\" (sign of ChangeAmount)", tr.Direction)
		}
		if tr.ChangePercent <= 0 {
			t.Errorf("ChangePercent = %.2f with ChangeAmount +628, want positive (|previous| denominator)", tr.ChangePercent)
		}
	}

	// previous == 0, current net refund: must read down/-100, not up/+100.
	trendsB := insights.MajorExpenseTrends(models.NewTransactionSet([]models.Transaction{
		cb3Txn("Carnival Cruise Lines", 500, mar(time.April, 9), models.Outflow, "Travel"),
	}), defs, nil, mar(time.April, 1), mar(time.April, 30))
	for _, tr := range trendsB {
		if tr.Category != "Cruise" {
			continue
		}
		if tr.Direction == "up" || tr.ChangePercent > 0 {
			t.Errorf("net-refund current with zero previous reports Direction=%q ChangePercent=%.2f — sign-inconsistent (want down/negative)", tr.Direction, tr.ChangePercent)
		}
	}
}

func TestCB3Oracle_PinnedRefundNetsSigned(t *testing.T) {
	mar := func(d int) time.Time { return time.Date(2025, 3, d, 0, 0, 0, 0, time.UTC) }
	defs := []models.MajorExpense{{ID: "cruise", Name: "Cruise", Keywords: []string{"zzz-nomatch"}}}
	spend := cb3Txn("Some Store", -300, mar(3), models.Outflow, "Travel")
	spend.Hash = "h-cb3-pin-spend"
	refund := cb3Txn("Some Store", 100, mar(9), models.Outflow, "Travel")
	refund.Hash = "h-cb3-pin-refund"
	pins := map[string]string{"h-cb3-pin-spend": "cruise", "h-cb3-pin-refund": "cruise"}
	trends := insights.MajorExpenseTrends(models.NewTransactionSet([]models.Transaction{spend, refund}), defs, pins, mar(1), mar(31))
	found := false
	for _, tr := range trends {
		if tr.Category == "Cruise" {
			found = true
			if !cb3Feq(tr.CurrentAmount, 200) {
				t.Errorf("pinned period total = %.2f, want 200 (signed net via the PIN path); abs gives 400", tr.CurrentAmount)
			}
		}
	}
	if !found {
		t.Fatalf("harness error: pinned Cruise not in trends: %+v", trends)
	}
}

func TestCB3Oracle_MonthProjectionIdentity(t *testing.T) {
	// Rows dated in the CURRENT calendar month so spentSoFar's bucket is
	// non-empty; the identity holds exactly only when spentSoFar is the
	// signed net. The abs mutant shifts MonthProjection by 2*|refund net|.
	// Timestamps must land inside SpendingVelocity's current-month bucket,
	// which is computed in time.Local ([month start, now]) — UTC-midnight
	// dates can fall outside it (validated the hard way: a 1st-of-month
	// UTC row precedes the local month start in any west-of-UTC zone).
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	d2 := now.Add(-time.Hour)
	if d2.Before(monthStart) {
		d2 = now // first hour of the month: keep both rows at now
	}
	d1 := d2.Add(-time.Hour)
	if d1.Before(monthStart) {
		d1 = monthStart.Add(time.Minute)
	}
	ts := models.NewTransactionSet([]models.Transaction{
		cb3Txn("Groceries", -300, d1, models.Outflow, "Food"),
		cb3Txn("Carnival Cruise Lines", 800, d2, models.Outflow, "Travel"),
	})
	v := insights.SpendingVelocity(ts, ts)
	if v == nil {
		t.Fatalf("harness error: SpendingVelocity nil")
	}
	signedSpent := -500.0 // -( -300 + 800 )
	expected := signedSpent + v.DailyAverage*float64(v.DaysRemaining)
	if math.Abs(v.MonthProjection-expected) > 0.01 {
		t.Errorf("MonthProjection = %.2f, want %.2f (signedSpent %.2f + DailyAverage %.2f * DaysRemaining %d) — the abs spentSoFar mutant breaks this identity", v.MonthProjection, expected, signedSpent, v.DailyAverage, v.DaysRemaining)
	}
}

// --- probe helpers ---

func keysOfCB3(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func cb3FindXY(v interface{}) ([]float64, []string) {
	switch node := v.(type) {
	case map[string]interface{}:
		xsRaw, xok := node["x"].([]interface{})
		ysRaw, yok := node["y"].([]interface{})
		if xok && yok {
			var xs []float64
			var ys []string
			for _, x := range xsRaw {
				if f, ok := x.(float64); ok {
					xs = append(xs, f)
				}
			}
			for _, y := range ysRaw {
				if s, ok := y.(string); ok {
					ys = append(ys, s)
				}
			}
			if len(xs) > 0 && len(ys) > 0 {
				return xs, ys
			}
		}
		for _, child := range node {
			if xs, ys := cb3FindXY(child); xs != nil {
				return xs, ys
			}
		}
	case []interface{}:
		for _, child := range node {
			if xs, ys := cb3FindXY(child); xs != nil {
				return xs, ys
			}
		}
	}
	return nil, nil
}

func cb3AllYSeries(v interface{}) [][]float64 {
	var series [][]float64
	var walk func(interface{})
	walk = func(v interface{}) {
		switch node := v.(type) {
		case map[string]interface{}:
			if ys, ok := node["y"].([]interface{}); ok {
				var fs []float64
				for _, y := range ys {
					if f, ok := y.(float64); ok {
						fs = append(fs, f)
					}
				}
				if len(fs) > 0 {
					series = append(series, fs)
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(v)
	return series
}
