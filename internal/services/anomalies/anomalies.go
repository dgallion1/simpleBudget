// Package anomalies provides pure, side-effect-free anomaly detection over
// a models.TransactionSet. It has no I/O and no package-level mutable
// state; Detect is deterministic given its input.
//
// # Scope
//
// Detect operates on expenses only: TransactionType == models.Outflow AND
// Amount < 0 (the app-native expense convention used elsewhere in this
// codebase — a plain Amount < 0 check alone would also catch refund-style
// negative rows that aren't tagged Outflow, and would miss nothing tagged
// Outflow with a non-negative amount, which should also be excluded). All
// math is done on absolute values. It calls TransactionSet.Active() itself,
// so Suppressed transactions are never considered. The caller controls the
// time window entirely by pre-filtering the TransactionSet it passes in
// (e.g. via FilterByDateRange) — Detect has no notion of "recent" or "this
// month".
//
// # The ruled algorithm (ANALYTICS_PORT_SPEC.md §2)
//
// Three detection methods, each producing candidate flags on individual
// transactions:
//
//  1. mad_merchant: transactions are grouped into merchant peer groups via
//     merchants.GroupTransactions (token-subset merge rule). Groups with at
//     least 4 members ("qualifying recurring groups") are tested with a
//     robust z-score against the group's own median/MAD of absolute
//     amounts: z = |x - median| / (1.4826 * MAD), flagged when z > 3.5.
//
//  2. mad_category: transactions are grouped by Category, but ONLY over
//     transactions that do NOT belong to any qualifying merchant group
//     (rule (a) below); categories need at least 6 such rows to form a
//     baseline. Same robust z-score test against the category's median/MAD.
//
//  3. new_merchant: for every merchant group (any size), the earliest
//     transaction in the set is treated as that merchant's first
//     occurrence in this window. If its absolute amount exceeds the 95th
//     percentile of all expense amounts in the set, it is flagged; score
//     is amount/p95. MadZ is always 0 for this method (there is no MAD
//     computation involved) — this is a materiality-only test, documented
//     here rather than left to infer from a zero value.
//
// Three hardening rules apply to BOTH MAD methods, including the MAD == 0
// fallback path (flag when x > 3*median instead of the z-score, used when
// a peer group's MAD collapses to zero because most members share the same
// amount):
//
//   - (a) recurring-group exclusion: a transaction that belongs to a
//     qualifying merchant group (n >= 4) is judged ONLY by mad_merchant; it
//     is excluded entirely from the category-baseline population (not just
//     from category-level flagging, but from the median/MAD computation
//     itself). Without this, a dominant constant recurring charge (e.g. a
//     weekly subscription that is the majority of its category's row
//     count) can collapse the category MAD to 0, which then mis-flags
//     unrelated background noise via the MAD == 0 fallback.
//   - (b) materiality floor: flag only when |x - peer median| >=
//     max(0.5 * peer median, $10). This suppresses immaterial wobble in
//     small, tightly-clustered peer groups from clearing the z-threshold
//     on a dollar amount nobody would call an anomaly.
//   - (c) high-side only: only x > peer median can ever flag. An unusually
//     SMALL expense is not treated as an anomaly in this ruled algorithm.
//
// # Dedupe and scoring
//
// A single transaction may be flagged by more than one method (e.g. both
// mad_merchant and new_merchant, since rule (a) only prevents mad_merchant
// / mad_category overlap, not overlap with new_merchant). Dedupe keeps only
// the highest-scoring candidate per transaction, keyed by Transaction.Hash
// — the same join-key convention used elsewhere in this codebase. This
// requires each input Transaction to carry a Hash that uniquely identifies
// it; if two distinct transactions share a Hash (a pre-existing, known
// limitation of the hash-based dedup convention — see
// Transaction.ComputeHash), Detect treats them as the same transaction for
// anomaly-reporting purposes, same as the rest of the codebase.
//
// Score is the value used both for severity and for dedupe: the robust
// z-score for the MAD normal path, the x/median ratio for the MAD == 0
// fallback path, and the amount/p95 ratio for new_merchant. Severity is
// "high" when score > 6, else "medium".
//
// Results are sorted by Score descending; ties are broken by Hash
// ascending for determinism.
package anomalies

import (
	"math"
	"sort"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/merchants"
)

