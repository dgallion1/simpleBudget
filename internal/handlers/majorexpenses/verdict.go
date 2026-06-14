package majorexpenses

// TrackingHealth classifies how much of a period's spending is categorized into
// declared major expenses, for the verdict band tint.
type TrackingHealth string

const (
	TrackingGreen   TrackingHealth = "green"
	TrackingAmber   TrackingHealth = "amber"
	TrackingRed     TrackingHealth = "red"
	TrackingNeutral TrackingHealth = "neutral"
)

// Tracking-coverage thresholds (percent of spend matched to declared expenses).
const (
	trackingGoodPct = 80.0 // >= this is green
	trackingOkPct   = 50.0 // >= this (and < good) is amber; below is red
)

// TrackingVerdictView is the precomputed model the major-expenses verdict band
// renders. Coverage = declared / (declared + unmatched).
type TrackingVerdictView struct {
	Health         TrackingHealth
	HasSpend       bool
	DeclaredTotal  float64
	UnmatchedTotal float64
	UnmatchedCount int
	TrackedPercent float64 // 0-100
}

// BuildTrackingVerdict classifies tracking coverage from the declared and
// unmatched spend already computed for the active window. Returns a neutral,
// no-spend view when there is nothing to track.
func BuildTrackingVerdict(declared, unmatched float64, unmatchedCount int) TrackingVerdictView {
	total := declared + unmatched
	if total <= 0 {
		return TrackingVerdictView{Health: TrackingNeutral}
	}

	pct := declared / total * 100
	v := TrackingVerdictView{
		HasSpend:       true,
		DeclaredTotal:  declared,
		UnmatchedTotal: unmatched,
		UnmatchedCount: unmatchedCount,
		TrackedPercent: pct,
	}
	switch {
	case pct >= trackingGoodPct:
		v.Health = TrackingGreen
	case pct >= trackingOkPct:
		v.Health = TrackingAmber
	default:
		v.Health = TrackingRed
	}
	return v
}
