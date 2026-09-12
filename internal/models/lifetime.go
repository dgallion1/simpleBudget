package models

import (
	"budget2/internal/moneyfmt"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// LifetimeSettingsVersion is the additive account/employment schema version.
const LifetimeSettingsVersion = 1

// LifetimeSettings replaces legacy aggregate opening balances when present.
// Accounts are the complete account registry, never an addition to PortfolioValue.
// StartMonth is derived from WhatIfSettings.StartDate during preparation.
type LifetimeSettings struct {
	Version           int                `json:"version"`
	StartMonth        string             `json:"start_month,omitempty"`
	Accounts          []LifetimeAccount  `json:"accounts"`
	Jobs              []LifetimeJob      `json:"jobs,omitempty"`
	ContributionRules []ContributionRule `json:"contribution_rules,omitempty"`
	ScheduledSavings  []ScheduledSaving  `json:"scheduled_savings,omitempty"`
	CashPolicy        LifetimeCashPolicy `json:"cash_policy"`
	YTD               LifetimeYTD        `json:"ytd"`
}

// PlanCapabilities are explicit plan provisions, not inferred from account names.
type PlanCapabilities struct {
	AllowCatchUp                        bool `json:"allow_catch_up"`
	AllowRothCatchUp                    bool `json:"allow_roth_catch_up"`
	AllowEmployeeRoth                   bool `json:"allow_employee_roth"`
	AllowEmployerRoth                   bool `json:"allow_employer_roth"`
	RestrictEmployeeCompensationToLimit bool `json:"restrict_employee_compensation_to_limit"`
}

// LifetimeAccount keeps legal identity separate from tax treatment. LegalType is
// cash, brokerage, ira, 401k, 403b or 457b; TaxTreatment is taxable, traditional
// or roth. Brokerage/IRA basis may exceed current value after a market loss.
// PlanID and EmployerID may be absent for unlinked balances; contributions
// require explicit workplace relationships. Allocation percentages sum to 100.
type LifetimeAccount struct {
	ID                         string           `json:"id"`
	Name                       string           `json:"name,omitempty"`
	OwnerID                    string           `json:"owner_id"`
	LegalType                  string           `json:"legal_type"`
	TaxTreatment               string           `json:"tax_treatment"`
	PlanID                     string           `json:"plan_id,omitempty"`
	EmployerID                 string           `json:"employer_id,omitempty"`
	LimitGroup                 string           `json:"limit_group,omitempty"`
	Capabilities               PlanCapabilities `json:"capabilities"`
	OpeningValue               float64          `json:"opening_value"`
	Basis                      float64          `json:"basis"`
	StockPercent               float64          `json:"stock_percent"`
	BondPercent                float64          `json:"bond_percent"`
	CashPercent                float64          `json:"cash_percent"`
	RothFirstFundedYear        int              `json:"roth_first_funded_year,omitempty"`
	PriorDecemberValue         *float64         `json:"prior_december_value,omitempty"`
	RMDPaidYTD                 float64          `json:"rmd_paid_ytd,omitempty"`
	CurrentEmployerRMDDeferral bool             `json:"current_employer_rmd_deferral,omitempty"`
	OwnerMoreThanFivePercent   bool             `json:"owner_more_than_five_percent,omitempty"`
}

// LifetimeJob annual compensation is expressed in dollars and GrowthPercent in
// percent (2 means 2%). StartMonth is inclusive; EndMonth is exclusive. A linked
// end uses the owner's RetirementMonth and leaves independent benefits alone.
// IncomeSourceID explicitly replaces that legacy income in the lifetime engine.
// PriorSponsorWages are prior-calendar-year Social Security wages at this sponsor.
type LifetimeJob struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name,omitempty"`
	OwnerID              string  `json:"owner_id"`
	EmployerID           string  `json:"employer_id"`
	GrossSalary          float64 `json:"gross_salary"`
	EligibleCompensation float64 `json:"eligible_compensation"`
	GrowthPercent        float64 `json:"growth_percent"`
	StartMonth           string  `json:"start_month"`
	EndMonth             string  `json:"end_month,omitempty"`
	EndAtRetirement      bool    `json:"end_at_retirement"`
	IncomeSourceID       string  `json:"income_source_id,omitempty"`
	PriorSponsorWages    float64 `json:"prior_sponsor_wages"`
}

