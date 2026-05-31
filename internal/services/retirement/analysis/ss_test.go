package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func ssWithinTolerance(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestRunSSAnalysis(t *testing.T) {
	t.Run("nil SS config returns nil", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		in := engineInput(t, s)
		if result := SSAnalysis(in); result != nil {
			t.Fatal("expected nil for nil SS config")
		}
	})

	t.Run("zero FRA benefit returns nil", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 0}
		in := engineInput(t, s)
		if result := SSAnalysis(in); result != nil {
			t.Fatal("expected nil for zero FRA benefit")
		}
	})

	t.Run("already claiming back-derives PIA", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 65
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 1500, // actual benefit being received
			FRA:        67,
			COLARate:   0.02,
			ClaimAge:   63,
		}
		in := engineInput(t, s)
		result := SSAnalysis(in)
		if result == nil {
			t.Fatal("expected analysis")
		}
		// Should use derived PIA, not entered benefit, for comparison
		if result.BestAge != 63 {
			t.Fatalf("already claiming: BestAge = %d, want 63 (current claim)", result.BestAge)
		}
	})

	t.Run("spouse has higher PIA uses spousal top-up for primary", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 60
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       1000,
			FRA:              67,
			COLARate:         0.02,
			ClaimAge:         67,
			SpouseFRABenefit: 3000, // higher than primary
			SpouseFRA:        67,
			SpouseClaimAge:   67,
		}
		in := engineInput(t, s)
		result := SSAnalysis(in)
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.Options) == 0 {
			t.Fatal("expected primary options with spousal top-up")
		}
		if len(result.SpouseOptions) == 0 {
			t.Fatal("expected spouse options")
		}
	})

	t.Run("spouse already claiming uses their claim age", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 65
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 65)},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       3000,
			FRA:              67,
			COLARate:         0.02,
			SpouseFRABenefit: 1000,
			SpouseFRA:        67,
			SpouseClaimAge:   63, // <= SpouseAge, already claiming
		}
		in := engineInput(t, s)
		result := SSAnalysis(in)
		if result == nil {
			t.Fatal("expected analysis")
		}
		if result.SpouseBestAge != 63 {
			t.Fatalf("SpouseBestAge = %d, want 63 (already claiming)", result.SpouseBestAge)
		}
	})

	t.Run("spouse age zero falls back to current age", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 0
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       3000,
			FRA:              67,
			COLARate:         0.02,
			SpouseFRABenefit: 1000,
			SpouseFRA:        67,
		}
		in := engineInput(t, s)
		result := SSAnalysis(in)
		if result == nil {
			t.Fatal("expected analysis")
		}
		// With spouseAge=0 falling back to currentAge=60, spouse options
		// should include ages 60-70
		if len(result.SpouseOptions) == 0 {
			t.Fatal("expected spouse options with fallback age")
		}
	})
}

