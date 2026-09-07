package insights

import (
	"budget2/internal/models"
	"testing"
	"time"
)

func TestDI5CadenceAndMerchantRetention(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var tx []models.Transaction
	for _, p := range []struct {
		name   string
		months int
		amount float64
	}{
		{"Netflix", 1, 0.99}, {"Spotify", 3, 12.34}, {"Cloud Storage", 12, 25.50},
	} {
		for i := 0; i < 3; i++ {
			tx = append(tx, models.Transaction{Description: p.name, OriginalDescription: p.name, Amount: -p.amount, Date: now.AddDate(0, -i*p.months, 0), TransactionType: models.Outflow})
		}
	}
	result := DetectRecurringAt(models.NewTransactionSet(tx), now)
	if len(result) != 3 {
		t.Fatalf("got %d merchants: %+v", len(result), result)
	}
	wants := map[string]float64{"netflix": 11.88, "spotify": 49.36, "cloud storage": 25.50}
	for _, p := range result {
		want, ok := wants[p.Description]
		delta := p.AnnualCost - want
		if !ok || delta > 1e-9 || delta < -1e-9 || p.Classification != "subscription" {
			t.Errorf("merchant=%s frequency=%s annual=%.12f class=%s want annual=%.12f", p.Description, p.Frequency, p.AnnualCost, p.Classification, want)
		}
		delete(wants, p.Description)
	}
	if len(wants) > 0 {
		t.Fatalf("missing merchants %v", wants)
	}
}
