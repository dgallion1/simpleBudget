package prepare

import (
	"budget2/internal/models"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// Invalid schedules must be rejected; expiry and horizon changes must not brick saved plans.
func TestLivingSpendingBoostValidation(t *testing.T) {
	for _, tc := range []struct {
		name, amount, stop string
		valid              bool
	}{
		{"positive cents", "1000.01", "2036-09", true},
		{"binary cents", "0.29", "2036-09", true},
		{"zero", "0", "2036-09", false},
		{"negative", "-1", "2036-09", false},
		{"fractional cent", "1000.001", "2036-09", false},
		{"below cent", "0.001", "2036-09", false},
		{"bad date", "1000", "2036-13", false},
		{"empty date", "1000", "", false},
		{"expired", "1000", "2025-09", true},
		{"same month", "1000", "2026-09", true},
		{"beyond horizon", "1000", "2090-09", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = "2026-09"
			s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
			err := json.Unmarshal([]byte(fmt.Sprintf(`{"living_spending_boost":{"monthly_real":%s,"stop_month":%q}}`, tc.amount, tc.stop)), s)
			if err != nil {
				t.Fatal(err)
			}
			_, err = From(s)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestLivingSpendingBoostSnapshotIsolation(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000.01, StopMonth: "2036-09"}
	for _, copySettings := range []func(*models.WhatIfSettings) (*models.WhatIfSettings, error){DeepCopy, Clone} {
		clone, err := copySettings(s)
		if err != nil {
			t.Fatal(err)
		}
		if clone.LivingSpendingBoost == s.LivingSpendingBoost || *clone.LivingSpendingBoost != *s.LivingSpendingBoost {
			t.Fatal("copy lost schedule or aliased it")
		}
		clone.LivingSpendingBoost.MonthlyReal = 999
		if s.LivingSpendingBoost.MonthlyReal != 1000.01 {
			t.Fatal("copy changed original")
		}
	}
	prepared := MustFrom(t, s)
	s.LivingSpendingBoost.MonthlyReal = 900
	if prepared.Settings().LivingSpendingBoost.MonthlyReal != 1000.01 {
		t.Fatal("prepared input mutated")
	}
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64} {
		s.LivingSpendingBoost.MonthlyReal = amount
		if _, err := From(s); err == nil {
			t.Fatalf("accepted invalid amount %v", amount)
		}
	}
}