func TestRunSSPortfolioAnalysis(t *testing.T) {
	base := func() *models.WhatIfSettings {
		s := models.DefaultWhatIfSettings()
		s.StartDate = "2026-04"
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.TaxDeferredPercent = 60
		s.RothPercent = 10
		s.ProjectionYears = 15
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			SpouseFRABenefit: 154,
			SpouseFRA:        67,
		}
		return s
	}

	t.Run("not eligible returns nil", func(t *testing.T) {
		in := engineInput(t, base())
		ssAnalysis := SSAnalysis(in)
		if result := SSPortfolio(engine.New(), in, ssAnalysis); result != nil {
			t.Fatal("expected nil when no claim ages are selected")
		}
	})

	t.Run("nil ssAnalysis returns nil", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68
		in := engineInput(t, s)
		if result := SSPortfolio(engine.New(), in, nil); result != nil {
			t.Fatal("expected nil for nil ssAnalysis")
		}
	})

	t.Run("spouse only varies spouse ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		in := engineInput(t, s)
		result := SSPortfolio(engine.New(), in, SSAnalysis(in))
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.PrimaryOptions) != 0 {
			t.Fatalf("expected no primary options, got %d", len(result.PrimaryOptions))
		}
		if len(result.SpouseOptions) != 9 {
			t.Fatalf("expected 9 spouse options, got %d", len(result.SpouseOptions))
		}
		if result.MonteCarloRuns != ssPortfolioMonteCarloRuns {
			t.Fatalf("MonteCarloRuns = %d, want %d", result.MonteCarloRuns, ssPortfolioMonteCarloRuns)
		}
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge < 62 || opt.ClaimAge > 70 {
				t.Fatalf("unexpected spouse claim age %d", opt.ClaimAge)
			}
			if opt.SurvivalRate < 0 || opt.SurvivalRate > 100 {
				t.Fatalf("spouse survival rate out of range: %.2f", opt.SurvivalRate)
			}
		}
		if result.OptimalSpouseAge < 62 || result.OptimalSpouseAge > 70 {
			t.Fatalf("optimal spouse age %d out of range", result.OptimalSpouseAge)
		}
	})

	t.Run("primary only varies primary ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68 // must be > CurrentAge (67)
		in := engineInput(t, s)
		result := SSPortfolio(engine.New(), in, SSAnalysis(in))
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.SpouseOptions) != 0 {
			t.Fatalf("expected no spouse options, got %d", len(result.SpouseOptions))
		}
		if len(result.PrimaryOptions) != 4 {
			t.Fatalf("expected 4 primary options, got %d", len(result.PrimaryOptions))
		}
		for _, opt := range result.PrimaryOptions {
			if opt.ClaimAge < 67 {
				t.Fatalf("unexpected primary claim age %d", opt.ClaimAge)
			}
		}
	})

	t.Run("both selected returns both tables", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68 // must be > CurrentAge (67)
		s.SocialSecurity.SpouseClaimAge = 62
		in := engineInput(t, s)
		result := SSPortfolio(engine.New(), in, SSAnalysis(in))
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.PrimaryOptions) == 0 || len(result.SpouseOptions) == 0 {
			t.Fatalf("expected both primary and spouse options, got %d and %d", len(result.PrimaryOptions), len(result.SpouseOptions))
		}
		if result.BaselineSurvivalRate < 0 || result.BaselineSurvivalRate > 100 {
			t.Fatalf("baseline survival rate out of range: %.2f", result.BaselineSurvivalRate)
		}
	})

	t.Run("baseline delta is zero for selected age", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		in := engineInput(t, s)
		result := SSPortfolio(engine.New(), in, SSAnalysis(in))
		if result == nil {
			t.Fatal("expected analysis")
		}
		found := false
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge == 62 {
				found = true
				if !ssWithinTolerance(opt.DeltaSurvivalRate, 0, 0.0001) {
					t.Fatalf("baseline delta = %.6f, want 0", opt.DeltaSurvivalRate)
				}
			}
		}
		if !found {
			t.Fatal("selected spouse age 62 not found in options")
		}
	})

	t.Run("monthly benefits match SS comparison table", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		in := engineInput(t, s)
		ssAnalysis := SSAnalysis(in)
		if ssAnalysis == nil {
			t.Fatal("expected SS analysis")
		}
		result := SSPortfolio(engine.New(), in, ssAnalysis)
		if result == nil {
			t.Fatal("expected analysis")
		}

		expected := make(map[int]float64, len(ssAnalysis.SpouseOptions))
		for _, opt := range ssAnalysis.SpouseOptions {
			expected[opt.ClaimAge] = opt.MonthlyBenefit
		}
		for _, opt := range result.SpouseOptions {
			if !ssWithinTolerance(opt.MonthlyBenefit, expected[opt.ClaimAge], 0.01) {
				t.Fatalf("age %d monthly benefit = %.2f, want %.2f", opt.ClaimAge, opt.MonthlyBenefit, expected[opt.ClaimAge])
			}
		}
	})
}

