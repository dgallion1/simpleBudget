package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// CB2 regression: handleKPIDetail's per-month "Expenses" figure (and its
// DERIVED "Savings") and handleKPIExport's CSV must show the SIGNED negated
// net for a refund-dominant month, not math.Abs'd, driven through the real
// HTTP surface with loader-inferred TransactionTypes.
//
// "Boutique Store Credit" avoids every classifier IncomeKeyword ("refund",
// "rebate", "cashback", etc.) so the loader classifies it Outflow -- the
// real-data refund shape. Amounts are distinct from the CB2 oracle's
// 5000/300/800.
func cb2KPIHTTPRows() [][]string {
	return [][]string{
		// Jan 2025: ordinary month.
		{"2025-01-15", "Salary", "4200", "Payroll"},
		{"2025-01-05", "Rent", "-1300", "Housing"},
		{"2025-01-10", "Groceries", "-220", "Food"},
		// Feb 2025: refund-dominant -- a $950 store credit exceeds the
		// month's $220 grocery spend, net outflow +730.
		{"2025-02-15", "Salary", "4200", "Payroll"},
		{"2025-02-10", "Groceries", "-220", "Food"},
		{"2025-02-12", "Boutique Store Credit", "950", "Furniture"},
	}
}

func TestHandleKPIDetail_CB2_ExpensesSignedForRefundDominantMonth(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb2KPIHTTPRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sums, ok := result["Monthly"].([]interface{})
	if !ok || len(sums) == 0 {
		t.Fatalf("harness error: Monthly missing or empty: %v", result)
	}

	var febStat map[string]interface{}
	for _, s := range sums {
		ms := s.(map[string]interface{})
		if strings.HasPrefix(ms["Month"].(string), "2025-02") {
			febStat = ms
		}
	}
	if febStat == nil {
		t.Fatalf("harness error: no Feb entry in Monthly: %v", sums)
	}

	if exp := febStat["Expenses"].(float64); !floatEqual(exp, -730) {
		t.Errorf("KPI Feb Expenses = %v, want -730 (signed net credit); per-month abs gives 730", exp)
	}
	if sav := febStat["Savings"].(float64); !floatEqual(sav, 4930) {
		t.Errorf("KPI Feb Savings = %v, want 4930 (income 4200 PLUS 730 net refund); abs gives 3470", sav)
	}
}

func TestHandleKPIExport_CB2_ExpensesSignedForRefundDominantMonth(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb2KPIHTTPRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses/export?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("harness error: parsing CSV: %v", err)
	}

	var febRow []string
	for _, row := range records {
		if len(row) > 0 && strings.HasPrefix(row[0], "2025-02") {
			febRow = row
		}
	}
	if febRow == nil {
		t.Fatalf("harness error: no Feb row in CSV export: %v", records)
	}
	if febRow[1] != "-730.00" {
		t.Errorf("CSV Feb Expenses = %q, want \"-730.00\" (signed net credit); per-month abs gives \"730.00\"; full body:\n%s", febRow[1], body)
	}
}

// CB2 amendment CB2-c, site 8: handleKPIMonthDetail's "Total Spent" tile
// must show the SIGNED negated net for a refund-dominant month, matching
// the KD-signed living/healthcare kinds in the SAME handler.
func TestHandleKPIMonthDetail_CB2_TotalSpentSignedForRefundDominantMonth(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb2KPIHTTPRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses/month/2025-02?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	tot, ok := result["Total"].(float64)
	if !ok {
		t.Fatalf("harness error: Total missing in month-detail keys %v", result)
	}
	if !floatEqual(tot, -730) {
		t.Errorf("month-detail Total Spent = %v, want -730 (signed net credit); per-month abs gives 730", tot)
	}
	if label, _ := result["TotalLabel"].(string); label != "Total Spent" {
		t.Errorf("harness error: TotalLabel = %q, want \"Total Spent\"", label)
	}
}
