package http

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"budget2/internal/templates"
)

// createTestRenderer creates a minimal Renderer with an in-memory filesystem
func createTestRenderer(t *testing.T) *templates.Renderer {
	t.Helper()
	memFS := fstest.MapFS{
		"layouts/base.html":    {Data: []byte(`{{define "base"}}{{template "content" .}}{{end}}`)},
		"pages/test.html":      {Data: []byte(`{{define "content"}}hello{{end}}`)},
		"partials/part.html":   {Data: []byte(`{{define "part"}}partial content{{end}}`)},
		"components/empty.html": {Data: []byte(`{{define "empty"}}{{end}}`)},
	}
	r, err := templates.NewFromFS(memFS, false)
	if err != nil {
		t.Fatalf("failed to create test renderer: %v", err)
	}
	return r
}

func TestRenderTemplate_WithRenderer(t *testing.T) {
	r := createTestRenderer(t)
	w := httptest.NewRecorder()
	data := map[string]interface{}{}

	RenderTemplate(w, r, "base", data)

	body := w.Body.String()
	if !strings.Contains(body, "hello") {
		t.Errorf("expected rendered content, got %q", body)
	}
}

func TestRenderTemplate_NilRenderer(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]interface{}{"key": "value"}

	RenderTemplate(w, nil, "test-page", data)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "text/html" {
		t.Errorf("expected Content-Type text/html, got %s", resp.Header.Get("Content-Type"))
	}
	body := w.Body.String()
	if !strings.Contains(body, "test-page") {
		t.Errorf("expected body to contain template name, got %s", body)
	}
	if !strings.Contains(body, "Templates not loaded") {
		t.Errorf("expected fallback message, got %s", body)
	}
}

func TestRenderPartial_WithRenderer(t *testing.T) {
	r := createTestRenderer(t)
	w := httptest.NewRecorder()
	data := map[string]interface{}{}

	RenderPartial(w, r, "part", data)

	body := w.Body.String()
	if !strings.Contains(body, "partial content") {
		t.Errorf("expected rendered partial, got %q", body)
	}
}

func TestRenderPartial_NilRenderer(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]interface{}{"key": "value"}

	RenderPartial(w, nil, "my-partial", data)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "text/html" {
		t.Errorf("expected Content-Type text/html, got %s", resp.Header.Get("Content-Type"))
	}
	body := w.Body.String()
	if !strings.Contains(body, "my-partial") {
		t.Errorf("expected body to contain partial name, got %s", body)
	}
}

func TestErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()

	ErrorResponse(w, "something went wrong", http.StatusBadRequest)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
	body := w.Body.String()
	if !strings.Contains(body, "something went wrong") {
		t.Errorf("expected error message in body, got %s", body)
	}
}

func TestParseDateRange_ExplicitDates(t *testing.T) {
	minDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.Local)

	start, end := ParseDateRange("2024-03-01", "2024-06-30", minDate, maxDate)

	expectedStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)

	if !start.Equal(expectedStart) {
		t.Errorf("expected start %v, got %v", expectedStart, start)
	}
	if !end.Equal(expectedEnd) {
		t.Errorf("expected end %v, got %v", expectedEnd, end)
	}
}

func TestParseDateRange_DefaultsYTD(t *testing.T) {
	now := time.Now()
	minDate := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.Local)

	start, end := ParseDateRange("", "", minDate, maxDate)

	expectedStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(expectedStart) {
		t.Errorf("expected YTD start %v, got %v", expectedStart, start)
	}
	if !end.Equal(maxDate) {
		t.Errorf("expected end %v, got %v", maxDate, end)
	}
}

func TestParseDateRange_YTDStartAfterMaxDate(t *testing.T) {
	// Data ends before current year starts -> should default to minDate
	minDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2020, 12, 31, 0, 0, 0, 0, time.Local)

	start, end := ParseDateRange("", "", minDate, maxDate)

	if !start.Equal(minDate) {
		t.Errorf("expected start to fall back to minDate %v, got %v", minDate, start)
	}
	if !end.Equal(maxDate) {
		t.Errorf("expected end %v, got %v", maxDate, end)
	}
}

func TestParseDateRange_YTDStartBeforeMinDate(t *testing.T) {
	// minDate is after Jan 1 of current year but maxDate is still in the future
	now := time.Now()
	minDate := time.Date(now.Year(), 3, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.Local)

	start, _ := ParseDateRange("", "", minDate, maxDate)

	// YTD start (Jan 1) is before minDate (Mar 1), so should clamp to minDate
	if !start.Equal(minDate) {
		t.Errorf("expected start clamped to minDate %v, got %v", minDate, start)
	}
}

func TestParseDateRange_ZeroMaxDate(t *testing.T) {
	// When maxDate is zero: IsZero() is true so the first condition is false.
	// YTD start (Jan 1 current year) is NOT before minDate (2024-01-01)
	// since current year is 2026, so start stays as YTD.
	now := time.Now()
	minDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)
	var maxDate time.Time // zero value

	start, end := ParseDateRange("", "", minDate, maxDate)

	expectedStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(expectedStart) {
		t.Errorf("expected start %v, got %v", expectedStart, start)
	}
	if !end.Equal(maxDate) {
		t.Errorf("expected end to be zero, got %v", end)
	}
}

func TestParseDateRange_InvalidDateStrings(t *testing.T) {
	minDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.Local)

	start, end := ParseDateRange("not-a-date", "also-bad", minDate, maxDate)

	if !start.IsZero() {
		t.Errorf("expected zero start for invalid date, got %v", start)
	}
	if !end.IsZero() {
		t.Errorf("expected zero end for invalid date, got %v", end)
	}
}

// Ensure the fs import is used
var _ fs.FS = fstest.MapFS{}
