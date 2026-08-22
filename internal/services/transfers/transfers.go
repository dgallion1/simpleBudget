// Package transfers classifies internal transfers -- money moving between
// the user's own accounts -- at load time.
//
// It replaces the old drop-on-load filter. The filter had two failure modes:
// a transfer whose description missed the substring patterns was counted
// twice (the debit inflated expenses, the credit was usually read as income),
// and a transfer that did match vanished from the ledger entirely, so the
// user could not see money they had actually moved. This package instead
// *types* those rows models.Transfer and leaves them in place; every
// aggregation already filters by type, so they fall out of income and
// expense totals while staying visible.
//
// Pairing, not string matching, is what makes the determination robust:
//
//   - Auto-pair. Opposite-sign rows of equal amount in cents, in DIFFERENT
//     accounts, within WindowDays of each other, neither already paired, and
//     with at least one leg matching classifier.InternalTransferPatterns or a
//     user-declared IsInternalTransfer major expense. A unique candidate
//     pairs; several candidates resolve to the closest date; an exact tie
//     goes to review rather than guessing.
//   - Suspected. The same cross-account amount match with NO pattern hit is
//     only ever *suggested*. Coincidentally equal amounts are common, and
//     auto-pairing them would silently erase real income or real spending --
//     the exact failure this package exists to end.
//   - External. A pattern-matching row with no counterparty in the loaded
//     data (a brokerage whose CSV is not imported) is still a transfer; it is
//     typed Transfer/external and carries no pair key.
//
// See GLOSSARY.md ("Internal transfer") and
// docs/superpowers/specs/2026-08-16-accounts-transfers-design.md §3.
package transfers

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/classifier"
	"budget2/internal/services/majorexpenses"
)

// WindowDays is the half-width, in calendar days, of the window two legs of
// one transfer must fall inside. Four days covers a weekend plus an ACH
// settlement day without reaching far enough to sweep up an unrelated
// same-amount row.
const WindowDays = 4

// TransferClass values for models.Transaction.TransferClass.
const (
	// ClassPaired means both legs are loaded and share a TransferPairKey.
	ClassPaired = "paired"
	// ClassExternal means the counterparty account's CSV is not loaded, so
	// the row is a transfer with no pair key.
	ClassExternal = "external"
)

// Verdict is a user's resolution of one suspected pair.
type Verdict string

const (
	// VerdictConfirm marks the pair a real transfer: both legs are paired
	// on the next load, pattern hit or not.
	VerdictConfirm Verdict = "confirm"
	// VerdictReject marks the pair a coincidence: it is never suggested
	// again and never auto-paired.
	VerdictReject Verdict = "reject"
)

// Reasons a candidate pair reached the review queue instead of being paired.
const (
	// ReasonAmountMatch: a cross-account, opposite-sign, equal-amount match
	// inside the window with no transfer pattern on either leg.
	ReasonAmountMatch = "amount_match"
	// ReasonAmbiguous: several pattern-backed candidates tied on date
	// distance, so no single counterparty is determinable.
	ReasonAmbiguous = "ambiguous"
)

// Decision is one persisted user resolution, stored in
// data/transfer_decisions.json keyed by PairKey. StableIDs names the two legs
// so a confirmed pair can be reconstructed on a later load even though the
// pattern gate would not have paired it. A decision whose StableIDs no longer
// resolve is retained but inert -- the rows are usually outside the loaded
// range, not gone.
type Decision struct {
	PairKey   string    `json:"pair_key"`
	StableIDs [2]string `json:"stable_ids"`
	Verdict   Verdict   `json:"verdict"`
	DecidedAt time.Time `json:"decided_at"`
}

// Suspected is one candidate pair awaiting the user's confirm/reject. Left
// and Right are copies of the two legs as they stood at classification time;
// order is deterministic (ascending identity) but carries no meaning.
type Suspected struct {
	PairKey string
	Left    models.Transaction
	Right   models.Transaction
	Reason  string
}

// Result is what Classify produces: the transaction slice with transfer rows
// typed, the review queue, and the tallies the loader reports.
type Result struct {
	// Transactions is the input slice with Transfer rows typed. Every
	// input row is present -- classification never drops anything.
	Transactions []models.Transaction
	// Suspected is the review queue, sorted by date then pair key.
	Suspected []Suspected
	// Paired and External count ROWS, not pairs, so Paired+External is the
	// number of Transfer-typed rows in Transactions.
	Paired   int
	External int
}

