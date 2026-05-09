package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/completeness"
)

func TestCompletenessCheck_StateTaxUnsetSurfaces(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingSingle,
			StateIncomeTaxRate: models.FloatPtr(0),
		},
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
		},
	}
	findings := completeness.Check(settings)

	found := false
	for _, f := range findings {
		if f.Code == "state_tax_unset" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected state_tax_unset finding from Check, got %+v", findings)
	}
}

func TestCompletenessBanner_RendersFindingTitle(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	findings := []completeness.Finding{
		{
			Severity:   completeness.SeverityWarn,
			Code:       "state_tax_unset",
			Title:      "TestTitle_StateTax",
			Detail:     "TestDetail",
			FormAnchor: "rate-assumptions-card",
			Action:     "TestAction",
		},
	}
	out, err := renderer.RenderToString("whatif-completeness", map[string]interface{}{
		"Findings": findings,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "TestTitle_StateTax") {
		t.Errorf("rendered partial missing finding title; got: %s", out)
	}
	if !strings.Contains(out, "TestDetail") {
		t.Errorf("rendered partial missing detail; got: %s", out)
	}
	if !strings.Contains(out, "TestAction") {
		t.Errorf("rendered partial missing action label; got: %s", out)
	}
	if !strings.Contains(out, "rate-assumptions-card") {
		t.Errorf("rendered partial missing form anchor href; got: %s", out)
	}
}

func TestCompletenessBanner_EmptyFindingsRendersNothingMeaningful(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out, err := renderer.RenderToString("whatif-completeness", map[string]interface{}{
		"Findings": []completeness.Finding{},
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	// With no findings, the {{if .Findings}} guard suppresses the wrapper.
	// Output should not contain the banner wrapper id.
	if strings.Contains(out, `id="whatif-completeness"`) {
		t.Errorf("expected no banner wrapper for empty findings; got: %s", out)
	}
}
