package accounts

import (
	"math"
	"sort"
	"time"

	"budget2/internal/models"
)

// DefaultLowBalanceThreshold is the fallback used when an account's
// LowBalanceThreshold is zero. The threshold is only meaningful for the
// checking and savings kinds; see GLOSSARY.md ("Account kind").
const DefaultLowBalanceThreshold = 500.0

// projectionHorizonDays is the roll-forward window. The spec (design doc §4)
// fixes it at 35 days.
const projectionHorizonDays = 35

// LowBalanceApplies reports whether the low-balance threshold is meaningful
// for accounts of this kind: only checking and savings
// (models.Account.LowBalanceThreshold, design doc §4). A credit card's
// balance is negative by nature (money owed) and a brokerage balance is not
// spendable cash, so comparing either to a cash floor would flag it
// permanently. Every surface that reports a low-balance flag or a funding
// projection — the dashboard accounts card and the MCP get_accounts /
// get_balance_projection tools — must gate on this before comparing a
// balance to the threshold.
func LowBalanceApplies(kind models.AccountKind) bool {
	return kind == models.AccountKindChecking || kind == models.AccountKindSavings
}

// ProjectionResult is the advisory funding forecast for one account. It is
// distinct from BalanceResult (a balance-at-a-date) so a reader can tell at a
// glance which kind of answer a value holds.
//
// Advisory only: computing a projection never writes anything to the ledger
// or to any sidecar (see Project, which performs only reads and arithmetic).
type ProjectionResult struct {
	// AccountID echoes the account the projection is for.
	AccountID string

	// AsOf is the caller-supplied "as of" date the projection rolls forward
	// from.
	AsOf time.Time

	// Available is false when BalanceAt reported no usable anchor at or
	// before AsOf. In that case no projection is produced: an unknown
	// balance and a zero balance are different facts (GLOSSARY.md
	// "BalanceAnchor") and the UI renders them differently, so the
	// projection must not invent a starting amount.
	Available bool

	// Threshold is the low-balance threshold the projection used: the
	// account's own LowBalanceThreshold, or DefaultLowBalanceThreshold when
	// that is zero. Reported so the UI can label the line it drew.
	Threshold float64

	// Crossing is the first date in the window the projected balance crosses
	// strictly below Threshold. It is the zero Time when Available is false
	// or when the balance never crosses below the threshold in the window.
	Crossing time.Time

	// Minimum is the lowest projected balance over the window. Zero when
	// Available is false. It is NOT clamped to the threshold; callers use it
	// to size the shortfall.
	Minimum float64

	// SuggestedTopUp is the shortfall (Threshold - Minimum) rounded up to
	// the nearest $100, but never negative. Zero when there is no crossing
	// (healthy) or when unavailable. The rounding is the deliberate kind the
	// spec calls for; intermediate arithmetic is in full float64 precision.
	SuggestedTopUp float64

	// ReferenceAmount is the median of confirmed inbound paired-transfer
	// amounts into this account, so the UI can say "you usually move $X".
	// HasReference is false when no such history exists; ReferenceAmount is
	// then zero and MUST NOT be presented as a number to move.
	ReferenceAmount float64
	HasReference    bool
}

