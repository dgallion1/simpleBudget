// Package majorexpenses implements pure matching and exception
// detection over a TransactionSet given a list of user-declared
// MajorExpense definitions.
package majorexpenses

import (
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
)

// MatchOptions controls thresholds used to detect exceptions.
type MatchOptions struct {
	// UnknownLargeThreshold is the absolute-dollar floor for flagging an
	// unmatched outflow. <= 0 disables the check.
	UnknownLargeThreshold float64
	// NewMerchantWindow is the trailing window (relative to the dataset's
	// max date) used to detect first-time merchants.
	NewMerchantWindow time.Duration
	// Pins maps transaction Hash → MajorExpense.ID. A pin overrides
	// keyword/amount matching for that one transaction; if the pinned
	// expense ID does not exist in defs, the pin is ignored and the
	// transaction falls through to the regular matching rules.
	Pins map[string]string
}

// MatchResult is the consolidated output the handler renders.
type MatchResult struct {
	Groups     map[string][]models.Transaction
	Unmatched  []models.Transaction
	Exceptions models.ExceptionsReport
	// PinnedHashes is the set of transaction hashes that matched
	// because of an explicit user pin (not keyword/amount). The UI
	// uses this to render an "unpin" affordance on those rows.
	PinnedHashes map[string]bool
}

// Match groups transactions against the declared major expenses and
// computes the three exception buckets.
func Match(ts *models.TransactionSet, defs []models.MajorExpense, opts MatchOptions) MatchResult {
	result := MatchResult{
		Groups:       make(map[string][]models.Transaction),
		Unmatched:    nil,
		PinnedHashes: make(map[string]bool),
		Exceptions: models.ExceptionsReport{
			Threshold:     opts.UnknownLargeThreshold,
			NewWindowDays: int(opts.NewMerchantWindow / (24 * time.Hour)),
		},
	}

	if ts == nil {
		return result
	}

	validIDs := make(map[string]bool, len(defs))
	for _, d := range defs {
		validIDs[d.ID] = true
	}

	for _, t := range ts.Transactions {
		// 1. Honor explicit pin if it points to an existing expense.
		if opts.Pins != nil && t.Hash != "" {
			if id, ok := opts.Pins[t.Hash]; ok && validIDs[id] {
				result.Groups[id] = append(result.Groups[id], t)
				result.PinnedHashes[t.Hash] = true
				continue
			}
		}
		// 2. Fall back to keyword/amount matching.
		if id, ok := MatchTransaction(t, defs); ok {
			result.Groups[id] = append(result.Groups[id], t)
		} else {
			result.Unmatched = append(result.Unmatched, t)
		}
	}

	result.Exceptions.Anomalous = detectAnomalies(result.Groups, defs, result.PinnedHashes)
	result.Exceptions.UnknownLarge = detectUnknownLarge(result.Unmatched, opts.UnknownLargeThreshold)
	result.Exceptions.NewMerchants = detectNewMerchants(ts, opts.NewMerchantWindow)

	return result
}

// AnnotateRecurringPayments fills in RecurringPayment.MajorExpenseName
// for each detected recurring payment whose first transaction maps to a
// declared major expense. Pins on that first transaction take
// precedence over keyword/amount matching, mirroring the engine's main
// matching logic.
//
// Returns a new slice; the input is not mutated.
func AnnotateRecurringPayments(payments []models.RecurringPayment, defs []models.MajorExpense, pins map[string]string) []models.RecurringPayment {
	if len(payments) == 0 {
		return payments
	}

	validIDs := make(map[string]bool, len(defs))
	defByID := make(map[string]models.MajorExpense, len(defs))
	for _, d := range defs {
		validIDs[d.ID] = true
		defByID[d.ID] = d
	}

	out := make([]models.RecurringPayment, len(payments))
	copy(out, payments)
	for i := range out {
		if len(out[i].Transactions) == 0 {
			continue
		}
		first := out[i].Transactions[0]
		// Pin wins.
		if pins != nil && first.Hash != "" {
			if id, ok := pins[first.Hash]; ok && validIDs[id] {
				out[i].MajorExpenseName = defByID[id].Name
				continue
			}
		}
		// Otherwise keyword/amount matching.
		if id, ok := MatchTransaction(first, defs); ok {
			out[i].MajorExpenseName = defByID[id].Name
		}
	}
	return out
}