func TestCloneSettingsWithClaimAges(t *testing.T) {
	t.Run("nil settings returns not-ok", func(t *testing.T) {
		_, ok := cloneSettingsWithClaimAges(nil, 67, 65)
		if ok {
			t.Fatal("expected ok=false for nil settings")
		}
	})

	t.Run("minimal settings without optional configs", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}
		// Ensure optional configs are nil
		s.SpendingPhaseConfig = nil
		s.TaxConfig = nil
		s.RothConversion = nil
		s.GlidePath = nil
		s.Guardrails = nil

		clone, ok := cloneSettingsWithClaimAges(s, 68, 65)
		if !ok {
			t.Fatal("expected ok=true")
		}
		cs := clone.Settings()
		if cs.SocialSecurity.ClaimAge != 68 {
			t.Fatalf("ClaimAge = %d, want 68", cs.SocialSecurity.ClaimAge)
		}
		if cs.SocialSecurity.SpouseClaimAge != 65 {
			t.Fatalf("SpouseClaimAge = %d, want 65", cs.SocialSecurity.SpouseClaimAge)
		}
	})

	t.Run("full settings deep-copies all sub-configs", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}
		s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Phases: []models.SpendingPhase{{Name: "go-go", Multiplier: 1.0}},
		}
		s.TaxConfig = &models.TaxConfig{FilingStatus: "married"}
		s.RothConversion = &models.RothConversionConfig{AnnualAmount: 50000}
		s.GlidePath = &models.GlidePathConfig{Enabled: true}
		s.Guardrails = &models.GuardrailConfig{Enabled: true}

		clone, ok := cloneSettingsWithClaimAges(s, 70, 62)
		if !ok {
			t.Fatal("expected ok=true")
		}

		// Verify deep copy via prepare.From — mutating clone must not
		// affect original.
		cs := clone.Settings()
		cs.SocialSecurity.FRABenefit = 9999
		if s.SocialSecurity.FRABenefit == 9999 {
			t.Fatal("clone shares SocialSecurity pointer with original")
		}
		cs.SpendingPhaseConfig.Phases[0].Name = "mutated"
		if s.SpendingPhaseConfig.Phases[0].Name == "mutated" {
			t.Fatal("clone shares SpendingPhaseConfig.Phases with original")
		}
	})
}

func TestSurvivorBenefitForClaimAge(t *testing.T) {
	const pia = 2000.0
	const fra = 67

	// Claim at 62 with FRA 67: own benefit reduced 30% → $1400, which is
	// below the RIB-LIM survivor floor of 82.5%·PIA = $1650. Floor applies.
	if got := SurvivorBenefitForClaimAge(pia, fra, 62); !ssWithinTolerance(got, 0.825*pia, 0.01) {
		t.Errorf("age 62: got %.2f, want RIB-LIM floor %.2f", got, 0.825*pia)
	}

	// Claim at 64 (36 months early): reduced 20% → $1600, still below the
	// 82.5%·PIA = $1650 floor, so the floor applies here too (intermediate
	// floor-active case, independent numeric oracle).
	if got := SurvivorBenefitForClaimAge(pia, fra, 64); !ssWithinTolerance(got, 1650.0, 0.01) {
		t.Errorf("age 64: got %.2f, want RIB-LIM floor 1650.00", got)
	}

	// Claim at 66 (12 months early): reduced 6.667% → $1866.67, above the
	// floor, so the survivor inherits the (reduced) actual benefit.
	want66 := AdjustedSSBenefit(pia, fra, 66)
	if got := SurvivorBenefitForClaimAge(pia, fra, 66); !ssWithinTolerance(got, want66, 0.01) {
		t.Errorf("age 66 (above floor): got %.2f, want adjusted %.2f", got, want66)
	}

	// Claim at FRA: exactly PIA.
	if got := SurvivorBenefitForClaimAge(pia, fra, 67); !ssWithinTolerance(got, pia, 0.01) {
		t.Errorf("FRA: got %.2f, want %.2f", got, pia)
	}

	// Claim at 70: delayed-retirement credits 8%/yr × 3 = 24% → $2480,
	// which the survivor inherits.
	if got := SurvivorBenefitForClaimAge(pia, fra, 70); !ssWithinTolerance(got, pia*1.24, 0.01) {
		t.Errorf("age 70: got %.2f, want %.2f", got, pia*1.24)
	}
}

