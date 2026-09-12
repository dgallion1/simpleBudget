package engine

import (
	"math"
	"reflect"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

func lifetimeMonthSettings() *models.WhatIfSettings {
	prior := 106000.0
	return &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, CurrentAge: 75, MonthlyLivingExpenses: 0, InflationRate: 0,
		Persons:   []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 200, CashPercent: 100},
			{ID: "broker", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", OpeningValue: 0, Basis: 0, StockPercent: 100},
			{ID: "ira", OwnerID: "older", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 106000, PriorDecemberValue: &prior, StockPercent: 100},
		}, ScheduledSavings: []models.ScheduledSaving{{ID: "save", OwnerID: "older", DestinationAccountID: "broker", MonthlyAmount: 500, StartMonth: "2026-01"}},
			CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "ira"}}}}
}
func TestLifetimeScheduledSavingUsesAvailableCashOnly(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Lifetime.Accounts[2].OpeningValue = 0
	zero := 0.0
	s.Lifetime.Accounts[2].PriorDecemberValue = &zero
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
		return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if out.Lifetime.UnfundedSaving != 300 || st.Lifetime.Accounts["broker"].Value != 200 || st.Lifetime.Accounts["ira"].Value != 0 {
		t.Fatalf("out=%+v accounts=%+v", out.Lifetime, st.Lifetime.Accounts)
	}
}
func TestLifetimeRMDUsesAccountPriorDecemberAndReinvestsGross(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Lifetime.ScheduledSavings = nil
	s.RMDTiming = models.RMDTimingStartOfYear
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
		return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	want := 106000 / GetLifeExpectancyFactor(75)
	if out.Lifetime == nil || out.Result.CashFlow.RMDWithdrawal != want {
		t.Fatalf("RMD=%v want=%v", out.Result.CashFlow.RMDWithdrawal, want)
	}
	if st.Lifetime.Accounts["ira"].Value != 106000-want {
		t.Fatalf("ira=%v", st.Lifetime.Accounts["ira"].Value)
	}
}
func TestLifetimeMissingRMDHistoryIsCalculationErrorNotDepletion(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Lifetime.Accounts[2].PriorDecemberValue = nil
	s.RMDTiming = models.RMDTimingStartOfYear
	result := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	if result.CalculationError == "" || result.DepletionMonth != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestLifetimeMidyearCalendarRollover(t *testing.T) {
	s := lifetimeMonthSettings()
	s.StartDate = "2026-09"
	s.ProjectionYears = 2
	s.Persons[0].BirthMonth = "1980-01"
	s.Lifetime.ScheduledSavings = nil
	s.Lifetime.Accounts[2].PriorDecemberValue = nil
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	var out MonthOutcome
	for month := 0; month <= 4; month++ {
		out = st.StepMonth(month, func(*models.WhatIfSettings, int) MonthReturns {
			return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
		})
		if out.Err != nil {
			t.Fatal(out.Err)
		}
	}
	if out.Lifetime.Year != 2027 || out.Lifetime.Month != 1 {
		t.Fatalf("calendar=%d-%d", out.Lifetime.Year, out.Lifetime.Month)
	}
	if st.Lifetime.Contributions.Year != 2027 || st.Lifetime.Contributions.LastMonth != 1 {
		t.Fatalf("contribution state=%+v", st.Lifetime.Contributions)
	}
	if st.Lifetime.Accounts["ira"].Account.PriorDecemberValue == nil {
		t.Fatal("January did not retain prior December balance")
	}
}

func TestLifetimeChainRejectsChangedLegalIdentity(t *testing.T) {
	s := lifetimeMonthSettings()
	state := NewLifetimeAccountState(s)
	next := *s.Lifetime
	next.Accounts = append([]models.LifetimeAccount(nil), s.Lifetime.Accounts...)
	next.Accounts[0].OwnerID = "different"
	if err := validateLifetimeChainIdentity(state, &next); err == nil {
		t.Fatal("changed account identity accepted")
	}
}

func TestLifetimeRMDIsPerOwnerAndYoungerSpouseDoesNotDistribute(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Persons = append(s.Persons, models.Person{ID: "younger", Name: "Younger", Role: models.PersonRoleSpouse, BirthMonth: "1966-01"})
	s.Lifetime.ScheduledSavings = nil
	s.Lifetime.Accounts[0].OpeningValue = 100000
	prior := 53000.0
	s.Lifetime.Accounts = append(s.Lifetime.Accounts, models.LifetimeAccount{ID: "younger-ira", OwnerID: "younger", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 53000, PriorDecemberValue: &prior, StockPercent: 100})
	s.Lifetime.CashPolicy.WithdrawalOrder = []string{"cash", "broker", "ira", "younger-ira"}
	s.RMDTiming = models.RMDTimingStartOfYear
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
		return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if st.Lifetime.Accounts["younger-ira"].Value != 53000 {
		t.Fatalf("younger spouse RMD fired: %v", st.Lifetime.Accounts["younger-ira"].Value)
	}
	if st.Lifetime.Accounts["ira"].Value >= 106000 {
		t.Fatal("older owner RMD did not fire")
	}
}

func TestLifetimeMidyearStartSatisfiesOverdueOpeningRMD(t *testing.T) {
	s := lifetimeMonthSettings()
	s.StartDate = "2026-09"
	s.Lifetime.ScheduledSavings = nil
	s.RMDTiming = models.RMDTimingMidYear
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
		return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if out.Result.CashFlow.RMDWithdrawal <= 0 {
		t.Fatal("opening-year overdue RMD was skipped")
	}
}

func TestLifetimeCurrentEmployerRMDDeferralIsAccountSpecific(t *testing.T) {
	for _, tc := range []struct {
		name     string
		overFive bool
		wantRMD  bool
	}{{"eligible deferral", false, false}, {"owner exception", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeMonthSettings()
			s.Lifetime.ScheduledSavings = nil
			s.RMDTiming = models.RMDTimingStartOfYear
			a := &s.Lifetime.Accounts[2]
			a.LegalType = "401k"
			a.PlanID = "plan"
			a.EmployerID = "employer"
			a.LimitGroup = "limit"
			a.CurrentEmployerRMDDeferral = true
			a.OwnerMoreThanFivePercent = tc.overFive
			s.Lifetime.Jobs = []models.LifetimeJob{{ID: "job", OwnerID: "older", EmployerID: "employer", GrossSalary: 12000, EligibleCompensation: 12000, StartMonth: "2026-01"}}
			st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
			out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
				return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
			})
			if out.Err != nil {
				t.Fatal(out.Err)
			}
			if (out.Result.CashFlow.RMDWithdrawal > 0) != tc.wantRMD {
				t.Fatalf("RMD=%v want positive=%v", out.Result.CashFlow.RMDWithdrawal, tc.wantRMD)
			}
		})
	}
}

