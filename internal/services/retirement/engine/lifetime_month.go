package engine

import (
	"fmt"
	"math"
	"time"

	"budget2/internal/models"
)

type lifetimeTrialResult struct {
	state         *LifetimeAccountState
	outcome       models.LifetimeMonthOutcome
	portfolio     TaxAwarePortfolioMonthResult
	snapshot      ProjectedTaxSnapshot
	income        MonthlyIncomeBreakdown
	expenses      float64
	settlementTax float64
}

func lifetimeOwnerAge(s *models.WhatIfSettings, owner string, year int) (int, error) {
	p := s.FindPerson(owner)
	if p == nil {
		return 0, fmt.Errorf("lifetime account owner %q not found", owner)
	}
	b, err := time.Parse("2006-01", p.BirthMonth)
	if err != nil {
		return 0, fmt.Errorf("owner %q birth month: %w", owner, err)
	}
	return year - b.Year(), nil
}
func lifetimeJobActiveForEmployer(s *models.WhatIfSettings, owner, employer, ym string) bool {
	for _, j := range s.Lifetime.Jobs {
		if j.OwnerID != owner || j.EmployerID != employer {
			continue
		}
		end, e := j.EffectiveEndMonth(s.Persons)
		if e == nil && activeAt(j.StartMonth, end, ym) {
			return true
		}
	}
	return false
}
func lifetimeRMD(st *LifetimeAccountState, s *models.WhatIfSettings, projectionMonth, calendarYear, monthInYear int, out *models.LifetimeMonthOutcome) (float64, error) {
	trigger := RMDTriggerMonth(s.RMDTiming)
	shouldDistribute := monthInYear == trigger || (projectionMonth == 0 && monthInYear > trigger)
	ym := fmt.Sprintf("%04d-%02d", calendarYear, monthInYear+1)
	total := 0.0
	for _, id := range sortedAccountIDs(st.Accounts) {
		a := st.Accounts[id]
		if a.Account.TaxTreatment != "traditional" {
			continue
		}
		age, err := lifetimeOwnerAge(s, a.Account.OwnerID, calendarYear)
		if err != nil {
			return 0, err
		}
		owner := s.FindPerson(a.Account.OwnerID)
		birth, err := time.Parse("2006-01", owner.BirthMonth)
		if err != nil {
			return 0, err
		}
		if age < effectiveRMDStartAgeForBirthYear(birth.Year()) {
			continue
		}
		if a.Account.CurrentEmployerRMDDeferral && !a.Account.OwnerMoreThanFivePercent && lifetimeJobActiveForEmployer(s, a.Account.OwnerID, a.Account.EmployerID, ym) {
			continue
		}
		accountForRMD := a.Account
		accountForRMD.RMDPaidYTD = a.RMDPaidYTD
		required, err := LifetimeRMDRequirement(accountForRMD, age)
		if err != nil {
			return 0, err
		}
		gross := 0.0
		if shouldDistribute {
			gross, _, _ = withdrawLifetime(&a, required)
			a.RMDPaidYTD += gross
			st.Accounts[id] = a
		}
		out.RMD = append(out.RMD, models.LifetimeRMDFact{AccountID: id, AccountName: a.Account.Name, OwnerID: a.Account.OwnerID, Year: calendarYear, Obligation: required, Distributed: gross, Credited: gross, Taxable: gross})
		if gross > 0 {
			cash := st.Accounts[s.Lifetime.CashPolicy.ReserveAccountID]
			depositLifetime(&cash, gross)
			st.Accounts[cash.Account.ID] = cash
			out.Movements = append(out.Movements, models.AccountMovement{Month: projectionMonth, SourceID: id, DestinationID: cash.Account.ID, OwnerID: a.Account.OwnerID, Reason: "required_distribution", Amount: gross, OrdinaryIncome: gross})
			total += gross
		}
	}
	return total, nil
}

type lifetimeTaxFacts struct {
	ordinaryWithdrawals, rmdCredit, gains, rothEarnings, earlyDistributionTax float64
}

