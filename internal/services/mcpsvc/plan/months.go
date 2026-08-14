package plan

import (
	"fmt"

	"budget2/internal/models"
)

// MaxMonthSpan bounds a single get_months call. A 30-year projection holds 360
// month records; returning them all would swamp the conversation this server
// exists to support.
const MaxMonthSpan = 120

// MonthRow is one month of projection detail.
type MonthRow struct {
	Month                     int     `json:"month"`
	PortfolioBalance          float64 `json:"portfolio_balance"`
	TaxDeferredBalance        float64 `json:"tax_deferred_balance"`
	TaxableBalance            float64 `json:"taxable_balance"`
	RothBalance               float64 `json:"roth_balance"`
	TotalExpenses             float64 `json:"total_expenses"`
	TotalIncome               float64 `json:"total_income"`
	TaxesPaid                 float64 `json:"taxes_paid"`
	StateTaxPaid              float64 `json:"state_tax_paid"`
	RMDWithdrawal             float64 `json:"rmd_withdrawal"`
	NetWithdrawal             float64 `json:"net_withdrawal"`
	WithdrawalFromTaxDeferred float64 `json:"withdrawal_tax_deferred"`
	WithdrawalFromTaxable     float64 `json:"withdrawal_taxable"`
	WithdrawalFromRoth        float64 `json:"withdrawal_roth"`
	RothConversions           float64 `json:"roth_conversions"`
	Depleted                  bool    `json:"depleted"`
}

// MonthWindow returns the inclusive [from, to] month range, rejecting spans
// wider than MaxMonthSpan and windows outside the projection.
func MonthWindow(p *models.ProjectionResult, from, to int) ([]MonthRow, error) {
	if p == nil || len(p.Months) == 0 {
		return nil, fmt.Errorf("projection has no months")
	}
	last := len(p.Months) - 1
	if from > to {
		return nil, fmt.Errorf("from_month (%d) must not exceed to_month (%d); valid range is %d..%d", from, to, 0, last)
	}
	if from < 0 || to > last {
		return nil, fmt.Errorf("requested months %d..%d are outside the projection; valid range is %d..%d", from, to, 0, last)
	}
	if span := to - from + 1; span > MaxMonthSpan {
		return nil, fmt.Errorf("requested %d months; at most %d may be returned per call — narrow the range", span, MaxMonthSpan)
	}

	rows := make([]MonthRow, 0, to-from+1)
	for _, m := range p.Months[from : to+1] {
		rows = append(rows, MonthRow{
			Month:                     m.Month,
			PortfolioBalance:          round0(m.PortfolioBalance),
			TaxDeferredBalance:        round0(m.TaxDeferredBalance),
			TaxableBalance:            round0(m.TaxableBalance),
			RothBalance:               round0(m.RothBalance),
			TotalExpenses:             round0(m.TotalExpenses),
			TotalIncome:               round0(m.TotalIncome),
			TaxesPaid:                 round0(m.TaxesPaid),
			StateTaxPaid:              round0(m.StateTaxPaid),
			RMDWithdrawal:             round0(m.RMDWithdrawal),
			NetWithdrawal:             round0(m.NetWithdrawal),
			WithdrawalFromTaxDeferred: round0(m.WithdrawalFromTaxDeferred),
			WithdrawalFromTaxable:     round0(m.WithdrawalFromTaxable),
			WithdrawalFromRoth:        round0(m.WithdrawalFromRoth),
			RothConversions:           round0(m.RothConversions),
			Depleted:                  m.Depleted,
		})
	}
	return rows, nil
}
