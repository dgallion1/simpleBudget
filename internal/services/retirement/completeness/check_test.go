package completeness

import (
	"testing"

	"budget2/internal/models"
)

func TestCheck_StateTaxUnset(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantCode  string
		wantFound bool
	}{
		{
			name:      "nil TaxConfig emits state_tax_unset",
			settings:  &models.WhatIfSettings{TaxConfig: nil},
			wantCode:  codeStateTaxUnset,
			wantFound: true,
		},
		{
			name: "zero StateIncomeTaxRate emits state_tax_unset",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 0.0},
			},
			wantCode:  codeStateTaxUnset,
			wantFound: true,
		},
		{
			name: "non-zero StateIncomeTaxRate emits no state_tax finding",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 5.0},
			},
			wantCode:  codeStateTaxUnset,
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, tc.wantCode); got != tc.wantFound {
				t.Fatalf("Check() finding %q present = %v, want %v (got %d findings)",
					tc.wantCode, got, tc.wantFound, len(findings))
			}
		})
	}
}

func TestCheck_StateTaxFindingShape(t *testing.T) {
	settings := &models.WhatIfSettings{TaxConfig: nil}
	findings := Check(settings)

	f := findByCode(findings, codeStateTaxUnset)
	if f == nil {
		t.Fatal("expected state_tax_unset finding, got none")
	}
	if f.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", f.Severity)
	}
	if f.Title == "" || f.Detail == "" || f.Action == "" {
		t.Errorf("Finding has empty user-facing fields: %+v", f)
	}
}

func hasCode(findings []Finding, code string) bool {
	return findByCode(findings, code) != nil
}

func findByCode(findings []Finding, code string) *Finding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}
