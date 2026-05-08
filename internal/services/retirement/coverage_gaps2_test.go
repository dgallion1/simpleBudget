package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// --- SS with non-zero investment income in provisional income ---

func TestCalculateTaxableSocialSecurity_InvestmentIncomeAffectsPI(t *testing.T) {
	// Qualified dividends and LTCG increase provisional income, pushing SS into taxable range.
	tests := []struct {
		name        string
		ssBenefits  float64
		otherIncome float64
		qd          float64
		ltcg        float64
		status      models.FilingStatus
	}{
		{
			name:        "qualified dividends push PI above base threshold single",
			ssBenefits:  20000,
			otherIncome: 10000,
			qd:          10000,
			ltcg:        0,
			status:      models.FilingSingle,
		},
		{
			name:        "LTCG push PI above base threshold single",
			ssBenefits:  20000,
			otherIncome: 10000,
			qd:          0,
			ltcg:        10000,
			status:      models.FilingSingle,
		},
		{
			name:        "both QD and LTCG push PI above upper threshold MFJ",
			ssBenefits:  30000,
			otherIncome: 20000,
			qd:          10000,
			ltcg:        10000,
			status:      models.FilingMarriedJoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withInvestment := CalculateTaxableSocialSecurity(tt.ssBenefits, tt.otherIncome, tt.qd, tt.ltcg, tt.status, false)
			withoutInvestment := CalculateTaxableSocialSecurity(tt.ssBenefits, tt.otherIncome, 0, 0, tt.status, false)

			if withInvestment <= withoutInvestment {
				t.Errorf("expected investment income to increase taxable SS: with=%f without=%f", withInvestment, withoutInvestment)
			}
		})
	}
}

// --- SS exactly at threshold boundaries ---

func TestCalculateTaxableSocialSecurity_ExactThresholds(t *testing.T) {
	tests := []struct {
		name        string
		ssBenefits  float64
		otherIncome float64
		status      models.FilingStatus
		wantZero    bool
		desc        string
	}{
		{
			name:        "single PI exactly at 25K base threshold",
			ssBenefits:  10000,
			otherIncome: 20000, // PI = 20000 + 5000 = 25000
			status:      models.FilingSingle,
			wantZero:    true,
			desc:        "at base threshold, should be zero",
		},
		{
			name:        "single PI just above 25K base threshold",
			ssBenefits:  10000,
			otherIncome: 20001, // PI = 20001 + 5000 = 25001
			status:      models.FilingSingle,
			wantZero:    false,
			desc:        "just above base threshold, should be non-zero",
		},
		{
			name:        "single PI exactly at 34K upper threshold",
			ssBenefits:  20000,
			otherIncome: 24000, // PI = 24000 + 10000 = 34000
			status:      models.FilingSingle,
			wantZero:    false,
			desc:        "at upper threshold, should be in between-thresholds range",
		},
		{
			name:        "single PI just above 34K upper threshold",
			ssBenefits:  20000,
			otherIncome: 24001, // PI = 24001 + 10000 = 34001
			status:      models.FilingSingle,
			wantZero:    false,
			desc:        "just above upper threshold, 85% formula kicks in",
		},
		{
			name:        "MFJ PI exactly at 32K base threshold",
			ssBenefits:  14000,
			otherIncome: 25000, // PI = 25000 + 7000 = 32000
			status:      models.FilingMarriedJoint,
			wantZero:    true,
			desc:        "at MFJ base threshold, should be zero",
		},
		{
			name:        "MFJ PI just above 32K base threshold",
			ssBenefits:  14000,
			otherIncome: 25001, // PI = 25001 + 7000 = 32001
			status:      models.FilingMarriedJoint,
			wantZero:    false,
			desc:        "just above MFJ base threshold",
		},
		{
			name:        "MFJ PI exactly at 44K upper threshold",
			ssBenefits:  20000,
			otherIncome: 34000, // PI = 34000 + 10000 = 44000
			status:      models.FilingMarriedJoint,
			wantZero:    false,
			desc:        "at MFJ upper threshold",
		},
		{
			name:        "MFJ PI just above 44K upper threshold",
			ssBenefits:  20000,
			otherIncome: 34001, // PI = 34001 + 10000 = 44001
			status:      models.FilingMarriedJoint,
			wantZero:    false,
			desc:        "just above MFJ upper threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTaxableSocialSecurity(tt.ssBenefits, tt.otherIncome, 0, 0, tt.status, false)
			if tt.wantZero && got != 0 {
				t.Errorf("%s: expected 0, got %f", tt.desc, got)
			}
			if !tt.wantZero && got <= 0 {
				t.Errorf("%s: expected >0, got %f", tt.desc, got)
			}
		})
	}
}

