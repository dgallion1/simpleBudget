// Package insights holds pattern-detection logic over a transaction
// history -- currently recurring-payment detection -- factored out of
// internal/handlers/insights so it can be called directly by non-HTTP
// consumers (e.g. the MCP get_recurring tool) without depending on the
// handlers package.
package insights

import (
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/merchants"
)

// retailKeywords identifies transactions that are store/retail purchases, not subscriptions.
// These have recurring patterns due to frequent shopping but are not subscription services.
var retailKeywords = []string{
	"walmart", "target", "costco", "kroger", "publix", "aldi",
	"wegmans", "trader joe", "whole foods", "safeway", "albertsons",
	"home depot", "lowes", "lowe's", "menards",
	"walgreens", "cvs", "rite aid",
	"amazon", "ebay", "etsy",
	"bj's", "sam's club",
	"pet supplies", "petsmart", "petco",
	"dollar general", "dollar tree", "five below",
	"restaurant", "grubhub", "doordash", "uber eats",
	"gas station", "shell", "exxon", "bp ", "chevron", "speedway",
	"starbucks", "dunkin", "mcdonald", "wendy", "chipotle",
	"grocery", "groceries", "market",
	"wine", "spirits", "liquor", "cafe", "diner", "grill",
	"food co op", "abundance",
}

// billKeywords identifies transactions that are utility bills, not subscription services.
var billKeywords = []string{
	"electric", "g&e", "power", "energy",
	"water", "sewer",
	"rent ", "mortgage", "hoa",
	"insurance", "casualty", "geico", "allstate", "state farm",
	"credit card payment", "loan payment", "payment - thank",
	"transfer", "funds transfer",
	"tax", "dmv", "government",
}

// IsSubscription classifies a recurring payment as a subscription service.
// Subscriptions are regular payments that are not retail stores or utility bills.
// This includes both fixed-amount (Netflix) and variable-amount (API billing) services.
func IsSubscription(rp models.RecurringPayment) bool {
	desc := strings.ToLower(rp.Description)

	// Check against retail keywords - stores you shop at are not subscriptions
	for _, kw := range retailKeywords {
		if strings.Contains(desc, kw) {
			return false
		}
	}

	// Check against bill keywords - utilities/bills are not subscriptions
	for _, kw := range billKeywords {
		if strings.Contains(desc, kw) {
			return false
		}
	}

	// Fixed-interval payments (monthly/yearly/quarterly) are subscriptions
	if rp.Frequency == "monthly" || rp.Frequency == "yearly" || rp.Frequency == "quarterly" {
		return true
	}

	// "ongoing" payments that aren't retail or bills are likely subscription services
	// with variable billing (e.g., API usage, metered services)
	if rp.Frequency == "ongoing" {
		return true
	}

	return false
}

func recurringFreshnessWindow(intervalDays float64) float64 {
	switch {
	case intervalDays <= 0:
		return 90
	case intervalDays <= 9:
		return 21
	case intervalDays <= 16:
		return 45
	case intervalDays <= 35:
		return 90
	case intervalDays <= 95:
		return 180
	default:
		return 455
	}
}

func recurringPaymentIsActive(lastDate time.Time, intervalDays float64, now time.Time) bool {
	daysSinceLastPayment := now.Sub(lastDate).Hours() / 24
	return daysSinceLastPayment <= recurringFreshnessWindow(intervalDays)
}

func ReferenceDate(ts *models.TransactionSet, referenceDate time.Time) time.Time {
	if !referenceDate.IsZero() {
		return referenceDate
	}
	if ts != nil {
		if maxDate := ts.MaxDate(); !maxDate.IsZero() {
			return maxDate
		}
	}
	return time.Now()
}

func TransactionSetForRecurring(ts *models.TransactionSet, referenceDate time.Time) *models.TransactionSet {
	if ts == nil || referenceDate.IsZero() {
		return ts
	}
	return ts.FilterByDateRange(ts.MinDate(), referenceDate)
}