// Project computes an advisory 35-day funding forecast for acct.
//
// The projection rolls the account's balance forward from
// BalanceAt(acct, txs, asOf) one day at a time over projectionHorizonDays
// days, applying expected recurring items for THIS ACCOUNT ONLY -- each
// RecurringPayment is attributed to exactly one account (recurringOwnerAccount,
// most-recent-leg wins) and applied only when that owner is acct.ID, so an
// item belonging to a different account cannot move this account's balance,
// and a single series is never applied to more than one account.
//
// Recurring items are applied on their NextExpected date, then again at each
// whole-frequency interval after it that falls within the window, as an
// outflow of the recurring Amount (the engine stores amounts positive, and
// recurring items are outflows, so each occurrence subtracts Amount). Advisory
// only: nothing is written to the ledger or any sidecar.
//
// When BalanceAt reports Available: false (no anchor at or before asOf), the
// projection is unavailable: Available is false and every other field is
// zero. Callers MUST distinguish this from a healthy projection (no
// crossing) -- the UI renders "unknown" and "healthy" differently.
func Project(acct models.Account, txs []models.Transaction, asOf time.Time, recurring []models.RecurringPayment) (ProjectionResult, error) {
	out := ProjectionResult{AccountID: acct.ID, AsOf: asOf, Threshold: thresholdFor(acct)}

	// Project speaks in the DATA'S UTC calendar, not the caller's local one:
	// the starting-balance cutoff, the walk grid, and every occurrence label
	// below are all derived from asOfUTC, the UTC instant asOf names. A
	// host's local timezone must not change any figure this function
	// reports. out.AsOf still echoes the caller's original asOf, Location
	// and all, unchanged -- only the internal day-boundary math is
	// normalized.
	asOfUTC := asOf.UTC()

	// Passing asOfUTC (rather than asOf) into BalanceAt matters: BalanceAt's
	// cutoff is dayOf(at), which rebuilds the calendar day in at's OWN
	// Location. Transactions and anchors in this codebase are UTC (parsed
	// from CSV), so a Local asOf would put the cutoff on a different
	// calendar day than the data it is being compared against, silently
	// dropping (or double-counting) a transaction dated on asOf's own UTC
	// day. dayOf itself is unchanged; only what we hand it is.
	start, err := BalanceAt(acct, txs, asOfUTC)
	if err != nil {
		return ProjectionResult{}, err
	}
	if !start.Available {
		// No anchor -> no projection. Do not invent a zero starting balance;
		// an unknown balance and a zero balance are different facts.
		return out, nil
	}
	out.Available = true

	// Build the per-day recurring cashflow deltas for THIS ACCOUNT ONLY.
	// A RecurringPayment is attributed to exactly one account -- the one
	// recurringOwnerAccount names -- never to more than one; see that
	// function's doc comment for the full rule (most-recent-leg wins) and
	// why (the engine groups outflows by merchant across the whole ledger,
	// so a series whose payment account changed carries legs on more than
	// one account, and applying the full Amount on every account that ever
	// appears in its legs would double-count -- or N-times-count -- a
	// single real-world payment). The test "other accounts' recurring
	// items are excluded" pins the common case: a series with no legs on
	// this account is skipped entirely.
	//
	// Each qualifying item is scheduled forward from its NextExpected date
	// at its Frequency interval: occurrences that fall strictly after asOf
	// and on or before asOf+horizon are applied. RecurringPayment.Amount is
	// positive and recurring items are outflows, so each occurrence
	// subtracts Amount.
	//
	// The grid is keyed by calendarDayKey, not by a raw time.Time value: Go
	// compares time.Time map keys by struct fields INCLUDING the *Location
	// pointer, and time.Local and time.UTC are distinct Location objects
	// even when they denote the same zone, so a raw-time.Time-keyed map
	// entry written from one Location and read from another would never
	// match even though the instants (and calendar days) are identical.
	// calendarDayKey drops the Location entirely and identifies a day by
	// its UTC (year, month, day) alone -- see utcCalendarDay.
	//
	// Both asOfDay (the walk grid's origin) and occ (each occurrence's
	// running position) are built from dayOf() applied to an already-UTC
	// value (asOfUTC and rp.NextExpected.UTC() respectively), so the advance
	// filter below (which compares INSTANTS via After) and the walk (which
	// reads LABELS via utcCalendarDay) agree about what "day" means: both
	// are UTC calendar days. Before this fix the filter compared instants in
	// asOf's own Location while the walk read UTC-derived labels, so an
	// occurrence strictly after asOf could land on a label the walk never
	// visited and silently vanish.
	byDay := make(map[calendarDayKey]float64)
	asOfDay := dayOf(asOfUTC)
	horizonEnd := asOfDay.AddDate(0, 0, projectionHorizonDays)
	for _, rp := range recurring {
		if recurringOwnerAccount(rp) != acct.ID {
			continue
		}
		interval, ok := frequencyIntervalDays(rp)
		if !ok {
			continue
		}
		// Walk occurrences forward from NextExpected. Advance past any
		// that are on or before asOf, then apply those within the window.
		occ := dayOf(rp.NextExpected.UTC())
		// Guard against a malformed interval that would spin forever: cap
		// the advance loop at a sane bound (a year of daily occurrences).
		for guard := 0; !occ.After(asOfDay) && guard < 400; guard++ {
			occ = occ.AddDate(0, 0, interval)
		}
		for guard := 0; !occ.After(horizonEnd) && guard < 400; guard++ {
			byDay[utcCalendarDay(occ)] += -rp.Amount
			occ = occ.AddDate(0, 0, interval)
		}
	}

	balance := start.Amount
	// The projected balance "as of" the start day is the starting balance;
	// crossings are detected on subsequent days. We walk [asOf+1, asOf+horizon]
	// inclusive, applying that day's recurring delta.
	minimum := balance
	var crossing time.Time
	crossed := false
	for d := 1; d <= projectionHorizonDays; d++ {
		day := asOfDay.AddDate(0, 0, d)
		balance += byDay[utcCalendarDay(day)]
		if balance < minimum {
			minimum = balance
		}
		if !crossed && balance < out.Threshold-floatTolerance {
			crossing = day
			crossed = true
		}
	}

	out.Minimum = minimum
	if crossed {
		out.Crossing = crossing
		out.SuggestedTopUp = roundUpToHundred(out.Threshold - minimum)
	}

	out.ReferenceAmount, out.HasReference = medianInboundPairedTransfer(acct, txs)

	return out, nil
}

