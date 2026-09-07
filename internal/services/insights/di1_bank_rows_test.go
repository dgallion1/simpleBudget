package insights

import (
	"budget2/internal/models"
	"testing"
	"time"
)

func TestDI1AdversarialExactBankRows(t *testing.T) {
	for _, c := range []struct{ display, original, category, want string }{
		{"Cloud Storage", "CLOUDBOX*STORAGE PLAN CLOUDBOX.COM CA", "Electronics & Software", "subscription"},
		{"AutoLoan Finance", "AUTOLOAN FINANCE BILL PMT", "Auto Payment", "bill"},
	} {
		t.Run(c.display, func(t *testing.T) {
			now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			var tx []models.Transaction
			for i := 0; i < 3; i++ {
				tx = append(tx, models.Transaction{Description: c.display, OriginalDescription: c.original, Category: c.category, Amount: -25, Date: now.AddDate(0, -i, 0), TransactionType: models.Outflow})
			}
			result := DetectRecurringAt(models.NewTransactionSet(tx), now)
			if len(result) != 1 {
				t.Fatalf("retained %d series want 1", len(result))
			}
			p := result[0]
			t.Logf("original=%q category=%q merchant=%q annual=%.2f classification=%s reason=%q", c.original, c.category, p.Description, p.AnnualCost, p.Classification, p.ClassificationReason)
			if p.Classification != c.want {
				t.Errorf("classification=%s want %s", p.Classification, c.want)
			}
			// Counterfactual verifies the exact mismatch is evidence recognition,
			// not detection, recurrence, amount, or category annotation.
			control := models.RecurringPayment{Description: c.display}
			if c.want == "bill" {
				control.Description = "Auto Loan"
			}
			got, _ := ClassifyRecurring(control)
			if got != c.want {
				t.Fatalf("control classification=%s want %s", got, c.want)
			}
		})
	}
}
