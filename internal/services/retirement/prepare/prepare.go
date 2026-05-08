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
	clone, err := DeepCopy(cfg)
	if err != nil {
		return PreparedSettings{}, fmt.Errorf("prepare.From: deep copy: %w", err)
	}
	NormalizePhaseAgeReference(clone)
	ComputeAges(clone)
	if err := ValidatePersons(clone); err != nil {
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
