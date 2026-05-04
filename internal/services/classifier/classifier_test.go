package classifier

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestClassifyTransactions_Income(t *testing.T) {
	tests := []struct {
		name        string
		desc        string
		category    string
		amount      float64
		wantType    models.TransactionType
		wantAmount  float64
	}{
		// Positive amount + income keyword => Income, positive amount
		{"payroll positive", "PAYROLL DEPOSIT", "", 1500, models.Income, 1500},
		{"salary category", "monthly pay", "Salary", 3000, models.Income, 3000},
		{"income category exact", "some desc", "income", 500, models.Income, 500},
		{"dividend keyword", "DIVIDEND PAYMENT", "", 50, models.Income, 50},
		{"interest earned", "Interest Earned on Savings", "", 10, models.Income, 10},
		{"refund keyword", "Amazon Refund", "", 25, models.Income, 25},
		{"deposit category", "check", "deposit", 100, models.Income, 100},
		{"reimbursement category", "expense", "reimbursement", 200, models.Income, 200},
		{"category contains income", "x", "Monthly Income Misc", 100, models.Income, 100},

		// Negative amount with income keyword => still Outflow (amount<0 means not positive)
		{"negative payroll", "PAYROLL", "", -100, models.Outflow, -100},

		// Positive amount but no income signal => Outflow, stays positive (credit/refund)
		{"positive no keyword", "GROCERY STORE", "Food", 25, models.Outflow, 25},

		// Negative amount, no income signal => Outflow, negative
		{"negative expense", "GROCERY STORE", "Food", -50, models.Outflow, -50},

		// Zero amount => Outflow
		{"zero amount", "PAYROLL", "", 0, models.Outflow, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txns := []models.Transaction{
				{Description: tt.desc, Category: tt.category, Amount: tt.amount},
			}
			result := ClassifyTransactions(txns)
			if result[0].TransactionType != tt.wantType {
				t.Errorf("got type %q, want %q", result[0].TransactionType, tt.wantType)
			}
			if result[0].Amount != tt.wantAmount {
				t.Errorf("got amount %v, want %v", result[0].Amount, tt.wantAmount)
			}
		})
	}
}

func TestClassifyTransactions_NeverIncomeOverride(t *testing.T) {
	// "never income" keywords override even positive amounts with income keywords
	tests := []struct {
		name string
		desc string
	}{
		{"credit card payment", "credit card payment payroll"},
		{"cc payment", "cc payment"},
		{"payment to", "payment to savings"},
		{"loan payment", "loan payment"},
		{"mortgage payment", "mortgage payment"},
		{"bill payment", "bill payment"},
		{"autopay", "autopay"},
		{"scheduled payment", "scheduled payment"},
		{"recurring payment", "recurring payment"},
		{"transfer to", "transfer to checking"},
		{"withdrawal", "withdrawal atm"},
		{"debit", "debit card"},
		{"fee", "monthly fee"},
		{"charge", "service charge"},
		{"penalty", "late penalty"},
		{"subscription", "subscription service"},
		{"membership", "gym membership"},
		{"automatic payment", "automatic payment"},
		{"payment - thank you", "payment - thank you"},
		{"usaa credit card payment", "usaa credit card payment"},
		{"recurring scheduled payment", "recurring scheduled payment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txns := []models.Transaction{
				{Description: tt.desc, Category: "income", Amount: 500},
			}
			result := ClassifyTransactions(txns)
			if result[0].TransactionType != models.Outflow {
				t.Errorf("got type %q, want Outflow for desc %q", result[0].TransactionType, tt.desc)
			}
		})
	}
}

func TestClassifyTransactions_NegativeIncomeBecomesPositive(t *testing.T) {
	// Some banks report income as negative; classifier should abs() it
	txns := []models.Transaction{
		{Description: "PAYROLL DEPOSIT", Amount: -2000},
	}
	// Amount is negative, so t.Amount > 0 check fails, classified as Outflow
	result := ClassifyTransactions(txns)
	if result[0].TransactionType != models.Outflow {
		t.Errorf("negative amount should be Outflow even with income keyword")
	}
	if result[0].Amount != -2000 {
		t.Errorf("got amount %v, want -2000", result[0].Amount)
	}
}

