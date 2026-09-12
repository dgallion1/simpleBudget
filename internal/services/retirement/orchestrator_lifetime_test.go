package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"testing"
)

func TestFastAnalysisCalculationErrorSuppressesDerivedSuccess(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	failed := &models.ProjectionResult{CalculationError: "settlement failed", Survives: true, FinalBalance: 999999}
	got := fastAnalysis(in, failed)
	if got.CalculationError != "settlement failed" || got.Settings == nil || got.Projection != failed {
		t.Fatalf("error shell=%+v", got)
	}
	if got.BudgetFit != nil || got.PresentValue != nil || got.Sustainability != nil || got.RMD != nil || got.Tax != nil || got.ProjectionExplainability != nil {
		t.Fatalf("stale derived success leaked: %+v", got)
	}
}
