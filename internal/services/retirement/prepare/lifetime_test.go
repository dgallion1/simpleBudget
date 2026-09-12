package prepare

import (
	"budget2/internal/models"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestLifetimeRejectInvalidVersion(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	if err := json.Unmarshal([]byte(`{"lifetime":{"version":99}}`), s); err != nil {
		t.Fatal(err)
	}
	if _, err := From(s); err == nil {
		t.Fatal("unsupported lifetime version accepted")
	}
}

func lifetimeFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p", Name: "Person", Role: models.PersonRolePrimary, BirthMonth: "1980-01", RetirementMonth: "2045-07"}}
	s.IncomeSources = []models.IncomeSource{{ID: "salary", Amount: 10000}, {ID: "benefit", StartMonth: 240, Amount: 500}}
	s.Lifetime = &models.LifetimeSettings{
		Version: 1,
		Accounts: []models.LifetimeAccount{
			{ID: "cash", OwnerID: "p", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 1000, CashPercent: 100},
			{ID: "broker", OwnerID: "p", LegalType: "brokerage", TaxTreatment: "taxable", OpeningValue: 2000, Basis: 2500, StockPercent: 100},
			{ID: "trad", OwnerID: "p", LegalType: "401k", TaxTreatment: "traditional", PlanID: "plan", EmployerID: "sponsor", LimitGroup: "deferral", StockPercent: 100, Capabilities: models.PlanCapabilities{AllowCatchUp: true, AllowRothCatchUp: true, AllowEmployeeRoth: true, AllowEmployerRoth: true}},
			{ID: "roth", OwnerID: "p", LegalType: "401k", TaxTreatment: "roth", PlanID: "plan", EmployerID: "sponsor", LimitGroup: "deferral", StockPercent: 100, Capabilities: models.PlanCapabilities{AllowCatchUp: true, AllowRothCatchUp: true, AllowEmployeeRoth: true, AllowEmployerRoth: true}},
		},
		Jobs:              []models.LifetimeJob{{ID: "job", OwnerID: "p", EmployerID: "sponsor", GrossSalary: 120000, EligibleCompensation: 100000, StartMonth: "2025-01", EndAtRetirement: true, IncomeSourceID: "salary"}},
		ContributionRules: []models.ContributionRule{{ID: "rule", JobID: "job", StartMonth: "2026-01", EndAtRetirement: true, Rate: models.ContributionRate{Mode: "percent", PercentOfCompensation: 10}, RothPercent: 20, TraditionalAccountID: "trad", RothAccountID: "roth", Employer: &models.EmployerContribution{Mode: "match", DestinationAccountID: "trad", MatchTiming: "monthly", Tiers: []models.MatchTier{{FromPercent: 0, ToPercent: 3, MatchPercent: 100}, {FromPercent: 3, ToPercent: 5, MatchPercent: 50}}}}},
		ScheduledSavings:  []models.ScheduledSaving{{ID: "saving", OwnerID: "p", DestinationAccountID: "broker", MonthlyAmount: 500, StartMonth: "2026-01"}},
		CashPolicy:        models.LifetimeCashPolicy{ReserveAccountID: "cash", ReserveTarget: 10000, SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "trad", "roth"}},
	}
	return s
}

