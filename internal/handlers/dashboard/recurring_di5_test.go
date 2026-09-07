package dashboard

import (
	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"testing"
	"time"
)

func di5ProjectionData() (*models.TransactionSet, time.Time) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var txns []models.Transaction
	for _, name := range []string{"Alder", "Birch", "Cedar", "Dogwood", "Elm", "Fir", "Ginkgo", "Hazel", "Ivy", "Juniper", "Kapok", "Larch", "Maple", "Nutmeg", "Oak", "Pine", "Quince", "Rowan", "Spruce", "Teak", "Electric Company", "Netflix"} {
		amount := -100.0
		if name == "Netflix" {
			amount = -10
		}
		for i := 0; i < 3; i++ {
			txns = append(txns, models.Transaction{AccountID: "checker-cash", Description: name, Category: "Household", Amount: amount, Date: now.AddDate(0, -i, 0), TransactionType: models.Outflow})
		}
	}
	return models.NewTransactionSet(txns), now
}

func TestDI5ProjectionRetention(t *testing.T) {
	ts, now := di5ProjectionData()
	for name, rows := range map[string][]models.RecurringPayment{"dashboard": detectRecurringForDashboard(ts, now)} {
		kinds := map[string]int{}
		total := 0.0
		for _, p := range rows {
			kinds[p.Classification]++
			total += p.AnnualCost
		}
		if len(rows) != 22 || total != 25320 || kinds["subscription"] != 1 || kinds["bill"] != 1 || kinds["other"] != 20 {
			t.Fatalf("%s rows=%d annual=%v classes=%v", name, len(rows), total, kinds)
		}
		result, err := accounts.Project(models.Account{ID: "checker-cash", Anchors: []models.BalanceAnchor{{Date: now, Amount: 5000}}, LowBalanceThreshold: 500}, ts.Transactions, now, rows)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Available || result.Minimum != 2890 {
			t.Fatalf("%s projection minimum=%v available=%v want 2890", name, result.Minimum, result.Available)
		}
		t.Logf("%s: retained 22 classified series, annual=25320, projected minimum=2890 (5000 - 2110)", name)
	}
}
