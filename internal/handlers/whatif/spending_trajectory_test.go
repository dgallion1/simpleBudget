package whatif

import (
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/models"
)

// syntheticTrajectoryMonth builds one ProjectionMonth with just the fields the
// trajectory builder reads.
func syntheticTrajectoryMonth(m int, cumInfl, expenses, income, draw, rmd, portfolio float64) models.ProjectionMonth {
	return models.ProjectionMonth{
		Month:               m,
		Year:                float64(m) / 12,
		CumulativeInflation: cumInfl,
		PortfolioBalance:    portfolio,
		TotalExpenses:       expenses,
		TotalIncome:         income,
		NetWithdrawal:       draw,
		RMDWithdrawal:       rmd,
	}
}

// TestBuildSpendingTrajectoryRows_MatchesSyntheticProjection verifies the
// per-year aggregation: monthly averages, today's-dollar deflation via
// CumulativeInflation, withdrawal rate against the year's starting balance,
// and the 2-year display cadence (year 0 and the final year always shown).
func TestBuildSpendingTrajectoryRows_MatchesSyntheticProjection(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.SpendingPhaseConfig.Enabled = false

	proj := &models.ProjectionResult{}
	for m := 0; m < 12; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 1000, 1_000_000))
	}
	for m := 12; m < 24; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.25, 5000, 2000, 3000, 500, 900_000))
	}
	proj.YearlySummaries = []models.ProjectionYearSummary{
		{Year: 0, StartingBalance: 1_000_000},
		{Year: 1, StartingBalance: 900_000},
	}

	rows := buildSpendingTrajectoryRows(s, proj)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (year 0 + final year), got %d: %+v", len(rows), rows)
	}
	r0, r1 := rows[0], rows[1]
	if r0.Year != 0 || r1.Year != 1 {
		t.Fatalf("expected years 0 and 1, got %d and %d", r0.Year, r1.Year)
	}
	if r0.PrimaryAge != 65 || r1.PrimaryAge != 66 {
		t.Errorf("ages = %d, %d; want 65, 66", r0.PrimaryAge, r1.PrimaryAge)
	}
	approx := func(got, want float64) bool { return math.Abs(got-want) < 0.01 }
	if !approx(r0.RMDNominal, 1000) || !approx(r1.RMDNominal, 500) {
		t.Errorf("RMDNominal = %.2f, %.2f; want 1000, 500", r0.RMDNominal, r1.RMDNominal)
	}
	if !approx(r1.RMDReal, 400) { // 500 / 1.25
		t.Errorf("RMDReal year 1 = %.2f; want 400", r1.RMDReal)
	}
	if !approx(r1.SpendReal, 4000) { // 5000 / 1.25
		t.Errorf("SpendReal year 1 = %.2f; want 4000", r1.SpendReal)
	}
	if !approx(r1.IncomeReal, 1600) { // 2000 / 1.25
		t.Errorf("IncomeReal year 1 = %.2f; want 1600", r1.IncomeReal)
	}
	if !approx(r0.DrawNominal, 3000) {
		t.Errorf("DrawNominal year 0 = %.2f; want 3000", r0.DrawNominal)
	}
	// WR% = annual draw / year starting balance.
	if !approx(r0.WithdrawalRate, 3.6) { // 36,000 / 1,000,000
		t.Errorf("WithdrawalRate year 0 = %.2f; want 3.6", r0.WithdrawalRate)
	}
	if !approx(r1.WithdrawalRate, 4.0) { // 36,000 / 900,000
		t.Errorf("WithdrawalRate year 1 = %.2f; want 4.0", r1.WithdrawalRate)
	}
	if r0.PhaseName != "-" {
		t.Errorf("phases disabled: PhaseName = %q; want \"-\"", r0.PhaseName)
	}
}

// TestBuildSpendingTrajectoryRows_PhaseNames verifies the phase column uses
// the same phase-reference-age logic as the engine's expense assembly.
func TestBuildSpendingTrajectoryRows_PhaseNames(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.PhaseAgeReference = "primary"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 66, Multiplier: 0.85},
		},
	}

	proj := &models.ProjectionResult{}
	for m := 0; m < 24; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 0, 1_000_000))
	}
	rows := buildSpendingTrajectoryRows(s, proj)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].PhaseName != "Go-Go" {
		t.Errorf("year 0 PhaseName = %q; want Go-Go", rows[0].PhaseName)
	}
	if rows[1].PhaseName != "Slow-Go" {
		t.Errorf("year 1 (age 66) PhaseName = %q; want Slow-Go", rows[1].PhaseName)
	}
}

