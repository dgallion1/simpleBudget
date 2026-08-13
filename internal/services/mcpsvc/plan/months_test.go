package plan

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func projectionWithMonths(n int) *models.ProjectionResult {
	months := make([]models.ProjectionMonth, n)
	for i := range months {
		months[i] = models.ProjectionMonth{
			Month:                     i,
			PortfolioBalance:          1_000_000 - float64(i)*100.6,
			TaxesPaid:                 10.4,
			RMDWithdrawal:             5.5,
			WithdrawalFromTaxDeferred: 20.7,
			WithdrawalFromTaxable:     8.3,
			WithdrawalFromRoth:        3.1,
		}
	}
	return &models.ProjectionResult{Months: months}
}

func TestMonthWindow_ReturnsInclusiveRange(t *testing.T) {
	rows, err := MonthWindow(projectionWithMonths(360), 12, 23)
	if err != nil {
		t.Fatalf("MonthWindow: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("len(rows) = %d, want 12", len(rows))
	}
	if rows[0].Month != 12 || rows[11].Month != 23 {
		t.Errorf("range = %d..%d, want 12..23", rows[0].Month, rows[11].Month)
	}
	if rows[0].TaxesPaid != 10 {
		t.Errorf("TaxesPaid = %v, want 10 (rounded)", rows[0].TaxesPaid)
	}
	if rows[0].WithdrawalFromTaxDeferred != 21 {
		t.Errorf("WithdrawalFromTaxDeferred = %v, want 21 (rounded)", rows[0].WithdrawalFromTaxDeferred)
	}
	if rows[0].WithdrawalFromTaxable != 8 {
		t.Errorf("WithdrawalFromTaxable = %v, want 8 (rounded)", rows[0].WithdrawalFromTaxable)
	}
	if rows[0].WithdrawalFromRoth != 3 {
		t.Errorf("WithdrawalFromRoth = %v, want 3 (rounded)", rows[0].WithdrawalFromRoth)
	}
}

func TestMonthWindow_RejectsSpanOverLimitStatingTheLimit(t *testing.T) {
	_, err := MonthWindow(projectionWithMonths(360), 0, 359)
	if err == nil {
		t.Fatal("expected an error for a 360-month span")
	}
	if !strings.Contains(err.Error(), "120") {
		t.Errorf("error should state the 120-month limit, got: %v", err)
	}
}

func TestMonthWindow_RejectsOutOfRangeStatingValidRange(t *testing.T) {
	_, err := MonthWindow(projectionWithMonths(24), 30, 40)
	if err == nil {
		t.Fatal("expected an error for an out-of-range window")
	}
	if !strings.Contains(err.Error(), "0") || !strings.Contains(err.Error(), "23") {
		t.Errorf("error should state the valid 0..23 range, got: %v", err)
	}
}

func TestMonthWindow_RejectsInvertedRangeStatingValidRange(t *testing.T) {
	_, err := MonthWindow(projectionWithMonths(24), 10, 5)
	if err == nil {
		t.Fatal("expected an error when from > to")
	}
	if !strings.Contains(err.Error(), "0") || !strings.Contains(err.Error(), "23") {
		t.Errorf("error should state the valid 0..23 range, got: %v", err)
	}
}
