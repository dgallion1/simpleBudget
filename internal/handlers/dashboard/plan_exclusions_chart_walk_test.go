package dashboard

import (
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
)

// TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance is SY4
// ruling SY-2026-08-30e's required equality test: buildBudgetVsActualChartData's
// cumulative-balance trace and metrics.Calculate's CombinedCumulativeBalance
// must agree MONTH-BY-MONTH on a SIGN-DIVERGENT fixture (Jan's living
// remainder nets a REFUND while Jan's Healthcare bucket is an ordinary net
// spend -- the two buckets diverge in sign), run BOTH with
// planExclusions=nil AND with a flagged def.
//
// Before the fix, the chart's walk summed two INDEPENDENTLY-Abs'd buckets
// (|LivingOutflows month| + |HI month|), which only equals the true
// combined |sum| when both buckets share a sign -- master's identity
// livingMonth = expAmt - hcAmt used to make that cancellation exact, but
// ruling SY-2026-08-30d decoupled livingMonth into its own bucket, so the
// walk diverged from metrics.go's own (correctly merge-then-Abs) walk by
// real dollars -- EVEN WITH planExclusions=nil, a regression against
// master that was impossible before attempt 3 (ruling SY-2026-08-30e).
func TestChartCumulativeWalk_AgreesWithMetricsCombinedCumulativeBalance(t *testing.T) {
	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	coverageStart := start.AddDate(-1, 0, 0)

	// buildTS returns a fresh TransactionSet each call -- both Calculate
	// and buildBudgetVsActualChartData mutate/filter internally, so sharing
	// one slice across two calls risks aliasing surprises; a fresh copy per
	// call side-steps that entirely.
	buildTS := func() *models.TransactionSet {
		return models.NewTransactionSet([]models.Transaction{
			// Jan: living remainder (grocery + credit, excluding the
			// flagged car payment and Health Insurance) nets a REFUND of
			// +3000 -- sign-divergent from Jan's Healthcare bucket (-400,
			// an ordinary net spend).
			{Hash: "h-jan-grocery", Amount: -1000, Date: jan, TransactionType: models.Outflow, Category: "Food"},
			{Hash: "h-jan-credit", Amount: 4000, Date: jan, TransactionType: models.Outflow, Category: "Misc"},
			{Hash: "h-jan-premium", Amount: -400, Date: jan, TransactionType: models.Outflow, Category: "Health Insurance"},
			{Hash: "h-jan-car", Amount: -500, Date: jan, TransactionType: models.Outflow, Category: "Loan"},
			// Feb: an ordinary month -- both buckets negative (spend-only),
			// included so the test also covers the non-divergent case.
			{Hash: "h-feb-rent", Amount: -1500, Date: feb, TransactionType: models.Outflow, Category: "Housing"},
			{Hash: "h-feb-premium", Amount: -400, Date: feb, TransactionType: models.Outflow, Category: "Health Insurance"},
		})
	}

	run := func(t *testing.T, planExclusions map[string]models.MajorExpense) {
		t.Helper()
		// budgetTarget/healthcareTarget are deliberately tiny (1, 1), not
		// realistic dollar figures: the two walks' TARGET/accrual formulas
		// have always differed by design and pre-date SY4 entirely --
		// metrics.go prorates by MonthsBetween's exact fractional day count
		// per calendar month, while the chart's monthTarget stays a FLAT
		// per-month rate ("living's share stays the flat monthly rate
		// (unchanged from master)", see buildBudgetVsActualChartData's own
		// comment). That is out of this attempt's scope (ruling
		// SY-2026-08-30e only asks for the SPEND accrual's merge-then-Abs
		// fix). A tiny target keeps that pre-existing, unrelated accrual
		// drift down to a few cents so the assertion below isolates the
		// SPEND-side bug (whose ~$600-$1600 magnitude here is driven by
		// the transaction amounts, not the target) instead of tripping on
		// unrelated noise.
		m := metrics.Calculate(buildTS(), start, end, 1, 1, coverageStart, true, planExclusions)
		result := buildBudgetVsActualChartData(buildTS(), start, end, 1, 1, coverageStart, true, planExclusions)

		data, ok := result["data"].([]map[string]interface{})
		if !ok || len(data) != 3 {
			t.Fatalf("data = %v, want 3 traces", result["data"])
		}
		chartCumY, ok := data[2]["y"].([]float64)
		if !ok {
			t.Fatalf("trace[2].y is not []float64: %T", data[2]["y"])
		}

		if len(m.CombinedCumulativeBalance) != len(chartCumY) {
			t.Fatalf("point count mismatch: metrics walk has %d points, chart walk has %d (metrics=%v chart=%v)",
				len(m.CombinedCumulativeBalance), len(chartCumY), m.CombinedCumulativeBalance, chartCumY)
		}
		// Tolerance 0.2: generously covers the tiny-target accrual-formula
		// drift above (observed <= ~$0.13 across these two months) while
		// remaining thousands of times tighter than the spend-side
		// Abs-merge bug's magnitude (observed $475-$800/point when broken;
		// see the calibration transcript in SY4.4.manifest.md).
		for i := range m.CombinedCumulativeBalance {
			if diff := m.CombinedCumulativeBalance[i] - chartCumY[i]; diff > 0.2 || diff < -0.2 {
				t.Errorf("point %d: metrics.Calculate's CombinedCumulativeBalance = %v, buildBudgetVsActualChartData's cumulative trace = %v -- the two walks must agree (both merge the living+healthcare buckets and take ONE Abs, mirroring each other)",
					i, m.CombinedCumulativeBalance[i], chartCumY[i])
			}
		}
	}

	t.Run("nil planExclusions", func(t *testing.T) {
		run(t, nil)
	})
	t.Run("flagged def", func(t *testing.T) {
		run(t, map[string]models.MajorExpense{
			"h-jan-car": {ID: "car", Name: "Car Loan", ExcludeFromPlanSync: true},
		})
	})
}