// ContributionRate has exactly one mode: fixed monthly dollars or percentage
// of eligible compensation. An explicit zero is a valid pause in contributions.
type ContributionRate struct {
	Mode                  string  `json:"mode"`
	FixedMonthly          float64 `json:"fixed_monthly"`
	PercentOfCompensation float64 `json:"percent_of_compensation"`
}

type ContributionChange struct {
	Month string           `json:"month"`
	Rate  ContributionRate `json:"rate"`
}

type ContributionRule struct {
	ID                   string                `json:"id"`
	JobID                string                `json:"job_id"`
	StartMonth           string                `json:"start_month"`
	EndMonth             string                `json:"end_month,omitempty"`
	EndAtRetirement      bool                  `json:"end_at_retirement"`
	Rate                 ContributionRate      `json:"rate"`
	Changes              []ContributionChange  `json:"changes,omitempty"`
	RothPercent          float64               `json:"roth_percent"`
	TraditionalAccountID string                `json:"traditional_account_id,omitempty"`
	RothAccountID        string                `json:"roth_account_id,omitempty"`
	Employer             *EmployerContribution `json:"employer,omitempty"`
}

// EmployerContribution is fully vested. MatchTiming must be monthly; annual
// TrueUp is optional and departure-year eligibility requires an explicit opt-in.
// Mode is fixed, percent (unconditional), or match (incremental tiers).
type EmployerContribution struct {
	Mode                  string      `json:"mode"`
	FixedMonthly          float64     `json:"fixed_monthly"`
	PercentOfCompensation float64     `json:"percent_of_compensation"`
	DestinationAccountID  string      `json:"destination_account_id"`
	Tiers                 []MatchTier `json:"tiers,omitempty"`
	MatchTiming           string      `json:"match_timing"`
	TrueUp                bool        `json:"true_up"`
	DepartureTrueUp       bool        `json:"departure_true_up"`
}

type ScheduledSaving struct {
	ID                   string  `json:"id"`
	OwnerID              string  `json:"owner_id"`
	DestinationAccountID string  `json:"destination_account_id"`
	MonthlyAmount        float64 `json:"monthly_amount"`
	StartMonth           string  `json:"start_month"`
	EndMonth             string  `json:"end_month,omitempty"`
}

type LifetimeCashPolicy struct {
	ReserveAccountID string   `json:"reserve_account_id"`
	ReserveTarget    float64  `json:"reserve_target"`
	SurplusAccountID string   `json:"surplus_account_id"`
	WithdrawalOrder  []string `json:"withdrawal_order"`
}

// Midyear plans require either explicit zero history or complete rule/job rows
// for the opening calendar year. Job pay is separate to avoid duplicating it
// when multiple contribution rules share a job. MatchingPaid is part of Employer.
type LifetimeYTD struct {
	Year                    int               `json:"year"`
	ZeroHistoryAcknowledged bool              `json:"zero_history_acknowledged"`
	Rules                   []ContributionYTD `json:"rules,omitempty"`
	Jobs                    []JobYTD          `json:"jobs,omitempty"`
}

type ContributionYTD struct {
	RuleID          string  `json:"rule_id"`
	EmployeeRegular float64 `json:"employee_regular"`
	EmployeeCatchUp float64 `json:"employee_catch_up"`
	Employer        float64 `json:"employer"`
	MatchingPaid    float64 `json:"matching_paid"`
}

type JobYTD struct {
	JobID       string  `json:"job_id"`
	GrossWages  float64 `json:"gross_wages"`
	EligiblePay float64 `json:"eligible_pay"`
}

type MatchTier struct{ FromPercent, ToPercent, MatchPercent float64 }
type ContributionAmounts struct {
	Requested, Permitted, Funded, Unfunded                               float64
	EmployeeTraditional, EmployeeRoth, EmployerTraditional, EmployerRoth float64
}
type AccountMovement struct {
	Month, Sequence                                  int
	SourceID, DestinationID, OwnerID, RuleID, Reason string
	Amount, OrdinaryIncome, CapitalGain, BasisChange float64
}
type AccountReconciliation struct {
	AccountID, AccountName                          string
	Opening, Deposits, Withdrawals, Return, Closing float64
	RoundingAdjustment                              float64
}

