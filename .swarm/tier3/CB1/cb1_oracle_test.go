package metrics

// CB1 oracle probe. Copied by accept.sh into internal/services/metrics/ as
// zz_cb1_oracle_test.go for the duration of the oracle run, then removed.
// Reuses the package's test helpers (makeTransaction, makeTransactionSet,
// fullCoverage, floatEqual) deliberately — if a rename breaks this file,
// the oracle fails loudly rather than silently testing nothing.

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

// TestCB1Oracle_RefundDominantMonth builds the two-month fixture the KD run
// proved necessary: a single-month fixture cannot discriminate per-month-abs
// from signed arithmetic. Jan is a normal month (net -2000); Feb is
// refund-dominant (its only outflow-typed row is a +500 refund, so the
// month bucket NETS POSITIVE while the range still nets negative).
//
// Contract (models.DashboardMetrics field doc): the walk's last element
// equals -CombinedCumulativeDelta up to float noise, because per-month
// spends partition the range total. A refund-dominant month must therefore
// enter the walk as a CREDIT (balance rises by accrual + refund), never as
// spend.
func TestCB1Oracle_RefundDominantMonth(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	ts := makeTransactionSet(
		makeTransaction("Rent", -2000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		makeTransaction("Cruise refund", 500, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Travel"),
	)

	m := Calculate(ts, start, end, 1500, 0, fullCoverage, true, nil)

	if len(m.CombinedCumulativeBalance) != 2 {
		t.Fatalf("CombinedCumulativeBalance length = %d, want 2", len(m.CombinedCumulativeBalance))
	}

	// Harness validity guard: Jan is identical under bug and fix.
	// Jan accrual = 1500*(31/30.4375) = 1527.7207; balance = accrual - 2000.
	if !floatEqual(m.CombinedCumulativeBalance[0], -472.28) {
		t.Errorf("harness error, not the defect: Jan point = %.4f, want ~-472.2793", m.CombinedCumulativeBalance[0])
	}

	// The discriminator: Feb's step must be accrual PLUS the 500 credit.
	// Feb accrual = 1500*(28/30.4375) = 1379.8768; step = 1879.8768.
	// Under per-month abs the step is 879.8768 instead.
	step := m.CombinedCumulativeBalance[1] - m.CombinedCumulativeBalance[0]
	if math.Abs(step-1879.8768) > 0.01 {
		t.Errorf("refund-dominant month mis-signed: Feb step = %.4f, want ~1879.8768 (accrual 1379.8768 + 500 credit); per-month abs gives 879.8768", step)
	}

	// The documented invariant, WITH the refund-dominant month present.
	last := m.CombinedCumulativeBalance[1]
	if math.Abs(last-(-m.CombinedCumulativeDelta)) > 0.01 {
		t.Errorf("invariant broken: last walk point = %.4f, -CombinedCumulativeDelta = %.4f", last, -m.CombinedCumulativeDelta)
	}
}
