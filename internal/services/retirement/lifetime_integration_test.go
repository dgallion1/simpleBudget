package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func assertLifetimeYearReconciles(t *testing.T, y models.LifetimeYearSummary) {
	t.Helper()
	if !y.Available {
		t.Fatalf("lifetime year unavailable: %s", y.UnavailableReason)
	}
	want := y.OpeningWealth + y.ExternalIncome + y.EmployerContributions + y.InvestmentReturn - y.Consumption - y.TaxPayments
	if math.Abs(want-y.ClosingWealth) > .0001 {
		t.Fatalf("household reconciliation: opening %.8f + income %.8f + employer %.8f + return %.8f - consumption %.8f - tax %.8f = %.8f, closing %.8f", y.OpeningWealth, y.ExternalIncome, y.EmployerContributions, y.InvestmentReturn, y.Consumption, y.TaxPayments, want, y.ClosingWealth)
	}
	for _, a := range y.Accounts {
		if math.Abs(a.Opening+a.Deposits-a.Withdrawals+a.Return-a.Closing) > .0001 {
			t.Fatalf("account %s does not reconcile: %+v", a.AccountID, a)
		}
	}
}

func assertLifetimeProjectionReconciles(t *testing.T, p *models.ProjectionResult) {
	t.Helper()
	if p.CalculationError != "" {
		t.Fatalf("calculation error: %s", p.CalculationError)
	}
	for i, m := range p.Months {
		if m.Lifetime == nil {
			t.Fatalf("month %d lacks lifetime facts", i)
		}
		l := m.Lifetime
		if math.Abs(l.TaxPayments+l.UnpaidTax-l.TaxLiability) > .0001 {
			t.Fatalf("month %d tax paid/unpaid mismatch: %+v", i, l)
		}
		if math.Abs(l.ConsumptionPaid+l.UnfundedExpenses-l.ConsumptionAssessed) > .0001 {
			t.Fatalf("month %d consumption paid/unpaid mismatch: %+v", i, l)
		}
		for _, a := range l.Accounts {
			if math.Abs(a.Opening+a.Deposits-a.Withdrawals+a.Return-a.Closing) > .0001 {
				t.Fatalf("month %d account %s does not reconcile: %+v", i, a.AccountID, a)
			}
		}
	}
	for _, y := range p.LifetimeYearSummaries {
		assertLifetimeYearReconciles(t, y)
		if math.Abs(y.TaxPayments+y.UnpaidTax-y.TaxLiability) > .0001 {
			t.Fatalf("year %d tax paid/unpaid mismatch: %+v", y.Year, y)
		}
		if math.Abs(y.Consumption+y.UnfundedExpenses-y.ConsumptionAssessed) > .0001 {
			t.Fatalf("year %d consumption paid/unpaid mismatch: %+v", y.Year, y)
		}
	}
}

func TestLifetimeIntegrationYoungerSaverContributionAndMatch(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, InvestmentReturn: 0, InflationRate: 0, Persons: []models.Person{{ID: "p", Name: "Saver", Role: models.PersonRolePrimary, BirthMonth: "1986-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}}
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
		{ID: "cash", Name: "Cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 1_000_000, CashPercent: 100},
		{ID: "broker", Name: "Brokerage", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", CashPercent: 100},
		{ID: "trad", Name: "401(k)", OwnerID: "p", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "401k", CashPercent: 100},
	}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "p", EmployerID: "employer", GrossSalary: 100000, EligibleCompensation: 100000, StartMonth: "2026-01"}}, ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: models.ContributionRate{Mode: "percent", PercentOfCompensation: 10}, TraditionalAccountID: "trad", Employer: &models.EmployerContribution{Mode: "match", Tiers: []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}}, DestinationAccountID: "trad", MatchTiming: "monthly"}}}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 1_000_000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}
	p := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, s)})
	if p.CalculationError != "" || len(p.LifetimeYearSummaries) != 1 {
		t.Fatalf("projection error=%q years=%d", p.CalculationError, len(p.LifetimeYearSummaries))
	}
	y := p.LifetimeYearSummaries[0]
	if math.Abs(y.EmployeeContributions-10000) > .01 || math.Abs(y.EmployerContributions-3000) > .01 {
		t.Fatalf("employee/employer = %.2f/%.2f, want 10000/3000", y.EmployeeContributions, y.EmployerContributions)
	}
	assertLifetimeProjectionReconciles(t, p)
}

