// Package prepare turns a user-facing WhatIfSettings configuration into a
// PreparedSettings witness that the retirement engine accepts.
//
// Preparation is:
//  1. Deep-copy the config (so mutations to the original don't leak).
//  2. Normalize derived state (phase reference, ages).
//  3. Validate cross-field invariants (persons).
//
// The witness type's purpose is to make "I expect normalized settings" a
// compile-time guarantee at the engine boundary instead of a documented
// convention scattered across Load/Save/chain.
package prepare

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"budget2/internal/models"
)

// PreparedSettings is the retirement engine's input. Constructable only via
// From or MustFrom. The underlying *WhatIfSettings has been deep-copied and
// normalized; treat it as read-only.
type PreparedSettings struct {
	s *models.WhatIfSettings
}

// Settings returns the prepared snapshot. Callers MUST NOT mutate the
// returned pointer; doing so violates the prepared invariants. The contract
// is documented rather than type-enforced; see plan
// docs/superpowers/plans/2026-05-08-prepared-settings.md.
func (p PreparedSettings) Settings() *models.WhatIfSettings {
	return p.s
}

// IsZero reports whether p is the zero value (constructed without From).
func (p PreparedSettings) IsZero() bool {
	return p.s == nil
}

// From deep-copies, normalizes, and validates a configuration, returning a
// PreparedSettings ready for the engine.
//
// From is idempotent: passing already-normalized settings produces an
// equivalent PreparedSettings (the deep copy still happens).
func From(cfg *models.WhatIfSettings) (PreparedSettings, error) {
	if cfg == nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: nil settings")
	}
	if err := validateLivingSpendingBoost(cfg.LivingSpendingBoost); err != nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: validate: %w", err)
	}
	clone, err := DeepCopy(cfg)
	if err != nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: deep copy: %w", err)
	}
	NormalizePhaseAgeReference(clone)
	ComputeAges(clone)
	if err := ValidatePersons(clone); err != nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: validate: %w", err)
	}
	if err := ValidateOneTimeExpenses(clone); err != nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: validate: %w", err)
	}
	return PreparedSettings{s: clone}, nil
}

// MustFrom wraps From for tests. It calls tb.Fatalf on error.
func MustFrom(tb testing.TB, cfg *models.WhatIfSettings) PreparedSettings {
	tb.Helper()
	p, err := From(cfg)
	if err != nil {
		tb.Fatalf("prepare.MustFrom: %v", err)
	}
	return p
}

// DeepCopy returns a deep clone of cfg via JSON round-trip. Fields marked
// json:"-" (CurrentAge, SpouseAge) are dropped by marshal but are re-derived
// by ComputeAges in From, so the round-trip is lossless for callers of From.
//
// Performance: ~microseconds per call. The projection loops dominate runtime
// in every analysis that calls this. Replace with structure-aware copy only
// if profiling proves it.
func DeepCopy(cfg *models.WhatIfSettings) (*models.WhatIfSettings, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil settings")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	out := &models.WhatIfSettings{}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}

// Clone returns a deep copy of cfg that is lossless: unlike DeepCopy it also
// carries the fields tagged json:"-", which the JSON round-trip silently
// drops.
//
// Use Clone (not DeepCopy) whenever the copy is handed to a caller that will
// read those fields WITHOUT first going through From. DeepCopy is only safe
// when From re-derives them: From runs ComputeAges, so CurrentAge/SpouseAge
// come back. Saved Roth schedules are JSON-visible and deep-copy normally.
//
// The set of carried fields is enforced by TestCloneCarriesEveryJSONOmittedField,
// which reflects over models.WhatIfSettings rather than hard-coding a list, so
// a newly added json:"-" field fails the test instead of being dropped.
//
// NOT value-faithful for empty slices. Clone inherits DeepCopy's JSON round
// trip, so an omitempty field holding a non-nil empty slice is omitted on
// marshal and comes back nil: RemovedIncomeSources, RemovedExpenseSources,
// BigTicketItems and RemovedBigTicketItems all make that trip on a default
// plan. Benign for every current caller — append, len and range treat nil and
// empty alike, templates render both as nothing, and omitempty emits the same
// bytes for either, so nothing reaches disk differently — but it is a real
// change of value, so reflect.DeepEqual(cfg, clone) is FALSE even when every
// field prints identically. Two tests in the retirement package
// (TestLoad_CacheHitOnSecondCall,
// TestSettingsManager_LoadReturnsCacheOnSubsequentCalls) compare two Load
// results with DeepEqual and only pass because BOTH operands are Clones and
// so share the nil-ing; making this value-faithful is safe, but check them
// first. The one place that re-fills the nils is initializeLoadedSettings, on
// the disk-decode path, which Clone does not go through.
func Clone(cfg *models.WhatIfSettings) (*models.WhatIfSettings, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil settings")
	}
	out, err := DeepCopy(cfg)
	if err != nil {
		return nil, err
	}
	carryJSONOmittedFields(cfg, out)
	return out, nil
}

// carryJSONOmittedFields copies src's json:"-" fields onto dst. Reference
// types are copied, not aliased: the whole point of Clone is that mutating
// one settings object cannot be observed through another.
//
// Keep this in sync with the json:"-" fields reachable from
// models.WhatIfSettings; TestCloneCarriesEveryJSONOmittedField enumerates them
// by reflection and fails if one is missed.
func carryJSONOmittedFields(src, dst *models.WhatIfSettings) {
	dst.CurrentAge = src.CurrentAge
	dst.SpouseAge = src.SpouseAge

}

// Saved schedules may already be expired or end beyond the modeled horizon.
// Active-request timing constraints belong to the optimizer request boundary.
func validateLivingSpendingBoost(boost *models.LivingSpendingBoost) error {
	if boost == nil {
		return nil
	}
	amount := boost.MonthlyReal
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || math.IsInf(amount*100, 0) || math.Round(amount*100)/100 != amount {
		return fmt.Errorf("living_spending_boost.monthly_real must be finite, positive, and in whole cents")
	}
	if _, err := models.ParseYearMonth(boost.StopMonth); err != nil {
		return fmt.Errorf("living_spending_boost.stop_month: %w", err)
	}
	return nil
}