// LifetimeRMDFact is the canonical per-account record for one calendar year's
// legal obligation and the distributions credited against it.
type LifetimeRMDFact struct {
	AccountID   string  `json:"account_id"`
	AccountName string  `json:"account_name,omitempty"`
	OwnerID     string  `json:"owner_id,omitempty"`
	Year        int     `json:"year"`
	Obligation  float64 `json:"obligation"`
	Distributed float64 `json:"distributed"`
	Credited    float64 `json:"credited"`
	Taxable     float64 `json:"taxable"`
}

// LifetimeMonthOutcome records one committed account-authoritative month.
type LifetimeMonthOutcome struct {
	Year                                         int
	Month                                        int
	Movements                                    []AccountMovement
	Contributions                                map[string]ContributionAmounts
	Accounts                                     []AccountReconciliation
	RMD                                          []LifetimeRMDFact
	ExternalIncome                               float64
	EmployeeContributions, EmployerContributions float64
	ConsumptionAssessed, ConsumptionPaid         float64
	IRMAAAssessed, IRMAAPaid                     float64
	AllSourceReinvestment                        float64
	TaxLiability, TaxPayments, UnpaidTax         float64
	EarlyDistributionTax                         float64
	UnfundedSaving, ReserveGap, UnfundedExpenses float64
}

// LifetimeYearSummary aggregates committed records for one actual calendar
// year. Flow fields sum; ReserveGap is the final modeled month's stock.
type LifetimeYearSummary struct {
	Year, FirstMonth, LastMonth                                    int
	Available, PartialYear                                         bool
	UnavailableReason                                              string
	Movements                                                      []AccountMovement
	Accounts                                                       []AccountReconciliation
	RMD                                                            []LifetimeRMDFact
	ExternalIncome, EmployeeContributions, EmployerContributions   float64
	Distributions, Transfers, Consumption, ConsumptionAssessed     float64
	IRMAAAssessed, IRMAAPaid                                       float64
	TaxLiability, TaxPayments, UnpaidTax                           float64
	InvestmentReturn, UnfundedSaving, UnfundedExpenses, ReserveGap float64
	OpeningWealth, ClosingWealth, HouseholdRoundingAdjustment      float64
	AllSourceReinvestment                                          float64
}

// EffectiveEndMonth resolves a retirement link without rewriting authored dates.
func (j LifetimeJob) EffectiveEndMonth(persons []Person) (string, error) {
	return lifetimeEndMonth(j.OwnerID, j.EndMonth, j.EndAtRetirement, persons)
}

// EffectiveEndMonth limits the rule to its job and its own independent or linked
// end. Empty means indefinite. The exclusive boundary does not move benefits.
func (r ContributionRule) EffectiveEndMonth(job LifetimeJob, persons []Person) (string, error) {
	end, err := lifetimeEndMonth(job.OwnerID, r.EndMonth, r.EndAtRetirement, persons)
	if err != nil {
		return "", err
	}
	jobEnd, err := job.EffectiveEndMonth(persons)
	if err != nil {
		return "", err
	}
	if jobEnd != "" && (end == "" || jobEnd < end) {
		end = jobEnd
	}
	return end, nil
}

func lifetimeEndMonth(owner, end string, linked bool, persons []Person) (string, error) {
	if !linked {
		return end, nil
	}
	if end != "" {
		return "", fmt.Errorf("end_month and end_at_retirement are mutually exclusive")
	}
	for _, p := range persons {
		if p.ID == owner {
			if p.RetirementMonth == "" {
				return "", fmt.Errorf("retirement_month required for owner %q", owner)
			}
			return p.RetirementMonth, nil
		}
	}
	return "", fmt.Errorf("owner_id %q not found", owner)
}

// UnmarshalJSON requires explicit YTD amounts at the input boundary. Numeric
// fields remain convenient for calculations; supplied zero and omitted differ.
func (v *ContributionYTD) UnmarshalJSON(data []byte) error {
	type plain ContributionYTD
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if err := requireLifetimeJSONAmounts(data, "employee_regular", "employee_catch_up", "employer", "matching_paid"); err != nil {
		return err
	}
	*v = ContributionYTD(next)
	return nil
}

// UnmarshalJSON rejects incomplete wage history instead of assuming zero wages.
func (v *JobYTD) UnmarshalJSON(data []byte) error {
	type plain JobYTD
	var next plain
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	if err := requireLifetimeJSONAmounts(data, "gross_wages", "eligible_pay"); err != nil {
		return err
	}
	*v = JobYTD(next)
	return nil
}

