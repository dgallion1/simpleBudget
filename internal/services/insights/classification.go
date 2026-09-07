package insights

import (
	"strings"
	"unicode"

	"budget2/internal/models"
)

// ClassifyRecurring uses original merchant/category evidence, never cadence or
// MajorExpenseName. Unknown and conflicting evidence stays other. Description
// is a fallback only for callers without the original transactions.
func ClassifyRecurring(rp models.RecurringPayment) (string, string) {
	txns := rp.Transactions
	if len(txns) == 0 {
		txns = []models.Transaction{{Description: rp.Description}}
	}
	var subscription, bill, retail bool
	for _, txn := range txns {
		desc := txn.OriginalDescription
		if strings.TrimSpace(desc) == "" {
			desc = txn.Description
		}
		desc = classificationText(desc)
		category := classificationText(txn.Category)
		subscription = subscription || matchesEvidence(desc, []string{"netflix", "spotify", "cloud storage", "cloudbox storage plan", "openai", "claude.ai", "anthropic", "adobe creative cloud", "youtube premium", "amazon prime", "apple music"})
		switch category {
		case "subscription", "subscriptions", "digital subscriptions", "streaming subscriptions":
			subscription = true
		case "utilities", "mortgage", "rent", "loans", "loan", "auto loan", "insurance", "taxes", "internet", "internet access":
			bill = true
		case "groceries", "cash", "pets", "fuel", "dining", "travel", "home maintenance", "shopping":
			retail = true
		}
		bill = bill || matchesEvidence(desc, billKeywords) || matchesEvidence(desc, []string{"auto loan", "autoloan finance bill pmt", "internet", "broadband", "comcast", "xfinity", "at&t"})
		// Explicit subscription products can disambiguate general retailers.
		if !matchesEvidence(desc, []string{"amazon prime"}) {
			retail = retail || matchesEvidence(desc, retailKeywords)
		}
	}
	if retail || (subscription && bill) {
		return models.RecurringOther, "Retail, everyday spending, or conflicting original transaction evidence"
	}
	if bill {
		return models.RecurringBill, "Bill or utility evidence in original merchant or transaction category"
	}
	if subscription {
		return models.RecurringSubscription, "Known subscription service or subscription-specific original transaction category"
	}
	return models.RecurringOther, "Recurring pattern without positive subscription or bill evidence"
}

func classificationText(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

func matchesEvidence(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(" "+text+" ", " "+classificationText(phrase)+" ") {
			return true
		}
	}
	return false
}
