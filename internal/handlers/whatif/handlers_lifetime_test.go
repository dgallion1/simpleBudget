package whatif

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"budget2/internal/models"
)

func lifetimeRequest(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	switch path {
	case "/whatif/lifetime/preview":
		handleWhatIfLifetimePreview(w, req)
	case "/whatif/lifetime/save":
		handleWhatIfLifetimeSave(w, req)
	case "/whatif/lifetime/remove":
		handleWhatIfLifetimeRemove(w, req)
	}
	return w
}

func validLifetimeFixture(s *models.WhatIfSettings) *models.LifetimeSettings {
	return &models.LifetimeSettings{Version: 1,
		Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: s.Persons[0].ID, LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 10000, CashPercent: 100},
			{ID: "broker", OwnerID: s.Persons[0].ID, LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
			{ID: "trad", OwnerID: s.Persons[0].ID, LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "employer", LimitGroup: "plan", StockPercent: 100},
		},
		Jobs:              []models.LifetimeJob{{ID: "job", OwnerID: s.Persons[0].ID, EmployerID: "employer", GrossSalary: 100000, EligibleCompensation: 100000, StartMonth: "2026-01"}},
		ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", Rate: models.ContributionRate{Mode: "percent", PercentOfCompensation: 10}, TraditionalAccountID: "trad", Employer: &models.EmployerContribution{Mode: "match", DestinationAccountID: "trad", MatchTiming: "monthly", Tiers: []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}}}}},
		CashPolicy:        models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "trad"}},
		YTD:               models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true},
	}
}

func TestLifetimePreviewAndGuardedSaveRoundTrip(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, rev, err := rm.LoadContextWithRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	s, rev, _ = rm.LoadContextWithRevision(t.Context())
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: validLifetimeFixture(s), RetirementMonths: map[string]string{"p": "2040-06"}}
	preview := lifetimeRequest(t, "/whatif/lifetime/preview", req)
	if preview.Code != 200 {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var got lifetimeResponse
	if err := json.Unmarshal(preview.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	a := got.Preview.AmountsByRule["rule"]
	if math.Abs(a.EmployeeTraditional-10000) > .01 || math.Abs(a.EmployerTraditional-3000) > .01 {
		t.Fatalf("preview amounts=%+v", a)
	}
	saved := lifetimeRequest(t, "/whatif/lifetime/save", req)
	if saved.Code != 200 {
		t.Fatalf("save status=%d body=%s", saved.Code, saved.Body.String())
	}
	loaded, _ := rm.Load()
	if loaded.Lifetime == nil || loaded.Persons[0].RetirementMonth != "2040-06" {
		t.Fatalf("roundtrip=%+v persons=%+v", loaded.Lifetime, loaded.Persons)
	}
	if loaded.Lifetime.Accounts[2].LegalType != "401k" || loaded.Lifetime.ContributionRules[0].Employer.Tiers[0].ToPercent != 6 {
		t.Fatal("canonical fields lost")
	}
}

func TestLifetimeStaleAndInvalidWritesAreAtomic(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, rev, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	s, rev, _ = rm.LoadContextWithRevision(t.Context())
	good := validLifetimeFixture(s)
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: good}
	stale := rev - 1
	req.ExpectedRevision = &stale
	if w := lifetimeRequest(t, "/whatif/lifetime/save", req); w.Code != 409 {
		t.Fatalf("stale=%d %s", w.Code, w.Body.String())
	}
	req.ExpectedRevision = &rev
	bad := *good
	bad.ContributionRules = append([]models.ContributionRule(nil), good.ContributionRules...)
	bad.ContributionRules[0].TraditionalAccountID = "missing"
	req.Lifetime = &bad
	if w := lifetimeRequest(t, "/whatif/lifetime/save", req); w.Code != 400 {
		t.Fatalf("invalid=%d %s", w.Code, w.Body.String())
	}
	loaded, _ := rm.Load()
	if loaded.Lifetime != nil {
		t.Fatal("invalid request overwrote settings")
	}
}

func TestLifetimeRemovePreservesPeopleAndLegacySettings(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-01"
	s.MonthlyLivingExpenses = 4321
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	s.Lifetime = validLifetimeFixture(s)
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := rm.LoadContextWithRevision(t.Context())
	w := lifetimeRequest(t, "/whatif/lifetime/remove", lifetimeRemoveRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Confirmed: true})
	if w.Code != 200 {
		t.Fatalf("remove=%d %s", w.Code, w.Body.String())
	}
	got, _ := rm.Load()
	if got.Lifetime != nil || got.Persons[0].RetirementMonth != "2045-01" || got.MonthlyLivingExpenses != 4321 {
		t.Fatalf("remove damaged settings: %+v", got)
	}
}