func ownerAtLeast59Half(s *models.WhatIfSettings, owner string, current time.Time) (bool, error) {
	p := s.FindPerson(owner)
	if p == nil {
		return false, fmt.Errorf("lifetime account owner %q not found", owner)
	}
	birth, err := time.Parse("2006-01", p.BirthMonth)
	if err != nil {
		return false, fmt.Errorf("owner %q birth month: %w", owner, err)
	}
	return (current.Year()-birth.Year())*12+int(current.Month()-birth.Month()) >= 59*12+6, nil
}

func rothQualified(s *models.WhatIfSettings, a models.LifetimeAccount, current time.Time) (bool, error) {
	oldEnough, err := ownerAtLeast59Half(s, a.OwnerID, current)
	if err != nil {
		return false, err
	}
	return oldEnough && a.RothFirstFundedYear > 0 && current.Year() >= a.RothFirstFundedYear+5, nil
}

func mergeLifetimeTaxFacts(a, b lifetimeTaxFacts) lifetimeTaxFacts {
	return lifetimeTaxFacts{
		ordinaryWithdrawals:  a.ordinaryWithdrawals + b.ordinaryWithdrawals,
		rmdCredit:            a.rmdCredit + b.rmdCredit,
		gains:                a.gains + b.gains,
		rothEarnings:         a.rothEarnings + b.rothEarnings,
		earlyDistributionTax: a.earlyDistributionTax + b.earlyDistributionTax,
	}
}

func fundLifetime(st *LifetimeAccountState, s *models.WhatIfSettings, projectionMonth, calendarYear int, amount float64, reason string, out *models.LifetimeMonthOutcome) (float64, lifetimeTaxFacts, error) {
	remaining := amount
	facts := lifetimeTaxFacts{}
	current, err := projectionDate(s.StartDate, projectionMonth)
	if err != nil {
		return 0, facts, err
	}
	for _, id := range s.Lifetime.CashPolicy.WithdrawalOrder {
		if remaining <= 1e-9 {
			break
		}
		a, ok := st.Accounts[id]
		if !ok || a.Value <= 0 {
			continue
		}
		requested := remaining
		penaltyRate := 0.0
		if a.Account.TaxTreatment == "traditional" && a.Account.LegalType == "ira" {
			oldEnough, ageErr := ownerAtLeast59Half(s, a.Account.OwnerID, current)
			if ageErr != nil {
				return amount - remaining, facts, ageErr
			}
			if !oldEnough {
				penaltyRate = 0.10
				requested = remaining / (1 - penaltyRate)
			}
		}
		if a.Account.TaxTreatment == "roth" {
			qualified, qErr := rothQualified(s, a.Account, current)
			if qErr != nil {
				return amount - remaining, facts, qErr
			}
			if !qualified {
				if a.Account.LegalType != "ira" {
					return amount - remaining, facts, fmt.Errorf("unsupported nonqualified designated Roth withdrawal from account %q", id)
				}
				if requested > a.Basis+1e-9 {
					return amount - remaining, facts, fmt.Errorf("unsupported nonqualified Roth IRA earnings or conversion withdrawal from account %q", id)
				}
			}
		}
		gross, gain, earn := withdrawLifetime(&a, requested)
		penalty := gross * penaltyRate
		spendable := gross - penalty
		st.Accounts[id] = a
		remaining -= math.Min(remaining, spendable)
		if a.Account.TaxTreatment == "traditional" {
			facts.ordinaryWithdrawals += gross
			facts.earlyDistributionTax += penalty
			age, ageErr := lifetimeOwnerAge(s, a.Account.OwnerID, calendarYear)
			owner := s.FindPerson(a.Account.OwnerID)
			if ageErr == nil && owner != nil {
				birth, birthErr := time.Parse("2006-01", owner.BirthMonth)
				ym := projectionDateMust(s.StartDate, projectionMonth)
				deferred := a.Account.CurrentEmployerRMDDeferral && !a.Account.OwnerMoreThanFivePercent && lifetimeJobActiveForEmployer(s, a.Account.OwnerID, a.Account.EmployerID, ym)
				if birthErr == nil && age >= effectiveRMDStartAgeForBirthYear(birth.Year()) && !deferred && a.Account.PriorDecemberValue != nil {
					accountForRMD := a.Account
					accountForRMD.RMDPaidYTD = a.RMDPaidYTD
					remainingRMD, _ := LifetimeRMDRequirement(accountForRMD, age)
					credit := math.Min(gross, remainingRMD)
					annualObligation := remainingRMD + a.RMDPaidYTD
					a.RMDPaidYTD += credit
					facts.rmdCredit += credit
					out.RMD = append(out.RMD, models.LifetimeRMDFact{AccountID: id, AccountName: a.Account.Name, OwnerID: a.Account.OwnerID, Year: calendarYear, Obligation: annualObligation, Distributed: credit, Credited: credit, Taxable: credit})
					st.Accounts[id] = a
				}
			}
		}
		facts.gains += gain
		if earn > 0 {
			qualified, _ := rothQualified(s, a.Account, current)
			if !qualified {
				facts.rothEarnings += earn
			}
		}
		if gross > 0 {
			out.Movements = append(out.Movements, models.AccountMovement{Month: projectionMonth, SourceID: id, DestinationID: "household", OwnerID: a.Account.OwnerID, Reason: reason, Amount: spendable, OrdinaryIncome: map[bool]float64{true: gross, false: 0}[a.Account.TaxTreatment == "traditional"], CapitalGain: gain, BasisChange: -math.Max(0, gross-gain)})
			if penalty > 0 {
				out.Movements = append(out.Movements, models.AccountMovement{Month: projectionMonth, SourceID: id, DestinationID: "tax_authority", OwnerID: a.Account.OwnerID, Reason: "early_distribution_tax", Amount: penalty})
			}
		}
	}
	return amount - math.Max(0, remaining), facts, nil
}

