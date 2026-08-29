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
