package classifier

import (
	"math"
	"strings"

	"budget2/internal/models"
)

// Income detection keywords (lowercase)
var IncomeKeywords = []string{
	"payroll", "salary", "paycheck",
	"deposit direct", "direct deposit",
	"refund", "cashback", "cash back",
	"dividend", "interest earned", "interest",
	"bonus", "tax refund", "rebate",
	"transfer in", "check deposit",
	"payment received", "direct dep",
	"reimbursement", "settlement",
	"gift received", "gift", "freelance",
	"commission", "income", "wages",
	"earnings", "rebuild payroll",
	"employer", "pay stub", "net pay",
	"gross pay", "take home",
}

// Income categories (lowercase)
var IncomeCategories = []string{
	"paycheck", "salary", "income",
	"wages", "payroll", "earnings",
	"dividend", "interest", "refund",
	"deposit", "reimbursement",
}

// Keywords that should NEVER be income (lowercase)
var NeverIncomeKeywords = []string{
	"credit card payment", "cc payment", "card payment", "payment to",
	"loan payment", "mortgage payment", "bill payment", "autopay",
	"scheduled payment", "recurring payment", "transfer to", "withdrawal",
	"debit", "fee", "charge", "penalty", "subscription", "membership",
	"automatic payment", "payment - thank you",
	"usaa credit card payment", "recurring scheduled payment",
}

// Internal transfer patterns to filter (lowercase)
var InternalTransferPatterns = []string{
	"usaa funds transfer",
	"internal transfer",
	"credit card payment",
	"usaa credit card payment",
	"automatic payment - thank you",
	"cc payment",
	"recurring scheduled payment",
}

// ClassifyTransactions classifies each transaction as Income or Outflow
func ClassifyTransactions(transactions []models.Transaction) []models.Transaction {
	for i := range transactions {
		transactions[i].TransactionType = classifyTransaction(&transactions[i])

		// Normalize amounts: positive for income, negative for outflow
		// Exception: positive amounts that aren't classified as income are likely
		// credits/refunds - keep them positive so they don't get grouped with outflows
		if transactions[i].TransactionType == models.Income {
			transactions[i].Amount = math.Abs(transactions[i].Amount)
		} else if transactions[i].Amount < 0 {
			// Only make negative if originally negative (actual purchases)
			transactions[i].Amount = -math.Abs(transactions[i].Amount)
		}
		// Positive amounts that aren't income stay positive (credits/refunds)
	}
	return transactions
}

// classifyTransaction determines if a single transaction is income or outflow
func classifyTransaction(t *models.Transaction) models.TransactionType {
	descLower := strings.ToLower(strings.TrimSpace(t.Description))
	catLower := strings.ToLower(strings.TrimSpace(t.Category))

	// First check: if it contains "never income" keywords, it's always an outflow
	for _, kw := range NeverIncomeKeywords {
		if strings.Contains(descLower, kw) {
			return models.Outflow
		}
	}

	// For positive amounts, check if it looks like income
	if t.Amount > 0 {
		// Check income categories (exact match or contains)
		for _, cat := range IncomeCategories {
			if catLower == cat || strings.Contains(catLower, cat) {
				return models.Income
			}
		}

		// Check income keywords in description
		for _, kw := range IncomeKeywords {
			if strings.Contains(descLower, kw) {
				return models.Income
			}
		}
	}

	return models.Outflow
}

// IsInternalTransfer checks if a transaction is an internal transfer
func IsInternalTransfer(t *models.Transaction) bool {
	descLower := strings.ToLower(strings.TrimSpace(t.Description))
	catLower := strings.ToLower(strings.TrimSpace(t.Category))

	// Check transfer patterns
	for _, pattern := range InternalTransferPatterns {
		if strings.Contains(descLower, pattern) {
			// Don't filter if it looks like income
			if t.Amount > 0 && containsAny(descLower, IncomeKeywords) {
				return false
			}
			return true
		}
	}

	// Check category-based filtering
	if catLower == "credit card payment" {
		return true
	}

	return false
}

// IsPotentialIncome checks if a transaction might be income
func IsPotentialIncome(t *models.Transaction) bool {
	if t.Amount <= 0 {
		return false
	}

	descLower := strings.ToLower(t.Description)
	catLower := strings.ToLower(t.Category)

	// Check categories
	for _, cat := range IncomeCategories {
		if catLower == cat || strings.Contains(catLower, cat) {
			return true
		}
	}

	// Check keywords
	return containsAny(descLower, IncomeKeywords)
}

// containsAny checks if text contains any of the keywords
func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
