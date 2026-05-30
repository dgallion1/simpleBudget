package analysis

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// PresentValue computes the present value of expenses, income, and the
// resulting gap / coverage ratio over the projection horizon. Pure
// closed-form annuity math — runs no projection.
func PresentValue(in engine.Input) *models.PresentValueAnalysis {
	s := in.Prepared.Settings()
	months := s.ProjectionYears * 12
	discountRate := s.DiscountRate

	// Calculate PV of expenses
	pvExpenses := 0.0

	// Living expenses with inflation - spending decline
	netInflation := s.InflationRate - s.SpendingDeclineRate
	pvExpenses += engine.PresentValueAnnuity(s.MonthlyLivingExpenses, discountRate, netInflation, 0, months)

	// Healthcare expenses using multi-person model or legacy
	if len(s.HealthcarePersons) > 0 {
		// Multi-person model: calculate PV for each person
		for _, person := range s.HealthcarePersons {
			pvExpenses += engine.HealthcarePV(person, discountRate, months)
		}
	} else if s.MonthlyHealthcare > 0 {
		// Legacy single-value model
		healthcareStartMonth := s.HealthcareStartYears * 12
		healthcareMonths := months - healthcareStartMonth
		if healthcareMonths > 0 {
			pvExpenses += engine.PresentValueAnnuity(s.MonthlyHealthcare, discountRate, s.HealthcareInflation, healthcareStartMonth, healthcareMonths)
		}
	}

	// Add expense sources
	for _, source := range s.ExpenseSources {
		startMonth := source.StartYear * 12
		endMonth := months
		if source.EndYear > 0 {
			endMonth = min(source.EndYear*12, months)
		}
		duration := endMonth - startMonth
		if duration > 0 {
			growthRate := 0.0
			if source.Inflation {
				growthRate = s.InflationRate
			}
			pvExpenses += engine.PresentValueAnnuity(source.Amount, discountRate, growthRate, startMonth, duration)
		}
	}

	// Calculate PV of income sources. Mirror CalculateMonthlyIncomeBreakdown
	// so PV income matches the projection and BudgetFit: when the SS
	// optimizer is active it replaces manual SS-typed sources with its own
	// projected stream (added below), so skip those sources here.
	useOptimizerSS := in.Hooks.SSActive(s)
	pvIncome := 0.0
	for _, source := range s.IncomeSources {
		if useOptimizerSS && engine.IsSocialSecurityIncomeSource(source) {
			continue
		}
		endMonth := months
		if source.EndMonth != nil {
			endMonth = min(*source.EndMonth, months)
		}
		duration := endMonth - source.StartMonth
		if duration > 0 {
			pvIncome += engine.PresentValueAnnuity(source.Amount, discountRate, source.COLARate*100, source.StartMonth, duration)
		}
	}

	// The SS optimizer supplies Social Security via the projection hook
	// (manual SS sources were skipped above). Its per-month stream — claim-
	// age ramp, COLA, two spouses, spousal top-up — doesn't reduce to a
	// single closed-form annuity, so discount it month by month. Omitting
	// it (the prior behavior) understated PV income and overstated the gap
	// on every plan using the optimizer.
	if useOptimizerSS {
		pvIncome += presentValueOfMonthlyStream(func(month int) float64 {
			return in.Hooks.SSIncome(s, month)
		}, discountRate, months)
	}

	pvGap := pvExpenses - pvIncome
	coverageRatio := 0.0
	if pvExpenses > 0 {
		coverageRatio = (s.PortfolioValue + pvIncome) / pvExpenses
	}
	surplusDeficit := s.PortfolioValue + pvIncome - pvExpenses

	return &models.PresentValueAnalysis{
		PVExpenses:     pvExpenses,
		PVIncome:       pvIncome,
		PVGap:          pvGap,
		CoverageRatio:  coverageRatio,
		SurplusDeficit: surplusDeficit,
	}
}

// presentValueOfMonthlyStream discounts an arbitrary per-month cash flow
// back to month 0, with month m discounted by (1+monthlyRate)^m. Used for
// the SS-optimizer stream, whose amounts vary by month and so don't fit a
// single closed-form annuity. The monthly rate is derived the same way as
// PresentValueAnnuity, so the two stay consistent.
func presentValueOfMonthlyStream(amountAt func(month int) float64, discountRate float64, months int) float64 {
	monthlyRate := engine.MonthlyCompoundFactorFromDecimal(discountRate/100) - 1
	pv := 0.0
	for m := range months {
		amt := amountAt(m)
		if amt == 0 {
			continue
		}
		if monthlyRate > 0 {
			pv += amt / math.Pow(1+monthlyRate, float64(m))
		} else {
			pv += amt
		}
	}
	return pv
}