func TestLifetimeEndpointsRequireRevisionKeyEvenAtRevisionZero(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, err := rm.LoadContextWithRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	plan := validLifetimeFixture(s)
	for _, tc := range []struct {
		name, path string
		body       any
	}{
		{"preview", "/whatif/lifetime/preview", lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), Lifetime: plan}},
		{"save", "/whatif/lifetime/save", lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), Lifetime: plan}},
		{"remove", "/whatif/lifetime/remove", lifetimeRemoveRequest{ExpectedScenario: rm.ActiveFilename(), Confirmed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := lifetimeRequest(t, tc.path, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLifetimeCanonicalValidationRejectsTierAndMissingMidyearHistoryAtomically(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-09"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*models.WhatIfSettings, *models.LifetimeSettings)
	}{
		{"overlapping tiers", func(_ *models.WhatIfSettings, p *models.LifetimeSettings) {
			p.ContributionRules[0].Employer.Tiers = []models.MatchTier{{FromPercent: 0, ToPercent: 6, MatchPercent: 50}, {FromPercent: 5, ToPercent: 8, MatchPercent: 25}}
		}},
		{"missing midyear history acknowledgement", func(s *models.WhatIfSettings, p *models.LifetimeSettings) {
			p.YTD = models.LifetimeYTD{Year: 2026}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, rev, _ := rm.LoadContextWithRevision(t.Context())
			plan := validLifetimeFixture(base)
			tc.mutate(base, plan)
			req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: plan}
			w := lifetimeRequest(t, "/whatif/lifetime/save", req)
			if w.Code != 400 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			got, _ := rm.Load()
			if got.Lifetime != nil {
				t.Fatal("invalid save mutated lifetime")
			}
		})
	}
}

func TestLifetimeRetirementEditPreservesIndependentBenefitDate(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	end := 150
	s.IncomeSources = []models.IncomeSource{{ID: "pension", Name: "Pension", Amount: 1000, Type: models.IncomeDelayed, StartMonth: 120, EndMonth: &end}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	base, rev, _ := rm.LoadContextWithRevision(t.Context())
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: validLifetimeFixture(base), RetirementMonths: map[string]string{"p": "2040-06"}}
	w := lifetimeRequest(t, "/whatif/lifetime/save", req)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := rm.Load()
	if got.IncomeSources[0].StartMonth != 120 || *got.IncomeSources[0].EndMonth != 150 {
		t.Fatalf("benefit dates changed: %+v", got.IncomeSources[0])
	}
}

func TestLifetimeEmployeeOnlyAndNonzeroMidyearHistoryRoundTrip(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-09"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	base, rev, _ := rm.LoadContextWithRevision(t.Context())
	plan := validLifetimeFixture(base)
	plan.ContributionRules[0].Employer = nil
	plan.ContributionRules[0].Rate = models.ContributionRate{Mode: "fixed", FixedMonthly: 500}
	plan.YTD = models.LifetimeYTD{Year: 2026, Rules: []models.ContributionYTD{{RuleID: "rule", EmployeeRegular: 4000, EmployeeCatchUp: 0, Employer: 0, MatchingPaid: 0}}, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 80000, EligiblePay: 80000}}}
	w := lifetimeRequest(t, "/whatif/lifetime/save", lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: plan})
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := rm.Load()
	if got.Lifetime.ContributionRules[0].Employer != nil || got.Lifetime.ContributionRules[0].Rate.FixedMonthly != 500 || got.Lifetime.YTD.Jobs[0].GrossWages != 80000 || got.Lifetime.YTD.Rules[0].EmployeeRegular != 4000 {
		t.Fatalf("roundtrip=%+v", got.Lifetime)
	}
}
func TestLP4SecondPartialPreviewAndLinkedRetirement(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-09"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2026-12"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	base, rev, _ := rm.LoadContextWithRevision(t.Context())
	plan := validLifetimeFixture(base)
	plan.Jobs[0].StartMonth = "2026-01"
	plan.Jobs[0].EndAtRetirement = true
	plan.ContributionRules[0].StartMonth = "2026-01"
	plan.ContributionRules[0].EndAtRetirement = true
	plan.YTD = models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: plan, RetirementMonths: map[string]string{"p": "2026-11"}}
	beforeRaw, _ := json.Marshal(base)
	w := lifetimeRequest(t, "/whatif/lifetime/preview", req)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var got lifetimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	a := got.Preview.AmountsByRule["rule"]
	// September and October are active; November retirement is exclusive.
	if math.Abs(a.EmployeeTraditional-1666.6666667) > .02 || math.Abs(a.EmployerTraditional-500) > .02 {
		t.Fatalf("partial linked preview employee=%v employer=%v period=%s", a.EmployeeTraditional, a.EmployerTraditional, got.Preview.Period)
	}
	after, _ := rm.Load()
	afterRaw, _ := json.Marshal(after)
	if string(beforeRaw) != string(afterRaw) {
		t.Fatal("preview wrote settings")
	}
}

