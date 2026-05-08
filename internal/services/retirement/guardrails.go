package retirement

import (
	"budget2/internal/services/retirement/engine"
)

// guardrailState is a parity-window alias for engine.GuardrailState so
// existing retirement-package call sites compile unchanged. The alias
// (and the var-alias for the constructor) is removed in Task 8 once
// all guardrail call sites have moved to the engine package.
type guardrailState = engine.GuardrailState

// newGuardrailState forwards to engine.NewGuardrailState.
var newGuardrailState = engine.NewGuardrailState