func TestLifetimeFailedMonthPreservesWholeOpeningState(t *testing.T) {
	s := lifetimeMonthSettings()
	s.RMDTiming = models.RMDTimingStartOfYear
	s.IncomeSources = []models.IncomeSource{{ID: "pension", Amount: 10000}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s), TaxSettlementMaxIterations: 1})
	beforeAccounts := st.Lifetime.Clone()
	beforeHistory := make([]float64, len(st.CompletedMAGIHistory))
	copy(beforeHistory, st.CompletedMAGIHistory)
	beforeInflation := st.CumulativeInflation
	beforeTax := st.TaxState
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) MonthReturns {
		return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Err == nil {
		t.Fatal("expected nonconvergence")
	}
	if !reflect.DeepEqual(st.Lifetime, beforeAccounts) || !reflect.DeepEqual(st.CompletedMAGIHistory, beforeHistory) || st.CumulativeInflation != beforeInflation || !reflect.DeepEqual(st.TaxState, beforeTax) {
		t.Fatalf("state mutated on failed month: got=%#v want=%#v history=%v/%v inflation=%v/%v tax=%#v/%#v", st.Lifetime, beforeAccounts, st.CompletedMAGIHistory, beforeHistory, st.CumulativeInflation, beforeInflation, st.TaxState, beforeTax)
	}
}

func attempt2Returns(_ *models.WhatIfSettings, _ int) MonthReturns {
	return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
}

func TestLifetimeWorkingAgeTraditionalIRAWithdrawalGrossesUpEarlyTax(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, MonthlyLivingExpenses: 500,
		Persons:   []models.Person{{ID: "young", Name: "Young", Role: models.PersonRolePrimary, BirthMonth: "1986-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "young", LegalType: "cash", TaxTreatment: "taxable", CashPercent: 100},
			{ID: "broker", OwnerID: "young", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
			{ID: "ira", OwnerID: "young", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 1000, StockPercent: 100},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "ira"}}}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if math.Abs(st.Lifetime.Accounts["ira"].Value-444.444444) > .02 ||
		math.Abs(out.Lifetime.EarlyDistributionTax-55.555556) > .02 {
		t.Fatalf("closing IRA %.6f early tax %.6f", st.Lifetime.Accounts["ira"].Value, out.Lifetime.EarlyDistributionTax)
	}
	if math.Abs(out.Lifetime.TaxLiability-out.Lifetime.TaxPayments) > .02 {
		t.Fatalf("liability %.6f payments %.6f", out.Lifetime.TaxLiability, out.Lifetime.TaxPayments)
	}
}