func TestLifetimeIntegrationMixedWorkingAndRMDHousehold(t *testing.T) {
	factor := engine.GetLifeExpectancyFactor(75)
	prior := 40000 * factor
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, InvestmentReturn: 0, InflationRate: 0, RMDTiming: models.RMDTimingStartOfYear, Persons: []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}, {ID: "worker", Name: "Worker", Role: models.PersonRoleSpouse, BirthMonth: "1986-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}}
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
		{ID: "cash", Name: "Cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 1_000_000, CashPercent: 100},
		{ID: "broker", Name: "Brokerage", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", CashPercent: 100},
		{ID: "ira", Name: "Older IRA", OwnerID: "older", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: prior, PriorDecemberValue: &prior, CashPercent: 100},
		{ID: "work", Name: "Worker 401(k)", OwnerID: "worker", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "401k", CashPercent: 100},
	}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "worker", EmployerID: "employer", GrossSalary: 100000, EligibleCompensation: 100000, StartMonth: "2026-01"}}, ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: models.ContributionRate{Mode: "percent", PercentOfCompensation: 10}, TraditionalAccountID: "work", Employer: &models.EmployerContribution{Mode: "match", Tiers: []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}}, DestinationAccountID: "work", MatchTiming: "monthly"}}}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 1_000_000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "ira"}}}
	p := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, s), CaptureLifetimeDiagnostics: true})
	if p.CalculationError != "" || len(p.LifetimeYearSummaries) != 1 {
		t.Fatalf("projection error=%q years=%d", p.CalculationError, len(p.LifetimeYearSummaries))
	}
	y := p.LifetimeYearSummaries[0]
	if math.Abs(y.Distributions-40000) > .01 {
		t.Fatalf("gross distributions=%.2f want40000", y.Distributions)
	}
	if math.Abs(y.EmployeeContributions-10000) > .01 || math.Abs(y.EmployerContributions-3000) > .01 {
		t.Fatalf("working flows=%+v", y)
	}
	if y.TaxLiability <= 0 || y.TaxPayments <= 0 {
		t.Fatalf("actual taxes absent: %+v", y)
	}
	if len(y.RMD) != 1 || math.Abs(y.RMD[0].Obligation-40000) > .01 || math.Abs(y.RMD[0].Distributed-40000) > .01 || math.Abs(y.RMD[0].Taxable-40000) > .01 {
		t.Fatalf("canonical RMD facts=%+v", y.RMD)
	}
	if y.AllSourceReinvestment <= 40000 {
		t.Fatalf("actual all-source reinvestment missing: %.8f", y.AllSourceReinvestment)
	}
	var surplusMovements float64
	for _, m := range y.Movements {
		if m.Reason == "surplus" {
			surplusMovements += m.Amount
		}
	}
	if math.Abs(surplusMovements-y.AllSourceReinvestment) > .0001 {
		t.Fatalf("reinvestment movements %.8f != canonical %.8f", surplusMovements, y.AllSourceReinvestment)
	}
	var brokerDeposits float64
	for _, a := range y.Accounts {
		if a.AccountID == "broker" {
			brokerDeposits = a.Deposits
		}
	}
	if p.LifetimeDiagnostics == nil {
		t.Fatal("missing opt-in lifetime diagnostics")
	}
	var brokerBasis float64
	for _, a := range p.LifetimeDiagnostics.Accounts {
		if a.AccountID == "broker" {
			brokerBasis = a.Basis
		}
	}
	if math.Abs(brokerBasis-brokerDeposits) > .0001 {
		t.Fatalf("broker basis %.8f differs from actual deposits %.8f", brokerBasis, brokerDeposits)
	}
	assertLifetimeProjectionReconciles(t, p)
}

func TestNilLifetimeMatchesFrozenAcceptedLP4NumericalBaseline(t *testing.T) {
	// Recorded from frozen accepted-LP4 snapshot /tmp/lp4-primary-a3-checker at HEAD a71e8b9b4541d36e9cdc6bf1beacea82c18e3fe8.
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.PortfolioValue = 100000
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	s.ExpenseSources = nil
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.InvestmentReturn = 6
	s.InflationRate = 0
	s.ProjectionYears = 1
	s.ProjectionTiming = models.ProjectionTimingEndOfMonth
	s.Lifetime = nil
	want := []float64{100486.75505653431, 100975.87941791923, 101467.38461686594, 101961.28224222164, 102457.58393924288, 102956.30140987002, 103457.44641300326, 103961.03076477982, 104467.06633885257, 104975.56506666998, 105486.53893775746, 106000.00000000003}
	p := newTestCalc(t, s).RunProjection()
	if len(p.Months) != len(want) {
		t.Fatalf("months=%d want%d", len(p.Months), len(want))
	}
	for i, w := range want {
		if p.Months[i].PortfolioBalance != w || p.Months[i].TotalIncome != 0 || p.Months[i].TotalExpenses != 0 || p.Months[i].TaxesPaid != 0 {
			t.Fatalf("month%d got=%+v want balance %.17g and zero flows", i, p.Months[i], w)
		}
	}
	if p.FinalBalance != 106000.00000000003 || len(p.YearlySummaries) != 1 {
		t.Fatalf("final/year=%v/%+v", p.FinalBalance, p.YearlySummaries)
	}
	y := p.YearlySummaries[0]
	if y.StartingBalance != 100000 || y.Growth != 6000.000000000014 || y.EndingBalance != 106000.00000000003 {
		t.Fatalf("annual baseline=%+v", y)
	}
}

