package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// newWhatIfRenderer builds a renderer over the embedded templates for the
// what-if component render tests.
func newWhatIfRenderer(t *testing.T) *Renderer {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}
	return r
}

// collapse normalizes runs of whitespace to single spaces so attribute
// assertions are insensitive to the template's source-level line breaks.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestRenderRateAssumptions_SpouseSoleBeneficiaryCheckbox verifies the checkbox
// renders with the hidden-false + checkbox-true pair and reflects the setting:
// checked when on (default), unchecked when explicitly off.
func TestRenderRateAssumptions_SpouseSoleBeneficiaryCheckbox(t *testing.T) {
	r := newWhatIfRenderer(t)

	// Default settings: SpouseSoleBeneficiary is nil → IsSpouseSoleBeneficiary
	// true → checkbox checked.
	htmlOn, err := r.RenderToString("whatif-rate-assumptions", map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
	})
	if err != nil {
		t.Fatalf("RenderToString(on) error: %v", err)
	}
	on := collapse(htmlOn)
	if !strings.Contains(on, `<input type="hidden" name="spouse_sole_beneficiary" value="false">`) {
		t.Errorf("expected hidden false input, got:\n%s", on)
	}
	if !strings.Contains(on, `name="spouse_sole_beneficiary" value="true" checked`) {
		t.Errorf("expected checked spouse-sole-beneficiary checkbox when default-on, got:\n%s", on)
	}
	if !strings.Contains(on, "Spouse is sole IRA beneficiary") {
		t.Errorf("expected checkbox label, got:\n%s", on)
	}

	// Explicit false → unchecked.
	settingsOff := models.DefaultWhatIfSettings()
	settingsOff.SpouseSoleBeneficiary = new(bool) // false
	htmlOff, err := r.RenderToString("whatif-rate-assumptions", map[string]any{
		"Settings": settingsOff,
	})
	if err != nil {
		t.Fatalf("RenderToString(off) error: %v", err)
	}
	off := collapse(htmlOff)
	if strings.Contains(off, `name="spouse_sole_beneficiary" value="true" checked`) {
		t.Errorf("expected unchecked checkbox when setting is false, got:\n%s", off)
	}
	if !strings.Contains(off, `name="spouse_sole_beneficiary" value="true" class=`) {
		t.Errorf("expected the checkbox input to still render when off, got:\n%s", off)
	}
}

// TestRenderRMD_CaptionNamesTableInEffect verifies the RMD schedule caption
// names the Joint Life Table II when UsesJointLifeTable is set and the Uniform
// Lifetime Table otherwise.
func TestRenderRMD_CaptionNamesTableInEffect(t *testing.T) {
	r := newWhatIfRenderer(t)

	settings := models.DefaultWhatIfSettings()
	settings.TaxDeferredPercent = 60

	mkAnalysis := func(joint bool) *models.WhatIfAnalysis {
		return &models.WhatIfAnalysis{
			RMD: &models.RMDAnalysis{
				CurrentAge:         73,
				StartAge:           73,
				TaxDeferredValue:   1_000_000,
				UsesJointLifeTable: joint,
				Projections: []models.RMDProjection{
					{Age: 73, Year: 0, TaxDeferredBal: 1_000_000, LifeExpFactor: 28.6, RMDAmount: 34965, RMDPercent: 3.5},
				},
			},
		}
	}

	htmlJoint, err := r.RenderToString("whatif-rmd", map[string]any{
		"Settings": settings,
		"Analysis": mkAnalysis(true),
	})
	if err != nil {
		t.Fatalf("RenderToString(joint) error: %v", err)
	}
	if !strings.Contains(htmlJoint, "Joint Life and Last Survivor Table II") {
		t.Errorf("expected Joint Life Table II caption, got:\n%s", htmlJoint)
	}

	htmlUniform, err := r.RenderToString("whatif-rmd", map[string]any{
		"Settings": settings,
		"Analysis": mkAnalysis(false),
	})
	if err != nil {
		t.Fatalf("RenderToString(uniform) error: %v", err)
	}
	if !strings.Contains(htmlUniform, "Uniform Lifetime Table") {
		t.Errorf("expected Uniform Lifetime Table caption, got:\n%s", htmlUniform)
	}
	if strings.Contains(htmlUniform, "Joint Life and Last Survivor Table II") {
		t.Errorf("uniform render must not name the Joint table, got:\n%s", htmlUniform)
	}
}
