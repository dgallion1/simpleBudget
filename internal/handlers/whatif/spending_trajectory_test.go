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
	// Withdrawal-rate coloring survives the rewrite.
	if !strings.Contains(out, "text-green-600") && !strings.Contains(out, "text-amber-600") && !strings.Contains(out, "text-red-600") {
		t.Error("expected WR% color classes in rendered rows")
	}
}