// Classified is the number of Transfer-typed rows, which is what
// DataLoader.FilteredTransfers reports.
func (r Result) Classified() int { return r.Paired + r.External }

// PairKeyFor is the key both legs of a paired transfer carry:
// sha256(idA|idB) truncated to 12 hex characters, with the two identities
// ordered lexicographically so either leg computes the same value.
func PairKeyFor(idA, idB string) string {
	lo, hi := idA, idB
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:])[:12]
}

// Classify types internal transfers in txns and returns the review queue.
//
// It runs after account stamping, StableID assignment and exact dedup, and
// before income/outflow classification -- dedup first so duplicate rows
// cannot manufacture phantom pair candidates, and income/outflow after so
// classifier.ClassifyTransactions can skip the rows typed here.
//
// transferDefs are the user's IsInternalTransfer-flagged major expenses;
// decisions is the persisted pairKey -> Decision map. Both may be empty. The
// input slice is not modified; the returned slice is a fresh copy.
func Classify(txns []models.Transaction, transferDefs []models.MajorExpense, decisions map[string]Decision) Result {
	out := make([]models.Transaction, len(txns))
	copy(out, txns)
	if len(out) == 0 {
		return Result{Transactions: out}
	}

	pattern := make([]bool, len(out))
	for i := range out {
		pattern[i] = MatchesPattern(out[i], transferDefs)
	}

	paired := make([]bool, len(out))
	var suspected []Suspected
	seenPair := make(map[string]bool)

	// 1. The user's decisions come first: a confirmed pair is paired
	// whether or not a pattern backs it, and a rejected pair is excluded
	// from every candidate set below so it is neither auto-paired nor
	// suggested again.
	rejected := applyDecisions(out, paired, decisions)

	// Only rows of equal magnitude can pair, so bucket by absolute cents:
	// that turns the comparison from every-row-against-every-row into a
	// handful of tiny buckets.
	byCents := make(map[int64][]int, len(out))
	for i := range out {
		if cents := absCents(out[i]); cents != 0 {
			byCents[cents] = append(byCents[cents], i)
		}
	}

	// 2. Auto-pair pass, restricted to candidates with a pattern hit.
	//
	// The old version of this pass walked rows in slice order: row i
	// claimed nearest(out, i, cands) and marked both legs paired, so a
	// later row j that was strictly nearer to that same counterparty never
	// got to compete -- the first claim won regardless of distance. That
	// made pairing a function of row ORDER, not row identity, which is
	// wrong: the documented rule is closest-date, and two permutations of
	// the same input must classify identically.
	//
	// The fix builds the whole pattern-gated candidate graph up front (an
	// edge for every structurally valid pair, symmetric by construction)
	// and resolves it in strictly increasing date-distance order, one
	// distance value ("layer") at a time. Within a layer every row still
	// free is, by construction, at its own nearest remaining distance --
	// any nearer edge it had was already resolved by an earlier layer.  A
	// row with more than one edge in its layer has a genuine tie: it and
	// every edge touching it go to review, because a row whose own
	// counterparty is undecided must not become someone else's "unique"
	// candidate either. What is left after removing tied rows is a set of
	// edges between two rows that are each other's sole remaining option,
	// which can only pair one way -- so the whole layer resolves without
	// ever asking which row a caller happened to look at first. This makes
	// the outcome a pure function of the row set: same rows in, same
	// pairing out, no matter the input order.
	ambiguous := make([]bool, len(out))
	type transferEdge struct {
		i, j int
		dist int
	}
	var edges []transferEdge
	for _, idxs := range byCents {
		for a := 0; a < len(idxs); a++ {
			for b := a + 1; b < len(idxs); b++ {
				i, j := idxs[a], idxs[b]
				if paired[i] || paired[j] {
					continue
				}
				if !isCandidatePair(out[i], out[j]) {
					continue
				}
				if !(pattern[i] || pattern[j]) {
					continue
				}
				if rejected[PairKeyFor(Identity(out[i]), Identity(out[j]))] {
					continue
				}
				edges = append(edges, transferEdge{i: i, j: j, dist: dayDiff(out[i].Date, out[j].Date)})
			}
		}
	}
	// Sort by distance so layers can be walked in ascending order, and
	// break same-distance ties by the pair's own identity rather than by
	// where either row landed in byCents' (map, so unordered) bucket
	// iteration -- the sort key must depend only on row content, never on
	// slice position, or the "order independent" guarantee would just move
	// one level down into this loop.
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].dist != edges[b].dist {
			return edges[a].dist < edges[b].dist
		}
		ka := PairKeyFor(Identity(out[edges[a].i]), Identity(out[edges[a].j]))
		kb := PairKeyFor(Identity(out[edges[b].i]), Identity(out[edges[b].j]))
		return ka < kb
	})

	for start := 0; start < len(edges); {
		end := start
		for end < len(edges) && edges[end].dist == edges[start].dist {
			end++
		}
		layer := edges[start:end]
		start = end

		// A row a nearer layer already settled (paired or sent to
		// review) is no longer free, so its edges in this layer are
		// stale and must be dropped before counting anything.
		var free []transferEdge
		degree := make(map[int]int, len(layer)*2)
		for _, e := range layer {
			if paired[e.i] || paired[e.j] || ambiguous[e.i] || ambiguous[e.j] {
				continue
			}
			free = append(free, e)
			degree[e.i]++
			degree[e.j]++
		}

		// Any row with more than one candidate at this, its nearest
		// remaining distance, is ambiguous -- and so is every edge
		// that touches it, since guessing which of a tied row's
		// candidates to keep would silently invent a relationship.
		for _, e := range free {
			if degree[e.i] > 1 || degree[e.j] > 1 {
				ambiguous[e.i] = true
				ambiguous[e.j] = true
				addSuspected(&suspected, seenPair, out, e.i, e.j, ReasonAmbiguous)
			}
		}

		// What remains is exactly the edges between two rows whose
		// only candidate, at this distance, is each other -- pair
		// them. Each such row has degree 1, so these edges cannot
		// overlap and there is nothing left to arbitrate.
		for _, e := range free {
			if degree[e.i] > 1 || degree[e.j] > 1 {
				continue
			}
			pairLegs(out, paired, e.i, e.j)
		}
	}

	// 3. Everything still unpaired that matches on amount/accounts/window
	// is a suggestion only -- never an auto-pair.
	for i := range out {
		if paired[i] {
			continue
		}
		cands := candidates(out, paired, rejected, byCents, i, nil)
		for _, j := range cands {
			reason := ReasonAmountMatch
			if pattern[i] || pattern[j] {
				reason = ReasonAmbiguous
			}
			addSuspected(&suspected, seenPair, out, i, j, reason)
		}
	}

	// 4. A pattern hit with no counterparty in the data is still a
	// transfer -- these are precisely the rows the old filter deleted.
	for i := range out {
		if paired[i] || !pattern[i] {
			continue
		}
		out[i].TransactionType = models.Transfer
		out[i].TransferClass = ClassExternal
		out[i].TransferPairKey = ""
	}

	sort.SliceStable(suspected, func(a, b int) bool {
		if !suspected[a].Left.Date.Equal(suspected[b].Left.Date) {
			return suspected[a].Left.Date.Before(suspected[b].Left.Date)
		}
		return suspected[a].PairKey < suspected[b].PairKey
	})

	res := Result{Transactions: out, Suspected: suspected}
	for i := range out {
		switch out[i].TransferClass {
		case ClassPaired:
			res.Paired++
		case ClassExternal:
			res.External++
		}
	}
	return res
}

