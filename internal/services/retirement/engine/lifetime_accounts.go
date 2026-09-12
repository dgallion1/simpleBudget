package engine

import (
	"fmt"
	"math"
	"sort"

	"budget2/internal/models"
)

type LifetimeAccountRuntime struct {
	Account          models.LifetimeAccount
	Value            float64
	Basis            float64
	RealizedGainsYTD float64
	RMDPaidYTD       float64
}

type LifetimeAccountState struct {
	Accounts      map[string]LifetimeAccountRuntime
	Contributions ContributionYearState
	Payroll       PayrollYearState
}

func NewLifetimeAccountState(s *models.WhatIfSettings) *LifetimeAccountState {
	out := &LifetimeAccountState{Accounts: map[string]LifetimeAccountRuntime{}}
	if s == nil || s.Lifetime == nil {
		return out
	}
	for _, a := range s.Lifetime.Accounts {
		if a.PriorDecemberValue != nil {
			v := *a.PriorDecemberValue
			a.PriorDecemberValue = &v
		}
		out.Accounts[a.ID] = LifetimeAccountRuntime{Account: a, Value: a.OpeningValue, Basis: a.Basis, RMDPaidYTD: a.RMDPaidYTD}
	}
	start, err := projectionDate(s.StartDate, 0)
	if err == nil && s.Lifetime.YTD.Year == start.Year() {
		out.Payroll = PayrollYearState{Year: start.Year(), SocialSecurityWagesByOwner: map[string]float64{}}
		jobs := map[string]models.LifetimeJob{}
		for _, j := range s.Lifetime.Jobs {
			jobs[j.ID] = j
		}
		for _, y := range s.Lifetime.YTD.Jobs {
			if j, ok := jobs[y.JobID]; ok {
				out.Payroll.SocialSecurityWagesByOwner[j.OwnerID] += y.GrossWages
				out.Payroll.MedicareWages += y.GrossWages
			}
		}
		status := models.FilingSingle
		if s.TaxConfig != nil {
			status = s.TaxConfig.FilingStatus
		}
		out.Payroll.AdditionalMedicarePaid = math.Max(0, out.Payroll.MedicareWages-additionalMedicareThreshold(status)) * additionalMedicareRate
	}
	return out
}

func (s *LifetimeAccountState) Clone() *LifetimeAccountState {
	if s == nil {
		return nil
	}
	out := &LifetimeAccountState{Accounts: make(map[string]LifetimeAccountRuntime, len(s.Accounts)), Contributions: cloneContributionYearState(s.Contributions), Payroll: clonePayrollYearState(s.Payroll)}
	for k, v := range s.Accounts {
		if v.Account.PriorDecemberValue != nil {
			n := *v.Account.PriorDecemberValue
			v.Account.PriorDecemberValue = &n
		}
		out.Accounts[k] = v
	}
	return out
}
func (s *LifetimeAccountState) Zero() {
	for k, v := range s.Accounts {
		v.Value = 0
		v.Basis = 0
		s.Accounts[k] = v
	}
}
func (s *LifetimeAccountState) Total() float64 {
	var n float64
	for _, a := range s.Accounts {
		n += a.Value
	}
	return n
}

func (st *ProjectionState) syncLifetimeAggregates() {
	if st.Lifetime == nil {
		return
	}
	st.TaxDeferredBalance, st.RothBalance = 0, 0
	taxableValue, taxableBasis := 0.0, 0.0
	for _, a := range st.Lifetime.Accounts {
		switch a.Account.TaxTreatment {
		case "traditional":
			st.TaxDeferredBalance += a.Value
		case "roth":
			st.RothBalance += a.Value
		case "taxable":
			if a.Account.LegalType == "brokerage" {
				taxableValue += a.Value
				taxableBasis += a.Basis
			}
		}
	}
	st.TaxableAccount = NewTaxableAccountState(st.active, taxableValue)
	st.TaxableAccount.CostBasis = taxableBasis
}

func lifetimeAccountReturn(a models.LifetimeAccount, p MonthReturns) float64 {
	if p.AssetClassMonthly != nil {
		return a.StockPercent/100*p.AssetClassMonthly.Stock + a.BondPercent/100*p.AssetClassMonthly.Bond + a.CashPercent/100*p.AssetClassMonthly.Cash
	}
	switch a.TaxTreatment {
	case "traditional":
		return p.TaxDeferredMonthly
	case "roth":
		return p.RothMonthly
	default:
		return monthlyCompoundFactorFromDecimal(p.TaxableAnnualPercent/100) - 1
	}
}