// SpouseEarlyClaimGapPct must be derived from the spouse's own claiming
// table. When the spouse is already claiming (so the spouse best-age loop
// is skipped) but the primary is not, the gap must not be computed against
// the primary table's best cumulative — the two share no state.
func TestSSAnalysis_SpouseEarlyClaimGap_SpouseAlreadyClaiming(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 65
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
		{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 65)},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       3000, // primary much higher → large primary cumulative
		FRA:              67,
		COLARate:         0.02,
		ClaimAge:         70, // primary NOT yet claiming (> CurrentAge): primary loop sets bestCum
		SpouseFRABenefit: 1000,
		SpouseFRA:        67,
		SpouseClaimAge:   63, // <= SpouseAge: spouse-already-claiming branch (best-age loop skipped)
	}
	result := SSAnalysis(engineInput(t, s))
	if result == nil {
		t.Fatal("expected analysis")
	}
	if len(result.SpouseOptions) < 2 {
		t.Fatalf("need >1 spouse option to exercise the gap, got %d", len(result.SpouseOptions))
	}

	// Oracle: the gap is a property of the spouse's own table alone.
	best := 0.0
	for _, opt := range result.SpouseOptions {
		if opt.CumulativeAt85 > best {
			best = opt.CumulativeAt85
		}
	}
	earliest := result.SpouseOptions[0].CumulativeAt85
	want := (best - earliest) / best * 100

	if !ssWithinTolerance(result.SpouseEarlyClaimGapPct, want, 1e-6) {
		t.Fatalf("SpouseEarlyClaimGapPct = %.6f, want %.6f (must derive from the spouse table, not the primary bestCum)",
			result.SpouseEarlyClaimGapPct, want)
	}
}

func ssSurvivorSettings(primaryFRA, spouseFRA float64) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 60
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
		{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: primaryFRA, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: spouseFRA, SpouseFRA: 67, SpouseClaimAge: 67,
		COLARate: 0.02, COLARateSet: true,
	}
	return s
}

func TestSSAnalysis_Survivor_PrimaryHigher(t *testing.T) {
	res := SSAnalysis(engineInput(t, ssSurvivorSettings(3000, 1500)))
	if res == nil {
		t.Fatal("nil analysis")
	}
	if !res.HasSurvivorAnalysis {
		t.Fatal("expected HasSurvivorAnalysis=true")
	}
	if res.SurvivorHigherEarnerIsSpouse {
		t.Error("primary is higher earner; SurvivorHigherEarnerIsSpouse should be false")
	}
	if len(res.Options) == 0 || res.Options[0].SurvivorMonthlyBenefit == 0 {
		t.Error("expected SurvivorMonthlyBenefit populated on primary Options")
	}
	for _, o := range res.SpouseOptions {
		if o.SurvivorMonthlyBenefit != 0 {
			t.Error("spouse options must not carry survivor benefit when primary is higher")
		}
	}
	if !res.HasSurvivorCallout || !res.HasSurvivorDelayUpside {
		t.Fatalf("expected callout with delay upside; callout=%v upside=%v", res.HasSurvivorCallout, res.HasSurvivorDelayUpside)
	}
	if res.SurvivorSelectedClaimAge != 67 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 67", res.SurvivorSelectedClaimAge)
	}
	wantAt70 := SurvivorBenefitForClaimAge(3000, 67, 70)
	if !ssWithinTolerance(res.SurvivorBenefitAt70, math.Round(wantAt70*100)/100, 0.01) {
		t.Errorf("SurvivorBenefitAt70 = %v, want %v", res.SurvivorBenefitAt70, wantAt70)
	}
	wantAtSelected := SurvivorBenefitForClaimAge(3000, 67, 67)
	if !ssWithinTolerance(res.SurvivorBenefitAtSelected, math.Round(wantAtSelected*100)/100, 0.01) {
		t.Errorf("SurvivorBenefitAtSelected = %v, want %v", res.SurvivorBenefitAtSelected, wantAtSelected)
	}
	if res.SurvivorDelayGainPct <= 0 {
		t.Errorf("expected positive SurvivorDelayGainPct, got %v", res.SurvivorDelayGainPct)
	}
}

