package importer

import (
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// row is a small constructor so the table tests below read as data, not
// boilerplate.
func row(t *testing.T, date, desc string, amount float64, accountID string) models.Transaction {
	return models.Transaction{
		Date:        mustDate(t, date),
		Description: desc,
		Amount:      amount,
		AccountID:   accountID,
	}
}

var testAccounts = []models.Account{
	{ID: "checking", Name: "USAA Checking"},
	{ID: "savings", Name: "USAA Savings"},
}

// threeSharedRows returns three rows that, when also present (by key) in
// the ledger for accountID, are enough to make that account a unique
// candidate.
func threeSharedRows(t *testing.T, accountID string) []models.Transaction {
	return []models.Transaction{
		row(t, "2026-01-01", "Wegmans", -50.00, accountID),
		row(t, "2026-01-02", "Netflix", -15.99, accountID),
		row(t, "2026-01-03", "Paycheck", 2000.00, accountID),
	}
}

func TestDetect_UniqueMatch(t *testing.T) {
	ledger := threeSharedRows(t, "checking")
	// The file being detected carries no AccountID yet — that's the point.
	candidate := threeSharedRows(t, "")

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "checking" {
		t.Fatalf("AccountID = %q, want %q", got.AccountID, "checking")
	}
	if got.AccountName != "USAA Checking" {
		t.Fatalf("AccountName = %q, want %q", got.AccountName, "USAA Checking")
	}
	if got.Reason != "" {
		t.Fatalf("Reason = %q, want empty for a unique match", got.Reason)
	}
}

func TestDetect_Ambiguous_ListsBothNamesSorted(t *testing.T) {
	shared := threeSharedRows(t, "")
	ledger := append(threeSharedRows(t, "savings"), threeSharedRows(t, "checking")...)

	got := Detect(shared, ledger, testAccounts)
	if got.AccountID != "" {
		t.Fatalf("AccountID = %q, want empty for an ambiguous match", got.AccountID)
	}
	want := "ambiguous: USAA Checking, USAA Savings"
	if got.Reason != want {
		t.Fatalf("Reason = %q, want %q", got.Reason, want)
	}
}

func TestDetect_TwoSharedKeys_NoMatch(t *testing.T) {
	ledger := threeSharedRows(t, "checking")
	// Only two of the three keys overlap.
	candidate := threeSharedRows(t, "")[:2]

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match", got)
	}
}

func TestDetect_ZeroSharedKeys_NoMatch(t *testing.T) {
	ledger := threeSharedRows(t, "checking")
	candidate := []models.Transaction{
		row(t, "2026-06-01", "Something Else Entirely", -9.99, ""),
	}

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match", got)
	}
}

// A credit-kind account's file, parsed while still unassigned, can have its
// signs flipped the OTHER way from how the account's own rows are signed
// (ParseCSV's heuristic runs before the account is known). Detect must
// still match on absolute cents.
func TestDetect_CreditKindSignFlip_StillMatchesOnAbsoluteCents(t *testing.T) {
	ledger := []models.Transaction{
		row(t, "2026-02-01", "Amazon", -40.00, "checking"), // bank convention: charge is negative
		row(t, "2026-02-02", "Gas Station", -25.00, "checking"),
		row(t, "2026-02-03", "Payment Received", 500.00, "checking"),
	}
	// Same rows, unassigned parse flipped every sign (credit-card
	// convention: charge positive, payment negative).
	candidate := []models.Transaction{
		row(t, "2026-02-01", "Amazon", 40.00, ""),
		row(t, "2026-02-02", "Gas Station", 25.00, ""),
		row(t, "2026-02-03", "Payment Received", -500.00, ""),
	}

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "checking" {
		t.Fatalf("AccountID = %q, want %q (got %+v)", got.AccountID, "checking", got)
	}
}

func TestDetect_EmptyLedger_NoMatch(t *testing.T) {
	candidate := threeSharedRows(t, "")
	got := Detect(candidate, nil, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match", got)
	}
}

// A first-ever export for a brand-new account (no prior rows loaded for it
// at all) is "no match" by design — there is nothing to overlap with yet.
func TestDetect_FirstEverExportForNewAccount_NoMatch(t *testing.T) {
	ledger := threeSharedRows(t, "checking")
	candidate := threeSharedRows(t, "") // shares nothing with "checking"'s dates/desc
	candidate[0].Description = "Brand New Merchant"
	candidate[1].Description = "Another New One"
	candidate[2].Description = "Yet Another"

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match", got)
	}
}

// Ledger rows with an empty AccountID (unassigned rows already sitting in
// the loaded ledger) must never vote for a candidate.
func TestDetect_UnassignedLedgerRows_NeverCount(t *testing.T) {
	ledger := threeSharedRows(t, "") // unassigned
	candidate := threeSharedRows(t, "")

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match (unassigned ledger rows must not count)", got)
	}
}

