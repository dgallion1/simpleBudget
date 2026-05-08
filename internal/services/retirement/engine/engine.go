// Package engine runs deterministic retirement projections from prepared
// settings. It has no caching, no global state, and Run is a pure
// function of its Input.
package engine

import (
	"budget2/internal/models"
)

// Engine is a stateless projection runner. Future deepening (caching,
// tracing, fault injection) can land on the struct without changing
// call sites.
type Engine struct{}

// New returns an Engine. Cheap; callers may construct per request.
func New() *Engine { return &Engine{} }

// Run produces a deterministic monthly projection for in. Returns a
// fully populated *models.ProjectionResult. Never returns nil. Run is a
// pure function of in.
//
// During Task 1a/1b/1c the body is a stub; the real implementation
// arrives in Task 1d once all helper dependencies have been moved into
// this package.
func (e *Engine) Run(in Input) *models.ProjectionResult {
	panic("engine.Run: not yet implemented (arrives in Task 1d)")
}
