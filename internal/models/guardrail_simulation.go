package models

// GuardrailPathYear captures a complete projection year from one simulated path.
// Living values are average funded monthly spending; portfolio values are year-end.
type GuardrailPathYear struct {
	Year             int     `json:"year"`
	LivingReal       float64 `json:"living_real"`
	LivingNominal    float64 `json:"living_nominal"`
	PortfolioReal    float64 `json:"portfolio_real"`
	PortfolioNominal float64 `json:"portfolio_nominal"`
}

// GuardrailPercentiles describes pointwise percentiles, not a single future path.
type GuardrailPercentiles struct {
	P10 float64 `json:"p10"`
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
}

// GuardrailSimulationYear summarizes only paths observed through this complete year.
type GuardrailSimulationYear struct {
	Year             int                  `json:"year"`
	Paths            int                  `json:"paths"`
	LivingReal       GuardrailPercentiles `json:"living_real"`
	LivingNominal    GuardrailPercentiles `json:"living_nominal"`
	PortfolioReal    GuardrailPercentiles `json:"portfolio_real"`
	PortfolioNominal GuardrailPercentiles `json:"portfolio_nominal"`
}
