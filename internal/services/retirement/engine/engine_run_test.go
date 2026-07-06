package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

func TestEngineRunCoversCanonicalMonthlyLoop(t *testing.T) {
	s := richEngineScenario()

	proj := New().Run(Input{
		Prepared: prepare.MustFrom(t, s),
		Hooks: Hooks{
			SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
			ProjectedSocialSecurityIncome:  func(*models.WhatIfSettings, int) float64 { return 900 },
		},
	})

	if proj == nil {
		t.Fatal("Run returned nil projection")
	}
	if !proj.Survives {
		t.Fatalf("projection depleted unexpectedly at month %v", proj.DepletionMonth)
	}
	if got, want := len(proj.Months), s.ProjectionYears*12; got != want {
		t.Fatalf("months=%d, want %d", got, want)
	}
	if got, want := len(proj.YearlySummaries), s.ProjectionYears; got != want {
		t.Fatalf("yearly summaries=%d, want %d", got, want)
	}

	first := proj.Months[0]
	if first.TotalIncome <= 0 {
		t.Fatalf("first month total income=%v, want positive", first.TotalIncome)
	}
	if first.SocialSecurityIncome != 900 {
		t.Fatalf("first month SS income=%v, want optimizer value 900", first.SocialSecurityIncome)
	}
	if first.TotalExpenses <= first.GeneralExpenses {
		t.Fatalf("total expenses=%v should include healthcare/property/extra expenses beyond general=%v", first.TotalExpenses, first.GeneralExpenses)
	}
	if first.RothConversions != 12000 {
		t.Fatalf("first month Roth conversion=%v, want 12000", first.RothConversions)
	}

	foundGuardrailCut := false
	for _, event := range proj.GuardrailEvents {
		if event.Type == "cut" && event.Multiplier < event.PreviousMultiplier {
			foundGuardrailCut = true
			break
		}
	}
	if !foundGuardrailCut {
		t.Fatalf("expected a guardrail cut event, got %#v", proj.GuardrailEvents)
	}

	if proj.FinalBalance <= 0 {
		t.Fatalf("final balance=%v, want positive", proj.FinalBalance)
	}
}

func TestEngineRunStopsAtDepletion(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 45)
	s.PortfolioValue = 500
	s.MonthlyLivingExpenses = 10_000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InvestmentReturn = 0
	s.ProjectionYears = 2
	s.IncomeSources = nil
	s.ExpenseSources = nil
	s.TaxDeferredPercent = 0
	s.RothPercent = 0

	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})

	if proj.Survives {
		t.Fatal("projection should deplete")
	}
	if proj.DepletionMonth == nil || *proj.DepletionMonth != 0 {
		t.Fatalf("depletion month=%v, want 0", proj.DepletionMonth)
	}
	if got := len(proj.Months); got != 1 {
		t.Fatalf("months=%d, want projection to stop after depleted month", got)
	}
	if proj.FinalBalance != 0 {
		t.Fatalf("final balance=%v, want 0", proj.FinalBalance)
	}
}

func TestEngineRunAppliesChainTransition(t *testing.T) {
	base := richEngineScenario()
	base.ProjectionYears = 3
	base.MonthlyLivingExpenses = 1_000
	base.Guardrails = nil

	next := richEngineScenario()
	next.MonthlyLivingExpenses = 2_500
	next.Guardrails = nil

	proj := New().Run(Input{
		Prepared: prepare.MustFrom(t, base),
		Chain: []PreparedChainLink{
			{
				ScenarioFilename: "next.json",
				TransitionAge:    66,
				Settings:         prepare.MustFrom(t, next),
			},
		},
		Hooks: Hooks{
			ResolveChainTransition: func(currentYear, nextChainIndex int, _ *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
				if currentYear == 1 && nextChainIndex == 0 {
					return 1, chain[0].Settings.Settings()
				}
				return nextChainIndex, nil
			},
		},
	})

	if len(proj.Months) < 14 {
		t.Fatalf("projection too short after chain transition: %d months", len(proj.Months))
	}
	before := proj.Months[11].GeneralExpenses
	after := proj.Months[12].GeneralExpenses
	if after <= before {
		t.Fatalf("chain transition did not rebase expenses upward: before=%v after=%v", before, after)
	}
}

