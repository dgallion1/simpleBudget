package plan

import (
	"encoding/json"
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
)

func checkerLifetimeOverrideBase(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.ProjectionYears = 4
	s.PortfolioValue = 9_999_999
	s.MonthlyLivingExpenses = 1500
	s.InflationRate = 2
	s.InvestmentReturn = 4
	s.Persons = []models.Person{{ID: "owner", Name: "Owner", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 61)}, {ID: "spouse", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 61)}}
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2000, FRA: 67, ClaimAge: 62}
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
		{ID: "cash", Name: "Household cash", OwnerID: "owner", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 100000, CashPercent: 100},
		{ID: "broker", Name: "Investments", OwnerID: "owner", LegalType: "brokerage", TaxTreatment: "taxable", OpeningValue: 100000, Basis: 100000, StockPercent: 100},
	}, Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "owner", EmployerID: "employer", GrossSalary: 120000, EligibleCompensation: 120000, StartMonth: "2026-01"}}, YTD: models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 50000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker"}}}
	return s
}

func checkerRunLifetimeOverride(t *testing.T, base *models.WhatIfSettings, o Overrides) AnalysisView {
	t.Helper()
	before, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	v, err := RunWithOverrides(base, o)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("RunWithOverrides mutated base")
	}
	if v.CalculationError != "" || len(v.Years) != base.ProjectionYears {
		t.Fatalf("missing available lifetime years: %+v", v)
	}
	if v.Headline.PortfolioValue != 200000 {
		t.Fatalf("legacy portfolio won over account openings: %+v", v.Headline)
	}
	if v.Years[0].ExternalIncome <= 0 {
		t.Fatalf("canonical lifetime year facts absent: %+v", v.Years[0])
	}
	prepared, err := preparedWithOverrides(base, o)
	if err != nil {
		t.Fatal(err)
	}
	projection := engine.New().Run(engine.Input{Prepared: prepared, Hooks: retirement.DefaultHooks()})
	if projection.CalculationError != "" || len(projection.LifetimeYearSummaries) != base.ProjectionYears {
		t.Fatalf("canonical projection unavailable: error=%q years=%d", projection.CalculationError, len(projection.LifetimeYearSummaries))
	}
	for _, year := range projection.LifetimeYearSummaries {
		if !year.Available || len(year.Accounts) == 0 {
			t.Fatalf("canonical lifetime year unavailable or lacks account facts: %+v", year)
		}
	}
	if v.Headline.FinalBalance != math.Round(projection.FinalBalance) {
		t.Fatalf("public final %.0f does not match rounded canonical final %.0f", v.Headline.FinalBalance, math.Round(projection.FinalBalance))
	}
	return v
}

func TestLifetimeOverrideInvestmentReturnEffect(t *testing.T) {
	b := checkerLifetimeOverrideBase(t)
	lo := checkerRunLifetimeOverride(t, b, Overrides{InvestmentReturn: ptr(0.5)})
	hi := checkerRunLifetimeOverride(t, b, Overrides{InvestmentReturn: ptr(10.0)})
	t.Logf("return final low=%.0f high=%.0f", lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	if hi.Headline.FinalBalance <= lo.Headline.FinalBalance {
		t.Fatal("investment return override had no canonical effect")
	}
}
func TestLifetimeOverrideExpenseEffect(t *testing.T) {
	b := checkerLifetimeOverrideBase(t)
	lo := checkerRunLifetimeOverride(t, b, Overrides{MonthlyLivingExpenses: ptr(500.0)})
	hi := checkerRunLifetimeOverride(t, b, Overrides{MonthlyLivingExpenses: ptr(5000.0)})
	t.Logf("expense final low=%.0f high=%.0f", lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	if hi.Headline.FinalBalance >= lo.Headline.FinalBalance {
		t.Fatal("expense override had no canonical effect")
	}
}
func TestLifetimeOverrideInflationEffect(t *testing.T) {
	b := checkerLifetimeOverrideBase(t)
	lo := checkerRunLifetimeOverride(t, b, Overrides{InflationRate: ptr(0.0)})
	hi := checkerRunLifetimeOverride(t, b, Overrides{InflationRate: ptr(10.0)})
	t.Logf("inflation final low=%.0f high=%.0f", lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	if hi.Headline.FinalBalance >= lo.Headline.FinalBalance {
		t.Fatal("inflation override had no canonical effect")
	}
}
func TestLifetimeOverrideTaxEffect(t *testing.T) {
	b := checkerLifetimeOverrideBase(t)
	single := "single"
	joint := "married_joint"
	a := checkerRunLifetimeOverride(t, b, Overrides{FilingStatus: &single})
	z := checkerRunLifetimeOverride(t, b, Overrides{FilingStatus: &joint})
	t.Logf("tax paid single=%.0f joint=%.0f finals %.0f/%.0f", a.Years[0].TaxPayments, z.Years[0].TaxPayments, a.Headline.FinalBalance, z.Headline.FinalBalance)
	if z.Years[0].TaxPayments >= a.Years[0].TaxPayments || z.Headline.FinalBalance <= a.Headline.FinalBalance {
		t.Fatal("filing-status override had no canonical tax effect")
	}
}
func TestLifetimeOverrideBenefitEffect(t *testing.T) {
	b := checkerLifetimeOverrideBase(t)
	b.Persons[0].BirthMonth = models.BirthMonthForAge(b.StartDate, 67)
	b.Persons[1].BirthMonth = models.BirthMonthForAge(b.StartDate, 67)
	b.SocialSecurity.FRA = 67
	b.SocialSecurity.ClaimAge = 67
	lo := checkerRunLifetimeOverride(t, b, Overrides{SocialSecurityFRABenefit: ptr(500.0)})
	hi := checkerRunLifetimeOverride(t, b, Overrides{SocialSecurityFRABenefit: ptr(5000.0)})
	t.Logf("benefit income low=%.0f high=%.0f finals %.0f/%.0f", lo.Years[0].ExternalIncome, hi.Years[0].ExternalIncome, lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	if hi.Years[0].ExternalIncome <= lo.Years[0].ExternalIncome || hi.Headline.FinalBalance <= lo.Headline.FinalBalance {
		t.Fatal("benefit override had no canonical effect")
	}
}
