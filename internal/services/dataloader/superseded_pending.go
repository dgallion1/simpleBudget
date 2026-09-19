package dataloader

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
)

// fileCoverage is one CSV file's account attribution and date range,
// gathered in LoadDataContext's per-file loop from the rows that file just
// contributed -- no second scan of the file. Only files matched to an
// account get an entry: rule (b) in dropSupersededPending's doc excludes an
// unassigned file (empty accountID) from superseding or being superseded,
// so there is nothing for its coverage to do.
type fileCoverage struct {
	accountID        string
	minDate, maxDate time.Time
}

// coverageOf computes one file's fileCoverage from the rows
// loadCSVFileForAccount just returned for it (post sign-flip, post
// attribution -- Date is unaffected by either, but accountID is what the
// caller passes in). Called once per matched file in LoadDataContext's
// per-file loop.
func coverageOf(accountID string, txns []models.Transaction) fileCoverage {
	cov := fileCoverage{accountID: accountID}
	for i := range txns {
		d := txns[i].Date
		if cov.minDate.IsZero() || d.Before(cov.minDate) {
			cov.minDate = d
		}
		if cov.maxDate.IsZero() || d.After(cov.maxDate) {
			cov.maxDate = d
		}
	}
	return cov
}

// dropSupersededPending removes a pending-status row when a newer export
// for the SAME account already covers its date -- i.e. the bank has since
// re-exported that period and the row is superseded, not merely
// duplicated. Runs after StableID is stamped (stampStableIDs) and before
// deduplicateTransactions and before the Hash -> StableID index is built
// (buildStableIDIndex), on all of LoadDataContext's exit paths that reach
// it.
//
// WHY before dedup: two real rows (Cybernet 28.42 on 2025-12-30, Chateau
// Wine & Spirits 35.63 on 2026-04-29) are Pending in an older export and
// Posted in a newer one with the same date/description/amount, hence one
// content Hash. deduplicateTransactions keeps the FIRST occurrence -- the
// older file's pending copy -- so a stage placed after dedup would drop the
// only surviving copy. Before dedup, the pending copy goes here and dedup
// then keeps the posted copy untouched.
//
// A row R from file F is dropped iff:
//
//	(a) isPendingStatus(R.Status), and
//	(b) F's accountID != "" (unassigned files never supersede and are
//	    never superseded -- rows from an unmatched CSV keep loading as
//	    they always have), and
//	(c) some OTHER file G for the SAME account has a maxDate strictly
//	    after F's maxDate, and G's [minDate, maxDate] covers R.Date.
//
// coverage is keyed by basename (Transaction.SourceFile) and built by the
// caller (see fileCoverage, coverageOf) from the rows each file
// contributed, before any row is dropped -- so it reflects every file's
// original date range regardless of what this function goes on to remove.
//
// Returns the surviving rows, in their original relative order, and a
// per-source-file count of rows dropped for logDroppedSupersededPending.
func dropSupersededPending(txns []models.Transaction, coverage map[string]fileCoverage) ([]models.Transaction, map[string]int) {
	dropped := make(map[string]int)
	if len(txns) == 0 {
		return txns, dropped
	}
	survivors := make([]models.Transaction, 0, len(txns))
	for i := range txns {
		t := &txns[i]
		if isSupersededPending(*t, coverage) {
			dropped[t.SourceFile]++
			continue
		}
		survivors = append(survivors, *t)
	}
	return survivors, dropped
}

// isSupersededPending applies dropSupersededPending's rule to a single row.
func isSupersededPending(t models.Transaction, coverage map[string]fileCoverage) bool {
	if !isPendingStatus(t.Status) {
		return false
	}
	f, ok := coverage[t.SourceFile]
	if !ok || f.accountID == "" {
		return false
	}
	for file, g := range coverage {
		if file == t.SourceFile {
			continue
		}
		if g.accountID != f.accountID {
			continue
		}
		if !g.maxDate.After(f.maxDate) {
			continue
		}
		if t.Date.Before(g.minDate) || t.Date.After(g.maxDate) {
			continue
		}
		return true
	}
	return false
}

// logDroppedSupersededPending emits dropSupersededPending's one summary
// line for the load that just finished: nothing when nothing was dropped,
// otherwise the total plus a per-file breakdown (file: count), files sorted
// for deterministic output.
func logDroppedSupersededPending(dropped map[string]int) {
	total := 0
	for _, n := range dropped {
		total += n
	}
	if total == 0 {
		return
	}
	files := make([]string, 0, len(dropped))
	for f := range dropped {
		files = append(files, f)
	}
	sort.Strings(files)
	parts := make([]string, 0, len(files))
	for _, f := range files {
		parts = append(parts, fmt.Sprintf("%s: %d", f, dropped[f]))
	}
	log.Printf("Dropped %d superseded pending rows (%s)", total, strings.Join(parts, ", "))
}
