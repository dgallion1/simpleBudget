package engine

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestLifetimeTieredMatch(t *testing.T) {
	tiers := []models.MatchTier{{FromPercent: 0, ToPercent: 3, MatchPercent: 100}, {FromPercent: 3, ToPercent: 5, MatchPercent: 50}}
	if got := MatchForCompensation(100000, 5000, tiers); got != 4000 {
		t.Fatalf("match = %v, want 4000", got)
	}
}

func TestLifetimeMatchUsesActualEmployeeContribution(t *testing.T) {
	tiers := []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}}
	for _, tc := range []struct {
		name           string
		employee, want float64
	}{
		{name: "above band", employee: 10000, want: 3000},
		{name: "inside band", employee: 2000, want: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchForCompensation(100000, tc.employee, tiers); got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}

func lifetimeContributionSettings() *models.WhatIfSettings {
	caps := models.PlanCapabilities{AllowCatchUp: true, AllowRothCatchUp: true, AllowEmployeeRoth: true, AllowEmployerRoth: true}
	return &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons:   []models.Person{{ID: "owner", BirthMonth: "1980-06"}},
		Lifetime: &models.LifetimeSettings{
			Version: 1,
			Accounts: []models.LifetimeAccount{
				{ID: "traditional", OwnerID: "owner", PlanID: "plan", EmployerID: "employer", LegalType: "401k", TaxTreatment: "traditional", Capabilities: caps},
				{ID: "roth", OwnerID: "owner", PlanID: "plan", EmployerID: "employer", LegalType: "401k", TaxTreatment: "roth", Capabilities: caps},
			},
			Jobs: []models.LifetimeJob{{ID: "job", OwnerID: "owner", EmployerID: "employer", GrossSalary: 100000, EligibleCompensation: 100000, StartMonth: "2026-01"}},
			ContributionRules: []models.ContributionRule{{
				ID: "rule", JobID: "job", StartMonth: "2026-01",
				Rate:                 models.ContributionRate{Mode: "percent", PercentOfCompensation: 10},
				TraditionalAccountID: "traditional", RothAccountID: "roth",
				Employer: &models.EmployerContribution{Mode: "match", DestinationAccountID: "traditional", MatchTiming: "monthly", Tiers: []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}}},
			}},
		},
	}
}

func runContributionYear(t *testing.T, settings *models.WhatIfSettings) (ContributionYearState, models.ContributionAmounts, float64) {
	t.Helper()
	var state ContributionYearState
	var totals models.ContributionAmounts
	var ordinary float64
	for month := 0; month < 12; month++ {
		result, err := CalculateContributions(settings, month, state)
		if err != nil {
			t.Fatalf("month %d: %v", month, err)
		}
		a := result.AmountsByRule["rule"]
		totals.Requested += a.Requested
		totals.Permitted += a.Permitted
		totals.Funded += a.Funded
		totals.Unfunded += a.Unfunded
		totals.EmployeeTraditional += a.EmployeeTraditional
		totals.EmployeeRoth += a.EmployeeRoth
		totals.EmployerTraditional += a.EmployerTraditional
		totals.EmployerRoth += a.EmployerRoth
		ordinary += result.OrdinaryIncomeAdjustment
		state = result.Next
	}
	return state, totals, ordinary
}

func closeEnough(got, want float64) bool { return math.Abs(got-want) < 1e-7 }

func TestLifetimeContributionExamples(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		rate, wantEmployee, wantEmployer float64
	}{
		{name: "ten percent", rate: 10, wantEmployee: 10000, wantEmployer: 3000},
		{name: "two percent", rate: 2, wantEmployee: 2000, wantEmployer: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeContributionSettings()
			s.Lifetime.ContributionRules[0].Rate.PercentOfCompensation = tc.rate
			_, got, _ := runContributionYear(t, s)
			if !closeEnough(got.Permitted, tc.wantEmployee) || !closeEnough(got.EmployerTraditional, tc.wantEmployer) {
				t.Fatalf("employee/employer = %v/%v, want %v/%v", got.Permitted, got.EmployerTraditional, tc.wantEmployee, tc.wantEmployer)
			}
		})
	}
}

func TestLifetimeContributionSplitsDestinationsAndWages(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].RothPercent = 40
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	a := result.AmountsByRule["rule"]
	if !closeEnough(a.EmployeeTraditional, 500) || !closeEnough(a.EmployeeRoth, 1000.0/3) || !closeEnough(a.EmployerTraditional, 250) || a.EmployerRoth != 0 {
		t.Fatalf("unexpected split: %+v", a)
	}
	if !closeEnough(result.PayrollWages, 100000.0/12) || !closeEnough(result.PayrollWagesByOwner["owner"], 100000.0/12) || !closeEnough(result.GrossPayByJob["job"], 100000.0/12) {
		t.Fatalf("wage tracing = %v %#v %#v", result.PayrollWages, result.PayrollWagesByOwner, result.GrossPayByJob)
	}
	if !closeEnough(result.OrdinaryIncomeAdjustment, -500) {
		t.Fatalf("ordinary adjustment = %v, want -500", result.OrdinaryIncomeAdjustment)
	}
	if len(result.Movements) != 3 || result.Movements[0].SourceID != "job" || result.Movements[2].SourceID != "employer" {
		t.Fatalf("movement sources = %+v", result.Movements)
	}
}