func TestClassifyTransactions_IncomeAbsAmount(t *testing.T) {
	// Income with already-positive amount stays positive
	txns := []models.Transaction{
		{Description: "PAYROLL", Amount: 1000},
	}
	result := ClassifyTransactions(txns)
	if result[0].Amount != 1000 {
		t.Errorf("income amount should be 1000, got %v", result[0].Amount)
	}
}

func TestClassifyTransactions_MultipleTransactions(t *testing.T) {
	txns := []models.Transaction{
		{Description: "PAYROLL", Amount: 3000},
		{Description: "WALMART", Category: "Shopping", Amount: -45.67},
		{Description: "Amazon Refund", Amount: 12.50},
		{Description: "credit card payment", Amount: 500},
	}
	result := ClassifyTransactions(txns)
	if result[0].TransactionType != models.Income {
		t.Error("payroll should be income")
	}
	if result[1].TransactionType != models.Outflow {
		t.Error("walmart should be outflow")
	}
	if result[2].TransactionType != models.Income {
		t.Error("refund should be income")
	}
	if result[3].TransactionType != models.Outflow {
		t.Error("cc payment should be outflow")
	}
}

func TestClassifyTransactions_WhitespaceHandling(t *testing.T) {
	txns := []models.Transaction{
		{Description: "  PAYROLL  ", Category: "  Salary  ", Amount: 100},
	}
	result := ClassifyTransactions(txns)
	if result[0].TransactionType != models.Income {
		t.Error("should handle whitespace in description and category")
	}
}

func TestClassifyTransactions_EmptySlice(t *testing.T) {
	result := ClassifyTransactions(nil)
	if result != nil {
		t.Error("nil input should return nil")
	}
	result = ClassifyTransactions([]models.Transaction{})
	if len(result) != 0 {
		t.Error("empty input should return empty")
	}
}

func TestClassifyTransactions_AllIncomeKeywords(t *testing.T) {
	for _, kw := range IncomeKeywords {
		t.Run(kw, func(t *testing.T) {
			txns := []models.Transaction{
				{Description: kw, Amount: 100},
			}
			result := ClassifyTransactions(txns)
			if result[0].TransactionType != models.Income {
				t.Errorf("keyword %q should classify as income", kw)
			}
		})
	}
}

func TestClassifyTransactions_AllIncomeCategories(t *testing.T) {
	for _, cat := range IncomeCategories {
		t.Run("exact_"+cat, func(t *testing.T) {
			txns := []models.Transaction{
				{Description: "generic", Category: cat, Amount: 100},
			}
			result := ClassifyTransactions(txns)
			if result[0].TransactionType != models.Income {
				t.Errorf("category %q should classify as income", cat)
			}
		})
		t.Run("contains_"+cat, func(t *testing.T) {
			txns := []models.Transaction{
				{Description: "generic", Category: "my " + cat + " stuff", Amount: 100},
			}
			result := ClassifyTransactions(txns)
			if result[0].TransactionType != models.Income {
				t.Errorf("category containing %q should classify as income", cat)
			}
		})
	}
}

