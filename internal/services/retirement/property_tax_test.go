package retirement

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestDefaultWhatIfSettings_PropertyTaxFields(t *testing.T) {
	s := models.DefaultWhatIfSettings()

	if s.MonthlyPropertyTax != 0 {
		t.Errorf("MonthlyPropertyTax default = %v, want 0", s.MonthlyPropertyTax)
	}
	if s.PropertyTaxInflation != 4.0 {
		t.Errorf("PropertyTaxInflation default = %v, want 4.0", s.PropertyTaxInflation)
	}
}

func TestInitializeLoadedSettings_PropertyTaxInflation(t *testing.T) {
	t.Run("legacy file with zero PropertyTaxInflation gets defaulted to 4.0", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			PropertyTaxInflation: 0,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.PropertyTaxInflation != 4.0 {
			t.Errorf("PropertyTaxInflation = %v, want 4.0 (defaulted)", settings.PropertyTaxInflation)
		}
	})

	t.Run("non-zero PropertyTaxInflation is preserved", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			PropertyTaxInflation: 5.5,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.PropertyTaxInflation != 5.5 {
			t.Errorf("PropertyTaxInflation = %v, want 5.5 (preserved)", settings.PropertyTaxInflation)
		}
	})

	t.Run("MonthlyPropertyTax is preserved verbatim (no defaulting)", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			MonthlyPropertyTax: 800,
		}
		raw := map[string]json.RawMessage{}

		initializeLoadedSettings(settings, raw)

		if settings.MonthlyPropertyTax != 800 {
			t.Errorf("MonthlyPropertyTax = %v, want 800 (preserved)", settings.MonthlyPropertyTax)
		}
	})
}

func TestApplySettingsUpdates_PropertyTax(t *testing.T) {
	t.Run("monthly_property_tax writes to settings", func(t *testing.T) {
		settings := &models.WhatIfSettings{}
		updates := map[string]interface{}{"monthly_property_tax": 750.0}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.MonthlyPropertyTax != 750 {
			t.Errorf("MonthlyPropertyTax = %v, want 750", settings.MonthlyPropertyTax)
		}
	})

	t.Run("property_tax_inflation writes to settings", func(t *testing.T) {
		settings := &models.WhatIfSettings{}
		updates := map[string]interface{}{"property_tax_inflation": 5.5}

		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, updates)

		if settings.PropertyTaxInflation != 5.5 {
			t.Errorf("PropertyTaxInflation = %v, want 5.5", settings.PropertyTaxInflation)
		}
	})
}