func activeScheduledSaving(r models.ScheduledSaving, current string) bool {
	return activeAt(r.StartMonth, r.EndMonth, current)
}

func runLifetimeTrial(st *ProjectionState, opening *LifetimeAccountState, s *models.WhatIfSettings, month int, p MonthReturns, taxGuess, irmaaGuess float64) (lifetimeTrialResult, error) {
	trial := opening.Clone()
	out := models.LifetimeMonthOutcome{Contributions: map[string]models.ContributionAmounts{}}
	currentDate, _ := projectionDate(s.StartDate, month)
	calendarYear, calendarMonth := currentDate.Year(), int(currentDate.Month())
	out.Year, out.Month = calendarYear, calendarMonth
	contrib, err := CalculateContributions(s, month, opening.Contributions)
	if err != nil {
		return lifetimeTrialResult{}, err
	}
	trial.Contributions = contrib.Next
	out.Contributions = contrib.AmountsByRule
	for _, amounts := range contrib.AmountsByRule {
		out.EmployeeContributions += amounts.EmployeeTraditional + amounts.EmployeeRoth
		out.EmployerContributions += amounts.EmployerTraditional + amounts.EmployerRoth
	}
	for _, m := range contrib.Movements {
		a := trial.Accounts[m.DestinationID]
		if a.Account.TaxTreatment == "roth" && a.Value == 0 && a.Account.RothFirstFundedYear == 0 && m.Amount > 0 {
			a.Account.RothFirstFundedYear = calendarYear
		}
		depositLifetime(&a, m.Amount)
		trial.Accounts[m.DestinationID] = a
		out.Movements = append(out.Movements, m)
	}
	payroll, err := CalculatePayrollTaxes(s, calendarYear, contrib.PayrollWagesByOwner, opening.Payroll)
	if err != nil {
		return lifetimeTrialResult{}, err
	}
	trial.Payroll = payroll.Next

	income := CalculateMonthlyIncomeBreakdown(st.in.Hooks, s, month)
	for _, j := range s.Lifetime.Jobs {
		if j.IncomeSourceID == "" {
			continue
		}
		for _, src := range s.IncomeSources {
			if src.ID == j.IncomeSourceID {
				v := src.GetAdjustedAmount(month)
				income.TotalIncome -= v
				income.OrdinaryIncome -= v
			}
		}
	}
	income.TotalIncome += contrib.PayrollWages
	income.OrdinaryIncome += contrib.PayrollWages + contrib.OrdinaryIncomeAdjustment

	cashID := s.Lifetime.CashPolicy.ReserveAccountID
	cash := trial.Accounts[cashID]
	employee := 0.0
	for _, v := range contrib.AmountsByRule {
		employee += v.EmployeeTraditional + v.EmployeeRoth
	}
	out.ExternalIncome = math.Max(0, income.TotalIncome)
	spendable := math.Max(0, income.TotalIncome-employee)
	depositLifetime(&cash, spendable)
	trial.Accounts[cashID] = cash
	if spendable > 0 {
		out.Movements = append(out.Movements, models.AccountMovement{Month: month, SourceID: "external_income", DestinationID: cashID, Reason: "income", Amount: spendable})
	}

	growth, interest := 0.0, 0.0
	qualifiedDividends, nonQualifiedDividends, capitalGainDistributions := 0.0, 0.0, 0.0
	for _, id := range sortedAccountIDs(trial.Accounts) {
		a := trial.Accounts[id]
		monthlyReturn := lifetimeAccountReturn(a.Account, p)
		if a.Account.LegalType == "brokerage" {
			taxable := NewTaxableAccountState(s, a.Value)
			taxable.CostBasis, taxable.RealizedGainsYTD = a.Basis, a.RealizedGainsYTD
			annualPercent := (math.Pow(1+monthlyReturn, 12) - 1) * 100
			g := taxable.ApplyGrowth(BuildTaxableReturnComponents(annualPercent, s), 1)
			taxable.AddCash(g.QualifiedDividends + g.NonQualifiedDividends + g.CapitalGainsDistributions)
			a.Value, a.Basis, a.RealizedGainsYTD = taxable.MarketValue, taxable.CostBasis, taxable.RealizedGainsYTD
			growth += g.TotalGrowth + g.QualifiedDividends + g.NonQualifiedDividends + g.CapitalGainsDistributions
			qualifiedDividends += g.QualifiedDividends
			nonQualifiedDividends += g.NonQualifiedDividends
			capitalGainDistributions += g.CapitalGainsDistributions
		} else {
			g := a.Value * monthlyReturn
			a.Value += g
			growth += g
			if a.Account.LegalType == "cash" {
				interest += g
			}
		}
		trial.Accounts[id] = a
	}
	rmd, err := lifetimeRMD(trial, s, month, calendarYear, calendarMonth-1, &out)
	if err != nil {
		return lifetimeTrialResult{}, err
	}

	taxPaid, taxFacts, err := fundLifetime(trial, s, month, calendarYear, taxGuess+payroll.Total, "tax_payment", &out)
	if err != nil {
		return lifetimeTrialResult{}, err
	}
	incomeTaxPaid := math.Max(0, taxPaid-payroll.Total)
	expenses := st.CurrentLivingExpenses + s.GetTotalHealthcareCost(month)*p.HealthcareMultiplier + PropertyTaxAtMonth(s, month) + p.ExtraExpenses
	for _, src := range s.ExpenseSources {
		expenses += src.GetAdjustedAmount(month, s.InflationRate)
	}
	paidExpenses, expenseFacts, err := fundLifetime(trial, s, month, calendarYear, expenses, "expense", &out)
	if err != nil {
		return lifetimeTrialResult{}, err
	}
	paidIRMAA, irmaaFacts, err := fundLifetime(trial, s, month, calendarYear, irmaaGuess, "irmaa", &out)
	if err != nil {
		return lifetimeTrialResult{}, err
	}
	out.ConsumptionAssessed = expenses + irmaaGuess
	out.ConsumptionPaid = paidExpenses + paidIRMAA
	out.IRMAAAssessed = irmaaGuess
	out.IRMAAPaid = paidIRMAA
	out.UnfundedExpenses = math.Max(0, expenses-paidExpenses) + math.Max(0, irmaaGuess-paidIRMAA)
	facts := mergeLifetimeTaxFacts(mergeLifetimeTaxFacts(taxFacts, expenseFacts), irmaaFacts)

	snapshot := st.TaxState.EstimateMonthlySnapshot(MonthlyTaxInputs{Calculator: st.TaxCalculator, YearsFromBase: calendarYear - taxBaseYear, MonthInYear: calendarMonth - 1,
		OrdinaryIncome: income.OrdinaryIncome + interest + facts.rothEarnings, SocialSecurityIncome: income.SocialSecurityIncome, TaxableWithdrawals: facts.ordinaryWithdrawals - facts.rmdCredit,
		RMDWithdrawals: rmd + facts.rmdCredit, QualifiedDividends: qualifiedDividends, NonQualifiedDividends: nonQualifiedDividends, LongTermCapitalGains: facts.gains + capitalGainDistributions, CompletedMAGIHistory: st.CompletedMAGIHistory, AssumedIRMALookbackMAGI: &st.AssumedLookbackMAGI,
		IRMAAEligibleAdults: MedicareEligibleAdultCountAtMonth(s, month), IRMAAInflationFactor: PlannerIRMAAInflationFactorForYear(s.InflationRate, float64(calendarYear-taxBaseYear)),
		IRMAASurchargeInflationFactor: PlannerIRMAASurchargeInflationFactorForYear(float64(calendarYear - taxBaseYear))})
	out.EarlyDistributionTax = facts.earlyDistributionTax
	out.TaxLiability = snapshot.MonthlyTax + payroll.Total + facts.earlyDistributionTax
	out.TaxPayments = taxPaid + facts.earlyDistributionTax
	out.UnpaidTax = math.Max(0, snapshot.MonthlyTax+payroll.Total-taxPaid)

	current := projectionDateMust(s.StartDate, month)
	for _, saving := range s.Lifetime.ScheduledSavings {
		if !activeScheduledSaving(saving, current) {
			continue
		}
		reserve := trial.Accounts[cashID]
		amount := math.Min(saving.MonthlyAmount, reserve.Value)
		withdrawLifetime(&reserve, amount)
		trial.Accounts[cashID] = reserve
		dest := trial.Accounts[saving.DestinationAccountID]
		depositLifetime(&dest, amount)
		trial.Accounts[saving.DestinationAccountID] = dest
		out.UnfundedSaving += saving.MonthlyAmount - amount
		if amount > 0 {
			out.Movements = append(out.Movements, models.AccountMovement{Month: month, SourceID: cashID, DestinationID: saving.DestinationAccountID, OwnerID: saving.OwnerID, RuleID: saving.ID, Reason: "scheduled_saving", Amount: amount, BasisChange: amount})
		}
	}
	reserve := trial.Accounts[cashID]
	out.ReserveGap = math.Max(0, s.Lifetime.CashPolicy.ReserveTarget-reserve.Value)
	if reserve.Value > s.Lifetime.CashPolicy.ReserveTarget {
		amount := reserve.Value - s.Lifetime.CashPolicy.ReserveTarget
		withdrawLifetime(&reserve, amount)
		trial.Accounts[cashID] = reserve
		dest := trial.Accounts[s.Lifetime.CashPolicy.SurplusAccountID]
		depositLifetime(&dest, amount)
		trial.Accounts[dest.Account.ID] = dest
		out.Movements = append(out.Movements, models.AccountMovement{Month: month, SourceID: cashID, DestinationID: dest.Account.ID, Reason: "surplus", Amount: amount, BasisChange: amount})
		out.AllSourceReinvestment += amount
	}

	for _, id := range sortedAccountIDs(trial.Accounts) {
		a := trial.Accounts[id]
		open := opening.Accounts[id]
		deposits, withdrawals := 0.0, 0.0
		for _, movement := range out.Movements {
			if movement.DestinationID == id {
				deposits += movement.Amount
			}
			if movement.SourceID == id {
				withdrawals += movement.Amount
			}
		}
		ret := a.Value - open.Value - deposits + withdrawals
		out.Accounts = append(out.Accounts, models.AccountReconciliation{AccountID: id, AccountName: a.Account.Name, Opening: open.Value, Deposits: deposits, Withdrawals: withdrawals, Return: ret, Closing: a.Value})
	}
	pr := TaxAwarePortfolioMonthResult{TaxesPaid: incomeTaxPaid, IRMAAExpense: snapshot.MonthlyIRMAA, TotalGrowth: growth, TaxSnapshot: snapshot, TaxableQualifiedDividends: qualifiedDividends, TaxableNonQualifiedDividends: nonQualifiedDividends, TaxableCapitalGains: facts.gains + capitalGainDistributions, TaxableCapitalGainsDistributions: capitalGainDistributions, Shortfall: out.UnfundedExpenses, CashFlow: PortfolioCashFlowResult{Shortfall: out.UnfundedExpenses, WithdrawalFromTaxDeferred: facts.ordinaryWithdrawals + rmd, RMDWithdrawal: rmd + facts.rmdCredit, TaxableRealizedGain: facts.gains}}
	return lifetimeTrialResult{state: trial, outcome: out, portfolio: pr, snapshot: snapshot, income: income, expenses: expenses, settlementTax: snapshot.MonthlyTax}, nil
}

