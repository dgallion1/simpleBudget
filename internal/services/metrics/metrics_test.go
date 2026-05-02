package metrics

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func makeTransaction(date string, amount float64, txType models.TransactionType) models.Transaction {
	t, _ := time.Parse("2006-01-02", date)
	tx := models.Transaction{
		Date:            t,
		Amount:          amount,
		TransactionType: txType,
		Description:     "test",
		Category:        "test",
	}
	tx.ComputeDerivedFields()
	return tx
}

func TestNew(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New() returned nil")
	}
}

func TestCalculateMetrics_Empty(t *testing.T) {
	s := New()
	ts := models.NewTransactionSet(nil)
	m := s.CalculateMetrics(ts)

	if m.TotalIncome != 0 {
		t.Errorf("expected 0 income, got %f", m.TotalIncome)
	}
	if m.TotalExpenses != 0 {
		t.Errorf("expected 0 expenses, got %f", m.TotalExpenses)
	}
	if m.NetSavings != 0 {
		t.Errorf("expected 0 net savings, got %f", m.NetSavings)
	}
	if m.SavingsRate != 0 {
		t.Errorf("expected 0 savings rate, got %f", m.SavingsRate)
	}
	if m.TransactionCount != 0 {
		t.Errorf("expected 0 transactions, got %d", m.TransactionCount)
	}
	if len(m.IncomeTrend) != 0 {
		t.Errorf("expected empty income trend, got %v", m.IncomeTrend)
	}
}

func TestCalculateMetrics_IncomeOnly(t *testing.T) {
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 5000, models.Income),
		makeTransaction("2025-02-15", 5000, models.Income),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if m.TotalIncome != 10000 {
		t.Errorf("expected 10000 income, got %f", m.TotalIncome)
	}
	if m.TotalExpenses != 0 {
		t.Errorf("expected 0 expenses, got %f", m.TotalExpenses)
	}
	if m.SavingsRate != 100 {
		t.Errorf("expected 100%% savings rate, got %f", m.SavingsRate)
	}
	if m.TransactionCount != 2 {
		t.Errorf("expected 2 transactions, got %d", m.TransactionCount)
	}
}

func TestCalculateMetrics_ZeroIncome(t *testing.T) {
	// When totalIncome == 0, savingsRate should stay 0
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", -500, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if m.SavingsRate != 0 {
		t.Errorf("expected 0 savings rate with zero income, got %f", m.SavingsRate)
	}
	if m.TotalExpenses != 500 {
		t.Errorf("expected 500 expenses, got %f", m.TotalExpenses)
	}
}

func TestCalculateMetrics_MixedIncomeAndExpenses(t *testing.T) {
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 5000, models.Income),
		makeTransaction("2025-01-20", -2000, models.Outflow),
		makeTransaction("2025-02-15", 5000, models.Income),
		makeTransaction("2025-02-20", -1500, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if m.TotalIncome != 10000 {
		t.Errorf("expected 10000 income, got %f", m.TotalIncome)
	}
	if m.TotalExpenses != 3500 {
		t.Errorf("expected 3500 expenses, got %f", m.TotalExpenses)
	}
	if m.NetSavings != 6500 {
		t.Errorf("expected 6500 net savings, got %f", m.NetSavings)
	}
	expectedRate := (6500.0 / 10000.0) * 100
	if m.SavingsRate != expectedRate {
		t.Errorf("expected %f savings rate, got %f", expectedRate, m.SavingsRate)
	}
	if m.TransactionCount != 4 {
		t.Errorf("expected 4 transactions, got %d", m.TransactionCount)
	}
}

// Regression: a refund (opposite-signed Outflow row) must REDUCE TotalExpenses,
// not be added as an absolute value. Pre-fix bug: $3500 of purchases plus a
// $300 refund produced TotalExpenses=$3800 instead of $3200.
func TestCalculateMetrics_RefundReducesTotalExpenses(t *testing.T) {
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 5000, models.Income),
		makeTransaction("2025-01-20", -2000, models.Outflow), // purchase (negative-convention)
		makeTransaction("2025-02-20", -1500, models.Outflow), // purchase
		makeTransaction("2025-02-25", 300, models.Outflow),   // refund (opposite sign)
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if m.TotalExpenses != 3200 {
		t.Errorf("TotalExpenses = %.2f, want 3200 (refund of +300 must subtract)", m.TotalExpenses)
	}
	if m.NetSavings != 1800 {
		t.Errorf("NetSavings = %.2f, want 1800", m.NetSavings)
	}
	// February has -1500 + 300 = -1200, abs = 1200
	if len(m.ExpensesTrend) != 2 || m.ExpensesTrend[1] != 1200 {
		t.Errorf("ExpensesTrend = %v, want Feb total 1200 (refund must subtract within month)", m.ExpensesTrend)
	}
}

