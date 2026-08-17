package accounts

import (
	"sort"
	"time"

	"budget2/internal/models"
)

// floatTolerance is the epsilon used for float64 comparisons in this
// package. Amounts are stored as float64 and summed in stored precision;
// the repo's existing tests compare money with 1e-9 (see
// internal/services/retirement/guardrails_test.go). We use the same
// convention here so a drift difference below the epsilon reads as zero
// rather than as a spurious 1e-15 gap from float accumulation.
const floatTolerance = 1e-9

// BalanceResult is the return value of BalanceAt. Amount is the rolled
// forward balance as of the end of `at`'s day; Available is false when no
// anchor exists at or before `at`, in which case Amount is zero and MUST
// NOT be rendered as a balance (a zero balance and an unknown balance are
// different facts -- see GLOSSARY.md "BalanceAnchor"). AnchorDate is the
// date of the anchor the balance was rolled forward from; it is the zero
// Time when Available is false.
type BalanceResult struct {
	Amount     float64
	Available  bool
	AnchorDate time.Time
}

// BalanceAt computes an account's balance as of the end of `at`'s day.
//
// The balance is the latest anchor whose day is on or before `at` plus the
// sum of this account's transaction amounts dated strictly after the
// anchor's day and on or before `at`'s day. An anchor is end-of-day, so a
// transaction dated on the anchor's own day is already reflected in the
// anchor's Amount and is NOT added again (this is the single easiest thing
// to get wrong -- there is a test pinning it).
//
// Transactions belonging to other accounts, and unassigned transactions
// (AccountID == ""), are never included.
//
// With no anchor at or before `at`, the balance is unavailable:
// Available is false and Amount is zero. The caller MUST distinguish this
// from a genuine zero balance; the UI renders them differently.
func BalanceAt(acct models.Account, txs []models.Transaction, at time.Time) (BalanceResult, error) {
	anchor, ok := latestAnchorAtOrBefore(acct, at)
	if !ok {
		return BalanceResult{Available: false}, nil
	}

	anchorDay := dayOf(anchor.Date)
	atDay := dayOf(at)

	var sum float64
	for _, tx := range txs {
		if tx.AccountID != acct.ID {
			continue
		}
		txDay := dayOf(tx.Date)
		// Strictly after the anchor's day (the anchor already reflects its
		// own day), and on or before `at`'s day.
		if txDay.After(anchorDay) && !txDay.After(atDay) {
			sum += tx.Amount
		}
	}
	return BalanceResult{
		Amount:     anchor.Amount + sum,
		Available:  true,
		AnchorDate: anchor.Date,
	}, nil
}

// Freshness returns the latest transaction date for this account and whether
// any transaction exists. This is what lets the UI say "data through Aug 12"
// and warn when a stale CSV would otherwise masquerade as a healthy balance.
// The zero Time and false are returned when no transaction belongs to the
// account.
func Freshness(acct models.Account, txs []models.Transaction) (time.Time, bool) {
	var latest time.Time
	var found bool
	for _, tx := range txs {
		if tx.AccountID != acct.ID {
			continue
		}
		if !found || tx.Date.After(latest) {
			latest = tx.Date
			found = true
		}
	}
	return latest, found
}

// DriftReport compares a consecutive pair of anchors: Predicted rolls the
// earlier anchor forward to the later anchor's day using the account's
// transactions; Actual is the later anchor's own stated amount; Difference
// is Actual - Predicted. A non-zero Difference means transactions are
// missing between those exports.
type DriftReport struct {
	From, To   time.Time
	Predicted  float64
	Actual     float64
	Difference float64
}

// Drift compares every consecutive pair of an account's anchors. For each
// pair it rolls the earlier anchor forward using the account's transactions
// dated strictly after the earlier anchor's day and on or before the later
// anchor's day, then compares that prediction against the later anchor's
// own stated amount. One report per consecutive pair; empty when there are
// fewer than two anchors. A non-zero Difference (beyond floatTolerance)
// signals missing rows between exports.
func Drift(acct models.Account, txs []models.Transaction) ([]DriftReport, error) {
	anchors := sortedAnchors(acct)
	if len(anchors) < 2 {
		return nil, nil
	}

	var reports []DriftReport
	for i := 1; i < len(anchors); i++ {
		earlier := anchors[i-1]
		later := anchors[i]

		earlierDay := dayOf(earlier.Date)
		laterDay := dayOf(later.Date)

		var sum float64
		for _, tx := range txs {
			if tx.AccountID != acct.ID {
				continue
			}
			txDay := dayOf(tx.Date)
			// Strictly after the earlier anchor's day (the earlier anchor
			// already reflects its own day), and on or before the later
			// anchor's day.
			if txDay.After(earlierDay) && !txDay.After(laterDay) {
				sum += tx.Amount
			}
		}
		predicted := earlier.Amount + sum
		diff := later.Amount - predicted
		// Round only for presentation of the stored computation: collapse
		// float dust to zero so a 1e-15 gap from accumulation does not
		// masquerade as drift. The underlying computation stays in full
		// precision.
		if absFloat(diff) < floatTolerance {
			diff = 0
		}
		reports = append(reports, DriftReport{
			From:       earlier.Date,
			To:         later.Date,
			Predicted:  predicted,
			Actual:     later.Amount,
			Difference: diff,
		})
	}
	return reports, nil
}

// latestAnchorAtOrBefore returns the anchor whose day is the latest on or
// before `at`, and whether one exists. Anchors are not assumed to be sorted;
// this scans them all so the caller's storage order does not matter. Ties on
// the same day resolve to the last seen, which is arbitrary but stable; an
// account should not carry two anchors on the same day (they would state
// the same end-of-day balance).
func latestAnchorAtOrBefore(acct models.Account, at time.Time) (models.BalanceAnchor, bool) {
	atDay := dayOf(at)
	var best models.BalanceAnchor
	var found bool
	for _, a := range acct.Anchors {
		ad := dayOf(a.Date)
		if ad.After(atDay) {
			continue
		}
		if !found || ad.After(dayOf(best.Date)) {
			best = a
			found = true
		}
	}
	return best, found
}

// sortedAnchors returns a copy of the account's anchors sorted by Date.
func sortedAnchors(acct models.Account) []models.BalanceAnchor {
	out := make([]models.BalanceAnchor, len(acct.Anchors))
	copy(out, acct.Anchors)
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// dayOf truncates a time to its calendar day in the same location, so that
// anchor/transaction date comparison is day-granularity regardless of any
// time component the source data carries. An anchor states the balance as
// of the END of its day, so a transaction on the same calendar day is
// already reflected and must be excluded -- comparing full timestamps would
// wrongly include a same-day transaction whose time was >= the anchor's.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// absFloat returns the absolute value of x without dragging in math just for
// one helper; kept here so the float-tolerance check stays local.
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