func TestLifetimeValidation(t *testing.T) {
	cases := []struct {
		name, field string
		mutate      func(*models.WhatIfSettings)
	}{
		{"duplicate account", "accounts", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[1].ID = "cash" }},
		{"dangling owner", "owner", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].OwnerID = "missing" }},
		{"negative value", "opening_value", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].OpeningValue = -1 }},
		{"nan value", "opening_value", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].OpeningValue = math.NaN() }},
		{"infinite basis", "basis", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].Basis = math.Inf(1) }},
		{"bad allocation", "allocation", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].StockPercent = 1 }},
		{"bad legal type", "legal_type", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].LegalType = "mystery" }},
		{"bad cash tax treatment", "tax_treatment", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[0].TaxTreatment = "roth" }},
		{"duplicate job", "jobs", func(s *models.WhatIfSettings) { s.Lifetime.Jobs = append(s.Lifetime.Jobs, s.Lifetime.Jobs[0]) }},
		{"duplicate salary link", "income_source", func(s *models.WhatIfSettings) {
			j := s.Lifetime.Jobs[0]
			j.ID = "second"
			s.Lifetime.Jobs = append(s.Lifetime.Jobs, j)
		}},
		{"dangling salary", "income_source", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].IncomeSourceID = "missing" }},
		{"invalid job date", "start_month", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].StartMonth = "2026-13" }},
		{"ambiguous job end", "end", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].EndMonth = "2030-01" }},
		{"retirement missing", "retirement", func(s *models.WhatIfSettings) { s.Persons[0].RetirementMonth = "" }},
		{"negative salary", "gross_salary", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].GrossSalary = -1 }},
		{"eligible over salary", "eligible_compensation", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].EligibleCompensation = 130000 }},
		{"duplicate rule", "contribution_rules", func(s *models.WhatIfSettings) {
			s.Lifetime.ContributionRules = append(s.Lifetime.ContributionRules, s.Lifetime.ContributionRules[0])
		}},
		{"dangling job", "job_id", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].JobID = "missing" }},
		{"dangling destination", "account", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].TraditionalAccountID = "missing" }},
		{"wrong destination", "traditional", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].TraditionalAccountID = "broker" }},
		{"unlinked employer", "employer", func(s *models.WhatIfSettings) { s.Lifetime.Jobs[0].EmployerID = "other" }},
		{"inconsistent plan", "plan", func(s *models.WhatIfSettings) { s.Lifetime.Accounts[3].Capabilities.AllowEmployerRoth = false }},
		{"overlapping tiers", "tiers", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Employer.Tiers[1].FromPercent = 2 }},
		{"inverted tier", "tiers", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Employer.Tiers[0].FromPercent = 4 }},
		{"negative match", "tiers", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Employer.Tiers[0].MatchPercent = -1 }},
		{"departure without trueup", "true_up", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Employer.DepartureTrueUp = true }},
		{"invalid rate", "mode", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Rate.Mode = "guess" }},
		{"mixed rate", "fixed_monthly", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].Rate.FixedMonthly = 100 }},
		{"invalid split", "roth_percent", func(s *models.WhatIfSettings) { s.Lifetime.ContributionRules[0].RothPercent = 101 }},
		{"invalid saving destination", "destination", func(s *models.WhatIfSettings) { s.Lifetime.ScheduledSavings[0].DestinationAccountID = "trad" }},
		{"negative saving", "monthly_amount", func(s *models.WhatIfSettings) { s.Lifetime.ScheduledSavings[0].MonthlyAmount = -1 }},
		{"invalid reserve", "reserve", func(s *models.WhatIfSettings) { s.Lifetime.CashPolicy.ReserveAccountID = "broker" }},
		{"duplicate withdrawal", "withdrawal_order", func(s *models.WhatIfSettings) {
			s.Lifetime.CashPolicy.WithdrawalOrder = append(s.Lifetime.CashPolicy.WithdrawalOrder, "cash")
		}},
		{"wrong time origin", "start_month", func(s *models.WhatIfSettings) { s.Lifetime.StartMonth = "2025-01" }},
		{"missing midyear history", "ytd", func(s *models.WhatIfSettings) { s.StartDate = "2026-07" }},
		{"unsorted changes", "changes", func(s *models.WhatIfSettings) {
			s.Lifetime.ContributionRules[0].Changes = []models.ContributionChange{{Month: "2027-01", Rate: models.ContributionRate{Mode: "fixed"}}, {Month: "2026-05", Rate: models.ContributionRate{Mode: "fixed"}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeFixture()
			tc.mutate(s)
			err := ValidateLifetime(s)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("got %v, want %s validation", err, tc.field)
			}
		})
	}
}