func TestLifetimeContributionsDoNotMutateOpeningState(t *testing.T) {
	s := lifetimeContributionSettings()
	opening := ContributionYearState{Initialized: true, Year: 2026, LastMonth: 1, EmployeeRegularByOwner: map[string]float64{"owner": 10}}
	want := cloneContributionYearState(opening)
	if _, err := CalculateContributions(s, 1, opening); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opening, want) {
		t.Fatalf("opening mutated: got %#v want %#v", opening, want)
	}
}

func TestLifetimeContributionRejectsNonSequentialAndUnknownTypes(t *testing.T) {
	s := lifetimeContributionSettings()
	_, err := CalculateContributions(s, 2, ContributionYearState{Initialized: true, Year: 2026, LastMonth: 1})
	if err == nil || !strings.Contains(err.Error(), "sequential") {
		t.Fatalf("nonsequential error = %v", err)
	}
	s.Lifetime.ContributionRules[0].Rate.Mode = "mystery"
	_, err = CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "unsupported contribution rate") {
		t.Fatalf("unknown rate error = %v", err)
	}
	s = lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].Employer.Mode = "mystery"
	_, err = CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "unsupported employer contribution") {
		t.Fatalf("unknown employer error = %v", err)
	}
}

func TestLifetimeFrontLoadedMatchTrueUp(t *testing.T) {
	for _, tc := range []struct {
		name   string
		trueUp bool
		want   float64
	}{{name: "monthly only", want: 500}, {name: "annual true-up", trueUp: true, want: 6000}} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeContributionSettings()
			j := &s.Lifetime.Jobs[0]
			j.GrossSalary = 120000
			j.EligibleCompensation = 120000
			r := &s.Lifetime.ContributionRules[0]
			r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 12000}
			r.Changes = []models.ContributionChange{{Month: "2026-02", Rate: models.ContributionRate{Mode: "fixed"}}}
			r.Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
			r.Employer.TrueUp = tc.trueUp
			_, got, _ := runContributionYear(t, s)
			if !closeEnough(got.Requested, 12000) || !closeEnough(got.Permitted, 10000) || !closeEnough(got.EmployerTraditional, tc.want) {
				t.Fatalf("requested/permitted/employer = %v/%v/%v, want 12000/10000/%v", got.Requested, got.Permitted, got.EmployerTraditional, tc.want)
			}
		})
	}
}

func TestLifetimeCapsActualEmployeeBeforeMatchAndSharesOwnerLimit(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 600000
	s.Lifetime.Jobs[0].EligibleCompensation = 600000
	s.Lifetime.ContributionRules[0].Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 30000}
	second := s.Lifetime.ContributionRules[0]
	second.ID = "rule2"
	second.TraditionalAccountID = "traditional"
	second.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 30000}
	second.Employer = nil
	s.Lifetime.ContributionRules = append(s.Lifetime.ContributionRules, second)
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.AmountsByRule["rule"].Permitted + result.AmountsByRule["rule2"].Permitted; got != 24500 {
		t.Fatalf("shared employee permitted = %v, want 24500", got)
	}
	if got := result.AmountsByRule["rule"].EmployerTraditional; got != 1500 {
		t.Fatalf("match after cap = %v, want 1500", got)
	}
}

func TestLifetimeAnnualAdditionsAndCompensationCaps(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 400000
	s.Lifetime.Jobs[0].EligibleCompensation = 400000
	s.Lifetime.ContributionRules[0].Rate.PercentOfCompensation = 5
	s.Lifetime.ContributionRules[0].Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
	_, got, _ := runContributionYear(t, s)
	if !closeEnough(got.Permitted, 20000) || !closeEnough(got.EmployerTraditional, 18000) {
		t.Fatalf("unrestricted employee/capped employer = %v/%v", got.Permitted, got.EmployerTraditional)
	}
	s = lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 400000
	s.Lifetime.Jobs[0].EligibleCompensation = 400000
	for i := range s.Lifetime.Accounts {
		s.Lifetime.Accounts[i].Capabilities.RestrictEmployeeCompensationToLimit = true
	}
	s.Lifetime.ContributionRules[0].Rate.PercentOfCompensation = 5
	s.Lifetime.ContributionRules[0].Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
	_, got, _ = runContributionYear(t, s)
	if !closeEnough(got.Permitted, 18000) || !closeEnough(got.EmployerTraditional, 18000) {
		t.Fatalf("restricted employee/employer = %v/%v", got.Permitted, got.EmployerTraditional)
	}
}

