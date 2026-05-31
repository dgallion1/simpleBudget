package analysis

import (
	"math"
	"testing"

	"budget2/internal/services/retirement/engine"
)

// presentValueOfMonthlyStream and engine.PresentValueAnnuity must use the same
// payment-timing convention, so that a leg discounted month-by-month (phased
// living expenses, optimizer SS, taxes) and a leg discounted by the closed-form
// annuity (flat expenses, manual income) agree for identical cash flows.
//
// PresentValueAnnuity is an ordinary annuity: the first payment falls one month
// out, discounted by (1+r)^-1. A constant-payment stream must therefore equal
// the ordinary annuity of that same payment exactly. Before the fix the stream
// used annuity-due timing (first payment undiscounted at m=0), overstating
// every stream leg by a factor of (1+monthlyRate).
func TestPresentValueOfMonthlyStream_MatchesOrdinaryAnnuity(t *testing.T) {
	const (
		payment      = 1000.0
		discountRate = 5.0 // percent
		months       = 240
	)

	stream := presentValueOfMonthlyStream(func(int) float64 { return payment }, discountRate, months)
	annuity := engine.PresentValueAnnuity(payment, discountRate, 0, 0, months)

	if math.Abs(stream-annuity) > 0.01 {
		t.Errorf("constant-payment stream PV = %.4f, want ordinary-annuity PV %.4f (diff %.4f)",
			stream, annuity, stream-annuity)
	}
}

// A start-month offset on the stream must also match the ordinary annuity:
// a constant payment beginning at month k equals PresentValueAnnuity with
// startMonth=k. This guards the SS-optimizer / expense-source legs that begin
// partway through the horizon.
func TestPresentValueOfMonthlyStream_MatchesOrdinaryAnnuity_WithStartOffset(t *testing.T) {
	const (
		payment      = 2500.0
		discountRate = 4.0 // percent
		months       = 360
		startMonth   = 60
	)

	stream := presentValueOfMonthlyStream(func(m int) float64 {
		if m < startMonth {
			return 0
		}
		return payment
	}, discountRate, months)
	annuity := engine.PresentValueAnnuity(payment, discountRate, 0, startMonth, months-startMonth)

	if math.Abs(stream-annuity) > 0.01 {
		t.Errorf("offset stream PV = %.4f, want ordinary-annuity PV %.4f (diff %.4f)",
			stream, annuity, stream-annuity)
	}
}
