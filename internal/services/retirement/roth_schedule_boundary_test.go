package retirement

import (
	"budget2/internal/models"
	"testing"
)

func TestRebaseSavedScheduleInclusiveBoundary(t *testing.T) {
	cfg := &models.RothConversionConfig{Enabled: true, EndYear: 3, PerYearOverrides: map[int]float64{3: 12345}}
	got := rebaseRothConversion(cfg, 3)
	if !got.Enabled || got.PerYearOverrides[0] != 12345 {
		t.Fatalf("final scheduled conversion dropped at transition: %#v", got)
	}
}
