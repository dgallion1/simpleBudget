package whatifmcp

import (
	"fmt"
	"math"
	"os"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/pricecreep"
)

// TransactionSource loads the full transaction history for the insight
// tools (get_anomalies, get_price_creep). *dataloader.DataLoader satisfies
// it via its existing LoadData method, so no adapter is needed in
// production. The interface exists so tests can substitute a canned
// models.TransactionSet directly -- constructing exact peer groups and
// planted anomalies through real CSV parsing, classification, and
// near-duplicate detection would be indirect and brittle.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// Transactions loads the full transaction history for tools that read
// across the whole ledger rather than a single saved scenario (currently
// get_anomalies and get_price_creep). It goes through the storage layer
// NewSource was given -- never os.ReadFile on a data file directly -- so a
// locked/encrypted store or a missing data directory surfaces as a plain
// error a tool call can report, the same way Source.Load already does for
// an unreadable scenario file, instead of a panic or a silently-empty
// result.
//
// txSource is nil in production; NewServer never sets it. This then builds
// a *dataloader.DataLoader rooted at the settings directory's PARENT --
// cmd/server's own DataLoader is rooted at cfg.DataDirectory, and
// settingsDir is always cfg.DataDirectory + "/settings" (see
// cmd/whatif-mcp/main.go's resolveDataDir). dataDirFromSettingsDir
// (live.go) enforces that shape and REFUSES anything else -- the same
// guard spawnArgs already applies to settingsDir for a different purpose
// (deriving BUDGET_DATA_DIR). Without it, a settingsDir not named
// ".../settings" (e.g. a custom -data flag value) would silently resolve
// to some unrelated parent directory: dataloader.LoadData finds no CSVs
// there, and get_anomalies/get_price_creep would report a confident
// "count: 0" instead of the misconfiguration that produced it. Tests set
// txSource directly to skip all of this.
func (s *Source) Transactions() (*models.TransactionSet, error) {
	src := s.txSource
	if src == nil {
		if s.store != nil && s.store.IsEncrypted() && !s.store.IsUnlocked() {
			return nil, fmt.Errorf(
				"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
		}

		dataDir, err := dataDirFromSettingsDir(s.settingsDir)
		if err != nil {
			return nil, fmt.Errorf(
				"cannot load transaction history: settings directory %q is not shaped <data-dir>/settings, "+
					"so the transaction data directory cannot be derived from it (%w)", s.settingsDir, err)
		}
		if _, err := os.Stat(dataDir); err != nil {
			return nil, fmt.Errorf("data directory %q is not readable: %w", dataDir, err)
		}

		src = dataloader.New(dataDir, s.store)
	}

	ts, err := src.LoadData()
	if err != nil {
		return nil, fmt.Errorf("load transaction history: %w", err)
	}
	if ts == nil {
		ts = models.NewTransactionSet(nil)
	}
	return ts, nil
}

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
	PeerGroup   string  `json:"peer_group"`
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

// anomalyRows runs anomalies.Detect over the FULL history in ts (baselines
// and new-merchant first-occurrence are never window-scoped, per
// ANALYTICS_PORT_SPEC.md Rulings) and returns only the flags whose Date
// falls in [start, end] for display. Detect's own ordering (score
// descending, Hash ascending) is preserved since filtering does not
// reorder.
func anomalyRows(ts *models.TransactionSet, start, end *time.Time) []anomalyRow {
	detected := anomalies.Detect(*ts)
	rows := make([]anomalyRow, 0, len(detected))
	for _, a := range detected {
		if !inWindow(a.Date, start, end) {
			continue
		}
		rows = append(rows, anomalyRow{
			Date:        a.Date.Format("2006-01-02"),
			Description: a.Description,
			Category:    a.Category,
			Amount:      round0(a.Amount),
			Method:      a.Method,
			Severity:    a.Severity,
			Score:       round2(a.Score),
			PeerGroup:   a.PeerGroup,
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
			FirstAmount:   round0(c.FirstAmount),
			CurrentAmount: round0(c.CurrentAmount),
			PctChange:     round2(c.PctChange),
			FirstDate:     c.FirstDate.Format("2006-01-02"),
			LastDate:      c.LastDate.Format("2006-01-02"),
			Occurrences:   c.Occurrences,
		})
	}
	return rows
}
