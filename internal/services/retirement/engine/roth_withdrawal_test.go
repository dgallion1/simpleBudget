package engine

import "testing"

func TestWithdrawFromRoth_BasisFirstOrdering(t *testing.T) {
	t.Run("zero need is a no-op", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(0, &balance, &basis)
		if got.Total != 0 || got.Basis != 0 || got.Earnings != 0 {
			t.Fatalf("expected zero result, got %+v", got)
		}
		if balance != 100 || basis != 60 {
			t.Fatalf("balances mutated: balance=%v basis=%v", balance, basis)
		}
	})

	t.Run("small withdrawal pulls only basis", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(40, &balance, &basis)
		if got.Total != 40 || got.Basis != 40 || got.Earnings != 0 {
			t.Fatalf("got %+v, want {40,40,0}", got)
		}
		if balance != 60 || basis != 20 {
			t.Fatalf("after: balance=%v basis=%v, want 60/20", balance, basis)
		}
	})

	t.Run("withdrawal exactly equals basis", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(60, &balance, &basis)
		if got.Total != 60 || got.Basis != 60 || got.Earnings != 0 {
			t.Fatalf("got %+v, want {60,60,0}", got)
		}
		if balance != 40 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 40/0", balance, basis)
		}
	})

	t.Run("large withdrawal exhausts basis then takes earnings", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(75, &balance, &basis)
		if got.Total != 75 || got.Basis != 60 || got.Earnings != 15 {
			t.Fatalf("got %+v, want {75,60,15}", got)
		}
		if balance != 25 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 25/0", balance, basis)
		}
	})

	t.Run("full withdrawal zeroes everything", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(100, &balance, &basis)
		if got.Total != 100 || got.Basis != 60 || got.Earnings != 40 {
			t.Fatalf("got %+v, want {100,60,40}", got)
		}
		if balance != 0 || basis != 0 {
			t.Fatalf("after: balance=%v basis=%v, want 0/0", balance, basis)
		}
	})

	t.Run("over-request capped at balance", func(t *testing.T) {
		balance := 100.0
		basis := 60.0
		got := WithdrawFromRoth(150, &balance, &basis)
		if got.Total != 100 || got.Basis != 60 || got.Earnings != 40 {
			t.Fatalf("got %+v, want capped at balance", got)
		}
	})

	t.Run("basis above balance is clamped down", func(t *testing.T) {
		// Defensive: floating-point drift could leave basis slightly above balance.
		balance := 50.0
		basis := 60.0
		got := WithdrawFromRoth(10, &balance, &basis)
		if got.Total != 10 || got.Basis != 10 || got.Earnings != 0 {
			t.Fatalf("got %+v", got)
		}
		if basis > balance {
			t.Fatalf("basis %v above balance %v after withdraw — should clamp", basis, balance)
		}
	})
}

func TestApplyBigTicketExpense_RothBasisAndEarningsSplit(t *testing.T) {
	taxable := &TaxableAccountState{}
	taxable.AddCash(0)
	td := 0.0
	roth := 100.0
	rothBasis := 60.0

	got := ApplyBigTicketExpenseWithTaxableState(75, false, 0, &td, taxable, &roth, &rothBasis)

	if got.UnfundedExpense != 0 {
		t.Fatalf("UnfundedExpense=%v, want 0", got.UnfundedExpense)
	}
	if got.RothBasisWithdrawal != 60 || got.RothEarningsWithdrawal != 15 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/15", got.RothBasisWithdrawal, got.RothEarningsWithdrawal)
	}
	if roth != 25 || rothBasis != 0 {
		t.Fatalf("balances: roth=%v basis=%v, want 25/0", roth, rothBasis)
	}
}

func TestApplyBigTicketExpense_TaxableThenRothOrdering(t *testing.T) {
	taxable := &TaxableAccountState{}
	taxable.AddCash(50)
	td := 0.0
	roth := 100.0
	rothBasis := 60.0

	got := ApplyBigTicketExpenseWithTaxableState(120, false, 0, &td, taxable, &roth, &rothBasis)

	// 50 from taxable, 70 from Roth (60 basis + 10 earnings).
	if got.UnfundedExpense != 0 {
		t.Fatalf("UnfundedExpense=%v, want 0", got.UnfundedExpense)
	}
	if got.RothBasisWithdrawal != 60 || got.RothEarningsWithdrawal != 10 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/10", got.RothBasisWithdrawal, got.RothEarningsWithdrawal)
	}
}

func TestWithdrawForExpenses_RothBasisAndEarningsSplit(t *testing.T) {
	t.Run("Roth withdrawal splits basis/earnings", func(t *testing.T) {
		td := 0.0
		taxable := 0.0
		roth := 100.0
		rothBasis := 60.0

		got := WithdrawForExpenses(75, 0, false, 0, &td, &taxable, &roth, &rothBasis)

		if got.WithdrawalFromRoth != 75 {
			t.Fatalf("WithdrawalFromRoth=%v, want 75", got.WithdrawalFromRoth)
		}
		if got.WithdrawalFromRothBasis != 60 || got.WithdrawalFromRothEarnings != 15 {
			t.Fatalf("split: basis=%v earnings=%v, want 60/15", got.WithdrawalFromRothBasis, got.WithdrawalFromRothEarnings)
		}
		if roth != 25 || rothBasis != 0 {
			t.Fatalf("balances: roth=%v basis=%v, want 25/0", roth, rothBasis)
		}
	})

	t.Run("non-Roth withdrawal leaves Roth fields at zero", func(t *testing.T) {
		td := 0.0
		taxable := 100.0
		roth := 50.0
		rothBasis := 50.0

		got := WithdrawForExpenses(80, 0, false, 0, &td, &taxable, &roth, &rothBasis)

		if got.WithdrawalFromTaxable != 80 || got.WithdrawalFromRoth != 0 {
			t.Fatalf("taxable=%v roth=%v, want 80/0", got.WithdrawalFromTaxable, got.WithdrawalFromRoth)
		}
		if got.WithdrawalFromRothBasis != 0 || got.WithdrawalFromRothEarnings != 0 {
			t.Fatalf("split should be zero: basis=%v earnings=%v", got.WithdrawalFromRothBasis, got.WithdrawalFromRothEarnings)
		}
	})
}

func TestExecutePortfolioCashFlowWithTaxableState_RothEarningsSurfaced(t *testing.T) {
	td := 0.0
	taxable := &TaxableAccountState{}
	roth := 100.0
	rothBasis := 60.0

	result := ExecutePortfolioCashFlowWithTaxableState(75, 0, false, 0, 0, &td, taxable, &roth, &rothBasis)

	if result.WithdrawalFromRoth != 75 {
		t.Fatalf("WithdrawalFromRoth=%v, want 75", result.WithdrawalFromRoth)
	}
	if result.WithdrawalFromRothBasis != 60 || result.WithdrawalFromRothEarnings != 15 {
		t.Fatalf("split: basis=%v earnings=%v, want 60/15", result.WithdrawalFromRothBasis, result.WithdrawalFromRothEarnings)
	}
}
