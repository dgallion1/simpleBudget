package retirement

import (
	"encoding/json"
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
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

	t.Run("user-saved zero PropertyTaxInflation is preserved when key is present in JSON", func(t *testing.T) {
		settings := &models.WhatIfSettings{
			PropertyTaxInflation: 0,
		}
		raw := map[string]json.RawMessage{
			"property_tax_inflation": json.RawMessage("0"),
		}

		initializeLoadedSettings(settings, raw)

		if settings.PropertyTaxInflation != 0 {
			t.Errorf("PropertyTaxInflation = %v, want 0 (user explicitly saved 0; key present in raw JSON)", settings.PropertyTaxInflation)
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

func TestTotalExpenses_PropertyTaxIncluded(t *testing.T) {
	t.Run("MonthlyPropertyTax adds to total expenses with own inflation rate", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.MonthlyPropertyTax = 800
		s.PropertyTaxInflation = 4.0
		s.InflationRate = 3.0
		s.SpendingDeclineRate = 0
		s.HealthcareInflation = 0

		// Month 0: living=5000, propertyTax=800, total=5800
		got := engine.TotalExpenses(s, 0)
		want := 5000.0 + 800.0
		if math.Abs(got-want) > 0.01 {
			t.Errorf("month 0 TotalExpenses = %v, want %v", got, want)
		}

		// Month 12 (1 year): living grows at 3% → 5150, propertyTax grows at 4% → 832, total=5982
		got12 := engine.TotalExpenses(s, 12)
		expectedLiving := 5000.0 * math.Pow(1.03, 1)
		expectedPropertyTax := 800.0 * math.Pow(1.04, 1)
		want12 := expectedLiving + expectedPropertyTax
		if math.Abs(got12-want12) > 1.0 {
			t.Errorf("month 12 TotalExpenses = %v, want ~%v", got12, want12)
		}
	})

	t.Run("Zero MonthlyPropertyTax has no effect", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.MonthlyLivingExpenses = 5000
		s.MonthlyHealthcare = 0
		s.MonthlyPropertyTax = 0
		s.HealthcareInflation = 0
		s.SpendingDeclineRate = 0

		got := engine.TotalExpenses(s, 0)
		if math.Abs(got-5000) > 0.01 {
			t.Errorf("month 0 TotalExpenses = %v, want 5000 (no property tax)", got)
		}
	})
}

func TestCalculateExpenseBreakdown_PropertyTaxIsEssential(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 5000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 600
	s.HealthcareInflation = 0
	s.SpendingDeclineRate = 0

	bd := engine.CalculateExpenseBreakdown(s, 0)
	wantEssential := 5600.0 // living + property tax
	if math.Abs(bd.Essential-wantEssential) > 0.01 {
		t.Errorf("Essential = %v, want %v (living + property tax)", bd.Essential, wantEssential)
	}
	if bd.Discretionary != 0 {
		t.Errorf("Discretionary = %v, want 0", bd.Discretionary)
	}
}

func TestPropertyTaxAffectsProjection(t *testing.T) {
	build := func(monthly float64) *models.WhatIfAnalysis {
		s := models.DefaultWhatIfSettings()
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5_000
		s.MonthlyPropertyTax = monthly
		s.PropertyTaxInflation = 4.0
		s.StartDate = "2026-01"
		s.Persons = []models.Person{
			{ID: "p1", Role: models.PersonRolePrimary, BirthMonth: "1960-01", Name: "You"},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, ClaimAge: 67}

		prepared := prepare.MustFrom(t, s)
		return RunFull(engine.New(), engine.Input{Prepared: prepared})
	}

	withPT := build(800)
	withoutPT := build(0)

	if withPT == nil || withoutPT == nil || withPT.Projection == nil || withoutPT.Projection == nil {
		t.Fatal("RunFull returned nil projection")
	}

	// Final-month balance should be lower with property tax.
	withPTFinal := withPT.Projection.Months[len(withPT.Projection.Months)-1].PortfolioBalance
	withoutPTFinal := withoutPT.Projection.Months[len(withoutPT.Projection.Months)-1].PortfolioBalance

	if !(withoutPTFinal > withPTFinal) {
		t.Errorf("expected $800/mo property tax to lower final balance; got with=%v without=%v",
			withPTFinal, withoutPTFinal)
	}
}
