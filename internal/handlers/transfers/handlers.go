// Package transfers serves the /transfers page: a monthly institution-flow
// chart, a transfer history (paired and external legs shown together), and a
// review queue for suspected pairs awaiting the user's confirm/reject.
//
// Classification is NOT done here. The dataloader's transfer-classification
// stage (A3) types rows models.Transfer at load; this package only reads
// what the loader produced: the classified ledger via LoadDataContext, the
// review queue via SuspectedTransfers, and the user's verdict via
// ResolveTransfer. Reimplementing pairing here would diverge from the
// loader's determination on the next load.
//
// The page and its HTMX partials follow the accounts/duplicates patterns:
// full pages render through the base layout with ActiveTab "transfers";
// mutations (confirm/reject) swap the review-queue partial in place and
// announce the outcome through an aria-live region so the change is both
// visible and announced (ACCESSIBILITY.md point 10).
package transfers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/services/transfers"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	store    *storage.Storage
	renderer *templates.Renderer
)

// Initialize wires the package with its dependencies. The storage service is
// required: institutions are resolved through the accounts sidecar, the same
// one the loader and the accounts page read, so the page and the loader can
// never disagree about which institution a transfer belongs to. The
// dataloader is the source of the classified ledger and the review queue.
func Initialize(l *dataloader.DataLoader, s *storage.Storage, r *templates.Renderer) {
	loader = l
	store = s
	renderer = r
}

// RegisterRoutes registers the /transfers routes. It is additive on the
// shared chi router; no existing route is modified.
func RegisterRoutes(r chi.Router) {
	r.Get("/transfers", handlePage)
	r.Get("/transfers/queue", handleQueuePartial)
	r.Post("/transfers/resolve", handleResolve)
}

// historyLeg is one row of the transfer-history table. Paired transfers are
// shown two legs at a time (the debit and the credit); an external leg has
// no loaded counterparty and is marked as such. The institution names are
// resolved through the accounts sidecar; an unassigned row shows
// "Unassigned" rather than a blank, matching the dashboard banner.
type historyLeg struct {
	Date          time.Time
	Description   string
	Institution   string
	AccountName   string
	Amount        float64
	TransferClass string // "paired" | "external"
	PairKey       string // shared by both legs of a paired transfer; "" for external
	IsExternal    bool   // external leg: counterparty not loaded
}

// historyPair is one entry in the transfer history: either the two legs of
// a paired transfer or a single external leg.
type historyPair struct {
	Legs []historyLeg
	// PairKey is the shared key for paired transfers; "" for external.
	PairKey string
	// Class is "paired" or "external".
	Class string
}

// flowCell is one month's value for one institution-to-institution
// direction, used by both the chart and the data table so they carry the
// same numbers (ACCESSIBILITY.md point 11).
type flowCell struct {
	Month    string
	FromInst string
	ToInst   string
	Amount   float64
}

// pageData is the full-page model. It is also the partial model: the
// review-queue partial reads the same keys.
type pageData struct {
	Title                    string
	ActiveTab                string
	MonthlyFlow              []flowCell
	MonthlyFlowChart         string // JSON-encoded Plotly data, built server-side
	MonthlyFlowSummary       string // one-line text takeaway
	History                  []historyPair
	Suspected                []transfers.Suspected
	ResolveMessage           string // outcome of the last resolve action (aria-live)
	UnresolvedDuplicateCount int

	// HasTransfers is false only when there are zero classified transfer rows
	// AND zero suspected pairs -- the empty-queue state.
	HasTransfers bool
}