func TestLifetimePreparationDeepCopyAndRetirementLinks(t *testing.T) {
	s := lifetimeFixture()
	p, err := From(s)
	if err != nil {
		t.Fatal(err)
	}
	before := p.Settings()
	if before.Lifetime.StartMonth != "2026-01" {
		t.Fatal("missing derived start month")
	}
	jobEnd, err := before.Lifetime.Jobs[0].EffectiveEndMonth(before.Persons)
	if err != nil || jobEnd != "2045-07" {
		t.Fatal("retirement link not resolved", err)
	}
	ruleEnd, err := before.Lifetime.ContributionRules[0].EffectiveEndMonth(before.Lifetime.Jobs[0], before.Persons)
	if err != nil || ruleEnd != "2045-07" {
		t.Fatal("rule retirement link not resolved", err)
	}
	if s.Lifetime.Jobs[0].EndMonth != "" {
		t.Fatal("input mutated")
	}
	s.Persons[0].RetirementMonth = "2040-04"
	s.Lifetime.ContributionRules[0].Employer.Tiers[0].MatchPercent = 25
	s.Lifetime.CashPolicy.WithdrawalOrder[0] = "roth"
	if before.Lifetime.ContributionRules[0].Employer.Tiers[0].MatchPercent != 100 || before.Lifetime.CashPolicy.WithdrawalOrder[0] != "cash" {
		t.Fatal("prepared slices alias input")
	}
	s.Lifetime.CashPolicy.WithdrawalOrder[0] = "cash"
	after, err := From(s)
	if err != nil {
		t.Fatal(err)
	}
	afterEnd, err := after.Settings().Lifetime.Jobs[0].EffectiveEndMonth(after.Settings().Persons)
	if err != nil || afterEnd != "2040-04" {
		t.Fatal("new retirement not honored", err)
	}
	if !reflect.DeepEqual(s.IncomeSources, before.IncomeSources) || !reflect.DeepEqual(s.IncomeSources, after.Settings().IncomeSources) {
		t.Fatal("independently dated benefits changed")
	}
	again, err := From(before)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, again.Settings()) {
		t.Fatal("preparation not idempotent")
	}
}

func TestLifetimeMidyearYTD(t *testing.T) {
	s := lifetimeFixture()
	s.StartDate = "2026-07"
	s.Lifetime.YTD = models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}
	if _, err := From(s); err != nil {
		t.Fatal(err)
	}
	s.Lifetime.YTD = models.LifetimeYTD{Year: 2026, Rules: []models.ContributionYTD{{RuleID: "rule", EmployeeRegular: 5000, EmployeeCatchUp: 1000, Employer: 2000, MatchingPaid: 1500}}, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 60000, EligiblePay: 50000}}}
	if _, err := From(s); err != nil {
		t.Fatal(err)
	}
	s.Lifetime.YTD.Rules[0].MatchingPaid = 2001
	if _, err := From(s); err == nil {
		t.Fatal("matching exceeds employer YTD accepted")
	}
	s.Lifetime.YTD.Rules[0].MatchingPaid = 1000
	s.Lifetime.YTD.ZeroHistoryAcknowledged = true
	if _, err := From(s); err == nil {
		t.Fatal("nonzero zero-history accepted")
	}
}

