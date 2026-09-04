package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// The coverage timeline in the healthcare person card must name the
// person's current coverage through CoverageType.Label() — the single
// coverage→label mapping — not a template-local "employer, else ACA"
// branch that labelled every non-employer coverage "ACA" (EX3).
func TestRenderHealthcarePerson_TimelineUsesCoverageLabel(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	render := func(t *testing.T, coverage models.CoverageType) string {
		t.Helper()
		person := models.HealthcarePerson{
			ID:                    "p1",
			Name:                  "Pat",
			CurrentAge:            60,
			CurrentCoverage:       coverage,
			CurrentMonthlyCost:    1234,
			MedicareEligibleAge:   65,
			MedicareMonthlyCost:   500,
			PreMedicareInflation:  7,
			PostMedicareInflation: 4,
		}
		html, err := renderer.RenderToString("whatif-healthcare-person", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Person":   person,
		})
		if err != nil {
			t.Fatalf("RenderToString(%s) error: %v", coverage, err)
		}
		return html
	}

	cases := []struct {
		coverage models.CoverageType
		want     string
		reject   string
	}{
		{models.CoverageACA, "ACA $1,234", ""},
		{models.CoverageEmployer, "Employer $1,234", "ACA $1,234"},
		{models.CoverageCOBRA, "COBRA $1,234", "ACA $1,234"},
	}
	for _, tc := range cases {
		html := render(t, tc.coverage)
		if !strings.Contains(html, tc.want) {
			t.Errorf("%s: timeline label %q missing from render", tc.coverage, tc.want)
		}
		if tc.reject != "" && strings.Contains(html, tc.reject) {
			t.Errorf("%s: timeline wrongly labelled %q", tc.coverage, tc.reject)
		}
	}

	// A person already on Medicare has no pre-Medicare timeline at all.
	if html := render(t, models.CoverageMedicare); strings.Contains(html, "Coverage Timeline") {
		t.Errorf("medicare: pre-Medicare coverage timeline should not render")
	}
}
