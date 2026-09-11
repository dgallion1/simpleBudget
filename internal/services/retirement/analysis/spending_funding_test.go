package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func TestSpendingFundingSourceAccountingUsesCanonicalFieldsOnce(t *testing.T) {
	s := spendingFundingSettings()
	s.ProjectionYears = 1
	s.IncomeSources = []models.IncomeSource{
		{ID: "pension", Name: "Pension", Amount: 1000, StartMonth: 0},
		{ID: "manual-ss", Name: "Social Security", Amount: 9999, StartMonth: 0},
	}
	in := engineInput(t, s)
	in.Hooks = engine.Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
		ProjectedSocialSecurityIncome:  func(*models.WhatIfSettings, int) float64 { return 2000 },
	}
	projection := &models.ProjectionResult{Months: []models.ProjectionMonth{{
		Month:                     0,
		CumulativeInflation:       2,
		SocialSecurityIncome:      2000,
		WithdrawalFromTaxDeferred: 3000,
		WithdrawalFromTaxable:     400,
		WithdrawalFromRoth:        100,
		TaxesPaid:                 800,
		StateTaxPaid:              100,
		PlannedLivingExpenses:     5000,
		FundedLivingExpenses:      4500,
		HealthcareExpense:         600,
		RMDWithdrawal:             700,
		RothConversions:           600,
		TaxableWithdrawals:        9000,
	}}}

	got, err := BuildSpendingFundingTimeline(in, projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 1 {
		t.Fatalf("months = %d, want 1", len(got.Months))
	}
	want := models.SpendingFundingMonth{
		Month:                     0,
		CalendarMonth:             "2026-01",
		SocialSecurityReal:        1000,
		OtherConfiguredIncomeReal: 500,
		TaxDeferredWithdrawalReal: 1500,
		TaxableWithdrawalReal:     200,
		RothWithdrawalReal:        50,
		TaxesPaidReal:             400,
		PlannedLivingReal:         2500,
		FundedLivingReal:          2250,
		HealthcareReal:            300,
	}
	if got.Months[0] != want {
		t.Fatalf("month = %+v, want %+v", got.Months[0], want)
	}
	if got.Complete || got.EndReason != "base_case_projection_ended" || got.ExpectedMonths != 12 || got.ObservedMonths != 1 {
		t.Fatalf("scope = %+v", got)
	}
	if len(got.AnnualAverages) != 1 || got.AnnualAverages[0].TaxesPaidReal != 400 {
		t.Fatalf("annual accounting = %+v", got.AnnualAverages)
	}
}

func TestSpendingFundingBenefitTimingInflationAndCalendarScope(t *testing.T) {
	s := spendingFundingSettings()
	s.StartDate = "2026-11"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.ProjectionYears = 2
	s.IncomeSources = []models.IncomeSource{
		{ID: "manual-ss", Name: "Social Security", Amount: 9999, StartMonth: 0},
		{ID: "pension", Name: "Pension", Amount: 1200, StartMonth: 0},
	}
	in := engineInput(t, s)
	in.Hooks = engine.Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
		ProjectedSocialSecurityIncome: func(_ *models.WhatIfSettings, month int) float64 {
			if month >= 6 {
				return 2400
			}
			return 0
		},
	}
	months := make([]models.ProjectionMonth, 15)
	for month := range months {
		cpi := float64(month + 1)
		ss := 0.0
		if month >= 6 {
			ss = 2400
		}
		months[month] = models.ProjectionMonth{
			Month:                 month,
			CumulativeInflation:   cpi,
			SocialSecurityIncome:  ss,
			FundedLivingExpenses:  5000 * cpi,
			PlannedLivingExpenses: 5500 * cpi,
		}
	}

	got, err := BuildSpendingFundingTimeline(in, &models.ProjectionResult{Months: months})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 15 || got.ObservedEndMonth != "2028-01" || got.ExpectedEndMonth != "2028-10" {
		t.Fatalf("observed scope = %+v", got)
	}
	if got.Months[0].OtherConfiguredIncomeReal != 1200 || got.Months[1].OtherConfiguredIncomeReal != 600 {
		t.Fatalf("monthly CPI deflation = %+v", got.Months[:2])
	}
	if got.Months[5].SocialSecurityReal != 0 || math.Abs(got.Months[6].SocialSecurityReal-(2400.0/7.0)) > 1e-9 {
		t.Fatalf("benefit timing = month5 %.4f month6 %.4f", got.Months[5].SocialSecurityReal, got.Months[6].SocialSecurityReal)
	}
	if len(got.AnnualAverages) != 3 {
		t.Fatalf("annual rows = %d, want 3", len(got.AnnualAverages))
	}
	if y := got.AnnualAverages[0]; y.CalendarYear != 2026 || y.ObservedMonths != 2 || !y.Partial || y.OtherConfiguredIncomeReal != 900 {
		t.Fatalf("2026 average = %+v", y)
	}
	if y := got.AnnualAverages[1]; y.CalendarYear != 2027 || y.ObservedMonths != 12 || y.Partial {
		t.Fatalf("2027 average = %+v", y)
	}
	if y := got.AnnualAverages[2]; y.CalendarYear != 2028 || y.ObservedMonths != 1 || !y.Partial {
		t.Fatalf("2028 average = %+v", y)
	}
	if len(got.Markers) != 1 || got.Markers[0].Kind != "configured_income_start" || got.Markers[0].Label != "Pension starts" {
		t.Fatalf("manual SS hook suppression markers = %+v", got.Markers)
	}
}