func TestLifetimeEmployerRothIsIncomeButNotPayrollWages(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].Employer.Mode = "fixed"
	s.Lifetime.ContributionRules[0].Employer.FixedMonthly = 1000
	s.Lifetime.ContributionRules[0].Employer.DestinationAccountID = "roth"
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	a := result.AmountsByRule["rule"]
	if a.EmployerRoth != 1000 || !closeEnough(result.OrdinaryIncomeAdjustment, 1000-a.EmployeeTraditional) || !closeEnough(result.PayrollWages, 100000.0/12) {
		t.Fatalf("amounts=%+v ordinary=%v wages=%v", a, result.OrdinaryIncomeAdjustment, result.PayrollWages)
	}
}

func TestLifetimeDepartureTrueUpIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		eligible bool
		want     float64
	}{{name: "ineligible", want: 500}, {name: "eligible", eligible: true, want: 3000}} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeContributionSettings()
			s.Lifetime.Jobs[0].GrossSalary = 120000
			s.Lifetime.Jobs[0].EligibleCompensation = 120000
			s.Lifetime.Jobs[0].EndMonth = "2026-07"
			r := &s.Lifetime.ContributionRules[0]
			r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 6000}
			r.Changes = []models.ContributionChange{{Month: "2026-02", Rate: models.ContributionRate{Mode: "fixed"}}}
			r.Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
			r.Employer.TrueUp = true
			r.Employer.DepartureTrueUp = tc.eligible
			var state ContributionYearState
			var employer float64
			for month := 0; month < 6; month++ {
				result, err := CalculateContributions(s, month, state)
				if err != nil {
					t.Fatal(err)
				}
				employer += result.AmountsByRule["rule"].EmployerTraditional
				state = result.Next
			}
			if !closeEnough(employer, tc.want) {
				t.Fatalf("employer = %v, want %v", employer, tc.want)
			}
		})
	}
}

func TestLifetimeMidyearYTDAndCalendarReset(t *testing.T) {
	s := lifetimeContributionSettings()
	s.StartDate = "2026-07"
	s.Lifetime.StartMonth = "2026-07"
	s.Lifetime.YTD = models.LifetimeYTD{Year: 2026, Rules: []models.ContributionYTD{{RuleID: "rule", EmployeeRegular: 24000, Employer: 1000, MatchingPaid: 1000}}, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 50000, EligiblePay: 50000}}}
	first, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(first.AmountsByRule["rule"].Permitted, 500) {
		t.Fatalf("midyear permitted = %v, want 500", first.AmountsByRule["rule"].Permitted)
	}
	state := first.Next
	for month := 1; month < 6; month++ {
		result, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		state = result.Next
	}
	january, err := CalculateContributions(s, 6, state)
	if err != nil {
		t.Fatal(err)
	}
	if january.Next.Year != 2027 || !closeEnough(january.AmountsByRule["rule"].Permitted, 100000.0/12*0.10) || january.Next.EmployeeRegularByOwner["owner"] >= 24500 {
		t.Fatalf("calendar reset result=%+v state=%+v", january.AmountsByRule["rule"], january.Next)
	}
}

func TestLifetimePriorSponsorWagesMustAgree(t *testing.T) {
	s := lifetimeContributionSettings()
	duplicate := s.Lifetime.Jobs[0]
	duplicate.ID = "job2"
	duplicate.PriorSponsorWages = 1
	s.Lifetime.Jobs[0].PriorSponsorWages = 2
	s.Lifetime.Jobs = append(s.Lifetime.Jobs, duplicate)
	_, err := CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "prior sponsor wages") {
		t.Fatalf("error = %v", err)
	}
}

func TestLifetimeRejectsNonfiniteInput(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = math.Inf(1)
	_, err := CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "finite") {
		t.Fatalf("error = %v", err)
	}
}

