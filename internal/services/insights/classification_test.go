package insights

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
)

func TestDI1PositiveEvidence(t *testing.T) {
	for _, tc := range []struct {
		description, category string
		want                  bool
	}{
		{"Netflix", "Entertainment", true}, {"Spotify", "Music", true}, {"Cloud Storage", "Software", true},
		{"Auto Loan", "Auto", false}, {"Travel", "Travel", false}, {"Fuel", "Auto", false},
		{"Pets", "Pets", false}, {"ATM Withdrawal", "Cash", false}, {"Dining", "Food", false},
		{"Home Maintenance", "Home", false}, {"Unknown Merchant", "", false}, {"Target", "Shopping", false},
		{"Independent Service", "Subscriptions", true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			for _, label := range []string{"Travel & Hobbies", "Netflix", "Utilities"} {
				rp := models.RecurringPayment{Description: tc.description, Frequency: "monthly", MajorExpenseName: label,
					Transactions: []models.Transaction{{Description: tc.description, Category: tc.category}}}
				if got := IsSubscription(rp); got != tc.want {
					t.Errorf("%s annotated %s: subscription=%v want %v", tc.description, label, got, tc.want)
				}
			}
		})
	}
}

func TestDI1OriginalEvidenceAndAnnotation(t *testing.T) {
	for _, tc := range []struct{ original, category, want string }{
		{"Spotify", "Entertainment", "subscription"},
		{"Auto Loan", "Auto", "bill"},
		{"Unrecognized Vendor", "Subscriptions", "subscription"},
		{"Unrecognized Vendor", "Internet Access", "bill"},
		{"Target", "Subscriptions", "other"},
		{"NotNetflix", "", "other"},
		{"Netflix", "Utilities", "other"},
	} {
		t.Run(tc.original+tc.category, func(t *testing.T) {
			rp := models.RecurringPayment{Description: "Renamed display", Frequency: "monthly", Transactions: []models.Transaction{{Hash: "synthetic", OriginalDescription: tc.original, Description: "Edited display", Category: tc.category}}}
			for _, label := range []string{"Netflix", "Travel & Hobbies", "Utilities"} {
				annotated := majorexpenses.AnnotateRecurringPayments([]models.RecurringPayment{rp}, []models.MajorExpense{{ID: "household", Name: label}}, map[string]string{"synthetic": "household"})[0]
				kind, reason := ClassifyRecurring(annotated)
				if kind != tc.want || reason == "" {
					t.Errorf("%s: kind=%s reason=%q want %s", label, kind, reason, tc.want)
				}
				if annotated.Description != rp.Description || annotated.MajorExpenseName != label {
					t.Errorf("annotation changed merchant or failed: %+v", annotated)
				}
				if IsSubscription(annotated) != (tc.want == "subscription") {
					t.Error("compatibility wrapper differs")
				}
			}
		})
	}
}

func TestDI1BankEvidenceGuards(t *testing.T) {
	for _, display := range []string{"Netflix", "Cloud Storage", "AutoLoan Finance"} {
		payment := models.RecurringPayment{Description: display, Transactions: []models.Transaction{{
			Description: display, OriginalDescription: "UNRECOGNIZED MERCHANT", Category: "Electronics & Software",
		}}}
		if kind, _ := ClassifyRecurring(payment); kind != models.RecurringOther {
			t.Errorf("edited display %q overrode original merchant: %s", display, kind)
		}
	}
	cloud := models.Transaction{Description: "Cloud Storage", OriginalDescription: "CLOUDBOX*STORAGE PLAN CLOUDBOX.COM CA", Category: "Electronics & Software"}
	for _, conflicting := range []models.Transaction{
		{Description: "Target", OriginalDescription: "TARGET", Category: "Shopping"},
		{Description: "AutoLoan Finance", OriginalDescription: "AUTOLOAN FINANCE BILL PMT", Category: "Auto Payment"},
	} {
		for _, transactions := range [][]models.Transaction{{cloud, conflicting}, {conflicting, cloud}} {
			payment := models.RecurringPayment{Transactions: transactions}
			if kind, _ := ClassifyRecurring(payment); kind != models.RecurringOther {
				t.Errorf("conflicting original evidence classified %s", kind)
			}
		}
	}
}
