package overrides

import (
	"errors"
	"strings"
	"testing"

	"budget2/internal/models"
)

// windowScenario is a scenario with a Roth conversion window already saved, so
// a sparse override lands on top of stored years rather than on nothing.
func windowScenario(start, end int, amount float64) *models.WhatIfSettings {
	s := baseSettings()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      amount > 0,
		AnnualAmount: amount,
		StartYear:    start,
		EndYear:      end,
	}
	return s
}

// TestApply_RejectsInvertedWindowFromASparseOverride is the regression. The
// per-request check can only compare the years the request itself carries: a
// request supplying only an end year was compared against nothing and merged
// on top of a later saved start year, producing a window that converts in no
// year at all — silently, because every field in it is individually valid.
func TestApply_RejectsInvertedWindowFromASparseOverride(t *testing.T) {
	tests := []struct {
		name       string
		savedStart int
		savedEnd   int
		o          Overrides
	}{
		{
			name:       "end year alone lands before the saved start year",
			savedStart: 10,
			savedEnd:   20,
			o:          Overrides{RothConversionAmount: ptr(50_000.0), RothConversionEnd: ptrInt(5)},
		},
		{
			name:       "start year alone lands after the saved end year",
			savedStart: 2,
			savedEnd:   5,
			o:          Overrides{RothConversionAmount: ptr(50_000.0), RothConversionStart: ptrInt(10)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := windowScenario(tt.savedStart, tt.savedEnd, 40_000)

			got, err := Apply(base, tt.o)
			if err == nil {
				t.Fatalf("Apply accepted a window of year %d to year %d, which runs zero conversions",
					got.RothConversion.StartYear, got.RothConversion.EndYear)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err=%v (%T), want a *ValidationError so handlers map it to 400", err, err)
			}
			if !strings.Contains(err.Error(), "zero conversions") {
				t.Errorf("err=%q, want it to say the window runs zero conversions", err)
			}
		})
	}
}

// TestApply_RejectsNegativeConversionYears covers the other half: neither year
// had a lower bound, so a negative start or end passed validation and merged
// into the saved scenario.
func TestApply_RejectsNegativeConversionYears(t *testing.T) {
	tests := []struct {
		name string
		o    Overrides
	}{
		{"negative start", Overrides{RothConversionAmount: ptr(50_000.0), RothConversionStart: ptrInt(-1)}},
		{"negative end", Overrides{RothConversionAmount: ptr(50_000.0), RothConversionEnd: ptrInt(-5)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Apply(windowScenario(0, 0, 40_000), tt.o); err == nil {
				t.Fatal("Apply accepted a negative projection year")
			}
		})
	}
}

// TestApply_AcceptsValidSparseWindowEdits guards against the merged check
// being too eager: a sparse edit that leaves a valid window must still apply,
// including the end year 0 that means "indefinite".
func TestApply_AcceptsValidSparseWindowEdits(t *testing.T) {
	tests := []struct {
		name               string
		savedStart         int
		savedEnd           int
		o                  Overrides
		wantStart, wantEnd int
	}{
		{
			name:       "end year alone, after the saved start",
			savedStart: 2,
			savedEnd:   5,
			o:          Overrides{RothConversionAmount: ptr(50_000.0), RothConversionEnd: ptrInt(8)},
			wantStart:  2,
			wantEnd:    8,
		},
		{
			name:       "end year 0 keeps meaning indefinite",
			savedStart: 10,
			savedEnd:   20,
			o:          Overrides{RothConversionAmount: ptr(50_000.0), RothConversionEnd: ptrInt(0)},
			wantStart:  10,
			wantEnd:    0,
		},
		{
			name:       "start year alone, before the saved end",
			savedStart: 5,
			savedEnd:   12,
			o:          Overrides{RothConversionAmount: ptr(50_000.0), RothConversionStart: ptrInt(3)},
			wantStart:  3,
			wantEnd:    12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Apply(windowScenario(tt.savedStart, tt.savedEnd, 40_000), tt.o)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if got.RothConversion.StartYear != tt.wantStart || got.RothConversion.EndYear != tt.wantEnd {
				t.Errorf("merged window = year %d to year %d, want year %d to year %d",
					got.RothConversion.StartYear, got.RothConversion.EndYear, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// TestApply_LeavesAnAlreadyInvertedStoredWindowPreviewable is the deliberate
// limit of the merged check. A scenario whose stored window is already
// inverted keeps previewing — only a request that touches the window has to
// leave it valid — so an existing plan cannot be made unopenable by a
// validation rule added after it was saved.
func TestApply_LeavesAnAlreadyInvertedStoredWindowPreviewable(t *testing.T) {
	base := windowScenario(10, 5, 40_000)

	got, err := Apply(base, Overrides{MonthlyLivingExpenses: ptr(7_000.0)})
	if err != nil {
		t.Fatalf("Apply rejected an override that does not touch the conversion window: %v", err)
	}
	if got.MonthlyLivingExpenses != 7_000 {
		t.Errorf("MonthlyLivingExpenses=%v, want 7000", got.MonthlyLivingExpenses)
	}
}
