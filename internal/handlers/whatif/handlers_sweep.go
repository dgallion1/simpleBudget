package whatif

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"sort"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// conversionSweepBaseAmounts are the flat annual Roth conversion amounts the
// sweep always evaluates, in ascending order. See CONVERSION_SWEEP_SPEC.md.
var conversionSweepBaseAmounts = []float64{
	0, 25_000, 50_000, 75_000, 100_000, 125_000, 150_000, 175_000, 200_000,
}

// ConversionSweepRow is one row of the conversion sweep table: what a flat
// annual Roth conversion amount does to portfolio longevity, lifetime tax,
// and lifetime IRMAA. Exactly one of (Survives + EndingBalanceReal) or
// (DepletionMonth + DepletionYears) is meaningful, selected by Survives.
type ConversionSweepRow struct {
	Amount  float64
	Current bool

	Survives          bool
	EndingBalanceReal float64 // meaningful only when Survives

	DepletionMonth int // meaningful only when !Survives
	DepletionYears int // meaningful only when !Survives; rounded, not truncated

	// LifetimeTax and LifetimeIRMAA are both real (inflation-deflated,
	// today's dollars) — see lifetimeRealTaxAndIRMAA.
	LifetimeTax   float64
	LifetimeIRMAA float64
}

// lifetimeRealTaxAndIRMAA sums proj's per-year taxes (federal + state + NIIT,
// the engine's Taxes field) and IRMAA surcharge, each deflated by
// CumulativeInflation into today's (start-year) dollars. Mirrors
// LifetimeTaxReal in tax_optimizer.go's projectionToCandidate — same
// per-year-summary deflation discipline, applied to IRMAA as well so both
// sweep columns match the "today's dollars" caption on the results template.
func lifetimeRealTaxAndIRMAA(proj *models.ProjectionResult) (tax, irmaa float64) {
	for _, ys := range proj.YearlySummaries {
		deflator := ys.CumulativeInflation
		if deflator <= 0 {
			deflator = 1 // pre-projection or unset
		}
		tax += ys.Taxes / deflator
		irmaa += ys.IRMAA / deflator
	}
	return tax, irmaa
}

// conversionSweepAmounts returns the candidate amounts to sweep: the base
// ladder plus the saved plan's current annual conversion amount, inserted in
// ascending order with no duplicate.
func conversionSweepAmounts(settings *models.WhatIfSettings) []float64 {
	current := conversionSweepCurrentAmount(settings)

	amounts := append([]float64(nil), conversionSweepBaseAmounts...)
	for _, a := range amounts {
		if a == current {
			return amounts
		}
	}
	amounts = append(amounts, current)
	sort.Float64s(amounts)
	return amounts
}

// conversionSweepCurrentAmount returns the saved plan's current flat annual
// Roth conversion amount, or 0 when Roth conversion is unset.
func conversionSweepCurrentAmount(settings *models.WhatIfSettings) float64 {
	if settings == nil || settings.RothConversion == nil {
		return 0
	}
	return settings.RothConversion.AnnualAmount
}

// candidateSettingsForConversionAmount returns a shallow copy of s with the
// Roth conversion annual amount overridden to amount (enabled iff amount >
// 0), preserving the saved start/end years. Mirrors the copy discipline of
// candidateSettingsForSS in tax_optimizer.go: s is never mutated — only the
// RothConversion pointer field is replaced with a fresh copy on cfg.
func candidateSettingsForConversionAmount(s *models.WhatIfSettings, amount float64) *models.WhatIfSettings {
	cfg := *s
	roth := models.RothConversionConfig{}
	if s.RothConversion != nil {
		roth = *s.RothConversion
	}
	roth.AnnualAmount = amount
	roth.Enabled = amount > 0
	cfg.RothConversion = &roth
	return &cfg
}

// buildConversionSweepRow runs one deterministic projection for amount and
// derives its row metrics. chain/hooks come from the saved plan's built
// engine input so scenario-chain transitions match the rest of the page;
// only Prepared varies per candidate.
func buildConversionSweepRow(eng *engine.Engine, settings *models.WhatIfSettings, amount float64, current float64, chain []engine.PreparedChainLink, hooks engine.Hooks) ConversionSweepRow {
	row := ConversionSweepRow{Amount: amount, Current: amount == current}

	candidate := candidateSettingsForConversionAmount(settings, amount)
	prepared, err := prepare.From(candidate)
	if err != nil {
		// Leave a zero-value (but amount/current populated) row rather than
		// dropping it silently — the table still shows one row per
		// candidate, per the acceptance criteria.
		return row
	}

	in := engine.Input{Prepared: prepared, Chain: chain, Hooks: hooks}
	proj := eng.Run(in)
	if proj == nil {
		return row
	}

	row.Survives = proj.Survives
	if !proj.Survives && proj.DepletionMonth != nil {
		row.DepletionMonth = *proj.DepletionMonth
		row.DepletionYears = int(math.Round(float64(*proj.DepletionMonth) / 12))
	}
	if proj.Survives && len(proj.Months) > 0 {
		row.EndingBalanceReal = proj.Months[len(proj.Months)-1].PortfolioBalanceReal
	}

	row.LifetimeTax, row.LifetimeIRMAA = lifetimeRealTaxAndIRMAA(proj)

	return row
}

// handleWhatIfConversionSweep runs one deterministic eng.Run per candidate
// flat annual Roth conversion amount and reports what each does to
// portfolio longevity, lifetime tax, and lifetime IRMAA. Deliberately never
// calls retirement.RunFull — that costs ~7s (Monte Carlo + backtest + SS-grid
// fan-out) and this endpoint must finish in well under 2s. Each candidate is
// one deterministic, side-effect-free projection, run concurrently via
// analysis.ParallelIndexed (same fan-out precedent as the Tax Optimizer).
func handleWhatIfConversionSweep(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	baseIn, _, err := buildEngineInput(settings)
	if err != nil {
		renderError(w, "Failed to build engine input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	amounts := conversionSweepAmounts(settings)
	current := conversionSweepCurrentAmount(settings)

	rows := make([]ConversionSweepRow, len(amounts))
	eng := getEngine()
	analysis.ParallelIndexed(len(amounts), runtime.NumCPU(), func(i int) {
		rows[i] = buildConversionSweepRow(eng, settings, amounts[i], current, baseIn.Chain, baseIn.Hooks)
	})

	partialData := map[string]interface{}{
		"Rows": rows,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-conversion-sweep-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