// TestBuildSpendingTrajectoryRows_PrefersSummaryPhaseName is the Z5
// regression: when the engine has recorded a year's PhaseName in the yearly
// summary (e.g. because a scenario chain switched to linked settings with a
// different phase config), the row must show that name — not
// trajectoryPhaseName(s, yi) computed from the PRIMARY settings passed in.
func TestBuildSpendingTrajectoryRows_PrefersSummaryPhaseName(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.PhaseAgeReference = "primary"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 66, Multiplier: 0.85},
		},
	}

	proj := &models.ProjectionResult{}
	for m := 0; m < 24; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 0, 1_000_000))
	}
	// Year 1's summary carries a linked scenario's phase name, which would
	// NOT match trajectoryPhaseName(s, 1) (that computes "Slow-Go" from the
	// primary settings passed to the builder).
	proj.YearlySummaries = []models.ProjectionYearSummary{
		{Year: 0, StartingBalance: 1_000_000, PhaseName: "Go-Go"},
		{Year: 1, StartingBalance: 1_000_000, PhaseName: "Chain-Linked-Phase"},
	}

	rows := buildSpendingTrajectoryRows(s, proj)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].PhaseName != "Go-Go" {
		t.Errorf("year 0 PhaseName = %q; want Go-Go", rows[0].PhaseName)
	}
	if rows[1].PhaseName != "Chain-Linked-Phase" {
		t.Errorf("year 1 PhaseName = %q; want the summary's Chain-Linked-Phase (not the primary settings' Slow-Go)", rows[1].PhaseName)
	}
}

// TestBuildSpendingTrajectoryRows_FallsBackWhenSummaryPhaseNameEmpty proves
// two things: (1) with no chain, projections whose yearly summaries carry no
// PhaseName (predating this field, or years the engine legitimately left
// empty) fall back to trajectoryPhaseName(s, yi) with byte-identical
// results — no behavior change on the common path; and (2) with phases
// disabled, rows still show "-".
func TestBuildSpendingTrajectoryRows_FallsBackWhenSummaryPhaseNameEmpty(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.PhaseAgeReference = "primary"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 66, Multiplier: 0.85},
		},
	}

	proj := &models.ProjectionResult{}
	for m := 0; m < 24; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 0, 1_000_000))
	}
	// No YearlySummaries at all: simulates a projection built before the
	// PhaseName field existed.
	rows := buildSpendingTrajectoryRows(s, proj)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if got, want := rows[0].PhaseName, trajectoryPhaseName(s, rows[0].Year); got != want {
		t.Errorf("year 0 PhaseName = %q; want fallback trajectoryPhaseName result %q", got, want)
	}
	if got, want := rows[1].PhaseName, trajectoryPhaseName(s, rows[1].Year); got != want {
		t.Errorf("year 1 PhaseName = %q; want fallback trajectoryPhaseName result %q", got, want)
	}
	if rows[0].PhaseName != "Go-Go" || rows[1].PhaseName != "Slow-Go" {
		t.Errorf("expected Go-Go/Slow-Go from the fallback path, got %q/%q", rows[0].PhaseName, rows[1].PhaseName)
	}

	// Phases disabled: unchanged "-" behavior, with or without a summary row
	// present (summary PhaseName stays empty when the engine sees phases
	// disabled, since GetSpendingPhaseNameAt returns "" in that case too).
	disabled := models.DefaultWhatIfSettings()
	disabled.CurrentAge = 65
	disabled.SpendingPhaseConfig.Enabled = false
	disabledProj := &models.ProjectionResult{}
	for m := 0; m < 12; m++ {
		disabledProj.Months = append(disabledProj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 0, 1_000_000))
	}
	disabledProj.YearlySummaries = []models.ProjectionYearSummary{
		{Year: 0, StartingBalance: 1_000_000, PhaseName: ""},
	}
	disabledRows := buildSpendingTrajectoryRows(disabled, disabledProj)
	if len(disabledRows) != 1 || disabledRows[0].PhaseName != "-" {
		t.Fatalf("phases disabled: rows = %+v; want single row with PhaseName \"-\"", disabledRows)
	}
}

