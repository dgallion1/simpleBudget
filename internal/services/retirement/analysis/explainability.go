package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BuildExplainability summarizes a projection into per-year totals and
// portfolio metrics consumed by the explainability panel. When
// projection.YearlySummaries is empty (older code paths or partial
// runs), the function rebuilds the summaries from monthly rows using
// the prepared settings' starting portfolio value.
func BuildExplainability(proj *models.ProjectionResult, in engine.Input) *models.ProjectionExplainability {
	if proj == nil || len(proj.Months) == 0 {
		return nil
	}

	s := in.Prepared.Settings()
	summaries := proj.YearlySummaries
	if len(summaries) == 0 {
		summaries = make([]models.ProjectionYearSummary, 0, len(proj.Months)/12+1)
		startingBalance := s.PortfolioValue
		currentYear := proj.Months[0].Month / 12
		summary := models.ProjectionYearSummary{
			Year:            currentYear,
			StartingBalance: startingBalance,
		}

		finalizeYear := func(month models.ProjectionMonth) {
			summary.EndingBalance = month.PortfolioBalance
			summary.EndingBalanceReal = month.PortfolioBalanceReal
			summary.CumulativeInflation = month.CumulativeInflation
			summaries = append(summaries, summary)
		}

		for idx, month := range proj.Months {
			year := month.Month / 12
			if year != currentYear {
				prev := proj.Months[idx-1]
				finalizeYear(prev)
				startingBalance = prev.PortfolioBalance
				currentYear = year
				summary = models.ProjectionYearSummary{
					Year:            currentYear,
					StartingBalance: startingBalance,
				}
			}

			summary.Growth += month.PortfolioGrowth
			summary.GrossIncome += month.GrossIncome
			summary.Taxes += month.TaxesPaid
			summary.Expenses += month.TotalExpenses
			summary.Withdrawals += month.NetWithdrawal
		}

		finalizeYear(proj.Months[len(proj.Months)-1])
	}

	totalTaxes := 0.0
	totalGrossIncome := 0.0
	for _, summary := range summaries {
		totalTaxes += summary.Taxes
		totalGrossIncome += summary.GrossIncome
	}

	lastMonth := proj.Months[len(proj.Months)-1]
	taxShare := 0.0
	if totalGrossIncome > 0 {
		taxShare = totalTaxes / totalGrossIncome * 100
	}
	inflationLossPercent := 0.0
	if lastMonth.PortfolioBalance > 0 {
		inflationLossPercent = (1 - (lastMonth.PortfolioBalanceReal / lastMonth.PortfolioBalance)) * 100
	}

	return &models.ProjectionExplainability{
		YearlySummaries:         summaries,
		TotalTaxes:              totalTaxes,
		TotalGrossIncome:        totalGrossIncome,
		TaxShareOfGrossCashFlow: taxShare,
		FinalBalanceReal:        lastMonth.PortfolioBalanceReal,
		CumulativeInflation:     lastMonth.CumulativeInflation,
		InflationLossPercent:    inflationLossPercent,
	}
}