func TestLifetimeUnlinkedBalancesHaveNoInferredEmployer(t *testing.T) {
	s := lifetimeFixture()
	s.Lifetime.ContributionRules = nil
	for i := range s.Lifetime.Accounts {
		a := &s.Lifetime.Accounts[i]
		a.PlanID = ""
		a.EmployerID = ""
		a.LimitGroup = ""
		a.Capabilities = models.PlanCapabilities{}
	}
	p, err := From(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Settings().Lifetime.Accounts {
		if a.EmployerID != "" || a.PlanID != "" {
			t.Fatal("employer inferred")
		}
	}
	if p.Settings().PortfolioValue != s.PortfolioValue || p.Settings().Lifetime.Accounts[0].OpeningValue != 1000 {
		t.Fatal("opening balances were combined or rewritten")
	}
}

func TestLifetimeMidyearHistoryBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*models.LifetimeSettings)
	}{
		{"wrong year", func(l *models.LifetimeSettings) { l.YTD.Year = 2025 }},
		{"duplicate rule", func(l *models.LifetimeSettings) { l.YTD.Rules = append(l.YTD.Rules, l.YTD.Rules[0]) }},
		{"missing rule", func(l *models.LifetimeSettings) { l.YTD.Rules = nil }},
		{"dangling rule", func(l *models.LifetimeSettings) { l.YTD.Rules[0].RuleID = "missing" }},
		{"negative catchup", func(l *models.LifetimeSettings) { l.YTD.Rules[0].EmployeeCatchUp = -1 }},
		{"infinite employer", func(l *models.LifetimeSettings) { l.YTD.Rules[0].Employer = math.Inf(1) }},
		{"duplicate job", func(l *models.LifetimeSettings) { l.YTD.Jobs = append(l.YTD.Jobs, l.YTD.Jobs[0]) }},
		{"missing job", func(l *models.LifetimeSettings) { l.YTD.Jobs = nil }},
		{"dangling job", func(l *models.LifetimeSettings) { l.YTD.Jobs[0].JobID = "missing" }},
		{"eligible over gross", func(l *models.LifetimeSettings) { l.YTD.Jobs[0].EligiblePay = 101 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeFixture()
			s.StartDate = "2026-07"
			s.Lifetime.YTD = models.LifetimeYTD{Year: 2026, Rules: []models.ContributionYTD{{RuleID: "rule"}}, Jobs: []models.JobYTD{{JobID: "job", GrossWages: 100}}}
			tc.mutate(s.Lifetime)
			if err := ValidateLifetime(s); err == nil {
				t.Fatal("invalid YTD accepted")
			}
		})
	}
}

func TestLifetimeEmployerAndScheduledChanges(t *testing.T) {
	for _, mode := range []string{"fixed", "percent"} {
		s := lifetimeFixture()
		e := s.Lifetime.ContributionRules[0].Employer
		e.Mode = mode
		e.Tiers = nil
		if mode == "fixed" {
			e.FixedMonthly = 500
		} else {
			e.PercentOfCompensation = 3
		}
		s.Lifetime.ContributionRules[0].Changes = []models.ContributionChange{{Month: "2027-01", Rate: models.ContributionRate{Mode: "fixed", FixedMonthly: 1000}}}
		if _, err := From(s); err != nil {
			t.Fatal(err)
		}
	}
	s := lifetimeFixture()
	s.Lifetime.ContributionRules[0].Employer.DestinationAccountID = "roth"
	if _, err := From(s); err != nil {
		t.Fatal(err)
	}
	for i := 2; i < 4; i++ {
		s.Lifetime.Accounts[i].Capabilities.AllowEmployerRoth = false
	}
	if _, err := From(s); err == nil {
		t.Fatal("employer Roth without explicit capability accepted")
	}
}

func TestLifetimeRetirementUsesCorrectOwnerAndExclusiveBoundary(t *testing.T) {
	s := lifetimeFixture()
	s.Persons = append(s.Persons, models.Person{ID: "spouse", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: "1985-01", RetirementMonth: "2050-01"})
	s.Persons[1].RetirementMonth = "2040-01"
	end, err := s.Lifetime.Jobs[0].EffectiveEndMonth(s.Persons)
	if err != nil || end != "2045-07" {
		t.Fatal("spouse retirement moved primary job", err)
	}
	j := s.Lifetime.Jobs[0]
	j.EndAtRetirement = false
	j.EndMonth = "2030-03"
	r := s.Lifetime.ContributionRules[0]
	end, err = r.EffectiveEndMonth(j, s.Persons)
	if err != nil || end != "2030-03" {
		t.Fatal("rule did not respect independent job end", err)
	}
	s.Lifetime.Jobs[0].EndAtRetirement = false
	s.Lifetime.Jobs[0].EndMonth = s.Lifetime.Jobs[0].StartMonth
	if _, err := From(s); err == nil {
		t.Fatal("empty exclusive range accepted")
	}
}