func TestLifetimeIRMAAFundedAsHealthcareNotTax(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1,
		Persons:   []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 10000, CashPercent: 100},
			{ID: "broker", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 10000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	st.AssumedLookbackMAGI = 300000
	opening := st.Lifetime.Total()
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if math.Abs((opening-st.Lifetime.Total())-out.Result.IRMAAExpense) > .01 {
		t.Fatalf("wealth reduction %.2f surcharge %.2f", opening-st.Lifetime.Total(), out.Result.IRMAAExpense)
	}
	if out.Lifetime.TaxPayments != 0 {
		t.Fatalf("IRMAA reclassified as tax: %.2f", out.Lifetime.TaxPayments)
	}
	found := false
	for _, m := range out.Lifetime.Movements {
		if m.Reason == "irmaa" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing IRMAA movement")
	}
}

func TestLifetimeMidyearPayrollSeedsCanonicalJobYTD(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-09", ProjectionYears: 1,
		Persons:   []models.Person{{ID: "owner", Name: "Owner", Role: models.PersonRolePrimary, BirthMonth: "1980-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "owner", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 100000, CashPercent: 100},
			{ID: "broker", OwnerID: "owner", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
		}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "owner", EmployerID: "e", GrossSalary: 120000, EligibleCompensation: 120000, StartMonth: "2026-01"}},
			YTD:        models.LifetimeYTD{Year: 2026, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 200000, EligiblePay: 200000}}},
			CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker"}}}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := st.Lifetime.Payroll.SocialSecurityWagesByOwner["owner"]; got != 210000 {
		t.Fatalf("SS wages %.2f", got)
	}
	if math.Abs(st.Lifetime.Payroll.AdditionalMedicarePaid-90) > .001 {
		t.Fatalf("additional Medicare %.2f", st.Lifetime.Payroll.AdditionalMedicarePaid)
	}
}

func TestLifetimeChainPreservesRuntimeHistoryWithoutPointerAlias(t *testing.T) {
	prior := 90000.0
	current := &LifetimeAccountState{Accounts: map[string]LifetimeAccountRuntime{"roth": {
		Account: models.LifetimeAccount{ID: "roth", OwnerID: "p", LegalType: "401k", TaxTreatment: "roth", PlanID: "plan", EmployerID: "e", RothFirstFundedYear: 2010, PriorDecemberValue: &prior, StockPercent: 60, BondPercent: 40},
		Value:   100000, Basis: 70000, RMDPaidYTD: 1234,
	}}}
	authoredPrior := 1.0
	next := &models.LifetimeSettings{Accounts: []models.LifetimeAccount{{ID: "roth", OwnerID: "p", LegalType: "401k", TaxTreatment: "roth", PlanID: "plan", EmployerID: "e", RothFirstFundedYear: 2029, PriorDecemberValue: &authoredPrior, StockPercent: 80, BondPercent: 20}}}
	applyLifetimeAssumptions(current, next)
	got := current.Accounts["roth"]
	if got.Account.StockPercent != 80 || got.Account.RothFirstFundedYear != 2010 || *got.Account.PriorDecemberValue != 90000 || got.RMDPaidYTD != 1234 {
		t.Fatalf("history/allocation: %+v", got)
	}
	prior = 7
	if *got.Account.PriorDecemberValue != 90000 {
		t.Fatal("prior December pointer aliased")
	}
}

func TestLifetimeIRMAAPartialFundingIsUnpaidEssentialExpense(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1,
		Persons:   []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 100, CashPercent: 100},
			{ID: "broker", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	st.AssumedLookbackMAGI = 300000
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if st.Lifetime.Total() != 0 || math.Abs(out.Lifetime.UnfundedExpenses-(out.Result.IRMAAExpense-100)) > .01 {
		t.Fatalf("closing %.2f unpaid %.2f IRMAA %.2f", st.Lifetime.Total(), out.Lifetime.UnfundedExpenses, out.Result.IRMAAExpense)
	}
	if out.Lifetime.TaxPayments != 0 {
		t.Fatalf("IRMAA entered tax payments %.2f", out.Lifetime.TaxPayments)
	}
}

