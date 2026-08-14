package spend

import (
	"fmt"
	"math"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	"budget2/internal/services/merchants"
	"budget2/internal/services/pricecreep"
)

// round0 rounds a currency amount to whole dollars. Duplicated from
// plan/view.go rather than exported across packages -- it is one line, and
// cross-package coupling for math.Round is not worth it.
func round0(v float64) float64 { return math.Round(v) }

// anomaliesInput is get_anomalies' parameters. Both dates are optional,
// inclusive, YYYY-MM-DD, and -- per the tool description -- narrow only
// which already-detected anomalies are RETURNED; they never change what
// anomalies.Detect treats as a peer group's baseline or a merchant's first
// occurrence, which are always computed over the full history.
type anomaliesInput struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"inclusive display-window start date, YYYY-MM-DD; omit for no lower bound"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"inclusive display-window end date, YYYY-MM-DD; omit for no upper bound"`
}

// anomalyRow is one flagged transaction in get_anomalies' result.
type anomalyRow struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Method      string  `json:"method"`
	Severity    string  `json:"severity"`
	Score       float64 `json:"score"`
	// PeerGroup is a category name (mad_category) or a merchant label
	// (mad_merchant, new_merchant). Merchant labels are lower-cased, matching
	// every other spend tool's merchant field -- NOT anomalies.Anomaly's own
	// PeerGroup, which for those two methods is the merchants package's
	// canonical uppercase-normalized matching key; see peerGroupLabel.
	PeerGroup string `json:"peer_group"`
}

// anomaliesWindow echoes back the requested display window. A field is null
// when that bound was not supplied.
type anomaliesWindow struct {
	Start *string `json:"start"`
	End   *string `json:"end"`
}

type anomaliesOutput struct {
	Count     int             `json:"count"`
	Window    anomaliesWindow `json:"window"`
	Anomalies []anomalyRow    `json:"anomalies"`
}

// priceCreepInput is get_price_creep's parameters: none. price-creep always
// compares a merchant's first 3 occurrences to its last 3 over the full
// history, so a display window has no meaning for it.
type priceCreepInput struct{}

type creepRow struct {
	Merchant      string  `json:"merchant"`
	FirstAmount   float64 `json:"first_amount"`
	CurrentAmount float64 `json:"current_amount"`
	PctChange     float64 `json:"pct_change"`
	FirstDate     string  `json:"first_date"`
	LastDate      string  `json:"last_date"`
	Occurrences   int     `json:"occurrences"`
}

type priceCreepOutput struct {
	Count int        `json:"count"`
	Items []creepRow `json:"items"`
}

// round2 rounds to two decimal places, for scores and percentages where
// round0's whole-dollar rounding would lose the signal.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// nilableString returns nil for an empty string, otherwise a pointer to a
// copy of s -- used to echo an optional input parameter back as JSON null
// versus the literal value.
func nilableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseWindowDate parses a YYYY-MM-DD date parameter. An empty value is not
// an error -- it means "no bound on this side" -- and returns a nil
// pointer. An unparseable value returns an error naming the offending field
// and value, meant to surface as a tool error rather than a panic.
func parseWindowDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a valid date (want YYYY-MM-DD): %w", field, value, err)
	}
	return &t, nil
}

// inWindow reports whether d falls within [start, end], both inclusive and
// either possibly nil for "unbounded on this side". Day boundaries match
// models.TransactionSet.FilterByDateRange (00:00:00 through 23:59:59.999999999
// in start's/end's own location) so a whole calendar day is never
// partially excluded by a bare date's implicit midnight timestamp.
func inWindow(d time.Time, start, end *time.Time) bool {
	if start != nil {
		startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		if d.Before(startDay) {
			return false
		}
	}
	if end != nil {
		endDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999999999, end.Location())
		if d.After(endDay) {
			return false
		}
	}
	return true
}

// anomalyExpenses returns the same expense set anomalies.Detect scores
// against, internally: active (non-suppressed) transactions that are
// outflows with a negative amount. anomalies.Detect does not expose the
// merchant groups it builds from this set (its canonical key is an internal
// matching key), so peerGroupLabel recomputes the identical, deterministic
// grouping to translate mad_merchant/new_merchant's PeerGroup into a display
// label without reaching into the anomalies package's internals.
func anomalyExpenses(ts *models.TransactionSet) []models.Transaction {
	active := ts.Active().Transactions
	expenses := make([]models.Transaction, 0, len(active))
	for _, t := range active {
		if t.TransactionType == models.Outflow && t.Amount < 0 {
			expenses = append(expenses, t)
		}
	}
	return expenses
}

// peerGroupLabel maps mad_merchant/new_merchant's PeerGroup -- the merchants
// package's canonical uppercase-normalized key -- to the lower-cased display
// label every other spend tool uses for a merchant (merchants.DisplayLabel).
// mad_category's PeerGroup is already a human-readable category name and is
// returned unchanged. groups is keyed by the same canonical key, built by
// merchants.GroupTransactions over the identical expense set anomalies.Detect
// used, so a mad_merchant/new_merchant PeerGroup always has a matching entry;
// a lookup miss (method mismatch or a future anomalies.go change) falls back
// to the raw key rather than losing the row.
func peerGroupLabel(a anomalies.Anomaly, groups map[string][]models.Transaction) string {
	if a.Method != "mad_merchant" && a.Method != "new_merchant" {
		return a.PeerGroup
	}
	if g, ok := groups[a.PeerGroup]; ok {
		return merchants.DisplayLabel(g)
	}
	return a.PeerGroup
}

// anomalyRows runs anomalies.Detect over the FULL history in ts (baselines
// and new-merchant first-occurrence are never window-scoped, per
// ANALYTICS_PORT_SPEC.md Rulings) and returns only the flags whose Date
// falls in [start, end] for display. Detect's own ordering (score
// descending, Hash ascending) is preserved since filtering does not
// reorder.
func anomalyRows(ts *models.TransactionSet, start, end *time.Time) []anomalyRow {
	detected := anomalies.Detect(*ts)
	groups := merchants.GroupTransactions(anomalyExpenses(ts))
	rows := make([]anomalyRow, 0, len(detected))
	for _, a := range detected {
		if !inWindow(a.Date, start, end) {
			continue
		}
		rows = append(rows, anomalyRow{
			Date:        a.Date.Format("2006-01-02"),
			Description: a.Description,
			Category:    a.Category,
			Amount:      round2(a.Amount),
			Method:      a.Method,
			Severity:    a.Severity,
			Score:       round2(a.Score),
			PeerGroup:   peerGroupLabel(a, groups),
		})
	}
	return rows
}

// priceCreepRows runs pricecreep.Detect over the full history in ts.
func priceCreepRows(ts *models.TransactionSet) []creepRow {
	creeps := pricecreep.Detect(*ts)
	rows := make([]creepRow, 0, len(creeps))
	for _, c := range creeps {
		rows = append(rows, creepRow{
			Merchant:      c.Merchant,
			FirstAmount:   round2(c.FirstAmount),
			CurrentAmount: round2(c.CurrentAmount),
			PctChange:     round2(c.PctChange),
			FirstDate:     c.FirstDate.Format("2006-01-02"),
			LastDate:      c.LastDate.Format("2006-01-02"),
			Occurrences:   c.Occurrences,
		})
	}
	return rows
}
