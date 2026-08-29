package whatif

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/internal/services/metrics"
)

// syncSourceChange records a proposed amount update to an existing
// auto-detected income source.
type syncSourceChange struct {
	Name      string
	OldAmount float64
	NewAmount float64
}

// syncSkippedPattern is a detected income pattern the sync deliberately
// leaves out of the plan, with the reason shown to the user.
type syncSkippedPattern struct {
	Name     string
	Reason   string
	LastDate time.Time
}

// syncPlan is the full set of changes a dashboard sync proposes. It is
// computed without touching settings so the user can review it first.
type syncPlan struct {
	OldMonthlyExpenses float64
	NewMonthlyExpenses float64

	// Sources is the complete proposed income-source list (user sources
	// preserved, insights sources rebuilt).
	Sources []models.IncomeSource

	Added   []models.IncomeSource
	Updated []syncSourceChange
	Removed []models.IncomeSource
	Skipped []syncSkippedPattern
}

// HasChanges reports whether applying the plan would alter settings.
func (p *syncPlan) HasChanges() bool {
	return len(p.Added) > 0 || len(p.Updated) > 0 || len(p.Removed) > 0 ||
		math.Abs(p.NewMonthlyExpenses-p.OldMonthlyExpenses) >= 0.005
}

// socialSecurityModeled reports whether the plan already models Social
// Security from the gross benefit via SocialSecurityConfig. Bank deposits
// are the NET benefit, so syncing them as an income source on top of an
// active config double-counts SS.
func socialSecurityModeled(s *models.WhatIfSettings) bool {
	return s.SocialSecurity != nil &&
		(s.SocialSecurity.FRABenefit > 0 || s.SocialSecurity.SpouseFRABenefit > 0)
}

// looksLikeSocialSecurity reports whether an income pattern description
// reads like a Social Security benefit deposit.
func looksLikeSocialSecurity(desc string) bool {
	d := strings.ToLower(desc)
	for _, marker := range []string{"social security", "soc sec", "ssa treas", "ssi treas"} {
		if strings.Contains(d, marker) {
			return true
		}
	}
	return false
}

// incomePatternStale reports whether a regular income pattern has ended:
// its most recent deposit is more than two cadence intervals (plus a week
// of grace) in the past. Ended income still inside the trailing window —
// a job that stopped months ago — must not be projected forward.
func incomePatternStale(p models.IncomePattern, now time.Time) bool {
	if p.LastDate.IsZero() {
		return false
	}
	var nominalDays float64
	switch p.Frequency {
	case "weekly":
		nominalDays = 7
	case "biweekly":
		nominalDays = 14
	case "monthly":
		nominalDays = 30.4375
	default:
		return false
	}
	gapDays := now.Sub(p.LastDate).Hours() / 24
	return gapDays > nominalDays*2+7
}