func TestCalculateTaxableSocialSecurity_BetweenVsAboveUpperThreshold(t *testing.T) {
	// Verify the between-thresholds formula vs the above-upper formula for single
	// PI exactly at $34K should use the between-thresholds formula
	ssBenefits := 20000.0
	otherIncomeAtUpper := 24000.0 // PI = 34000

	atUpper := CalculateTaxableSocialSecurity(ssBenefits, otherIncomeAtUpper, 0, 0, models.FilingSingle, false)
	// Between thresholds: min(SS*0.5, (PI - baseThreshold)*0.5) = min(10000, (34000-25000)*0.5) = min(10000, 4500) = 4500
	if math.Abs(atUpper-4500) > 0.01 {
		t.Errorf("at upper threshold expected 4500, got %f", atUpper)
	}

	// Just above upper threshold: taxable = min(SS*0.5, baseTaxableAmount) + (PI - upperThreshold)*0.85
	otherIncomeAboveUpper := 25000.0 // PI = 35000
	aboveUpper := CalculateTaxableSocialSecurity(ssBenefits, otherIncomeAboveUpper, 0, 0, models.FilingSingle, false)
	// taxable = min(10000, 4500) + (35000-34000)*0.85 = 4500 + 850 = 5350
	// capped: min(SS*0.85, 5350) = min(17000, 5350) = 5350
	if math.Abs(aboveUpper-5350) > 0.01 {
		t.Errorf("above upper threshold expected 5350, got %f", aboveUpper)
	}
}

func TestCalculateTaxableSocialSecurity_NegativeSSBenefits(t *testing.T) {
	got := CalculateTaxableSocialSecurity(-1000, 50000, 0, 0, models.FilingSingle, false)
	if got != 0 {
		t.Errorf("expected 0 for negative SS benefits, got %f", got)
	}
}

func TestCalculateTaxableSocialSecurity_HeadOfHousehold(t *testing.T) {
	// HOH uses same thresholds as single
	gotHOH := CalculateTaxableSocialSecurity(20000, 30000, 0, 0, models.FilingHeadOfHousehold, false)
	gotSingle := CalculateTaxableSocialSecurity(20000, 30000, 0, 0, models.FilingSingle, false)
	if math.Abs(gotHOH-gotSingle) > 0.01 {
		t.Errorf("HOH and single should have same SS taxation: HOH=%f single=%f", gotHOH, gotSingle)
	}
}

// --- IRMAA for MFS filing status ---

func TestCalculateMonthlyIRMAA_MarriedFilingSeparately(t *testing.T) {
	// MFS has a special bracket structure: only 3 brackets
	tests := []struct {
		name string
		magi float64
		want float64
	}{
		{
			name: "below first tier MFS",
			magi: 100000,
			want: 0,
		},
		{
			name: "between first and second tier MFS",
			magi: 200000,
			// MFS second bracket upper is 391000, surcharge is 446.30+83.30=529.60
			want: 529.60,
		},
		{
			name: "above top tier MFS",
			magi: 500000,
			// MFS top bracket surcharge is 487.00+91.00=578.00
			want: 578.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateMonthlyIRMAA(tt.magi, models.FilingMarriedSeparate, 1.0)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("CalculateMonthlyIRMAA(MFS, MAGI=%f) = %f, want %f", tt.magi, got, tt.want)
			}
		})
	}
}

func TestCalculateMonthlyIRMAA_NegativeMAGI(t *testing.T) {
	got := CalculateMonthlyIRMAA(-50000, models.FilingSingle, 1.0)
	if got != 0 {
		t.Errorf("expected 0 for negative MAGI, got %f", got)
	}
}