// MatchesPattern reports whether a row's own text marks it as an internal
// transfer, via the hardcoded classifier patterns or a user-declared
// IsInternalTransfer major expense. It is only ever a *gate* on pairing:
// on its own it decides nothing about a paired transfer, which is the point
// of the redesign.
func MatchesPattern(t models.Transaction, transferDefs []models.MajorExpense) bool {
	if classifier.IsInternalTransfer(&t) {
		return true
	}
	if len(transferDefs) > 0 {
		if _, ok := majorexpenses.MatchTransaction(t, transferDefs); ok {
			return true
		}
	}
	return false
}

// Identity is the key a row is paired under: its StableID, falling back to
// the legacy content hash for rows that never went through the loader's
// StableID assignment (hand-built fixtures).
func Identity(t models.Transaction) string {
	if t.StableID != "" {
		return t.StableID
	}
	if t.Hash != "" {
		return t.Hash
	}
	return t.ComputeHash()
}

// applyDecisions pairs every confirmed decision whose two legs are both
// loaded and both still free, and returns the set of pair keys the user
// rejected. A rejected key is excluded from candidacy everywhere below, which
// is what "never suggested again" means.
func applyDecisions(out []models.Transaction, paired []bool, decisions map[string]Decision) map[string]bool {
	rejected := make(map[string]bool)
	if len(decisions) == 0 {
		return rejected
	}

	byIdentity := make(map[string]int, len(out))
	for i := range out {
		if id := Identity(out[i]); id != "" {
			if _, seen := byIdentity[id]; !seen {
				byIdentity[id] = i
			}
		}
	}

	// Iterate in key order so a corrupt store with overlapping decisions
	// resolves the same way on every load.
	keys := make([]string, 0, len(decisions))
	for k := range decisions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		d := decisions[key]
		switch d.Verdict {
		case VerdictReject:
			rejected[key] = true
			if d.PairKey != "" {
				rejected[d.PairKey] = true
			}
		case VerdictConfirm:
			i, iOK := byIdentity[d.StableIDs[0]]
			j, jOK := byIdentity[d.StableIDs[1]]
			// A decision naming rows that are not in this load is
			// retained on disk but inert here: the rows are
			// usually outside the loaded range, not gone.
			if !iOK || !jOK || i == j || paired[i] || paired[j] {
				continue
			}
			pairLegs(out, paired, i, j)
		}
	}
	return rejected
}

