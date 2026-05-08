package engine

import (
	"budget2/internal/services/retirement/prepare"
)

// Input bundles everything Engine.Run needs. Chain may be nil for
// single-scenario projections.
type Input struct {
	Prepared prepare.PreparedSettings
	Chain    []PreparedChainLink
}

// PreparedChainLink describes a scenario transition that fires when the
// reference person reaches TransitionAge. Settings is the prepared
// snapshot for the post-transition scenario.
type PreparedChainLink struct {
	ScenarioFilename string
	TransitionAge    int
	Settings         prepare.PreparedSettings
}