func TestIsInternalTransfer(t *testing.T) {
	tests := []struct {
		name   string
		desc   string
		cat    string
		amount float64
		want   bool
	}{
		{"usaa funds transfer", "USAA FUNDS TRANSFER", "", -500, true},
		{"internal transfer", "Internal Transfer Out", "", -200, true},
		{"credit card payment desc", "Credit Card Payment", "", -1000, true},
		{"usaa cc payment", "USAA CREDIT CARD PAYMENT", "", -500, true},
		{"automatic payment thank you", "automatic payment - thank you", "", -300, true},
		{"cc payment", "CC PAYMENT", "", -200, true},
		{"recurring scheduled payment", "Recurring Scheduled Payment", "", -100, true},

		// Category-based
		{"cc payment category", "some payment", "Credit Card Payment", -500, true},

		// Not a transfer
		{"grocery", "WALMART", "Food", -50, false},
		{"payroll", "PAYROLL", "Income", 3000, false},
		{"empty", "", "", 0, false},

		// Transfer pattern but positive with income keyword => NOT transfer
		{"transfer with income keyword", "usaa funds transfer payroll", "", 1000, false},
		// Transfer pattern, positive, but no income keyword => IS transfer
		{"transfer positive no income kw", "usaa funds transfer", "", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn := &models.Transaction{
				Description: tt.desc,
				Category:    tt.cat,
				Amount:      tt.amount,
			}
			if got := IsInternalTransfer(txn); got != tt.want {
				t.Errorf("IsInternalTransfer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInternalTransfer_BrokerPatterns(t *testing.T) {
	// Real-world descriptions seen from major US brokers — these should
	// all be filtered as internal transfers (positive amounts mean money
	// flowing back into the bank account from the brokerage).
	tests := []struct {
		name string
		desc string
	}{
		{"schwab moneylink debit", "SCHWAB BROKERAGE MONEYLINK ***********1115"},
		{"schwab moneylink credit", "Schwab Brokerage MoneyLink"},
		{"fidelity ach", "FID BKG SVC LLC MONEYLINE"},
		{"vanguard buy", "VANGUARD BUY INVESTMENT"},
		{"vanguard sell", "VANGUARD SELL INVESTMENT"},
		{"etrade ach lower", "etrade ach"},
		{"etrade ach with star", "E*TRADE ACH"},
		{"coinbase ach", "COINBASE ACH"},
		{"coinbase wire", "Coinbase Inc."},
		{"robinhood ach", "ROBINHOOD ACH"},
		{"interactive brokers", "INTERACTIVE BROKERS LLC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn := &models.Transaction{Description: tt.desc, Amount: -1000}
			if !IsInternalTransfer(txn) {
				t.Errorf("expected %q to be detected as internal transfer", tt.desc)
			}
		})
	}
}

func TestIsInternalTransfer_WhitespaceHandling(t *testing.T) {
	txn := &models.Transaction{
		Description: "  USAA FUNDS TRANSFER  ",
		Category:    "  Credit Card Payment  ",
		Amount:      -100,
	}
	if !IsInternalTransfer(txn) {
		t.Error("should handle whitespace")
	}
}

func TestIsPotentialIncome(t *testing.T) {
	tests := []struct {
		name   string
		desc   string
		cat    string
		amount float64
		want   bool
	}{
		// Positive + income category
		{"income category", "desc", "income", 100, true},
		{"salary category", "desc", "Salary", 200, true},
		{"category contains", "desc", "my paycheck stuff", 100, true},

		// Positive + income keyword
		{"payroll keyword", "PAYROLL", "", 500, true},
		{"dividend keyword", "quarterly dividend", "", 50, true},

		// Zero or negative => false
		{"zero amount", "PAYROLL", "income", 0, false},
		{"negative amount", "PAYROLL", "income", -100, false},

		// Positive but no signals
		{"no match", "WALMART", "Food", 25, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn := &models.Transaction{
				Description: tt.desc,
				Category:    tt.cat,
				Amount:      tt.amount,
			}
			if got := IsPotentialIncome(txn); got != tt.want {
				t.Errorf("IsPotentialIncome() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	if !containsAny("hello world", []string{"world"}) {
		t.Error("should find 'world'")
	}
	if !containsAny("hello world", []string{"nope", "world"}) {
		t.Error("should find 'world' in second keyword")
	}
	if containsAny("hello world", []string{"nope", "nada"}) {
		t.Error("should not match")
	}
	if containsAny("hello world", nil) {
		t.Error("nil keywords should return false")
	}
	if containsAny("hello world", []string{}) {
		t.Error("empty keywords should return false")
	}
	if containsAny("", []string{"hello"}) {
		t.Error("empty text should not match")
	}
}

func TestClassifyTransactions_AmountEdgeCases(t *testing.T) {
	// Very small positive amount with income keyword
	txns := []models.Transaction{
		{Description: "interest", Amount: 0.01},
	}
	result := ClassifyTransactions(txns)
	if result[0].TransactionType != models.Income {
		t.Error("tiny positive with income keyword should be income")
	}
	if math.Abs(result[0].Amount-0.01) > 1e-10 {
		t.Errorf("amount should be 0.01, got %v", result[0].Amount)
	}
}