func TestCalculateMetrics_TrendLabels(t *testing.T) {
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 5000, models.Income),
		makeTransaction("2025-02-15", 5000, models.Income),
		makeTransaction("2025-02-20", -1000, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if len(m.TrendLabels) != 2 {
		t.Fatalf("expected 2 trend labels, got %d: %v", len(m.TrendLabels), m.TrendLabels)
	}
	if m.TrendLabels[0] != "2025-01" || m.TrendLabels[1] != "2025-02" {
		t.Errorf("unexpected trend labels: %v", m.TrendLabels)
	}
	// Jan: income=5000, expenses=0, savings=5000
	if m.IncomeTrend[0] != 5000 {
		t.Errorf("expected 5000 income trend[0], got %f", m.IncomeTrend[0])
	}
	if m.ExpensesTrend[0] != 0 {
		t.Errorf("expected 0 expenses trend[0], got %f", m.ExpensesTrend[0])
	}
	if m.SavingsTrend[0] != 5000 {
		t.Errorf("expected 5000 savings trend[0], got %f", m.SavingsTrend[0])
	}
	// Feb: income=5000, expenses=1000, savings=4000
	if m.IncomeTrend[1] != 5000 {
		t.Errorf("expected 5000 income trend[1], got %f", m.IncomeTrend[1])
	}
	if m.ExpensesTrend[1] != 1000 {
		t.Errorf("expected 1000 expenses trend[1], got %f", m.ExpensesTrend[1])
	}
	if m.SavingsTrend[1] != 4000 {
		t.Errorf("expected 4000 savings trend[1], got %f", m.SavingsTrend[1])
	}
}

func TestCalculateMetrics_MoreThan6Months(t *testing.T) {
	s := New()
	// Create 8 months of data to trigger the >6 truncation
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 1000, models.Income),
		makeTransaction("2025-02-15", 1000, models.Income),
		makeTransaction("2025-03-15", 1000, models.Income),
		makeTransaction("2025-04-15", 1000, models.Income),
		makeTransaction("2025-05-15", 1000, models.Income),
		makeTransaction("2025-06-15", 1000, models.Income),
		makeTransaction("2025-07-15", 1000, models.Income),
		makeTransaction("2025-08-15", 1000, models.Income),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if len(m.TrendLabels) != 6 {
		t.Errorf("expected 6 trend labels (truncated), got %d", len(m.TrendLabels))
	}
	// Should be last 6 months: 03 through 08
	if m.TrendLabels[0] != "2025-03" {
		t.Errorf("expected first trend label 2025-03, got %s", m.TrendLabels[0])
	}
	if m.TrendLabels[5] != "2025-08" {
		t.Errorf("expected last trend label 2025-08, got %s", m.TrendLabels[5])
	}
}

func TestCalculateMetrics_MonthWithOnlyExpenses(t *testing.T) {
	// A month that only appears in outflows, not income — tests the monthSet merge
	s := New()
	txns := []models.Transaction{
		makeTransaction("2025-01-15", 5000, models.Income),
		makeTransaction("2025-02-15", -1000, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)
	m := s.CalculateMetrics(ts)

	if len(m.TrendLabels) != 2 {
		t.Fatalf("expected 2 trend labels, got %d", len(m.TrendLabels))
	}
	// Jan: income=5000, expenses=0
	// Feb: income=0, expenses=1000
	if m.IncomeTrend[1] != 0 {
		t.Errorf("expected 0 income trend for Feb, got %f", m.IncomeTrend[1])
	}
	if m.ExpensesTrend[0] != 0 {
		t.Errorf("expected 0 expenses trend for Jan, got %f", m.ExpensesTrend[0])
	}
}

func TestPercentChange(t *testing.T) {
	s := New()

	tests := []struct {
		name     string
		current  float64
		previous float64
		expected float64
	}{
		{"both zero", 0, 0, 0},
		{"previous zero current positive", 100, 0, 100},
		{"previous zero current negative", -50, 0, 100},
		{"increase", 150, 100, 50},
		{"decrease", 50, 100, -50},
		{"double", 200, 100, 100},
		{"negative previous", -50, -100, 50},
		{"same value", 100, 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.PercentChange(tt.current, tt.previous)
			if got != tt.expected {
				t.Errorf("PercentChange(%f, %f) = %f, want %f", tt.current, tt.previous, got, tt.expected)
			}
		})
	}
}

