package retirement

import (
	"budget2/internal/services/retirement/engine"
)

// init wires the engine package's Social Security hooks to the
// retirement-side helpers. The engine doesn't import retirement
// (cycle), so the projection-loop helpers in the engine package
// reach into retirement's SS optimizer through these function-valued
// vars.
//
// Removed in Task 8 once SS analysis lives inside engine.
func init() {
	engine.SocialSecurityProjectionActive = socialSecurityProjectionActive
	engine.ProjectedSocialSecurityIncome = projectedSocialSecurityIncome
}