// exactAmountTolerance is the slack used when matching against an
// "exact" amount (Min == Max). One cent absorbs floating-point noise
// without expanding the match into nearby amounts.
const exactAmountTolerance = 0.01

// MatchTransaction returns the first MajorExpense.ID that matches the
// transaction. The matching rules are:
//
//  1. Keyword + Exact amount (Min == Max > 0):
//     BOTH must match — the keyword AND the amount must be within
//     exactAmountTolerance of Min. This lets users disambiguate
//     transactions that share a generic keyword like "check" by
//     pinning each definition to its own dollar amount.
//
//  2. Keyword only (no amount or a range):
//     Match if any non-empty keyword is a case-insensitive substring
//     of t.Description or t.DisplayName. A range (Min < Max) is
//     used for anomaly detection only when a keyword is present.
//
//  3. Exact amount only (Min == Max > 0, no keyword):
//     Match if abs(t.Amount) is within exactAmountTolerance of Min.
//
//  4. Range only (Min < Max, no keyword):
//     Match if abs(t.Amount) ∈ [Min, Max].
//
// First-def-wins for determinism.
func MatchTransaction(t models.Transaction, defs []models.MajorExpense) (string, bool) {
	desc := strings.ToLower(t.Description)
	display := strings.ToLower(t.DisplayName)
	amt := math.Abs(t.Amount)

	for _, def := range defs {
		hasKeyword := false
		keywordMatched := false
		for _, kw := range def.Keywords {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw == "" {
				continue
			}
			hasKeyword = true
			if strings.Contains(desc, kw) || (display != "" && strings.Contains(display, kw)) {
				keywordMatched = true
				break
			}
		}

		isExactAmount := def.ExpectedMin > 0 && def.ExpectedMin == def.ExpectedMax
		isRange := def.ExpectedMin > 0 && def.ExpectedMax > def.ExpectedMin

		switch {
		case hasKeyword && isExactAmount:
			// AND filter: keyword AND exact amount
			if keywordMatched && math.Abs(amt-def.ExpectedMin) <= exactAmountTolerance {
				return def.ID, true
			}
		case hasKeyword:
			// Keyword only — any range is anomaly-only
			if keywordMatched {
				return def.ID, true
			}
		case isExactAmount:
			if math.Abs(amt-def.ExpectedMin) <= exactAmountTolerance {
				return def.ID, true
			}
		case isRange:
			if amt >= def.ExpectedMin && amt <= def.ExpectedMax {
				return def.ID, true
			}
		}
	}
	return "", false
}

// hasUsableKeyword reports whether the def has at least one non-empty
// keyword. Used by detectAnomalies to skip amount-only definitions
// (their group is range-matched by definition, so anomaly is moot).
func hasUsableKeyword(def models.MajorExpense) bool {
	for _, kw := range def.Keywords {
		if strings.TrimSpace(kw) != "" {
			return true
		}
	}
	return false
}