const (
	madConstant             = 1.4826
	madZThreshold           = 3.5
	madZeroFallbackRatio    = 3.0
	minCategoryRows         = 6
	minMerchantGroupRows    = 4
	newMerchantPercentile   = 95.0
	severityHighThreshold   = 6.0
	materialityFloorDollars = 10.0
	materialityFloorRatio   = 0.5
)

// Anomaly is a single flagged transaction.
type Anomaly struct {
	// Hash is the flagged transaction's Transaction.Hash, carried through
	// as the join key back to the source transaction.
	Hash string

	Date time.Time

	// Description is DisplayName if non-empty, otherwise Description —
	// the same precedence merchants.GroupTransactions uses to derive its
	// raw merchant key (NOT the fuller Transaction.Label() precedence,
	// which also considers EnrichedDescription/MajorExpenseName).
	Description string

	// Amount is the transaction's amount as stored (negative, since
	// Detect only considers expenses).
	Amount float64

	Category string

	// Method is "mad_category", "mad_merchant", or "new_merchant".
	Method string

	// Score is the value used for both severity classification and
	// cross-method dedupe: the robust z-score for the MAD normal path,
	// the x/median ratio for the MAD == 0 fallback path, and the
	// amount/p95 ratio for new_merchant.
	Score float64

	// MadZ is the robust z-score (MAD normal path) or the x/median
	// fallback ratio (MAD == 0 path) for mad_category/mad_merchant
	// methods. It is always 0 for new_merchant, which has no MAD
	// computation.
	MadZ float64

	// PeerGroup is the canonical merchant key (mad_merchant, new_merchant)
	// or the category name (mad_category) the transaction was scored
	// against.
	PeerGroup string

	// Severity is "high" when Score > 6, else "medium".
	Severity string
}

// Detect runs the ruled anomaly-detection algorithm (package doc) over ts
// and returns every flagged transaction, deduped to one Anomaly per
// transaction and sorted by Score descending (ties broken by Hash
// ascending). Detect calls ts.Active() itself, so Suppressed transactions
// are never flagged; it considers only expenses (TransactionType ==
// models.Outflow AND Amount < 0). Returns an empty, non-nil slice (never
// panics) when ts has no qualifying expenses.
func Detect(ts models.TransactionSet) []Anomaly {
	active := ts.Active().Transactions

	expenses := make([]models.Transaction, 0, len(active))
	for _, t := range active {
		if t.TransactionType == models.Outflow && t.Amount < 0 {
			expenses = append(expenses, t)
		}
	}
	if len(expenses) == 0 {
		return []Anomaly{}
	}

	groups := merchants.GroupTransactions(expenses)

	// Rule (a): the category baseline excludes every transaction that
	// belongs to a qualifying (n >= minMerchantGroupRows) merchant group.
	categoryPool := make([]models.Transaction, 0, len(expenses))
	for _, group := range groups {
		if len(group) < minMerchantGroupRows {
			categoryPool = append(categoryPool, group...)
		}
	}

	candidates := make(map[string]Anomaly, len(expenses))
	consider := func(t models.Transaction, method string, madZ, score float64, peerGroup string) {
		cand := Anomaly{
			Hash:        t.Hash,
			Date:        t.Date,
			Description: displayNameOrDescription(t),
			Amount:      t.Amount,
			Category:    t.Category,
			Method:      method,
			Score:       score,
			MadZ:        madZ,
			PeerGroup:   peerGroup,
		}
		if existing, ok := candidates[t.Hash]; !ok || score > existing.Score {
			candidates[t.Hash] = cand
		}
	}

	// mad_category
	for cat, rows := range groupByCategory(categoryPool) {
		if len(rows) < minCategoryRows {
			continue
		}
		abs := absAmounts(rows)
		med := median(abs)
		mad := medianAbsoluteDeviation(abs, med)
		for i, t := range rows {
			if flagged, madZ, score := madFlag(abs[i], med, mad); flagged {
				consider(t, "mad_category", madZ, score, cat)
			}
		}
	}

	// mad_merchant
	for key, rows := range groups {
		if len(rows) < minMerchantGroupRows {
			continue
		}
		abs := absAmounts(rows)
		med := median(abs)
		mad := medianAbsoluteDeviation(abs, med)
		for i, t := range rows {
			if flagged, madZ, score := madFlag(abs[i], med, mad); flagged {
				consider(t, "mad_merchant", madZ, score, key)
			}
		}
	}

	// new_merchant
	p95 := percentile(absAmounts(expenses), newMerchantPercentile)
	for key, rows := range groups {
		first := earliest(rows)
		amt := math.Abs(first.Amount)
		if amt > p95 {
			score := math.Inf(1)
			if p95 > 0 {
				score = amt / p95
			}
			consider(first, "new_merchant", 0, score, key)
		}
	}

	result := make([]Anomaly, 0, len(candidates))
	for _, c := range candidates {
		if c.Score > severityHighThreshold {
			c.Severity = "high"
		} else {
			c.Severity = "medium"
		}
		result = append(result, c)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Hash < result[j].Hash
	})

	return result
}