func TestLifetimeNonqualifiedDesignatedRothWithdrawalIsExplicitlyUnsupported(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, MonthlyLivingExpenses: 1,
		Persons:   []models.Person{{ID: "young", Name: "Young", Role: models.PersonRolePrimary, BirthMonth: "1986-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "young", LegalType: "cash", TaxTreatment: "taxable", CashPercent: 100},
			{ID: "broker", OwnerID: "young", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
			{ID: "roth", OwnerID: "young", LegalType: "401k", TaxTreatment: "roth", OpeningValue: 100, Basis: 100, RothFirstFundedYear: 2020, StockPercent: 100},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "roth"}}}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	before := st.Lifetime.Clone()
	out := st.StepMonth(0, attempt2Returns)
	if out.Err == nil {
		t.Fatal("expected unsupported nonqualified designated Roth withdrawal")
	}
	if !reflect.DeepEqual(st.Lifetime, before) {
		t.Fatal("unsupported withdrawal mutated state")
	}
}

func TestLifetimeActualChainKeepsRothClockAndAdoptsAllocation(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-09", ProjectionYears: 2,
		Persons:   []models.Person{{ID: "p", Name: "P", Role: models.PersonRolePrimary, BirthMonth: "1980-01"}},
		TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle},
		Lifetime: &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 1000, CashPercent: 100},
			{ID: "broker", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
			{ID: "roth", OwnerID: "p", LegalType: "401k", TaxTreatment: "roth", OpeningValue: 100, Basis: 100, RothFirstFundedYear: 2010, StockPercent: 60, BondPercent: 40},
		}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 1000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "roth"}}}}
	next := *s
	nextLifetime := *s.Lifetime
	nextAccounts := append([]models.LifetimeAccount(nil), s.Lifetime.Accounts...)
	nextAccounts[2].RothFirstFundedYear = 2029
	nextAccounts[2].StockPercent, nextAccounts[2].BondPercent = 80, 20
	nextLifetime.Accounts = nextAccounts
	next.Lifetime = &nextLifetime
	input := Input{Prepared: prepare.MustFrom(t, s), Chain: []PreparedChainLink{{Settings: prepare.MustFrom(t, &next)}}, Hooks: Hooks{
		ResolveChainTransition: func(year, idx int, _ *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
			if year == 1 && idx == 0 {
				return 1, chain[0].Settings.Settings()
			}
			return idx, nil
		},
	}}
	st := NewProjectionState(input)
	for m := 0; m <= 12; m++ {
		if out := st.StepMonth(m, attempt2Returns); out.Err != nil {
			t.Fatalf("month %d: %v", m, out.Err)
		}
	}
	got := st.Lifetime.Accounts["roth"].Account
	if got.RothFirstFundedYear != 2010 || got.StockPercent != 80 {
		t.Fatalf("chain account %+v", got)
	}
}

func TestLifetimeOutcomeExposesCanonicalRMDAndConsumptionFacts(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Lifetime.ScheduledSavings = nil
	s.RMDTiming = models.RMDTimingStartOfYear
	s.MonthlyLivingExpenses = 100
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	l := out.Lifetime
	if l.ConsumptionAssessed < 100 || l.ConsumptionPaid <= 0 {
		t.Fatalf("consumption facts=%+v", l)
	}
	if len(l.RMD) != 1 || l.RMD[0].AccountID != "ira" || l.RMD[0].Obligation <= 0 || l.RMD[0].Distributed <= 0 || l.RMD[0].Taxable <= 0 {
		t.Fatalf("RMD facts=%+v", l.RMD)
	}
	for _, a := range l.Accounts {
		if math.Abs(a.Opening+a.Deposits-a.Withdrawals+a.Return-a.Closing) > 1e-8 {
			t.Fatalf("account does not reconcile: %+v", a)
		}
	}
}

func TestLifetimeProjectionAdaptsMonthsAndCalendarYears(t *testing.T) {
	s := lifetimeMonthSettings()
	s.StartDate = "2026-11"
	s.ProjectionYears = 1
	s.Persons[0].BirthMonth = "1980-01"
	s.Lifetime.ScheduledSavings = nil
	s.Lifetime.Accounts[2].PriorDecemberValue = nil
	got := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	if got.CalculationError != "" {
		t.Fatal(got.CalculationError)
	}
	if len(got.Months) != 12 || got.Months[0].Lifetime == nil || got.Months[0].Lifetime.Year != 2026 || got.Months[2].Lifetime.Year != 2027 {
		t.Fatalf("month adapter=%+v", got.Months)
	}
	if len(got.LifetimeYearSummaries) != 2 || !got.LifetimeYearSummaries[0].Available || !got.LifetimeYearSummaries[1].Available || got.LifetimeYearSummaries[0].Year != 2026 || !got.LifetimeYearSummaries[0].PartialYear || got.LifetimeYearSummaries[1].Year != 2027 || !got.LifetimeYearSummaries[1].PartialYear {
		t.Fatalf("calendar years=%+v", got.LifetimeYearSummaries)
	}
}