func TestCalculateComparison_Previous(t *testing.T) {
	s := New()

	// Current period: Feb 2025
	// Previous period: Jan 2025
	txns := []models.Transaction{
		makeTransaction("2025-01-10", 4000, models.Income),
		makeTransaction("2025-01-15", -1000, models.Outflow),
		makeTransaction("2025-02-10", 5000, models.Income),
		makeTransaction("2025-02-15", -1500, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	comp := s.CalculateComparison(ts, start, end, "previous")
	if comp == nil {
		t.Fatal("expected non-nil comparison")
	}
	if !comp.HasData {
		t.Fatal("expected HasData to be true")
	}
	if comp.Current.TotalIncome != 5000 {
		t.Errorf("expected current income 5000, got %f", comp.Current.TotalIncome)
	}
	if comp.Previous.TotalIncome != 4000 {
		t.Errorf("expected previous income 4000, got %f", comp.Previous.TotalIncome)
	}
	// Income change: (5000-4000)/4000 * 100 = 25%
	if comp.IncomeChange != 25 {
		t.Errorf("expected 25%% income change, got %f", comp.IncomeChange)
	}
}

func TestCalculateComparison_Year(t *testing.T) {
	s := New()

	txns := []models.Transaction{
		makeTransaction("2024-02-10", 4000, models.Income),
		makeTransaction("2024-02-15", -1000, models.Outflow),
		makeTransaction("2025-02-10", 5000, models.Income),
		makeTransaction("2025-02-15", -2000, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	comp := s.CalculateComparison(ts, start, end, "year")
	if comp == nil {
		t.Fatal("expected non-nil comparison")
	}
	if !comp.HasData {
		t.Fatal("expected HasData to be true")
	}
	if comp.Current.TotalIncome != 5000 {
		t.Errorf("expected current income 5000, got %f", comp.Current.TotalIncome)
	}
	if comp.Previous.TotalIncome != 4000 {
		t.Errorf("expected previous income 4000, got %f", comp.Previous.TotalIncome)
	}
}

func TestCalculateComparison_InvalidType(t *testing.T) {
	s := New()
	ts := models.NewTransactionSet(nil)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	comp := s.CalculateComparison(ts, start, end, "invalid")
	if comp != nil {
		t.Error("expected nil for invalid comparison type")
	}
}

func TestCalculateComparison_NoComparisonData(t *testing.T) {
	s := New()

	// Only current period data, no previous period
	txns := []models.Transaction{
		makeTransaction("2025-02-10", 5000, models.Income),
	}
	ts := models.NewTransactionSet(txns)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	comp := s.CalculateComparison(ts, start, end, "previous")
	if comp == nil {
		t.Fatal("expected non-nil comparison")
	}
	if comp.HasData {
		t.Error("expected HasData to be false when no comparison data exists")
	}
}

func TestCalculateComparison_SavingsRateChange(t *testing.T) {
	s := New()

	txns := []models.Transaction{
		// Jan: income=10000, expenses=5000, rate=50%
		makeTransaction("2025-01-10", 10000, models.Income),
		makeTransaction("2025-01-15", -5000, models.Outflow),
		// Feb: income=10000, expenses=2000, rate=80%
		makeTransaction("2025-02-10", 10000, models.Income),
		makeTransaction("2025-02-15", -2000, models.Outflow),
	}
	ts := models.NewTransactionSet(txns)

	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	comp := s.CalculateComparison(ts, start, end, "previous")
	if comp == nil {
		t.Fatal("expected non-nil comparison")
	}
	// SavingsRateChange = 80 - 50 = 30 percentage points
	if comp.SavingsRateChange != 30 {
		t.Errorf("expected 30 pp savings rate change, got %f", comp.SavingsRateChange)
	}
}