// calendarDayKey is a Location-independent identifier for a UTC calendar
// day, used as the key of Project's byDay grid. A plain (year, month, day)
// struct -- rather than a time.Time -- sidesteps Go's map-key trap: a
// time.Time's equality (and hence its map-key identity) includes its
// *Location pointer, so two time.Time values naming the same instant but
// carrying different Location objects (e.g. one from a UTC transaction date,
// one from a time.Local asOf) would never collide as map keys even though
// Equal() reports them identical. calendarDayKey has no Location field, so
// it cannot fall into that trap.
type calendarDayKey struct {
	year  int
	month time.Month
	day   int
}

// utcCalendarDay returns t's calendar day expressed in UTC, as a
// Location-independent key. Used for both the recurring-occurrence labels
// and the walk-grid labels in Project, so every read and write of byDay
// names the same UTC day regardless of what Location t itself carries.
func utcCalendarDay(t time.Time) calendarDayKey {
	u := t.UTC()
	return calendarDayKey{u.Year(), u.Month(), u.Day()}
}

// recurringOwnerAccount returns the single account ID a RecurringPayment is
// attributed to, or "" when it has no historical legs at all (an rp with an
// empty Transactions slice is attributed to nothing, matching the
// no-legs-no-attribution behavior of the old per-leg filter).
//
// The recurring engine ("internal/services/insights") groups outflows by
// merchant across the WHOLE ledger, not per account, so a series whose
// payment account changed over its lifetime -- a subscription moved from an
// old card to a new one, say -- carries legs on more than one account. The
// projection used to apply the full rp.Amount to every account that owned
// ANY leg, which double-counted (or N-times-counted) a single real-world
// payment: a $200/month series with legs on two cards produced a $400/month
// aggregate reduction across the two accounts' projections, inventing
// low-balance crossings and SuggestedTopUp recommendations on an account
// that no longer pays it.
//
// The rule implemented here (user decision 2026-08-20a) is MOST-RECENT-LEG
// WINS: a series is attributed to exactly one account, the one whose leg has
// the latest Transactions[i].Date. That account's projection subtracts the
// full occurrence amount; every other account subtracts nothing for this
// series, so summed across all accounts one series never removes more than
// one occurrence per period.
//
// Ties (two legs sharing the exact same Date, to the resolution the data
// carries) are broken by comparing AccountID with Go's default string
// ordering and keeping the lexicographically GREATER one. This is an
// arbitrary but deterministic choice -- any total order on AccountID would
// do equally well -- picked only so that two runs over the same data always
// agree, and so the choice does not depend on the incidental order legs
// appear in rp.Transactions (the comparison is against the current best
// leg, not "first wins", so it is order-independent).
//
// ACCEPTED LIMITATION, recorded here deliberately rather than left as an
// oversight: a series that genuinely alternates between two cards payment
// to payment (rather than migrating once) is projected entirely against
// whichever card holds the single newest leg. Real, still-active occurrences
// on the OTHER card are invisible to that other account's projection. This
// is judged an acceptable trade-off against the status quo bug (which
// invented crossings on both accounts); a fully accurate model would need
// the recurring engine itself to track a per-leg account rather than
// collapsing a series to one Amount, which is out of scope here (the
// insights/recurring detection engine is explicitly out of scope for this
// fix).
func recurringOwnerAccount(rp models.RecurringPayment) string {
	var owner string
	var ownerDate time.Time
	var seen bool
	for _, leg := range rp.Transactions {
		if !seen || leg.Date.After(ownerDate) ||
			(leg.Date.Equal(ownerDate) && leg.AccountID > owner) {
			owner = leg.AccountID
			ownerDate = leg.Date
			seen = true
		}
	}
	return owner
}

