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

	// LeastLifetimeTax and LongestLasting mark this row as the sweep's best
	// row for that criterion (T16 — see markBestConversionSweepRows). Both
	// may be true on the same row; the template renders one combined marker
	// in that case rather than stacking two badges.
	LeastLifetimeTax bool
	LongestLasting   bool

	// RothStartYear and RothEndYear are the saved plan's Roth conversion
	// start/end years, carried on every row so the "Apply" button (T16) can
	// preserve them when it POSTs a different annual_amount to
	// /whatif/roth-conversion — the same start/end years for every row,
	// since only the amount varies across the sweep.
	RothStartYear int
	RothEndYear   int
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
// Roth conversion amount, or 0 when Roth conversion is unset or disabled.
// The rates form deliberately preserves AnnualAmount when a user disables
// conversions (a UI convenience for re-enabling later, not a plan value), so
// a disabled config's stored amount must not be treated as the active
// current amount here (D-Z-e).
func conversionSweepCurrentAmount(settings *models.WhatIfSettings) float64 {
	if settings == nil || settings.RothConversion == nil || !settings.RothConversion.Enabled {
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
	if settings != nil && settings.RothConversion != nil {
		row.RothStartYear = settings.RothConversion.StartYear
		row.RothEndYear = settings.RothConversion.EndYear
	}

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

// markBestConversionSweepRows sets LeastLifetimeTax on the row with the
// lowest lifetime tax and LongestLasting on the row that lasts longest
// (T16): surviving beats any depletion; among survivors, the higher ending
// real balance wins; among depleting rows, the later depletion month wins.
// A tie on either marker prefers the smaller amount. When the same row wins
// both, both fields end up true on that one row — the template renders a
// single combined marker rather than stacking two badges. No-op on an empty
// slice.
func markBestConversionSweepRows(rows []ConversionSweepRow) {
	if len(rows) == 0 {
		return
	}

	leastTaxIdx, longestIdx := 0, 0
	for i := 1; i < len(rows); i++ {
		if isLowerLifetimeTax(rows[i], rows[leastTaxIdx]) {
			leastTaxIdx = i
		}
		if lastsLonger(rows[i], rows[longestIdx]) {
			longestIdx = i
		}
	}

	rows[leastTaxIdx].LeastLifetimeTax = true
	rows[longestIdx].LongestLasting = true
}

// isLowerLifetimeTax reports whether candidate beats current for the "least
// lifetime tax" marker: a strictly lower LifetimeTax wins; a tie prefers the
// smaller Amount.
func isLowerLifetimeTax(candidate, current ConversionSweepRow) bool {
	if candidate.LifetimeTax != current.LifetimeTax {
		return candidate.LifetimeTax < current.LifetimeTax
	}
	return candidate.Amount < current.Amount
}

// lastsLonger reports whether candidate beats current for the
// "longest-lasting portfolio" marker: surviving beats any depletion; among
// survivors, a higher EndingBalanceReal wins; among depleting rows, a later
// DepletionMonth wins; a tie prefers the smaller Amount.
func lastsLonger(candidate, current ConversionSweepRow) bool {
	if candidate.Survives != current.Survives {
		return candidate.Survives
	}
	if candidate.Survives {
		if candidate.EndingBalanceReal != current.EndingBalanceReal {
			return candidate.EndingBalanceReal > current.EndingBalanceReal
		}
		return candidate.Amount < current.Amount
	}
	if candidate.DepletionMonth != current.DepletionMonth {
		return candidate.DepletionMonth > current.DepletionMonth
	}
	return candidate.Amount < current.Amount
}

// buildConversionSweepRows runs the sweep for settings' candidate amounts
// (one deterministic eng.Run per candidate, run concurrently via
// analysis.ParallelIndexed — same fan-out precedent as the Tax Optimizer;
// deliberately never calls retirement.RunFull, which costs ~7s of Monte
// Carlo/backtest/SS-grid fan-out) and returns the rows with the current,
// least-lifetime-tax, and longest-lasting markers set. Shared by the
// run-sweep endpoint and the Apply-button render tail (T16) so both paths
// compute rows and markers identically.
func buildConversionSweepRows(settings *models.WhatIfSettings) ([]ConversionSweepRow, error) {
	baseIn, _, err := buildEngineInput(settings)
	if err != nil {
		return nil, err
	}

	amounts := conversionSweepAmounts(settings)
	current := conversionSweepCurrentAmount(settings)

	rows := make([]ConversionSweepRow, len(amounts))
	eng := getEngine()
	analysis.ParallelIndexed(len(amounts), runtime.NumCPU(), func(i int) {
		rows[i] = buildConversionSweepRow(eng, settings, amounts[i], current, baseIn.Chain, baseIn.Hooks)
	})

	markBestConversionSweepRows(rows)

	return rows, nil
}

// renderConversionSweepResults renders the "whatif-conversion-sweep-results"
// partial for rows. applied/appliedAmount surface the aria-live confirmation
// (ACCESSIBILITY.md #10 — state-changing actions announce their result) shown
// after an Apply-button click (T16); the button-triggered
// POST /whatif/conversion-sweep run passes applied=false.
func renderConversionSweepResults(w http.ResponseWriter, rows []ConversionSweepRow, applied bool, appliedAmount float64) {
	partialData := map[string]interface{}{
		"Rows":          rows,
		"Applied":       applied,
		"AppliedAmount": appliedAmount,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-conversion-sweep-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
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

	rows, err := buildConversionSweepRows(settings)
	if err != nil {
		renderError(w, "Failed to build engine input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	renderConversionSweepResults(w, rows, false, 0)
}

// conversionSweepApplySource is the value of the "apply_source" hidden form
// field the conversion-sweep table's "Apply" buttons (T16) submit to
// POST /whatif/roth-conversion, distinguishing an Apply click from the
// standalone Roth Conversion form (which never sends this field) so
// handleWhatIfRothConversion knows to re-render the sweep table — instead of
// the standard what-if results column — once the save completes.
const conversionSweepApplySource = "conversion-sweep"

// saveAndRenderConversionSweep persists settings (already mutated in place by
// handleWhatIfRothConversion's usual form-parsing logic) and re-renders the
// conversion-sweep results partial, so an Apply click in the sweep table
// immediately shows the applied amount's row as current (T16). Mirrors
// saveAndRecalc's save-then-render shape and its HX-Trigger revision
// announcement, but the render tail is the sweep table rather than the
// standard what-if results column.
func saveAndRenderConversionSweep(w http.ResponseWriter, r *http.Request, settings *models.WhatIfSettings) {
	revision, err := retirementMgr.SaveWithRevision(settings)
	if err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), statusForMutationError(err))
		return
	}
	if trigger, err := json.Marshal(map[string]int{"whatif:revision": revision}); err == nil {
		w.Header().Set("HX-Trigger", string(trigger))
	}

	rows, err := buildConversionSweepRows(settings)
	if err != nil {
		renderError(w, "Failed to build engine input: "+err.Error(), http.StatusInternalServerError)
		return
	}

	appliedAmount := 0.0
	if settings.RothConversion != nil {
		appliedAmount = settings.RothConversion.AnnualAmount
	}
	renderConversionSweepResults(w, rows, true, appliedAmount)
}