// handlePage renders the full /transfers page.
func handlePage(w http.ResponseWriter, r *http.Request) {
	data := buildPageData(r, "")
	if renderer != nil {
		_ = renderer.Render(w, "base", data)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleQueuePartial renders only the review-queue partial. It is the HTMX
// swap target for a confirm/reject action, so a single write is the whole
// response to a mutation. Asserting against THIS partial (not the full page)
// is what ruling 2026-08-16a demands: a feature can ship green-tested and
// broken in the browser when the test asserts on a different template than
// the handler returns.
func handleQueuePartial(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	data := buildPageData(r, msg)
	if renderer != nil {
		_ = renderer.RenderPartial(w, "transfers-queue-partial", data)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleResolve accepts a confirm/reject from the review queue. It posts
// pair_key and verdict (confirm|reject) and calls loader.ResolveTransfer,
// which persists to data/transfer_decisions.json. A confirmed pair becomes
// paired on the next load; a rejected one is never suggested again.
//
// Re-posting the SAME decision must be idempotent, not an error: the queue
// partial is rebuilt from the CURRENT load's SuspectedTransfers, so once a
// decision is applied the pair is gone from the queue and a repeat POST for
// the now-absent key returns the (unchanged) queue rather than a 4xx. This
// matches the spec: "Re-posting the same decision must be idempotent, not an
// error."
//
// ResolveTransfer returns the same "no suspected transfer pair with key"
// error whether the pair was already decided or never existed at all, so
// that error alone cannot tell the two apart -- and it cannot say WHICH
// verdict was already recorded. When it fires, the handler reads the
// persisted decisions itself (loader.LoadTransferDecisions) and reports what
// is actually on disk rather than assuming the request's own verdict landed.
// Four cases fall out of that read, all handled in describeAlreadyResolved:
// a re-post of the SAME verdict is the idempotent no-op described above; a
// re-post of a DIFFERENT verdict must say plainly that the stored verdict
// disagrees and that this request was not applied, so a stale second tab
// never leaves the user believing an action landed that did not; a key
// that is in neither the queue nor the decisions map is reported as unknown,
// claiming no verdict at all; and if the decisions file itself cannot be
// read (present but unparsable), the handler says so honestly rather than
// guessing a verdict it never actually observed.
func handleResolve(w http.ResponseWriter, r *http.Request) {
	if loader == nil {
		http.Error(w, "loader not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pairKey := strings.TrimSpace(r.FormValue("pair_key"))
	verdict := strings.TrimSpace(r.FormValue("verdict"))

	var msg string
	if pairKey == "" || (verdict != "confirm" && verdict != "reject") {
		msg = "Invalid request: a pair key and a confirm or reject verdict are required."
		data := buildPageData(r, msg)
		renderQueue(w, r, data)
		return
	}

	v := transfers.Verdict(verdict)
	err := loader.ResolveTransfer(pairKey, v)
	if err != nil {
		// The pair is no longer in the queue: either it was already resolved
		// (idempotent re-post, or a conflicting re-post from a stale tab) or
		// it never existed. ResolveTransfer's error does not distinguish
		// these, so read the persisted decisions directly and report what is
		// actually stored rather than assuming the request's own verdict.
		if isAlreadyResolvedError(err) {
			msg = describeAlreadyResolved(pairKey, v)
		} else {
			msg = "Could not resolve transfer: " + err.Error()
		}
	} else {
		// Reload so the queue reflects the new decision (a confirmed pair
		// becomes paired on the next load; a rejected one is dropped from
		// the queue). SuspectedTransfers reads the most recent load, so a
		// reload here makes the partial show the updated queue.
		if _, lerr := loader.LoadData(); lerr != nil {
			log.Printf("transfers: reload after resolve: %v", lerr)
		}
		switch v {
		case transfers.VerdictConfirm:
			msg = fmt.Sprintf("Confirmed pair %s. It will be paired on the next load.", pairKey)
		case transfers.VerdictReject:
			msg = fmt.Sprintf("Rejected pair %s. It will not be suggested again.", pairKey)
		}
	}

	data := buildPageData(r, msg)
	renderQueue(w, r, data)
}

// renderQueue renders the review-queue partial. It is the HTMX swap target
// for the resolve action. When no renderer is wired (tests), it falls back
// to JSON so the mutation is still observable.
func renderQueue(w http.ResponseWriter, r *http.Request, data pageData) {
	if renderer != nil {
		_ = renderer.RenderPartial(w, "transfers-queue-partial", data)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// isAlreadyResolvedError reports whether err is the "no suspected transfer
// pair with key" error returned when the pair is no longer in the queue --
// the idempotent re-post case.
func isAlreadyResolvedError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no suspected transfer pair with key")
}

// describeAlreadyResolved builds the resolve message for a pair that
// ResolveTransfer rejected as absent from the queue. It reads the persisted
// decisions itself so the message names the verdict actually on disk, not
// the one the failed request carried: those two can differ (a stale second
// tab posting reject after another tab already confirmed the same pair), and
// collapsing them would tell the user their reject landed when it did not.
// Four outcomes are possible here:
//  1. LoadTransferDecisions itself fails (the file exists but will not
//     parse) -- there is no verdict to read at all, so the message says
//     plainly that the stored decision could not be read, rather than
//     guessing confirm/reject/unknown from a state the handler never
//     actually observed.
//  2. The key is in neither the queue nor the decisions map: unknown pair,
//     no verdict recorded.
//  3. The stored verdict matches the one just requested: idempotent no-op.
//  4. The stored verdict differs from the one just requested: the request
//     was not applied, and the message says which verdict IS stored.
func describeAlreadyResolved(pairKey string, requested transfers.Verdict) string {
	decisions, err := loader.LoadTransferDecisions()
	if err != nil {
		return fmt.Sprintf("Could not resolve transfer: the stored decision for pair %s could not be read (%v).", pairKey, err)
	}
	dec, ok := decisions[pairKey]
	if !ok {
		return fmt.Sprintf("Pair %s is not a known pair; no verdict is recorded for it.", pairKey)
	}
	if dec.Verdict == requested {
		return fmt.Sprintf("Pair %s was already %s; nothing to do.", pairKey, verdictWord(dec.Verdict))
	}
	return fmt.Sprintf("Pair %s was already %s; the %s you just submitted was not applied.", pairKey, verdictWord(dec.Verdict), requested)
}

// verdictWord renders a Verdict as the past-participle word used in
// resolve-outcome messages ("confirmed"/"rejected"). An unrecognized verdict
// (which should not occur: ResolveTransfer validates it before persisting)
// falls back to the raw string rather than panicking.
func verdictWord(v transfers.Verdict) string {
	switch v {
	case transfers.VerdictConfirm:
		return "confirmed"
	case transfers.VerdictReject:
		return "rejected"
	default:
		return string(v)
	}
}

// buildPageData loads the ledger and the review queue and assembles the
// full page model. It never panics: a missing loader or store degrades to
// empty sections so the page still renders.
func buildPageData(r *http.Request, resolveMessage string) pageData {
	data := pageData{
		Title:     "Transfers",
		ActiveTab: "transfers",
	}

	var ledger *models.TransactionSet
	if loader != nil {
		ts, err := loader.LoadDataContext(r.Context())
		if err != nil {
			log.Printf("transfers: load data: %v", err)
		} else {
			ledger = ts
		}
		data.Suspected = loader.SuspectedTransfers()
	}

	accts := loadAccounts()

	if ledger != nil {
		transferRows := ledger.FilterByType(models.Transfer)
		data.History = buildHistory(transferRows, accts)
		data.MonthlyFlow, data.MonthlyFlowChart, data.MonthlyFlowSummary = buildMonthlyFlow(transferRows, accts)
		data.HasTransfers = transferRows.Len() > 0 || len(data.Suspected) > 0
	}

	if !data.HasTransfers {
		data.HasTransfers = len(data.Suspected) > 0
	}
	data.ResolveMessage = resolveMessage

	// UnresolvedDuplicateCount is read by the base layout's nav badge. The
	// transfers page does not own that count; it stays zero here so the
	// badge stays hidden, matching the other settings-style pages.
	data.UnresolvedDuplicateCount = 0

	return data
}

// loadAccounts reads the accounts sidecar through the accounts service, the
// same path the loader uses, so institution resolution agrees with the
// loader. Returns nil (not a panic) on any failure: the page degrades to
// account IDs where institutions are unknown.
func loadAccounts() []models.Account {
	if store == nil {
		return nil
	}
	accts, err := accounts.Load(store)
	if err != nil {
		log.Printf("transfers: load accounts: %v", err)
		return nil
	}
	return accts
}

// institutionFor resolves an AccountID to its Institution, falling back to
// the account Name, then the ID, then "Unassigned" for rows whose CSV
// matched no account. The fallback chain keeps the page informative when the
// sidecar is missing or the account is unassigned.
func institutionFor(accts []models.Account, accountID string) string {
	if accountID == "" {
		return "Unassigned"
	}
	a := accounts.Find(accts, accountID)
	if a == nil {
		return accountID
	}
	if a.Institution != "" {
		return a.Institution
	}
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}

// accountNameFor resolves an AccountID to its Name, falling back to the ID
// and then "Unassigned" for empty.
func accountNameFor(accts []models.Account, accountID string) string {
	if accountID == "" {
		return "Unassigned"
	}
	a := accounts.Find(accts, accountID)
	if a == nil {
		return accountID
	}
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}

// buildHistory assembles the transfer history from the classified ledger.
// Paired legs are grouped under their shared TransferPairKey and shown
// together; external legs stand alone and are marked as having no loaded
// counterparty. The slice is sorted by date (earliest first) using the
// earliest leg of each pair.
func buildHistory(ts *models.TransactionSet, accts []models.Account) []historyPair {
	if ts == nil || ts.Len() == 0 {
		return nil
	}

	// Group paired legs by pair key; external legs stand alone.
	byKey := make(map[string][]historyLeg)
	var external []historyLeg
	var pairOrder []string // first-seen order, stable
	pairFirstDate := make(map[string]time.Time)

	for _, t := range ts.Transactions {
		leg := historyLeg{
			Date:          t.Date,
			Description:   t.Label(),
			Institution:   institutionFor(accts, t.AccountID),
			AccountName:   accountNameFor(accts, t.AccountID),
			Amount:        t.Amount,
			TransferClass: t.TransferClass,
			PairKey:       t.TransferPairKey,
			IsExternal:    t.TransferClass == transfers.ClassExternal,
		}
		if t.TransferClass == transfers.ClassPaired && t.TransferPairKey != "" {
			if _, ok := byKey[t.TransferPairKey]; !ok {
				pairOrder = append(pairOrder, t.TransferPairKey)
				pairFirstDate[t.TransferPairKey] = t.Date
			} else if t.Date.Before(pairFirstDate[t.TransferPairKey]) {
				pairFirstDate[t.TransferPairKey] = t.Date
			}
			byKey[t.TransferPairKey] = append(byKey[t.TransferPairKey], leg)
		} else {
			external = append(external, leg)
		}
	}

	out := make([]historyPair, 0, len(pairOrder)+len(external))
	for _, key := range pairOrder {
		legs := byKey[key]
		// Sort legs within a pair by date then amount for stable display.
		sort.SliceStable(legs, func(i, j int) bool {
			if !legs[i].Date.Equal(legs[j].Date) {
				return legs[i].Date.Before(legs[j].Date)
			}
			return legs[i].Amount < legs[j].Amount
		})
		out = append(out, historyPair{Legs: legs, PairKey: key, Class: transfers.ClassPaired})
	}
	for _, leg := range external {
		out = append(out, historyPair{Legs: []historyLeg{leg}, PairKey: "", Class: transfers.ClassExternal})
	}

	// Sort the whole history by earliest leg date (descending: most recent
	// first, matching the explorer's default order).
	sort.SliceStable(out, func(i, j int) bool {
		di := earliestDate(out[i].Legs)
		dj := earliestDate(out[j].Legs)
		return di.After(dj)
	})
	return out
}

// earliestDate returns the earliest date among legs.
func earliestDate(legs []historyLeg) time.Time {
	if len(legs) == 0 {
		return time.Time{}
	}
	d := legs[0].Date
	for _, l := range legs[1:] {
		if l.Date.Before(d) {
			d = l.Date
		}
	}
	return d
}

// buildMonthlyFlow produces the month-by-institution flow data used by both
// the chart and its data-table alternative. For each paired transfer, the
// full magnitude counts once for the month and the from->to direction. The
// chart JSON is a Plotly bar trace per from->to direction; the summary line
// states the single largest month and direction so the takeaway is in text.
//
// External legs are excluded from the institution flow: by definition the
// counterparty institution is not loaded, so there is no second institution
// to flow to. They remain in the history table.
func buildMonthlyFlow(ts *models.TransactionSet, accts []models.Account) ([]flowCell, string, string) {
	if ts == nil || ts.Len() == 0 {
		return nil, "", ""
	}

	// Pair legs by TransferPairKey; for each pair, the negative leg is the
	// "from" institution and the positive leg is the "to" institution. The
	// amount is the magnitude (positive).
	type pairLeg struct {
		inst   string
		amount float64
		date   time.Time
	}
	byKey := make(map[string][]pairLeg)
	for _, t := range ts.Transactions {
		if t.TransferClass != transfers.ClassPaired || t.TransferPairKey == "" {
			continue
		}
		byKey[t.TransferPairKey] = append(byKey[t.TransferPairKey], pairLeg{
			inst:   institutionFor(accts, t.AccountID),
			amount: t.Amount,
			date:   t.Date,
		})
	}

	// Aggregate by month + direction.
	type dirKey struct {
		month    string
		fromInst string
		toInst   string
	}
	agg := make(map[dirKey]float64)
	order := []dirKey{}
	for _, legs := range byKey {
		if len(legs) != 2 {
			continue // a paired transfer has exactly two legs
		}
		var from, to pairLeg
		// The negative leg is the source (money leaves), the positive leg is
		// the destination (money arrives).
		if legs[0].amount < legs[1].amount {
			from, to = legs[0], legs[1]
		} else {
			from, to = legs[1], legs[0]
		}
		month := from.date.Format("2006-01")
		k := dirKey{month: month, fromInst: from.inst, toInst: to.inst}
		if _, ok := agg[k]; !ok {
			order = append(order, k)
		}
		agg[k] += -from.amount // magnitude
	}

	// Build cells sorted by month then from->to.
	cells := make([]flowCell, 0, len(order))
	for _, k := range order {
		cells = append(cells, flowCell{
			Month:    k.month,
			FromInst: k.fromInst,
			ToInst:   k.toInst,
			Amount:   agg[k],
		})
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].Month != cells[j].Month {
			return cells[i].Month < cells[j].Month
		}
		if cells[i].FromInst != cells[j].FromInst {
			return cells[i].FromInst < cells[j].FromInst
		}
		return cells[i].ToInst < cells[j].ToInst
	})

	chartJSON := buildFlowChartJSON(cells)
	summary := buildFlowSummary(cells)
	return cells, chartJSON, summary
}

// buildFlowChartJSON builds the Plotly bar-chart payload: one stacked bar
// per month, each bar segmented by from->to direction. The data table
// carries the same values, satisfying ACCESSIBILITY.md point 11.
func buildFlowChartJSON(cells []flowCell) string {
	if len(cells) == 0 {
		return ""
	}
	// Collect months and directions in order.
	monthSet := []string{}
	monthIdx := map[string]int{}
	dirSet := []string{}
	dirIdx := map[string]int{}
	for _, c := range cells {
		if _, ok := monthIdx[c.Month]; !ok {
			monthIdx[c.Month] = len(monthSet)
			monthSet = append(monthSet, c.Month)
		}
		dir := c.FromInst + " → " + c.ToInst
		if _, ok := dirIdx[dir]; !ok {
			dirIdx[dir] = len(dirSet)
			dirSet = append(dirSet, dir)
		}
	}

	// One trace per direction; trace.y is the amount per month.
	traces := make([]map[string]interface{}, 0, len(dirSet))
	for di, dir := range dirSet {
		y := make([]float64, len(monthSet))
		for _, c := range cells {
			dir2 := c.FromInst + " → " + c.ToInst
			if dir2 == dir {
				y[monthIdx[c.Month]] += c.Amount
			}
		}
		traces = append(traces, map[string]interface{}{
			"type": "bar",
			"name": dir,
			"x":    monthSet,
			"y":    y,
		})
		_ = di
	}

	payload := map[string]interface{}{
		"data": traces,
		"layout": map[string]interface{}{
			"barmode": "stack",
			"title": map[string]interface{}{
				"text": "Transfers Between Institutions by Month",
			},
			"xaxis": map[string]interface{}{
				"title": map[string]interface{}{"text": "Month"},
			},
			"yaxis": map[string]interface{}{
				"title": map[string]interface{}{"text": "Amount ($)"},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		log.Printf("transfers: marshal chart: %v", err)
		return ""
	}
	return string(b)
}

// buildFlowSummary returns a one-line text takeaway of the chart's point:
// the largest single month's flow and its direction. If there are no flows,
// it states that.
func buildFlowSummary(cells []flowCell) string {
	if len(cells) == 0 {
		return "No paired transfers between institutions in the loaded data."
	}
	byMonth := make(map[string]float64)
	for _, c := range cells {
		byMonth[c.Month] += c.Amount
	}
	var bestMonth string
	var bestAmt float64
	for m, a := range byMonth {
		if a > bestAmt {
			bestMonth = m
			bestAmt = a
		}
	}
	// Find the top direction in that month.
	var topDir string
	var topDirAmt float64
	for _, c := range cells {
		if c.Month != bestMonth {
			continue
		}
		if c.Amount > topDirAmt {
			topDirAmt = c.Amount
			topDir = c.FromInst + " to " + c.ToInst
		}
	}
	return fmt.Sprintf("Largest flow: %s moved %s from %s in %s.",
		topDir, formatUSD(topDirAmt), topDir, bestMonth)
}

// formatUSD formats a dollar amount with a leading $.
func formatUSD(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

// writeJSON is the test-fallback path (renderer == nil). It lets the
// handler-package tests assert on round-trips without rendering HTML.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// init guarantees the package-level logger is never a nil-deref in tests.
var _ = log.Print
