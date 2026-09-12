package engine

import (
	"fmt"
	"math"
	"sort"
	"time"

	"budget2/internal/models"
)

// SponsorKey keeps prior-year Social Security wages scoped to one participant
// and sponsor without relying on an ambiguous string delimiter.
type SponsorKey struct{ OwnerID, EmployerID string }

// ContributionYearState is proposed calendar-year state. Maps are copied on
// entry so iterative projection trials cannot mutate their shared opening.
type ContributionYearState struct {
	Year                       int
	LastMonth                  int
	Initialized                bool
	EmployeeRegularByOwner     map[string]float64
	EmployeeCatchUpByOwner     map[string]float64
	AdditionsByPlan            map[string]float64
	EligibleCompensationByPlan map[string]float64
	EligibleCompensationByJob  map[string]float64
	GrossCompensationByPlan    map[string]float64
	GrossCompensationByJob     map[string]float64
	MatchingPaidByRule         map[string]float64
	EmployeeByRule             map[string]float64
	SponsorWages               map[SponsorKey]float64
	PriorSponsorWages          map[SponsorKey]float64
}

type ContributionMonth struct {
	AmountsByRule            map[string]models.ContributionAmounts
	Movements                []models.AccountMovement
	Next                     ContributionYearState
	OrdinaryIncomeAdjustment float64
	PayrollWages             float64
	PayrollWagesByOwner      map[string]float64
	GrossPayByJob            map[string]float64
	Policy                   ContributionPolicy
}

// MatchForCompensation applies validated incremental bands to actual employee
// contributions. Its result is independent of tier order and input state.
func MatchForCompensation(compensation, employee float64, tiers []models.MatchTier) float64 {
	if compensation <= 0 || employee <= 0 {
		return 0
	}
	var matched float64
	for _, tier := range tiers {
		from := compensation * tier.FromPercent / 100
		to := compensation * tier.ToPercent / 100
		eligible := math.Min(employee, to) - from
		if eligible > 0 {
			matched += eligible * tier.MatchPercent / 100
		}
	}
	return matched
}

func cloneStringFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneSponsorMap(in map[SponsorKey]float64) map[SponsorKey]float64 {
	if in == nil {
		return nil
	}
	out := make(map[SponsorKey]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneContributionYearState(in ContributionYearState) ContributionYearState {
	out := in
	out.EmployeeRegularByOwner = cloneStringFloatMap(in.EmployeeRegularByOwner)
	out.EmployeeCatchUpByOwner = cloneStringFloatMap(in.EmployeeCatchUpByOwner)
	out.AdditionsByPlan = cloneStringFloatMap(in.AdditionsByPlan)
	out.EligibleCompensationByPlan = cloneStringFloatMap(in.EligibleCompensationByPlan)
	out.EligibleCompensationByJob = cloneStringFloatMap(in.EligibleCompensationByJob)
	out.GrossCompensationByPlan = cloneStringFloatMap(in.GrossCompensationByPlan)
	out.GrossCompensationByJob = cloneStringFloatMap(in.GrossCompensationByJob)
	out.MatchingPaidByRule = cloneStringFloatMap(in.MatchingPaidByRule)
	out.EmployeeByRule = cloneStringFloatMap(in.EmployeeByRule)
	out.SponsorWages = cloneSponsorMap(in.SponsorWages)
	out.PriorSponsorWages = cloneSponsorMap(in.PriorSponsorWages)
	return out
}

func ensureContributionMaps(state *ContributionYearState) {
	if state.EmployeeRegularByOwner == nil {
		state.EmployeeRegularByOwner = map[string]float64{}
	}
	if state.EmployeeCatchUpByOwner == nil {
		state.EmployeeCatchUpByOwner = map[string]float64{}
	}
	if state.AdditionsByPlan == nil {
		state.AdditionsByPlan = map[string]float64{}
	}
	if state.EligibleCompensationByPlan == nil {
		state.EligibleCompensationByPlan = map[string]float64{}
	}
	if state.EligibleCompensationByJob == nil {
		state.EligibleCompensationByJob = map[string]float64{}
	}
	if state.GrossCompensationByPlan == nil {
		state.GrossCompensationByPlan = map[string]float64{}
	}
	if state.GrossCompensationByJob == nil {
		state.GrossCompensationByJob = map[string]float64{}
	}
	if state.MatchingPaidByRule == nil {
		state.MatchingPaidByRule = map[string]float64{}
	}
	if state.EmployeeByRule == nil {
		state.EmployeeByRule = map[string]float64{}
	}
	if state.SponsorWages == nil {
		state.SponsorWages = map[SponsorKey]float64{}
	}
	if state.PriorSponsorWages == nil {
		state.PriorSponsorWages = map[SponsorKey]float64{}
	}
}

func emptyContributionYearState(year int) ContributionYearState {
	return ContributionYearState{Year: year,
		EmployeeRegularByOwner: map[string]float64{}, EmployeeCatchUpByOwner: map[string]float64{},
		AdditionsByPlan: map[string]float64{}, EligibleCompensationByPlan: map[string]float64{}, EligibleCompensationByJob: map[string]float64{},
		GrossCompensationByPlan: map[string]float64{}, GrossCompensationByJob: map[string]float64{}, MatchingPaidByRule: map[string]float64{}, EmployeeByRule: map[string]float64{},
		SponsorWages: map[SponsorKey]float64{}, PriorSponsorWages: map[SponsorKey]float64{},
	}
}

func finiteNonnegative(label string, values ...float64) error {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s must be finite", label)
		}
		if v < 0 {
			return fmt.Errorf("%s must be nonnegative", label)
		}
	}
	return nil
}

