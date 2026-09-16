package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestIncomeSourceDuration(t *testing.T) {
	amount := 1000.0

	t.Run("EndMonth nil is perpetual", func(t *testing.T) {
		is := &models.IncomeSource{
			Amount:     amount,
			StartMonth: 0,
			EndMonth:   nil,
		}

		val := is.GetAdjustedAmount(120) // 10 years later
		if val != amount {
			t.Errorf("expected perpetual income %.0f, got %.0f", amount, val)
		}
	})

	t.Run("EndMonth 0 ends immediately", func(t *testing.T) {
		endMonth := 0
		is := &models.IncomeSource{
			Amount:     amount,
			StartMonth: 0,
			EndMonth:   &endMonth,
		}

		val := is.GetAdjustedAmount(0)
		if val != 0 {
			t.Errorf("expected 0 income at month 0 when EndMonth is 0, got %.0f", val)
		}

		val = is.GetAdjustedAmount(12)
		if val != 0 {
			t.Errorf("expected 0 income at month 12 when EndMonth is 0, got %.0f", val)
		}
	})

	t.Run("EndMonth 12 ends after 1 year", func(t *testing.T) {
		endMonth := 12
		is := &models.IncomeSource{
			Amount:     amount,
			StartMonth: 0,
			EndMonth:   &endMonth,
		}

		val := is.GetAdjustedAmount(11)
		if val != amount {
			t.Errorf("expected income %.0f at month 11, got %.0f", amount, val)
		}

		val = is.GetAdjustedAmount(12)
		if val != 0 {
			t.Errorf("expected 0 income at month 12, got %.0f", val)
		}
	})
}

func TestExpenseSourceDuration(t *testing.T) {
	amount := 1000.0

	t.Run("nil EndMonth is perpetual", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:     amount,
			StartMonth: 0,
			EndMonth:   nil, // nil is perpetual, as for IncomeSource
		}

		val := es.GetAdjustedAmount(120, 0)
		if val != amount {
			t.Errorf("expected perpetual expense %.0f, got %.0f", amount, val)
		}
	})

	t.Run("EndMonth same as StartMonth ends immediately", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:     amount,
			StartMonth: 12,
			EndMonth:   durationEndMonth(12), // Ends the month it starts (end exclusive)
		}

		val := es.GetAdjustedAmount(12, 0)
		if val != 0 {
			t.Errorf("expected 0 expense at year 1 (month 12), got %.0f", val)
		}
	})

	t.Run("EndMonth 12 ends after 1 year", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:     amount,
			StartMonth: 0,
			EndMonth:   durationEndMonth(12),
		}

		val := es.GetAdjustedAmount(11, 0)
		if val != amount {
			t.Errorf("expected expense %.0f at month 11, got %.0f", amount, val)
		}

		val = es.GetAdjustedAmount(12, 0)
		if val != 0 {
			t.Errorf("expected 0 expense at month 12, got %.0f", val)
		}
	})
}

func TestIncomeSourceMonthlyCOLA(t *testing.T) {
	is := &models.IncomeSource{
		Amount:     1000,
		StartMonth: 0,
		COLARate:   0.12,
	}

	got := is.GetAdjustedAmount(6)
	want := 1000.0 * math.Pow(1.12, 0.5)
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("month 6: want %.2f, got %.2f", want, got)
	}
}

func TestExpenseSourceMonthlyInflation(t *testing.T) {
	es := &models.ExpenseSource{
		Amount:     1000,
		StartMonth: 0,
		Inflation:  true,
	}

	got := es.GetAdjustedAmount(6, 12)
	want := 1000.0 * math.Pow(1.12, 0.5)
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("month 6: want %.2f, got %.2f", want, got)
	}
}

// durationEndMonth returns the *int an ExpenseSource's EndMonth needs.
func durationEndMonth(month int) *int { return &month }

// TestExpenseSourceMonthPrecision pins the behaviour the monthly rollover
// depends on: an expense can start and end mid-year, not just on a year
// boundary.
func TestExpenseSourceMonthPrecision(t *testing.T) {
	es := &models.ExpenseSource{Amount: 500, StartMonth: 11, EndMonth: durationEndMonth(23)}
	for _, tc := range []struct {
		month int
		want  float64
	}{{10, 0}, {11, 500}, {12, 500}, {22, 500}, {23, 0}} {
		if got := es.GetAdjustedAmount(tc.month, 0); got != tc.want {
			t.Errorf("month %d: got %.0f, want %.0f", tc.month, got, tc.want)
		}
		if got := es.IsActive(tc.month); got != (tc.want > 0) {
			t.Errorf("month %d: IsActive %v, want %v", tc.month, got, tc.want > 0)
		}
	}
}