func TestSpendingFundingRejectsInvalidOrMissingCanonicalEvidence(t *testing.T) {
	in := engineInput(t, spendingFundingSettings())
	valid := models.ProjectionMonth{Month: 0, CumulativeInflation: 1}
	for _, tc := range []struct {
		name       string
		projection *models.ProjectionResult
	}{
		{name: "nil result", projection: nil},
		{name: "no observed months", projection: &models.ProjectionResult{}},
		{name: "zero CPI", projection: &models.ProjectionResult{Months: []models.ProjectionMonth{{Month: 0}}}},
		{name: "NaN CPI", projection: &models.ProjectionResult{Months: []models.ProjectionMonth{{Month: 0, CumulativeInflation: math.NaN()}}}},
		{name: "infinite CPI", projection: &models.ProjectionResult{Months: []models.ProjectionMonth{{Month: 0, CumulativeInflation: math.Inf(1)}}}},
		{name: "month gap", projection: &models.ProjectionResult{Months: []models.ProjectionMonth{valid, {Month: 2, CumulativeInflation: 1}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := BuildSpendingFundingTimeline(in, tc.projection); err == nil {
				t.Fatalf("got timeline %+v, want error", got)
			}
		})
	}
}

func TestSpendingFundingCompleteHorizonAndMarkersStayWithinEvidence(t *testing.T) {
	s := spendingFundingSettings()
	s.StartDate = "2026-09"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.ProjectionYears = 1
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2027-03"}
	s.IncomeSources = []models.IncomeSource{
		{ID: "inside", Name: "Pension", Amount: 500, StartMonth: 3},
		{ID: "outside", Name: "Deferred annuity", Amount: 500, StartMonth: 12},
	}
	in := engineInput(t, s)
	months := make([]models.ProjectionMonth, 12)
	for month := range months {
		months[month] = models.ProjectionMonth{Month: month, CumulativeInflation: 1}
	}

	got, err := BuildSpendingFundingTimeline(in, &models.ProjectionResult{Months: months})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete || got.EndReason != "horizon" || got.ObservedEndMonth != "2027-08" || got.ExpectedEndMonth != "2027-08" {
		t.Fatalf("complete scope = %+v", got)
	}
	if len(got.Markers) != 2 {
		t.Fatalf("markers = %+v, want pension start and boost stop", got.Markers)
	}
	if got.Markers[0].Month != 3 || got.Markers[0].CalendarMonth != "2026-12" || got.Markers[1].Month != 6 || got.Markers[1].Kind != "living_spending_boost_stop" {
		t.Fatalf("markers = %+v", got.Markers)
	}
}

func spendingFundingSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	return s
}