func validateContributionState(state ContributionYearState) error {
	stringMaps := []map[string]float64{
		state.EmployeeRegularByOwner, state.EmployeeCatchUpByOwner, state.AdditionsByPlan,
		state.EligibleCompensationByPlan, state.EligibleCompensationByJob,
		state.GrossCompensationByPlan, state.GrossCompensationByJob,
		state.MatchingPaidByRule, state.EmployeeByRule,
	}
	for _, values := range stringMaps {
		for _, value := range values {
			if err := finiteNonnegative("contribution year state", value); err != nil {
				return err
			}
		}
	}
	for _, values := range []map[SponsorKey]float64{state.SponsorWages, state.PriorSponsorWages} {
		for _, value := range values {
			if err := finiteNonnegative("contribution year sponsor wages", value); err != nil {
				return err
			}
		}
	}
	return nil
}

func projectionDate(start string, month int) (time.Time, error) {
	if month < 0 {
		return time.Time{}, fmt.Errorf("month must be nonnegative")
	}
	t, err := time.Parse("2006-01", start)
	if err != nil {
		return time.Time{}, fmt.Errorf("start_date: %w", err)
	}
	return t.AddDate(0, month, 0), nil
}
func yearMonth(t time.Time) string { return t.Format("2006-01") }
func activeAt(start, end, current string) bool {
	return current >= start && (end == "" || current < end)
}

func jobRulePlans(l *models.LifetimeSettings, accounts map[string]models.LifetimeAccount) (map[string]string, error) {
	plans := map[string]string{}
	for _, r := range l.ContributionRules {
		accountID := r.TraditionalAccountID
		if accountID == "" {
			accountID = r.RothAccountID
		}
		account, ok := accounts[accountID]
		if !ok {
			return nil, fmt.Errorf("rule %q contribution destination not found", r.ID)
		}
		if account.PlanID == "" {
			return nil, fmt.Errorf("rule %q destination requires plan_id", r.ID)
		}
		if prior, ok := plans[r.JobID]; ok && prior != account.PlanID {
			return nil, fmt.Errorf("job %q contributes to multiple plans", r.JobID)
		}
		plans[r.JobID] = account.PlanID
	}
	return plans, nil
}

