package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
)

// RMD start age per IRS rules (SECURE 2.0 Act). Re-exported from the
// engine package during the migration window so handler/template code
// reading retirement.RMDStartAge keeps compiling.
const RMDStartAge = engine.RMDStartAge

// RMD-day helpers were moved to the engine package. The aliases below
// keep existing call sites in calculator.go, backtest.go, handler
// code, and tests compiling unchanged. The aliases (and these
// declarations) are removed in Task 8.
// FirstRMDCalendarYear moved to eligibility.go in this package.
var (
	EffectiveRMDStartAge   = engine.EffectiveRMDStartAge
	RMDApplies             = engine.RMDApplies
	RMDAgeForCalendarYear  = engine.RMDAgeForCalendarYear
	CalculateRMD           = engine.CalculateRMD
	GetLifeExpectancyFactor = engine.GetLifeExpectancyFactor
)

// The retirement-side rmdTriggerMonth/parseStartYear aliases were
// retired with backtest.go's move to analysis. Engine still owns the
// canonical implementations (engine.RMDTriggerMonth,
// engine.ParseStartYear); analysis-side callers reach for them
// directly.

// BuildRMDAnalysis (F-072) is a thin delegator over analysis.BuildRMD.
// The body lived here through Task 1; Task 2 moved the math into the
// pure analysis package so it composes with the engine-driven
// projection without going through Calculator.
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	return analysis.BuildRMD(projection, engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
