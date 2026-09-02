package dataloader

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func makeTx(date string, amount float64, desc, status string) models.Transaction {
	d, _ := time.Parse("2006-01-02", date)
	t := models.Transaction{
		Date:            d,
		Amount:          amount,
		Description:     desc,
		Status:          status,
		TransactionType: models.Outflow,
	}
	t.Hash = t.ComputeHash()
	return t
}

// makeTxOriginal is a sibling of makeTx for the same-day-reimport shape,
// which additionally keys on OriginalDescription.
func makeTxOriginal(date string, amount float64, desc, status, original string) models.Transaction {
	t := makeTx(date, amount, desc, status)
	t.OriginalDescription = original
	return t
}

// makeTxAccount is a sibling of makeTx for the pending->posted shape, which
// additionally keys on AccountID.
func makeTxAccount(date string, amount float64, desc, status, account string) models.Transaction {
	t := makeTx(date, amount, desc, status)
	t.AccountID = account
	return t
}

func TestDetect_SameDayReimport_LucidPair(t *testing.T) {
	// Live example from IMPORT_FIXES_SPEC.md T13: the aggregator's first
	// export carries the raw bank text with a "Category Pending"
	// placeholder; a later export rewrites Description and assigns a
	// category, but Original Description stays identical.
	const original = "Lucid BILL PMT                622400A0AHSQ"
	left := makeTxOriginal("2026-08-12", -1580.43, original, "Posted", original)
	left.Category = "Category Pending"
	right := makeTxOriginal("2026-08-12", -1580.43, "Lucid Bill a Ahsq", "Posted", original)
	right.Category = "Bills & Utilities"
	txns := []models.Transaction{left, right}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestDetect_SameDayReimport_WindowExceeded(t *testing.T) {
	const original = "Lucid BILL PMT                622400A0AHSQ"
	txns := []models.Transaction{
		makeTxOriginal("2026-08-12", -1580.43, original, "Posted", original),
		makeTxOriginal("2026-08-14", -1580.43, "Lucid Bill a Ahsq", "Posted", original), // 2 days
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (window exceeded), got %d", len(got))
	}
}

func TestDetect_SameDayReimport_DifferentOriginalDescription(t *testing.T) {
	txns := []models.Transaction{
		makeTxOriginal("2026-08-12", -1580.43, "Lucid BILL PMT 622400A0AHSQ", "Posted", "Lucid BILL PMT 622400A0AHSQ"),
		makeTxOriginal("2026-08-12", -1580.43, "Lucid Bill a Ahsq", "Posted", "Some Other Raw Text"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (different Original Description), got %d", len(got))
	}
}

func TestDetect_SameDayReimport_EmptyOriginalNotPaired(t *testing.T) {
	const original = "Lucid BILL PMT                622400A0AHSQ"
	txns := []models.Transaction{
		makeTxOriginal("2026-08-12", -1580.43, original, "Posted", original),
		makeTxOriginal("2026-08-12", -1580.43, "Lucid Bill a Ahsq", "Posted", ""),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (empty Original Description), got %d", len(got))
	}
}

func TestDetect_PositiveLucidCase(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestDetect_NegativeTooFarApart(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-27", -1580.43, "Check #996583", "Posted"), // 8 days
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeBothChecks(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Check #996582", "Posted"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeBothBillPays(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Toyota", "Pending"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeOppositeSign(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", 1580.43, "Check #996583", "Posted")
	check.TransactionType = models.Income
	txns := []models.Transaction{billPay, check}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (opposite signs), got %d", len(got))
	}
}

func TestDetect_NegativeWrongAmount(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.44, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (different cents), got %d", len(got))
	}
}

func TestDetect_PositiveEmptyStatusOnBillPaySide(t *testing.T) {
	// Bill-pay side has no status; check side has Posted. Should pair.
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", ""),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 1 {
		t.Errorf("expected 1 pair, got %d", len(got))
	}
}

func TestDetect_PositiveEmptyStatusOnCheckSide(t *testing.T) {
	// Check side has no status (description-only signal). Should pair.
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", ""),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 1 {
		t.Errorf("expected 1 pair, got %d", len(got))
	}
}