func TestLifetimeAccountHistoryValidation(t *testing.T) {
	s := lifetimeFixture()
	y := 1000.0
	s.Lifetime.Accounts[2].PriorDecemberValue = &y
	s.Lifetime.Accounts[2].RMDPaidYTD = 10
	if err := ValidateLifetime(s); err != nil {
		t.Fatal(err)
	}
	s.Lifetime.Accounts[2].RothFirstFundedYear = 2020
	if err := ValidateLifetime(s); err == nil || !strings.Contains(err.Error(), "Roth") {
		t.Fatalf("error=%v", err)
	}
	s.Lifetime.Accounts[2].RothFirstFundedYear = 0
	s.Lifetime.Accounts[0].CurrentEmployerRMDDeferral = true
	if err := ValidateLifetime(s); err == nil || !strings.Contains(err.Error(), "workplace") {
		t.Fatalf("error=%v", err)
	}
}
func independentSettingsSnapshot(s *models.WhatIfSettings) string {
	var b strings.Builder
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			b.WriteString("invalid;")
			return
		}
		b.WriteString(v.Type().String())
		b.WriteByte(':')
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if v.IsNil() {
				b.WriteString("nil;")
				return
			}
			walk(v.Elem())
		case reflect.Struct:
			b.WriteByte('{')
			for i := 0; i < v.NumField(); i++ {
				b.WriteString(v.Type().Field(i).Name)
				b.WriteByte('=')
				walk(v.Field(i))
			}
			b.WriteString("};")
		case reflect.Slice, reflect.Array:
			b.WriteByte('[')
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
			b.WriteString("];")
		case reflect.Map:
			if v.IsNil() {
				b.WriteString("nil;")
				return
			}
			type item struct {
				key   string
				value reflect.Value
			}
			items := make([]item, 0, v.Len())
			for _, k := range v.MapKeys() {
				var kb strings.Builder
				saved := b
				b = kb
				walk(k)
				key := b.String()
				b = saved
				items = append(items, item{key, v.MapIndex(k)})
			}
			sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
			b.WriteByte('{')
			for _, it := range items {
				b.WriteString(it.key)
				walk(it.value)
			}
			b.WriteString("};")
		case reflect.String:
			b.WriteString(strconv.Quote(v.String()))
			b.WriteByte(';')
		case reflect.Bool:
			b.WriteString(strconv.FormatBool(v.Bool()))
			b.WriteByte(';')
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			b.WriteString(strconv.FormatInt(v.Int(), 10))
			b.WriteByte(';')
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			b.WriteString(strconv.FormatUint(v.Uint(), 10))
			b.WriteByte(';')
		case reflect.Float32, reflect.Float64:
			b.WriteString(strconv.FormatUint(math.Float64bits(v.Convert(reflect.TypeOf(float64(0))).Float()), 16))
			b.WriteByte(';')
		default:
			b.WriteString(";")
		}
	}
	walk(reflect.ValueOf(s))
	return b.String()
}

