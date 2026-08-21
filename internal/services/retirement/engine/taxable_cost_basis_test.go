package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Seeding the taxable account with a real cost basis (FINANCEAPPCONCERNS.md
// §2). Before this, NewTaxableAccountState set CostBasis = MarketValue, so
// every projection began with zero unrealized gain and understated the tax on
// taxable withdrawals for the whole horizon.

func TestNewTaxableAccountState_CostBasisSeeding(t *testing.T) {
	tests := []struct {
		name        string
		basis       *float64
		marketValue float64
		want        float64
		why         string
	}{
		{
			name:        "unset falls back to market value",
			basis:       nil,
			marketValue: 500000,
			want:        500000,
			why:         "legacy zero-embedded-gain behaviour, so saved scenarios do not move",
		},
		{
			name:        "configured basis is used verbatim",
			basis:       models.FloatPtr(280000),
			marketValue: 500000,
			want:        280000,
			why:         "$220,000 of embedded gain",
		},
		{
			name:        "explicit zero means fully appreciated",
			basis:       models.FloatPtr(0),
			marketValue: 500000,
			want:        0,
			why:         "ptr-to-0 is configured, not unset — the whole position is gain",
		},
		{
			name:        "basis above market value clamps down",
			basis:       models.FloatPtr(900000),
			marketValue: 500000,
			want:        500000,
			why:         "no capital-loss deduction is modelled; a stale basis must not manufacture a phantom loss",
		},
		{
			name:        "negative basis clamps to zero",
			basis:       models.FloatPtr(-1),
			marketValue: 500000,
			want:        0,
			why:         "defensive; a negative basis is not a thing",
		},
		{
			name:        "zero market value has zero basis",
			basis:       models.FloatPtr(280000),
			marketValue: 0,
			want:        0,
			why:         "an empty account cannot carry basis (ZeroBalances, chain transitions)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.TaxableCostBasis = tc.basis

			account := NewTaxableAccountState(s, tc.marketValue)
			if math.Abs(account.CostBasis-tc.want) > 0.01 {
				t.Errorf("CostBasis = %.2f, want %.2f (%s)", account.CostBasis, tc.want, tc.why)
			}
			if math.Abs(account.MarketValue-tc.marketValue) > 0.01 {
				t.Errorf("MarketValue = %.2f, want %.2f", account.MarketValue, tc.marketValue)
			}
		})
	}
}

// TestTaxableCostBasis_ChangesRealizedGain is the point of the whole change:
// a withdrawal from an account with real embedded gain realizes real gain.
func TestTaxableCostBasis_ChangesRealizedGain(t *testing.T) {
	const marketValue = 500000.0
	const withdrawal = 50000.0

	unset := models.DefaultWhatIfSettings()
	seeded := models.DefaultWhatIfSettings()
	seeded.TaxableCostBasis = models.FloatPtr(280000) // 44% embedded gain

	unsetAccount := NewTaxableAccountState(unset, marketValue)
	seededAccount := NewTaxableAccountState(seeded, marketValue)

	_, _, unsetGain := unsetAccount.Withdraw(withdrawal)
	_, _, seededGain := seededAccount.Withdraw(withdrawal)

	if unsetGain != 0 {
		t.Errorf("unset basis realized %.2f of gain; want 0 (that is the legacy assumption)", unsetGain)
	}

	// Pro-rata: 10% of the account is sold, so 10% of the $220,000 embedded
	// gain is realized.
	wantSeeded := (marketValue - 280000) * (withdrawal / marketValue)
	if math.Abs(seededGain-wantSeeded) > 0.01 {
		t.Errorf("seeded basis realized %.2f of gain; want %.2f", seededGain, wantSeeded)
	}
	if seededGain <= unsetGain {
		t.Fatal("seeding a real basis must increase realized gain on withdrawal")
	}
	t.Logf("withdrawal of %.0f: unset basis realizes %.2f, real basis realizes %.2f",
		withdrawal, unsetGain, seededGain)
}

// TestTaxableCostBasis_SyncAssumptionsPreservesBasis guards the chain-
// transition path: switching scenarios refreshes yield assumptions but must
// not reset a partially-consumed basis back to the seed.
func TestTaxableCostBasis_SyncAssumptionsPreservesBasis(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.TaxableCostBasis = models.FloatPtr(280000)

	account := NewTaxableAccountState(s, 500000)
	account.Withdraw(50000)
	basisAfterWithdrawal := account.CostBasis

	account.SyncAssumptions(s)

	if math.Abs(account.CostBasis-basisAfterWithdrawal) > 0.01 {
		t.Errorf("SyncAssumptions changed CostBasis from %.2f to %.2f; "+
			"a scenario transition must not re-seed a consumed basis",
			basisAfterWithdrawal, account.CostBasis)
	}
}
