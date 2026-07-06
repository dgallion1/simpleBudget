package retirement

import (
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