func TestDetect_TripletPicksClosestDate(t *testing.T) {
	// Three same-amount transactions, mixed roles. The bill-pay should
	// pair with the check that's closest in date (3/20), not the one
	// that's farther (3/24).
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	closeCheck := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")
	farCheck := makeTx("2026-03-24", -1580.43, "Check #996590", "Posted")
	txns := []models.Transaction{billPay, closeCheck, farCheck}

	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly 1 pair, got %d: %+v", len(pairs), pairs)
	}
	gotHashes := map[string]bool{pairs[0].Left.Hash: true, pairs[0].Right.Hash: true}
	if !gotHashes[billPay.Hash] || !gotHashes[closeCheck.Hash] {
		t.Errorf("expected pair (billPay, closeCheck), got %+v", gotHashes)
	}
	if gotHashes[farCheck.Hash] {
		t.Error("farCheck should not be in any pair")
	}
}

func TestDetect_PairKeyIsOrderIndependent(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")

	a := detectNearDuplicatePairs([]models.Transaction{billPay, check})
	b := detectNearDuplicatePairs([]models.Transaction{check, billPay})

	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected 1 pair each, got %d / %d", len(a), len(b))
	}
	if a[0].Key != b[0].Key {
		t.Errorf("pair key should be order-independent: %q vs %q", a[0].Key, b[0].Key)
	}
}

func TestDetect_Idempotency(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	a := detectNearDuplicatePairs(txns)
	b := detectNearDuplicatePairs(txns)
	if len(a) != 1 || len(b) != 1 || a[0].Key != b[0].Key {
		t.Errorf("detection should be idempotent: %+v vs %+v", a, b)
	}
}

func TestDetect_CheckRegexTolerance(t *testing.T) {
	// Various check-description shapes should all match.
	for _, desc := range []string{"Check #996583", "CHECK # 996583", "Check#996583", "Check #996583 cleared"} {
		txns := []models.Transaction{
			makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
			makeTx("2026-03-20", -1580.43, desc, "Posted"),
		}
		if got := detectNearDuplicatePairs(txns); len(got) != 1 {
			t.Errorf("desc %q should still match check pattern, got %d pairs", desc, len(got))
		}
	}
}

// --- Third shape: pending->posted settlement pair ---

// Verbatim live example from the DP1 defect report: an aggregator export
// captured the charge while Pending, a later overlapping export captured
// it Posted with BOTH Description and Original Description rewritten by
// the bank.
func TestDetect_PendingPosted_HarborFreightVerbatim(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	pending.OriginalDescription = "Harbor Freight Tools USA"
	posted := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit")
	posted.OriginalDescription = "HARBOR FREIGHT TOOLS3185 PENFIELD     NY"

	txns := []models.Transaction{pending, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestDetect_PendingPosted_CrossAccountNoMatch(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", "usaa-checking")
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (different accounts), got %d", len(got))
	}
}

func TestDetect_PendingPosted_WindowExceeded(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-07", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit") // 6 days
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (window exceeded), got %d", len(got))
	}
}

func TestDetect_PendingPosted_WindowBoundaryFiveDays(t *testing.T) {
	// Pins the window at exactly 5: together with WindowExceeded (6 days →
	// 0 pairs) this kills both the 4-day and 6-day mutants of
	// pendingPostedWindowDays.
	pending := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-06", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit") // 5 days
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 1 {
		t.Errorf("expected 1 pair at the 5-day boundary, got %d", len(got))
	}
}

func TestDetect_PendingPosted_FourDayLag_AmazonVerbatim(t *testing.T) {
	// Verbatim 2025-11 live rows: a card capture that settles 4 days later
	// (weekend + holiday lag). Outside the old 3-day window — the genuine
	// settlement pair that motivated widening it to 5 (ND3).
	pending := makeTxAccount("2025-11-16", -30.81, "AMAZON MKTPLACE PMTS", "Pending", "usaa-credit")
	pending.OriginalDescription = "AMAZON MKTPLACE PMTS"
	posted := makeTxAccount("2025-11-20", -30.81, "Amazon", "Posted", "usaa-credit")
	posted.OriginalDescription = "AMAZON MKTPL*B02UM4581   Amzn.com/billWA"

	txns := []models.Transaction{pending, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair (4-day settlement lag), got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestDetect_PendingPosted_AlienDescriptionNoMatch(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Home Depot", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit")
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (alien description), got %d", len(got))
	}
}

