package whatif

import (
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/models"
	budgettemplates "budget2/internal/templates"
)

// BF1 (2026-09-18): the post-apply banner renders its money figures through
// FormatMoney — the same formatter as the spending option cards — instead of
// a second `$%.2f` formatter that dropped thousands separators.
func TestSpendingAppliedAnnouncementUsesFormatMoney(t *testing.T) {
	settings := &models.WhatIfSettings{
		MonthlyLivingExpenses: 7639.342517162467,
		LivingSpendingBoost:   &models.LivingSpendingBoost{MonthlyReal: 2000, StopMonth: "2030-01"},
	}
	req := httptest.NewRequest("GET", "/whatif?spending_applied=3", nil)
	got := spendingAppliedAnnouncement(req, settings)
	want := "Saved spending plan: base living expenses $7,639.34 per month and the complete portfolio-trigger spending rules. Extra living spending of $2,000.00 per month stops in 2030-01."
	if got != want {
		t.Errorf("announcement = %q, want %q", got, want)
	}
	// One formatter: the banner's base string is FormatMoney's for the same
	// value the card face and frontier rows render.
	if base := budgettemplates.FormatMoney(settings.MonthlyLivingExpenses); !strings.Contains(got, "expenses "+base+" per month") {
		t.Errorf("banner base %q not FormatMoney %q", got, base)
	}
	if strings.Contains(got, "$7639") || strings.Contains(got, "$2000") {
		t.Errorf("banner still uses the unseparated formatter: %q", got)
	}

	// Large value keeps every separator.
	big := &models.WhatIfSettings{MonthlyLivingExpenses: 1234567.891}
	if got := spendingAppliedAnnouncement(req, big); !strings.Contains(got, "expenses $1,234,567.89 per month") {
		t.Errorf("large value = %q", got)
	}

	// No boost: the second sentence is absent.
	noBoost := &models.WhatIfSettings{MonthlyLivingExpenses: 8000}
	if got := spendingAppliedAnnouncement(req, noBoost); got != "Saved spending plan: base living expenses $8,000.00 per month and the complete portfolio-trigger spending rules." {
		t.Errorf("no-boost announcement = %q", got)
	}

	// Not a post-apply reload: empty.
	for _, q := range []string{"", "?spending_applied=0", "?spending_applied=abc", "?spending_applied=-1"} {
		if got := spendingAppliedAnnouncement(httptest.NewRequest("GET", "/whatif"+q, nil), settings); got != "" {
			t.Errorf("query %q: announcement = %q, want empty", q, got)
		}
	}
}