func initializeContributionState(settings *models.WhatIfSettings, year int, jobs map[string]models.LifetimeJob, rules map[string]models.ContributionRule, accounts map[string]models.LifetimeAccount, jobPlans map[string]string) (ContributionYearState, error) {
	state := emptyContributionYearState(year)
	for _, job := range settings.Lifetime.Jobs {
		key := SponsorKey{OwnerID: job.OwnerID, EmployerID: job.EmployerID}
		if prior, ok := state.PriorSponsorWages[key]; ok && prior != job.PriorSponsorWages {
			return state, fmt.Errorf("prior sponsor wages disagree for owner %q employer %q", job.OwnerID, job.EmployerID)
		}
		state.PriorSponsorWages[key] = job.PriorSponsorWages
	}
	ytd := settings.Lifetime.YTD
	if ytd.Year != year {
		return state, nil
	}
	for _, row := range ytd.Jobs {
		job, ok := jobs[row.JobID]
		if !ok {
			return state, fmt.Errorf("YTD job %q not found", row.JobID)
		}
		plan := jobPlans[job.ID]
		state.EligibleCompensationByJob[job.ID] += row.EligiblePay
		state.EligibleCompensationByPlan[plan] += row.EligiblePay
		state.GrossCompensationByJob[job.ID] += row.GrossWages
		state.GrossCompensationByPlan[plan] += row.GrossWages
		state.SponsorWages[SponsorKey{OwnerID: job.OwnerID, EmployerID: job.EmployerID}] += row.GrossWages
	}
	for _, row := range ytd.Rules {
		rule, ok := rules[row.RuleID]
		if !ok {
			return state, fmt.Errorf("YTD rule %q not found", row.RuleID)
		}
		job := jobs[rule.JobID]
		plan := jobPlans[job.ID]
		state.EmployeeRegularByOwner[job.OwnerID] += row.EmployeeRegular
		state.EmployeeCatchUpByOwner[job.OwnerID] += row.EmployeeCatchUp
		state.AdditionsByPlan[plan] += row.EmployeeRegular + row.Employer
		state.MatchingPaidByRule[rule.ID] += row.MatchingPaid
		state.EmployeeByRule[rule.ID] += row.EmployeeRegular + row.EmployeeCatchUp
	}
	return state, nil
}

func resetContributionYear(opening ContributionYearState, year int) ContributionYearState {
	next := emptyContributionYearState(year)
	next.PriorSponsorWages = cloneSponsorMap(opening.SponsorWages)
	return next
}

func prepareContributionState(settings *models.WhatIfSettings, current time.Time, opening ContributionYearState, jobs map[string]models.LifetimeJob, rules map[string]models.ContributionRule, accounts map[string]models.LifetimeAccount, jobPlans map[string]string) (ContributionYearState, error) {
	year, month := current.Year(), int(current.Month())
	if !opening.Initialized {
		state, err := initializeContributionState(settings, year, jobs, rules, accounts, jobPlans)
		if err != nil {
			return state, err
		}
		state.Initialized = true
		state.LastMonth = month - 1
		return state, nil
	}
	state := cloneContributionYearState(opening)
	ensureContributionMaps(&state)
	sequential := (state.Year == year && state.LastMonth+1 == month) || (state.Year+1 == year && state.LastMonth == 12 && month == 1)
	if !sequential {
		return state, fmt.Errorf("contribution months must advance sequentially: state %d-%02d, requested %d-%02d", state.Year, state.LastMonth, year, month)
	}
	if state.Year != year {
		state = resetContributionYear(state, year)
		state.Initialized = true
	}
	return state, nil
}

