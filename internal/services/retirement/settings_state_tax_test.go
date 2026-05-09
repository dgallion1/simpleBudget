package retirement

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestInitializeLoadedSettings_TaxConfigDefaults(t *testing.T) {
	t.Run("legacy file without tax_config and only primary Person defaults to single", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			TaxConfig: nil,
			Persons: []models.Person{
				{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
			},
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.TaxConfig == nil {
			t.Fatal("expected non-nil TaxConfig after initializeLoadedSettings")
		}
		if settings.TaxConfig.FilingStatus != models.FilingSingle {
			t.Errorf("FilingStatus = %v, want FilingSingle (single-person scenario)",
				settings.TaxConfig.FilingStatus)
		}
		if settings.TaxConfig.StateIncomeTaxRate != 0 {
			t.Errorf("StateIncomeTaxRate = %v, want 0", settings.TaxConfig.StateIncomeTaxRate)
		}
	})

	t.Run("legacy file with primary + spouse Person defaults to married_joint", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			TaxConfig: nil,
			Persons: []models.Person{
				{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				{ID: "p2", Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
			},
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.TaxConfig == nil {
			t.Fatal("expected non-nil TaxConfig after initializeLoadedSettings")
		}
		if settings.TaxConfig.FilingStatus != models.FilingMarriedJoint {
			t.Errorf("FilingStatus = %v, want FilingMarriedJoint (couple scenario)",
				settings.TaxConfig.FilingStatus)
		}
	})

	t.Run("existing TaxConfig is preserved (pointer-equal)", func(t *testing.T) {
		original := &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: 7.5,
		}
		settings := &models.WhatIfSettings{
			TaxConfig: original,
			Persons: []models.Person{
				{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				{ID: "p2", Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
			},
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.TaxConfig != original {
			t.Errorf("TaxConfig was replaced; expected pointer-equal preservation")
		}
		if settings.TaxConfig.StateIncomeTaxRate != 7.5 {
			t.Errorf("StateIncomeTaxRate = %v, want 7.5 (preserved)",
				settings.TaxConfig.StateIncomeTaxRate)
		}
	})
}

func TestApplySettingsUpdates_StateIncomeTaxRate(t *testing.T) {
	t.Run("state_income_tax_rate writes to TaxConfig (allocating if nil)", func(t *testing.T) {
		settings := &models.WhatIfSettings{TaxConfig: nil}
		updates := map[string]interface{}{"state_income_tax_rate": 5.0}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.TaxConfig == nil {
			t.Fatal("expected TaxConfig allocated by applySettingsUpdates")
		}
		if settings.TaxConfig.StateIncomeTaxRate != 5.0 {
			t.Errorf("StateIncomeTaxRate = %v, want 5.0", settings.TaxConfig.StateIncomeTaxRate)
		}
	})

	t.Run("state_income_tax_rate preserves existing TaxConfig fields", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			TaxConfig: &models.TaxConfig{
				FilingStatus: models.FilingSingle,
				Age65Count:   1,
			},
		}
		updates := map[string]interface{}{"state_income_tax_rate": 4.25}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.TaxConfig.FilingStatus != models.FilingSingle {
			t.Errorf("FilingStatus mutated to %v", settings.TaxConfig.FilingStatus)
		}
		if settings.TaxConfig.Age65Count != 1 {
			t.Errorf("Age65Count mutated to %v", settings.TaxConfig.Age65Count)
		}
		if settings.TaxConfig.StateIncomeTaxRate != 4.25 {
			t.Errorf("StateIncomeTaxRate = %v, want 4.25", settings.TaxConfig.StateIncomeTaxRate)
		}
	})
}
