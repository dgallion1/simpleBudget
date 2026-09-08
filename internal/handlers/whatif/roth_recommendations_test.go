package whatif

import (
	"budget2/internal/models"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRothFixedEditsClearSchedule(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.RothConversion = &models.RothConversionConfig{Enabled: true, EndYear: 4, PerYearOverrides: map[int]float64{0: 99999}}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", strings.NewReader("enabled=on&annual_amount=25000&start_year=0&end_year=4"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, req)
	got, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.RothConversion.PerYearOverrides != nil {
		t.Fatal("fixed edit retained overriding schedule")
	}
	if got.RothConversion.AnnualAmount != 25000 {
		t.Fatal("amount not saved")
	}
}

func TestRothSweepCandidateClearsSchedule(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: map[int]float64{0: 99999}}
	got := candidateSettingsForConversionAmount(s, 25000)
	if got.RothConversion.PerYearOverrides != nil {
		t.Fatal("sweep candidate still uses schedule")
	}
	if s.RothConversion.PerYearOverrides[0] != 99999 {
		t.Fatal("source mutated")
	}
}