// detectAnomalies emits an entry for every grouped transaction whose
// abs(amount) falls outside the user's expected range. A bound of 0
// disables that side of the check.
func detectAnomalies(groups map[string][]models.Transaction, defs []models.MajorExpense, pinned map[string]bool) []models.ExceptionAnomalousAmount {
	var out []models.ExceptionAnomalousAmount
	// Iterate defs (not groups map) so output order is deterministic.
	for _, def := range defs {
		txns := groups[def.ID]
		if len(txns) == 0 {
			continue
		}
		if def.ExpectedMin <= 0 && def.ExpectedMax <= 0 {
			continue
		}
		// Amount-only defs match BY range, so by definition every
		// transaction in their group is in range — no anomalies possible.
		if !hasUsableKeyword(def) {
			continue
		}
		// Keyword + exact amount is an AND filter: every matched txn
		// is already within tolerance of the expected amount. The
		// anomaly check would just produce false positives at the
		// edges of the float-precision tolerance.
		if def.ExpectedMin == def.ExpectedMax {
			continue
		}
		for _, t := range txns {
			// Pinned transactions are explicit user intent — never flag.
			if pinned != nil && pinned[t.Hash] {
				continue
			}
			amt := math.Abs(t.Amount)
			belowMin := def.ExpectedMin > 0 && amt < def.ExpectedMin
			aboveMax := def.ExpectedMax > 0 && amt > def.ExpectedMax
			if belowMin || aboveMax {
				out = append(out, models.ExceptionAnomalousAmount{
					MajorExpenseID:   def.ID,
					MajorExpenseName: def.Name,
					Transaction:      t,
					ExpectedMin:      def.ExpectedMin,
					ExpectedMax:      def.ExpectedMax,
				})
			}
		}
	}
	return out
}

// detectUnknownLarge returns unmatched outflows whose abs(amount) meets
// or exceeds the threshold. Income transactions are intentionally
// ignored — this feature is about expenses.
func detectUnknownLarge(unmatched []models.Transaction, threshold float64) []models.ExceptionUnknownLargeTxn {
	if threshold <= 0 {
		return nil
	}
	var out []models.ExceptionUnknownLargeTxn
	for _, t := range unmatched {
		if t.TransactionType != models.Outflow {
			continue
		}
		if math.Abs(t.Amount) >= threshold {
			out = append(out, models.ExceptionUnknownLargeTxn{Transaction: t})
		}
	}
	return out
}

// detectNewMerchants returns transactions whose normalized description
// first appears within the trailing window — i.e., never observed
// before (ref - window). Each new description is emitted once with its
// earliest in-window occurrence.
func detectNewMerchants(ts *models.TransactionSet, window time.Duration) []models.ExceptionNewMerchant {
	if ts == nil || window <= 0 || len(ts.Transactions) == 0 {
		return nil
	}
	ref := ts.MaxDate()
	if ref.IsZero() {
		ref = time.Now()
	}
	cutoff := ref.Add(-window)

	// Build "seen before cutoff" set across all transaction types — even
	// past income should count as "known", so a refund or one-off
	// inflow doesn't pollute the bucket later.
	seenBefore := make(map[string]struct{})
	for _, t := range ts.Transactions {
		if t.Date.Before(cutoff) {
			seenBefore[normalizeDescription(t.Description)] = struct{}{}
		}
	}

	// Only flag outflows as new merchants — income is not what this
	// page is about. This mirrors detectUnknownLarge's behavior.
	earliest := make(map[string]models.Transaction)
	for _, t := range ts.Transactions {
		if t.Date.Before(cutoff) {
			continue
		}
		if t.TransactionType != models.Outflow {
			continue
		}
		key := normalizeDescription(t.Description)
		if key == "" {
			continue
		}
		if _, seen := seenBefore[key]; seen {
			continue
		}
		if existing, ok := earliest[key]; !ok || t.Date.Before(existing.Date) {
			earliest[key] = t
		}
	}

	out := make([]models.ExceptionNewMerchant, 0, len(earliest))
	for k, t := range earliest {
		out = append(out, models.ExceptionNewMerchant{
			Description: k,
			FirstSeen:   t.Date,
			Transaction: t,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FirstSeen.Equal(out[j].FirstSeen) {
			return out[i].Description < out[j].Description
		}
		return out[i].FirstSeen.Before(out[j].FirstSeen)
	})
	return out
}

func normalizeDescription(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
