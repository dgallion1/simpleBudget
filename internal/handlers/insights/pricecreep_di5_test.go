package insights

import (
	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	insightssvc "budget2/internal/services/insights"
	"budget2/internal/services/pricecreep"
	"fmt"
	"testing"
	"time"
)

func TestDI5CreepTieMedianAndHistory(t *testing.T) {
	var tx []models.Transaction
	for i, amount := range []float64{-10, -10, -10, -20, -20, -20} {
		tx = append(tx, models.Transaction{Hash: fmt.Sprintf("h%d", i), Description: "Aster Coffee", Category: "Dining", Date: time.Date(2026, time.Month(i+1), 15, 12, 0, 0, 0, time.UTC), Amount: amount, TransactionType: models.Outflow})
	}
	tx = append(tx, models.Transaction{Hash: "z-latest", Description: "Aster Coffee Store", Category: "Dining", Date: tx[5].Date, Amount: -27, TransactionType: models.Outflow})
	tx = append(tx, models.Transaction{Hash: "suppressed", Description: "Aster Coffee Store", Date: di4Date("2026-07-15"), Amount: -100, TransactionType: models.Outflow, Suppressed: true}, models.Transaction{Hash: "transfer", Description: "Aster Coffee Store", Date: di4Date("2026-07-16"), Amount: -100, TransactionType: models.Transfer})
	ts := models.NewTransactionSet(tx)
	creeps := pricecreep.Detect(*ts)
	if len(creeps) != 1 || creeps[0].CurrentAmount != 20 {
		t.Fatalf("fixture creep=%+v", creeps)
	}
	p := insightssvc.BuildPeriodContext(ts, di4Date("2026-06-01"), di4Date("2026-06-15"), di4Date("2026-09-06"))
	flags := []anomalies.Anomaly{{Hash: "z-latest"}, {Hash: "z-latest"}, {Hash: "h1"}, {Hash: "suppressed"}}
	out := buildFindings(ts, p, flags, append(creeps, creeps[0]))
	if len(out) != 2 {
		t.Fatalf("hash/type dedup got %d want 2", len(out))
	}
	for _, f := range out {
		if f.Transaction.Hash != "z-latest" || f.Transaction.Amount != -27 {
			t.Fatalf("wrong real anchor %+v", f)
		}
		if f.Type == "price-creep" && f.Creep.CurrentAmount != 20 {
			t.Fatal("median replaced by anchor amount")
		}
	}
	p.SelectedStart = di4Date("2026-05-01")
	p.SelectedEnd = di4Date("2026-05-31")
	if out := buildFindings(ts, p, nil, creeps); len(out) != 0 {
		t.Fatal("latest full-history anchor moved backward to fit selection")
	}
}