func TestSSAnalysis_Survivor_SpouseHigher(t *testing.T) {
	res := SSAnalysis(engineInput(t, ssSurvivorSettings(1500, 3000)))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis")
	}
	if !res.SurvivorHigherEarnerIsSpouse {
		t.Error("spouse is higher earner; flag should be true")
	}
	if len(res.SpouseOptions) == 0 || res.SpouseOptions[0].SurvivorMonthlyBenefit == 0 {
		t.Error("expected SurvivorMonthlyBenefit populated on SpouseOptions")
	}
	for _, o := range res.Options {
		if o.SurvivorMonthlyBenefit != 0 {
			t.Error("primary options must not carry survivor benefit when spouse is higher")
		}
	}
	if !res.HasSurvivorCallout {
		t.Error("expected callout for spouse-higher path")
	}
	if res.SurvivorSelectedClaimAge != 67 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 67", res.SurvivorSelectedClaimAge)
	}
}

// The surviving spouse inherits the LARGER survivor benefit, which depends on
// each worker's actual claim age — not PIA. A higher-PIA worker who claims
// early is floored by RIB-LIM (82.5% of PIA), while a lower-PIA worker who
// delays to 70 earns delayed-retirement credits. When the lower-PIA record
// produces the larger survivor benefit, the survivor column/callout must
// attach to that record, not the higher-PIA one.
func TestSSAnalysis_Survivor_PicksRecordWithLargerSurvivorBenefit(t *testing.T) {
	s := ssSurvivorSettings(3000, 2200)  // primaryPIA 3000 > spousePIA 2200
	s.SocialSecurity.ClaimAge = 62       // primary claims early → survivor floored at 0.825×3000 = 2475
	s.SocialSecurity.SpouseClaimAge = 70 // spouse delays → survivor = 1.24×2200 = 2728

	res := SSAnalysis(engineInput(t, s))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis")
	}

	primarySurvivor := SurvivorBenefitForClaimAge(3000, 67, 62)
	spouseSurvivor := SurvivorBenefitForClaimAge(2200, 67, 70)
	if !(spouseSurvivor > primarySurvivor) {
		t.Fatalf("scenario invalid: spouseSurvivor %.2f must exceed primarySurvivor %.2f", spouseSurvivor, primarySurvivor)
	}

	if !res.SurvivorHigherEarnerIsSpouse {
		t.Errorf("spouse record yields the larger survivor benefit (%.2f > %.2f); SurvivorHigherEarnerIsSpouse should be true",
			spouseSurvivor, primarySurvivor)
	}
	if res.SurvivorSelectedClaimAge != 70 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 70 (spouse's selected age)", res.SurvivorSelectedClaimAge)
	}
	if len(res.SpouseOptions) == 0 || res.SpouseOptions[0].SurvivorMonthlyBenefit == 0 {
		t.Error("expected SurvivorMonthlyBenefit populated on SpouseOptions")
	}
	for _, o := range res.Options {
		if o.SurvivorMonthlyBenefit != 0 {
			t.Error("primary options must not carry survivor benefit when the spouse record is larger")
		}
	}
	wantAtSelected := math.Round(spouseSurvivor*100) / 100
	if !ssWithinTolerance(res.SurvivorBenefitAtSelected, wantAtSelected, 0.01) {
		t.Errorf("SurvivorBenefitAtSelected = %v, want %v (spouse @70)", res.SurvivorBenefitAtSelected, wantAtSelected)
	}
}

