package whatif

import (
	"encoding/json"
	"net/http"

	"budget2/internal/models"
)

// trajectoryRow is one displayed year of the Spending Trajectory table,
// aggregated from the engine's monthly projection. Dollar fields are monthly
// averages for the year; Nominal/Real carry both dollar views so the client
// toggle never re-fetches. WithdrawalRate is the year's actual portfolio
// draw as a percent of its starting balance.
type trajectoryRow struct {
	Year       int    `json:"year"`
	PrimaryAge int    `json:"primary_age"`
	SpouseAge  int    `json:"spouse_age,omitempty"`
	PhaseName  string `json:"phase_name"`

	SpendNominal  float64 `json:"spend_nominal"`
	SpendReal     float64 `json:"spend_real"`
	IncomeNominal float64 `json:"income_nominal"`
	IncomeReal    float64 `json:"income_real"`
	DrawNominal   float64 `json:"draw_nominal"`
	DrawReal      float64 `json:"draw_real"`
	RMDNominal    float64 `json:"rmd_nominal"`
	RMDReal       float64 `json:"rmd_real"`

	WithdrawalRate float64 `json:"withdrawal_rate"`
	Depleted       bool    `json:"depleted"`
}

// trajectoryPhaseName returns the spending-phase label for a projection year,
// using the same reference-age rule (older/younger/primary/spouse) the engine
// applies in expense assembly. "-" when phases are disabled.
func trajectoryPhaseName(s *models.WhatIfSettings, year int) string {
	cfg := s.SpendingPhaseConfig
	if cfg == nil || !cfg.Enabled || len(cfg.Phases) == 0 {
		return "-"
	}
	age := s.GetPhaseReferenceAge(year)
	name := cfg.Phases[0].Name
	for _, p := range cfg.Phases {
		if age >= p.StartAge {
			name = p.Name
		}
	}
	return name
}

// buildSpendingTrajectoryRows aggregates the engine projection into per-year
// table rows. It displays year 0, every second year, and the final projected
// year — the cadence the old client-side preview used. All dollar figures
// come straight from the projection months (today's dollars via each month's
// CumulativeInflation); nothing is re-simulated.
func buildSpendingTrajectoryRows(s *models.WhatIfSettings, projection *models.ProjectionResult) []trajectoryRow {
	if s == nil || projection == nil || len(projection.Months) == 0 {
		return nil
	}

	type yearAgg struct {
		months                           int
		spendN, spendR, incomeN, incomeR float64
		drawN, drawR, rmdN, rmdR         float64
		endBalance                       float64
		depleted                         bool
	}
	var years []int
	byYear := map[int]*yearAgg{}
	for _, m := range projection.Months {
		yi := int(m.Year)
		a, ok := byYear[yi]
		if !ok {
			a = &yearAgg{}
			byYear[yi] = a
			years = append(years, yi)
		}
		infl := m.CumulativeInflation
		if infl <= 0 {
			infl = 1
		}
		a.months++
		a.spendN += m.TotalExpenses
		a.spendR += m.TotalExpenses / infl
		a.incomeN += m.TotalIncome
		a.incomeR += m.TotalIncome / infl
		a.drawN += m.NetWithdrawal
		a.drawR += m.NetWithdrawal / infl
		a.rmdN += m.RMDWithdrawal
		a.rmdR += m.RMDWithdrawal / infl
		a.endBalance = m.PortfolioBalance
		a.depleted = a.depleted || m.Depleted
	}

	// Year starting balances: the canonical loop records them in the yearly
	// summaries; fall back to the prior year's ending balance (year 0: the
	// configured portfolio value).
	//
	// Phase names: the yearly summary records the label from whichever
	// settings were ACTIVE that year, so a scenario chain's linked settings
	// are reflected after a transition. The engine's sentinel contract: a
	// summary PhaseName of "-" means the engine says no phase was active
	// that year (phases disabled/unconfigured on the ACTIVE settings) — use
	// it as-is, it already matches what trajectoryPhaseName renders for
	// disabled phases. A summary PhaseName of "" means the engine recorded
	// nothing at all (a projection built before this field existed) —
	// legacy fallback to trajectoryPhaseName (computed from the PRIMARY
	// settings passed in). Only "" triggers the fallback; "-" flows
	// straight through.
	startBalance := map[int]float64{}
	phaseNameByYear := map[int]string{}
	for _, ys := range projection.YearlySummaries {
		startBalance[ys.Year] = ys.StartingBalance
		if ys.PhaseName != "" {
			phaseNameByYear[ys.Year] = ys.PhaseName
		}
	}
	prevEnd := s.PortfolioValue
	for _, yi := range years {
		if _, ok := startBalance[yi]; !ok {
			startBalance[yi] = prevEnd
		}
		prevEnd = byYear[yi].endBalance
	}

	lastYear := years[len(years)-1]
	rows := make([]trajectoryRow, 0, len(years)/2+2)
	for _, yi := range years {
		if yi%2 != 0 && yi != lastYear {
			continue
		}
		a := byYear[yi]
		n := float64(a.months)
		phaseName := phaseNameByYear[yi]
		if phaseName == "" {
			phaseName = trajectoryPhaseName(s, yi)
		}
		row := trajectoryRow{
			Year:          yi,
			PrimaryAge:    s.PrimaryAgeAt(yi),
			SpouseAge:     s.SpouseAgeAt(yi),
			PhaseName:     phaseName,
			SpendNominal:  a.spendN / n,
			SpendReal:     a.spendR / n,
			IncomeNominal: a.incomeN / n,
			IncomeReal:    a.incomeR / n,
			DrawNominal:   a.drawN / n,
			DrawReal:      a.drawR / n,
			RMDNominal:    a.rmdN / n,
			RMDReal:       a.rmdR / n,
			Depleted:      a.depleted,
		}
		if sb := startBalance[yi]; sb > 0 {
			row.WithdrawalRate = a.drawN / sb * 100
		}
		rows = append(rows, row)
	}
	return rows
}

// handleWhatIfSpendingTrajectory renders the Spending Trajectory table rows
// from the engine projection (GET /whatif/spending-trajectory). This replaced
// a client-side mini-model that re-simulated balances and RMDs from form
// fields — ignoring Roth conversions, taxes, and the engine's withdrawal
// order — and could contradict the projection shown on the same page.
func handleWhatIfSpendingTrajectory(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	in, _, err := buildEngineInput(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	analysis, _, err := analysisFastOrCached(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepared settings carry the engine's own derived ages, so the age and
	// phase columns match what the projection actually simulated.
	prepared := in.Prepared.Settings()
	rows := buildSpendingTrajectoryRows(prepared, analysis.Projection)

	data := map[string]interface{}{
		"Rows":      rows,
		"HasSpouse": prepared.HasSpouse(),
	}
	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-spending-trajectory-rows", data)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}