// TestBuildSpendingTrajectoryRows_ChainToPhasesDisabledShowsSentinelNotPrimaryLabel
// is the Z5 attempt-2 regression at the handler level: a scenario chain
// transitions from a phases-ENABLED primary into a phases-DISABLED linked
// scenario. The engine now records "-" (never "") in that year's summary
// PhaseName (see engine.phaseNameOrNoPhaseSentinel). The handler must show
// that "-" as-is through the preference branch — not fall back to
// trajectoryPhaseName(primary, yi), which would wrongly reproduce the
// still-enabled primary's label (e.g. "Slow-Go").
func TestBuildSpendingTrajectoryRows_ChainToPhasesDisabledShowsSentinelNotPrimaryLabel(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.PhaseAgeReference = "primary"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 66, Multiplier: 0.85},
		},
	}

	proj := &models.ProjectionResult{}
	for m := 0; m < 24; m++ {
		proj.Months = append(proj.Months, syntheticTrajectoryMonth(m, 1.0, 5000, 2000, 3000, 0, 1_000_000))
	}
	// Year 1 is post-transition into a phases-disabled linked scenario: the
	// engine records the sentinel "-", not "". If the handler mishandled
	// this like the pre-fix "" case, it would fall back to
	// trajectoryPhaseName(s, 1), which computes "Slow-Go" from these
	// (still phases-enabled) primary settings — the exact adversarial bug.
	proj.YearlySummaries = []models.ProjectionYearSummary{
		{Year: 0, StartingBalance: 1_000_000, PhaseName: "Go-Go"},
		{Year: 1, StartingBalance: 1_000_000, PhaseName: "-"},
	}

	rows := buildSpendingTrajectoryRows(s, proj)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].PhaseName != "Go-Go" {
		t.Errorf("year 0 PhaseName = %q; want Go-Go", rows[0].PhaseName)
	}
	if rows[1].PhaseName != "-" {
		t.Errorf("year 1 PhaseName = %q; want the engine's sentinel \"-\" (not the primary's Slow-Go)", rows[1].PhaseName)
	}
	// Guard against the specific regression: never the primary's label.
	if rows[1].PhaseName == "Slow-Go" {
		t.Fatalf("year 1 PhaseName regressed to the primary's Slow-Go label")
	}
}

// conversionHeavySettings returns a fixture where large Roth conversions
// drain the tax-deferred pool well before RMD age, so the engine projection
// contains no RMDs at all — the exact scenario the old client-side preview
// got wrong (it modeled RMDs from a never-converted tax-deferred balance).
func conversionHeavySettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 80
	s.RothPercent = 10
	s.MonthlyLivingExpenses = 3000
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 200_000,
		StartYear:    0,
		EndYear:      7,
	}
	return s
}

