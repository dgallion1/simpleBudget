package retirement

import (
	"testing"

	"budget2/internal/models"
)

// The taxable cost basis arrives from the form as an optional float, so the
// settings layer must distinguish "left blank" (nil) from "explicitly zero".
// A typo in the updates key would silently do nothing, which is exactly the
// failure this covers.
func TestApplySettingsUpdates_TaxableCostBasis(t *testing.T) {
	cases := []struct {
		name    string
		initial *float64
		update  interface{}
		want    *float64
	}{
		{
			name:    "a configured basis is stored",
			initial: nil,
			update:  models.FloatPtr(280_000),
			want:    models.FloatPtr(280_000),
		},
		{
			name:    "explicit zero is stored as ptr-to-zero, not left unset",
			initial: nil,
			update:  models.FloatPtr(0),
			want:    models.FloatPtr(0),
		},
		{
			name:    "a blank field clears a previously configured basis",
			initial: models.FloatPtr(280_000),
			update:  (*float64)(nil),
			want:    nil,
		},
		{
			name:    "an overwrite replaces the old value",
			initial: models.FloatPtr(280_000),
			update:  models.FloatPtr(310_000),
			want:    models.FloatPtr(310_000),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := &models.WhatIfSettings{TaxableCostBasis: tc.initial}
			sm := &SettingsManager{}
			sm.applySettingsUpdates(settings, map[string]interface{}{
				"taxable_cost_basis": tc.update,
			})

			got := settings.TaxableCostBasis
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("TaxableCostBasis = %v, want nil (unset)", *got)
			case tc.want != nil && got == nil:
				t.Errorf("TaxableCostBasis = nil, want %v", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("TaxableCostBasis = %v, want %v", *got, *tc.want)
			}
		})
	}

	t.Run("an absent key leaves the basis untouched", func(t *testing.T) {
		settings := &models.WhatIfSettings{TaxableCostBasis: models.FloatPtr(280_000)}
		sm := &SettingsManager{}
		sm.applySettingsUpdates(settings, map[string]interface{}{"inflation_rate": 3.0})

		if settings.TaxableCostBasis == nil || *settings.TaxableCostBasis != 280_000 {
			t.Errorf("TaxableCostBasis = %v; an unrelated update must not clear it",
				settings.TaxableCostBasis)
		}
	})
}
