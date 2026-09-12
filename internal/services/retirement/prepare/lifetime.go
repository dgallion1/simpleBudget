package prepare

import (
	"fmt"
	"math"
	"strings"
	"time"

	"budget2/internal/models"
)

// ValidateLifetime validates only the additive model; nil retains legacy behavior.
// It never normalizes, infers employers, changes dates, or mutates the input.
func ValidateLifetime(s *models.WhatIfSettings) error {
	if s == nil {
		return fmt.Errorf("lifetime: nil settings")
	}
	l := s.Lifetime
	if l == nil {
		return nil
	}
	if l.Version != models.LifetimeSettingsVersion {
		return fmt.Errorf("lifetime.version: unsupported version %d", l.Version)
	}
	start, err := time.Parse("2006-01", s.StartDate)
	if err != nil {
		return fmt.Errorf("lifetime.start_month: %w", err)
	}
	if l.StartMonth != "" && l.StartMonth != s.StartDate {
		return fmt.Errorf("lifetime.start_month must equal start_date")
	}
	persons := map[string]models.Person{}
	for _, p := range s.Persons {
		if err := uniqueLifetimeID("persons", p.ID, persons); err != nil {
			return err
		}
		persons[p.ID] = p
		if p.RetirementMonth != "" {
			if err := lifetimeDate("persons.retirement_month", p.RetirementMonth); err != nil {
				return err
			}
			if p.RetirementMonth < p.BirthMonth {
				return fmt.Errorf("persons.retirement_month precedes birth_month")
			}
		}
	}
	accounts := map[string]models.LifetimeAccount{}
	plans := map[string]models.LifetimeAccount{}
	for _, a := range l.Accounts {
		path := "lifetime.accounts[" + a.ID + "]"
		if err := uniqueLifetimeID("lifetime.accounts", a.ID, accounts); err != nil {
			return err
		}
		if _, ok := persons[a.OwnerID]; !ok {
			return fmt.Errorf("%s.owner_id not found", path)
		}
		if err := validateLifetimeAccount(path, a); err != nil {
			return err
		}
		if a.PlanID != "" {
			if prev, ok := plans[a.PlanID]; ok && (prev.OwnerID != a.OwnerID || prev.EmployerID != a.EmployerID || prev.LegalType != a.LegalType || prev.LimitGroup != a.LimitGroup || prev.Capabilities != a.Capabilities) {
				return fmt.Errorf("%s.plan_id: inconsistent plan metadata", path)
			}
			plans[a.PlanID] = a
		}
		accounts[a.ID] = a
	}
	jobs := map[string]models.LifetimeJob{}
	linked := map[string]bool{}
	incomes := map[string]int{}
	for _, income := range s.IncomeSources {
		incomes[income.ID]++
	}
	for _, j := range l.Jobs {
		path := "lifetime.jobs[" + j.ID + "]"
		if err := uniqueLifetimeID("lifetime.jobs", j.ID, jobs); err != nil {
			return err
		}
		if _, ok := persons[j.OwnerID]; !ok {
			return fmt.Errorf("%s.owner_id not found", path)
		}
		if strings.TrimSpace(j.EmployerID) == "" {
			return fmt.Errorf("%s.employer_id required", path)
		}
		if err := lifetimeAmounts(path, map[string]float64{"gross_salary": j.GrossSalary, "eligible_compensation": j.EligibleCompensation, "prior_sponsor_wages": j.PriorSponsorWages}); err != nil {
			return err
		}
		if j.EligibleCompensation > j.GrossSalary {
			return fmt.Errorf("%s.eligible_compensation exceeds gross_salary", path)
		}
		if !finiteLifetime(j.GrowthPercent) || j.GrowthPercent < -100 {
			return fmt.Errorf("%s.growth_percent must be finite and at least -100", path)
		}
		end, err := j.EffectiveEndMonth(s.Persons)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := lifetimeRange(path, j.StartMonth, end); err != nil {
			return err
		}
		if j.IncomeSourceID != "" {
			if incomes[j.IncomeSourceID] != 1 || linked[j.IncomeSourceID] {
				return fmt.Errorf("%s.income_source_id must identify one uniquely linked income source", path)
			}
			linked[j.IncomeSourceID] = true
		}
		jobs[j.ID] = j
	}
	rules := map[string]models.ContributionRule{}
	for _, r := range l.ContributionRules {
		path := "lifetime.contribution_rules[" + r.ID + "]"
		if err := uniqueLifetimeID("lifetime.contribution_rules", r.ID, rules); err != nil {
			return err
		}
		j, ok := jobs[r.JobID]
		if !ok {
			return fmt.Errorf("%s.job_id not found", path)
		}
		end, err := r.EffectiveEndMonth(j, s.Persons)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := lifetimeRange(path, r.StartMonth, end); err != nil {
			return err
		}
		if r.EndMonth != "" {
			if err := lifetimeRange(path, r.StartMonth, r.EndMonth); err != nil {
				return err
			}
		}
		if r.StartMonth < j.StartMonth {
			return fmt.Errorf("%s.start_month precedes job", path)
		}
		if err := validateContributionRate(path+".rate", r.Rate); err != nil {
			return err
		}
		if !lifetimePercent(r.RothPercent) {
			return fmt.Errorf("%s.roth_percent must be between 0 and 100", path)
		}
		var planID string
		destination := func(id, tax string, required, employer bool) error {
			if id == "" && !required {
				return nil
			}
			a, ok := accounts[id]
			if !ok {
				return fmt.Errorf("%s.%s_account: destination not found", path, tax)
			}
			if a.TaxTreatment != tax {
				return fmt.Errorf("%s.%s_account: incompatible tax treatment", path, tax)
			}
			if !lifetimeWorkplace(a.LegalType) || a.PlanID == "" || a.EmployerID != j.EmployerID || a.OwnerID != j.OwnerID || a.LimitGroup == "" {
				return fmt.Errorf("%s.%s_account: explicit owner, plan and employer relationship required", path, tax)
			}
			if planID != "" && planID != a.PlanID {
				return fmt.Errorf("%s: split destinations must share plan_id", path)
			}
			planID = a.PlanID
			if tax == "roth" && ((employer && !a.Capabilities.AllowEmployerRoth) || (!employer && !a.Capabilities.AllowEmployeeRoth)) {
				return fmt.Errorf("%s.roth_account: plan capability required", path)
			}
			return nil
		}
		if err := destination(r.TraditionalAccountID, "traditional", r.RothPercent < 100, false); err != nil {
			return err
		}
		if err := destination(r.RothAccountID, "roth", r.RothPercent > 0, false); err != nil {
			return err
		}
		last := r.StartMonth
		for _, c := range r.Changes {
			if err := lifetimeDate(path+".changes.month", c.Month); err != nil {
				return err
			}
			if c.Month <= last || (end != "" && c.Month >= end) {
				return fmt.Errorf("%s.changes must be ordered, unique and within rule dates", path)
			}
			if err := validateContributionRate(path+".changes.rate", c.Rate); err != nil {
				return err
			}
			last = c.Month
		}
		if r.Employer != nil {
			e := r.Employer
			a, ok := accounts[e.DestinationAccountID]
			if !ok {
				return fmt.Errorf("%s.employer.destination_account not found", path)
			}
			if err := destination(e.DestinationAccountID, a.TaxTreatment, true, true); err != nil {
				return err
			}
			if err := validateEmployerContribution(path+".employer", *e); err != nil {
				return err
			}
		}
		rules[r.ID] = r
	}
	savings := map[string]models.ScheduledSaving{}
	for _, r := range l.ScheduledSavings {
		path := "lifetime.scheduled_savings[" + r.ID + "]"
		if err := uniqueLifetimeID("lifetime.scheduled_savings", r.ID, savings); err != nil {
			return err
		}
		if _, ok := rules[r.ID]; ok {
			return fmt.Errorf("%s.id duplicates contribution rule", path)
		}
		if _, ok := persons[r.OwnerID]; !ok {
			return fmt.Errorf("%s.owner_id not found", path)
		}
		a, ok := accounts[r.DestinationAccountID]
		if !ok || a.LegalType != "brokerage" || a.TaxTreatment != "taxable" || a.OwnerID != r.OwnerID {
			return fmt.Errorf("%s.destination_account_id must be owner's taxable brokerage", path)
		}
		if err := lifetimeAmounts(path, map[string]float64{"monthly_amount": r.MonthlyAmount}); err != nil {
			return err
		}
		if err := lifetimeRange(path, r.StartMonth, r.EndMonth); err != nil {
			return err
		}
		savings[r.ID] = r
	}
	cash := l.CashPolicy
	if err := lifetimeAmounts("lifetime.cash_policy", map[string]float64{"reserve_target": cash.ReserveTarget}); err != nil {
		return err
	}
	if a, ok := accounts[cash.ReserveAccountID]; !ok || a.LegalType != "cash" || a.TaxTreatment != "taxable" {
		return fmt.Errorf("lifetime.cash_policy.reserve_account_id must identify cash")
	}
	if a, ok := accounts[cash.SurplusAccountID]; !ok || a.LegalType != "brokerage" || a.TaxTreatment != "taxable" {
		return fmt.Errorf("lifetime.cash_policy.surplus_account_id must identify taxable brokerage")
	}
	seen := map[string]bool{}
	for _, id := range cash.WithdrawalOrder {
		if _, ok := accounts[id]; !ok || seen[id] {
			return fmt.Errorf("lifetime.cash_policy.withdrawal_order must contain unique existing accounts")
		}
		seen[id] = true
	}
	if len(cash.WithdrawalOrder) == 0 {
		return fmt.Errorf("lifetime.cash_policy.withdrawal_order required")
	}
	return validateLifetimeYTD(l, start, jobs, rules)
}

