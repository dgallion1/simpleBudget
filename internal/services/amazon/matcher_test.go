package amazon

import (
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

func mkTx(date string, amount float64, desc string) models.Transaction {
	t, _ := time.Parse("2006-01-02", date)
	tx := models.Transaction{
		Date:        t,
		Amount:      amount,
		Description: desc,
	}
	tx.Hash = tx.ComputeHash()
	return tx
}

func mkShip(shipDate string, total float64, orderID string, products ...string) Shipment {
	t, _ := time.Parse("2006-01-02", shipDate)
	return Shipment{
		OrderID:   orderID,
		OrderDate: t,
		ShipDate:  t,
		Total:     total,
		Products:  products,
		Source:    SourceRetail,
	}
}

func TestMatch_NonAmazonIgnored(t *testing.T) {
	txs := []models.Transaction{mkTx("2024-01-05", -10.00, "Walmart")}
	ships := []Shipment{mkShip("2024-01-05", 10.00, "111", "Item")}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 0 {
		t.Fatalf("expected 0 matches for non-Amazon tx; got %d (%+v)", len(got), got)
	}
}

func TestMatch_ExactAmountInWindow(t *testing.T) {
	txs := []models.Transaction{mkTx("2024-01-05", -11.21, "AMZN MKTP US")}
	ships := []Shipment{mkShip("2024-01-04", 11.21, "111", "Rosemary Whole Organic 1 LB")}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 1 {
		t.Fatalf("expected 1 match; got %d", len(got))
	}
	if got[0].Label != "Amazon: Rosemary Whole Organic 1 LB" {
		t.Errorf("Label = %q", got[0].Label)
	}
	if got[0].OrderIDs[0] != "111" {
		t.Errorf("OrderIDs = %v", got[0].OrderIDs)
	}
}

func TestMatch_OutOfWindow(t *testing.T) {
	txs := []models.Transaction{mkTx("2024-01-20", -10.00, "Amazon.com")}
	ships := []Shipment{mkShip("2024-01-05", 10.00, "111", "Item")}
	got := Match(txs, ships, MatchOptions{WindowDays: 5})
	if len(got) != 0 {
		t.Fatalf("expected 0 matches outside 5-day window; got %d", len(got))
	}
}

func TestMatch_AmountMismatch(t *testing.T) {
	txs := []models.Transaction{mkTx("2024-01-05", -10.50, "Amazon.com")}
	ships := []Shipment{mkShip("2024-01-05", 10.00, "111", "Item")}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 0 {
		t.Fatalf("expected 0 matches when amount differs; got %d", len(got))
	}
}

func TestMatch_AmbiguousAmountSkipped(t *testing.T) {
	// Two distinct shipments at $9.99 in window — refuse to guess.
	txs := []models.Transaction{mkTx("2024-01-05", -9.99, "AMZN MKTP US")}
	ships := []Shipment{
		mkShip("2024-01-04", 9.99, "111", "Item A"),
		mkShip("2024-01-05", 9.99, "222", "Item B"),
	}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 0 {
		t.Fatalf("expected 0 matches when ambiguous; got %d", len(got))
	}
}

func TestMatch_MultiShipmentSum(t *testing.T) {
	// One Order ID split into two shipments billed as one charge.
	txs := []models.Transaction{mkTx("2024-01-08", -25.00, "AMAZON.COM*ABC")}
	ships := []Shipment{
		mkShip("2024-01-07", 10.00, "111", "Coffee Beans"),
		mkShip("2024-01-08", 15.00, "111", "Filters"),
	}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 1 {
		t.Fatalf("expected 1 multi-shipment match; got %d", len(got))
	}
	if got[0].Label != "Amazon: Coffee Beans +1 more" {
		t.Errorf("Label = %q", got[0].Label)
	}
}

func TestMatch_MultiProductLabel(t *testing.T) {
	txs := []models.Transaction{mkTx("2024-01-05", -30.00, "Amazon.com")}
	ships := []Shipment{
		mkShip("2024-01-05", 30.00, "111", "Foo", "Bar", "Baz"),
	}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 1 {
		t.Fatalf("expected 1 match; got %d", len(got))
	}
	if got[0].Label != "Amazon: Foo +2 more" {
		t.Errorf("Label = %q", got[0].Label)
	}
}

func TestMatch_TruncatesLongProduct(t *testing.T) {
	long := "X" + strings.Repeat("y", 200)
	txs := []models.Transaction{mkTx("2024-01-05", -1.00, "Amazon.com")}
	ships := []Shipment{mkShip("2024-01-05", 1.00, "111", long)}
	got := Match(txs, ships, MatchOptions{MaxLabelLen: 20})
	if len(got) != 1 {
		t.Fatalf("expected 1 match; got %d", len(got))
	}
	// Label = "Amazon: " + truncated 20 chars + "…"
	if !strings.HasPrefix(got[0].Label, "Amazon: X") {
		t.Errorf("Label prefix = %q", got[0].Label)
	}
	if !strings.HasSuffix(got[0].Label, "…") {
		t.Errorf("Label should end with ellipsis: %q", got[0].Label)
	}
	// Total runes should be "Amazon: " (8) + 20 + 1 ellipsis = 29
	if n := len([]rune(got[0].Label)); n != 29 {
		t.Errorf("Label rune len = %d, want 29 (%q)", n, got[0].Label)
	}
}

func TestMatch_ConsumedShipmentNotReused(t *testing.T) {
	// Two identical-amount Amazon txns, but only one shipment that matches.
	// The first tx (oldest) consumes it; the second goes unmatched.
	txs := []models.Transaction{
		mkTx("2024-01-05", -11.00, "Amazon.com"),
		mkTx("2024-01-06", -11.00, "Amazon.com"),
	}
	ships := []Shipment{mkShip("2024-01-05", 11.00, "111", "Single Item")}
	got := Match(txs, ships, MatchOptions{})
	if len(got) != 1 {
		t.Fatalf("expected 1 match; got %d", len(got))
	}
	if got[0].TxHash != txs[0].Hash {
		t.Errorf("expected oldest tx to claim shipment; got hash %q", got[0].TxHash)
	}
}

func TestMatchByDescription_OrderIDInDesc(t *testing.T) {
	txs := []models.Transaction{
		mkTx("2024-06-01", -50.00, "AMZN Mktp US 111-9999999-1234567"),
	}
	ships := []Shipment{
		// 60 days off — would never match by amount/window.
		mkShip("2024-04-01", 50.00, "111-9999999-1234567", "Far Item"),
	}
	got := MatchByDescription(txs, ships, nil, nil, MatchOptions{})
	if len(got) != 1 {
		t.Fatalf("expected 1 desc match; got %d", len(got))
	}
	if got[0].Label != "Amazon: Far Item" {
		t.Errorf("Label = %q", got[0].Label)
	}
}

func TestMatchByDescription_SkipsAlreadyMatched(t *testing.T) {
	tx := mkTx("2024-06-01", -50.00, "AMZN Mktp US 111-9999999-1234567")
	txs := []models.Transaction{tx}
	ships := []Shipment{mkShip("2024-04-01", 50.00, "111-9999999-1234567", "Far Item")}
	already := map[string]bool{tx.Hash: true}
	got := MatchByDescription(txs, ships, already, nil, MatchOptions{})
	if len(got) != 0 {
		t.Fatalf("expected 0 (already matched); got %d", len(got))
	}
}

func TestMatchByDescription_SkipsConsumedOrderIDs(t *testing.T) {
	// Pass 1 (Match) attributed order 111-9999999-1234567 to txA via
	// amount+window. Pass 2 sees txB whose description embeds the same
	// order ID but has a different (unrelated) amount. Without the
	// consumed-order-ID guard, txB would inherit txA's label.
	txA := mkTx("2024-04-03", -50.00, "AMZN Mktp US")
	txB := mkTx("2024-06-01", -7.99, "AMZN Mktp US 111-9999999-1234567")
	ships := []Shipment{
		mkShip("2024-04-01", 50.00, "111-9999999-1234567", "Far Item"),
	}

	already := map[string]bool{txA.Hash: true}
	consumed := map[string]bool{"111-9999999-1234567": true}

	got := MatchByDescription([]models.Transaction{txA, txB}, ships, already, consumed, MatchOptions{})
	if len(got) != 0 {
		t.Fatalf("expected 0 (order id already consumed); got %d (%+v)", len(got), got)
	}
}
