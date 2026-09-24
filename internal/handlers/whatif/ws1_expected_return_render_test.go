package whatif

import (
	"fmt"
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestWS1ExpectedReturnDataAttribute pins data-expected-return (WS1 C1) on
// BOTH investment-return displays — the in-card one (whatif-rate-
// assumptions.html) and the Quick Adjust one (whatif-quick-adjust.html's
// "rates" tab) — to EXACTLY the served figure, printf "%.1f" on
// .Settings.GetExpectedReturnFromAllocation, on a fixture with a ZERO
// allocation field (RothPercent=0) matching the live plan (WS1.4 finding
// O1: the JS client recompute's `|| 10` fallback treats a real 0 as "not
// set" and can disagree with the server on the live plan's real
// allocations). This is the Go-side half of the C1 fix; the JS side (that
// updateInvestmentReturnDisplay reads this attribute, never recomputes) is
// pinned by whatif-rate-assumptions.test.cjs's C1/C1k tests.
func TestWS1ExpectedReturnDataAttribute(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.InvestmentReturn = 0 // renders the "Using asset allocation" branch on both displays
	s.TaxDeferredPercent = 60
	s.RothPercent = 0 // the live-plan zero-allocation field (WS1.4 O1)
	s.TaxDeferredStockPercent = 70.55
	s.TaxDeferredCashPercent = 2.25
	s.TaxableStockPercent = 99.35
	s.TaxableCashPercent = 0.55

	// Derived independently through the SAME production formula
	// (GetExpectedReturnFromAllocation) and the SAME format verb the
	// templates use — a mutation that changes the TEMPLATE's format verb
	// (e.g. %.2f) or its data source still has to match this string
	// exactly, so it fails regardless of which side of the render it
	// touches.
	want := fmt.Sprintf(`data-expected-return="%.1f"`, s.GetExpectedReturnFromAllocation())

	inCard, err := renderer.RenderToString("whatif-rate-assumptions", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString(whatif-rate-assumptions): %v", err)
	}
	if !strings.Contains(inCard, want) {
		t.Errorf("in-card investment-return-display: expected %s; got: %s", want, truncate(inCard, 4000))
	}

	qa, err := renderer.RenderToString("whatif-quick-adjust-rates-content", map[string]any{"Settings": s})
	if err != nil {
		t.Fatalf("RenderToString(whatif-quick-adjust-rates-content): %v", err)
	}
	if !strings.Contains(qa, want) {
		t.Errorf("Quick Adjust investment-return display: expected %s; got: %s", want, truncate(qa, 4000))
	}
}
