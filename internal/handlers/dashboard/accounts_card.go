package dashboard

import (
	"log"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/insights"
	"budget2/internal/services/storage"
)

// staleAccountDays is how many days an account's latest transaction may lag
// the "as of" date before the account is flagged stale on the dashboard
// card. Bank exports arrive roughly monthly; 45 days covers one missed
// monthly cycle, so a balance computed from a CSV a month and a half old
// is presented with an explicit staleness warning rather than allowed to
// masquerade as current. The spec (design doc §4, "Freshness") requires
// that "a stale CSV must not masquerade as a healthy balance"; this
// constant is the dashboard's definition of stale. It is a presentation
// concern only -- the accounts service's Freshness reports the raw latest
// transaction date; this threshold lives here so the service stays
// UI-agnostic.
const staleAccountDays = 45

// nowFunc returns the dashboard's notion of "today". Production uses
// time.Now; tests substitute a fixed date so the staleness check
// (nowFunc().Sub(latest) > staleAccountDays) is deterministic rather than
// dependent on the date the test is run. The staleness reference is today,
// NOT the balance as-of date, because a CSV whose latest row is from
// February is stale in August regardless of what the dashboard's date
// filter is set to -- comparing against the as-of date would let a stale
// CSV masquerade as fresh whenever the user narrows the date range.
var nowFunc = time.Now

// accountCardState is the distinct rendering state the dashboard accounts
// card uses per account. The states are mutually exclusive and rendered
// visibly differently; the template switches on this field. See the task
// spec (A8, "Accounts card on the dashboard") for the four states.
type accountCardState string

const (
	// accountStateHealthy: balance available and at or above the
	// low-balance threshold, and the account's data is fresh.
	accountStateHealthy accountCardState = "healthy"
	// accountStateLow: balance available but below the account's
	// low-balance threshold. Rendered with an icon AND text, never color
	// alone (ACCESSIBILITY.md point 8).
	accountStateLow accountCardState = "low"
	// accountStateStale: the account's latest transaction is older than
	// staleAccountDays. The balance may still be available, but a stale
	// CSV must not masquerade as a healthy balance, so the card says so
	// visibly.
	accountStateStale accountCardState = "stale"
	// accountStateNoAnchor: the balance is unavailable (no anchor at or
	// before the as-of date). Rendered as unknown, NOT as $0.00 -- a zero
	// balance and an unknown balance are different facts (GLOSSARY.md
	// "BalanceAnchor").
	accountStateNoAnchor accountCardState = "no-anchor"
)

// accountCardView is the per-account model the dashboard accounts card
// template renders. It carries the stored account plus the derived
// balance/freshness/projection data the card needs, precomputed here so
// the template is free of arithmetic.
type accountCardView struct {
	Account      models.Account
	Institution  string
	State        accountCardState
	Balance      float64 // available balance; 0 when State == no-anchor
	Available    bool    // false when no anchor exists at or before asOf
	Threshold    float64 // the threshold used (account's own or default)
	Freshness    time.Time
	HasFreshness bool
	Stale        bool

	// Projection is the checking/savings funding forecast, or nil when
	// the account kind is not checking/savings. ProjectedBalance is
	// unavailable when no anchor exists; the template renders "unknown"
	// rather than a number in that case.
	Projection      *accounts.ProjectionResult
	ProjectionState projectionLineState
}

// accountsCardData is the full model the accounts card template renders.
type accountsCardData struct {
	Accounts []accountCardView
	HasAny   bool
}

// projectionLineState is the distinct rendering state for the per-account
// projection summary line. The template switches on this field. See the
// task spec (A8, "Projection summary line") for the three states.
type projectionLineState string

const (
	projectionCrossing    projectionLineState = "crossing"
	projectionNoCrossing  projectionLineState = "no-crossing"
	projectionUnavailable projectionLineState = "unavailable"
)

// buildAccountsCard loads accounts through the store, computes each
// account's balance, freshness, and (for checking/savings) projection, and
// returns the view model the accounts card template renders. asOf is the
// "as of" date balances and freshness are computed against (the loader's
// maxDate in production). It never returns an error that blocks the
// dashboard: a storage failure logs and yields an empty card, since the
// rest of the dashboard still renders.
//
// This function reuses accounts.BalanceAt, accounts.Freshness, and
// accounts.Project exactly as A4/A5 built them; it adds no balance,
// projection, or classification logic of its own.
func buildAccountsCard(store *storage.Storage, txs []models.Transaction, asOf time.Time, recurring []models.RecurringPayment) accountsCardData {
	if store == nil {
		return accountsCardData{}
	}
	accts, err := accounts.Load(store)
	if err != nil {
		log.Printf("dashboard: loading accounts for card: %v", err)
		return accountsCardData{}
	}
	if len(accts) == 0 {
		return accountsCardData{}
	}

	views := make([]accountCardView, 0, len(accts))
	for _, a := range accts {
		view := buildAccountCardView(a, txs, asOf, recurring)
		views = append(views, view)
	}
	return accountsCardData{Accounts: views, HasAny: len(views) > 0}
}