func TestName_DateRange(t *testing.T) {
	rows := []models.Transaction{
		row(t, "2026-07-01", "A", -1, "usaa-credit-card"),
		row(t, "2026-08-15", "B", -2, "usaa-credit-card"),
		row(t, "2026-09-18", "C", -3, "usaa-credit-card"),
	}
	got := Name("usaa-credit-card", rows)
	want := "usaa-credit-card_2026-07-01_to_2026-09-18.csv"
	if got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestName_IgnoresZeroDates(t *testing.T) {
	rows := []models.Transaction{
		{Description: "no date", Amount: -1}, // zero Date
		row(t, "2026-01-05", "dated", -2, "acct"),
	}
	got := Name("acct", rows)
	want := "acct_2026-01-05_to_2026-01-05.csv"
	if got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

// A generated name only means something if the account's own file pattern
// would actually claim it on a future load. An account configured with an
// unrelated pattern must not match its own generated name — the handler
// uses this exact check (accounts.MatchFile) to refuse such an import.
func TestName_MismatchedPatternDoesNotSelfMatch(t *testing.T) {
	accts := []models.Account{
		{ID: "usaa-credit-card", Name: "USAA Credit Card", FilePatterns: []string{"other*.csv"}},
	}
	rows := []models.Transaction{
		row(t, "2026-07-01", "A", -1, "usaa-credit-card"),
		row(t, "2026-09-18", "B", -2, "usaa-credit-card"),
	}
	name := Name("usaa-credit-card", rows)
	if got := accounts.MatchFile(accts, name); got == "usaa-credit-card" {
		t.Fatalf("MatchFile(%q) matched despite an unrelated pattern; accts=%+v", name, accts)
	}
}

func TestContentIdentical_MatchAndNoMatch(t *testing.T) {
	data := []byte("Date,Description,Amount\n2026-01-01,A,-1.00\n")
	existing := map[string][]byte{
		"other.csv":     []byte("Date,Description,Amount\n2026-02-02,B,-2.00\n"),
		"identical.csv": append([]byte(nil), data...),
	}
	if got := ContentIdentical(data, existing); got != "identical.csv" {
		t.Fatalf("ContentIdentical() = %q, want %q", got, "identical.csv")
	}

	delete(existing, "identical.csv")
	if got := ContentIdentical(data, existing); got != "" {
		t.Fatalf("ContentIdentical() = %q, want empty (no match)", got)
	}
}

func TestContentIdentical_TieBreaksAlphabetically(t *testing.T) {
	data := []byte("same bytes")
	existing := map[string][]byte{
		"zzz.csv": []byte("same bytes"),
		"aaa.csv": []byte("same bytes"),
	}
	if got := ContentIdentical(data, existing); got != "aaa.csv" {
		t.Fatalf("ContentIdentical() = %q, want %q (alphabetically first)", got, "aaa.csv")
	}
}

func TestNormalizeDescription_CaseTrimAndWhitespace(t *testing.T) {
	if got := normalizeDescription("  Wegmans   #123  "); got != "wegmans #123" {
		t.Fatalf("normalizeDescription() = %q", got)
	}
}

func TestAbsCents_SignIndependent(t *testing.T) {
	if a, b := absCents(-40.00), absCents(40.00); a != b {
		t.Fatalf("absCents(-40.00)=%d absCents(40.00)=%d, want equal", a, b)
	}
	if got := absCents(-4.505); got != 451 {
		// -450.5 -> rounds to -451 via the -0.5 offset before negation;
		// pin the exact rounding rule so a future edit can't silently
		// change it.
		t.Fatalf("absCents(-4.505) = %d, want 451", got)
	}
}

func TestDetect_ReasonNeverMentionsUnknownAccount(t *testing.T) {
	// A ledger row whose AccountID no longer has a matching entry in accts
	// (deleted account) still resolves to something displayable rather than
	// panicking or rendering an empty name.
	ledger := threeSharedRows(t, "ghost")
	candidate := threeSharedRows(t, "")
	got := Detect(candidate, ledger, nil)
	if got.AccountID != "ghost" || !strings.Contains(got.AccountName, "ghost") {
		t.Fatalf("got %+v, want AccountID=ghost and a displayable fallback name", got)
	}
}

// A single coincidental key that happens to appear three times in the file
// (one recurring identical charge on one day, a $0 placeholder row) must not
// reach the threshold by repetition alone: the threshold counts DISTINCT
// shared keys. (Promoted from the IM2 attempt-1 primary-checker finding F1,
// ruling 2026-09-18c.)
func TestDetect_RepeatedCoincidentalKey_DoesNotReachThreshold(t *testing.T) {
	ledger := []models.Transaction{
		row(t, "2026-01-01", "Wegmans", -50.00, "checking"),
		row(t, "2026-01-02", "Netflix", -15.99, "checking"),
	}
	candidate := []models.Transaction{
		row(t, "2026-01-01", "Wegmans", -50.00, ""),
		row(t, "2026-01-01", "Wegmans", -50.00, ""),
		row(t, "2026-01-01", "Wegmans", -50.00, ""),
	}

	got := Detect(candidate, ledger, testAccounts)
	if got.AccountID != "" || got.Reason != "no match" {
		t.Fatalf("got %+v, want no match: one distinct shared key repeated three times is still one key", got)
	}

	// Three DISTINCT shared keys still detect (the threshold itself is unchanged).
	got = Detect(threeSharedRows(t, ""), threeSharedRows(t, "checking"), testAccounts)
	if got.AccountID != "checking" {
		t.Fatalf("control: got %+v, want checking", got)
	}
}
