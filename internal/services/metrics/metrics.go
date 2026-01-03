package metrics

import (
	"math"
	"sort"
	"time"

	"budget2/internal/models"
)

// Service provides metric calculation functionality
type Service struct{}

// New creates a new metrics service
func New() *Service {
	return &Service{}
}

// CalculateMetrics computes dashboard metrics from a transaction set
func (s *Service) CalculateMetrics(ts *models.TransactionSet) *models.DashboardMetrics {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	totalIncome := income.SumAmount()
	totalExpenses := outflows.SumAbsAmount()
	netSavings := totalIncome - totalExpenses

	var savingsRate float64
	if totalIncome > 0 {
		savingsRate = (netSavings / totalIncome) * 100
	}

	// Calculate monthly trends
	var incomeTrend, expensesTrend, savingsTrend []float64
	var trendLabels []string

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

	// Get sorted months
	monthSet := make(map[string]bool)
	for m := range monthlyIncome {
		monthSet[m] = true
	}
	for m := range monthlyOutflows {
		monthSet[m] = true
	}

	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	// Take last 6 months
	if len(months) > 6 {
		months = months[len(months)-6:]
	}

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = exp.SumAbsAmount()
		}

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		trendLabels = append(trendLabels, m)
	}

	return &models.DashboardMetrics{
		TotalIncome:      totalIncome,
		TotalExpenses:    totalExpenses,
		NetSavings:       netSavings,
		SavingsRate:      savingsRate,
		TransactionCount: ts.Len(),
		StartDate:        ts.MinDate(),
		EndDate:          ts.MaxDate(),
		IncomeTrend:      incomeTrend,
		ExpensesTrend:    expensesTrend,
		SavingsTrend:     savingsTrend,
		TrendLabels:      trendLabels,
	}
}

// CalculateComparison computes period-over-period comparison metrics
func (s *Service) CalculateComparison(data *models.TransactionSet, start, end time.Time, compType string) *models.PeriodComparison {
	duration := end.Sub(start)

	var compStart, compEnd time.Time

	switch compType {
	case "previous":
		compEnd = start.Add(-24 * time.Hour) // Day before start
		compStart = compEnd.Add(-duration)
	case "year":
		compStart = start.AddDate(-1, 0, 0)
		compEnd = end.AddDate(-1, 0, 0)
	default:
		return nil
	}

	currentFiltered := data.FilterByDateRange(start, end)
	compFiltered := data.FilterByDateRange(compStart, compEnd)

	if compFiltered.Len() == 0 {
		return &models.PeriodComparison{HasData: false}
	}

	currentMetrics := s.CalculateMetrics(currentFiltered)
	compMetrics := s.CalculateMetrics(compFiltered)

	incomeChange := s.PercentChange(currentMetrics.TotalIncome, compMetrics.TotalIncome)
	expensesChange := s.PercentChange(currentMetrics.TotalExpenses, compMetrics.TotalExpenses)
	savingsChange := s.PercentChange(currentMetrics.NetSavings, compMetrics.NetSavings)
	savingsRateChange := currentMetrics.SavingsRate - compMetrics.SavingsRate

	return &models.PeriodComparison{
		Current:           currentMetrics,
		Previous:          compMetrics,
		HasData:           true,
		IncomeChange:      incomeChange,
		ExpensesChange:    expensesChange,
		SavingsChange:     savingsChange,
		SavingsRateChange: savingsRateChange,
	}
}

// PercentChange calculates the percentage change between two values
func (s *Service) PercentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return ((current - previous) / math.Abs(previous)) * 100
}
