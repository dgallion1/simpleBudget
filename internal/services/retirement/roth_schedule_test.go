package retirement

import (
	"budget2/internal/models"
	"reflect"
	"testing"
)

func TestRebaseRothSchedule(t *testing.T) {
	cfg := &models.RothConversionConfig{Enabled: true, EndYear: 5, PerYearOverrides: map[int]float64{0: 111, 3: 222, 5: 333}}
	got := rebaseRothConversion(cfg, 3)
	if !reflect.DeepEqual(got.PerYearOverrides, map[int]float64{0: 222, 2: 333}) {
		t.Fatalf("schedule not rebased: %v", got.PerYearOverrides)
	}
	if cfg.PerYearOverrides[0] != 111 {
		t.Fatal("source mutated")
	}
}
