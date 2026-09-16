package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"time"
)

// monthsBetween returns the signed number of whole months from `from` to `to`,
// both "YYYY-MM". ok is false when either string fails to parse, and the
// caller must then leave the schedule alone rather than shift it by a
// meaningless amount.
func monthsBetween(from, to string) (int, bool) {
	fromYear, fromMonth, ok := models.ParseYearMonthParts(from)
	if !ok {
		return 0, false
	}
	toYear, toMonth, ok := models.ParseYearMonthParts(to)
	if !ok {
		return 0, false
	}
	return (toYear-fromYear)*12 + (toMonth - fromMonth), true
}

// shiftScheduleOffsets re-anchors every month-granular schedule offset in
// settings by `elapsed` months, so that a schedule entered against the OLD
// StartDate still means the same calendar month against the new one. Without
// it, advancing StartDate silently pushes every scheduled cash flow one month
// further into the future on every rollover.
//
// Income and expense sources clamp at 0: a source whose start (or end) has
// passed is kept, with a start of 0 meaning "already running" and an end of 0
// meaning "already finished, contributing nothing" — the same treatment
// chain.go's rebase gives a transition. One-time expenses and big-ticket items
// are single-month events and are NOT clamped: a past entry keeps a negative
// month, which retains the user's data while guaranteeing it is never charged
// (the projection loop never reaches a negative month).
//
// An elapsed of 0 is a no-op.
func shiftScheduleOffsets(settings *models.WhatIfSettings, elapsed int) {
	if settings == nil || elapsed == 0 {
		return
	}

	shiftRange := func(startMonth int, endMonth *int) (int, *int) {
		shiftedStart := max(0, startMonth-elapsed)
		if endMonth != nil {
			shiftedEnd := max(0, *endMonth-elapsed)
			return shiftedStart, &shiftedEnd
		}
		return shiftedStart, nil
	}

	for _, sources := range [][]models.IncomeSource{settings.IncomeSources, settings.RemovedIncomeSources} {
		for i := range sources {
			sources[i].StartMonth, sources[i].EndMonth = shiftRange(sources[i].StartMonth, sources[i].EndMonth)
		}
	}

	for _, sources := range [][]models.ExpenseSource{settings.ExpenseSources, settings.RemovedExpenseSources} {
		for i := range sources {
			sources[i].StartMonth, sources[i].EndMonth = shiftRange(sources[i].StartMonth, sources[i].EndMonth)
		}
	}

	for i := range settings.OneTimeExpenses {
		settings.OneTimeExpenses[i].Month -= elapsed
	}

	for _, items := range [][]models.BigTicketItem{settings.BigTicketItems, settings.RemovedBigTicketItems} {
		for i := range items {
			items[i].Month -= elapsed
		}
	}
}

// resolveCurrentMonth runs at the settings boundary, keeping the engine deterministic.
//
// The settings' own StartDate is the anchor every schedule offset is measured
// from, so advancing it to the current month must shift those offsets by the
// same number of months. saveInternal calls this function too, which is why
// the persisted start_date stays a truthful anchor and a load→save→load cycle
// shifts exactly once.
//
// Fixed-date plans (UseCurrentMonth false) are never advanced and never
// shifted. Social Security is unaffected either way: its start month is
// re-derived from the claimant's age on every run.
func resolveCurrentMonth(settings *models.WhatIfSettings, now time.Time) {
	if settings.UseCurrentMonth {
		// An unparseable anchor cannot be re-anchored, so leave every offset
		// exactly where it is rather than shift it by a meaningless amount.
		elapsed, ok := monthsBetween(settings.StartDate, now.Format("2006-01"))
		if ok && elapsed != 0 {
			shiftScheduleOffsets(settings, elapsed)
		}
		settings.StartDate = now.Format("2006-01")
		prepare.ComputeAges(settings)
	}
}

// Refresh a private snapshot even when the manager cache predates this month.
func cloneForCurrentMonth(settings *models.WhatIfSettings) (*models.WhatIfSettings, error) {
	cloned, err := prepare.Clone(settings)
	if err != nil {
		return nil, err
	}
	resolveCurrentMonth(cloned, time.Now())
	return cloned, nil
}