func TestCalculateMonthlyIRMAA_ZeroInflationFactor(t *testing.T) {
	// inflationFactor <= 0 should be treated as 1
	got := CalculateMonthlyIRMAA(150000, models.FilingSingle, 0)
	gotWithOne := CalculateMonthlyIRMAA(150000, models.FilingSingle, 1)
	if math.Abs(got-gotWithOne) > 0.01 {
		t.Errorf("inflationFactor=0 should be treated as 1: got=%f, expected=%f", got, gotWithOne)
	}
}

func TestCalculateMonthlyIRMAA_NegativeInflationFactor(t *testing.T) {
	got := CalculateMonthlyIRMAA(150000, models.FilingSingle, -1)
	gotWithOne := CalculateMonthlyIRMAA(150000, models.FilingSingle, 1)
	if math.Abs(got-gotWithOne) > 0.01 {
		t.Errorf("negative inflationFactor should be treated as 1: got=%f, expected=%f", got, gotWithOne)
	}
}

func TestCalculateMonthlyIRMAA_UnknownFilingStatus(t *testing.T) {
	// Unknown filing status should fall back to MFJ
	got := CalculateMonthlyIRMAA(300000, "unknown_status", 1.0)
	gotMFJ := CalculateMonthlyIRMAA(300000, models.FilingMarriedJoint, 1.0)
	if math.Abs(got-gotMFJ) > 0.01 {
		t.Errorf("unknown filing status should fall back to MFJ: got=%f, expected=%f", got, gotMFJ)
	}
}

// --- NIIT with nonQualifiedDividends ---

func TestCalculateTaxWithInvestmentIncomeBreakdown_NonQualifiedDividends(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	// MAGI must exceed 200K threshold. excessMAGI must be larger than QD+LTCG
	// so that netInvestmentIncome is the binding constraint for NIIT.
	// MAGI = ordinaryIncome + QD + LTCG = 190000 + 5000 + 5000 = 200000 -- at threshold
	// Use higher values so excess MAGI is large: 250000 + 5000 + 5000 = 260000
	// excessMAGI = 60000, netInvestmentIncome without NQD = 10000, with NQD = 15000
	// NIIT without = min(10000, 60000) * 0.038 = 380
	// NIIT with    = min(15000, 60000) * 0.038 = 570
	ordinaryIncome := 250000.0
	qd := 5000.0
	ltcg := 5000.0

	withoutNQD := tc.CalculateTaxWithInvestmentIncomeBreakdown(ordinaryIncome, qd, ltcg, 0, 0)
	withNQD := tc.CalculateTaxWithInvestmentIncomeBreakdown(ordinaryIncome, qd, ltcg, 5000, 0)

	if withNQD.NIIT <= withoutNQD.NIIT {
		t.Errorf("expected NIIT to increase with nonQualifiedDividends: with=%f, without=%f", withNQD.NIIT, withoutNQD.NIIT)
	}
	if math.Abs(withoutNQD.NIIT-380) > 0.01 {
		t.Errorf("NIIT without NQD: expected 380, got %f", withoutNQD.NIIT)
	}
	if math.Abs(withNQD.NIIT-570) > 0.01 {
		t.Errorf("NIIT with NQD: expected 570, got %f", withNQD.NIIT)
	}
}

func TestCalculateTaxWithInvestmentIncomeBreakdown_NIITPresent(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 5.0,
	}, 0)

	// MAGI = 250K, well above 200K single threshold
	breakdown := tc.CalculateTaxWithInvestmentIncomeBreakdown(200000, 30000, 20000, 0, 0)

	if breakdown.NIIT <= 0 {
		t.Errorf("expected positive NIIT for high income single filer, got %f", breakdown.NIIT)
	}
	if breakdown.MAGI != 250000 {
		t.Errorf("expected MAGI=250000, got %f", breakdown.MAGI)
	}
	if breakdown.FederalTax <= 0 {
		t.Error("expected positive federal tax")
	}
	if breakdown.StateTax <= 0 {
		t.Error("expected positive state tax")
	}
	if breakdown.TotalTax <= 0 {
		t.Error("expected positive total tax")
	}
	if breakdown.EffectiveRate <= 0 {
		t.Error("expected positive effective rate")
	}
}

// --- normalizeFilingStatus with unknown status ---