func TestProjectionAndSteadyStateHelpers(t *testing.T) {
	s := richEngineScenario()
	s.ProjectionYears = 4
	s.IncomeSources = append(s.IncomeSources, models.IncomeSource{
		Name:       "Delayed pension",
		Amount:     750,
		StartMonth: 18,
	})
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 1500,
		FRA:        67,
		ClaimAge:   68,
	}

	if taxDeferredDelayActive(s, 0) {
		t.Fatal("tax-deferred delay should be disabled by default")
	}
	s.TaxDeferredDelayYears = 2
	if !taxDeferredDelayActive(s, 1) || taxDeferredDelayActive(s, 2) {
		t.Fatal("tax-deferred delay window was not applied")
	}
	if got := earlyWithdrawalPenaltyRate(58, 1); got != 0.10 {
		t.Fatalf("early penalty=%v, want 0.10", got)
	}
	if got := earlyWithdrawalPenaltyRate(59, 1); got != 0 {
		t.Fatalf("age 60 penalty=%v, want 0", got)
	}

	breakdown := CalculateMonthlyIncomeBreakdown(Hooks{}, s, 0)
	if breakdown.OrdinaryIncome <= 0 || breakdown.SocialSecurityIncome <= 0 {
		t.Fatalf("manual income breakdown missing ordinary or SS income: %#v", breakdown)
	}
	optimizerBreakdown := CalculateMonthlyIncomeBreakdown(Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
		ProjectedSocialSecurityIncome:  func(*models.WhatIfSettings, int) float64 { return 1234 },
	}, s, 0)
	if optimizerBreakdown.SocialSecurityIncome != 1234 {
		t.Fatalf("optimizer SS income=%v, want 1234", optimizerBreakdown.SocialSecurityIncome)
	}

	if got := MedicareEligibleAdultCountAtYear(s, 1); got != 2 {
		t.Fatalf("Medicare eligible adults in year 1=%d, want 2", got)
	}
	if got := PlannerIRMAAInflationFactorForYear(3, 2); got != 1 {
		t.Fatalf("IRMAA base factor=%v, want 1", got)
	}
	if got := PlannerIRMAAInflationFactorForYear(3, 3); got <= 1 {
		t.Fatalf("future IRMAA factor=%v, want > 1", got)
	}

	steady := FindSteadyStateMonth(Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
	}, s)
	if steady != 48 {
		t.Fatalf("steady state month=%d, want SS claim start at 48", steady)
	}
	if !ssValidClaimAge(62) || !ssValidClaimAge(70) || ssValidClaimAge(71) {
		t.Fatal("SS claim age validation failed")
	}
	if got := ssClaimStartMonth(67, 70); got != 36 {
		t.Fatalf("claim start month=%d, want 36", got)
	}
}

