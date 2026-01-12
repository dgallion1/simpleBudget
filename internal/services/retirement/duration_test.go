package retirement

import (
	"budget2/internal/models"
	"testing"
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

	t.Run("EndYear 0 is perpetual", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:    amount,
			StartYear: 0,
			EndYear:   0, // Traditional behavior: 0 is perpetual
		}

		val := es.GetAdjustedAmount(120, 0)
		if val != amount {
			t.Errorf("expected perpetual expense %.0f, got %.0f", amount, val)
		}
	})

	t.Run("EndYear same as StartYear ends immediately", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:    amount,
			StartYear: 1,
			EndYear:   1, // Ends at start of year 1 (immediately after starting)
		}

		val := es.GetAdjustedAmount(12, 0)
		if val != 0 {
			t.Errorf("expected 0 expense at year 1 (month 12), got %.0f", val)
		}
	})

	t.Run("EndYear 1 ends after 1 year", func(t *testing.T) {
		es := &models.ExpenseSource{
			Amount:    amount,
			StartYear: 0,
			EndYear:   1,
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