func TestLP4SecondFullHistoryRoundTripAndStaleRemove(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-09"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	base, rev, _ := rm.LoadContextWithRevision(t.Context())
	plan := validLifetimeFixture(base)
	prior := 77777.25
	plan.Accounts[2].PriorDecemberValue = &prior
	plan.Accounts[2].RMDPaidYTD = 123.45
	plan.Accounts[2].CurrentEmployerRMDDeferral = true
	plan.Accounts[2].OwnerMoreThanFivePercent = true
	plan.Jobs[0].PriorSponsorWages = 65432.10
	plan.ContributionRules[0].Changes = []models.ContributionChange{{Month: "2026-11", Rate: models.ContributionRate{Mode: "fixed", FixedMonthly: 321}}}
	plan.ScheduledSavings = []models.ScheduledSaving{{ID: "save", OwnerID: "p", DestinationAccountID: "broker", MonthlyAmount: 88.25, StartMonth: "2026-10", EndMonth: "2027-01"}}
	plan.YTD = models.LifetimeYTD{Year: 2026, Rules: []models.ContributionYTD{{RuleID: "rule", EmployeeRegular: 1, EmployeeCatchUp: 2, Employer: 3, MatchingPaid: 3}}, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 4, EligiblePay: 4}}}
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: plan}
	w := lifetimeRequest(t, "/whatif/lifetime/save", req)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	loaded, _ := rm.Load()
	if !reflect.DeepEqual(loaded.Lifetime, plan) {
		t.Fatalf("roundtrip mismatch\n got=%#v\nwant=%#v", loaded.Lifetime, plan)
	}
	before, _ := json.Marshal(loaded)
	stale := rev
	w = lifetimeRequest(t, "/whatif/lifetime/remove", lifetimeRemoveRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &stale, Confirmed: true})
	if w.Code != 409 {
		t.Fatalf("stale remove %d %s", w.Code, w.Body.String())
	}
	after, _ := rm.Load()
	afterRaw, _ := json.Marshal(after)
	if string(before) != string(afterRaw) {
		t.Fatal("stale remove mutated settings")
	}
}

func TestLP4SecondCappedPreviewUsesPolicy(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", Persons: []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01"}}, TaxConfig: &models.TaxConfig{FilingStatus: models.FilingSingle}}
	s.Lifetime = validLifetimeFixture(s)
	s.Lifetime.Jobs[0].GrossSalary = 1000000
	s.Lifetime.Jobs[0].EligibleCompensation = 1000000
	p, err := annualContributionPreview(s)
	if err != nil {
		t.Fatal(err)
	}
	a := p.AmountsByRule["rule"]
	if math.Abs(a.Requested-100000) > .02 || math.Abs(a.Permitted-24500) > .02 || math.Abs(a.EmployeeTraditional-24500) > .02 || math.Abs(a.EmployerTraditional-7500) > .02 {
		t.Fatalf("capped preview %+v", a)
	}
}
func TestLP4SecondSplitPreviewAndRothHistoryRoundTrip(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _, _ := rm.LoadContextWithRevision(t.Context())
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Pat", Role: models.PersonRolePrimary, BirthMonth: "1980-01"}}
	if _, err := rm.SaveWithRevision(s); err != nil {
		t.Fatal(err)
	}
	base, rev, _ := rm.LoadContextWithRevision(t.Context())
	plan := validLifetimeFixture(base)
	caps := models.PlanCapabilities{AllowEmployeeRoth: true, AllowEmployerRoth: true}
	plan.Accounts[2].Capabilities = caps
	prior := 9000.0
	plan.Accounts = append(plan.Accounts, models.LifetimeAccount{ID: "roth", Name: "Roth plan", OwnerID: "p", LegalType: "401k", TaxTreatment: "roth", PlanID: "plan", EmployerID: "employer", LimitGroup: "plan", Capabilities: caps, OpeningValue: 10000, Basis: 7000, StockPercent: 100, RothFirstFundedYear: 2015, PriorDecemberValue: &prior, RMDPaidYTD: 12})
	plan.ContributionRules[0].RothPercent = 40
	plan.ContributionRules[0].RothAccountID = "roth"
	req := lifetimeMutationRequest{ExpectedScenario: rm.ActiveFilename(), ExpectedRevision: &rev, Lifetime: plan}
	w := lifetimeRequest(t, "/whatif/lifetime/preview", req)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var got lifetimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	a := got.Preview.AmountsByRule["rule"]
	if math.Abs(a.EmployeeTraditional-6000) > .02 || math.Abs(a.EmployeeRoth-4000) > .02 || math.Abs(a.EmployerTraditional-3000) > .02 || a.EmployerRoth != 0 {
		t.Fatalf("split %+v", a)
	}
	w = lifetimeRequest(t, "/whatif/lifetime/save", req)
	if w.Code != 200 {
		t.Fatalf("save %d %s", w.Code, w.Body.String())
	}
	loaded, _ := rm.Load()
	r := loaded.Lifetime.Accounts[3]
	if r.RothFirstFundedYear != 2015 || r.PriorDecemberValue == nil || *r.PriorDecemberValue != 9000 || r.RMDPaidYTD != 12 || r.Basis != 7000 {
		t.Fatalf("Roth history %+v", r)
	}
}