// TestSpendingTrajectory_RMDMatchesEngineProjection is the regression test
// for the old client-side mini-model: every displayed RMD figure must equal
// the engine projection's per-year RMD (monthly average), and in a
// conversion-heavy plan whose tax-deferred pool is exhausted before RMD age
// every row must show zero RMD. A no-conversion control proves the assertion
// discriminates (RMDs really do appear at age 73+ otherwise).
func TestSpendingTrajectory_RMDMatchesEngineProjection(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	rowsFor := func(t *testing.T, s *models.WhatIfSettings) ([]trajectoryRow, *models.ProjectionResult, *models.WhatIfSettings) {
		t.Helper()
		in, _, err := buildEngineInput(s)
		if err != nil {
			t.Fatalf("buildEngineInput: %v", err)
		}
		analysis, _, err := analysisFastOrCached(s)
		if err != nil {
			t.Fatalf("analysisFastOrCached: %v", err)
		}
		prepared := in.Prepared.Settings()
		return buildSpendingTrajectoryRows(prepared, analysis.Projection), analysis.Projection, prepared
	}

	assertRowsMatchProjection := func(t *testing.T, rows []trajectoryRow, proj *models.ProjectionResult) {
		t.Helper()
		type yearAgg struct {
			rmd    float64
			months int
		}
		byYear := map[int]*yearAgg{}
		for _, m := range proj.Months {
			yi := int(m.Year)
			if byYear[yi] == nil {
				byYear[yi] = &yearAgg{}
			}
			byYear[yi].rmd += m.RMDWithdrawal
			byYear[yi].months++
		}
		for _, row := range rows {
			agg := byYear[row.Year]
			if agg == nil {
				t.Fatalf("row for year %d has no projection months", row.Year)
			}
			want := agg.rmd / float64(agg.months)
			if math.Abs(row.RMDNominal-want) > 0.01 {
				t.Errorf("year %d: table RMD %.2f != projection RMD %.2f", row.Year, row.RMDNominal, want)
			}
		}
	}

	t.Run("conversion-heavy plan shows zero RMDs, matching the engine", func(t *testing.T) {
		rows, proj, _ := rowsFor(t, conversionHeavySettings())
		if len(rows) == 0 {
			t.Fatal("no trajectory rows")
		}

		// Fixture validity: the conversions must actually drain tax-deferred
		// before RMD age (owner is 65 at year 0, RMDs start at 73 = year 8).
		for _, m := range proj.Months {
			if int(m.Year) >= 8 && m.TaxDeferredBalance > 1000 {
				t.Fatalf("fixture invalid: tax-deferred still %.0f in year %d", m.TaxDeferredBalance, int(m.Year))
			}
		}
		var totalRMD float64
		for _, m := range proj.Months {
			totalRMD += m.RMDWithdrawal
		}
		if totalRMD > 0.01 {
			t.Fatalf("fixture invalid: engine projected %.2f of RMDs", totalRMD)
		}

		for _, row := range rows {
			if row.RMDNominal != 0 {
				t.Errorf("year %d: table shows RMD %.2f but the engine projects none", row.Year, row.RMDNominal)
			}
		}
		assertRowsMatchProjection(t, rows, proj)
	})

	t.Run("no-conversion control shows the engine's RMDs at 73+", func(t *testing.T) {
		s := conversionHeavySettings()
		s.RothConversion.Enabled = false
		rows, proj, _ := rowsFor(t, s)

		var totalRMD float64
		for _, m := range proj.Months {
			totalRMD += m.RMDWithdrawal
		}
		if totalRMD <= 0 {
			t.Fatal("control invalid: engine projected no RMDs without conversions")
		}
		var sawRMDRow bool
		for _, row := range rows {
			if row.Year >= 8 && row.RMDNominal > 0 {
				sawRMDRow = true
			}
		}
		if !sawRMDRow {
			t.Error("expected at least one displayed row with a nonzero RMD at age 73+")
		}
		assertRowsMatchProjection(t, rows, proj)
	})
}

// phaseReferenceModeSettings builds a real (no-chain) fixture with a primary
// and a younger spouse, so the four PhaseAgeReference modes ("primary",
// "spouse", "older", "younger") select from two genuinely different age
// sequences across the projection.
func phaseReferenceModeSettings(mode string) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 3000
	s.Persons = []models.Person{
		{
			ID:         "primary",
			Name:       "Primary",
			BirthMonth: models.BirthMonthForAge(s.StartDate, 65),
			Role:       models.PersonRolePrimary,
		},
		{
			ID:         "spouse",
			Name:       "Spouse",
			BirthMonth: models.BirthMonthForAge(s.StartDate, 60),
			Role:       models.PersonRoleSpouse,
		},
	}
	s.PhaseAgeReference = mode
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow-Go", StartAge: 70, Multiplier: 0.85},
			{Name: "No-Go", StartAge: 80, Multiplier: 0.70},
		},
	}
	s.ProjectionYears = 20
	return s
}

