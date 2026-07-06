package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func TestRunProjection_TaxDeferredDelayBlocksUntilExpiry(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 500000
	settings.TaxDeferredPercent = 60
	settings.RothPercent = 20
	settings.MonthlyLivingExpenses = 3000
	settings.MonthlyHealthcare = 0
	settings.ProjectionYears = 8
	settings.CurrentAge = 55
	settings.TaxDeferredDelayYears = 5
	settings.InvestmentReturn = 0.0000001
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0

	calc := newTestCalc(t, settings)
	result := calc.RunProjection()

	if !result.Survives {
		t.Fatalf("expected projection to survive the delay window, got depletion month %v", result.DepletionMonth)
	}

	for _, pm := range result.Months {
		if pm.Month < 60 && pm.WithdrawalFromTaxDeferred > 0 {
			t.Fatalf("month %d: expected no tax-deferred withdrawal during delay, got %.2f", pm.Month, pm.WithdrawalFromTaxDeferred)
		}
	}

	postDelayWithdrawal := false
	for _, pm := range result.Months {
		if pm.Month >= 60 && pm.WithdrawalFromTaxDeferred > 0 {
			postDelayWithdrawal = true
			break
		}
	}
	if !postDelayWithdrawal {
		t.Fatal("expected tax-deferred withdrawals after delay expired")
	}
}

func TestRunProjection_ZeroDelayAllowsEarlyTaxDeferred(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 500000
	settings.TaxDeferredPercent = 80
	settings.RothPercent = 0
	settings.MonthlyLivingExpenses = 5000
	settings.MonthlyHealthcare = 0
	settings.ProjectionYears = 5
	settings.CurrentAge = 55
	settings.TaxDeferredDelayYears = 0
	settings.InvestmentReturn = 0.0000001
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0

	calc := newTestCalc(t, settings)
	result := calc.RunProjection()

	earlyWithdrawal := false
	for _, pm := range result.Months {
		if pm.Month <= 24 && pm.WithdrawalFromTaxDeferred > 0 {
			earlyWithdrawal = true
			break
		}
	}
	if !earlyWithdrawal {
		t.Fatal("expected tax-deferred withdrawals within the first two years when delay is disabled")
	}
}

func TestRunProjection_RMDOverridesDelay(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 1000000
	settings.TaxDeferredPercent = 80
	settings.RothPercent = 0
	settings.MonthlyLivingExpenses = 2000
	settings.MonthlyHealthcare = 0
	settings.ProjectionYears = 15
	settings.CurrentAge = 65
	settings.TaxDeferredDelayYears = 15
	settings.InvestmentReturn = 0.0000001
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0

	calc := newTestCalc(t, settings)
	result := calc.RunProjection()

	hasRMDWithdrawal := false
	for _, pm := range result.Months {
		if pm.Month >= 96 && pm.WithdrawalFromTaxDeferred > 0 {
			hasRMDWithdrawal = true
			break
		}
	}
	if !hasRMDWithdrawal {
		t.Fatal("expected RMD withdrawals to override the tax-deferred delay at age 73")
	}
}

func TestRunSingleHistoricalSequence_RespectsDelay(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 500000
	settings.TaxDeferredPercent = 80
	settings.RothPercent = 0
	settings.MonthlyLivingExpenses = 4000
	settings.MonthlyHealthcare = 0
	settings.ProjectionYears = 5
	settings.CurrentAge = 55
	settings.TaxDeferredDelayYears = 5
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0

	calc := newTestCalc(t, settings)
	withDelay := calc.runSingleHistoricalSequence(1990)
	settings.TaxDeferredDelayYears = 0
	withoutDelay := newTestCalc(t, settings).runSingleHistoricalSequence(1990)

	if !withDelay.Survives {
		t.Fatal("expected historical backtest to treat delay-period shortfalls as temporary while tax-deferred assets remain")
	}
	if !withoutDelay.Survives {
		t.Fatal("expected the same historical sequence to survive once the delay is removed")
	}
}

func TestRunProjection_TemporaryShortfallDoesNotStopProjection(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.PortfolioValue = 820000
	settings.TaxDeferredPercent = 97 // ~$800k in tax-deferred
	settings.RothPercent = 0         // zero Roth
	// remaining ~$20k goes to taxable
	settings.MonthlyLivingExpenses = 5000
	settings.MonthlyHealthcare = 0
	settings.ProjectionYears = 15
	settings.CurrentAge = 55
	settings.TaxDeferredDelayYears = 10
	settings.InvestmentReturn = 0.0000001
	settings.InflationRate = 0
	settings.SpendingDeclineRate = 0

	calc := newTestCalc(t, settings)
	result := calc.RunProjection()

	// Taxable (~$20k) will be exhausted within a few months at $5k/mo.
	// During the remaining delay period, accessible accounts are empty but
	// tax-deferred still has ~$800k. The projection should NOT treat this
	// temporary shortfall as portfolio depletion.
	if !result.Survives {
		t.Fatalf("expected projection to survive (tax-deferred has funds), got depletion month %v", result.DepletionMonth)
	}

	// After the delay expires (month 120), tax-deferred withdrawals should resume.
	postDelayWithdrawal := false
	for _, pm := range result.Months {
		if pm.Month >= 120 && pm.WithdrawalFromTaxDeferred > 0 {
			postDelayWithdrawal = true
			break
		}
	}
	if !postDelayWithdrawal {
		t.Fatal("expected tax-deferred withdrawals to resume after delay expired")
	}
}

func TestWithdrawForExpenses_TracksSourcesAndShortfall(t *testing.T) {
	taxDeferred := 300000.0
	taxable := 100000.0
	roth := 50000.0
	// TEMP scaffold: Task 7 replaces with real basis pointer from PortfolioMonthInput.
	dummyBasis := roth

	withdrawal := engine.WithdrawForExpenses(200000, 0, false, 0, &taxDeferred, &taxable, &roth, &dummyBasis)

	if math.Abs(withdrawal.WithdrawalFromTaxable-100000) > 0.01 {
		t.Fatalf("expected taxable withdrawal of 100000, got %.2f", withdrawal.WithdrawalFromTaxable)
	}
	if math.Abs(withdrawal.WithdrawalFromRoth-50000) > 0.01 {
		t.Fatalf("expected Roth withdrawal of 50000, got %.2f", withdrawal.WithdrawalFromRoth)
	}
	if withdrawal.WithdrawalFromTaxDeferred != 0 {
		t.Fatalf("expected no tax-deferred withdrawal while blocked, got %.2f", withdrawal.WithdrawalFromTaxDeferred)
	}
	if math.Abs(withdrawal.RemainingNeed-50000) > 0.01 {
		t.Fatalf("expected 50000 shortfall, got %.2f", withdrawal.RemainingNeed)
	}
}