func TestLifetimeCatchUpAgeAndRothRequirement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		birth     string
		prior     float64
		allowRoth bool
		want      float64
		wantRoth  float64
	}{
		{name: "age 49", birth: "1977-01", want: 24500},
		{name: "age 50", birth: "1976-12", want: 32500},
		{name: "age 60", birth: "1966-12", want: 35750},
		{name: "strict threshold remains traditional", birth: "1971-01", prior: 150000, want: 32500},
		{name: "above threshold forced Roth", birth: "1971-01", prior: 150000.01, allowRoth: true, want: 32500, wantRoth: 8000},
		{name: "above threshold without Roth capability", birth: "1971-01", prior: 160000, want: 24500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeContributionSettings()
			s.Persons[0].BirthMonth = tc.birth
			s.Lifetime.Jobs[0].GrossSalary = 500000
			s.Lifetime.Jobs[0].EligibleCompensation = 500000
			s.Lifetime.Jobs[0].PriorSponsorWages = tc.prior
			s.Lifetime.ContributionRules[0].Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 40000}
			s.Lifetime.ContributionRules[0].Employer = nil
			if tc.prior > 150000 && !tc.allowRoth {
				for i := range s.Lifetime.Accounts {
					s.Lifetime.Accounts[i].Capabilities.AllowEmployeeRoth = false
					s.Lifetime.Accounts[i].Capabilities.AllowRothCatchUp = false
				}
			}
			result, err := CalculateContributions(s, 0, ContributionYearState{})
			if err != nil {
				t.Fatal(err)
			}
			a := result.AmountsByRule["rule"]
			if !closeEnough(a.Permitted, tc.want) || !closeEnough(a.EmployeeRoth, tc.wantRoth) {
				t.Fatalf("amounts=%+v, want permitted %v Roth %v", a, tc.want, tc.wantRoth)
			}
		})
	}
}

func TestLifetimeAnnualAdditionsExcludeCatchUpButCapEmployer(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Persons[0].BirthMonth = "1971-01"
	s.Lifetime.Jobs[0].GrossSalary = 600000
	s.Lifetime.Jobs[0].EligibleCompensation = 600000
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed"}
	r.Employer.Mode = "fixed"
	r.Employer.FixedMonthly = 47501
	opening := ContributionYearState{Initialized: true, Year: 2026, LastMonth: 11,
		EmployeeRegularByOwner: map[string]float64{"owner": 24500}, EmployeeCatchUpByOwner: map[string]float64{"owner": 8000},
		AdditionsByPlan: map[string]float64{"plan": 24500}, EligibleCompensationByPlan: map[string]float64{"plan": 330000}, EligibleCompensationByJob: map[string]float64{"job": 330000},
		GrossCompensationByPlan: map[string]float64{"plan": 330000}, GrossCompensationByJob: map[string]float64{"job": 330000}, MatchingPaidByRule: map[string]float64{}, EmployeeByRule: map[string]float64{"rule": 32500}, SponsorWages: map[SponsorKey]float64{}, PriorSponsorWages: map[SponsorKey]float64{},
	}
	result, err := CalculateContributions(s, 11, opening)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.AmountsByRule["rule"].EmployerTraditional; got != 47500 {
		t.Fatalf("employer = %v, want 47500", got)
	}
	if result.Next.AdditionsByPlan["plan"] != 72000 || result.Next.EmployeeCatchUpByOwner["owner"] != 8000 {
		t.Fatalf("state = %+v", result.Next)
	}
}

func TestLifetimeStableRuleOrderSharesOwnerCapAcrossJobs(t *testing.T) {
	s := lifetimeContributionSettings()
	secondJob := s.Lifetime.Jobs[0]
	secondJob.ID = "job2"
	secondJob.EmployerID = "employer2"
	s.Lifetime.Jobs = append(s.Lifetime.Jobs, secondJob)
	secondAccount := s.Lifetime.Accounts[0]
	secondAccount.ID = "traditional2"
	secondAccount.PlanID = "plan2"
	secondAccount.EmployerID = "employer2"
	s.Lifetime.Accounts = append(s.Lifetime.Accounts, secondAccount)
	first := &s.Lifetime.ContributionRules[0]
	first.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 20000}
	first.Employer = nil
	secondRule := *first
	secondRule.ID = "rule2"
	secondRule.JobID = "job2"
	secondRule.TraditionalAccountID = "traditional2"
	s.Lifetime.ContributionRules = append(s.Lifetime.ContributionRules, secondRule)
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AmountsByRule["rule"].Permitted != 100000.0/12 || !closeEnough(result.AmountsByRule["rule2"].Permitted, 100000.0/12) {
		t.Fatalf("monthly payroll caps = %#v", result.AmountsByRule)
	}
	state := result.Next
	for month := 1; month < 2; month++ {
		result, err = CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		state = result.Next
	}
	if !closeEnough(state.EmployeeRegularByOwner["owner"], 24500) || result.AmountsByRule["rule"].Permitted <= 0 || result.AmountsByRule["rule2"].Permitted != 0 {
		t.Fatalf("stable cap priority result=%#v state=%#v", result.AmountsByRule, state)
	}
}

