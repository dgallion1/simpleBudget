package engine

import "testing"

func TestLifetime2026ContributionPolicyBoundaries(t *testing.T) {
	p, err := ContributionPolicyForYear(2026)
	if err != nil {
		t.Fatal(err)
	}
	if p.RegularDeferralLimit != 24500 || p.AnnualAdditionsLimit != 72000 || p.CompensationLimit != 360000 || p.OrdinaryCatchUpLimit != 8000 || p.Age60To63CatchUpLimit != 11250 || p.RothCatchUpWageThreshold != 150000 || p.FrozenFromYear != 0 {
		t.Fatalf("policy = %+v", p)
	}
	for _, tc := range []struct {
		age  int
		want float64
	}{{49, 0}, {50, 8000}, {59, 8000}, {60, 11250}, {63, 11250}, {64, 8000}} {
		if got := p.CatchUpLimit(tc.age); got != tc.want {
			t.Errorf("age %d catch-up = %v, want %v", tc.age, got, tc.want)
		}
	}
	if p.RequiresRothCatchUp(150000) {
		t.Error("threshold must be strictly greater than $150,000")
	}
	if !p.RequiresRothCatchUp(150000.01) {
		t.Error("wages above threshold must require Roth catch-up")
	}
}

func TestLifetimeFuturePolicyFreezesLatestPublishedLimitsVisibly(t *testing.T) {
	p, err := ContributionPolicyForYear(2028)
	if err != nil {
		t.Fatal(err)
	}
	if p.PolicyYear != 2028 || p.FrozenFromYear != 2026 || p.RegularDeferralLimit != 24500 {
		t.Fatalf("future policy = %+v", p)
	}
	if p.Assumption == "" {
		t.Fatal("future frozen-dollar assumption must be visible")
	}
}

func TestLifetimePolicyRejectsUnsupportedPastYear(t *testing.T) {
	if _, err := ContributionPolicyForYear(2025); err == nil {
		t.Fatal("expected unsupported policy-year error")
	}
}