func validateLifetimeChainIdentity(current *LifetimeAccountState, next *models.LifetimeSettings) error {
	if current == nil || next == nil {
		return fmt.Errorf("lifetime chain settings missing")
	}
	if len(current.Accounts) != len(next.Accounts) {
		return fmt.Errorf("lifetime chain account IDs changed")
	}
	for _, n := range next.Accounts {
		c, ok := current.Accounts[n.ID]
		if !ok {
			return fmt.Errorf("lifetime chain account %q added or removed", n.ID)
		}
		a := c.Account
		if a.OwnerID != n.OwnerID || a.LegalType != n.LegalType || a.TaxTreatment != n.TaxTreatment || a.PlanID != n.PlanID || a.EmployerID != n.EmployerID {
			return fmt.Errorf("lifetime chain account %q legal identity changed", n.ID)
		}
	}
	return nil
}

func applyLifetimeAssumptions(state *LifetimeAccountState, next *models.LifetimeSettings) {
	for _, n := range next.Accounts {
		v := state.Accounts[n.ID]
		live := v.Account
		n.OpeningValue = v.Value
		n.Basis = v.Basis
		n.RothFirstFundedYear = live.RothFirstFundedYear
		n.RMDPaidYTD = v.RMDPaidYTD
		if live.PriorDecemberValue != nil {
			prior := *live.PriorDecemberValue
			n.PriorDecemberValue = &prior
		} else {
			n.PriorDecemberValue = nil
		}
		v.Account = n
		state.Accounts[n.ID] = v
	}
}

func sortedAccountIDs(m map[string]LifetimeAccountRuntime) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func withdrawLifetime(a *LifetimeAccountRuntime, amount float64) (cash, gain, rothEarnings float64) {
	if amount <= 0 || a.Value <= 0 {
		return
	}
	cash = math.Min(amount, a.Value)
	switch a.Account.TaxTreatment {
	case "taxable":
		if a.Account.LegalType == "brokerage" {
			basisReduction := a.Basis * (cash / a.Value)
			gain = math.Max(0, cash-basisReduction)
			a.Basis -= basisReduction
			a.RealizedGainsYTD += gain
		}
	case "roth":
		basis := math.Min(cash, a.Basis)
		a.Basis -= basis
		rothEarnings = cash - basis
	}
	a.Value -= cash
	if a.Value < 1e-9 {
		a.Value = 0
		if a.Account.TaxTreatment == "taxable" {
			a.Basis = 0
		}
	}
	return
}

func depositLifetime(a *LifetimeAccountRuntime, amount float64) {
	if amount <= 0 {
		return
	}
	a.Value += amount
	if a.Account.TaxTreatment == "taxable" || a.Account.TaxTreatment == "roth" {
		a.Basis += amount
	}
}

func (st *ProjectionState) CaptureLifetimeDiagnostics(totals models.LifetimeDiagnostics) *models.LifetimeDiagnostics {
	if st == nil || st.Lifetime == nil {
		return nil
	}
	for _, id := range sortedAccountIDs(st.Lifetime.Accounts) {
		a := st.Lifetime.Accounts[id]
		totals.Accounts = append(totals.Accounts, models.LifetimeAccountDiagnostic{AccountID: id, Value: a.Value, Basis: a.Basis})
	}
	return &totals
}

// AccumulateLifetimeDiagnostics adds committed monthly facts without retaining history.
func AccumulateLifetimeDiagnostics(totals *models.LifetimeDiagnostics, out *models.LifetimeMonthOutcome) {
	if totals == nil || out == nil {
		return
	}
	totals.EmployeeContributions += out.EmployeeContributions
	totals.EmployerContributions += out.EmployerContributions
	totals.TaxLiability += out.TaxLiability
	totals.TaxPayments += out.TaxPayments
	if totals.RMDObligationByAccountYear == nil {
		totals.RMDObligationByAccountYear = map[string]float64{}
	}
	for _, f := range out.RMD {
		key := fmt.Sprintf("%s/%d", f.AccountID, f.Year)
		if f.Obligation > totals.RMDObligationByAccountYear[key] {
			totals.RMDObligation += f.Obligation - totals.RMDObligationByAccountYear[key]
			totals.RMDObligationByAccountYear[key] = f.Obligation
		}
		totals.RMDDistributed += f.Distributed
	}
}
