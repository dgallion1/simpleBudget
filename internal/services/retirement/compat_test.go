package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// TestPresentValue covers the three branches of PresentValue:
//   - periods <= 0 returns futureValue as-is
//   - annualRate <= 0 returns futureValue as-is
//   - the normal discounting path
func TestPresentValue(t *testing.T) {
	t.Run("zero periods returns future value", func(t *testing.T) {
		got := PresentValue(1000, 5.0, 0)
		if got != 1000 {
			t.Errorf("PresentValue(1000,5,0) = %f, want 1000", got)
		}
	})

	t.Run("negative periods returns future value", func(t *testing.T) {
		got := PresentValue(1000, 5.0, -5)
		if got != 1000 {
			t.Errorf("PresentValue(1000,5,-5) = %f, want 1000", got)
		}
	})

	t.Run("zero rate returns future value", func(t *testing.T) {
		got := PresentValue(1000, 0.0, 12)
		if got != 1000 {
			t.Errorf("PresentValue(1000,0,12) = %f, want 1000", got)
		}
	})

	t.Run("negative rate returns future value", func(t *testing.T) {
		got := PresentValue(1000, -2.0, 12)
		if got != 1000 {
			t.Errorf("PresentValue(1000,-2,12) = %f, want 1000", got)
		}
	})

	t.Run("normal discount", func(t *testing.T) {
		got := PresentValue(1000, 5.0, 12)
		// One year discount at 5%: ~952.38
		if got <= 940 || got >= 960 {
			t.Errorf("PresentValue(1000,5,12) = %f, want ~952", got)
		}
	})
}

// TestPresentValueAnnuity exercises each branch of the PVA function.
func TestPresentValueAnnuity(t *testing.T) {
	t.Run("zero payments", func(t *testing.T) {
		if got := PresentValueAnnuity(100, 5.0, 0, 0, 0); got != 0 {
			t.Errorf("got %f, want 0", got)
		}
	})

	t.Run("zero payment amount", func(t *testing.T) {
		if got := PresentValueAnnuity(0, 5.0, 0, 0, 12); got != 0 {
			t.Errorf("got %f, want 0", got)
		}
	})

	t.Run("zero discount no growth", func(t *testing.T) {
		// monthlyRate <= 0, monthlyGrowth == 0 -> payment * n
		got := PresentValueAnnuity(100, 0, 0, 0, 12)
		if got != 1200 {
			t.Errorf("got %f, want 1200", got)
		}
	})

	t.Run("zero discount with growth", func(t *testing.T) {
		// monthlyRate <= 0, monthlyGrowth != 0 -> summation branch
		got := PresentValueAnnuity(100, 0, 3.0, 0, 12)
		if got <= 1200 {
			t.Errorf("got %f, want > 1200 due to growth", got)
		}
	})

	t.Run("growth equals discount", func(t *testing.T) {
		got := PresentValueAnnuity(100, 5.0, 5.0, 0, 12)
		if math.Abs(got-1200) > 0.01 {
			t.Errorf("got %f, want ~1200 when growth==discount", got)
		}
	})

	t.Run("growing annuity formula", func(t *testing.T) {
		// monthlyGrowth != 0 and not equal to monthlyRate
		got := PresentValueAnnuity(100, 5.0, 2.0, 0, 12)
		if got <= 0 || got >= 1300 {
			t.Errorf("got %f, want positive but less than nominal sum", got)
		}
	})

	t.Run("regular annuity formula", func(t *testing.T) {
		// monthlyGrowth == 0, monthlyRate > 0
		got := PresentValueAnnuity(100, 5.0, 0, 0, 12)
		// ~1167 for 12 monthly payments of 100 at 5% nominal annual
		if got <= 1100 || got >= 1200 {
			t.Errorf("got %f, want ~1167", got)
		}
	})

	t.Run("start month delay", func(t *testing.T) {
		// startMonth > 0 and monthlyRate > 0 -> additional discount
		gotNow := PresentValueAnnuity(100, 5.0, 0, 0, 12)
		gotLater := PresentValueAnnuity(100, 5.0, 0, 12, 12)
		if !(gotLater < gotNow) {
			t.Errorf("later start (%f) should discount more than now (%f)", gotLater, gotNow)
		}
	})
}

func TestMonthlyCompoundFactorFromDecimal(t *testing.T) {
	t.Run("zero rate", func(t *testing.T) {
		if got := monthlyCompoundFactorFromDecimal(0); got != 1.0 {
			t.Errorf("got %f, want 1.0", got)
		}
	})

	t.Run("positive rate", func(t *testing.T) {
		got := monthlyCompoundFactorFromDecimal(0.05)
		// (1.05)^(1/12) ~ 1.00407
		if math.Abs(got-1.00407) > 0.001 {
			t.Errorf("got %f, want ~1.00407", got)
		}
	})
}

func TestMonthlyCompoundFactorFromPercent(t *testing.T) {
	gotPct := monthlyCompoundFactorFromPercent(5.0)
	gotDec := monthlyCompoundFactorFromDecimal(0.05)
	if math.Abs(gotPct-gotDec) > 1e-9 {
		t.Errorf("percent (%f) and decimal (%f) should match", gotPct, gotDec)
	}
}

func TestIsSocialSecurityIncomeSource(t *testing.T) {
	cases := []struct {
		name   string
		source models.IncomeSource
		want   bool
	}{
		{"social security name", models.IncomeSource{Name: "Social Security"}, true},
		{"hyphenated", models.IncomeSource{Name: "social-security"}, true},
		{"SSI token", models.IncomeSource{Name: "My SSI Benefit"}, true},
		{"unrelated pension", models.IncomeSource{Name: "Pension"}, false},
		{"empty name", models.IncomeSource{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSocialSecurityIncomeSource(tc.source); got != tc.want {
				t.Errorf("IsSocialSecurityIncomeSource(%q) = %v, want %v", tc.source.Name, got, tc.want)
			}
		})
	}
}