// frequencyIntervalDays maps a RecurringPayment's Frequency string to its
// interval in days. The recurring engine ("internal/services/insights")
// emits "weekly", "biweekly", "monthly", "quarterly", "yearly", and
// "ongoing" (variable). A monthly cadence is modeled at 30 days, matching
// the engine's own median-interval band (25-35 days -> "monthly"). Returns
// ok=false for "ongoing" and unrecognized values, which are skipped: an
// "ongoing" relationship has no fixed schedule to roll forward, so applying
// it would fabricate a cadence the engine did not assert.
func frequencyIntervalDays(rp models.RecurringPayment) (int, bool) {
	switch rp.Frequency {
	case "weekly":
		return 7, true
	case "biweekly":
		return 14, true
	case "monthly":
		return 30, true
	case "quarterly":
		return 90, true
	case "yearly":
		return 365, true
	default:
		return 0, false
	}
}

// thresholdFor returns the account's LowBalanceThreshold, falling back to
// DefaultLowBalanceThreshold when it is zero. Reported on the result so the
// UI can label the line it drew.
func thresholdFor(acct models.Account) float64 {
	if acct.LowBalanceThreshold > floatTolerance {
		return acct.LowBalanceThreshold
	}
	return DefaultLowBalanceThreshold
}

// roundUpToHundred returns x rounded up to the nearest 100. A non-positive x
// yields 0 (no shortfall to fund). The rounding is the deliberate
// presentation rounding the spec calls for (behavior 4); it is applied only
// here, never in intermediate arithmetic.
//
// The subtracted floatTolerance before math.Ceil guards the exact-boundary
// case against float64 dust. Cent-valued bank data produces shortfalls whose
// true decimal value is exactly $100 but whose float64 representation is a
// hair above (e.g. 100.00000000000006 from start=800.30, outflow=400.30).
// math.Ceil(1.0000000000000006)*100 would yield 200, over-recommending by a
// full hundred and risking an overdraft of the source account. Shrinking the
// quotient by floatTolerance -- the same epsilon used at lines 154, 214, and
// 250 -- collapses that dust below 1.0 so the exact boundary rounds to
// itself, while a genuinely-past-boundary shortfall like $100.01 (quotient
// 1.0001) still exceeds the epsilon and rounds up to $200.
func roundUpToHundred(x float64) float64 {
	if x < floatTolerance {
		return 0
	}
	return math.Ceil(x/100-floatTolerance) * 100
}

// medianInboundPairedTransfer returns the median of confirmed inbound
// paired-transfer amounts into this account, and whether any such history
// exists. A transfer leg is inbound to this account when it is
// TransactionType == models.Transfer, TransferClass == "paired", its
// AccountID is this account's, and its amount is positive (money received).
// With no such history, HasReference is false and the amount is zero -- the
// caller reports "no prior transfers" rather than inventing a number.
func medianInboundPairedTransfer(acct models.Account, txs []models.Transaction) (float64, bool) {
	var amounts []float64
	for _, tx := range txs {
		if tx.TransactionType != models.Transfer {
			continue
		}
		if tx.TransferClass != "paired" {
			continue
		}
		if tx.AccountID != acct.ID {
			continue
		}
		if tx.Amount <= floatTolerance {
			continue
		}
		amounts = append(amounts, tx.Amount)
	}
	if len(amounts) == 0 {
		return 0, false
	}
	sort.Float64s(amounts)
	n := len(amounts)
	if n%2 == 1 {
		return amounts[n/2], true
	}
	// Even count: median of the two middle values, in full precision.
	return (amounts[n/2-1] + amounts[n/2]) / 2, true
}
