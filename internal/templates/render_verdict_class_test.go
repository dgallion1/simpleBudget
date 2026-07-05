package templates

import (
	"testing"

	"budget2/internal/models"
)

// TestVerdictBandClass covers all four models.Health constants plus the
// fail-loud default: an unknown/zero-value health must render as red (not
// neutral gray) so a missing or typo'd health value is noticed.
func TestVerdictBandClass(t *testing.T) {
	tests := []struct {
		name string
		h    models.Health
		want string
	}{
		{"green", models.HealthGreen, "bg-emerald-50 dark:bg-emerald-900/20 border-emerald-300 dark:border-emerald-700"},
		{"amber", models.HealthAmber, "bg-amber-50 dark:bg-amber-900/20 border-amber-300 dark:border-amber-700"},
		{"red", models.HealthRed, "bg-rose-50 dark:bg-rose-900/20 border-rose-300 dark:border-rose-700"},
		{"neutral", models.HealthNeutral, "bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700"},
		{"zero value fails loud as red", models.Health(""), "bg-rose-50 dark:bg-rose-900/20 border-rose-300 dark:border-rose-700"},
		{"unknown value fails loud as red", models.Health("chartreuse"), "bg-rose-50 dark:bg-rose-900/20 border-rose-300 dark:border-rose-700"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verdictBandClass(tt.h); got != tt.want {
				t.Errorf("verdictBandClass(%q) = %q, want %q", tt.h, got, tt.want)
			}
		})
	}
}

func TestVerdictValueClass(t *testing.T) {
	tests := []struct {
		name string
		h    models.Health
		want string
	}{
		{"green", models.HealthGreen, "text-emerald-600 dark:text-emerald-400"},
		{"amber", models.HealthAmber, "text-amber-600 dark:text-amber-400"},
		{"red", models.HealthRed, "text-rose-600 dark:text-rose-400"},
		{"neutral", models.HealthNeutral, "text-gray-700 dark:text-gray-200"},
		{"zero value fails loud as red", models.Health(""), "text-rose-600 dark:text-rose-400"},
		{"unknown value fails loud as red", models.Health("chartreuse"), "text-rose-600 dark:text-rose-400"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verdictValueClass(tt.h); got != tt.want {
				t.Errorf("verdictValueClass(%q) = %q, want %q", tt.h, got, tt.want)
			}
		})
	}
}

func TestVerdictLabelClass(t *testing.T) {
	tests := []struct {
		name string
		h    models.Health
		want string
	}{
		{"green", models.HealthGreen, "text-emerald-700 dark:text-emerald-300"},
		{"amber", models.HealthAmber, "text-amber-700 dark:text-amber-300"},
		{"red", models.HealthRed, "text-rose-700 dark:text-rose-300"},
		{"neutral", models.HealthNeutral, "text-gray-500 dark:text-gray-400"},
		{"zero value fails loud as red", models.Health(""), "text-rose-700 dark:text-rose-300"},
		{"unknown value fails loud as red", models.Health("chartreuse"), "text-rose-700 dark:text-rose-300"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verdictLabelClass(tt.h); got != tt.want {
				t.Errorf("verdictLabelClass(%q) = %q, want %q", tt.h, got, tt.want)
			}
		})
	}
}
