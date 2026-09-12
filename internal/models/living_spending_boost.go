package models

// LivingSpendingBoost adds cuttable living spending in today's dollars until
// StopMonth, the first calendar month without the boost.
type LivingSpendingBoost struct {
	MonthlyReal float64 `json:"monthly_real"`
	StopMonth   string  `json:"stop_month"`
}

// CloneLivingSpendingBoost returns an independent copy, preserving disabled nil.
func CloneLivingSpendingBoost(boost *LivingSpendingBoost) *LivingSpendingBoost {
	if boost == nil {
		return nil
	}
	clone := *boost
	return &clone
}
