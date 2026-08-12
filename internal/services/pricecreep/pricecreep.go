// Package pricecreep detects recurring merchant charges whose amount has
// drifted upward over their history: for each qualifying merchant group it
// compares the median of the first three occurrences' absolute amounts
// against the median of the last three, and reports the group when the
// increase exceeds 5%. Decreases, and single-transaction outliers among
// the last three (absorbed by the median), never report.
//
// It has no I/O and no package-level mutable state; every exported
// function is deterministic given its inputs, per the ruled algorithm
// standard ported from the b2 analytics build (ANALYTICS_PORT_SPEC.md §2).
package pricecreep

import (
	"math"
	"sort"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/merchants"
)

// Creep describes a merchant group whose charge amount has drifted
// upward across its history.
type Creep struct {
	// Merchant is the human-readable label for the group: the most
	// frequent raw DisplayName-or-Description among its transactions.
	// It is NOT the merchants package's canonical (uppercase-normalized)
	// key, which exists purely for matching and should never be shown
	// to a user.
	Merchant string
	// GroupKey is the canonical normalized merchant key from the
	// merchants package, suitable as a stable join/dedupe key.
	GroupKey string
	// FirstAmount is the median of the first 3 occurrences' absolute
	// amounts, in chronological order.
	FirstAmount float64
	// CurrentAmount is the median of the last 3 occurrences' absolute
	// amounts, in chronological order.
	CurrentAmount float64
	// PctChange is (CurrentAmount-FirstAmount)/FirstAmount*100.
	PctChange   float64
	FirstDate   time.Time
	LastDate    time.Time
	Occurrences int
}

// minOccurrences is the smallest group size price-creep considers; below
// this, "first 3" and "last 3" windows would overlap or the sample is too
// thin to trust as a genuine drift rather than noise.
const minOccurrences = 6

// creepThresholdPct is the minimum percentage increase (comparing median
// of last 3 to median of first 3) required to report a group. Increases at
// or below this threshold, and any decrease, are not reported.
const creepThresholdPct = 5.0

// Detect finds merchant groups with a price-creep pattern in ts.
//
// It considers expenses only (TransactionType == models.Outflow AND
// Amount < 0, compared in absolute value — the app-native expense
// convention used elsewhere in this codebase, e.g. internal/services/
// anomalies; a plain Amount < 0 check alone would also catch
// refund-style negative rows that aren't tagged Outflow, and would miss
// nothing tagged Outflow with a non-negative amount, which should also
// be excluded), applies TransactionSet.Active() first so suppressed
// near-duplicate transactions are excluded, groups the remainder via
// merchants.GroupTransactions (the shared token-subset merge rule with
// the degenerate-key guard), and — among groups with at least 6
// occurrences sorted by date — compares the median of the first 3 abs
// amounts to the median of the last 3. A group is reported only when
// that comparison shows an increase greater than 5%.
//
// Results are sorted by PctChange descending, with a deterministic
// tie-break (occurrence count descending, then GroupKey ascending) so
// output order never depends on map iteration.
func Detect(ts models.TransactionSet) []Creep {
	active := ts.Active()

	var expenses []models.Transaction
	for _, t := range active.Transactions {
		if t.TransactionType == models.Outflow && t.Amount < 0 {
			expenses = append(expenses, t)
		}
	}
	if len(expenses) == 0 {
		return nil
	}

	groups := merchants.GroupTransactions(expenses)

	var creeps []Creep
	for canonKey, txns := range groups {
		if len(txns) < minOccurrences {
			continue
		}

		sorted := make([]models.Transaction, len(txns))
		copy(sorted, txns)
		sort.SliceStable(sorted, func(i, j int) bool {
			if !sorted[i].Date.Equal(sorted[j].Date) {
				return sorted[i].Date.Before(sorted[j].Date)
			}
			// Same-day tie-break: Hash ascending, so the chronological
			// order within a group is fully deterministic regardless of
			// input ordering (map iteration over merchants.GroupTransactions
			// results does not preserve any particular order across
			// same-day rows).
			return sorted[i].Hash < sorted[j].Hash
		})

		firstMedian := median(absAmounts(sorted[:3]))
		lastMedian := median(absAmounts(sorted[len(sorted)-3:]))

		if firstMedian == 0 {
			// Can't compute a meaningful percentage change from a zero
			// baseline; skip rather than divide by zero.
			continue
		}

		pct := (lastMedian - firstMedian) / firstMedian * 100
		if pct <= creepThresholdPct {
			// Decreases and sub-threshold moves never report.
			continue
		}

		creeps = append(creeps, Creep{
			Merchant:      merchants.DisplayLabel(sorted),
			GroupKey:      canonKey,
			FirstAmount:   firstMedian,
			CurrentAmount: lastMedian,
			PctChange:     pct,
			FirstDate:     sorted[0].Date,
			LastDate:      sorted[len(sorted)-1].Date,
			Occurrences:   len(sorted),
		})
	}

	sort.Slice(creeps, func(i, j int) bool {
		if creeps[i].PctChange != creeps[j].PctChange {
			return creeps[i].PctChange > creeps[j].PctChange
		}
		if creeps[i].Occurrences != creeps[j].Occurrences {
			return creeps[i].Occurrences > creeps[j].Occurrences
		}
		return creeps[i].GroupKey < creeps[j].GroupKey
	})

	return creeps
}

// absAmounts returns the absolute value of each transaction's Amount.
func absAmounts(txns []models.Transaction) []float64 {
	vals := make([]float64, len(txns))
	for i, t := range txns {
		vals[i] = math.Abs(t.Amount)
	}
	return vals
}

// median returns the median of vals without mutating the input slice. For
// an even count it averages the two middle values; price-creep always
// calls this with exactly 3 values (an odd count), so it resolves to the
// middle element, but the general form keeps the helper reusable and
// obviously correct.
func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