func TestLifetimePartialEmploymentUsesCanonicalRetirementMonth(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Persons[0].RetirementMonth = "2026-03"
	s.Lifetime.Jobs[0].EndAtRetirement = true
	s.Lifetime.ContributionRules[0].EndAtRetirement = true
	var state ContributionYearState
	for month := 0; month < 3; month++ {
		result, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		if month < 2 && len(result.AmountsByRule) != 1 {
			t.Fatalf("month %d should contribute", month)
		}
		if month == 2 && (len(result.AmountsByRule) != 0 || result.PayrollWages != 0) {
			t.Fatalf("retirement month must be exclusive: %+v", result)
		}
		state = result.Next
	}
}

func TestLifetimePercentAndFixedEmployerContributions(t *testing.T) {
	for _, tc := range []struct {
		name, mode           string
		fixed, percent, want float64
	}{
		{name: "fixed", mode: "fixed", fixed: 700, want: 700},
		{name: "percent", mode: "percent", percent: 3, want: 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeContributionSettings()
			e := s.Lifetime.ContributionRules[0].Employer
			e.Mode = tc.mode
			e.FixedMonthly = tc.fixed
			e.PercentOfCompensation = tc.percent
			result, err := CalculateContributions(s, 0, ContributionYearState{})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.AmountsByRule["rule"].EmployerTraditional; !closeEnough(got, tc.want) {
				t.Fatalf("employer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLifetimeSponsorWagesRollIntoNextYear(t *testing.T) {
	s := lifetimeContributionSettings()
	var state ContributionYearState
	for month := 0; month < 13; month++ {
		result, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		state = result.Next
	}
	key := SponsorKey{OwnerID: "owner", EmployerID: "employer"}
	if !closeEnough(state.PriorSponsorWages[key], 100000) || !closeEnough(state.SponsorWages[key], 100000.0/12) {
		t.Fatalf("sponsor wage rollover = prior %v current %v", state.PriorSponsorWages[key], state.SponsorWages[key])
	}
	if state.Year != 2027 || state.LastMonth != 1 {
		t.Fatalf("calendar state = %+v", state)
	}
}

func TestLifetimeRejectsNonfiniteOpeningStateAndUnknownTiming(t *testing.T) {
	s := lifetimeContributionSettings()
	opening := ContributionYearState{Initialized: true, Year: 2026, LastMonth: 1, AdditionsByPlan: map[string]float64{"plan": math.Inf(1)}}
	_, err := CalculateContributions(s, 1, opening)
	if err == nil || !strings.Contains(err.Error(), "finite") {
		t.Fatalf("nonfinite state error = %v", err)
	}
	s = lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].Employer.MatchTiming = "annual"
	_, err = CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "match timing") {
		t.Fatalf("timing error = %v", err)
	}
}

func TestLifetimeRejectsInvalidRothSplitAtCalculationBoundary(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].RothPercent = 101
	_, err := CalculateContributions(s, 0, ContributionYearState{})
	if err == nil || !strings.Contains(err.Error(), "Roth percentage") {
		t.Fatalf("error = %v", err)
	}
}

func TestLifetimeRejectsOpeningAdditionsAboveRevisedAnnualCap(t *testing.T) {
	s := lifetimeContributionSettings()
	opening := ContributionYearState{Initialized: true, Year: 2026, LastMonth: 1, AdditionsByPlan: map[string]float64{"plan": 80000}}
	_, err := CalculateContributions(s, 1, opening)
	if err == nil || !strings.Contains(err.Error(), "already exceed") {
		t.Fatalf("error = %v", err)
	}
}
func TestLP2PrimaryOwnerCapIgnoresCustomLimitGroups(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 600000
	s.Lifetime.Jobs[0].EligibleCompensation = 600000
	s.Lifetime.Accounts[0].LimitGroup = "invented-a"
	s.Lifetime.ContributionRules[0].Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 20000}
	s.Lifetime.ContributionRules[0].Employer = nil
	job2 := s.Lifetime.Jobs[0]
	job2.ID, job2.EmployerID = "job2", "employer2"
	s.Lifetime.Jobs = append(s.Lifetime.Jobs, job2)
	account2 := s.Lifetime.Accounts[0]
	account2.ID, account2.PlanID, account2.EmployerID, account2.LimitGroup = "traditional2", "plan2", "employer2", "invented-b"
	s.Lifetime.Accounts = append(s.Lifetime.Accounts, account2)
	rule2 := s.Lifetime.ContributionRules[0]
	rule2.ID, rule2.JobID, rule2.TraditionalAccountID = "rule2", "job2", "traditional2"
	s.Lifetime.ContributionRules = append(s.Lifetime.ContributionRules, rule2)
	state := ContributionYearState{}
	for month := 0; month < 2; month++ {
		got, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		state = got.Next
	}
	if got := state.EmployeeRegularByOwner["owner"]; got != 24500 {
		t.Fatalf("owner cap = %v, want 24500", got)
	}
}

