package dataloader

import (
	"bytes"
	"encoding/json"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

// csvHeader is the header row every fixture CSV below uses: Date,
// Description, Category, Amount, Status. Status is what isPendingStatus
// reads.
const csvHeader = "Date,Description,Category,Amount,Status"

// findByStableID returns the one transaction in ts carrying id, failing the
// test if there isn't exactly one.
func findByStableID(t *testing.T, txns []models.Transaction, id string) models.Transaction {
	t.Helper()
	var found []models.Transaction
	for _, txn := range txns {
		if txn.StableID == id {
			found = append(found, txn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d rows with StableID %q, want exactly 1 (got %+v)", len(found), id, found)
	}
	return found[0]
}

// descriptions returns the Description of every loaded row, for a quick
// membership check independent of slice order.
func descriptions(txns []models.Transaction) []string {
	out := make([]string, len(txns))
	for i, t := range txns {
		out[i] = t.Description
	}
	return out
}

func containsDescription(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestDropSupersededPending_AC1_DropsRowCoveredByStrictlyNewerExport is the
// core rule: a pending row dated inside a strictly-newer same-account file's
// coverage does not survive the load.
func TestDropSupersededPending_AC1_DropsRowCoveredByStrictlyNewerExport(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-01-03,Coffee Shop,Dining,-20.00,Pending"
	newer := csvHeader + "\n" +
		"2026-01-01,Filler One,Misc,-1.00,Posted\n" +
		"2026-01-05,Filler Two,Misc,-2.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-01a.csv": older,
		"usaa-checking-2026-01b.csv": newer,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) != 2 {
		t.Fatalf("loaded %d rows, want 2 (Coffee Shop should be dropped): %+v", len(ts.Transactions), descriptions(ts.Transactions))
	}
	if containsDescription(descriptions(ts.Transactions), "Coffee Shop") {
		t.Error("Coffee Shop (pending, superseded) survived the load")
	}
}

// TestDropSupersededPending_AC2_DecisionInertWhenPendingSideDropped covers
// the interaction with stored duplicate decisions: a kept_winner decision
// whose kept side is the now-dropped pending row is inert once that row is
// gone -- the suppressed side stays live, counted, and off the unresolved
// queue. The fixture's pending row and its identically-dated/amounted
// posted twin also exercise the shape a real kept_winner decision would
// have named before this stage existed.
func TestDropSupersededPending_AC2_DecisionInertWhenPendingSideDropped(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-01-03,Coffee Shop,Dining,-20.00,Pending"
	newer := csvHeader + "\n" +
		"2026-01-01,Filler One,Misc,-1.00,Posted\n" +
		"2026-01-03,Coffee Shop,Dining,-20.00,Posted\n" +
		"2026-01-05,Filler Two,Misc,-2.00,Posted"

	date, err := time.Parse("2006-01-02", "2026-01-03")
	if err != nil {
		t.Fatalf("time.Parse: %v", err)
	}
	// P (the pending row, file a, loaded first) gets occurrence 0 in its
	// bucket; its identically-dated/amounted posted twin (file b, loaded
	// second) gets occurrence 1. See stampStableIDs' doc for why file order
	// is occurrence order.
	keptID := models.StableIDFor("usaa-checking", date, -2000, 0)
	suppressedID := models.StableIDFor("usaa-checking", date, -2000, 1)

	doc := duplicateDecisionsDoc{Decisions: map[string]DuplicateDecision{
		pairKey(keptID, suppressedID): {
			KeptHash:       keptID,
			SuppressedHash: suppressedID,
			Outcome:        DuplicateOutcomeKeptWinner,
			DecidedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-01a.csv": older,
		"usaa-checking-2026-01b.csv": newer,
		"duplicate_decisions.json":   string(data),
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	twin := findByStableID(t, ts.Transactions, suppressedID)
	if twin.Suppressed {
		t.Error("twin.Suppressed = true, want false: the stored decision names a kept side that no longer " +
			"exists after dropSupersededPending, so it must not suppress anything")
	}
	if got := len(loader.UnresolvedDuplicates()); got != 0 {
		t.Errorf("UnresolvedDuplicates() = %d, want 0 (the pending side of the only candidate pair is gone)", got)
	}
	active := ts.Active()
	sum := active.SumAmount()
	const want = -1.00 + -20.00 + -2.00
	if diff := sum - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("Active().SumAmount() = %v, want %v -- the twin must be counted", sum, want)
	}
}

// TestDropSupersededPending_AC3_NewestExportKeepsItsOwnPendingRows: a
// pending row in the file with the latest maxDate for its account survives
// -- nothing else is newer, so rule (c) can never be satisfied for it.
func TestDropSupersededPending_AC3_NewestExportKeepsItsOwnPendingRows(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-02-01,Old Row,Misc,-5.00,Posted"
	newest := csvHeader + "\n" +
		"2026-02-10,New Pending,Dining,-30.00,Pending"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-02a.csv": older,
		"usaa-checking-2026-02b.csv": newest,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if !containsDescription(descriptions(ts.Transactions), "New Pending") {
		t.Error("New Pending (in the newest file for its account) was dropped, want it to survive")
	}
}

// TestDropSupersededPending_AC4_UncoveredPendingRowSurvives: a newer file
// exists for the same account, but its minDate is after the pending row's
// date, so the row's date is not covered and rule (c) fails.
func TestDropSupersededPending_AC4_UncoveredPendingRowSurvives(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-03-01,Early Pending,Dining,-12.00,Pending"
	newer := csvHeader + "\n" +
		"2026-03-15,Later Row,Misc,-7.00,Posted\n" +
		"2026-03-20,Later Row Two,Misc,-3.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-03a.csv": older,
		"usaa-checking-2026-03b.csv": newer,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if !containsDescription(descriptions(ts.Transactions), "Early Pending") {
		t.Error("Early Pending (dated before the newer file's coverage begins) was dropped, want it to survive")
	}
}

// TestDropSupersededPending_AC5_UnassignedFilesNeverParticipate: no
// accounts are configured, so both files load unassigned (AccountID
// empty). An unassigned file must never supersede another, so the pending
// row survives even though a later, overlapping file exists.
func TestDropSupersededPending_AC5_UnassignedFilesNeverParticipate(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-04-01,Unassigned Pending,Misc,-9.00,Pending"
	newer := csvHeader + "\n" +
		"2026-03-25,Cover Row,Misc,-4.00,Posted\n" +
		"2026-04-10,Cover Row Two,Misc,-6.00,Posted"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"random-a.csv": older,
		"random-b.csv": newer,
	})
	defer cleanup()
	// Deliberately no writeAccounts call: every file loads unassigned.

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	found := false
	for _, txn := range ts.Transactions {
		if txn.Description == "Unassigned Pending" {
			found = true
			if txn.AccountID != "" {
				t.Errorf("AccountID = %q, want empty (unassigned file)", txn.AccountID)
			}
		}
	}
	if !found {
		t.Error("Unassigned Pending was dropped, want it to survive: unassigned files never supersede")
	}
}

// TestDropSupersededPending_AC6_EqualMaxDateNeverSupersedes: two files for
// the same account share the same maxDate, so neither is "strictly newer"
// than the other and rule (c) never fires.
func TestDropSupersededPending_AC6_EqualMaxDateNeverSupersedes(t *testing.T) {
	a := csvHeader + "\n" +
		"2026-05-10,Equal Pending,Dining,-15.00,Pending"
	b := csvHeader + "\n" +
		"2026-05-01,Filler,Misc,-1.00,Posted\n" +
		"2026-05-10,Filler Two,Misc,-2.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-05a.csv": a,
		"usaa-checking-2026-05b.csv": b,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if !containsDescription(descriptions(ts.Transactions), "Equal Pending") {
		t.Error("Equal Pending was dropped by a file with an equal (not strictly later) maxDate")
	}
}

// TestDropSupersededPending_AC7_OnlyPendingStatusDropped: Posted, Cleared,
// empty-status and Scheduled rows are never dropped by this stage, even
// when every other condition (same account, covered by a strictly newer
// file) holds.
func TestDropSupersededPending_AC7_OnlyPendingStatusDropped(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-06-01,Posted Row,Dining,-10.00,Posted\n" +
		"2026-06-01,Cleared Row,Dining,-11.00,Cleared\n" +
		"2026-06-01,Empty Status Row,Dining,-12.00,\n" +
		"2026-06-01,Scheduled Row,Dining,-13.00,Scheduled\n" +
		"2026-06-01,Pending Row,Dining,-14.00,Pending"
	newer := csvHeader + "\n" +
		"2026-05-30,Filler,Misc,-1.00,Posted\n" +
		"2026-06-15,Filler Two,Misc,-2.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-06a.csv": older,
		"usaa-checking-2026-06b.csv": newer,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	got := descriptions(ts.Transactions)
	for _, want := range []string{"Posted Row", "Cleared Row", "Empty Status Row", "Scheduled Row"} {
		if !containsDescription(got, want) {
			t.Errorf("%q was dropped, want it to survive (not a Pending status)", want)
		}
	}
	if containsDescription(got, "Pending Row") {
		t.Error("Pending Row survived, want it dropped (covered by the strictly newer file)")
	}
	if len(ts.Transactions) != 6 {
		t.Errorf("loaded %d rows, want 6 (4 non-pending survivors from the older file + 2 from the newer): %+v", len(ts.Transactions), got)
	}
}

// TestDropSupersededPending_AC8_StableIDStabilityAndIndexOnIdenticalKeyCopies
// covers the case IM1 exists to fix: a pending row and a Posted row sharing
// the exact same (date, description, amount) -- hence the same content Hash
// -- where the pending copy is older and gets dropped. Exactly one row must
// survive, it must be the Posted one, its occurrence index must be the one
// it would have had if nothing were ever dropped (|1, not renumbered to
// |0), and the published Hash -> StableID index must point at THAT
// StableID, not the dropped row's.
func TestDropSupersededPending_AC8_StableIDStabilityAndIndexOnIdenticalKeyCopies(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-02-10,Coffee Shop,Dining,-20.00,Pending"
	newer := csvHeader + "\n" +
		"2026-02-08,Filler,Misc,-1.00,Posted\n" +
		"2026-02-10,Coffee Shop,Dining,-20.00,Posted\n" +
		"2026-02-15,Filler Two,Misc,-2.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-02a.csv": older,
		"usaa-checking-2026-02b.csv": newer,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	var coffeeRows []models.Transaction
	for _, txn := range ts.Transactions {
		if txn.Description == "Coffee Shop" {
			coffeeRows = append(coffeeRows, txn)
		}
	}
	if len(coffeeRows) != 1 {
		t.Fatalf("found %d Coffee Shop rows, want exactly 1: %+v", len(coffeeRows), coffeeRows)
	}
	survivor := coffeeRows[0]
	if survivor.Status != "Posted" {
		t.Errorf("survivor.Status = %q, want Posted", survivor.Status)
	}

	date, err := time.Parse("2006-01-02", "2026-02-10")
	if err != nil {
		t.Fatalf("time.Parse: %v", err)
	}
	wantStableID := models.StableIDFor("usaa-checking", date, -2000, 1)
	if survivor.StableID != wantStableID {
		t.Errorf("survivor.StableID = %q, want %q (occurrence 1: unchanged by the drop)", survivor.StableID, wantStableID)
	}

	index := loader.stableIDIndex()
	if got := index[survivor.Hash]; got != wantStableID {
		t.Errorf("stableIDIndex()[%q] = %q, want %q (the survivor's StableID, not the dropped row's |0)",
			survivor.Hash, got, wantStableID)
	}
}

// TestDropSupersededPending_AC9_TransfersUnaffected: a real checking/credit
// transfer pair stays paired under the same pair_key after a pending row
// elsewhere in the checking account's history is dropped.
func TestDropSupersededPending_AC9_TransfersUnaffected(t *testing.T) {
	checkingOlder := csvHeader + "\n" +
		"2026-02-10,Random Charge,Shopping,-15.00,Pending"
	checkingNewer := csvHeader + "\n" +
		"2026-02-05,USAA CREDIT CARD PAYMENT,Transfer,-300.00,Posted\n" +
		"2026-02-10,Random Charge,Shopping,-15.00,Posted\n" +
		"2026-02-15,Filler,Misc,-1.00,Posted"
	// Kind credit force-flips every amount in this file (see ParseCSV), so
	// -300.00 here becomes +300.00 -- the opposite sign from the checking
	// leg's -300.00, which is what the pairing rule requires.
	creditCSV := csvHeader + "\n" +
		"2026-02-06,Payment Received,Payment,-300.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-a.csv": checkingOlder,
		"usaa-checking-b.csv": checkingNewer,
		"usaa-credit-a.csv":   creditCSV,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{
		{ID: "usaa-checking", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "usaa-credit", Name: "USAA Credit", Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"}},
	})

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	// The pending row is still dropped: same shape as AC1, just alongside
	// a transfer pair this time.
	count := 0
	for _, txn := range ts.Transactions {
		if txn.Description == "Random Charge" {
			count++
			if txn.Status != "Posted" {
				t.Errorf("surviving Random Charge has Status = %q, want Posted", txn.Status)
			}
		}
	}
	if count != 1 {
		t.Errorf("found %d Random Charge rows, want 1 (the Pending copy should be dropped)", count)
	}

	checkingLeg := findByDescription(t, ts, "USAA CREDIT CARD PAYMENT")
	creditLeg := findByDescription(t, ts, "Payment Received")
	if checkingLeg.TransactionType != models.Transfer || creditLeg.TransactionType != models.Transfer {
		t.Fatalf("legs not classified Transfer: checking=%v credit=%v", checkingLeg.TransactionType, creditLeg.TransactionType)
	}
	if checkingLeg.TransferClass != "paired" || creditLeg.TransferClass != "paired" {
		t.Fatalf("legs not classified paired: checking=%q credit=%q", checkingLeg.TransferClass, creditLeg.TransferClass)
	}
	if checkingLeg.TransferPairKey == "" || checkingLeg.TransferPairKey != creditLeg.TransferPairKey {
		t.Errorf("pair keys differ or empty: checking=%q credit=%q", checkingLeg.TransferPairKey, creditLeg.TransferPairKey)
	}
}

// TestDropSupersededPending_AC10_LogsSummaryLineOnlyWhenDropped covers the
// one summary log line: present with a total and a per-file breakdown when
// rows were dropped, silent when nothing was.
func TestDropSupersededPending_AC10_LogsSummaryLineOnlyWhenDropped(t *testing.T) {
	older := csvHeader + "\n" +
		"2026-01-03,Coffee Shop,Dining,-20.00,Pending"
	newer := csvHeader + "\n" +
		"2026-01-01,Filler One,Misc,-1.00,Posted\n" +
		"2026-01-05,Filler Two,Misc,-2.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{
		"usaa-checking-2026-01a.csv": older,
		"usaa-checking-2026-01b.csv": newer,
	})
	defer cleanup()
	writeAccounts(t, dir, []models.Account{{
		ID:           "usaa-checking",
		Name:         "USAA Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"usaa-checking*.csv"},
	}})

	var buf bytes.Buffer
	orig := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(orig)
		log.SetFlags(origFlags)
	}()

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Dropped 1 superseded pending rows") {
		t.Errorf("log output missing the summary line, got:\n%s", got)
	}
	if !strings.Contains(got, "usaa-checking-2026-01a.csv: 1") {
		t.Errorf("log output missing the per-file breakdown, got:\n%s", got)
	}

	// Zero drops: reload a fixture with nothing to drop and confirm silence.
	buf.Reset()
	_, loader2, cleanup2 := setupTestDir(t, map[string]string{
		"usaa-checking-2026-07.csv": csvHeader + "\n" + "2026-07-01,Only Row,Misc,-3.00,Posted",
	})
	defer cleanup2()
	if _, err := loader2.LoadData(); err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if strings.Contains(buf.String(), "Dropped") {
		t.Errorf("log output should be silent on zero drops, got:\n%s", buf.String())
	}
}