func requireLifetimeJSONAmounts(data []byte, fields ...string) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	for _, field := range fields {
		v, ok := values[field]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("lifetime.ytd.%s: explicit amount required (zero is valid)", field)
		}
	}
	return nil
}

func lifetimeEquation(terms ...float64) (residual, tolerance float64, valid bool) {
	scale := 0.0
	for _, term := range terms {
		if math.IsNaN(term) || math.IsInf(term, 0) {
			return 0, 0, false
		}
		residual += term
		if math.IsNaN(residual) || math.IsInf(residual, 0) {
			return 0, 0, false
		}
		scale = math.Max(scale, math.Abs(term))
	}
	ulp := math.Nextafter(scale, math.Inf(1)) - scale
	tolerance = math.Min(0.0001, math.Max(0.000001, 16*ulp))
	return residual, tolerance, true
}

func lifetimeDisplayAdjustment(rawTolerance float64, signedTerms ...float64) (float64, bool) {
	var cents int64
	for _, term := range signedTerms {
		value, err := moneyfmt.Cents(term)
		if err != nil {
			return 0, false
		}
		maxInt64 := int64(^uint64(0) >> 1)
		minInt64 := -maxInt64 - 1
		if (value > 0 && cents > maxInt64-value) || (value < 0 && cents < minInt64-value) {
			return 0, false
		}
		cents += value
	}
	adjustment := -float64(cents) / 100
	envelope := float64(len(signedTerms))*.005 + rawTolerance
	return adjustment, math.Abs(adjustment) <= envelope
}

