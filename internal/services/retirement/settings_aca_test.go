package retirement

import (
	"testing"

	"budget2/internal/models"
)

// The ACA block is allocated lazily, so a plan with no marketplace coverage
// does not sprout an empty one — and a partial post from another form section
// must not clear what is already there.
func TestApplySettingsUpdates_ACA(t *testing.T) {
	sm := &SettingsManager{}

	t.Run("allocates the config when first set", func(t *testing.T) {
		s := &models.WhatIfSettings{}
		sm.applySettingsUpdates(s, map[string]interface{}{"aca_household_size": 3})
		if s.ACA == nil || s.ACA.HouseholdSize != 3 {
			t.Fatalf("ACA = %+v, want household size 3", s.ACA)
		}
	})

	t.Run("leaves it nil when nothing ACA-related is posted", func(t *testing.T) {
		s := &models.WhatIfSettings{}
		sm.applySettingsUpdates(s, map[string]interface{}{"inflation_rate": 3.0})
		if s.ACA != nil {
			t.Errorf("ACA = %+v, want nil for a plan with no marketplace coverage", s.ACA)
		}
	})

	t.Run("each field updates independently", func(t *testing.T) {
		s := &models.WhatIfSettings{ACA: &models.ACAConfig{
			HouseholdSize:          2,
			AnnualPremiumTaxCredit: models.FloatPtr(9600),
			AdvanceCreditsTaken:    true,
		}}
		sm.applySettingsUpdates(s, map[string]interface{}{"aca_household_size": 4})

		if s.ACA.HouseholdSize != 4 {
			t.Errorf("HouseholdSize = %d, want 4", s.ACA.HouseholdSize)
		}
		if s.ACA.AnnualPremiumTaxCredit == nil || *s.ACA.AnnualPremiumTaxCredit != 9600 {
			t.Errorf("credit = %v; an unrelated update must not clear it", s.ACA.AnnualPremiumTaxCredit)
		}
		if !s.ACA.AdvanceCreditsTaken {
			t.Error("advance-credits flag must survive an unrelated update")
		}
	})

	t.Run("a blank credit clears a configured one", func(t *testing.T) {
		s := &models.WhatIfSettings{ACA: &models.ACAConfig{
			HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600),
		}}
		sm.applySettingsUpdates(s, map[string]interface{}{"aca_premium_credit": (*float64)(nil)})
		if s.ACA.AnnualPremiumTaxCredit != nil {
			t.Errorf("credit = %v, want nil after a blank post", *s.ACA.AnnualPremiumTaxCredit)
		}
	})

	t.Run("unchecking advance credits turns it off", func(t *testing.T) {
		s := &models.WhatIfSettings{ACA: &models.ACAConfig{HouseholdSize: 2, AdvanceCreditsTaken: true}}
		sm.applySettingsUpdates(s, map[string]interface{}{"aca_advance_credits": false})
		if s.ACA.AdvanceCreditsTaken {
			t.Error("unchecking the box must turn the flag off")
		}
	})
}