func TestNormalizeFilingStatus_InvalidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status models.FilingStatus
		want   models.FilingStatus
	}{
		{"empty string", "", models.FilingMarriedJoint},
		{"garbage", "garbage", models.FilingMarriedJoint},
		{"valid single", models.FilingSingle, models.FilingSingle},
		{"valid MFJ", models.FilingMarriedJoint, models.FilingMarriedJoint},
		{"valid MFS", models.FilingMarriedSeparate, models.FilingMarriedSeparate},
		{"valid HOH", models.FilingHeadOfHousehold, models.FilingHeadOfHousehold},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeFilingStatus(tt.status)
			if got != tt.want {
				t.Errorf("normalizeFilingStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// --- inflationFactor with yearsFromBase = 0 and negative ---

func TestInflationFactor_EdgeCases(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 3.0)

	tests := []struct {
		name          string
		yearsFromBase int
		want          float64
	}{
		{"zero years", 0, 1.0},
		{"negative years", -5, 1.0},
		{"negative one year", -1, 1.0},
		{"positive years", 5, math.Pow(1.03, 5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tc.inflationFactor(tt.yearsFromBase)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("inflationFactor(%d) = %f, want %f", tt.yearsFromBase, got, tt.want)
			}
		})
	}
}

// --- medicareEligibleAdultCountAtYear ---

func TestMedicareEligibleAdultCountAtYear(t *testing.T) {
	tests := []struct {
		name      string
		settings  *models.WhatIfSettings
		year      int
		wantCount int
	}{
		{
			name:      "nil settings",
			settings:  nil,
			year:      0,
			wantCount: 0,
		},
		{
			name: "primary under 65 no spouse",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 60
				s.SpouseAge = 0
				return s
			}(),
			year:      0,
			wantCount: 0,
		},
		{
			name: "primary at 65 no spouse",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 60
				s.SpouseAge = 0
				return s
			}(),
			year:      5,
			wantCount: 1,
		},
		{
			name: "primary at 65 spouse under 65",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 60
				s.SpouseAge = 55
				return s
			}(),
			year:      5,
			wantCount: 1,
		},
		{
			name: "both at 65",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 60
				s.SpouseAge = 60
				return s
			}(),
			year:      5,
			wantCount: 2,
		},
		{
			name: "primary over 65 spouse at 65",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 65
				s.SpouseAge = 60
				return s
			}(),
			year:      5,
			wantCount: 2,
		},
		{
			name: "both under 65",
			settings: func() *models.WhatIfSettings {
				s := models.DefaultWhatIfSettings()
				s.CurrentAge = 50
				s.SpouseAge = 48
				return s
			}(),
			year:      0,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := medicareEligibleAdultCountAtYear(tt.settings, tt.year)
			if got != tt.wantCount {
				t.Errorf("medicareEligibleAdultCountAtYear() = %d, want %d", got, tt.wantCount)
			}
		})
	}
}

// --- annualizedInputs edge cases ---

func TestAnnualizedInputs_MonthZero(t *testing.T) {
	acc := projectionTaxAccumulator{}
	inputs := acc.annualizedInputs(0, 1000, 200, 500, 100, 50, 25, 0)

	// monthInYear=0 => monthsElapsed=1, annualizationFactor=12
	if math.Abs(inputs.OrdinaryIncome-12000) > 0.01 {
		t.Errorf("expected OrdinaryIncome=12000, got %f", inputs.OrdinaryIncome)
	}
	if math.Abs(inputs.SocialSecurityIncome-2400) > 0.01 {
		t.Errorf("expected SocialSecurityIncome=2400, got %f", inputs.SocialSecurityIncome)
	}
	if math.Abs(inputs.QualifiedDividends-1200) > 0.01 {
		t.Errorf("expected QualifiedDividends=1200, got %f", inputs.QualifiedDividends)
	}
}

func TestAnnualizedInputs_MonthEleven(t *testing.T) {
	acc := projectionTaxAccumulator{
		OrdinaryIncomeYTD: 11000,
	}
	inputs := acc.annualizedInputs(11, 1000, 0, 0, 0, 0, 0, 0)

	// monthInYear=11 => monthsElapsed=12, annualizationFactor=1
	if math.Abs(inputs.OrdinaryIncome-12000) > 0.01 {
		t.Errorf("expected OrdinaryIncome=12000, got %f", inputs.OrdinaryIncome)
	}
}