// AggregateLifetimeYear is the single adapter from committed monthly records
// to an annual explanation. It never reruns financial rules.
func AggregateLifetimeYear(outcomes []LifetimeMonthOutcome) LifetimeYearSummary {
	var out LifetimeYearSummary
	if len(outcomes) == 0 {
		out.UnavailableReason = "no committed lifetime months"
		return out
	}
	ordered := append([]LifetimeMonthOutcome(nil), outcomes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Year != ordered[j].Year {
			return ordered[i].Year < ordered[j].Year
		}
		return ordered[i].Month < ordered[j].Month
	})
	out.Year, out.FirstMonth, out.LastMonth = ordered[0].Year, ordered[0].Month, ordered[0].Month
	accounts := map[string]*AccountReconciliation{}
	accountIDs := map[string]bool{}
	for _, m := range ordered {
		for _, a := range m.Accounts {
			accountIDs[a.AccountID] = true
		}
	}
	rmd := map[string]*LifetimeRMDFact{}
	lastMonth := 0
	for _, m := range ordered {
		if m.Year != out.Year {
			out.UnavailableReason = "lifetime year summary contains multiple calendar years"
			return out
		}
		if m.Month < 1 || m.Month > 12 || m.Month == lastMonth {
			out.UnavailableReason = "lifetime year summary has an invalid or duplicate calendar month"
			return out
		}
		lastMonth = m.Month
		if m.Month < out.FirstMonth {
			out.FirstMonth = m.Month
		}
		if m.Month > out.LastMonth {
			out.LastMonth = m.Month
		}
		out.Movements = append(out.Movements, m.Movements...)
		out.ExternalIncome += m.ExternalIncome
		out.EmployeeContributions += m.EmployeeContributions
		out.EmployerContributions += m.EmployerContributions
		out.Consumption += m.ConsumptionPaid
		out.ConsumptionAssessed += m.ConsumptionAssessed
		out.IRMAAAssessed += m.IRMAAAssessed
		out.IRMAAPaid += m.IRMAAPaid
		out.TaxLiability += m.TaxLiability
		out.TaxPayments += m.TaxPayments
		out.UnpaidTax += m.UnpaidTax
		out.UnfundedSaving += m.UnfundedSaving
		out.UnfundedExpenses += m.UnfundedExpenses
		out.ReserveGap = m.ReserveGap
		out.AllSourceReinvestment += m.AllSourceReinvestment
		for _, a := range m.Accounts {
			accountResidual, tolerance, valid := lifetimeEquation(a.Opening, a.Deposits, -a.Withdrawals, a.Return, -a.Closing)
			if !valid || math.Abs(accountResidual) > tolerance {
				out.UnavailableReason = fmt.Sprintf("account %s does not reconcile", a.AccountID)
				return out
			}
			v := accounts[a.AccountID]
			if v == nil {
				x := a
				accounts[a.AccountID] = &x
			} else {
				continuityResidual, continuityTolerance, valid := lifetimeEquation(v.Closing, -a.Opening)
				if !valid || math.Abs(continuityResidual) > continuityTolerance {
					out.UnavailableReason = fmt.Sprintf("account %s month openings are not continuous", a.AccountID)
					return out
				}
				v.Deposits += a.Deposits
				v.Withdrawals += a.Withdrawals
				v.Return += a.Return
				v.Closing = a.Closing
			}
			out.InvestmentReturn += a.Return
		}
		for _, f := range m.RMD {
			v := rmd[f.AccountID]
			if v == nil {
				x := f
				rmd[f.AccountID] = &x
			} else {
				if f.Obligation > v.Obligation {
					v.Obligation = f.Obligation
				}
				v.Distributed += f.Distributed
				v.Credited += f.Credited
				v.Taxable += f.Taxable
			}
		}
		for _, mv := range m.Movements {
			if accountIDs[mv.SourceID] && accountIDs[mv.DestinationID] {
				out.Transfers += mv.Amount
			}
			if mv.Reason == "required_distribution" {
				out.Distributions += mv.Amount
			}
		}
	}
	ids := make([]string, 0, len(accounts))
	for id := range accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Accounts = append(out.Accounts, *accounts[id])
	}
	for i := range out.Accounts {
		a := &out.Accounts[i]
		_, tolerance, valid := lifetimeEquation(a.Opening, a.Deposits, -a.Withdrawals, a.Return, -a.Closing)
		if !valid {
			out.UnavailableReason = fmt.Sprintf("account %s has nonfinite reconciliation values", a.AccountID)
			return out
		}
		adjustment, ok := lifetimeDisplayAdjustment(tolerance, a.Opening, a.Deposits, -a.Withdrawals, a.Return, -a.Closing)
		if !ok {
			out.UnavailableReason = fmt.Sprintf("account %s display rounding adjustment exceeds its cent-rounding envelope", a.AccountID)
			return out
		}
		a.RoundingAdjustment = adjustment
	}
	for _, a := range out.Accounts {
		out.OpeningWealth += a.Opening
		out.ClosingWealth += a.Closing
	}
	householdResidual, householdTolerance, valid := lifetimeEquation(out.OpeningWealth, out.ExternalIncome, out.EmployerContributions, out.InvestmentReturn, -out.Consumption, -out.TaxPayments, -out.ClosingWealth)
	if !valid || math.Abs(householdResidual) > householdTolerance {
		out.UnavailableReason = "household lifetime records do not reconcile"
		return out
	}
	adjustment, ok := lifetimeDisplayAdjustment(householdTolerance, out.OpeningWealth, out.ExternalIncome, out.EmployerContributions, out.InvestmentReturn, -out.Consumption, -out.TaxPayments, -out.ClosingWealth)
	if !ok {
		out.UnavailableReason = "household display rounding adjustment exceeds its cent-rounding envelope"
		return out
	}
	out.HouseholdRoundingAdjustment = adjustment
	ids = ids[:0]
	for id := range rmd {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.RMD = append(out.RMD, *rmd[id])
	}
	out.PartialYear = out.FirstMonth != 1 || out.LastMonth != 12 || len(ordered) != 12
	out.Available = true
	return out
}

// OpeningTotal is the authoritative opening household account value.
func (s *LifetimeSettings) OpeningTotal() float64 {
	if s == nil {
		return 0
	}
	total := 0.0
	for _, a := range s.Accounts {
		total += a.OpeningValue
	}
	return total
}

// LifetimeAccountDiagnostic is a bounded final-state observation for parity tests.
type LifetimeAccountDiagnostic struct {
	AccountID    string
	Value, Basis float64
}

// LifetimeDiagnostics is emitted only when internal capture is requested.
type LifetimeDiagnostics struct {
	Accounts                                     []LifetimeAccountDiagnostic
	EmployeeContributions, EmployerContributions float64
	TaxLiability, TaxPayments                    float64
	RMDObligation, RMDDistributed                float64
	RMDObligationByAccountYear                   map[string]float64 `json:"-"`
}
