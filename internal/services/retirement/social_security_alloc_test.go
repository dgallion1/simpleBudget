package retirement

import (
	"testing"

	"budget2/internal/models"
)

// The engine calls the projected-SS income hook every simulated month.
// Rebuilding the entry slice on each call was 65% of all bytes allocated
// by the guardrail and spending optimizers (1.65 GB per spending run), so
// the hook now assembles the entries into a stack buffer and must not
// allocate at all.
func TestProjectedSocialSecurityIncome_AllocationFree(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2000, FRA: 67, COLARate: 0.02, ClaimAge: 67}
	if !socialSecurityProjectionActive(s) {
		t.Fatal("fixture does not activate the SS projection")
	}
	if got := projectedSocialSecurityIncome(s, 120); got <= 0 {
		t.Fatalf("projectedSocialSecurityIncome(month 120) = %v; fixture should be paying by then", got)
	}
	if allocs := testing.AllocsPerRun(200, func() { projectedSocialSecurityIncome(s, 120) }); allocs != 0 {
		t.Fatalf("projectedSocialSecurityIncome allocates %.1f/op; want 0", allocs)
	}
}
