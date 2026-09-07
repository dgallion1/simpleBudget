package metrics

import "budget2/internal/models"

// ReportingMoney uses the renderer's exact precision, normalizing signed zero.
// This is a presentation adapter; transaction amounts and raw metrics stay intact.
func ReportingMoney(v float64) float64 {
	v = models.RoundToCents(v)
	if v == 0 {
		return 0
	}
	return v
}

// CashFlowDisplay preserves rounded aggregate anchors, then derives their
// displayed difference. It makes no inference about investment withdrawals.
type CashFlowDisplay struct {
	Income, Spending, Balance float64
	Explanation               string
}

func ReportingCashFlow(income, spending float64) CashFlowDisplay {
	c := CashFlowDisplay{Income: ReportingMoney(income), Spending: ReportingMoney(spending)}
	c.Balance = ReportingMoney(c.Income - c.Spending)
	switch {
	case c.Balance < 0:
		c.Explanation = "Spending not covered by recorded income"
	case c.Balance > 0:
		c.Explanation = "Recorded income above spending"
	default:
		c.Explanation = "Recorded income matches spending"
	}
	return c
}