// candidates returns the indexes that could be i's counterparty: unpaired,
// opposite sign, equal magnitude, a DIFFERENT account, inside the window, and
// not part of a rejected pair. extra adds the caller's own gate (the pattern
// requirement, for the auto-pair pass).
func candidates(out []models.Transaction, paired []bool, rejected map[string]bool, byCents map[int64][]int, i int, extra func(j int) bool) []int {
	var found []int
	for _, j := range byCents[absCents(out[i])] {
		if j == i || paired[j] {
			continue
		}
		if !isCandidatePair(out[i], out[j]) {
			continue
		}
		if rejected[PairKeyFor(Identity(out[i]), Identity(out[j]))] {
			continue
		}
		if extra != nil && !extra(j) {
			continue
		}
		found = append(found, j)
	}
	return found
}

// isCandidatePair is the structural test for two legs of one transfer:
// opposite signs, the same magnitude to the cent, different accounts, and
// dates no more than WindowDays apart.
//
// The different-accounts rule is what makes this a transfer rather than a
// coincidence: money that leaves and returns inside one account has not
// moved. Two rows both lacking an AccountID count as the same account, so an
// unassigned file cannot pair with itself.
func isCandidatePair(a, b models.Transaction) bool {
	ca, cb := models.AmountCents(a.Amount), models.AmountCents(b.Amount)
	if ca == 0 || cb == 0 || ca != -cb {
		return false
	}
	if a.AccountID == b.AccountID {
		return false
	}
	return dayDiff(a.Date, b.Date) <= WindowDays
}

// pairLegs types both legs Transfer/paired under one shared key.
func pairLegs(out []models.Transaction, paired []bool, i, j int) {
	key := PairKeyFor(Identity(out[i]), Identity(out[j]))
	for _, k := range [2]int{i, j} {
		out[k].TransactionType = models.Transfer
		out[k].TransferClass = ClassPaired
		out[k].TransferPairKey = key
	}
	paired[i] = true
	paired[j] = true
}

// addSuspected appends one review-queue entry, deduplicated by pair key so
// the same two rows are never queued twice. Left/Right are ordered by
// identity so the entry is byte-identical no matter which leg found it.
func addSuspected(dst *[]Suspected, seen map[string]bool, out []models.Transaction, i, j int, reason string) {
	key := PairKeyFor(Identity(out[i]), Identity(out[j]))
	if seen[key] {
		return
	}
	seen[key] = true
	left, right := out[i], out[j]
	if Identity(right) < Identity(left) {
		left, right = right, left
	}
	*dst = append(*dst, Suspected{PairKey: key, Left: left, Right: right, Reason: reason})
}

// absCents is a row's magnitude in cents; 0 for a zero-amount row, which can
// never pair with anything.
func absCents(t models.Transaction) int64 {
	c := models.AmountCents(t.Amount)
	if c < 0 {
		return -c
	}
	return c
}

// dayDiff is the absolute distance in calendar days, computed on the date
// parts alone so a timestamped export and a midnight one compare equal.
func dayDiff(a, b time.Time) int {
	ad := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	bd := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	d := int(ad.Sub(bd).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}
