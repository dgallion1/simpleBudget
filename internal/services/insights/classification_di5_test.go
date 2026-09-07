package insights

import (
	"budget2/internal/models"
	"testing"
)

func TestDI5EvidenceBoundaries(t *testing.T) {
	for _, c := range []struct{ original, category, want string }{
		{"POS CLOUDBOX*STORAGE PLAN CLOUDBOX.COM CA", "Electronics & Software", "subscription"},
		{"cloudbox/storage-plan", "", "subscription"},
		{"NOTCLOUDBOX STORAGE PLAN", "", "other"},
		{"CLOUDBOX STORAGE PLANT", "", "other"},
		{"CLOUDBOX STORAGE", "", "other"},
		{"TARGET CLOUDBOX STORAGE PLAN", "", "other"},
		{"CLOUDBOX STORAGE PLAN", "Shopping", "other"},
		{"CLOUDBOX STORAGE PLAN", "Utilities", "other"},
		{"POS AUTOLOAN FINANCE BILL PMT", "Auto Payment", "bill"},
		{"NOTAUTOLOAN FINANCE BILL PMT", "Auto Payment", "other"},
		{"AUTOLOAN FINANCE BILL PMTS", "Auto Payment", "other"},
		{"AUTOLOAN FINANCE BILL PMT", "Subscriptions", "other"},
		{"TARGET AUTOLOAN FINANCE BILL PMT", "Shopping", "other"},
		{"NETFLIX.COM", "Entertainment", "subscription"},
		{"NotNetflix", "Entertainment", "other"},
	} {
		t.Run(c.original+c.category, func(t *testing.T) {
			for _, alias := range []string{"Netflix", "Cloud Storage", "AutoLoan Finance", "Travel & Hobbies"} {
				p := models.RecurringPayment{Description: alias, MajorExpenseName: alias, Frequency: "monthly", Transactions: []models.Transaction{{Description: alias, OriginalDescription: c.original, Category: c.category}}}
				got, reason := ClassifyRecurring(p)
				if got != c.want || reason == "" || IsSubscription(p) != (got == "subscription") {
					t.Fatalf("original=%q category=%q alias=%q got=%s reason=%q want=%s", c.original, c.category, alias, got, reason, c.want)
				}
			}
		})
	}
}
