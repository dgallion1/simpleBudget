package completeness

import (
	"testing"

	"budget2/internal/models"
)

func TestCheck_TaxableCostBasisUnset(t *testing.T) {
	// A portfolio that is 60% tax-deferred / 10% Roth leaves a real taxable
	// balance, so the check has something to warn about.
	withTaxable := func(basis *float64) *models.WhatIfSettings {
		return &models.WhatIfSettings{
			PortfolioValue:     1_000_000,
			TaxDeferredPercent: 60,
			RothPercent:        10,
			TaxableCostBasis:   basis,
		}
	}

	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name:      "unset basis with a taxable balance emits the finding",
			settings:  withTaxable(nil),
			wantFound: true,
		},
		{
			name:      "configured basis is silent",
			settings:  withTaxable(models.FloatPtr(280_000)),
			wantFound: false,
		},
		{
			name:      "explicit zero (fully appreciated) is configured, not unset",
			settings:  withTaxable(models.FloatPtr(0)),
			wantFound: false,
		},
		{
			name: "no taxable balance means nothing to warn about",
			settings: &models.WhatIfSettings{
				PortfolioValue:     1_000_000,
				TaxDeferredPercent: 100,
				RothPercent:        0,
				TaxableCostBasis:   nil,
			},
			wantFound: false,
		},
		{
			name: "empty portfolio is silent",
			settings: &models.WhatIfSettings{
				PortfolioValue:   0,
				TaxableCostBasis: nil,
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, codeTaxableCostBasisUnset); got != tc.wantFound {
				t.Errorf("hasCode(%s) = %v, want %v", codeTaxableCostBasisUnset, got, tc.wantFound)
			}
		})
	}
}

func TestCheck_TaxableCostBasisFindingShape(t *testing.T) {
	findings := Check(&models.WhatIfSettings{
		PortfolioValue:     1_000_000,
		TaxDeferredPercent: 60,
		RothPercent:        10,
	})

	f := findByCode(findings, codeTaxableCostBasisUnset)
	if f == nil {
		t.Fatal("expected taxable_cost_basis_unset finding, got none")
	}
	if f.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", f.Severity)
	}
	if f.Title == "" || f.Detail == "" || f.Action == "" {
		t.Errorf("Finding has empty user-facing fields: %+v", f)
	}
	// The anchor must match the input id so the banner can deep-link to it.
	if f.FormAnchor != "taxable-cost-basis-input" {
		t.Errorf("FormAnchor = %q; want it to match the form input id", f.FormAnchor)
	}
}