func rulePlan(rule models.ContributionRule, accounts map[string]models.LifetimeAccount) (string, models.PlanCapabilities, error) {
	id := rule.TraditionalAccountID
	if id == "" {
		id = rule.RothAccountID
	}
	a, ok := accounts[id]
	if !ok {
		return "", models.PlanCapabilities{}, fmt.Errorf("rule %q destination %q not found", rule.ID, id)
	}
	return a.PlanID, a.Capabilities, nil
}

func annualJobAmounts(job models.LifetimeJob, year, startYear int) (gross, eligible float64) {
	growthYears := year - startYear
	factor := math.Pow(1+job.GrowthPercent/100, float64(growthYears))
	return job.GrossSalary * factor, job.EligibleCompensation * factor
}

func projectedPlanComp(settings *models.WhatIfSettings, plan string, year, startYear int, jobs map[string]models.LifetimeJob, jobPlans map[string]string) (float64, error) {
	total := 0.0
	for _, job := range settings.Lifetime.Jobs {
		if jobPlans[job.ID] != plan {
			continue
		}
		_, eligibleAnnual := annualJobAmounts(job, year, startYear)
		end, err := job.EffectiveEndMonth(settings.Persons)
		if err != nil {
			return 0, err
		}
		for m := 1; m <= 12; m++ {
			current := fmt.Sprintf("%04d-%02d", year, m)
			if activeAt(job.StartMonth, end, current) {
				total += eligibleAnnual / 12
			}
		}
	}
	return total, nil
}

func ageAtYearEnd(person models.Person, year int) (int, error) {
	birth, err := time.Parse("2006-01", person.BirthMonth)
	if err != nil {
		return 0, fmt.Errorf("owner %q birth_month: %w", person.ID, err)
	}
	return year - birth.Year(), nil
}

func effectiveRate(rule models.ContributionRule, current string) models.ContributionRate {
	rate := rule.Rate
	changes := append([]models.ContributionChange(nil), rule.Changes...)
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Month < changes[j].Month })
	for _, change := range changes {
		if change.Month <= current {
			rate = change.Rate
		}
	}
	return rate
}

func requestedContribution(rate models.ContributionRate, compensation float64) (float64, error) {
	switch rate.Mode {
	case "fixed":
		return rate.FixedMonthly, finiteNonnegative("fixed employee contribution", rate.FixedMonthly)
	case "percent":
		if err := finiteNonnegative("employee contribution percentage", rate.PercentOfCompensation); err != nil {
			return 0, err
		}
		result := compensation * rate.PercentOfCompensation / 100
		if math.IsInf(result, 0) || math.IsNaN(result) {
			return 0, fmt.Errorf("employee contribution calculation must be finite")
		}
		return result, nil
	default:
		return 0, fmt.Errorf("unsupported contribution rate mode %q", rate.Mode)
	}
}

func addMovement(out *ContributionMonth, month, sequence int, source, destination, owner, rule, reason string, amount, ordinary float64) int {
	if amount <= 0 {
		return sequence
	}
	out.Movements = append(out.Movements, models.AccountMovement{Month: month, Sequence: sequence, SourceID: source, DestinationID: destination, OwnerID: owner, RuleID: rule, Reason: reason, Amount: amount, OrdinaryIncome: ordinary})
	return sequence + 1
}