func TestLP2PrimarySponsorPriorWagesNotDuplicatedAcrossJobs(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Persons[0].BirthMonth = "1971-01"
	s.Lifetime.Jobs[0].PriorSponsorWages = 80000
	job2 := s.Lifetime.Jobs[0]
	job2.ID = "job2"
	s.Lifetime.Jobs = append(s.Lifetime.Jobs, job2)
	account2 := s.Lifetime.Accounts[0]
	account2.ID, account2.PlanID = "traditional2", "plan2"
	s.Lifetime.Accounts = append(s.Lifetime.Accounts, account2)
	rule2 := s.Lifetime.ContributionRules[0]
	rule2.ID, rule2.JobID, rule2.TraditionalAccountID = "rule2", "job2", "traditional2"
	rule2.Employer = nil
	s.Lifetime.ContributionRules = append(s.Lifetime.ContributionRules, rule2)
	got, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	key := SponsorKey{OwnerID: "owner", EmployerID: "employer"}
	if got.Next.PriorSponsorWages[key] != 80000 {
		t.Fatalf("prior sponsor wages = %v, want 80000", got.Next.PriorSponsorWages[key])
	}
	if got.Next.SponsorWages[key] != 100000.0/6 {
		t.Fatalf("current sponsor wages = %v, want %v", got.Next.SponsorWages[key], 100000.0/6)
	}
}