// buildAccountCardView computes one account's card view. Split from
// buildAccountsCard so the per-account state logic is testable in
// isolation.
func buildAccountCardView(a models.Account, txs []models.Transaction, asOf time.Time, recurring []models.RecurringPayment) accountCardView {
	threshold := thresholdForCard(a)
	balance, err := accounts.BalanceAt(a, txs, asOf)
	if err != nil {
		log.Printf("dashboard: balance for %s: %v", a.ID, err)
	}

	latest, hasFresh := accounts.Freshness(a, txs)
	stale := false
	if hasFresh {
		// Staleness is measured against today (nowFunc), not asOf (the
		// balance as-of date). A CSV whose latest row is from February is
		// stale in August even if the dashboard filter is set to February;
		// comparing against asOf would let a stale CSV masquerade as fresh
		// whenever the user narrows the date range. nowFunc is a package
		// var (time.Now in production, a fixed date in tests) so the
		// staleness check is deterministic under test.
		stale = nowFunc().Sub(latest) > staleAccountDays*24*time.Hour
	}

	view := accountCardView{
		Account:      a,
		Institution:  a.Institution,
		Threshold:    threshold,
		Freshness:    latest,
		HasFreshness: hasFresh,
		Stale:        stale,
	}

	// State precedence: no-anchor dominates (the balance is unknown), then
	// stale (a stale CSV must not masquerade as healthy), then low, then
	// healthy. A stale account whose balance is also low is shown as stale;
	// staleness is the more important signal because the balance itself may
	// be wrong.
	if !balance.Available {
		view.State = accountStateNoAnchor
	} else {
		view.Balance = balance.Amount
		view.Available = true
		switch {
		case stale:
			view.State = accountStateStale
		case balance.Amount < threshold:
			view.State = accountStateLow
		default:
			view.State = accountStateHealthy
		}
	}

	// Projection only for checking/savings kinds (design doc §4). Even when
	// the balance is unavailable, we still attach a ProjectionResult with
	// Available=false so the template can render the "unknown" line.
	if a.Kind == models.AccountKindChecking || a.Kind == models.AccountKindSavings {
		proj, err := accounts.Project(a, txs, asOf, recurring)
		if err != nil {
			log.Printf("dashboard: projection for %s: %v", a.ID, err)
		}
		if err == nil {
			view.Projection = &proj
			view.ProjectionState = projectionLineStateFor(&proj)
		} else {
			view.ProjectionState = projectionUnavailable
		}
	}

	return view
}

// projectionLineStateFor maps a ProjectionResult to the three-state
// rendering state the template switches on. Available=false is
// "unavailable"; a non-zero Crossing is "crossing"; otherwise
// "no-crossing".
func projectionLineStateFor(p *accounts.ProjectionResult) projectionLineState {
	if p == nil {
		return projectionUnavailable
	}
	if !p.Available {
		return projectionUnavailable
	}
	if !p.Crossing.IsZero() {
		return projectionCrossing
	}
	return projectionNoCrossing
}

// thresholdForCard returns the account's LowBalanceThreshold, or the
// accounts service default when it is zero. This mirrors
// accounts.thresholdFor but is exported for the dashboard view since the
// dashboard needs the resolved threshold to label the card without
// reaching into the unexported service helper.
func thresholdForCard(a models.Account) float64 {
	if a.LowBalanceThreshold > 0 {
		return a.LowBalanceThreshold
	}
	return accounts.DefaultLowBalanceThreshold
}

// detectRecurringForDashboard wraps the insights recurring engine to return
// the RecurringPayment slice the projection consumes. It is a thin seam so
// the dashboard handler does not import the insights package directly in
// its hot path and so tests can substitute the recurring input. The
// recurring engine groups outflows by merchant across the whole ledger;
// accounts.Project filters that output to the account in question, so
// passing the full ledger's recurring items here is correct.
func detectRecurringForDashboard(ts *models.TransactionSet, asOf time.Time) []models.RecurringPayment {
	if ts == nil {
		return nil
	}
	return insights.DetectRecurringAt(ts, asOf)
}