func TestLP1CheckerIndependentBoundaries(t *testing.T) {
	valid := lifetimeFixture()
	valid.StartDate = "2026-08"
	valid.Lifetime.YTD = models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}
	if _, err := From(valid); err != nil {
		t.Fatalf("explicit zero-history acknowledgment rejected: %v", err)
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.WhatIfSettings
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("explicit zero history did not round-trip: %v", err)
	}
	if decoded.Lifetime == nil || !decoded.Lifetime.YTD.ZeroHistoryAcknowledged {
		t.Fatal("round-trip lost lifetime/YTD acknowledgment")
	}

	invalid := []struct {
		name, want string
		mutate     func(*models.WhatIfSettings)
	}{
		{"nonfinite reserve", "+Inf", func(s *models.WhatIfSettings) { s.Lifetime.CashPolicy.ReserveTarget = math.Inf(1) }},
		{"negative employer fixed", "contribution_rules[rule].employer.fixed_monthly", func(s *models.WhatIfSettings) {
			s.Lifetime.ContributionRules[0].Employer.Mode = "fixed"
			s.Lifetime.ContributionRules[0].Employer.Tiers = nil
			s.Lifetime.ContributionRules[0].Employer.FixedMonthly = -.01
		}},
		{"saving end equals start", "scheduled_savings[saving].end_month", func(s *models.WhatIfSettings) {
			s.Lifetime.ScheduledSavings[0].EndMonth = s.Lifetime.ScheduledSavings[0].StartMonth
		}},
		{"rule start equals linked retirement", "contribution_rules[rule].end_month", func(s *models.WhatIfSettings) {
			s.Lifetime.ContributionRules[0].StartMonth = s.Persons[0].RetirementMonth
		}},
		{"wrong owner scheduled saving destination", "scheduled_savings[saving].destination_account_id", func(s *models.WhatIfSettings) {
			s.Persons = append(s.Persons, models.Person{ID: "q", Name: "Q", Role: models.PersonRoleSpouse, BirthMonth: "1980-01"})
			s.Lifetime.Accounts[1].OwnerID = "q"
		}},
		{"wrong legal type surplus", "cash_policy.surplus_account_id", func(s *models.WhatIfSettings) {
			s.Lifetime.Accounts[1].LegalType = "ira"
			s.Lifetime.Accounts[1].TaxTreatment = "traditional"
			s.Lifetime.Accounts[1].Basis = 0
			s.Lifetime.ScheduledSavings = nil
		}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			s := lifetimeFixture()
			tc.mutate(s)
			before := independentSettingsSnapshot(s)
			_, err := From(s)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want identity %q", err, tc.want)
			}
			if independentSettingsSnapshot(s) != before {
				t.Fatal("rejected prepare mutated input")
			}
			if tc.name == "nonfinite reserve" {
				if err := ValidateLifetime(s); err == nil || !strings.Contains(err.Error(), "cash_policy.reserve_target") {
					t.Fatalf("ValidateLifetime error=%v", err)
				}
			}
		})
	}

	other := lifetimeFixture()
	other.Persons = append(other.Persons, models.Person{ID: "q", Name: "Q", Role: models.PersonRoleSpouse, BirthMonth: "1980-01"})
	other.Lifetime.Accounts[1].OwnerID = "q"
	other.Lifetime.ScheduledSavings = nil
	if _, err := From(other); err != nil {
		t.Fatalf("valid other-owner household surplus rejected: %v", err)
	}

	s := lifetimeFixture()
	p1, err := From(s)
	if err != nil {
		t.Fatal(err)
	}
	first := p1.Settings()
	s.Persons[0].RetirementMonth = "2031-02"
	p2, err := From(s)
	if err != nil {
		t.Fatal(err)
	}
	second := p2.Settings()
	endMonth, err := second.Lifetime.Jobs[0].EffectiveEndMonth(second.Persons)
	if err != nil || endMonth != "2031-02" {
		t.Fatalf("dynamic linked end = %q, %v", endMonth, err)
	}
	if !reflect.DeepEqual(first.IncomeSources, second.IncomeSources) {
		t.Fatal("retirement change altered independently dated income/benefit records")
	}
}

func TestLifetimeScheduledSavingUsesExclusiveEndMonth(t *testing.T) {
	s := lifetimeFixture()
	s.StartDate = "2026-09"
	s.Lifetime.YTD = models.LifetimeYTD{Year: 2026, ZeroHistoryAcknowledged: true}
	s.Lifetime.ScheduledSavings[0].StartMonth = "2026-09"
	s.Lifetime.ScheduledSavings[0].EndMonth = "2026-10"
	if _, err := From(s); err != nil {
		t.Fatalf("one-active-month saving rejected: %v", err)
	}
}

func TestLP1CheckerOmittedVersusExplicitZeroYTDJSON(t *testing.T) {
	omitted := []byte(`{"rule_id":"rule","employee_regular":0,"employee_catch_up":0,"employer":0}`)
	var row models.ContributionYTD
	if err := json.Unmarshal(omitted, &row); err == nil {
		t.Fatal("omitted matching_paid accepted")
	}
	explicit := []byte(`{"rule_id":"rule","employee_regular":0,"employee_catch_up":0,"employer":0,"matching_paid":0}`)
	if err := json.Unmarshal(explicit, &row); err != nil {
		t.Fatalf("explicit zero rejected: %v", err)
	}
	if row.EmployeeRegular != 0 || row.EmployeeCatchUp != 0 || row.Employer != 0 || row.MatchingPaid != 0 {
		t.Fatalf("explicit zeros decoded incorrectly: %+v", row)
	}
}