// mergeSimilarGroups consolidates transaction groups where descriptions refer to
// the same vendor. It strips common suffixes like ".com", ".net", etc. and then
// checks if the shorter stripped name is a prefix of the longer one. When a match
// is found, transactions are merged under the shorter (more canonical) description.
func mergeSimilarGroups(groups map[string][]models.Transaction) map[string][]models.Transaction {
	// Strip common domain/URL suffixes for comparison
	strip := func(s string) string {
		s = strings.TrimSuffix(s, ".com")
		s = strings.TrimSuffix(s, ".net")
		s = strings.TrimSuffix(s, ".org")
		s = strings.TrimSuffix(s, ".io")
		s = strings.TrimSuffix(s, ".co")
		return s
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	// Sort by length so shorter (canonical) names come first
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) < len(keys[j])
	})

	merged := make(map[string][]models.Transaction)
	// Maps a stripped key to the canonical group key
	canonical := make(map[string]string)

	for _, key := range keys {
		stripped := strip(key)
		// Check if this stripped name matches or is a prefix of an existing canonical,
		// or if an existing canonical is a prefix of this one
		found := false
		for canon, canonKey := range canonical {
			if strings.HasPrefix(stripped, canon) || strings.HasPrefix(canon, stripped) {
				// Merge into the existing canonical group
				merged[canonKey] = append(merged[canonKey], groups[key]...)
				// If the new stripped name is shorter, update the canonical mapping
				if len(stripped) < len(canon) {
					// Re-map: the shorter name becomes canonical
					txns := merged[canonKey]
					delete(merged, canonKey)
					merged[key] = txns
					delete(canonical, canon)
					canonical[stripped] = key
				}
				found = true
				break
			}
		}
		if !found {
			merged[key] = groups[key]
			canonical[stripped] = key
		}
	}

	return merged
}

func DetectRecurring(ts *models.TransactionSet) []models.RecurringPayment {
	return DetectRecurringAt(ts, time.Time{})
}