func TestDetect_PendingPosted_BothPendingNoMatch(t *testing.T) {
	a := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	a.OriginalDescription = "Harbor Freight Tools USA"
	b := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Pending", "usaa-credit")
	// Distinct OriginalDescription so shape 2 (same-day reimport) cannot
	// fire; this isolates shape 3's status-split rule.
	b.OriginalDescription = "Harbor Freight Tools"
	txns := []models.Transaction{a, b}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (both pending), got %d", len(got))
	}
}

func TestDetect_PendingPosted_PostedEmptyStatusNoMatch(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "", "usaa-credit")
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (posted side has empty status), got %d", len(got))
	}
}

func TestDetect_PendingPosted_ShortPrefixNoMatch(t *testing.T) {
	pending := makeTxAccount("2026-05-01", -188.98, "Ab Cdefghij", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-01", -188.98, "Ab Cdxyzabc", "Posted", "usaa-credit")
	// Neither is a prefix of the other, and the shared prefix ("ab cd" ==
	// 5 bytes) is below pendingPostedPrefixMinLen (12).
	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (prefix too short), got %d", len(got))
	}
}

// --- Gap A: orig-desc affinity fallback ---

func TestDetect_PendingPosted_OrigDescAffinityWhenDescriptionPrefixFails(t *testing.T) {
	// Description prefix rule fails outright ("BJS WHOLESALE #0075" vs the
	// bank's prettified "BJ's Wholesale" share no useful prefix), but
	// OriginalDescription on the posted side still carries the pending
	// side's raw text as a prefix.
	pending := makeTxAccount("2026-05-02", -634.40, "BJS WHOLESALE #0075", "Pending", "usaa-credit")
	pending.OriginalDescription = "BJS WHOLESALE #0075"
	posted := makeTxAccount("2026-05-02", -634.40, "BJ's Wholesale", "Posted", "usaa-credit")
	posted.OriginalDescription = "BJS WHOLESALE #0075      WEBSTER      NY"

	txns := []models.Transaction{pending, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair via orig-desc affinity, got %d", len(pairs))
	}
}

func TestDetect_PendingPosted_OrigDescEmptyOnOneSideNoMatch(t *testing.T) {
	// Description prefix also fails, and OriginalDescription is empty on
	// the pending side, so the orig-desc fallback must not fire either.
	pending := makeTxAccount("2026-05-02", -634.40, "BJS WHOLESALE #0075", "Pending", "usaa-credit")
	posted := makeTxAccount("2026-05-02", -634.40, "BJ's Wholesale", "Posted", "usaa-credit")
	posted.OriginalDescription = "BJS WHOLESALE #0075      WEBSTER      NY"

	txns := []models.Transaction{pending, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (empty orig-desc on pending side), got %d", len(got))
	}
}

// --- Gap B: scheduled -> settled (non-check) pair ---