func TestAnnualizedInputs_NegativeMonthClampedToOne(t *testing.T) {
	// monthInYear that would make monthsElapsed <= 0 is clamped to 1
	acc := projectionTaxAccumulator{}
	inputs := acc.annualizedInputs(-2, 1000, 0, 0, 0, 0, 0, 0)

	// monthsElapsed = -2+1 = -1, clamped to 1, annualizationFactor=12
	if math.Abs(inputs.OrdinaryIncome-12000) > 0.01 {
		t.Errorf("expected OrdinaryIncome=12000 with negative month, got %f", inputs.OrdinaryIncome)
	}
}

func TestAnnualizedInputs_RothConversionsNotAnnualized(t *testing.T) {
	acc := projectionTaxAccumulator{
		RothConversionsYTD: 5000,
	}
	// Roth conversions should NOT be annualized - they're a one-time event
	inputs := acc.annualizedInputs(0, 0, 0, 0, 0, 0, 0, 10000)

	// RothConversions = YTD + current = 5000 + 10000 = 15000 (no annualization factor)
	if math.Abs(inputs.RothConversions-15000) > 0.01 {
		t.Errorf("expected RothConversions=15000 (not annualized), got %f", inputs.RothConversions)
	}
}

// --- buildProjectionExplainability with data but no YearlySummaries ---

func TestBuildProjectionExplainability_WithMonthsNoYearlySummaries(t *testing.T) {
	s := defaultSettingsForTest()
	s.ProjectionYears = 2
	c := newTestCalc(t, s)

	// Create a projection with months spanning 2 years but no yearly summaries
	months := make([]models.ProjectionMonth, 24)
	for i := range months {
		months[i] = models.ProjectionMonth{
			Month:                i,
			Year:                 float64(i) / 12.0,
			PortfolioBalance:     1000000 - float64(i)*1000,
			PortfolioBalanceReal: 900000 - float64(i)*900,
			CumulativeInflation:  1 + float64(i)*0.002,
			PortfolioGrowth:      500,
			GrossIncome:          5000,
			TaxesPaid:            1000,
			TotalExpenses:        3000,
			NetWithdrawal:        -1000,
		}
	}

	projection := &models.ProjectionResult{
		Months:          months,
		YearlySummaries: nil, // Force the build-from-months path
	}

	result := c.buildProjectionExplainability(projection)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.YearlySummaries) == 0 {
		t.Error("expected yearly summaries to be built from months")
	}
	if result.TotalTaxes <= 0 {
		t.Error("expected positive total taxes")
	}
	if result.TotalGrossIncome <= 0 {
		t.Error("expected positive total gross income")
	}
	if result.TaxShareOfGrossCashFlow <= 0 {
		t.Error("expected positive tax share")
	}
}

func TestBuildProjectionExplainability_WithYearlySummaries(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	months := []models.ProjectionMonth{
		{
			Month:                0,
			PortfolioBalance:     900000,
			PortfolioBalanceReal: 850000,
			CumulativeInflation:  1.03,
		},
	}

	summaries := []models.ProjectionYearSummary{
		{Year: 0, GrossIncome: 60000, Taxes: 10000, StartingBalance: 1000000, EndingBalance: 900000},
	}

	projection := &models.ProjectionResult{
		Months:          months,
		YearlySummaries: summaries,
	}

	result := c.buildProjectionExplainability(projection)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TotalTaxes != 10000 {
		t.Errorf("expected TotalTaxes=10000, got %f", result.TotalTaxes)
	}
	if result.TotalGrossIncome != 60000 {
		t.Errorf("expected TotalGrossIncome=60000, got %f", result.TotalGrossIncome)
	}
}

func TestBuildProjectionExplainability_ZeroGrossIncome(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	months := []models.ProjectionMonth{
		{
			Month:                0,
			PortfolioBalance:     1000000,
			PortfolioBalanceReal: 1000000,
			CumulativeInflation:  1.0,
		},
	}

	summaries := []models.ProjectionYearSummary{
		{Year: 0, GrossIncome: 0, Taxes: 0},
	}

	projection := &models.ProjectionResult{
		Months:          months,
		YearlySummaries: summaries,
	}

	result := c.buildProjectionExplainability(projection)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TaxShareOfGrossCashFlow != 0 {
		t.Errorf("expected 0 tax share with 0 gross income, got %f", result.TaxShareOfGrossCashFlow)
	}
}