// TestSpendingTrajectory_PhaseNamesMatchAcrossReferenceModes is the F-Z5-1
// promotion: a real (no-chain) engine run, across all four
// PhaseAgeReference modes, asserting every trajectory row's PhaseName goes
// through the PREFERENCE branch (the engine-recorded summary value, not the
// "" fallback) and equals trajectoryPhaseName(s, year) — proving the engine
// and the fallback formula agree on the no-chain path. A companion
// phases-disabled real run proves the same preference branch now carries
// the sentinel "-" (previously this reached "-" only via the "" fallback).
func TestSpendingTrajectory_PhaseNamesMatchAcrossReferenceModes(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	rowsFor := func(t *testing.T, s *models.WhatIfSettings) ([]trajectoryRow, *models.ProjectionResult, *models.WhatIfSettings) {
		t.Helper()
		in, _, err := buildEngineInput(s)
		if err != nil {
			t.Fatalf("buildEngineInput: %v", err)
		}
		analysis, _, err := analysisFastOrCached(s)
		if err != nil {
			t.Fatalf("analysisFastOrCached: %v", err)
		}
		prepared := in.Prepared.Settings()
		return buildSpendingTrajectoryRows(prepared, analysis.Projection), analysis.Projection, prepared
	}

	for _, mode := range []string{"primary", "spouse", "older", "younger"} {
		t.Run(mode, func(t *testing.T) {
			rows, proj, prepared := rowsFor(t, phaseReferenceModeSettings(mode))
			if len(rows) == 0 {
				t.Fatal("no trajectory rows")
			}
			// Sanity: the engine must actually have recorded a PhaseName for
			// every row's year (non-empty), so the assertion below exercises
			// the PREFERENCE branch, not the "" fallback.
			summaryByYear := map[int]string{}
			for _, ys := range proj.YearlySummaries {
				summaryByYear[ys.Year] = ys.PhaseName
			}
			var sawSlowGo, sawGoGo bool
			for _, row := range rows {
				summaryName, ok := summaryByYear[row.Year]
				if !ok || summaryName == "" {
					t.Fatalf("year %d: engine summary has no PhaseName (%q, present=%v) — test would exercise the fallback, not the preference branch", row.Year, summaryName, ok)
				}
				want := trajectoryPhaseName(prepared, row.Year)
				if row.PhaseName != want {
					t.Errorf("year %d: row PhaseName = %q, want trajectoryPhaseName result %q", row.Year, row.PhaseName, want)
				}
				if row.PhaseName != summaryName {
					t.Errorf("year %d: row PhaseName = %q, want the engine summary's %q (preference branch)", row.Year, row.PhaseName, summaryName)
				}
				switch row.PhaseName {
				case "Slow-Go":
					sawSlowGo = true
				case "Go-Go":
					sawGoGo = true
				}
			}
			if !sawGoGo || !sawSlowGo {
				t.Errorf("fixture invalid: expected both Go-Go and Slow-Go to appear across the 20-year projection for mode %q, got Go-Go=%v Slow-Go=%v", mode, sawGoGo, sawSlowGo)
			}
		})
	}

	t.Run("phases disabled shows sentinel via the preference branch", func(t *testing.T) {
		s := phaseReferenceModeSettings("primary")
		s.SpendingPhaseConfig.Enabled = false
		rows, proj, _ := rowsFor(t, s)
		if len(rows) == 0 {
			t.Fatal("no trajectory rows")
		}
		summaryByYear := map[int]string{}
		for _, ys := range proj.YearlySummaries {
			summaryByYear[ys.Year] = ys.PhaseName
		}
		for _, row := range rows {
			summaryName, ok := summaryByYear[row.Year]
			if !ok || summaryName != "-" {
				t.Fatalf("year %d: engine summary PhaseName = %q (present=%v), want the sentinel \"-\" so this exercises the preference branch", row.Year, summaryName, ok)
			}
			if row.PhaseName != "-" {
				t.Errorf("year %d: row PhaseName = %q, want \"-\"", row.Year, row.PhaseName)
			}
		}
	})
}

// TestSpendingTrajectoryEndpoint_RendersRowsFromEngine drives the HTTP
// endpoint with the renderer wired up and asserts the rows partial renders
// engine-derived rows: a conversion-heavy plan renders "-" in every RMD cell.
func TestSpendingTrajectoryEndpoint_RendersRowsFromEngine(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	if err := rm.Save(conversionHeavySettings()); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	req := httptest.NewRequest("GET", "/whatif/spending-trajectory", nil)
	w := httptest.NewRecorder()
	handleWhatIfSpendingTrajectory(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d; body: %s", w.Code, truncate(w.Body.String(), 500))
	}
	out := w.Body.String()
	if !strings.Contains(out, "<tr") {
		t.Fatalf("expected table rows; got: %s", truncate(out, 500))
	}
	// Every RMD cell must be "-" (no RMDs in a drained-before-73 plan).
	if strings.Contains(out, "data-col=\"rmd\"") {
		for _, cell := range strings.Split(out, "data-col=\"rmd\"")[1:] {
			end := strings.Index(cell, "</td>")
			if end < 0 {
				t.Fatal("malformed RMD cell")
			}
			if !strings.Contains(cell[:end], "-") || strings.Contains(cell[:end], "$") {
				t.Errorf("expected empty RMD cell, got: %s", cell[:end])
			}
		}
	} else {
		t.Error("expected RMD cells marked with data-col=\"rmd\"")
	}
	// Withdrawal-rate coloring survives the rewrite. U6 replaced the
	// text-green-700/text-amber-600/text-red-600 hue literals with the
	// semantic positive/warning/negative tokens.
	if !strings.Contains(out, "text-positive") && !strings.Contains(out, "text-warning") && !strings.Contains(out, "text-negative") {
		t.Error("expected WR% color classes in rendered rows")
	}
}
