package dashboard

import "budget2/internal/models"

// BudgetHealth classifies dashboard budget performance for the verdict band tint.
type BudgetHealth string

const (
	BudgetGreen   BudgetHealth = "green"
	BudgetAmber   BudgetHealth = "amber"
	BudgetRed     BudgetHealth = "red"
	BudgetNeutral BudgetHealth = "neutral"
)

// overAmberPct: over budget by up to this fraction of the target total is amber;
// beyond it is red.
const overAmberPct = 0.10

// onBudgetEps is the dollar slack within which spending counts as "on budget"
// rather than over/under (matches the Budget KPI card's threshold).
const onBudgetEps = 1.0

// BudgetVerdictView is the precomputed model the dashboard verdict band renders.
// Classification lives here (testable); currency formatting stays in the template
// (reusing formatMoney), so this carries figures and flags, not display strings.
type BudgetVerdictView struct {
	Health      BudgetHealth
	HasTarget   bool
	Delta       float64 // CombinedCumulativeDelta (>0 = over budget)
	IsOver      bool    // Delta > onBudgetEps
	IsUnder     bool    // Delta < -onBudgetEps
	SpentTotal  float64 // living + healthcare actual for the period
	TargetTotal float64 // living + healthcare target for the period
	Months      float64
	NetSavings  float64
	SavingsRate float64
	TotalIncome float64
}

// BuildBudgetVerdict derives the dashboard verdict band model from metrics
// already computed for the selected date range. It does no money math beyond
// summing totals the metrics expose.
func BuildBudgetVerdict(m *models.DashboardMetrics) BudgetVerdictView {
	v := BudgetVerdictView{Health: BudgetNeutral}
	if m == nil {
		return v
	}

	v.NetSavings = m.NetSavings
	v.SavingsRate = m.SavingsRate
	v.TotalIncome = m.TotalIncome
	v.Months = m.MonthsInRange

	if !m.HasCombinedTarget {
		return v
	}

	v.HasTarget = true
	v.Delta = m.CombinedCumulativeDelta
	v.SpentTotal = m.LivingExpensesTotal + m.HealthcareTotal
	v.TargetTotal = m.LivingTargetTotal + m.HealthcareTargetTotal

	switch {
	case v.Delta > onBudgetEps:
		v.IsOver = true
		if v.TargetTotal > 0 && v.Delta/v.TargetTotal > overAmberPct {
			v.Health = BudgetRed
		} else {
			v.Health = BudgetAmber
		}
	default:
		// On budget or under budget — both healthy.
		if v.Delta < -onBudgetEps {
			v.IsUnder = true
		}
		v.Health = BudgetGreen
	}
	return v
}
