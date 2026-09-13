package models

// SpendingPathOutcome records funded living and funding failures for one
// complete simulated path. Month indexes are one-based.
type SpendingPathOutcome struct {
	MonthsObserved                     int     `json:"months_observed"`
	NearTermMonths                     int     `json:"near_term_months"`
	NearTermFundedLivingReal           float64 `json:"near_term_funded_living_real"`
	MinFundedMonthlyReal               float64 `json:"min_funded_monthly_real"`
	MinFundedMonth                     int     `json:"min_funded_month"`
	FloorShortfallMonths               int     `json:"floor_shortfall_months"`
	LongestFloorShortfallMonths        int     `json:"longest_floor_shortfall_months"`
	LargestFloorGapReal                float64 `json:"largest_floor_gap_real"`
	UnpaidObligationMonths             int     `json:"unpaid_obligation_months"`
	DepletionMonth                     int     `json:"depletion_month"`
	FloorShortfallMonthsAfterDepletion int     `json:"floor_shortfall_months_after_depletion"`
	FirstCutMonth                      int     `json:"first_cut_month"`
	MonthsBelowPlan                    int     `json:"months_below_plan"`
	LongestBelowPlanMonths             int     `json:"longest_below_plan_months"`
	MaxCutReal                         float64 `json:"max_cut_real"`
	MaxCutPct                          float64 `json:"max_cut_pct"`
	BelowPlanAtEnd                     bool    `json:"below_plan_at_end"`
}

// SpendingOptimizerRequest defines the user's minimum and bounded living-budget search.
// MaxShortfallPct is the largest share of checked futures (0–100, exclusive of
// 100) that may fall below the minimum for a budget to qualify; 0 requires
// every future to fund it.
type SpendingOptimizerRequest struct {
	FloorMonthlyReal      float64              `json:"floor_monthly_real"`
	MaxShortfallPct       float64              `json:"max_shortfall_pct"`
	NearTermYears         int                  `json:"near_term_years"`
	LivingSpendingBoost   *LivingSpendingBoost `json:"living_spending_boost"`
	SearchMinMonthlyReal  float64              `json:"search_min_monthly_real"`
	SearchMaxMonthlyReal  float64              `json:"search_max_monthly_real"`
	SearchStepMonthlyReal float64              `json:"search_step_monthly_real"`
	Seed                  int64                `json:"seed,string"`
}

// SpendingCandidate is one fully specified plan measured by the spending optimizer.
type SpendingCandidate struct {
	ID                        string                    `json:"id"`
	Kind                      string                    `json:"kind"`
	BaseMonthlyLivingExpenses float64                   `json:"base_monthly_living_expenses"`
	StartingMonthlyLivingReal float64                   `json:"starting_monthly_living_real"`
	Guardrails                *GuardrailConfig          `json:"guardrails"`
	LivingSpendingBoost       *LivingSpendingBoost      `json:"living_spending_boost"`
	Baseline                  bool                      `json:"baseline"`
	Qualifies                 bool                      `json:"qualifies"`
	Metrics                   *SpendingRiskMetrics      `json:"metrics"`
	SimulationYears           []GuardrailSimulationYear `json:"simulation_years"`
	WorstPathYears            []GuardrailPathYear       `json:"worst_path_years"`
	WorstPathIndex            int                       `json:"worst_path_index"`
}

// SpendingRiskMetrics aggregates observed funded living and failure paths.
type SpendingRiskMetrics struct {
	Runs                        int                       `json:"runs"`
	CutPaths                    int                       `json:"cut_paths"`
	EarlyCutPaths               int                       `json:"early_cut_paths"`
	FloorShortfallPaths         int                       `json:"floor_shortfall_paths"`
	UnpaidObligationPaths       int                       `json:"unpaid_obligation_paths"`
	DepletionPaths              int                       `json:"depletion_paths"`
	DepletionFloorFundedPaths   int                       `json:"depletion_floor_funded_paths"`
	CutPathsStillBelowPlan      int                       `json:"cut_paths_still_below_plan"`
	MedianNearTermMonthlyReal   float64                   `json:"median_near_term_monthly_real"`
	P10NearTermMonthlyReal      float64                   `json:"p10_near_term_monthly_real"`
	MedianFirstCutMonth         *float64                  `json:"median_first_cut_month"`
	MedianMaxCutReal            *float64                  `json:"median_max_cut_real"`
	P95MaxCutReal               *float64                  `json:"p95_max_cut_real"`
	MedianMaxCutPct             *float64                  `json:"median_max_cut_pct"`
	P95MaxCutPct                *float64                  `json:"p95_max_cut_pct"`
	MedianMonthsBelowPlan       *float64                  `json:"median_months_below_plan"`
	P95LongestBelowPlanMonths   *float64                  `json:"p95_longest_below_plan_months"`
	LowestObservedMonthlyReal   float64                   `json:"lowest_observed_monthly_real"`
	LargestFloorGapReal         float64                   `json:"largest_floor_gap_real"`
	LowestObservedMonth         int                       `json:"lowest_observed_month"`
	WorstPathIndex              int                       `json:"worst_path_index"`
	LongestFloorShortfallMonths int                       `json:"longest_floor_shortfall_months"`
	FloorEvidence               GuardrailOptimizerMetrics `json:"floor_evidence"`
}