func TestLifetimeRMDObligationObservableBeforeDistributionMonth(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Lifetime.ScheduledSavings = nil
	s.RMDTiming = models.RMDTimingEndOfYear
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, attempt2Returns)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if len(out.Lifetime.RMD) != 1 || out.Lifetime.RMD[0].Obligation <= 0 || out.Lifetime.RMD[0].Distributed != 0 {
		t.Fatalf("pre-distribution RMD facts=%+v", out.Lifetime.RMD)
	}
}
func TestLifetimeDiagnosticsAreOptInAndFinalOnly(t *testing.T) {
	s := lifetimeMonthSettings()
	s.Persons[0].BirthMonth = "1980-01"
	s.Lifetime.ScheduledSavings = nil
	s.Lifetime.Accounts[2].PriorDecemberValue = nil
	off := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	if off.LifetimeDiagnostics != nil {
		t.Fatalf("default diagnostics=%+v", off.LifetimeDiagnostics)
	}
	on := New().Run(Input{Prepared: prepare.MustFrom(t, s), CaptureLifetimeDiagnostics: true})
	if on.LifetimeDiagnostics == nil || len(on.LifetimeDiagnostics.Accounts) != 3 {
		t.Fatalf("diagnostics=%+v", on.LifetimeDiagnostics)
	}
}

func TestLifetimeExternalGrossIgnoresContributionTaxClassification(t *testing.T) {
	cases := []struct {
		name, treatment                     string
		rothPercent, employee, employerRoth float64
		wantCash                            float64
	}{
		{name: "traditional employee", treatment: "traditional", employee: 500, wantCash: 9500},
		{name: "Roth employee", treatment: "roth", rothPercent: 100, employee: 500, wantCash: 9500},
		{name: "Roth employer", treatment: "roth", employerRoth: 100, wantCash: 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			destination := "traditional"
			tradID, rothID := "retirement", ""
			if tc.treatment == "roth" {
				destination, tradID, rothID = "roth", "", "retirement"
			}
			rate := models.ContributionRate{Mode: "fixed", FixedMonthly: tc.employee}
			rule := models.ContributionRule{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: rate, RothPercent: tc.rothPercent, TraditionalAccountID: tradID, RothAccountID: rothID}
			if tc.employerRoth > 0 {
				rule.RothPercent = 100
				rule.Employer = &models.EmployerContribution{Mode: "fixed", FixedMonthly: tc.employerRoth, DestinationAccountID: "retirement", MatchTiming: "monthly"}
			}
			s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, Persons: []models.Person{{ID: "p", Name: "Person", Role: models.PersonRolePrimary, BirthMonth: "1980-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}, Lifetime: &models.LifetimeSettings{Version: 1,
				Accounts: []models.LifetimeAccount{{ID: "cash", Name: "Cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", CashPercent: 100}, {ID: "broker", Name: "Brokerage", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", CashPercent: 100}, {ID: "retirement", Name: "Retirement", OwnerID: "p", LegalType: "401k", TaxTreatment: destination, PlanID: "plan", EmployerID: "employer", LimitGroup: "group", Capabilities: models.PlanCapabilities{AllowEmployeeRoth: true, AllowEmployerRoth: true}, CashPercent: 100}},
				Jobs:     []models.LifetimeJob{{ID: "job", OwnerID: "p", EmployerID: "employer", GrossSalary: 120000, EligibleCompensation: 120000, StartMonth: "2026-01"}}, ContributionRules: []models.ContributionRule{rule}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 1e9, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}}
			st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
			out := st.StepMonth(0, attempt2Returns)
			if out.Err != nil {
				t.Fatal(out.Err)
			}
			if out.Lifetime.ExternalIncome != 10000 || out.Lifetime.EmployeeContributions != tc.employee || out.Lifetime.EmployerContributions != tc.employerRoth {
				t.Fatalf("canonical resources=%+v", out.Lifetime)
			}
			var cashIncome float64
			for _, movement := range out.Lifetime.Movements {
				if movement.Reason == "income" {
					cashIncome += movement.Amount
				}
			}
			if cashIncome != tc.wantCash {
				t.Fatalf("cash income %.2f want %.2f", cashIncome, tc.wantCash)
			}
			projection := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
			if projection.CalculationError != "" || len(projection.LifetimeYearSummaries) != 1 || !projection.LifetimeYearSummaries[0].Available {
				t.Fatalf("annual projection=%+v", projection)
			}
		})
	}
}