// computeDashboardSync derives the sync plan from the trailing 12 months of
// transaction data without modifying settings.
func computeDashboardSync(settings *models.WhatIfSettings) (*syncPlan, error) {
	data, err := loader.LoadData()
	if err != nil {
		return nil, err
	}

	// Calculate average monthly values from last 12 months
	now := time.Now()
	yearAgo := now.AddDate(-1, 0, 0)
	filtered := data.Active().FilterByDateRange(yearAgo, now)
	outflows := filtered.FilterByType(models.Outflow)

	months := 12.0
	if filtered.MinDate().After(yearAgo) {
		months = now.Sub(filtered.MinDate()).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}

	// Average monthly living expenses. Signed sum + math.Abs so refunds
	// reduce the total instead of inflating it. Health Insurance outflows
	// are excluded: the plan's healthcare persons model those premiums, so
	// folding them into living expenses would double-count them (the spend
	// summary makes the same living/healthcare split).
	var totalExpenses float64
	for _, t := range outflows.Transactions {
		if t.Category == metrics.HealthInsuranceCategory {
			continue
		}
		totalExpenses += t.Amount
	}
	totalExpenses = math.Abs(totalExpenses)

	plan := &syncPlan{
		OldMonthlyExpenses: settings.MonthlyLivingExpenses,
		NewMonthlyExpenses: totalExpenses / months,
	}

	// Use insights income pattern detection for individual income sources
	incomePatterns := insights.IncomePatterns(filtered)

	// Remove old auto-detected sources (prefixed with "insights-" or old "dashboard-income")
	// Keep user-added sources (no special prefix)
	// BUT preserve user modifications (EndMonth, StartMonth, COLARate, Type) from existing insights sources
	userSources := make([]models.IncomeSource, 0)
	existingMods := make(map[string]models.IncomeSource)

	for _, src := range settings.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") || src.ID == "dashboard-income" {
			// Save user modifications for this auto-detected source
			existingMods[src.ID] = src
		} else {
			userSources = append(userSources, src)
		}
	}

	ssModeled := socialSecurityModeled(settings)
	titler := cases.Title(language.English)
	rebuilt := make(map[string]bool)

	// Convert detected income patterns to income sources
	for _, pattern := range incomePatterns {
		// Only include regular income patterns (skip one-time or irregular)
		if !pattern.IsRegular {
			continue
		}

		displayName := titler.String(pattern.Description)

		if incomePatternStale(pattern, now) {
			plan.Skipped = append(plan.Skipped, syncSkippedPattern{
				Name:     displayName,
				Reason:   "ended — no deposit recent enough for its " + pattern.Frequency + " cadence",
				LastDate: pattern.LastDate,
			})
			continue
		}

		if ssModeled && looksLikeSocialSecurity(pattern.Description) {
			plan.Skipped = append(plan.Skipped, syncSkippedPattern{
				Name:     displayName,
				Reason:   "already modeled by the plan's Social Security settings (gross benefit); syncing the net deposits would double-count it",
				LastDate: pattern.LastDate,
			})
			continue
		}

		// Convert to monthly amount based on frequency
		monthlyAmount := pattern.AvgAmount
		switch pattern.Frequency {
		case "weekly":
			monthlyAmount = pattern.AvgAmount * 52 / 12
		case "biweekly":
			monthlyAmount = pattern.AvgAmount * 26 / 12
			// monthly is already correct
		}

		// Create a stable ID from the description
		id := "insights-" + strings.ToLower(strings.ReplaceAll(pattern.Description, " ", "-"))

		newSource := models.IncomeSource{
			ID:     id,
			Name:   displayName,
			Amount: monthlyAmount,
			Type:   models.IncomeFixed,
		}

		// Preserve user modifications from existing source with same ID
		if existing, ok := existingMods[id]; ok {
			newSource.EndMonth = existing.EndMonth
			newSource.StartMonth = existing.StartMonth
			newSource.COLARate = existing.COLARate
			newSource.InflationAdjusted = existing.InflationAdjusted
			// Preserve Type only if user changed it from default
			if existing.Type != "" && existing.Type != models.IncomeFixed {
				newSource.Type = existing.Type
			}
			if math.Abs(existing.Amount-newSource.Amount) >= 0.005 {
				plan.Updated = append(plan.Updated, syncSourceChange{
					Name:      newSource.Name,
					OldAmount: existing.Amount,
					NewAmount: newSource.Amount,
				})
			}
		} else {
			plan.Added = append(plan.Added, newSource)
		}
		rebuilt[id] = true

		userSources = append(userSources, newSource)
	}

	// Existing auto-detected sources that no pattern re-created get removed.
	for id, src := range existingMods {
		if !rebuilt[id] {
			plan.Removed = append(plan.Removed, src)
		}
	}
	sort.Slice(plan.Removed, func(i, j int) bool { return plan.Removed[i].ID < plan.Removed[j].ID })

	plan.Sources = userSources
	return plan, nil
}

// applySyncPlan writes the plan's proposed values into settings.
func applySyncPlan(settings *models.WhatIfSettings, plan *syncPlan) {
	settings.MonthlyLivingExpenses = plan.NewMonthlyExpenses
	settings.IncomeSources = plan.Sources
}

// syncSettingsFromDashboard updates settings with values from dashboard data
func syncSettingsFromDashboard(settings *models.WhatIfSettings) error {
	plan, err := computeDashboardSync(settings)
	if err != nil {
		return err
	}
	applySyncPlan(settings, plan)
	return nil
}

// handleWhatIfSync previews the changes a dashboard sync would make —
// nothing is saved until the user confirms via /whatif/sync/apply. The
// sync overwrites MonthlyLivingExpenses and rebuilds auto-detected income
// sources, so a silent save would clobber deliberately set values.
func handleWhatIfSync(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	plan, err := computeDashboardSync(settings)
	if err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-sync-preview", map[string]any{"Plan": plan})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(plan)
}

// handleWhatIfSyncApply performs the confirmed sync: recompute from the
// dashboard, save, and render the standard results partial.
func handleWhatIfSyncApply(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := syncSettingsFromDashboard(settings); err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	saveAndRecalc(w, r, settings)
}
