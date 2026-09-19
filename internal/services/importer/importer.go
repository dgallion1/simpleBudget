// Package importer holds the pure, handler-independent logic shared by the
// two ways a CSV lands in the data directory — the import-folder scan
// (GET/POST /explorer/import*) and the drag-and-drop upload
// (POST /explorer/upload):
//
//   - Detect matches a parsed-but-not-yet-attributed file against the
//     account whose ledger rows it overlaps with, so the user doesn't have
//     to remember which account a browser-named export belongs to.
//   - Name turns a detected account and a row set into the same
//     `<accountID>_<min>_to_<max>.csv` shape the rest of the app already
//     uses for on-disk CSVs.
//   - ContentIdentical recognizes a byte-for-byte re-download (the same
//     export saved twice, often under a different browser-generated name)
//     so it can be skipped instead of silently duplicated under a new name.
//
// Nothing here touches a filesystem or a handler; every function takes its
// inputs as plain values and returns plain values, so it is exercised with
// ordinary table tests. See internal/handlers/explorer for the HTTP glue.
package importer

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
)

// Detection is Detect's verdict for one not-yet-attributed file.
//
// Exactly one of two shapes holds:
//   - AccountID and AccountName are set and Reason is empty: a unique
//     account matched.
//   - AccountID and AccountName are empty and Reason explains why: "no
//     match" (nothing shared enough rows) or "ambiguous: <A>, <B>" (more
//     than one account shared enough rows, names sorted).
type Detection struct {
	AccountID   string
	AccountName string
	Reason      string
}

// minSharedKeys is the number of overlapping (date, description, amount)
// rows a candidate account's existing ledger rows must share with the file
// being detected before that account counts as a match. Below this,
// coincidental overlap (a single shared coffee-shop charge) is too weak a
// signal; a handful of files that small simply detect as "no match" and the
// user assigns them by hand.
const minSharedKeys = 3

// txnKey is the sign-convention-independent identity Detect compares rows
// on. Amount is absolute cents: an unassigned parse and the eventual
// account-owned parse can flip a file's sign differently (ParseCSV's
// heuristic runs before the account is known), so comparing signed amounts
// would miss every real match on a credit-kind account's first import.
type txnKey struct {
	date  string
	desc  string
	cents int64
}

func keyFor(t models.Transaction) txnKey {
	return txnKey{
		date:  t.Date.Format("2006-01-02"),
		desc:  normalizeDescription(t.Description),
		cents: absCents(t.Amount),
	}
}

// normalizeDescription lower-cases, trims, and collapses internal
// whitespace runs to a single space, so "Wegmans   #123 " and
// "wegmans #123" key the same.
func normalizeDescription(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func absCents(amount float64) int64 {
	return int64(math.Round(math.Abs(amount) * 100))
}

// Detect matches rows (parsed from a not-yet-attributed file) against the
// currently loaded ledger, keying every row as (date, normalized
// description, absolute cents) so the comparison holds regardless of which
// sign convention either side happened to use.
//
// Only ledger rows already carrying a non-empty AccountID count — an
// unassigned ledger row cannot vote for a candidate. Candidates are the
// accounts whose ledger rows share at least minSharedKeys keys with rows.
// An empty ledger, or a first-ever export for an account with no prior
// rows loaded, therefore has no candidates and detects as "no match" by
// design, not as an error.
func Detect(rows []models.Transaction, ledger []models.Transaction, accts []models.Account) Detection {
	ledgerKeys := make(map[string]map[txnKey]bool)
	for _, t := range ledger {
		if t.AccountID == "" {
			continue
		}
		keys := ledgerKeys[t.AccountID]
		if keys == nil {
			keys = make(map[txnKey]bool)
			ledgerKeys[t.AccountID] = keys
		}
		keys[keyFor(t)] = true
	}

	// Count DISTINCT keys the file shares with each account: a single
	// coincidental row repeated three times in an export (an identical
	// recurring charge on one day, a $0 placeholder) must not reach the
	// threshold on its own. Dedupe the file's keys before counting.
	fileKeys := make(map[txnKey]bool, len(rows))
	for _, r := range rows {
		fileKeys[keyFor(r)] = true
	}
	shared := make(map[string]int)
	for k := range fileKeys {
		for id, keys := range ledgerKeys {
			if keys[k] {
				shared[id]++
			}
		}
	}

	var candidates []string
	for id, n := range shared {
		if n >= minSharedKeys {
			candidates = append(candidates, id)
		}
	}
	sort.Strings(candidates)

	switch len(candidates) {
	case 0:
		return Detection{Reason: "no match"}
	case 1:
		id := candidates[0]
		return Detection{AccountID: id, AccountName: accountName(accts, id)}
	default:
		names := make([]string, 0, len(candidates))
		for _, id := range candidates {
			names = append(names, accountName(accts, id))
		}
		sort.Strings(names)
		return Detection{Reason: "ambiguous: " + strings.Join(names, ", ")}
	}
}

// accountName resolves id to its account's display name, falling back to
// the bare id when the account no longer exists (deleted after some of its
// rows were loaded) so a caller always has something to show.
func accountName(accts []models.Account, id string) string {
	if a := accounts.Find(accts, id); a != nil {
		return a.Name
	}
	return id
}

// Name builds the on-disk filename an account's freshly detected rows
// should be saved under: `<accountID>_<min>_to_<max>.csv`, where min/max are
// the rows' own date range (rows with a zero Date are ignored when finding
// the range). A row set with no dated rows at all yields an empty range on
// both sides.
func Name(accountID string, rows []models.Transaction) string {
	var min, max time.Time
	for _, r := range rows {
		if r.Date.IsZero() {
			continue
		}
		if min.IsZero() || r.Date.Before(min) {
			min = r.Date
		}
		if max.IsZero() || r.Date.After(max) {
			max = r.Date
		}
	}
	minStr, maxStr := "", ""
	if !min.IsZero() {
		minStr = min.Format("2006-01-02")
	}
	if !max.IsZero() {
		maxStr = max.Format("2006-01-02")
	}
	return fmt.Sprintf("%s_%s_to_%s.csv", accountID, minStr, maxStr)
}

// ContentIdentical returns the name of the entry in existing whose bytes
// sha256-match data, or "" when none does. existing is typically every *.csv
// currently in the data directory, keyed by basename. Ties (more than one
// existing file with identical content) resolve to the alphabetically first
// name, so the result is deterministic regardless of map iteration order.
func ContentIdentical(data []byte, existing map[string][]byte) string {
	sum := sha256.Sum256(data)
	names := make([]string, 0, len(existing))
	for name := range existing {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sha256.Sum256(existing[name]) == sum {
			return name
		}
	}
	return ""
}