func midyearLifetimeSettings() *models.WhatIfSettings {
	s := &models.WhatIfSettings{StartDate: "2026-09", ProjectionYears: 1, InvestmentReturn: 0, InflationRate: 0, Persons: []models.Person{{ID: "p", Name: "Worker", Role: models.PersonRolePrimary, BirthMonth: "1986-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}}
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "cash", Name: "Cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 500000, CashPercent: 100}, {ID: "broker", Name: "Brokerage", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", CashPercent: 100}, {ID: "trad", Name: "401(k)", OwnerID: "p", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "401k", CashPercent: 100}}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "p", EmployerID: "employer", GrossSalary: 120000, EligibleCompensation: 120000, StartMonth: "2026-01"}}, ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: models.ContributionRate{Mode: "fixed", FixedMonthly: 2000}, TraditionalAccountID: "trad"}}, YTD: models.LifetimeYTD{Year: 2026, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 80000, EligiblePay: 80000}}, Rules: []models.ContributionYTD{{RuleID: "rule", EmployeeRegular: 20000, EmployeeCatchUp: 0, Employer: 0, MatchingPaid: 0}}}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 500000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}
	return s
}

func TestLifetimeIntegrationMidyearHistoryDecemberCloseAndJanuaryReset(t *testing.T) {
	p := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, midyearLifetimeSettings())})
	if p.CalculationError != "" || len(p.LifetimeYearSummaries) != 2 {
		t.Fatalf("projection error=%q years=%+v", p.CalculationError, p.LifetimeYearSummaries)
	}
	dec, jan := p.LifetimeYearSummaries[0], p.LifetimeYearSummaries[1]
	if dec.Year != 2026 || dec.FirstMonth != 9 || dec.LastMonth != 12 || !dec.PartialYear || math.Abs(dec.EmployeeContributions-4500) > .01 {
		t.Fatalf("2026 partial/YTD cap=%+v", dec)
	}
	if jan.Year != 2027 || jan.FirstMonth != 1 || jan.LastMonth != 8 || !jan.PartialYear || math.Abs(jan.EmployeeContributions-16000) > .01 {
		t.Fatalf("2027 reset=%+v", jan)
	}
	assertLifetimeProjectionReconciles(t, p)
}

func linkedRetirementSettings(retirement string) *models.WhatIfSettings {
	s := &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 1, InvestmentReturn: 0, InflationRate: 0, Persons: []models.Person{{ID: "p", Name: "Worker", Role: models.PersonRolePrimary, BirthMonth: "1986-01", RetirementMonth: retirement}}, IncomeSources: []models.IncomeSource{{ID: "salary", Amount: 10000}, {ID: "benefit", Amount: 500}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}}
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{{ID: "cash", Name: "Cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 500000, CashPercent: 100}, {ID: "broker", Name: "Brokerage", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", CashPercent: 100}, {ID: "trad", Name: "401(k)", OwnerID: "p", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "401k", CashPercent: 100}}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "p", EmployerID: "employer", GrossSalary: 120000, EligibleCompensation: 120000, StartMonth: "2026-01", EndAtRetirement: true, IncomeSourceID: "salary"}}, ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", EndAtRetirement: true, Rate: models.ContributionRate{Mode: "percent", PercentOfCompensation: 10}, TraditionalAccountID: "trad"}}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 500000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash"}}}
	return s
}

func TestLifetimeIntegrationRetirementMoveChangesOnlyLinkedWork(t *testing.T) {
	march := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, linkedRetirementSettings("2026-03"))})
	april := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, linkedRetirementSettings("2026-04"))})
	if march.CalculationError != "" || april.CalculationError != "" {
		t.Fatalf("errors=%q/%q", march.CalculationError, april.CalculationError)
	}
	if march.Months[2].TotalIncome != 500 || april.Months[2].TotalIncome != 10500 || march.Months[3].TotalIncome != 500 || april.Months[3].TotalIncome != 500 {
		t.Fatalf("independent benefit or linked salary moved: March setting=%v/%v April setting=%v/%v", march.Months[2].TotalIncome, march.Months[3].TotalIncome, april.Months[2].TotalIncome, april.Months[3].TotalIncome)
	}
	if math.Abs(april.LifetimeYearSummaries[0].EmployeeContributions-march.LifetimeYearSummaries[0].EmployeeContributions-1000) > .01 {
		t.Fatalf("linked contribution delta=%v/%v", march.LifetimeYearSummaries[0].EmployeeContributions, april.LifetimeYearSummaries[0].EmployeeContributions)
	}
	assertLifetimeProjectionReconciles(t, march)
	assertLifetimeProjectionReconciles(t, april)
}