func TestBuildProjectionExplainability_ZeroPortfolioBalance(t *testing.T) {
	s := defaultSettingsForTest()
	c := newTestCalc(t, s)

	months := []models.ProjectionMonth{
		{
			Month:                0,
			PortfolioBalance:     0,
			PortfolioBalanceReal: 0,
			CumulativeInflation:  1.03,
		},
	}

	summaries := []models.ProjectionYearSummary{
		{Year: 0, GrossIncome: 1000, Taxes: 100},
	}

	projection := &models.ProjectionResult{
		Months:          months,
		YearlySummaries: summaries,
	}

	result := c.buildProjectionExplainability(projection)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.InflationLossPercent != 0 {
		t.Errorf("expected 0 inflation loss with 0 balance, got %f", result.InflationLossPercent)
	}
}

func TestBuildProjectionExplainability_MultiYearNoSummaries(t *testing.T) {
	s := defaultSettingsForTest()
	s.PortfolioValue = 500000
	c := newTestCalc(t, s)

	// 3 years of data (36 months), no pre-built summaries
	months := make([]models.ProjectionMonth, 36)
	for i := range months {
		months[i] = models.ProjectionMonth{
			Month:                i,
			Year:                 float64(i) / 12.0,
			PortfolioBalance:     500000 + float64(i)*100,
			PortfolioBalanceReal: 490000 + float64(i)*80,
			CumulativeInflation:  1 + float64(i)*0.002,
			PortfolioGrowth:      200,
			GrossIncome:          4000,
			TaxesPaid:            800,
			TotalExpenses:        2500,
			NetWithdrawal:        500,
		}
	}

	projection := &models.ProjectionResult{
		Months:          months,
		YearlySummaries: nil,
	}

	result := c.buildProjectionExplainability(projection)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should have 3 yearly summaries
	if len(result.YearlySummaries) != 3 {
		t.Errorf("expected 3 yearly summaries, got %d", len(result.YearlySummaries))
	}
}

// --- NIIT edge cases ---

func TestCalculateNIIT_ZeroMAGI(t *testing.T) {
	got := CalculateNIIT(0, 50000, models.FilingSingle)
	if got != 0 {
		t.Errorf("expected 0 for zero MAGI, got %f", got)
	}
}

func TestCalculateNIIT_NegativeMAGI(t *testing.T) {
	got := CalculateNIIT(-50000, 50000, models.FilingSingle)
	if got != 0 {
		t.Errorf("expected 0 for negative MAGI, got %f", got)
	}
}

func TestCalculateNIIT_ZeroNetInvestmentIncome(t *testing.T) {
	got := CalculateNIIT(300000, 0, models.FilingSingle)
	if got != 0 {
		t.Errorf("expected 0 for zero net investment income, got %f", got)
	}
}

func TestCalculateNIIT_HeadOfHousehold(t *testing.T) {
	// HOH threshold is 200K, same as single
	got := CalculateNIIT(250000, 100000, models.FilingHeadOfHousehold)
	// excess MAGI = 50000, min(100000, 50000) = 50000, NIIT = 50000 * 0.038 = 1900
	if math.Abs(got-1900) > 0.01 {
		t.Errorf("expected NIIT=1900, got %f", got)
	}
}

// --- GetMarginalRate edge cases ---

func TestGetMarginalRate_NegativeIncome(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 0)

	rate := tc.GetMarginalRate(-5000, 0)
	if rate != 10 {
		t.Errorf("expected 10 for negative income, got %f", rate)
	}
}

func TestGetMarginalRate_WithInflation(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 3.0)

	// Same nominal income should have lower marginal rate with inflation-adjusted brackets
	rateBase := tc.GetMarginalRate(100000, 0)
	rateFuture := tc.GetMarginalRate(100000, 20)

	if rateFuture > rateBase {
		t.Errorf("expected lower or equal marginal rate with inflation-adjusted brackets: base=%f future=%f", rateBase, rateFuture)
	}
}

// --- estimateMonthlySnapshot with IRMAA lookback ---

