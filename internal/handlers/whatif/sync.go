package whatif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
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

	// Calculate average monthly values from last 12 months, pinned to
	// LOCAL midnight of today rather than the live instant. Two calls made
	// minutes apart -- preview, then apply -- must see the identical value
	// here so they compute the identical plan (and therefore the identical
	// syncPlanHash); without pinning, "months" below carries live
	// sub-second time.Now() drift into NewMonthlyExpenses, and the
	// sync-confirm guard would reject unchanged data as stale on every
	// apply. This must be a LOCAL calendar day, not time.Now().Truncate
	// (which rounds to a UTC-epoch boundary): in any zone ahead of UTC that
	// silently excludes same-day transactions once the wall clock crosses
	// local midnight but UTC hasn't yet -- FilterByDateRange below already
	// extends the end bound to 23:59:59.999999999 in this Location, so today
	// is still fully included.
	realNow := time.Now()
	now := time.Date(realNow.Year(), realNow.Month(), realNow.Day(), 0, 0, 0, 0, realNow.Location())
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
	// summary makes the same living/healthcare split). The comparison is
	// case-insensitive to match TransactionSet.FilterByCategory, which the
	// dashboard's healthcare split uses.
	var totalExpenses float64
	for _, t := range outflows.Transactions {
		if strings.EqualFold(t.Category, metrics.HealthInsuranceCategory) {
			continue
		}
		totalExpenses += t.Amount
	}
	totalExpenses = math.Abs(totalExpenses)

	plan := &syncPlan{
		OldMonthlyExpenses: settings.MonthlyLivingExpenses,
		NewMonthlyExpenses: totalExpenses / months,
	}

	// Use insights income pattern detection for individual income sources.
	// IncomePatterns groups by a map internally, so its return order is not
	// stable across calls; sort here so two calls against unchanged data
	// produce byte-identical Added/Updated/Skipped/Sources and therefore the
	// same syncPlanHash -- an unstable order would make the sync-confirm
	// guard reject unchanged data as "stale".
	incomePatterns := insights.IncomePatterns(filtered)
	sort.Slice(incomePatterns, func(i, j int) bool {
		return incomePatterns[i].Description < incomePatterns[j].Description
	})

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

