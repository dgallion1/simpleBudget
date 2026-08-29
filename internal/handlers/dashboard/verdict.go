package dashboard

import "budget2/internal/models"

// overAmberPct: over budget by up to this fraction of the target total is amber;
// beyond it is red.
const overAmberPct = 0.10

// onBudgetEps is the dollar slack within which spending counts as "on budget"
// rather than over/under (matches the Budget KPI card's threshold).
const onBudgetEps = 1.0

// BudgetVerdictView is the precomputed model the dashboard verdict band renders.
// Classification lives here (testable); currency formatting stays in the template
// (reusing formatMoney), so this carries figures and flags, not display strings.
type BudgetVerdictView struct {
	Health      models.Health
	HasTarget   bool
	Delta       float64 // CombinedCumulativeDelta (>0 = over budget)
	IsOver      bool    // Delta > onBudgetEps
	IsUnder     bool    // Delta < -onBudgetEps
	SpentTotal  float64 // living + healthcare actual for the period
	TargetTotal float64 // living + healthcare target for the period
	Months      float64
	NetSavings  float64
	SavingsRate float64
	TotalIncome float64

	// Eps is onBudgetEps, always set regardless of HasTarget/classification
	// outcome. Exposed so the sparkline-budget chart can apply the same
	// dead band the card's own over/under classification uses, instead of
	// coloring by a bare v>0/v<0 comparison that disagrees with the card.
	Eps float64

	// Living/Healthcare are the per-bucket decomposition of Delta, so the
	// band can say *which* bucket is over/under instead of only the net.
	// A bucket with no target configured is never shown and never counted
	// as "over" — see BucketFigure.
	Living     BucketFigure
	Healthcare BucketFigure

	// Sentence is the plain-English headroom/overage explanation. Kind ""
	// means "don't render a sentence" (mirrors HasTarget being false).
	Sentence VerdictSentence
}

// BucketFigure is one bucket's (living or healthcare) contribution to the
// combined budget delta, classified against onBudgetEps.
type BucketFigure struct {
	Configured bool    // whether this bucket has a target set at all
	Delta      float64 // cumulative actual-minus-target; >0 = over. Only meaningful when Configured.
	Status     string  // "over" | "under" | "on target"; "on target" when unconfigured
}

// classifyBucket buckets a cumulative delta against onBudgetEps. An
// unconfigured bucket is always reported "on target" with a zero delta,
// regardless of whatever degenerate raw value metrics carries for it — a
// bucket with no budget target cannot be "over" or "under" one.
func classifyBucket(delta float64, configured bool) BucketFigure {
	if !configured {
		return BucketFigure{Status: "on target"}
	}
	f := BucketFigure{Configured: true, Delta: delta}
	switch {
	case delta > onBudgetEps:
		f.Status = "over"
	case delta < -onBudgetEps:
		f.Status = "under"
	default:
		f.Status = "on target"
	}
	return f
}

// VerdictSentenceKind selects which plain-English headroom sentence the
// band renders. The literal English lives in the template; this only picks
// the branch and supplies the figures/bucket names it needs.
type VerdictSentenceKind string

const (
	SentenceNone          VerdictSentenceKind = ""      // no sentence (mirrors !HasTarget)
	SentenceBothAtOrUnder VerdictSentenceKind = "both"  // both buckets at-or-under target
	SentenceMixed         VerdictSentenceKind = "mixed" // total under, but one bucket is over
	SentenceOver          VerdictSentenceKind = "over"  // total over target
)

// VerdictSentence carries the figures/bucket names the template needs to
// render exactly one plain-English sentence for Kind.
type VerdictSentence struct {
	Kind VerdictSentenceKind

	// UnderRoom: total headroom available (Kind Both/Mixed). Always >= 0.
	UnderRoom float64
	// UnderBucket: which bucket the headroom comes from (Kind Mixed only).
	UnderBucket string

	// OverAmount: how far over target (Kind Mixed: that bucket's overage;
	// Kind Over: the total overage). Always >= 0.
	OverAmount float64
	// OverBucket: which bucket is over (Kind Mixed only).
	OverBucket string
}

// classifyVerdictSentence picks the headroom sentence from the already
// classified living/healthcare buckets and the total over/under verdict.
// It does no rounding/formatting — that is the template's job via formatMoney.
func classifyVerdictSentence(living, healthcare BucketFigure, totalIsOver bool, totalDelta float64) VerdictSentence {
	if totalIsOver {
		return VerdictSentence{Kind: SentenceOver, OverAmount: totalDelta}
	}

	// room is a headroom magnitude and must never be negative — nor -0.0.
	// -totalDelta on an exact +0.0 totalDelta yields -0.0 in IEEE 754, and
	// -0.0 < 0 is false, so a strict "< 0" clamp lets -0.0 slip through and
	// formatMoney renders it as "$-0.00" (W4 attempt-2 defect). "<= 0"
	// catches that case too, resetting it to the positive zero literal.
	room := -totalDelta
	if room <= 0 {
		room = 0
	}

	switch {
	case living.Configured && living.Status == "over":
		return VerdictSentence{
			Kind: SentenceMixed, UnderRoom: room, UnderBucket: "healthcare",
			OverAmount: living.Delta, OverBucket: "living",
		}
	case healthcare.Configured && healthcare.Status == "over":
		return VerdictSentence{
			Kind: SentenceMixed, UnderRoom: room, UnderBucket: "living",
			OverAmount: healthcare.Delta, OverBucket: "healthcare",
		}
	default:
		return VerdictSentence{Kind: SentenceBothAtOrUnder, UnderRoom: room}
	}
}

// BuildBudgetVerdict derives the dashboard verdict band model from metrics
// already computed for the selected date range. It does no money math beyond
// summing totals the metrics expose.
func BuildBudgetVerdict(m *models.DashboardMetrics) BudgetVerdictView {
	v := BudgetVerdictView{Health: models.HealthNeutral, Eps: onBudgetEps}
	if m == nil {
		return v
	}

	v.NetSavings = m.NetSavings
	v.SavingsRate = m.SavingsRate
	v.TotalIncome = m.TotalIncome
	v.Months = m.MonthsInRange

	if !m.HasCombinedTarget {
		return v
	}

	v.HasTarget = true
	v.Delta = m.CombinedCumulativeDelta
	v.SpentTotal = m.LivingExpensesTotal + m.HealthcareTotal
	v.TargetTotal = m.LivingTargetTotal + m.HealthcareTargetTotal

	v.Living = classifyBucket(m.CumulativeDelta, m.HasBudgetTarget)
	v.Healthcare = classifyBucket(m.HealthcareCumulativeDelta, m.HasHealthcareTarget)

	switch {
	case v.Delta > onBudgetEps:
		v.IsOver = true
		// Over budget by more than overAmberPct of the target is red. When the
		// target total is zero/unknown we can't form a ratio, so any real
		// overage is treated as red rather than silently downgraded to amber.
		if v.TargetTotal <= 0 || v.Delta/v.TargetTotal > overAmberPct {
			v.Health = models.HealthRed
		} else {
			v.Health = models.HealthAmber
		}
	default:
		// On budget or under budget — both healthy.
		if v.Delta < -onBudgetEps {
			v.IsUnder = true
		}
		v.Health = models.HealthGreen
	}

	v.Sentence = classifyVerdictSentence(v.Living, v.Healthcare, v.IsOver, v.Delta)
	return v
}
