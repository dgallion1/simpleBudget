package overrides

import (
	"budget2/internal/models"
	"testing"
)

func TestFixedRothOverrideClearsSchedule(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: map[int]float64{0: 99999}}
	amount := 12345.0
	got, err := Apply(s, Overrides{RothConversionAmount: &amount})
	if err != nil {
		t.Fatal(err)
	}
	if got.RothConversion.PerYearOverrides != nil {
		t.Fatal("fixed override retained schedule")
	}
	if s.RothConversion.PerYearOverrides[0] != 99999 {
		t.Fatal("source mutated")
	}
}