func TestSSAnalysis_Survivor_SingleFiler(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 0
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 60)},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67}
	res := SSAnalysis(engineInput(t, s))
	if res == nil {
		t.Fatal("nil analysis")
	}
	if res.HasSurvivorAnalysis {
		t.Error("single filer must not have survivor analysis")
	}
}

func TestSSAnalysis_Survivor_AnalysisOnlyClaimAge(t *testing.T) {
	s := ssSurvivorSettings(3000, 1500)
	s.SocialSecurity.ClaimAge = 0 // "Analysis only" — no selected age
	res := SSAnalysis(engineInput(t, s))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis (column still populated)")
	}
	if res.HasSurvivorCallout {
		t.Error("unset claim age must suppress the callout")
	}
	if res.SurvivorSelectedClaimAge != 0 {
		t.Errorf("SurvivorSelectedClaimAge = %d, want 0", res.SurvivorSelectedClaimAge)
	}
	if len(res.Options) == 0 || res.Options[0].SurvivorMonthlyBenefit == 0 {
		t.Error("survivor column should still be populated for the higher earner")
	}
}

func TestSSAnalysis_Survivor_HigherEarnerAlreadyClaiming(t *testing.T) {
	// Primary is the higher earner and is already claiming (claim age <=
	// current age), so the survivor benefit is locked: callout present,
	// SurvivorSelectedAgeLocked=true, no delay upside, gain 0.
	s := ssSurvivorSettings(3000, 1500)
	s.CurrentAge = 68
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 68)
	s.SocialSecurity.ClaimAge = 65 // already claiming (<= 68)

	res := SSAnalysis(engineInput(t, s))
	if res == nil || !res.HasSurvivorAnalysis {
		t.Fatal("expected survivor analysis")
	}
	if !res.HasSurvivorCallout {
		t.Error("expected callout for a valid (already-claimed) selected age")
	}
	if !res.SurvivorSelectedAgeLocked {
		t.Error("expected SurvivorSelectedAgeLocked=true for already-claiming higher earner")
	}
	if res.HasSurvivorDelayUpside {
		t.Error("locked higher earner must not show delay upside")
	}
	if res.SurvivorDelayGainPct != 0 {
		t.Errorf("locked: SurvivorDelayGainPct should be 0, got %v", res.SurvivorDelayGainPct)
	}
	if res.SurvivorBenefitAtSelected <= 0 {
		t.Error("expected positive SurvivorBenefitAtSelected")
	}
}

// F-029: When the primary is already claiming at a non-FRA age, the
// SpouseUsingSpousalBenefit flag must be derived from the primary PIA
// (back-derived from FRABenefit + claim age + FRA), not from the raw
// FRABenefit.
func TestRunSSAnalysis_F029_SpousalUsesPrimaryPIA(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.SpouseAge = 62
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       1000.0, // actual benefit at claim 62; PIA ≈ 1428.57
		FRA:              67,
		COLARate:         0.02,
		ClaimAge:         62,    // primary already claiming at 62
		SpouseFRABenefit: 600.0, // spouse own PIA; not yet claiming
		SpouseFRA:        67,
		// SpouseClaimAge intentionally zero — spouse not yet claiming
	}
	in := engineInput(t, s)
	result := SSAnalysis(in)
	if result == nil {
		t.Fatal("expected non-nil SS analysis")
	}
	if !result.SpouseUsingSpousalBenefit {
		t.Errorf("SpouseUsingSpousalBenefit = false; want true " +
			"(primaryPIA≈1428.57 × 0.5 ≈ 714 > SpouseFRABenefit 600). " +
			"Bug: ss.FRABenefit(1000) × 0.5 = 500 < 600 gives wrong false.")
	}
}
