package dashboard

import (
	"net/http"
	"time"

	"budget2/internal/models"
	insightssvc "budget2/internal/services/insights"
	"budget2/internal/services/metrics"
)

var dashboardNow = time.Now

func dashboardPeriod(ts *models.TransactionSet, r *http.Request, now time.Time) models.PeriodContext {
	active := ts.Active()
	start, end := resolveDateRangeAt(r.URL.Query().Get("start"), r.URL.Query().Get("end"), active.MinDate(), active.MaxDate(), now)
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = end
	}
	p := insightssvc.BuildPeriodContext(active, start, end, now)
	// Preserve the existing explicit "same period last year" control. Default
	// and "previous" requests use the shared adjacent-period policy unmodified.
	if r.URL.Query().Get("comparison") == "year" {
		p.PreviousStart = p.SelectedStart.AddDate(-1, 0, 0)
		p.PreviousEnd = p.SelectedEnd.AddDate(-1, 0, 0)
		p.PreviousDays = int(p.PreviousEnd.Sub(p.PreviousStart)/(24*time.Hour)) + 1
		p.ComparisonKind = "year"
		p.ComparisonClamped = false
		p.HistoryAvailable = insightssvc.HasCivilDateCoverage(active, p.PreviousStart, p.PreviousEnd)
		p.HistoryReason = "Not enough history to compare"
		if p.HistoryAvailable {
			p.HistoryReason = ""
		}
	}
	return p
}

// dashboardPeriodComparison adapts shared bounds to the existing KPI shape,
// retaining target/exclusion provenance and ChangeDisplay's rounded-pair rule.
func dashboardPeriodComparison(ts *models.TransactionSet, p models.PeriodContext, comparison string, settings *models.WhatIfSettings, exclusions map[string]models.MajorExpense) *models.PeriodComparison {
	if comparison != "previous" && comparison != "year" {
		return nil
	}
	if !p.HistoryAvailable {
		return &models.PeriodComparison{HasData: false}
	}
	active := ts.Active()
	coverage, hasCoverage := metrics.HealthcareCoverageStart(active)
	calculate := func(start, end time.Time) *models.DashboardMetrics {
		target, health := metrics.BudgetTargets(settings, start, end)
		return metrics.Calculate(insightssvc.TransactionsForPeriod(active, start, end), start, end, target, health, coverage, hasCoverage, exclusions)
	}
	current, prior := calculate(p.SelectedStart, p.SelectedEnd), calculate(p.PreviousStart, p.PreviousEnd)
	return &models.PeriodComparison{
		Current: current, Previous: prior, HasData: true,
		IncomeChange:          insightssvc.ChangeDisplay(prior.TotalIncome, current.TotalIncome).Percent,
		ExpensesChange:        insightssvc.ChangeDisplay(prior.TotalExpenses, current.TotalExpenses).Percent,
		SavingsChange:         insightssvc.ChangeDisplay(prior.NetSavings, current.NetSavings).Percent,
		SavingsRateChange:     models.RoundToCents(current.SavingsRate - prior.SavingsRate),
		ActualMonthlyChange:   insightssvc.ChangeDisplay(prior.ActualMonthly, current.ActualMonthly).Amount,
		CumulativeDeltaChange: insightssvc.ChangeDisplay(prior.CumulativeDelta, current.CumulativeDelta).Amount,
	}
}