func TestEstimateMonthlySnapshot_IRMAALookback(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	acc := projectionTaxAccumulator{}

	// Provide completed MAGI history with 2+ years so lookback applies
	completedMAGI := []float64{200000, 300000} // 2 years ago: 200K

	snapshot := acc.estimateMonthlySnapshot(
		tc, 0, 0,
		5000, // ordinaryIncome
		2000, // socialSecurityIncome
		0,    // taxableWithdrawals
		500,  // qualifiedDividends
		300,  // longTermCapitalGains
		0,    // nonQualifiedDividends
		0,    // rothConversions
		completedMAGI,
		nil,
		1,   // irmaaEligibleAdults
		1.0, // irmaaInflationFactor
	)

	// IRMAA should be based on lookback MAGI (200K, 2 years ago), not current year
	if snapshot.AnnualIRMAA <= 0 {
		t.Error("expected positive IRMAA with high lookback MAGI and eligible adult")
	}
	if snapshot.MonthlyIRMAA <= 0 {
		t.Error("expected positive monthly IRMAA")
	}
}

func TestEstimateMonthlySnapshot_NoIRMAAEligibleAdults(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	acc := projectionTaxAccumulator{}

	snapshot := acc.estimateMonthlySnapshot(
		tc, 0, 0,
		5000, 0, 0, 0, 0, 0, 0,
		nil,
		nil,
		0, // no eligible adults
		1.0,
	)

	if snapshot.AnnualIRMAA != 0 {
		t.Errorf("expected 0 IRMAA with no eligible adults, got %f", snapshot.AnnualIRMAA)
	}
}

func TestEstimateMonthlySnapshot_TwoIRMAAEligibleAdults(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: 0,
	}, 0)

	acc := projectionTaxAccumulator{}
	assumedLookbackMAGI := 300000.0

	snapshot1 := acc.estimateMonthlySnapshot(
		tc, 0, 0,
		20000, 0, 0, 0, 0, 0, 0,
		nil,
		&assumedLookbackMAGI,
		1, // 1 eligible adult
		1.0,
	)

	snapshot2 := acc.estimateMonthlySnapshot(
		tc, 0, 0,
		20000, 0, 0, 0, 0, 0, 0,
		nil,
		&assumedLookbackMAGI,
		2, // 2 eligible adults
		1.0,
	)

	// With 2 eligible adults, IRMAA should be double
	if snapshot2.AnnualIRMAA != 2*snapshot1.AnnualIRMAA {
		t.Errorf("expected IRMAA to double with 2 adults: 1adult=%f, 2adults=%f", snapshot1.AnnualIRMAA, snapshot2.AnnualIRMAA)
	}
}

func TestEstimateMonthlySnapshot_SocialSecurityTaxablePct(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	acc := projectionTaxAccumulator{}

	snapshot := acc.estimateMonthlySnapshot(
		tc, 0, 0,
		5000, // ordinaryIncome
		2000, // socialSecurityIncome
		0, 0, 0, 0, 0,
		nil, nil, 0, 1.0,
	)

	// With enough income, SS should be partially taxable
	if snapshot.AnnualTaxableSocialSecurity < 0 {
		t.Error("taxable SS should be non-negative")
	}
	if snapshot.TaxableSocialSecurityPct < 0 || snapshot.TaxableSocialSecurityPct > 100 {
		t.Errorf("taxable SS pct should be 0-100, got %f", snapshot.TaxableSocialSecurityPct)
	}
}

// --- estimateMonthlySnapshot: remainingMonths <= 0 branch ---

func TestEstimateMonthlySnapshot_MonthInYearTwelve(t *testing.T) {
	// monthInYear=12 makes remainingMonths = 12-12 = 0, which is clamped to 1
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	acc := projectionTaxAccumulator{}
	snapshot := acc.estimateMonthlySnapshot(
		tc, 0, 12,
		5000, 0, 0, 0, 0, 0, 0,
		nil, nil, 0, 1.0,
	)

	if snapshot.MonthlyTax < 0 {
		t.Errorf("expected non-negative monthly tax, got %f", snapshot.MonthlyTax)
	}
}

// --- plannerInflationFactorForYear ---

