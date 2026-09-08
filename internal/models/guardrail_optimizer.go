package models

// GuardrailOptimizerRequest describes a real living-spending floor and the
// minimum simulated success percentage. Seed zero requests a recorded random seed.
type GuardrailOptimizerRequest struct {
	FloorMonthlyReal float64 `json:"floor_monthly_real"`
	TargetSuccessPct float64 `json:"target_success_pct"`
	Seed             int64   `json:"seed"`
}

// GuardrailOptimizerResult is a preview; producing it never changes settings.
// Every candidate metric uses the same held-out validation scenarios.
type GuardrailOptimizerResult struct {
	Request         GuardrailOptimizerRequest     `json:"request"`
	Seed            int64                         `json:"seed"`
	ValidationSeed  int64                         `json:"validation_seed"`
	SearchRuns      int                           `json:"search_runs"`
	ValidationRuns  int                           `json:"validation_runs"`
	Candidates      []GuardrailOptimizerCandidate `json:"candidates"`
	Recommendations []string                      `json:"recommendations"`
	HorizonMinYears int                           `json:"horizon_min_years"`
	HorizonMaxYears int                           `json:"horizon_max_years"`
}

// GuardrailOptimizerCandidate contains a policy plus independently measured
// outcomes. Baselines are comparison-only, even when they qualify.
type GuardrailOptimizerCandidate struct {
	ID         string                    `json:"id"`
	Labels     []string                  `json:"labels"`
	Guardrails *GuardrailConfig          `json:"guardrails"`
	Metrics    GuardrailOptimizerMetrics `json:"metrics"`
	Qualifies  bool                      `json:"qualifies"`
	Baseline   bool                      `json:"baseline"`
}

// GuardrailOptimizerMetrics uses funded living in today's dollars, never
// requested spending. Percentages are unrounded; qualification uses counts.
// Percentiles use linear interpolation between ordered observations.
type GuardrailOptimizerMetrics struct {
	Runs                           int     `json:"runs"`
	FloorShortfallPaths            int     `json:"floor_shortfall_paths"`
	DepletionPaths                 int     `json:"depletion_paths"`
	FloorSuccessPct                float64 `json:"floor_success_pct"`
	FloorSuccessCILowPct           float64 `json:"floor_success_ci_low_pct"`
	FloorSuccessCIHighPct          float64 `json:"floor_success_ci_high_pct"`
	DepletionRiskPct               float64 `json:"depletion_risk_pct"`
	MedianLifetimeFundedLivingReal float64 `json:"median_lifetime_funded_living_real"`
	P10LifetimeFundedLivingReal    float64 `json:"p10_lifetime_funded_living_real"`
	P95WorstAnnualCutPct           float64 `json:"p95_worst_annual_cut_pct"`
	MeanAnnualCutCount             float64 `json:"mean_annual_cut_count"`
	MedianEndingBalanceReal        float64 `json:"median_ending_balance_real"`
}
