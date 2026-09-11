package analysis

import (
	"fmt"
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// PrepareSpendingCandidate returns a complete engine input with only the
// candidate's saved living budget, boost schedule, and guardrails replaced.
func PrepareSpendingCandidate(in engine.Input, c models.SpendingCandidate) (engine.Input, error) {
	if err := validateSpendingCandidateInput(in); err != nil {
		return engine.Input{}, err
	}
	if !spendingCandidateFinite(c.BaseMonthlyLivingExpenses) || c.BaseMonthlyLivingExpenses <= 0 {
		return engine.Input{}, fmt.Errorf("candidate base monthly living expenses must be finite and positive")
	}

	cloned, err := prepare.Clone(in.Prepared.Settings())
	if err != nil {
		return engine.Input{}, err
	}
	cloned.MonthlyLivingExpenses = c.BaseMonthlyLivingExpenses
	cloned.Guardrails = guardrailOptimizerCloneConfig(c.Guardrails)
	cloned.LivingSpendingBoost = models.CloneLivingSpendingBoost(c.LivingSpendingBoost)
	prepared, err := prepare.From(cloned)
	if err != nil {
		return engine.Input{}, err
	}
	out := in
	out.Prepared = prepared
	return out, nil
}

func spendingCandidateForStart(in engine.Input, start, floor float64,
	boost *models.LivingSpendingBoost, policy *models.GuardrailConfig,
	kind, id string,
) (models.SpendingCandidate, error) {
	if err := validateSpendingCandidateInput(in); err != nil {
		return models.SpendingCandidate{}, err
	}

	s := in.Prepared.Settings()
	if kind == "current" {
		actualStart := engine.LivingExpensesAtMonth(s, 0)
		if !spendingCandidateFinite(s.MonthlyLivingExpenses) || s.MonthlyLivingExpenses <= 0 ||
			!spendingCandidateFinite(actualStart) || actualStart <= 0 {
			return models.SpendingCandidate{}, fmt.Errorf("current plan has invalid starting living expenses")
		}
		return models.SpendingCandidate{
			ID:                        id,
			Kind:                      kind,
			BaseMonthlyLivingExpenses: s.MonthlyLivingExpenses,
			StartingMonthlyLivingReal: actualStart,
			Guardrails:                guardrailOptimizerCloneConfig(s.Guardrails),
			LivingSpendingBoost:       models.CloneLivingSpendingBoost(s.LivingSpendingBoost),
			Baseline:                  true,
		}, nil
	}

	if !spendingCandidateFinite(start) || start <= 0 {
		return models.SpendingCandidate{}, fmt.Errorf("candidate starting monthly living must be finite and positive")
	}
	if !spendingCandidateFinite(floor) || floor <= 0 {
		return models.SpendingCandidate{}, fmt.Errorf("minimum monthly living must be finite and positive")
	}
	if spendingCandidateCents(start) < spendingCandidateCents(floor) {
		return models.SpendingCandidate{}, fmt.Errorf("candidate starting monthly living must be at least the minimum")
	}

	unit, err := prepare.Clone(s)
	if err != nil {
		return models.SpendingCandidate{}, err
	}
	unit.MonthlyLivingExpenses = 1
	unit.LivingSpendingBoost = nil
	unitPrepared, err := prepare.From(unit)
	if err != nil {
		return models.SpendingCandidate{}, err
	}
	factor := engine.LivingExpensesAtMonth(unitPrepared.Settings(), 0)
	if !spendingCandidateFinite(factor) || factor <= 0 {
		return models.SpendingCandidate{}, fmt.Errorf("candidate month-zero living factor must be finite and positive")
	}

	startMonth, err := models.ParseYearMonth(s.StartDate)
	if err != nil {
		return models.SpendingCandidate{}, err
	}
	activeBoost := engine.LivingSpendingBoostAtMonth(boost, startMonth, 1)
	base := (start - activeBoost) / factor
	if !spendingCandidateFinite(base) || base <= 0 {
		return models.SpendingCandidate{}, fmt.Errorf("candidate must leave a positive nonboost base living budget")
	}

	candidate := models.SpendingCandidate{
		ID:                        id,
		Kind:                      kind,
		BaseMonthlyLivingExpenses: base,
		StartingMonthlyLivingReal: start,
		Guardrails:                guardrailOptimizerCloneConfig(policy),
		LivingSpendingBoost:       models.CloneLivingSpendingBoost(boost),
	}
	preparedCandidate, err := PrepareSpendingCandidate(in, candidate)
	if err != nil {
		return models.SpendingCandidate{}, err
	}
	canonicalStart := engine.LivingExpensesAtMonth(preparedCandidate.Prepared.Settings(), 0)
	if !spendingCandidateFinite(canonicalStart) || spendingCandidateCents(canonicalStart) != spendingCandidateCents(start) {
		return models.SpendingCandidate{}, fmt.Errorf("candidate canonical starting living does not match requested amount")
	}
	return candidate, nil
}

func validateSpendingCandidateInput(in engine.Input) error {
	if in.Prepared.IsZero() {
		return fmt.Errorf("prepared retirement settings are required")
	}
	if len(in.Chain) > 0 || len(in.Prepared.Settings().ScenarioChain) > 0 {
		return fmt.Errorf("spending candidates do not support chained scenarios")
	}
	return nil
}

func spendingCandidateFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func spendingCandidateCents(value float64) int64 {
	return int64(math.Round(value * 100))
}