// madFlag applies the robust z-score test (with MAD == 0 fallback) and the
// materiality (b) and high-side-only (c) hardening rules to a single value
// x against a peer group's median/MAD of absolute amounts. It returns
// whether x is flagged, the MadZ to report (z-score or fallback ratio),
// and the score to use for severity/dedupe (same value as MadZ for both
// paths here).
func madFlag(x, med, mad float64) (flagged bool, madZ float64, score float64) {
	// Rule (c): high-side only.
	if x <= med {
		return false, 0, 0
	}

	if mad > 0 {
		z := (x - med) / (madConstant * mad)
		if z > madZThreshold && isMaterial(x, med) {
			return true, z, z
		}
		return false, 0, 0
	}

	// MAD == 0 fallback. Guarded on med > 0 to avoid a division by zero;
	// unreachable in practice since med is the median of strictly
	// positive absolute expense amounts, but kept explicit for safety.
	if med > 0 && x > madZeroFallbackRatio*med && x != med && isMaterial(x, med) {
		ratio := x / med
		return true, ratio, ratio
	}
	return false, 0, 0
}

// isMaterial applies rule (b): |x - median| >= max(0.5*median, $10).
func isMaterial(x, med float64) bool {
	floor := materialityFloorRatio * med
	if materialityFloorDollars > floor {
		floor = materialityFloorDollars
	}
	return math.Abs(x-med) >= floor
}

// displayNameOrDescription returns t.DisplayName if non-empty, otherwise
// t.Description — the same precedence merchants.GroupTransactions uses for
// its raw merchant key.
func displayNameOrDescription(t models.Transaction) string {
	if t.DisplayName != "" {
		return t.DisplayName
	}
	return t.Description
}

// groupByCategory groups rows by Category, falling back to "Uncategorized"
// for an empty category — the same convention TransactionSet.GroupByCategory
// and TransactionSet.CategoryTotals already use elsewhere in this codebase.
func groupByCategory(rows []models.Transaction) map[string][]models.Transaction {
	result := make(map[string][]models.Transaction)
	for _, t := range rows {
		cat := t.Category
		if cat == "" {
			cat = "Uncategorized"
		}
		result[cat] = append(result[cat], t)
	}
	return result
}

// absAmounts returns the absolute value of each row's Amount, in the same
// order as rows.
func absAmounts(rows []models.Transaction) []float64 {
	out := make([]float64, len(rows))
	for i, t := range rows {
		out[i] = math.Abs(t.Amount)
	}
	return out
}

// median returns the standard median of vals (average of the two middle
// values for even-length input). Returns 0 for an empty slice.
func median(vals []float64) float64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := n / 2
	if n%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// medianAbsoluteDeviation returns the median of |v - med| over vals.
func medianAbsoluteDeviation(vals []float64, med float64) float64 {
	devs := make([]float64, len(vals))
	for i, v := range vals {
		devs[i] = math.Abs(v - med)
	}
	return median(devs)
}

// percentile returns the p-th percentile of vals using linear
// interpolation between closest ranks (numpy's default "linear" method,
// the reference implementation this algorithm was ported from). Returns 0
// for an empty slice.
func percentile(vals []float64, p float64) float64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	if n == 1 {
		return sorted[0]
	}
	idx := (p / 100) * float64(n-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// earliest returns the row with the earliest Date in rows. Ties (equal
// dates) are broken by input order — the first-occurring row in rows wins
// — which is deterministic because merchants.GroupTransactions preserves
// input order within each group and Detect's caller controls the input
// order of the TransactionSet it passes in. Panics if rows is empty (all
// callers only invoke this on non-empty merchant groups).
func earliest(rows []models.Transaction) models.Transaction {
	best := rows[0]
	for _, t := range rows[1:] {
		if t.Date.Before(best.Date) {
			best = t
		}
	}
	return best
}
