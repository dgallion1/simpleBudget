package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// LowestPlannedLivingReal is the single source for "what is the lowest
// monthly living-expense request your OWN plan design ever schedules,
// before any guardrail multiplier or funding shortfall is applied". It
// exists so every surface that explains a guardrail policy's minimum-
// spending target (the optimizer's floor-field hint, its results notice,
// and each row's "Below target" wording) reads the same figure computed
// the same way, per SPEC.md's one-source-per-figure rule.
//
// amount is the minimum, over every month of the projection, of
// engine.RoundLivingCents(m.PlannedLivingExpenses / m.CumulativeInflation)
// -- PlannedLivingExpenses is already documented as the "pre-guardrail-
// multiplier living expense for the month", so this figure reflects the
// plan's spending-phase schedule alone, independent of which guardrail
// policy (if any) is attached to the projection that produced it. Months
// with CumulativeInflation <= 0 are skipped (nothing to convert to real
// dollars). Rounding matches how the rest of the guardrail UI rounds
// planned/funded living spending (see floorOutcomeTracker.observe).
//
// year is the 0-based projection year (Month/12) of the FIRST month
// attaining that minimum -- ties keep the earliest occurrence. phase is
// the PhaseName recorded on that year's YearlySummaries entry, or "" when
// phases are disabled/unconfigured (the engine's "-" sentinel; see
// models.ProjectionYearSummary.PhaseName) or the projection predates the
// PhaseName field (also recorded as ""). ok is false only when the
// projection has no months to examine (nil projection or empty Months).
func LowestPlannedLivingReal(p *models.ProjectionResult) (amount float64, year int, phase string, ok bool) {
	if p == nil {
		return 0, 0, "", false
	}
	found := false
	for _, m := range p.Months {
		if m.CumulativeInflation <= 0 {
			continue
		}
		real := engine.RoundLivingCents(m.PlannedLivingExpenses / m.CumulativeInflation)
		if !found || real < amount {
			amount = real
			year = m.Month / 12
			found = true
		}
	}
	if !found {
		return 0, 0, "", false
	}
	for _, ys := range p.YearlySummaries {
		if ys.Year == year {
			if ys.PhaseName != "-" {
				phase = ys.PhaseName
			}
			break
		}
	}
	return amount, year, phase, true
}