func DetectRecurringAt(ts *models.TransactionSet, referenceDate time.Time) []models.RecurringPayment {
	var recurring []models.RecurringPayment

	relevantTransactions := TransactionSetForRecurring(ts, referenceDate)
	outflows := relevantTransactions.FilterByType(models.Outflow)
	if outflows.Len() < 2 {
		return recurring
	}

	// Group transactions into merchant clusters via the shared merchants
	// package (token-subset merge rule with the degenerate-key guard,
	// ported from the ruled b2 algorithm standard) instead of the old
	// hand-rolled per-transaction lowercase-description key. Each
	// cluster is then re-keyed under a human-readable display label
	// (merchants.DisplayLabel) — the merchants canonical key itself is
	// UPPERCASE-normalized purely for matching and must never leak into
	// RecurringPayment.Description or any other user-visible string.
	canonGroups := merchants.GroupTransactions(outflows.Transactions)
	groups := make(map[string][]models.Transaction, len(canonGroups))
	for _, txns := range canonGroups {
		label := merchants.DisplayLabel(txns)
		groups[label] = append(groups[label], txns...)
	}

	// The ruled token-subset rule intentionally does NOT merge two
	// single-token keys unless they are exactly equal (the degenerate-key
	// guard that stops bare processor prefixes like "SQ" from bridging
	// unrelated merchants) — so it does not, on its own, merge e.g.
	// "lucid" and "lucidmotors.com" (both single-token: "LUCID" and
	// "LUCIDMOTORS.COM"). mergeSimilarGroups is layered on top of the
	// canonical clusters to preserve that pre-existing domain-suffix
	// consolidation, which is orthogonal to (and not prohibited by) the
	// ruled standard. See the P3 report for this flagged discrepancy.
	groups = mergeSimilarGroups(groups)

	// Track which descriptions matched strict criteria
	strictMatches := make(map[string]bool)
	now := ReferenceDate(ts, referenceDate)

	// First pass: strict recurring detection (consistent amounts and intervals)
	for desc, txns := range groups {
		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		if len(intervals) == 0 {
			continue
		}

		sortedIntervals := make([]float64, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Float64s(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		var sumSq float64
		for _, interval := range intervals {
			diff := interval - medianInterval
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(len(intervals)))

		if stdDev > 7 {
			continue
		}

		var amounts []float64
		for _, t := range txns {
			amounts = append(amounts, math.Abs(t.Amount))
		}
		avgAmount := 0.0
		for _, a := range amounts {
			avgAmount += a
		}
		avgAmount /= float64(len(amounts))

		amountConsistent := true
		for _, a := range amounts {
			if math.Abs(a-avgAmount)/avgAmount > 0.10 {
				amountConsistent = false
				break
			}
		}

		if !amountConsistent {
			continue
		}

		var frequency string
		var annualMultiplier float64

		switch {
		case medianInterval >= 5 && medianInterval <= 9:
			if len(txns) < 4 {
				continue
			}
			frequency = "weekly"
			annualMultiplier = 52
		case medianInterval >= 12 && medianInterval <= 16:
			if len(txns) < 4 {
				continue
			}
			frequency = "biweekly"
			annualMultiplier = 26
		case medianInterval >= 25 && medianInterval <= 35:
			frequency = "monthly"
			annualMultiplier = 12
		case medianInterval >= 85 && medianInterval <= 95:
			frequency = "quarterly"
			annualMultiplier = 4
		case medianInterval >= 350 && medianInterval <= 380:
			frequency = "yearly"
			annualMultiplier = 1
		default:
			continue
		}

		confidence := 1.0 - (stdDev / medianInterval)
		if confidence < 0.5 {
			continue
		}

		lastDate := txns[len(txns)-1].Date
		if !recurringPaymentIsActive(lastDate, medianInterval, now) {
			continue
		}
		nextExpected := lastDate.AddDate(0, 0, int(medianInterval))

		recurring = append(recurring, models.RecurringPayment{
			Description:  desc,
			Amount:       avgAmount,
			Frequency:    frequency,
			LastDate:     lastDate,
			NextExpected: nextExpected,
			AnnualCost:   avgAmount * annualMultiplier,
			Occurrences:  len(txns),
			Confidence:   confidence,
			Transactions: txns,
		})
		strictMatches[desc] = true
	}

	// Second pass: ongoing payment detection (variable amounts but consistent relationship)
	for desc, txns := range groups {
		// Skip if already matched by strict criteria
		if strictMatches[desc] {
			continue
		}

		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		firstDate := txns[0].Date
		lastDate := txns[len(txns)-1].Date

		// Must span at least 60 days (2+ months of relationship)
		spanDays := lastDate.Sub(firstDate).Hours() / 24
		if spanDays < 60 {
			continue
		}

		// Must have activity within last 90 days (still active)
		if !recurringPaymentIsActive(lastDate, 0, now) {
			continue
		}
		daysSinceLastPayment := now.Sub(lastDate).Hours() / 24

		// Calculate total and average amount
		var totalAmount float64
		for _, t := range txns {
			totalAmount += math.Abs(t.Amount)
		}
		avgAmount := totalAmount / float64(len(txns))

		// Calculate annual cost based on actual spending rate
		months := spanDays / 30.0
		if months < 1 {
			months = 1
		}
		monthlyRate := totalAmount / months
		annualCost := monthlyRate * 12

		// Confidence based on recency and number of transactions
		confidence := 0.7
		if daysSinceLastPayment < 30 {
			confidence = 0.9
		} else if daysSinceLastPayment < 60 {
			confidence = 0.8
		}

		// Estimate next expected based on average interval
		avgInterval := spanDays / float64(len(txns)-1)
		nextExpected := lastDate.AddDate(0, 0, int(avgInterval))

		recurring = append(recurring, models.RecurringPayment{
			Description:  desc,
			Amount:       avgAmount,
			Frequency:    "ongoing",
			LastDate:     lastDate,
			NextExpected: nextExpected,
			AnnualCost:   annualCost,
			Occurrences:  len(txns),
			Confidence:   confidence,
			Transactions: txns,
		})
		strictMatches[desc] = true
	}

	// Third pass: amount-based grouping for payments with different descriptions
	// but identical amounts at regular intervals (e.g., check payments and bill pay
	// to the same vendor like "Check #996578" and "Lucid" both at $1,580.43).
	recurring = append(recurring, detectByAmount(strictMatches, groups, now)...)

	sort.Slice(recurring, func(i, j int) bool {
		return recurring[i].AnnualCost > recurring[j].AnnualCost
	})

	if len(recurring) > 20 {
		recurring = recurring[:20]
	}

	return recurring
}

// detectByAmount finds recurring payments across different descriptions that share
// the same exact amount and regular intervals. This catches cases like a car payment
// that switches from checks to direct bill pay mid-stream.
func detectByAmount(alreadyMatched map[string]bool, groups map[string][]models.Transaction, now time.Time) []models.RecurringPayment {
	var results []models.RecurringPayment

	// Collect all unmatched transactions
	var unmatched []models.Transaction
	for desc, txns := range groups {
		if alreadyMatched[desc] {
			continue
		}
		unmatched = append(unmatched, txns...)
	}

	// Group unmatched transactions by exact amount (rounded to cents)
	amountGroups := make(map[int64][]models.Transaction)
	for _, t := range unmatched {
		// Key on cents to avoid floating point comparison issues
		cents := int64(math.Round(math.Abs(t.Amount) * 100))
		// Skip tiny amounts — not meaningful recurring payments
		if cents < 500 { // $5.00 minimum
			continue
		}
		amountGroups[cents] = append(amountGroups[cents], t)
	}

	for _, txns := range amountGroups {
		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		if len(intervals) == 0 {
			continue
		}

		sortedIntervals := make([]float64, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Float64s(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		var sumSq float64
		for _, interval := range intervals {
			diff := interval - medianInterval
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(len(intervals)))

		// Allow slightly more variance than pass 1 since descriptions differ
		if stdDev > 10 {
			continue
		}

		var frequency string
		var annualMultiplier float64

		switch {
		case medianInterval >= 25 && medianInterval <= 35:
			frequency = "monthly"
			annualMultiplier = 12
		case medianInterval >= 85 && medianInterval <= 95:
			frequency = "quarterly"
			annualMultiplier = 4
		case medianInterval >= 350 && medianInterval <= 380:
			frequency = "yearly"
			annualMultiplier = 1
		default:
			continue
		}

		confidence := 1.0 - (stdDev / medianInterval)
		if confidence < 0.4 {
			continue
		}
		// Reduce confidence slightly since we're matching on amount alone
		confidence *= 0.9

		// Must have activity within last 90 days
		lastDate := txns[len(txns)-1].Date
		if !recurringPaymentIsActive(lastDate, medianInterval, now) {
			continue
		}

		avgAmount := math.Abs(txns[0].Amount)
		nextExpected := lastDate.AddDate(0, 0, int(medianInterval))

		// Use the most recent transaction's description as the label
		desc := strings.ToLower(strings.TrimSpace(txns[len(txns)-1].Description))

		results = append(results, models.RecurringPayment{
			Description:  desc,
			Amount:       avgAmount,
			Frequency:    frequency,
			LastDate:     lastDate,
			NextExpected: nextExpected,
			AnnualCost:   avgAmount * annualMultiplier,
			Occurrences:  len(txns),
			Confidence:   confidence,
			Transactions: txns,
		})
	}

	return results
}