// TestParseCSV_AC11_MatchesLoadCSVFileForAccount pins the ParseCSV
// refactor: parsing the same bytes through the file-opening path
// (loadCSVFileForAccount) and directly through ParseCSV must produce
// identical slices, for a credit-kind account (exercises the forced sign
// flip and re-hash) and for acct == nil (exercises the unassigned path).
func TestParseCSV_AC11_MatchesLoadCSVFileForAccount(t *testing.T) {
	csvContent := csvHeader + "\n" +
		"2026-01-01,Wegmans,Groceries,50.00,Posted\n" +
		"2026-01-02,Walgreens,Pharmacy,15.00,Posted\n" +
		"2026-01-03,Netflix,Television,20.00,Cleared\n" +
		"2026-01-04,Spotify,Music,10.00,\n" +
		"2026-01-05,Target,Shopping,25.00,Posted\n" +
		"2026-01-06,Lowes,Hardware,12.00,Posted\n" +
		"2026-01-07,Coffee,Food,5.00,Posted\n" +
		"2026-01-08,Bistro,Restaurants,40.00,Posted\n" +
		"2026-01-09,Amazon,Shopping,30.00,Posted\n" +
		"2026-01-10,Wegmans,Groceries,60.00,Posted"

	dir, loader, cleanup := setupTestDir(t, map[string]string{"card.csv": csvContent})
	defer cleanup()
	path := filepath.Join(dir, "card.csv")

	creditAcct := &models.Account{ID: "the-card", Name: "The Card", Kind: models.AccountKindCredit}

	viaFile, err := loader.loadCSVFileForAccount(path, creditAcct)
	if err != nil {
		t.Fatalf("loadCSVFileForAccount (credit): %v", err)
	}
	viaParse, err := ParseCSV(strings.NewReader(csvContent), "card.csv", creditAcct)
	if err != nil {
		t.Fatalf("ParseCSV (credit): %v", err)
	}
	if !reflect.DeepEqual(viaFile, viaParse) {
		t.Errorf("credit-kind: loadCSVFileForAccount and ParseCSV disagree:\nfile:  %+v\nparse: %+v", viaFile, viaParse)
	}

	viaFileNil, err := loader.loadCSVFileForAccount(path, nil)
	if err != nil {
		t.Fatalf("loadCSVFileForAccount (nil): %v", err)
	}
	viaParseNil, err := ParseCSV(strings.NewReader(csvContent), "card.csv", nil)
	if err != nil {
		t.Fatalf("ParseCSV (nil): %v", err)
	}
	if !reflect.DeepEqual(viaFileNil, viaParseNil) {
		t.Errorf("acct == nil: loadCSVFileForAccount and ParseCSV disagree:\nfile:  %+v\nparse: %+v", viaFileNil, viaParseNil)
	}
}