func TestExpenseGuardrailAndPortfolioHelpers(t *testing.T) {
	s := richEngineScenario()
	s.SpendingPhaseConfig.Enabled = true
	phaseExpense := LivingExpensesAtMonth(s, 12)
	if phaseExpense <= 0 {
		t.Fatalf("phase expense after inflation=%v, want positive", phaseExpense)
	}

	rebasedPhase := RebaseLivingExpensesAtTransition(s, 66, 1.03, 1.01)
	if math.Abs(rebasedPhase-s.MonthlyLivingExpenses*0.90*1.03) > 0.001 {
		t.Fatalf("rebased phase expense=%v, want phase multiplier with inflation", rebasedPhase)
	}
	s.SpendingPhaseConfig.Enabled = false
	rebasedSimple := RebaseLivingExpensesAtTransition(s, 66, 1.50, 1.01)
	if math.Abs(rebasedSimple-s.MonthlyLivingExpenses*1.01) > 0.001 {
		t.Fatalf("simple rebase=%v, want net inflation anchor", rebasedSimple)
	}

	if got := TotalIncome(s, 0); got <= 0 {
		t.Fatalf("total income=%v, want positive", got)
	}
	if got := TotalExpenses(s, 0); got <= s.MonthlyLivingExpenses {
		t.Fatalf("total expenses=%v, want more than living expenses", got)
	}
	breakdown := CalculateExpenseBreakdown(s, 0)
	if breakdown.Essential <= 0 || breakdown.Discretionary <= 0 || breakdown.Total <= breakdown.Essential {
		t.Fatalf("expense breakdown missing categories: %#v", breakdown)
	}

	state := NewGuardrailState(100_000)
	cut := state.Evaluate(&models.GuardrailConfig{
		FloorDropPct:    10,
		FloorCutPct:     20,
		CeilingRisePct:  10,
		CeilingRaisePct: 20,
		MinSpendingPct:  70,
		MaxSpendingPct:  130,
	}, 80_000)
	if cut != 0.8 || state.Multiplier() != 0.8 {
		t.Fatalf("guardrail cut multiplier=%v state=%v, want 0.8", cut, state.Multiplier())
	}
	raise := state.Evaluate(&models.GuardrailConfig{
		FloorDropPct:    10,
		FloorCutPct:     20,
		CeilingRisePct:  10,
		CeilingRaisePct: 25,
		MinSpendingPct:  70,
		MaxSpendingPct:  130,
	}, 130_000)
	if raise != 1.0 {
		t.Fatalf("guardrail raise multiplier=%v, want 1.0", raise)
	}

	before, after := ProjectionTimingGrowthFractions(models.ProjectionTimingMidMonth)
	if before != 0.5 || after != 0.5 {
		t.Fatalf("mid-month growth fractions=%v/%v, want 0.5/0.5", before, after)
	}
	cashFlow := PortfolioCashFlowResult{
		WithdrawalFromTaxDeferred: 10,
		WithdrawalFromTaxable:     20,
		WithdrawalFromRoth:        30,
	}
	if got := cashFlow.GrossWithdrawal(); got != 60 {
		t.Fatalf("gross withdrawal=%v, want 60", got)
	}
}

func richEngineScenario() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{
			ID:         "primary",
			Name:       "Primary",
			BirthMonth: models.BirthMonthForAge(s.StartDate, 64),
			Role:       models.PersonRolePrimary,
		},
		{
			ID:         "spouse",
			Name:       "Spouse",
			BirthMonth: models.BirthMonthForAge(s.StartDate, 65),
			Role:       models.PersonRoleSpouse,
		},
	}
	s.CurrentAge = 64
	s.SpouseAge = 65
	s.PortfolioValue = 750_000
	s.MonthlyLivingExpenses = 2_500
	s.MonthlyHealthcare = 400
	s.MonthlyPropertyTax = 300
	s.ProjectionYears = 3
	s.InflationRate = 3
	s.HealthcareInflation = 5
	s.PropertyTaxInflation = 4
	s.SpendingDeclineRate = 1
	s.InvestmentReturn = -15
	s.TaxDeferredPercent = 50
	s.RothPercent = 20
	s.TaxableDividendYield = 2
	s.TaxableCapitalGainsDistributionRate = 1
	s.TaxableQualifiedDividendPercent = 60
	s.ProjectionTiming = models.ProjectionTimingMidMonth
	s.RMDTiming = models.RMDTimingStartOfYear
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.10},
			{Name: "Slow-Go", StartAge: 66, Multiplier: 0.90},
		},
	}
	s.IncomeSources = []models.IncomeSource{
		{Name: "Pension", Amount: 1_200, COLARate: 0.02},
		{Name: "Social Security", Amount: 800},
	}
	s.ExpenseSources = []models.ExpenseSource{
		{Name: "Travel", Amount: 250, Inflation: true, Discretionary: true},
	}
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		StartYear:    0,
		EndYear:      1,
		AnnualAmount: 12_000,
	}
	s.BigTicketItems = []models.BigTicketItem{
		{Name: "Gift", Amount: 5_000, Year: 0, Type: models.BigTicketIncome},
		{Name: "Car", Amount: 15_000, Year: 1, Type: models.BigTicketExpense},
	}
	s.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    5,
		FloorCutPct:     10,
		CeilingRisePct:  25,
		CeilingRaisePct: 10,
		MinSpendingPct:  75,
		MaxSpendingPct:  125,
	}
	return s
}