func independentContributionState(in ContributionYearState) ContributionYearState {
	out := in
	copyStringMap := func(src map[string]float64) map[string]float64 {
		if src == nil {
			return nil
		}
		dst := make(map[string]float64, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	copySponsorMap := func(src map[SponsorKey]float64) map[SponsorKey]float64 {
		if src == nil {
			return nil
		}
		dst := make(map[SponsorKey]float64, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	out.EmployeeRegularByOwner = copyStringMap(in.EmployeeRegularByOwner)
	out.EmployeeCatchUpByOwner = copyStringMap(in.EmployeeCatchUpByOwner)
	out.AdditionsByPlan = copyStringMap(in.AdditionsByPlan)
	out.EligibleCompensationByPlan = copyStringMap(in.EligibleCompensationByPlan)
	out.EligibleCompensationByJob = copyStringMap(in.EligibleCompensationByJob)
	out.GrossCompensationByPlan = copyStringMap(in.GrossCompensationByPlan)
	out.GrossCompensationByJob = copyStringMap(in.GrossCompensationByJob)
	out.MatchingPaidByRule = copyStringMap(in.MatchingPaidByRule)
	out.EmployeeByRule = copyStringMap(in.EmployeeByRule)
	out.SponsorWages = copySponsorMap(in.SponsorWages)
	out.PriorSponsorWages = copySponsorMap(in.PriorSponsorWages)
	return out
}

func independentContributionMonth(in ContributionMonth) ContributionMonth {
	out := in
	out.AmountsByRule = make(map[string]models.ContributionAmounts, len(in.AmountsByRule))
	for k, v := range in.AmountsByRule {
		out.AmountsByRule[k] = v
	}
	out.Movements = append([]models.AccountMovement(nil), in.Movements...)
	out.Next = independentContributionState(in.Next)
	out.PayrollWagesByOwner = make(map[string]float64, len(in.PayrollWagesByOwner))
	for k, v := range in.PayrollWagesByOwner {
		out.PayrollWagesByOwner[k] = v
	}
	out.GrossPayByJob = make(map[string]float64, len(in.GrossPayByJob))
	for k, v := range in.GrossPayByJob {
		out.GrossPayByJob[k] = v
	}
	out.Policy.SourceURLs = append([]string(nil), in.Policy.SourceURLs...)
	return out
}

func populatedContributionOpening() ContributionYearState {
	key := SponsorKey{OwnerID: "sentinel", EmployerID: "sentinel"}
	return ContributionYearState{Initialized: true, Year: 2026, LastMonth: 0, EmployeeRegularByOwner: map[string]float64{"sentinel": 1}, EmployeeCatchUpByOwner: map[string]float64{"sentinel": 2}, AdditionsByPlan: map[string]float64{"sentinel": 3}, EligibleCompensationByPlan: map[string]float64{"sentinel": 4}, EligibleCompensationByJob: map[string]float64{"sentinel": 5}, GrossCompensationByPlan: map[string]float64{"sentinel": 6}, GrossCompensationByJob: map[string]float64{"sentinel": 7}, MatchingPaidByRule: map[string]float64{"sentinel": 8}, EmployeeByRule: map[string]float64{"sentinel": 9}, SponsorWages: map[SponsorKey]float64{key: 10}, PriorSponsorWages: map[SponsorKey]float64{key: 11}}
}

func TestLP2PrimaryDeepCopiesEveryOpeningMap(t *testing.T) {
	opening := populatedContributionOpening()
	want := independentContributionState(opening)
	got, err := CalculateContributions(nil, 1, opening)
	if err != nil {
		t.Fatal(err)
	}
	key := SponsorKey{OwnerID: "sentinel", EmployerID: "sentinel"}
	got.Next.EmployeeRegularByOwner["sentinel"] = 99
	got.Next.EmployeeCatchUpByOwner["sentinel"] = 99
	got.Next.AdditionsByPlan["sentinel"] = 99
	got.Next.EligibleCompensationByPlan["sentinel"] = 99
	got.Next.EligibleCompensationByJob["sentinel"] = 99
	got.Next.GrossCompensationByPlan["sentinel"] = 99
	got.Next.GrossCompensationByJob["sentinel"] = 99
	got.Next.MatchingPaidByRule["sentinel"] = 99
	got.Next.EmployeeByRule["sentinel"] = 99
	got.Next.SponsorWages[key] = 99
	got.Next.PriorSponsorWages[key] = 99
	if !reflect.DeepEqual(opening, want) {
		t.Fatalf("opening aliased through result: got %#v want %#v", opening, want)
	}
}

func TestLP2ProbeLateErrorLeavesOpeningUntouched(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.ContributionRules[0].Employer.Mode = "unsupported"
	opening := ContributionYearState{
		Initialized: true, Year: 2026, LastMonth: 1,
		EmployeeRegularByOwner: map[string]float64{"owner": 123},
		SponsorWages:           map[SponsorKey]float64{{OwnerID: "owner", EmployerID: "employer"}: 456},
	}
	want := independentContributionState(opening)
	_, err := CalculateContributions(s, 1, opening)
	if err == nil || !strings.Contains(err.Error(), "unsupported employer") {
		t.Fatalf("expected late employer error, got %v", err)
	}
	if !reflect.DeepEqual(opening, want) {
		t.Fatalf("opening mutated on error: got %#v want %#v", opening, want)
	}
}

func TestLP2ProbeAnnualCompensationCapIsNotYTDCap(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 12000
	s.Lifetime.Jobs[0].EligibleCompensation = 12000
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 1000}
	r.Employer.Mode = "fixed"
	r.Employer.FixedMonthly = 2000
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	a := result.AmountsByRule["rule"]
	if a.Permitted != 1000 || a.EmployerTraditional != 2000 {
		t.Fatalf("January amounts = employee %v employer %v; annual 100%% compensation ceiling must not be limited to January YTD pay", a.Permitted, a.EmployerTraditional)
	}
}

func TestLP2ProbeCurrentHighPayDoesNotTriggerRothCatchUp(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Persons[0].BirthMonth = "1971-01"
	s.Lifetime.Jobs[0].GrossSalary = 600000
	s.Lifetime.Jobs[0].EligibleCompensation = 600000
	s.Lifetime.Jobs[0].PriorSponsorWages = 0
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 40000}
	r.Employer = nil
	result, err := CalculateContributions(s, 0, ContributionYearState{})
	if err != nil {
		t.Fatal(err)
	}
	a := result.AmountsByRule["rule"]
	if a.Permitted != 32500 || a.EmployeeTraditional != 32500 || a.EmployeeRoth != 0 {
		t.Fatalf("high current pay incorrectly affected catch-up: %+v", a)
	}
}

func TestLP2ProbeDepartureTrueUpOccursOnlyOnFinalActiveMonth(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 120000
	s.Lifetime.Jobs[0].EligibleCompensation = 120000
	s.Lifetime.Jobs[0].EndMonth = "2026-07"
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 6000}
	r.Changes = []models.ContributionChange{{Month: "2026-02", Rate: models.ContributionRate{Mode: "fixed"}}}
	r.Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
	r.Employer.TrueUp = true
	r.Employer.DepartureTrueUp = true
	var state ContributionYearState
	for month := 0; month < 5; month++ {
		result, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		if result.AmountsByRule["rule"].EmployerTraditional != map[bool]float64{true: 500, false: 0}[month == 0] {
			t.Fatalf("month %d paid unexpected pre-departure true-up: %+v", month, result.AmountsByRule["rule"])
		}
		state = result.Next
	}
	result, err := CalculateContributions(s, 5, state)
	if err != nil {
		t.Fatal(err)
	}
	if result.AmountsByRule["rule"].EmployerTraditional != 2500 {
		t.Fatalf("departure month payment = %v, want 2500 (entire payment is the departure true-up because the June employee rate is zero)", result.AmountsByRule["rule"].EmployerTraditional)
	}
}

func TestLifetimeContributionResultHasNoMutableAliases(t *testing.T) {
	s := lifetimeContributionSettings()
	opening := populatedContributionOpening()
	openingWant := independentContributionState(opening)
	first, err := CalculateContributions(s, 0, opening)
	if err != nil {
		t.Fatal(err)
	}
	independent, err := CalculateContributions(s, 0, opening)
	if err != nil {
		t.Fatal(err)
	}
	want := independentContributionMonth(independent)
	first.AmountsByRule["rule"] = models.ContributionAmounts{Requested: 999}
	first.PayrollWagesByOwner["owner"] = 999
	first.GrossPayByJob["job"] = 999
	if len(first.Movements) == 0 {
		t.Fatal("fixture produced no movements")
	}
	first.Movements[0].Amount = 999
	first.Next.EmployeeRegularByOwner["sentinel"] = 999
	first.Next.EmployeeCatchUpByOwner["sentinel"] = 999
	first.Next.AdditionsByPlan["sentinel"] = 999
	first.Next.EligibleCompensationByPlan["sentinel"] = 999
	first.Next.EligibleCompensationByJob["sentinel"] = 999
	first.Next.GrossCompensationByPlan["sentinel"] = 999
	first.Next.GrossCompensationByJob["sentinel"] = 999
	first.Next.MatchingPaidByRule["sentinel"] = 999
	first.Next.EmployeeByRule["sentinel"] = 999
	key := SponsorKey{OwnerID: "sentinel", EmployerID: "sentinel"}
	first.Next.SponsorWages[key] = 999
	first.Next.PriorSponsorWages[key] = 999
	if len(first.Policy.SourceURLs) == 0 {
		t.Fatal("fixture policy has no source URLs")
	}
	first.Policy.SourceURLs[0] = "mutated"
	if !reflect.DeepEqual(independent, want) {
		t.Fatalf("mutating one result changed separately calculated result: got=%#v want=%#v", independent, want)
	}
	if !reflect.DeepEqual(opening, openingWant) {
		t.Fatalf("mutating result changed opening: got=%#v want=%#v", opening, openingWant)
	}
}

func TestLP2DepartureTrueUpHasNoFollowingMonthPayment(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 120000
	s.Lifetime.Jobs[0].EligibleCompensation = 120000
	s.Lifetime.Jobs[0].EndMonth = "2026-07"
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 6000}
	r.Changes = []models.ContributionChange{{Month: "2026-02", Rate: models.ContributionRate{Mode: "fixed"}}}
	r.Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 5, MatchPercent: 100}}
	r.Employer.TrueUp = true
	r.Employer.DepartureTrueUp = true
	var state ContributionYearState
	for month := 0; month <= 5; month++ {
		result, err := CalculateContributions(s, month, state)
		if err != nil {
			t.Fatal(err)
		}
		state = result.Next
		if month == 5 && result.AmountsByRule["rule"].EmployerTraditional != 2500 {
			t.Fatalf("June departure true-up=%v want2500", result.AmountsByRule["rule"].EmployerTraditional)
		}
	}
	july, err := CalculateContributions(s, 6, state)
	if err != nil {
		t.Fatal(err)
	}
	a := july.AmountsByRule["rule"]
	if a.Funded != 0 || a.EmployerTraditional != 0 || a.EmployerRoth != 0 {
		t.Fatalf("July inactive amounts=%+v", a)
	}
	for _, m := range july.Movements {
		if m.RuleID == "rule" && m.Amount != 0 {
			t.Fatalf("July retained rule movement: %+v", m)
		}
	}
}