func TestPlannerInflationFactorForYear(t *testing.T) {
	tests := []struct {
		name string
		rate float64
		yrs  float64
		want float64
	}{
		{"zero years", 3.0, 0, 1.0},
		{"negative years", 3.0, -5, 1.0},
		{"positive years", 3.0, 10, math.Pow(1.03, 10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plannerInflationFactorForYear(tt.rate, tt.yrs)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("plannerInflationFactorForYear(%f, %f) = %f, want %f", tt.rate, tt.yrs, got, tt.want)
			}
		})
	}
}

// --- RunProjection: depletion path (shortfallCausesDepletion) ---

func TestRunProjection_Depletion(t *testing.T) {
	s := defaultSettingsForTest()
	s.PortfolioValue = 50000        // Very small portfolio
	s.MonthlyLivingExpenses = 10000 // Very high expenses
	s.ProjectionYears = 10
	s.InvestmentReturn = 2.0
	s.TaxDeferredPercent = 0
	s.RothPercent = 0

	c := newTestCalc(t, s)
	result := c.RunProjection()
	if result == nil {
		t.Fatal("expected non-nil projection")
	}
	if result.Survives {
		t.Error("expected portfolio to deplete with very high expenses")
	}
	if result.DepletionMonth == nil {
		t.Error("expected depletion month to be set")
	}
	if result.LongevityYears == nil {
		t.Error("expected longevity years to be set")
	}
}

// --- SS taxation via TaxCalculator method (coverage for the method wrapper) ---

func TestTaxCalculator_CalculateTaxableSocialSecurity(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 0)

	got := tc.CalculateTaxableSocialSecurity(20000, 30000, 5000, 3000)
	if got <= 0 {
		t.Errorf("expected positive taxable SS via TaxCalculator method, got %f", got)
	}
}

func TestTaxCalculator_CalculateNIIT(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 0)

	got := tc.CalculateNIIT(250000, 50000)
	if got <= 0 {
		t.Errorf("expected positive NIIT via TaxCalculator method, got %f", got)
	}
}

func TestTaxCalculator_CalculateMonthlyIRMAA(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 0)

	got := tc.CalculateMonthlyIRMAA(150000, 1.0)
	if got <= 0 {
		t.Errorf("expected positive IRMAA via TaxCalculator method, got %f", got)
	}
}

// --- SS with married separate always 85% ---

func TestCalculateTaxableSocialSecurity_MarriedSeparateAlways85Pct(t *testing.T) {
	// MFS-lived-with-spouse: $0/$0 thresholds → 85% cap applies immediately
	// (per 26 USC § 86(c)(2)(B), F-018). mfsLivedWithSpouse=true.
	tests := []float64{1000, 10000, 50000, 100000}
	for _, ss := range tests {
		got := CalculateTaxableSocialSecurity(ss, 0, 0, 0, models.FilingMarriedSeparate, true)
		want := ss * 0.85
		if math.Abs(got-want) > 0.01 {
			t.Errorf("SS=%f: expected %f (85%%), got %f", ss, want, got)
		}
	}
}

// --- SS with negative other income ---

func TestCalculateTaxableSocialSecurity_NegativeOtherIncome(t *testing.T) {
	// Negative otherIncome is clamped to 0 via math.Max
	got := CalculateTaxableSocialSecurity(20000, -50000, 0, 0, models.FilingSingle, false)
	// PI = max(0, -50000) + 0 + 0 + 0.5*20000 = 10000, below 25K threshold
	if got != 0 {
		t.Errorf("expected 0 with negative other income, got %f", got)
	}
}

// --- SS: verify cap at 85% of benefits ---

func TestCalculateTaxableSocialSecurity_CappedAt85Percent(t *testing.T) {
	// Very high income should cap taxable SS at 85% of benefits
	ssBenefits := 30000.0
	got := CalculateTaxableSocialSecurity(ssBenefits, 500000, 0, 0, models.FilingSingle, false)
	maxTaxable := ssBenefits * 0.85
	if got > maxTaxable+0.01 {
		t.Errorf("taxable SS should be capped at 85%%: got=%f, max=%f", got, maxTaxable)
	}
	if math.Abs(got-maxTaxable) > 0.01 {
		t.Errorf("with very high income, taxable SS should be exactly 85%%: got=%f, want=%f", got, maxTaxable)
	}
}
