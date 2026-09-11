package analysis

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

type spendingMonthObservation struct {
	Planned   float64
	Adjusted  float64
	Funded    float64
	Shortfall float64
	CPI       float64
	Depleted  bool
}

type spendingExperienceTracker struct {
	floor                   float64
	nearYears               int
	currentBelowPlanMonths  int
	currentBelowFloorMonths int
	valid                   bool
	outcome                 models.SpendingPathOutcome
}

func newSpendingExperienceTracker(floor float64, nearYears int) *spendingExperienceTracker {
	return &spendingExperienceTracker{
		floor: floor, nearYears: nearYears,
		valid: finiteSpendingValue(floor) && floor > 0 && nearYears > 0,
	}
}

func (t *spendingExperienceTracker) observe(m spendingMonthObservation) {
	if t == nil || !t.valid {
		return
	}
	if !finiteSpendingValue(m.Planned) || !finiteSpendingValue(m.Adjusted) ||
		!finiteSpendingValue(m.Funded) || !finiteSpendingValue(m.Shortfall) ||
		!finiteSpendingValue(m.CPI) || m.Planned < 0 || m.Adjusted < 0 || m.Funded < 0 || m.CPI <= 0 {
		t.valid = false
		return
	}

	t.outcome.MonthsObserved++
	month := t.outcome.MonthsObserved
	fundedReal := engine.RoundLivingCents(m.Funded / m.CPI)
	plannedReal := engine.RoundLivingCents(m.Planned / m.CPI)
	floorReal := engine.RoundLivingCents(t.floor)
	unpaidReal := engine.RoundLivingCents((math.Max(0, m.Shortfall) - m.Adjusted) / m.CPI)
	if !finiteSpendingValue(fundedReal) || !finiteSpendingValue(plannedReal) || !finiteSpendingValue(unpaidReal) {
		t.valid = false
		return
	}
	if month <= t.nearYears*12 {
		t.outcome.NearTermFundedLivingReal = engine.RoundLivingCents(t.outcome.NearTermFundedLivingReal + fundedReal)
		t.outcome.NearTermMonths++
	}
	if month == 1 || fundedReal < t.outcome.MinFundedMonthlyReal {
		t.outcome.MinFundedMonthlyReal = fundedReal
		t.outcome.MinFundedMonth = month
	}

	belowPlan := fundedReal < plannedReal
	t.outcome.BelowPlanAtEnd = belowPlan
	if belowPlan {
		if t.outcome.FirstCutMonth == 0 {
			t.outcome.FirstCutMonth = month
		}
		t.outcome.MonthsBelowPlan++
		t.currentBelowPlanMonths++
		t.outcome.LongestBelowPlanMonths = max(t.outcome.LongestBelowPlanMonths, t.currentBelowPlanMonths)
		cut := engine.RoundLivingCents(plannedReal - fundedReal)
		t.outcome.MaxCutReal = math.Max(t.outcome.MaxCutReal, cut)
		if plannedReal > 0 {
			t.outcome.MaxCutPct = math.Max(t.outcome.MaxCutPct, 100*cut/plannedReal)
		}
	} else {
		t.currentBelowPlanMonths = 0
	}

	unpaid := unpaidReal > 0
	if unpaid {
		t.outcome.UnpaidObligationMonths++
	}
	belowFloor := fundedReal < floorReal || unpaid
	if belowFloor {
		t.outcome.FloorShortfallMonths++
		t.currentBelowFloorMonths++
		t.outcome.LongestFloorShortfallMonths = max(t.outcome.LongestFloorShortfallMonths, t.currentBelowFloorMonths)
		t.outcome.LargestFloorGapReal = math.Max(t.outcome.LargestFloorGapReal, engine.RoundLivingCents(math.Max(0, floorReal-fundedReal)))
	} else {
		t.currentBelowFloorMonths = 0
	}

	if t.outcome.DepletionMonth == 0 && m.Depleted {
		t.outcome.DepletionMonth = month
	}
	if m.Depleted && belowFloor {
		t.outcome.FloorShortfallMonthsAfterDepletion++
	}
}

func (t *spendingExperienceTracker) result() *models.SpendingPathOutcome {
	if t == nil || !t.valid {
		return nil
	}
	out := t.outcome
	return &out
}

func finiteSpendingValue(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
