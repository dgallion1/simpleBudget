package templates

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestRenderRateAssumptions_TaxableCostBasisInput covers the cost-basis field
// added for FINANCEAPPCONCERNS.md §2.
//
// The populated case is the one that matters: `printf "%.0f"` applied straight
// to a *float64 renders the pointer address, not the number. The template must
// go through the `deref` helper. Without this test the field looked fine
// whenever it was empty — which is every scenario that has not set it.
func TestRenderRateAssumptions_TaxableCostBasisInput(t *testing.T) {
	r := newWhatIfRenderer(t)

	t.Run("unset renders an empty value", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.TaxableCostBasis = nil

		out, err := r.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		html := collapse(out)

		if !strings.Contains(html, `id="taxable-cost-basis-input"`) {
			t.Fatal("cost-basis input is missing from the rate-assumptions panel")
		}
		if !strings.Contains(html, `name="taxable_cost_basis" value=""`) {
			t.Errorf("unset basis should render an empty value attribute; got:\n%s",
				excerptAround(html, "taxable-cost-basis-input"))
		}
	})

	t.Run("configured basis renders the number, not a pointer", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.TaxableCostBasis = models.FloatPtr(280000)

		out, err := r.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		html := collapse(out)

		if !strings.Contains(html, `name="taxable_cost_basis" value="280000"`) {
			t.Errorf("configured basis did not render as 280000; got:\n%s",
				excerptAround(html, "taxable-cost-basis-input"))
		}
		// The specific failure mode worth naming.
		if strings.Contains(html, "%!f(") || strings.Contains(html, "0x") {
			t.Error("template rendered a pointer address — use the deref helper")
		}
	})

	t.Run("explicit zero renders 0, not blank", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.TaxableCostBasis = models.FloatPtr(0)

		out, err := r.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		html := collapse(out)

		if !strings.Contains(html, `name="taxable_cost_basis" value="0"`) {
			t.Errorf("ptr-to-zero is configured, not unset, and must render 0; got:\n%s",
				excerptAround(html, "taxable-cost-basis-input"))
		}
	})
}

// excerptAround returns a short window of html around the first occurrence of
// needle, for readable failure output.
func excerptAround(html, needle string) string {
	i := strings.Index(html, needle)
	if i < 0 {
		return "(not found)"
	}
	start := i - 120
	if start < 0 {
		start = 0
	}
	end := i + 220
	if end > len(html) {
		end = len(html)
	}
	return html[start:end]
}
