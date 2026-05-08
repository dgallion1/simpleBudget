package retirement

import (
	"budget2/internal/services/retirement/engine"
)

// newGuardrailState forwards to engine.NewGuardrailState. The
// retirement-side guardrailState type alias was retired with
// backtest.go's move to analysis; tests still consume the
// constructor through this alias. Removed in Task 8.
var newGuardrailState = engine.NewGuardrailState