// SpendingOptimizerResult contains only independently final-validated candidates.
// Seeds are decimal strings in JSON to preserve their exact values in browsers.
type SpendingOptimizerResult struct {
	Request                 SpendingOptimizerRequest `json:"request"`
	SearchSeed              int64                    `json:"search_seed,string"`
	SelectionSeed           int64                    `json:"selection_seed,string"`
	ValidationSeed          int64                    `json:"validation_seed,string"`
	SearchRuns              int                      `json:"search_runs"`
	SelectionRuns           int                      `json:"selection_runs"`
	ValidationRuns          int                      `json:"validation_runs"`
	EvaluatedCandidates     int                      `json:"evaluated_candidates"`
	EffectiveMinMonthlyReal float64                  `json:"effective_min_monthly_real"`
	EffectiveMaxMonthlyReal float64                  `json:"effective_max_monthly_real"`
	ResolutionMonthlyReal   float64                  `json:"resolution_monthly_real"`
	RangeLimited            bool                     `json:"range_limited"`
	HorizonMinYears         int                      `json:"horizon_min_years"`
	HorizonMaxYears         int                      `json:"horizon_max_years"`
	Candidates              []SpendingCandidate      `json:"candidates"`
	RecommendationIDs       []string                 `json:"recommendation_ids"`
}

// SpendingFundingMonth records one observed canonical projection month. All
// monetary values are monthly amounts in today's dollars.
type SpendingFundingMonth struct {
	Month                     int     `json:"month"`
	CalendarMonth             string  `json:"calendar_month"`
	SocialSecurityReal        float64 `json:"social_security_real"`
	OtherConfiguredIncomeReal float64 `json:"other_configured_income_real"`
	TaxDeferredWithdrawalReal float64 `json:"tax_deferred_withdrawal_real"`
	TaxableWithdrawalReal     float64 `json:"taxable_withdrawal_real"`
	RothWithdrawalReal        float64 `json:"roth_withdrawal_real"`
	TaxesPaidReal             float64 `json:"taxes_paid_real"`
	PlannedLivingReal         float64 `json:"planned_living_real"`
	FundedLivingReal          float64 `json:"funded_living_real"`
	HealthcareReal            float64 `json:"healthcare_real"`
}

// SpendingFundingAnnualAverage is the average observed monthly amount for one
// calendar year. Partial years divide by ObservedMonths, never by twelve.
type SpendingFundingAnnualAverage struct {
	CalendarYear              int     `json:"calendar_year"`
	ObservedMonths            int     `json:"observed_months"`
	Partial                   bool    `json:"partial"`
	SocialSecurityReal        float64 `json:"social_security_real"`
	OtherConfiguredIncomeReal float64 `json:"other_configured_income_real"`
	TaxDeferredWithdrawalReal float64 `json:"tax_deferred_withdrawal_real"`
	TaxableWithdrawalReal     float64 `json:"taxable_withdrawal_real"`
	RothWithdrawalReal        float64 `json:"roth_withdrawal_real"`
	TaxesPaidReal             float64 `json:"taxes_paid_real"`
	PlannedLivingReal         float64 `json:"planned_living_real"`
	FundedLivingReal          float64 `json:"funded_living_real"`
	HealthcareReal            float64 `json:"healthcare_real"`
}

// SpendingFundingMarker identifies a configured event whose actual month is
// present in the retained canonical evidence.
type SpendingFundingMarker struct {
	Month         int    `json:"month"`
	CalendarMonth string `json:"calendar_month"`
	Kind          string `json:"kind"`
	Label         string `json:"label"`
}

// SpendingFundingTimeline explains the observed income, account withdrawals,
// taxes, and spending for the selected candidate's canonical base case.
type SpendingFundingTimeline struct {
	Title            string                         `json:"title"`
	Months           []SpendingFundingMonth         `json:"months"`
	AnnualAverages   []SpendingFundingAnnualAverage `json:"annual_averages"`
	Markers          []SpendingFundingMarker        `json:"markers"`
	StartMonth       string                         `json:"start_month"`
	ObservedEndMonth string                         `json:"observed_end_month"`
	ExpectedEndMonth string                         `json:"expected_end_month"`
	ExpectedMonths   int                            `json:"expected_months"`
	ObservedMonths   int                            `json:"observed_months"`
	Complete         bool                           `json:"complete"`
	EndReason        string                         `json:"end_reason"`
	AccountingNote   string                         `json:"accounting_note"`
	CoverageNote     string                         `json:"coverage_note"`
	AvailabilityNote string                         `json:"availability_note,omitempty"`
}