// CalculateContributions proposes one simulated payroll month without mutating
// opening. The input rule slice defines stable priority when shared caps bind.
func CalculateContributions(settings *models.WhatIfSettings, month int, opening ContributionYearState) (ContributionMonth, error) {
	out := ContributionMonth{AmountsByRule: map[string]models.ContributionAmounts{}, PayrollWagesByOwner: map[string]float64{}, GrossPayByJob: map[string]float64{}}
	if settings == nil || settings.Lifetime == nil {
		out.Next = cloneContributionYearState(opening)
		return out, nil
	}
	current, err := projectionDate(settings.StartDate, month)
	if err != nil {
		return out, err
	}
	policy, err := ContributionPolicyForYear(current.Year())
	if err != nil {
		return out, err
	}
	out.Policy = policy
	start, _ := projectionDate(settings.StartDate, 0)
	l := settings.Lifetime
	jobs := map[string]models.LifetimeJob{}
	for _, j := range l.Jobs {
		jobs[j.ID] = j
		if err := finiteNonnegative("job compensation", j.GrossSalary, j.EligibleCompensation, j.PriorSponsorWages); err != nil {
			return out, err
		}
	}
	rules := map[string]models.ContributionRule{}
	for _, r := range l.ContributionRules {
		rules[r.ID] = r
	}
	accounts := map[string]models.LifetimeAccount{}
	for _, a := range l.Accounts {
		accounts[a.ID] = a
	}
	persons := map[string]models.Person{}
	for _, p := range settings.Persons {
		persons[p.ID] = p
	}
	jobPlans, err := jobRulePlans(l, accounts)
	if err != nil {
		return out, err
	}
	state, err := prepareContributionState(settings, current, opening, jobs, rules, accounts, jobPlans)
	if err != nil {
		return out, err
	}
	if err := validateContributionState(state); err != nil {
		return out, err
	}
	currentYM := yearMonth(current)
	grossThisJob := map[string]float64{}
	eligibleThisJob := map[string]float64{}
	employeeBaseThisJob := map[string]float64{}
	for _, job := range l.Jobs {
		end, e := job.EffectiveEndMonth(settings.Persons)
		if e != nil {
			return out, e
		}
		if !activeAt(job.StartMonth, end, currentYM) {
			continue
		}
		annualGross, annualEligible := annualJobAmounts(job, current.Year(), start.Year())
		gross := annualGross / 12
		eligible := annualEligible / 12
		if err := finiteNonnegative("modeled monthly compensation", gross, eligible); err != nil {
			return out, err
		}
		plan := jobPlans[job.ID]
		remainingEligible := math.Max(0, policy.CompensationLimit-state.EligibleCompensationByPlan[plan])
		cappedEligible := math.Min(eligible, remainingEligible)
		remainingGross := math.Max(0, policy.CompensationLimit-state.GrossCompensationByPlan[plan])
		cappedGross := math.Min(gross, remainingGross)
		grossThisJob[job.ID] = gross
		eligibleThisJob[job.ID] = cappedEligible
		employeeBaseThisJob[job.ID] = cappedGross
		state.EligibleCompensationByJob[job.ID] += cappedEligible
		state.EligibleCompensationByPlan[plan] += cappedEligible
		state.GrossCompensationByJob[job.ID] += gross
		state.GrossCompensationByPlan[plan] += cappedGross
		key := SponsorKey{OwnerID: job.OwnerID, EmployerID: job.EmployerID}
		state.SponsorWages[key] += gross
		out.PayrollWages += gross
		out.PayrollWagesByOwner[job.OwnerID] += gross
		out.GrossPayByJob[job.ID] = gross
	}
	sequence := 0
	employeeThisJob := map[string]float64{}
	for _, rule := range l.ContributionRules {
		job, ok := jobs[rule.JobID]
		if !ok {
			return out, fmt.Errorf("rule %q job %q not found", rule.ID, rule.JobID)
		}
		end, e := rule.EffectiveEndMonth(job, settings.Persons)
		if e != nil {
			return out, e
		}
		if !activeAt(rule.StartMonth, end, currentYM) {
			continue
		}
		plan, caps, e := rulePlan(rule, accounts)
		if e != nil {
			return out, e
		}
		if err := finiteNonnegative("employee Roth percentage", rule.RothPercent); err != nil || rule.RothPercent > 100 {
			return out, fmt.Errorf("rule %q Roth percentage must be finite and between 0 and 100", rule.ID)
		}
		base := grossThisJob[job.ID]
		if caps.RestrictEmployeeCompensationToLimit {
			base = employeeBaseThisJob[job.ID]
		}
		requested, e := requestedContribution(effectiveRate(rule, currentYM), base)
		if e != nil {
			return out, fmt.Errorf("rule %q: %w", rule.ID, e)
		}
		person, ok := persons[job.OwnerID]
		if !ok {
			return out, fmt.Errorf("owner %q not found", job.OwnerID)
		}
		age, e := ageAtYearEnd(person, current.Year())
		if e != nil {
			return out, e
		}
		payRemaining := math.Max(0, grossThisJob[job.ID]-employeeThisJob[job.ID])
		capEligibleRequest := math.Min(requested, payRemaining)
		regularRemaining := math.Max(0, policy.RegularDeferralLimit-state.EmployeeRegularByOwner[job.OwnerID])
		annualComp, e := projectedPlanComp(settings, plan, current.Year(), start.Year(), jobs, jobPlans)
		if e != nil {
			return out, e
		}
		additionsCeiling := math.Min(policy.AnnualAdditionsLimit, annualComp)
		if additionsCeiling < state.EligibleCompensationByPlan[plan] {
			additionsCeiling = math.Min(policy.AnnualAdditionsLimit, state.EligibleCompensationByPlan[plan])
		}
		if state.AdditionsByPlan[plan] > additionsCeiling {
			return out, fmt.Errorf("plan %q opening additions %.2f already exceed revised annual cap %.2f", plan, state.AdditionsByPlan[plan], additionsCeiling)
		}
		additionsRemaining := math.Max(0, additionsCeiling-state.AdditionsByPlan[plan])
		regular := math.Min(capEligibleRequest, math.Min(regularRemaining, additionsRemaining))
		remainingRequest := math.Max(0, capEligibleRequest-regular)
		catchLimit := 0.0
		if caps.AllowCatchUp {
			catchLimit = policy.CatchUpLimit(age)
		}
		prior := state.PriorSponsorWages[SponsorKey{OwnerID: job.OwnerID, EmployerID: job.EmployerID}]
		rothCatchUpRequired := policy.RequiresRothCatchUp(prior)
		if rothCatchUpRequired && (!caps.AllowEmployeeRoth || !caps.AllowRothCatchUp) {
			catchLimit = 0
		}
		catchRemaining := math.Max(0, catchLimit-state.EmployeeCatchUpByOwner[job.OwnerID])
		catchUp := math.Min(remainingRequest, catchRemaining)
		permitted := regular + catchUp
		if err := finiteNonnegative("employee result", requested, permitted); err != nil {
			return out, err
		}
		rothRegular := regular * rule.RothPercent / 100
		traditionalRegular := regular - rothRegular
		rothCatch := catchUp * rule.RothPercent / 100
		traditionalCatch := catchUp - rothCatch
		if rothCatchUpRequired {
			rothCatch = catchUp
			traditionalCatch = 0
		}
		employeeRoth := rothRegular + rothCatch
		employeeTraditional := traditionalRegular + traditionalCatch
		if employeeRoth > 0 && (!caps.AllowEmployeeRoth || rule.RothAccountID == "") {
			return out, fmt.Errorf("rule %q employee Roth contribution is not supported", rule.ID)
		}
		if employeeTraditional > 0 && rule.TraditionalAccountID == "" {
			return out, fmt.Errorf("rule %q traditional destination required", rule.ID)
		}
		amounts := models.ContributionAmounts{Requested: requested, Permitted: permitted, Funded: permitted, Unfunded: requested - permitted, EmployeeTraditional: employeeTraditional, EmployeeRoth: employeeRoth}
		employeeThisJob[job.ID] += permitted
		state.EmployeeRegularByOwner[job.OwnerID] += regular
		state.EmployeeCatchUpByOwner[job.OwnerID] += catchUp
		state.AdditionsByPlan[plan] += regular
		state.EmployeeByRule[rule.ID] += permitted
		sequence = addMovement(&out, month, sequence, job.ID, rule.TraditionalAccountID, job.OwnerID, rule.ID, "employee_contribution", employeeTraditional, -employeeTraditional)
		sequence = addMovement(&out, month, sequence, job.ID, rule.RothAccountID, job.OwnerID, rule.ID, "employee_contribution", employeeRoth, 0)
		out.OrdinaryIncomeAdjustment -= employeeTraditional
		if employer := rule.Employer; employer != nil {
			if employer.MatchTiming != "monthly" {
				return out, fmt.Errorf("rule %q unsupported match timing %q", rule.ID, employer.MatchTiming)
			}
			var employerAmount, matching float64
			switch employer.Mode {
			case "fixed":
				employerAmount = employer.FixedMonthly
				e = finiteNonnegative("fixed employer contribution", employerAmount)
			case "percent":
				employerAmount = eligibleThisJob[job.ID] * employer.PercentOfCompensation / 100
				e = finiteNonnegative("employer contribution percentage", employer.PercentOfCompensation, employerAmount)
			case "match":
				matching = MatchForCompensation(eligibleThisJob[job.ID], permitted, employer.Tiers)
				employerAmount = matching
				departure := end != "" && current.AddDate(0, 1, 0).Format("2006-01") == end
				trueUpNow := employer.TrueUp && (current.Month() == time.December || (departure && employer.DepartureTrueUp))
				if trueUpNow {
					entitlement := MatchForCompensation(state.EligibleCompensationByJob[job.ID], state.EmployeeByRule[rule.ID], employer.Tiers)
					extra := math.Max(0, entitlement-(state.MatchingPaidByRule[rule.ID]+matching))
					employerAmount += extra
					matching += extra
				}
			default:
				e = fmt.Errorf("unsupported employer contribution mode %q", employer.Mode)
			}
			if e != nil {
				return out, fmt.Errorf("rule %q: %w", rule.ID, e)
			}
			employerAmount = math.Min(employerAmount, math.Max(0, additionsCeiling-state.AdditionsByPlan[plan]))
			matching = math.Min(matching, employerAmount)
			destination, ok := accounts[employer.DestinationAccountID]
			if !ok {
				return out, fmt.Errorf("rule %q employer destination not found", rule.ID)
			}
			if destination.TaxTreatment == "roth" {
				if !destination.Capabilities.AllowEmployerRoth {
					return out, fmt.Errorf("rule %q employer Roth contribution is not supported", rule.ID)
				}
				amounts.EmployerRoth = employerAmount
				out.OrdinaryIncomeAdjustment += employerAmount
				sequence = addMovement(&out, month, sequence, job.EmployerID, destination.ID, job.OwnerID, rule.ID, "employer_contribution", employerAmount, employerAmount)
			} else if destination.TaxTreatment == "traditional" {
				amounts.EmployerTraditional = employerAmount
				sequence = addMovement(&out, month, sequence, job.EmployerID, destination.ID, job.OwnerID, rule.ID, "employer_contribution", employerAmount, 0)
			} else {
				return out, fmt.Errorf("rule %q unsupported employer destination tax treatment %q", rule.ID, destination.TaxTreatment)
			}
			state.AdditionsByPlan[plan] += employerAmount
			state.MatchingPaidByRule[rule.ID] += matching
		}
		out.AmountsByRule[rule.ID] = amounts
	}
	for label, values := range map[string]map[string]float64{"regular state": state.EmployeeRegularByOwner, "catch-up state": state.EmployeeCatchUpByOwner, "additions state": state.AdditionsByPlan, "matching state": state.MatchingPaidByRule} {
		for _, v := range values {
			if err := finiteNonnegative(label, v); err != nil {
				return out, err
			}
		}
	}
	state.Year = current.Year()
	state.LastMonth = int(current.Month())
	state.Initialized = true
	out.Next = state
	return out, nil
}
