package retirement

import (
	"budget2/internal/services/retirement/engine"
)

// RMD start age per IRS rules (SECURE 2.0 Act). Re-exported from the
// engine package so handler/template code reading retirement.RMDStartAge
// keeps compiling.
const RMDStartAge = engine.RMDStartAge

// RMD-day helpers re-exported from the engine package.
// FirstRMDCalendarYear lives in eligibility.go in this package.
var (
	EffectiveRMDStartAge    = engine.EffectiveRMDStartAge
	RMDApplies              = engine.RMDApplies
	RMDAgeForCalendarYear   = engine.RMDAgeForCalendarYear
	CalculateRMD            = engine.CalculateRMD
	GetLifeExpectancyFactor = engine.GetLifeExpectancyFactor
)
