package retirement

import (
	"budget2/internal/services/retirement/engine"
)

// newGuardrailState forwards to engine.NewGuardrailState. Kept as a
// compat shim for retirement-package guardrails_test, which calls the
// constructor by its lowercase name.
var newGuardrailState = engine.NewGuardrailState
