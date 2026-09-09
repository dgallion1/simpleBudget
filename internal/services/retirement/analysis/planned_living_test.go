package analysis

import (
	"testing"

	"budget2/internal/models"
)

func TestLowestPlannedLivingReal_NilAndEmpty(t *testing.T) {
	if _, _, _, ok := LowestPlannedLivingReal(nil); ok {
		t.Fatal("nil projection must report ok=false")
	}
	if _, _, _, ok := LowestPlannedLivingReal(&models.ProjectionResult{}); ok {
		t.Fatal("empty projection must report ok=false")
	}
}

// TestLowestPlannedLivingReal_PhaseDecline mirrors the live-plan scenario in
// the brief: living spending declines phase by phase, and the lowest point
// is reached mid-projection, not in the first or last month. The helper must
// report that year's PhaseName and the FIRST month attaining the minimum.
func TestLowestPlannedLivingReal_PhaseDecline(t *testing.T) {
	months := make([]models.ProjectionMonth, 0, 36)
	// Year 0: Go-Go, full 10900/mo.
	for i := 0; i < 12; i++ {
		months = append(months, models.ProjectionMonth{Month: i, CumulativeInflation: 1, PlannedLivingExpenses: 10900})
	}
	// Year 1: Slow-Go, 80% = 8720/mo.
	for i := 12; i < 24; i++ {
		months = append(months, models.ProjectionMonth{Month: i, CumulativeInflation: 1, PlannedLivingExpenses: 8720})
	}
	// Year 2: No-Go, 65% = 7085/mo -- the lowest point, hit at month 24 first.
	for i := 24; i < 36; i++ {
		months = append(months, models.ProjectionMonth{Month: i, CumulativeInflation: 1, PlannedLivingExpenses: 7085})
	}
	p := &models.ProjectionResult{
		Months: months,
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0, PhaseName: "Go-Go"},
			{Year: 1, PhaseName: "Slow-Go"},
			{Year: 2, PhaseName: "No-Go"},
		},
	}
	amount, year, phase, ok := LowestPlannedLivingReal(p)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if amount != 7085 {
		t.Errorf("amount = %v, want 7085", amount)
	}
	if year != 2 {
		t.Errorf("year = %v, want 2 (0-based)", year)
	}
	if phase != "No-Go" {
		t.Errorf("phase = %q, want %q", phase, "No-Go")
	}
}

// TestLowestPlannedLivingReal_Flat covers a projection with no phase decline
// (the minimum is the first month, ties keep the earliest), a fractional-cent
// value that must go through engine.RoundLivingCents, CumulativeInflation
// growing month to month (division still finds the true minimum in REAL
// dollars, not nominal), and a month with CumulativeInflation <= 0 that must
// be skipped rather than propagate a division artifact.
func TestLowestPlannedLivingReal_Flat(t *testing.T) {
	p := &models.ProjectionResult{
		Months: []models.ProjectionMonth{
			// Skipped: no inflation factor yet.
			{Month: 0, CumulativeInflation: 0, PlannedLivingExpenses: 1},
			// Real = 5000.005 / 1.0 -> rounds to 5000.01 (half away from zero).
			{Month: 1, CumulativeInflation: 1.0, PlannedLivingExpenses: 5000.005},
			// Nominal grows with inflation but real stays flat at 5000.01 --
			// not strictly less than the first real value, so the first
			// occurrence (month 1) must win.
			{Month: 2, CumulativeInflation: 1.02, PlannedLivingExpenses: 5100.0102},
			{Month: 3, CumulativeInflation: 1.04, PlannedLivingExpenses: 5200.0104},
		},
		// No YearlySummaries at all -- projection predates PhaseName, or
		// phases were never configured; phase must come back "".
	}
	amount, year, phase, ok := LowestPlannedLivingReal(p)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if amount != 5000.01 {
		t.Errorf("amount = %v, want 5000.01 (cent-rounded)", amount)
	}
	if year != 0 {
		t.Errorf("year = %v, want 0 (Month 1 is still projection year 0)", year)
	}
	if phase != "" {
		t.Errorf("phase = %q, want \"\" (no YearlySummaries entry)", phase)
	}
}

// TestLowestPlannedLivingReal_DivisorSensitive is GV3 item 3: nominal
// PlannedLivingExpenses INCREASES month to month even as the REAL (CPI-
// deflated) value decreases, so the minimum REAL month (month 24) differs
// from the minimum NOMINAL month (month 0). This kills a mutant that drops
// the "/ m.CumulativeInflation" division (or otherwise substitutes nominal
// PlannedLivingExpenses for the real figure): such a mutant would report
// the minimum NOMINAL value (4900.00 at month 0) and year 0, not the true
// minimum real value and year computed below.
//   - Month 0:  planned 4900.00, CPI 1.00 -> real 4900.00.
//   - Month 12: planned 5100.00, CPI 1.10 -> real 4636.363636... -> rounds
//     to 4636.36 (the new minimum).
//   - Month 24: planned 5500.00, CPI 1.25 -> real 4400.00 (the true
//     minimum, and the lowest of all three).
func TestLowestPlannedLivingReal_DivisorSensitive(t *testing.T) {
	p := &models.ProjectionResult{
		Months: []models.ProjectionMonth{
			{Month: 0, CumulativeInflation: 1.00, PlannedLivingExpenses: 4900.00},
			{Month: 12, CumulativeInflation: 1.10, PlannedLivingExpenses: 5100.00},
			{Month: 24, CumulativeInflation: 1.25, PlannedLivingExpenses: 5500.00},
		},
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0, PhaseName: "Go-Go"},
			{Year: 1, PhaseName: "Slow-Go"},
			{Year: 2, PhaseName: "No-Go"},
		},
	}
	amount, year, phase, ok := LowestPlannedLivingReal(p)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// Guard the fixture itself: if this ever stops holding, the test no
	// longer proves divisor sensitivity.
	if amount == 4900.00 || amount == 5100.00 || amount == 5500.00 {
		t.Fatalf("fixture invariant broken: amount = %v looks like a NOMINAL value, not a real one", amount)
	}
	if amount != 4400.00 {
		t.Errorf("amount = %v, want 4400.00 (month 24's real value: 5500.00 / 1.25)", amount)
	}
	if year != 2 {
		t.Errorf("year = %v, want 2 (month 24 is projection year 2)", year)
	}
	if phase != "No-Go" {
		t.Errorf("phase = %q, want %q", phase, "No-Go")
	}
}

// TestLowestPlannedLivingReal_PhaseSentinel confirms the engine's "-"
// no-phase sentinel is normalized to "" like a missing entry, per the
// documented contract, so callers never have to special-case "-" themselves.
func TestLowestPlannedLivingReal_PhaseSentinel(t *testing.T) {
	p := &models.ProjectionResult{
		Months: []models.ProjectionMonth{
			{Month: 0, CumulativeInflation: 1, PlannedLivingExpenses: 4000},
		},
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0, PhaseName: "-"},
		},
	}
	_, _, phase, ok := LowestPlannedLivingReal(p)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if phase != "" {
		t.Errorf("phase = %q, want \"\" for the \"-\" sentinel", phase)
	}
}
