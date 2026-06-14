package insights

import "budget2/internal/models"

// PaceHealth classifies spending pace (burn rate vs. historical) for the
// insights verdict band tint.
type PaceHealth string

const (
	PaceGreen   PaceHealth = "green"
	PaceAmber   PaceHealth = "amber"
	PaceRed     PaceHealth = "red"
	PaceNeutral PaceHealth = "neutral"
)

// paceRedThreshold: burn rate more than this many percent above the historical
// daily average is "red"; at-or-below 0 is "green"; in between is "amber".
// This mirrors the existing Daily Spending KPI tile semantic exactly.
const paceRedThreshold = 10.0

// PaceVerdictView is the precomputed model the insights verdict band renders.
type PaceVerdictView struct {
	Health          PaceHealth
	HasData         bool
	BurnRateChange  float64 // % vs historical daily average (>0 = faster than usual)
	IsAbove         bool    // BurnRateChange > 0
	IsBelow         bool    // BurnRateChange < 0
	DailyAverage    float64
	HistoricalDaily float64
	MonthProjection float64
}

// BuildPaceVerdict derives the insights verdict band model from the spending
// velocity already computed for the selected range. Returns a neutral, no-data
// view when velocity is unavailable.
func BuildPaceVerdict(v *models.SpendingVelocity) PaceVerdictView {
	if v == nil {
		return PaceVerdictView{Health: PaceNeutral}
	}

	pv := PaceVerdictView{
		HasData:         true,
		BurnRateChange:  v.BurnRateChange,
		IsAbove:         v.BurnRateChange > 0,
		IsBelow:         v.BurnRateChange < 0,
		DailyAverage:    v.DailyAverage,
		HistoricalDaily: v.HistoricalDaily,
		MonthProjection: v.MonthProjection,
	}

	switch {
	case v.BurnRateChange < 0:
		pv.Health = PaceGreen
	case v.BurnRateChange > paceRedThreshold:
		pv.Health = PaceRed
	default:
		pv.Health = PaceAmber
	}
	return pv
}