func projectionDateMust(start string, month int) string {
	t, _ := projectionDate(start, month)
	return t.Format("2006-01")
}

func (st *ProjectionState) stepLifetimeMonth(m int, returnsFor func(*models.WhatIfSettings, int) MonthReturns) MonthOutcome {
	s := st.active
	if s.RothConversion != nil && s.RothConversion.Enabled {
		return MonthOutcome{Err: fmt.Errorf("lifetime account-aware Roth conversions are not supported")}
	}
	currentDate, dateErr := projectionDate(s.StartDate, m)
	if dateErr != nil {
		return MonthOutcome{Err: dateErr}
	}
	calendarYear, calendarMonth := currentDate.Year(), int(currentDate.Month())
	currentYear := m / 12
	if m%12 == 0 && len(st.in.Chain) > 0 {
		idx, next := st.in.Hooks.ResolveChain(currentYear, st.nextChainIdx, st.primary, st.in.Chain)
		if next != nil {
			if err := validateLifetimeChainIdentity(st.Lifetime, next.Lifetime); err != nil {
				return MonthOutcome{Err: err}
			}
			applyLifetimeAssumptions(st.Lifetime, next.Lifetime)
			st.active = next
			s = next
			st.nextChainIdx = idx
			st.TaxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
			st.TaxCalculator.Age65Count = Age65CountForYear(s, calendarYear-ParseStartYear(s.StartDate))
		}
	}
	if calendarMonth == 1 {
		if m > 0 {
			st.CompletedMAGIHistory = append(st.CompletedMAGIHistory, st.CurrentYearTaxSnapshot.AnnualMAGI)
			st.TaxState = ProjectionTaxAccumulator{}
			for id, a := range st.Lifetime.Accounts {
				a.RMDPaidYTD = 0
				v := a.Value
				a.Account.PriorDecemberValue = &v
				st.Lifetime.Accounts[id] = a
			}
		}
		st.TaxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
		st.TaxCalculator.Age65Count = Age65CountForYear(s, calendarYear-ParseStartYear(s.StartDate))
	}
	p := returnsFor(s, m)
	if m > 0 {
		st.CumulativeInflation *= monthlyCompoundFactorFromDecimal(p.InflationAnnual)
		st.NetCumulativeInflation *= monthlyCompoundFactorFromDecimal(p.NetInflationAnnual)
		st.CurrentLivingExpenses *= monthlyCompoundFactorFromDecimal(p.NetInflationAnnual)
	}
	opening := st.Lifetime.Clone()
	maxIterations := st.in.TaxSettlementMaxIterations
	if maxIterations < 0 {
		return MonthOutcome{Err: fmt.Errorf("tax settlement iteration budget must be nonnegative")}
	}
	if maxIterations == 0 {
		maxIterations = 32
	}
	var final lifetimeTrialResult
	_, err := executeTaxSettlement(maxIterations, true, func(taxGuess, irmaaGuess float64) (ProjectedTaxSnapshot, float64, error) {
		r, err := runLifetimeTrial(st, opening, s, m, p, taxGuess, irmaaGuess)
		if err == nil {
			final = r
		}
		return r.snapshot, r.settlementTax, err
	})
	if err != nil {
		return MonthOutcome{Err: err}
	}
	final.portfolio.Converged = true
	st.Lifetime = final.state
	st.syncLifetimeAggregates()
	st.CurrentYearTaxSnapshot = final.snapshot
	ApplyTaxStateMonth(&st.TaxState, final.income, final.portfolio, 0)
	return MonthOutcome{Result: final.portfolio, Income: final.income, TotalExpenses: final.expenses + final.portfolio.IRMAAExpense, PlannedExpenses: final.expenses + final.portfolio.IRMAAExpense, LivingExpenses: st.CurrentLivingExpenses, Healthcare: s.GetTotalHealthcareCost(m), GuardrailMultiplier: 1, AllowTaxDeferredWithdrawal: true, TotalBalance: st.Lifetime.Total(), Lifetime: &final.outcome}
}
