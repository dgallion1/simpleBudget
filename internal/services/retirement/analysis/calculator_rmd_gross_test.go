package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// F-073: when the RMD is forced but cash is not needed (surplus path),
// the resulting cashFlow.RMDWithdrawal and cashFlow.WithdrawalFromTaxDeferred
// must reflect the GROSS distribution. The taxable-account deposit (basis)
// is correctly net (F-049), but the reported distribution drives downstream
// tax/MAGI math and must remain gross.
func TestExecutePortfolioCashFlow_F073_SurplusRMDReportedGross(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := engine.NewTaxableAccountState(s, 0)
	taxDeferred := 1_000_000.0
	rothBalance := 0.0
	monthlyRMD := 5_000.0
	marginalRate := 0.22

	rothBasis := 0.0
	result := engine.ExecutePortfolioCashFlowWithTaxableState(
		0.0, // neededFromPortfolio == 0 → surplus path (else-branch at line 853)
		monthlyRMD,
		true,        // allowTaxDeferred
		0.0,         // earlyPenaltyRate
		marginalRate,
		&taxDeferred,
		&taxable,
		&rothBalance,
		&rothBasis,
	)

	if math.Abs(result.RMDWithdrawal-monthlyRMD) > 0.01 {
		t.Errorf("result.RMDWithdrawal = %.2f; want %.2f (gross)", result.RMDWithdrawal, monthlyRMD)
	}
	if math.Abs(result.WithdrawalFromTaxDeferred-monthlyRMD) > 0.01 {
		t.Errorf("result.WithdrawalFromTaxDeferred = %.2f; want %.2f (gross)", result.WithdrawalFromTaxDeferred, monthlyRMD)
	}

	// F-049 contract preserved: taxable account got NET deposit and basis.
	wantNet := monthlyRMD * (1 - marginalRate) // 3,900
	if math.Abs(taxable.MarketValue-wantNet) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want %.2f (net deposit)", taxable.MarketValue, wantNet)
	}
	if math.Abs(taxable.CostBasis-wantNet) > 0.01 {
		t.Errorf("taxable.CostBasis = %.2f; want %.2f (net basis)", taxable.CostBasis, wantNet)
	}

	// Tax-deferred decremented by GROSS (legal distribution).
	wantTaxDeferred := 1_000_000.0 - monthlyRMD
	if math.Abs(taxDeferred-wantTaxDeferred) > 0.01 {
		t.Errorf("taxDeferred = %.2f; want %.2f (decremented by gross)", taxDeferred, wantTaxDeferred)
	}
}

// F-073: same gross-reporting requirement on the partial-shortfall path
// (cash needed but RMD exceeds what was used to satisfy expenses).
func TestExecutePortfolioCashFlow_F073_PartialShortfallSurplusReportedGross(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := engine.NewTaxableAccountState(s, 0)
	taxable.MarketValue = 50_000
	taxable.CostBasis = 50_000
	taxDeferred := 1_000_000.0
	rothBalance := 0.0
	monthlyRMD := 5_000.0
	marginalRate := 0.22
	needed := 1_000.0 // small need; RMD will satisfy it and have surplus

	rothBasis := 0.0
	result := engine.ExecutePortfolioCashFlowWithTaxableState(
		needed,
		monthlyRMD,
		true,
		0.0,
		marginalRate,
		&taxDeferred,
		&taxable,
		&rothBalance,
		&rothBasis,
	)

	// withdrawForExpenses uses RMD first to satisfy `needed`, so 1,000 of the
	// 5,000 RMD goes to expenses (gross). Remaining 4,000 is surplus — it
	// must be reinvested and reported as GROSS in the two fields.
	if math.Abs(result.RMDWithdrawal-monthlyRMD) > 0.01 {
		t.Errorf("result.RMDWithdrawal = %.2f; want %.2f (gross sum: 1000 used + 4000 surplus)", result.RMDWithdrawal, monthlyRMD)
	}
	if math.Abs(result.WithdrawalFromTaxDeferred-monthlyRMD) > 0.01 {
		t.Errorf("result.WithdrawalFromTaxDeferred = %.2f; want %.2f (gross)", result.WithdrawalFromTaxDeferred, monthlyRMD)
	}
}