// syncPlanHash is the single source of what "the same plan" means between
// preview and apply — both handlers call this, never their own encoding, or
// the two could drift and pass a plan the user never actually reviewed.
// SHA-256 over the plan's canonical (struct-order) JSON encoding: syncPlan
// has no maps, so json.Marshal's field order is already deterministic.
func syncPlanHash(plan *syncPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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

// syncPreviewResponse is the JSON fallback shape for handleWhatIfSync
// (used when no renderer is wired). It embeds the plan fields at the top
// level for backward compatibility and adds the three guard values a caller
// must echo back to /whatif/sync/apply.
type syncPreviewResponse struct {
	*syncPlan
	ExpectedScenario string `json:"expected_scenario"`
	PlanHash         string `json:"plan_hash"`
	ExpectedRevision int    `json:"expected_revision"`
}

// handleWhatIfSync previews the changes a dashboard sync would make —
// nothing is saved until the user confirms via /whatif/sync/apply. The
// sync overwrites MonthlyLivingExpenses and rebuilds auto-detected income
// sources, so a silent save would clobber deliberately set values.
//
// ExpectedScenario, PlanHash, and ExpectedRevision bind the confirmation to
// what was actually previewed. handleWhatIfSyncApply rejects a mismatch
// instead of writing: see its doc comment for why.
//
// ExpectedRevision is obtained from LoadContextWithRevision, under the SAME
// lock as the settings load below -- not by calling LoadContext and then
// retirementMgr's revision separately. A load-then-read-revision sequence
// would leave a window between the two calls for a concurrent save to bump
// the counter, and this handler would then echo back a revision that
// describes a LATER snapshot than the one it actually computed the plan
// from -- recreating, at the read end, exactly the TOCTOU this whole guard
// exists to close at the write end (Z7).
func handleWhatIfSync(w http.ResponseWriter, r *http.Request) {
	settings, expectedRevision, err := retirementMgr.LoadContextWithRevision(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	plan, err := computeDashboardSync(settings)
	if err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	hash, err := syncPlanHash(plan)
	if err != nil {
		renderError(w, "Failed to hash sync plan: "+err.Error(), http.StatusInternalServerError)
		return
	}
	expectedScenario := retirementMgr.ActiveFilename()

	if renderer != nil {
		_ = renderer.RenderPartial(w, "whatif-sync-preview", map[string]any{
			"Plan":             plan,
			"ExpectedScenario": expectedScenario,
			"PlanHash":         hash,
			"ExpectedRevision": expectedRevision,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(syncPreviewResponse{
		syncPlan:         plan,
		ExpectedScenario: expectedScenario,
		PlanHash:         hash,
		ExpectedRevision: expectedRevision,
	})
}

// syncApplyRaceTestHook, when non-nil, runs in handleWhatIfSyncApply
// immediately before the confirmed sync is saved -- after every one of the
// handler's own unlocked pre-checks (expected_scenario, plan_hash) and
// before saveAndRecalcIfScenario's locked write. It exists only so a test
// can deterministically land a concurrent scenario switch in the exact
// window a caller-side check cannot close on its own -- the TOCTOU an
// earlier attempt at this guard left open, closed by
// SettingsManager.SaveWithRevisionIfScenario's locked re-check. Production
// code leaves this nil; it is never invoked outside tests.
var syncApplyRaceTestHook func()

// handleWhatIfSyncApply performs the confirmed sync: recompute from the
// dashboard, save, and render the standard results partial.
//
// expected_scenario, plan_hash, AND expected_revision are all required
// (mirrors handleWhatIfApply's expected_scenario guard,
// internal/handlers/whatif/handlers_live.go): a client that skipped preview,
// or whose preview is stale, gets 400/409 instead of an unreviewed write.
// Between preview and apply, another tab/MCP call can switch the active
// scenario, change the transactions the plan was computed from, OR save a
// same-scenario edit directly to settings (e.g. a rate change on the
// Settings page) — without these guards the confirmed values could land on
// top of a plan, or a setting, the user never looked at.
//
// The check below (against retirementMgr.ActiveFilename()) is a fast-fail
// for UX only — it is NOT what makes this safe, because it releases the lock
// before computeDashboardSync/applySyncPlan run, leaving a window where a
// scenario switch OR a same-scenario concurrent save lands between the check
// and the write. The AUTHORITATIVE check is inside saveAndRecalcIfScenario,
// which re-compares BOTH expectedScenario against the active scenario AND
// expectedRevision against the manager's current revision, and performs the
// write, all in the SAME held lock (SettingsManager.SaveWithRevisionIfScenario)
// — see that method and ApplyOverrides' doc comment. A mismatch on EITHER
// check there surfaces as *retirement.ScenarioConflictError, which
// saveAndRecalcIfScenario itself intercepts and renders as a retargeted 409
// into #whatif-sync-preview, and nothing is written. This closes the
// lost-update window a scenario-only guard leaves open: a same-scenario
// concurrent edit (e.g. DiscountRate changed via the Settings page) landing
// between this handler's LoadContext above and the guarded save used to be
// silently reverted by the whole-object save below, because the earlier
// guard compared only the filename — never whether the loaded snapshot was
// still current (Z7).
//
// The plan_hash comparison, by contrast, does NOT need to be inside that
// lock: the plan actually saved is the one recomputed by computeDashboardSync
// just above (freshly, at apply time, from current settings/transactions),
// not the one hashed at preview time. The hash only binds this apply to what
// the user reviewed being still current; it names no mutable manager state
// that could rot between an unlocked check and the locked write. The
// scenario identity and the settings revision both do that, which is exactly
// what the locked checks inside SaveWithRevisionIfScenario guard.
func handleWhatIfSyncApply(w http.ResponseWriter, r *http.Request) {
	expectedScenario := strings.TrimSpace(r.FormValue("expected_scenario"))
	if expectedScenario == "" {
		renderRetargetedError(w, "expected_scenario is required: re-open the sync preview and apply from there", http.StatusBadRequest, "#whatif-sync-preview")
		return
	}
	expectedHash := strings.TrimSpace(r.FormValue("plan_hash"))
	if expectedHash == "" {
		renderRetargetedError(w, "plan_hash is required: re-open the sync preview and apply from there", http.StatusBadRequest, "#whatif-sync-preview")
		return
	}
	expectedRevisionRaw := strings.TrimSpace(r.FormValue("expected_revision"))
	if expectedRevisionRaw == "" {
		renderRetargetedError(w, "expected_revision is required: re-open the sync preview and apply from there", http.StatusBadRequest, "#whatif-sync-preview")
		return
	}
	expectedRevision, err := strconv.Atoi(expectedRevisionRaw)
	if err != nil {
		renderRetargetedError(w, "expected_revision is invalid: re-open the sync preview and apply from there", http.StatusBadRequest, "#whatif-sync-preview")
		return
	}

	settings, err := retirementMgr.LoadContext(r.Context())
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fast-fail only; see the doc comment above for why this is not the
	// guard that makes the write safe.
	if active := retirementMgr.ActiveFilename(); active != expectedScenario {
		renderRetargetedError(w, fmt.Sprintf(
			"refusing to apply: the active scenario is %s, but this sync was previewed for %s "+
				"(the active scenario changed since the preview). Re-open the sync preview and try again",
			active, expectedScenario), http.StatusConflict, "#whatif-sync-preview")
		return
	}

	plan, err := computeDashboardSync(settings)
	if err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}
	hash, err := syncPlanHash(plan)
	if err != nil {
		renderError(w, "Failed to hash sync plan: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if hash != expectedHash {
		renderRetargetedError(w, "refusing to apply: the dashboard data changed since this sync was previewed. "+
			"Re-open the sync preview and try again", http.StatusConflict, "#whatif-sync-preview")
		return
	}

	if syncApplyRaceTestHook != nil {
		syncApplyRaceTestHook()
	}

	applySyncPlan(settings, plan)
	saveAndRecalcIfScenario(w, r, settings, expectedScenario, expectedRevision)
}