func TestDetect_ScheduledSettled_InsuranceVerbatim(t *testing.T) {
	scheduled := makeTxAccount("2026-05-04", -464.99, "USAA INSURANCE BILL PAYMENT", "Recurring Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-05", -464.99, "USAA Property and Casualty Insurance", "Posted", "usaa-checking")
	posted.OriginalDescription = "USAA P&C         AUTOPAY    ***********1380"

	txns := []models.Transaction{scheduled, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
}

func TestDetect_ScheduledSettled_MonroeWaterVerbatim(t *testing.T) {
	scheduled := makeTxAccount("2026-05-22", -20.00, "Monroe County Water Authority", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-22", -20.00, "Monroe Water", "Posted", "usaa-checking")
	posted.OriginalDescription = "MONROE WATER     ONLINE PMT ***********7POS"

	txns := []models.Transaction{scheduled, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
}

func TestDetect_ScheduledSettled_SingleSharedTokenNoMatch(t *testing.T) {
	scheduled := makeTxAccount("2026-05-22", -20.00, "Monroe County Water Authority", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-22", -20.00, "Wire Fee", "Posted", "usaa-checking")
	posted.OriginalDescription = "WIRE FEE"

	txns := []models.Transaction{scheduled, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (0 shared tokens), got %d", len(got))
	}
}

func TestDetect_ScheduledSettled_BothPostedNoMatch(t *testing.T) {
	a := makeTxAccount("2026-05-22", -20.00, "Monroe Water", "Posted", "usaa-checking")
	b := makeTxAccount("2026-05-25", -20.00, "Monroe Water", "Posted", "usaa-checking")
	txns := []models.Transaction{a, b}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (neither side scheduled), got %d", len(got))
	}
}

func TestDetect_ScheduledSettled_CrossAccountNoMatch(t *testing.T) {
	scheduled := makeTxAccount("2026-05-22", -20.00, "Monroe County Water Authority", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-22", -20.00, "Monroe Water", "Posted", "usaa-credit")
	txns := []models.Transaction{scheduled, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (different accounts), got %d", len(got))
	}
}

func TestDetect_ScheduledSettled_WindowExceededNoMatch(t *testing.T) {
	scheduled := makeTxAccount("2026-05-04", -464.99, "USAA INSURANCE BILL PAYMENT", "Recurring Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-12", -464.99, "USAA Property and Casualty Insurance", "Posted", "usaa-checking") // 8 days
	txns := []models.Transaction{scheduled, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (window exceeded), got %d", len(got))
	}
}

func TestDetect_ScheduledSettled_CheckShapeRegression(t *testing.T) {
	// A scheduled bill pay settling as an actual Check #NNN row must still
	// pair via the original classify() shape; Gap B must not interfere
	// (and cannot, since it explicitly excludes check-shaped descriptions).
	scheduled := makeTxAccount("2026-05-15", -626.00, "Hyundi Motor Finance", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-19", -626.00, "Check #996593", "Posted", "usaa-checking")
	posted.OriginalDescription = "CHECK # 0000996593"

	txns := []models.Transaction{scheduled, posted}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("check-settlement shape regressed: expected 1 pair, got %d", len(pairs))
	}
}

// --- Mutation-kill regression tests (ND2, promoted from ND1 checker probes) ---

// TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch guards the
// sharedTokenCount(...) >= 2 threshold in isScheduledSettledPair. Unlike
// TestDetect_ScheduledSettled_SingleSharedTokenNoMatch (which shares zero
// tokens and so also survives a >= 1 mutation), this pair shares exactly
// one token ("lucid"), so it only fails to pair under the real >= 2 rule.
func TestDetect_ScheduledSettled_ExactlyOneSharedTokenNoMatch(t *testing.T) {
	scheduled := makeTxAccount("2026-05-19", -1580.43, "Lucid", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-21", -1580.43, "Lucid Bill Payment", "Posted", "usaa-checking")
	posted.OriginalDescription = "LUCID BILL PMT 622400A0AHSQ"

	txns := []models.Transaction{scheduled, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (exactly 1 shared token, below the >=2 threshold), got %d", len(got))
	}
}

// TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch guards the
// checkPrefixRE exclusion in isScheduledSettledPair. The scheduled side's
// Description looks check-shaped but its Status has no posted keyword, so
// classify() claims neither side (leaving Gap B as the only path); every
// other isScheduledSettledPair condition holds (2 shared tokens, same
// account, same day, scheduled/posted split), so only the exclusion itself
// keeps this pair from matching.
func TestDetect_ScheduledSettled_CheckPrefixExclusionNoMatch(t *testing.T) {
	scheduled := makeTxAccount("2026-05-22", -20.00, "Check #12345 Monroe Water", "Scheduled Bill Pay", "usaa-checking")
	posted := makeTxAccount("2026-05-22", -20.00, "Monroe Water", "Posted", "usaa-checking")
	posted.OriginalDescription = "MONROE WATER     ONLINE PMT ***********7POS"

	txns := []models.Transaction{scheduled, posted}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (checkPrefixRE exclusion), got %d", len(got))
	}
}

func TestDetect_PendingPosted_NegativeBothPosted(t *testing.T) {
	// Two Posted transactions with same account, amount, and date, but
	// different OriginalDescription values. The same-day-reimport shape
	// cannot fire (different Original Description), and the pending->posted
	// shape cannot fire (both are Posted, not Pending->Posted).
	a := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", "usaa-credit")
	a.OriginalDescription = "HARBOR FREIGHT TOOLS3185 PENFIELD     NY"
	b := makeTxAccount("2026-05-01", -188.98, "Harbor Freight Tools USA", "Posted", "usaa-credit")
	b.OriginalDescription = "Harbor Freight Tools USA"
	txns := []models.Transaction{a, b}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (both Posted), got %d", len(got))
	}
}
