package models

import (
	"math"
	"testing"
)

func TestHealthcarePersonMonthlyCompounding(t *testing.T) {
	t.Run("pre medicare compounds monthly", func(t *testing.T) {
		person := HealthcarePerson{
			CurrentAge:           60,
			CurrentCoverage:      CoverageACA,
			CurrentMonthlyCost:   1000,
			PreMedicareInflation: 12,
			MedicareMonthlyCost:  400,
			MedicareEligibleAge:  65,
		}

		got := person.GetMonthlyCost(6)
		want := 1000.0 * math.Pow(1.12, 0.5)
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("month 6: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("post medicare compounds monthly after transition", func(t *testing.T) {
		person := HealthcarePerson{
			CurrentAge:            64,
			CurrentCoverage:       CoverageACA,
			CurrentMonthlyCost:    1000,
			PreMedicareInflation:  12,
			MedicareMonthlyCost:   400,
			PostMedicareInflation: 6,
			MedicareEligibleAge:   65,
		}

		got := person.GetMonthlyCost(18)
		want := 400.0 * math.Pow(1.06, 0.5)
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("month 18: want %.2f, got %.2f", want, got)
		}
	})
}
