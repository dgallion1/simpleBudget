package templates

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestRenderRateAssumptions_ACAInputs covers the marketplace-coverage form
// section. The populated cases matter most: `printf "%.0f"` applied straight
// to a *float64 renders the pointer address, and a checkbox that never renders
// `checked` silently discards the setting on every save.
func TestRenderRateAssumptions_ACAInputs(t *testing.T) {
	r := newWhatIfRenderer(t)

	render := func(t *testing.T, aca *models.ACAConfig) string {
		t.Helper()
		s := models.DefaultWhatIfSettings()
		s.ACA = aca
		out, err := r.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		return collapse(out)
	}

	t.Run("unset renders empty inputs and an unchecked box", func(t *testing.T) {
		html := render(t, nil)
		for _, want := range []string{
			`id="aca-household-size-input"`,
			`name="aca_household_size" value=""`,
			`name="aca_premium_credit" value=""`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(html, `name="aca_advance_credits" value="true" checked`) {
			t.Error("advance-credits box should be unchecked when unset")
		}
		// The hidden-false partner must be present or an unchecked box posts nothing.
		if !strings.Contains(html, `type="hidden" name="aca_advance_credits" value="false"`) {
			t.Error("checkbox needs its hidden false partner to clear the value on save")
		}
	})

	t.Run("configured values render as numbers", func(t *testing.T) {
		html := render(t, &models.ACAConfig{
			HouseholdSize:          2,
			AnnualPremiumTaxCredit: models.FloatPtr(9600),
			AdvanceCreditsTaken:    true,
		})
		if !strings.Contains(html, `name="aca_household_size" value="2"`) {
			t.Error("household size did not render")
		}
		if !strings.Contains(html, `name="aca_premium_credit" value="9600"`) {
			t.Error("premium credit did not render as 9600")
		}
		if !strings.Contains(html, `name="aca_advance_credits" value="true" checked`) {
			t.Error("advance-credits box should be checked when set")
		}
		if strings.Contains(html, "%!f(") || strings.Contains(html, "0x") {
			t.Error("template rendered a pointer address — use the deref helper")
		}
	})

	t.Run("explains the two rules people get wrong", func(t *testing.T) {
		html := render(t, nil)
		if !strings.Contains(html, "all</em> Social Security") {
			t.Error("should say ACA MAGI counts all Social Security, not just the taxable part")
		}
		if !strings.Contains(html, "COBRA") {
			t.Error("should say COBRA forfeits the credit")
		}
	})
}