func TestLP2AnnualCompensationCapCountsOpeningYTDHistory(t *testing.T) {
	s := lifetimeContributionSettings()
	s.Lifetime.Jobs[0].GrossSalary = 12000
	s.Lifetime.Jobs[0].EligibleCompensation = 12000
	r := &s.Lifetime.ContributionRules[0]
	r.Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 1000}
	r.Employer.Mode = "fixed"
	r.Employer.FixedMonthly = 2000
	opening := ContributionYearState{Initialized: true, Year: 2026, LastMonth: 0, AdditionsByPlan: map[string]float64{"plan": 5000}, EligibleCompensationByPlan: map[string]float64{"plan": 6000}, EligibleCompensationByJob: map[string]float64{"job": 6000}, GrossCompensationByPlan: map[string]float64{"plan": 6000}, GrossCompensationByJob: map[string]float64{"job": 6000}}
	got, err := CalculateContributions(s, 0, opening)
	if err != nil {
		t.Fatal(err)
	}
	a := got.AmountsByRule["rule"]
	if a.Permitted != 1000 || a.EmployerTraditional != 2000 {
		t.Fatalf("January with opening history employee/employer=%v/%v want1000/2000; annual salary compensation ceiling must not clip to YTD", a.Permitted, a.EmployerTraditional)
	}
	if got.Next.AdditionsByPlan["plan"] != 8000 || got.Next.EligibleCompensationByPlan["plan"] != 7000 {
		t.Fatalf("opening history not counted exactly: %+v", got.Next)
	}
}
