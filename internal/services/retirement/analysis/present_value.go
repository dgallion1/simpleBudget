package analysis

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// PresentValue computes the present value of expenses, income, and the
// resulting gap / coverage ratio over the projection horizon.
//
// Expenses and income are closed-form annuity math. Taxes are not: income
// taxes and IRMAA depend on withdrawal sequencing, SS taxation, and RMDs,
// which don't reduce to a closed form — so when proj is non-nil the
// projection's actual per-month taxes and per-year IRMAA are discounted
// into Total Needs (PVTaxes). This keeps the coverage ratio after-tax and
// consistent with the Budget Analysis panel. Pass nil for a pre-tax
// estimate (PVTaxes stays 0).
func PresentValue(in engine.Input, proj *models.ProjectionResult) *models.PresentValueAnalysis {
	s := in.Prepared.Settings()
	months := s.ProjectionYears * 12
	discountRate := s.DiscountRate

	// Calculate PV of expenses
	pvExpenses := 0.0

	// Living expenses. Spending phases apply an age-stepped multiplier that
	// a single closed-form annuity can't express, so when enabled discount
	// the engine's actual per-month living expense (matching the
	// projection). Otherwise use the closed-form net-inflation annuity.
	if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
		pvExpenses += presentValueOfMonthlyStream(func(month int) float64 {
			return engine.LivingExpensesAtMonth(s, month)
		}, discountRate, months)
	} else {
		netInflation := s.InflationRate - s.SpendingDeclineRate
		pvExpenses += engine.PresentValueAnnuity(s.MonthlyLivingExpenses, discountRate, netInflation, 0, months)
	}

	// Property tax grows at its own (typically higher) inflation rate,
	// mirroring engine.PropertyTaxAtMonth. The projection includes it in
	// expenses, so omitting it here understated Total Needs.
	if s.MonthlyPropertyTax > 0 {
		pvExpenses += engine.PresentValueAnnuity(s.MonthlyPropertyTax, discountRate, s.PropertyTaxInflation, 0, months)
	}

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

	// Discount the projection's actual income taxes (per month) and IRMAA
	// (per year, spread evenly) into present value. The closed-form
	// expenses above contain neither, so there's no double-count. nil proj
	// → pre-tax estimate.
	pvTaxes := presentValueOfTaxes(proj, discountRate, months)

	totalNeeds := pvExpenses + pvTaxes
	pvGap := totalNeeds - pvIncome
	coverageRatio := 0.0
	if totalNeeds > 0 {
		coverageRatio = (s.PortfolioValue + pvIncome) / totalNeeds
	}
	surplusDeficit := s.PortfolioValue + pvIncome - totalNeeds

	return &models.PresentValueAnalysis{
		PVExpenses:     pvExpenses,
		PVTaxes:        pvTaxes,
		PVIncome:       pvIncome,
		PVGap:          pvGap,
		CoverageRatio:  coverageRatio,
		SurplusDeficit: surplusDeficit,
	}
}

// presentValueOfTaxes discounts the projection's tax burden back to month
// 0: each month's income tax (federal + state + NIIT, already summed into
// ProjectionMonth.TaxesPaid) plus that year's IRMAA surcharge spread
// evenly across its 12 months. Returns 0 when no projection is supplied,
// preserving the pre-tax estimate.
func presentValueOfTaxes(proj *models.ProjectionResult, discountRate float64, months int) float64 {
	if proj == nil || len(proj.Months) == 0 {
		return 0
	}
	return presentValueOfMonthlyStream(func(month int) float64 {
		if month >= len(proj.Months) {
			return 0
		}
		tax := proj.Months[month].TaxesPaid
		if y := month / 12; y < len(proj.YearlySummaries) {
			tax += proj.YearlySummaries[y].IRMAA / 12
		}
		return tax
	}, discountRate, months)
}

// presentValueOfMonthlyStream discounts an arbitrary per-month cash flow
// back to month 0. Used for the SS-optimizer stream, taxes, and phase-
// based living expenses — amounts that vary by month and so don't fit a
// single closed-form annuity.
//
// Month m is discounted by (1+monthlyRate)^(m+1) — i.e. the payment for
// month m is treated as occurring at the end of that month. This matches
// PresentValueAnnuity's ordinary-annuity convention (its first payment
// lands one month out), so a constant stream over months 0..n-1 equals
// PresentValueAnnuity(amount, …, startMonth=0, n) exactly. Keeping both
// on the same convention prevents a ~one-month discount drift between the
// closed-form legs (no-phase living expenses, property tax, healthcare,
// expense sources, manual income) and the stream legs.
func presentValueOfMonthlyStream(amountAt func(month int) float64, discountRate float64, months int) float64 {
	monthlyRate := engine.MonthlyCompoundFactorFromDecimal(discountRate/100) - 1
	pv := 0.0
	for m := range months {
		amt := amountAt(m)
		if amt == 0 {
			continue
		}
		if monthlyRate > 0 {
			pv += amt / math.Pow(1+monthlyRate, float64(m+1))
		} else {
			pv += amt
		}
	}
	return pv
}
