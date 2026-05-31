package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// ssRenderData builds the data map the "whatif-social-security-results"
// template expects: .Analysis.SocialSecurity and .Settings.
func ssRenderData(ss *models.SSComparisonAnalysis, settings *models.WhatIfSettings) map[string]interface{} {
	return map[string]interface{}{
		"Analysis": map[string]interface{}{"SocialSecurity": ss},
		"Settings": settings,
	}
}

func ssRenderSettings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		SpouseAge: 60,
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3000, FRA: 67, ClaimAge: 67,
			SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 67,
			COLARate: 0.02,
		},
	}
}

func TestSSSurvivor_RendersColumnForPrimaryHigher(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		SpouseOptions: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 1500},
		},
		HasSurvivorAnalysis:       true,
		HasSurvivorCallout:        true,
		SurvivorSelectedClaimAge:  67,
		SurvivorBenefitAtSelected: 3000,
		SurvivorBenefitAt70:       3720,
		SurvivorDelayGainPct:      24,
		HasSurvivorDelayUpside:    true,
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Survivor") {
		t.Errorf("expected a Survivor column/callout; got: %s", out)
	}
	if !strings.Contains(out, "You are the higher earner") {
		t.Errorf("expected higher-earner callout; got: %s", out)
	}
	if !strings.Contains(out, "to 70") {
		t.Errorf("expected delay-to-70 callout copy; got: %s", out)
	}
}

func TestSSSurvivor_RendersColumnForSpouseHigher(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 1500},
		},
		SpouseOptions: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		HasSurvivorAnalysis:          true,
		SurvivorHigherEarnerIsSpouse: true,
		HasSurvivorCallout:           true,
		SurvivorSelectedClaimAge:     67,
		SurvivorBenefitAtSelected:    3000,
		SurvivorBenefitAt70:          3720,
		SurvivorDelayGainPct:         24,
		HasSurvivorDelayUpside:       true,
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "Survivor") {
		t.Errorf("expected a Survivor column; got: %s", out)
	}
	if !strings.Contains(out, "Your spouse is the higher earner") {
		t.Errorf("expected spouse-higher callout copy; got: %s", out)
	}
}

func TestSSSurvivor_AnalysisOnlySuppressesCallout(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	ss := &models.SSComparisonAnalysis{
		Options: []models.SSClaimingOption{
			{ClaimAge: 67, MonthlyBenefit: 3000, SurvivorMonthlyBenefit: 3000},
		},
		SpouseOptions:       []models.SSClaimingOption{{ClaimAge: 67, MonthlyBenefit: 1500}},
		HasSurvivorAnalysis: true,
		HasSurvivorCallout:  false, // unset claim age
	}
	out, err := renderer.RenderToString("whatif-social-security-results", ssRenderData(ss, ssRenderSettings()))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if strings.Contains(out, "higher earner") {
		t.Errorf("callout must be suppressed when HasSurvivorCallout=false; got: %s", out)
	}
	if strings.Contains(out, "age 0") {
		t.Errorf("must not render 'age 0' copy; got: %s", out)
	}
}