func finiteLifetime(v float64) bool      { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func lifetimePercent(v float64) bool     { return finiteLifetime(v) && v >= 0 && v <= 100 }
func lifetimeWorkplace(kind string) bool { return kind == "401k" || kind == "403b" || kind == "457b" }
func uniqueLifetimeID[T any](path, id string, seen map[string]T) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
		return fmt.Errorf("%s.id must be nonempty without surrounding whitespace", path)
	}
	if _, ok := seen[id]; ok {
		return fmt.Errorf("%s: duplicate id %q", path, id)
	}
	return nil
}
func lifetimeAmounts(path string, values map[string]float64) error {
	for field, value := range values {
		if !finiteLifetime(value) || value < 0 {
			return fmt.Errorf("%s.%s must be finite and nonnegative", path, field)
		}
	}
	return nil
}
func lifetimeDate(path, value string) error {
	if len(value) != 7 {
		return fmt.Errorf("%s must be YYYY-MM", path)
	}
	parsed, err := time.Parse("2006-01", value)
	if err != nil || parsed.Year() < 1 {
		return fmt.Errorf("%s must be a valid YYYY-MM", path)
	}
	return nil
}
func lifetimeRange(path, start, end string) error {
	if err := lifetimeDate(path+".start_month", start); err != nil {
		return err
	}
	if end != "" {
		if err := lifetimeDate(path+".end_month", end); err != nil {
			return err
		}
		if end <= start {
			return fmt.Errorf("%s.end_month must follow start_month (exclusive)", path)
		}
	}
	return nil
}
func validateLifetimeAccount(path string, a models.LifetimeAccount) error {
	switch a.LegalType {
	case "cash", "brokerage":
		if a.TaxTreatment != "taxable" {
			return fmt.Errorf("%s.tax_treatment must be taxable", path)
		}
	case "ira", "401k", "403b", "457b":
		if a.TaxTreatment != "traditional" && a.TaxTreatment != "roth" {
			return fmt.Errorf("%s.tax_treatment must be traditional or roth", path)
		}
	default:
		return fmt.Errorf("%s.legal_type unsupported", path)
	}
	if err := lifetimeAmounts(path, map[string]float64{"opening_value": a.OpeningValue, "basis": a.Basis}); err != nil {
		return err
	}
	if a.PriorDecemberValue != nil {
		if err := lifetimeAmounts(path, map[string]float64{"prior_december_value": *a.PriorDecemberValue}); err != nil {
			return err
		}
	}
	if err := lifetimeAmounts(path, map[string]float64{"rmd_paid_ytd": a.RMDPaidYTD}); err != nil {
		return err
	}
	if a.RothFirstFundedYear < 0 {
		return fmt.Errorf("%s.roth_first_funded_year must be nonnegative", path)
	}
	if a.RothFirstFundedYear != 0 && a.TaxTreatment != "roth" {
		return fmt.Errorf("%s.roth_first_funded_year requires Roth account", path)
	}
	if a.TaxTreatment == "roth" && a.OpeningValue > 0 && a.RothFirstFundedYear == 0 {
		return fmt.Errorf("%s.roth_first_funded_year required for positive opening Roth balance", path)
	}
	if (a.CurrentEmployerRMDDeferral || a.OwnerMoreThanFivePercent) && !lifetimeWorkplace(a.LegalType) {
		return fmt.Errorf("%s.current-employer RMD facts require workplace account", path)
	}
	if a.CurrentEmployerRMDDeferral && a.EmployerID == "" {
		return fmt.Errorf("%s.current-employer RMD deferral requires employer", path)
	}
	if !lifetimePercent(a.StockPercent) || !lifetimePercent(a.BondPercent) || !lifetimePercent(a.CashPercent) || math.Abs(a.StockPercent+a.BondPercent+a.CashPercent-100) > 1e-8 {
		return fmt.Errorf("%s.allocation must sum to 100 percent", path)
	}
	if a.LegalType == "cash" && (a.CashPercent != 100 || a.Basis != 0) {
		return fmt.Errorf("%s: cash allocation must be 100 percent cash and basis zero", path)
	}
	if !lifetimeWorkplace(a.LegalType) && (a.PlanID != "" || a.EmployerID != "" || a.LimitGroup != "" || a.Capabilities != (models.PlanCapabilities{})) {
		return fmt.Errorf("%s: employer plan metadata requires workplace legal_type", path)
	}
	if (a.PlanID == "") != (a.EmployerID == "") {
		return fmt.Errorf("%s: plan_id and employer_id must be supplied together", path)
	}
	if a.PlanID == "" && (a.LimitGroup != "" || a.Capabilities != (models.PlanCapabilities{})) {
		return fmt.Errorf("%s: plan metadata requires explicit plan_id", path)
	}
	if a.Capabilities.AllowRothCatchUp && (!a.Capabilities.AllowCatchUp || !a.Capabilities.AllowEmployeeRoth) {
		return fmt.Errorf("%s.plan: Roth catch-up requires catch-up and employee Roth capabilities", path)
	}
	return nil
}
func validateContributionRate(path string, r models.ContributionRate) error {
	if err := lifetimeAmounts(path, map[string]float64{"fixed_monthly": r.FixedMonthly, "percent_of_compensation": r.PercentOfCompensation}); err != nil {
		return err
	}
	switch r.Mode {
	case "fixed":
		if r.PercentOfCompensation != 0 {
			return fmt.Errorf("%s.percent_of_compensation must be zero for fixed mode", path)
		}
	case "percent":
		if r.FixedMonthly != 0 {
			return fmt.Errorf("%s.fixed_monthly must be zero for percent mode", path)
		}
		if r.PercentOfCompensation > 100 {
			return fmt.Errorf("%s.percent_of_compensation exceeds 100", path)
		}
	default:
		return fmt.Errorf("%s.mode must be fixed or percent", path)
	}
	return nil
}
func validateEmployerContribution(path string, e models.EmployerContribution) error {
	if err := lifetimeAmounts(path, map[string]float64{"fixed_monthly": e.FixedMonthly, "percent_of_compensation": e.PercentOfCompensation}); err != nil {
		return err
	}
	if e.MatchTiming != "monthly" {
		return fmt.Errorf("%s.match_timing must be monthly", path)
	}
	if e.DepartureTrueUp && !e.TrueUp {
		return fmt.Errorf("%s.departure_true_up requires true_up", path)
	}
	switch e.Mode {
	case "fixed", "percent":
		if err := validateContributionRate(path, models.ContributionRate{Mode: e.Mode, FixedMonthly: e.FixedMonthly, PercentOfCompensation: e.PercentOfCompensation}); err != nil {
			return err
		}
		if len(e.Tiers) > 0 || e.TrueUp || e.DepartureTrueUp {
			return fmt.Errorf("%s.tiers and true_up require match mode", path)
		}
	case "match":
		if e.FixedMonthly != 0 || e.PercentOfCompensation != 0 || len(e.Tiers) == 0 {
			return fmt.Errorf("%s.tiers required and fixed/percent amounts must be zero for match", path)
		}
		last := 0.0
		for _, tier := range e.Tiers {
			if !lifetimePercent(tier.FromPercent) || !lifetimePercent(tier.ToPercent) || !finiteLifetime(tier.MatchPercent) || tier.MatchPercent < 0 || tier.ToPercent <= tier.FromPercent || tier.FromPercent < last {
				return fmt.Errorf("%s.tiers must be ordered nonoverlapping positive bands with nonnegative rates", path)
			}
			last = tier.ToPercent
		}
	default:
		return fmt.Errorf("%s.mode must be fixed, percent or match", path)
	}
	return nil
}
func validateLifetimeYTD(l *models.LifetimeSettings, start time.Time, jobs map[string]models.LifetimeJob, rules map[string]models.ContributionRule) error {
	y := l.YTD
	hasHistory := len(y.Rules) > 0 || len(y.Jobs) > 0
	if (hasHistory || y.ZeroHistoryAcknowledged || y.Year != 0) && y.Year != start.Year() {
		return fmt.Errorf("lifetime.ytd.year must equal start calendar year")
	}
	if y.ZeroHistoryAcknowledged && hasHistory {
		return fmt.Errorf("lifetime.ytd: zero history acknowledgment cannot be combined with history rows")
	}
	seenRules := map[string]bool{}
	for _, v := range y.Rules {
		r, ok := rules[v.RuleID]
		if !ok || seenRules[v.RuleID] {
			return fmt.Errorf("lifetime.ytd.rules: rule_id must exist and be unique")
		}
		seenRules[v.RuleID] = true
		if err := lifetimeAmounts("lifetime.ytd.rules", map[string]float64{"employee_regular": v.EmployeeRegular, "employee_catch_up": v.EmployeeCatchUp, "employer": v.Employer, "matching_paid": v.MatchingPaid}); err != nil {
			return err
		}
		if v.MatchingPaid > v.Employer {
			return fmt.Errorf("lifetime.ytd.matching_paid exceeds employer")
		}
		if r.Employer == nil && v.Employer != 0 {
			return fmt.Errorf("lifetime.ytd.employer requires employer rule")
		}
		if v.MatchingPaid != 0 && (r.Employer == nil || r.Employer.Mode != "match") {
			return fmt.Errorf("lifetime.ytd.matching_paid requires match rule")
		}
	}
	seenJobs := map[string]bool{}
	for _, v := range y.Jobs {
		if _, ok := jobs[v.JobID]; !ok || seenJobs[v.JobID] {
			return fmt.Errorf("lifetime.ytd.jobs: job_id must exist and be unique")
		}
		seenJobs[v.JobID] = true
		if err := lifetimeAmounts("lifetime.ytd.jobs", map[string]float64{"gross_wages": v.GrossWages, "eligible_pay": v.EligiblePay}); err != nil {
			return err
		}
		if v.EligiblePay > v.GrossWages {
			return fmt.Errorf("lifetime.ytd.eligible_pay exceeds gross_wages")
		}
	}
	if start.Month() != time.January && !y.ZeroHistoryAcknowledged {
		for id := range jobs {
			if !seenJobs[id] {
				return fmt.Errorf("lifetime.ytd.jobs: history or explicit zero acknowledgment required for midyear start")
			}
		}
		for id := range rules {
			if !seenRules[id] {
				return fmt.Errorf("lifetime.ytd.rules: history or explicit zero acknowledgment required for midyear start")
			}
		}
	}
	return nil
}
