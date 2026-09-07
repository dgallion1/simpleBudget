package templates

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/web"
)

// LT1: shared/period-context collapses into one always-visible summary <p>
// (plus a visible stale-data <p> when applicable) and a closed <details>
// holding the prior-period, history-reason and evidence-bounds sentences.
// The old collapsible "Data freshness" wrapper around the stale notice is
// gone; the stale notice itself must stay outside any details.
func TestLT1RenderPeriodContextDisclosure(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	stale := models.PeriodContext{
		SelectedStart:     day("2026-08-01"),
		SelectedEnd:       day("2026-08-31"),
		SelectedDays:      31,
		PreviousStart:     day("2026-07-01"),
		PreviousEnd:       day("2026-07-31"),
		PreviousDays:      31,
		ComparisonKind:    "calendar_month",
		ComparisonClamped: false,
		HasData:           true,
		LatestTransaction: day("2026-08-20"),
		DataAgeDays:       11,
		Stale:             true,
		HistoryAvailable:  false,
		HistoryReason:     "Not enough history to compare",
	}

	fresh := models.PeriodContext{
		SelectedStart:     day("2026-08-01"),
		SelectedEnd:       day("2026-08-31"),
		SelectedDays:      31,
		PreviousStart:     day("2025-08-01"),
		PreviousEnd:       day("2025-08-31"),
		PreviousDays:      31,
		ComparisonKind:    "year",
		ComparisonClamped: false,
		HasData:           true,
		LatestTransaction: day("2026-08-31"),
		DataAgeDays:       0,
		Stale:             false,
		HistoryAvailable:  true,
		HistoryReason:     "",
	}

	for _, tc := range []struct {
		name       string
		p          models.PeriodContext
		wantStale  bool
		wantReason string
	}{
		{"stale", stale, true, "Not enough history to compare"},
		{"fresh", fresh, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderer.RenderToString("shared/period-context", map[string]any{"Period": tc.p})
			if err != nil {
				t.Fatal(err)
			}

			if n := strings.Count(out, "<details"); n != 1 {
				t.Errorf("want exactly one <details, got %d: %s", n, out)
			}
			if strings.Contains(out, "<details open") {
				t.Error("details must be closed by default")
			}
			if !strings.Contains(out, `<summary class="text-sm text-accent cursor-pointer">Period details</summary>`) {
				t.Error("missing 'Period details' summary")
			}
			if strings.Contains(out, "Data freshness") {
				t.Error("'Data freshness' wrapper must be gone")
			}

			detailsAt := strings.Index(out, "<details")
			if detailsAt < 0 {
				t.Fatal("no <details found")
			}
			before, after := out[:detailsAt], out[detailsAt:]

			selectedAt := strings.Index(before, "Selected period:")
			latestAt := strings.Index(before, "Latest transaction:")
			if selectedAt < 0 || latestAt < 0 {
				t.Errorf("'Selected period:' and 'Latest transaction:' must both occur before <details>: %s", before)
			}

			staleSentence := "Data is more than seven calendar days old"
			if tc.wantStale {
				if !strings.Contains(before, staleSentence) {
					t.Errorf("stale sentence must occur before <details>: %s", before)
				}
			} else if strings.Contains(out, staleSentence) {
				t.Error("stale sentence must not render when not stale")
			}

			for _, want := range []string{"Prior period:", "evidence bounds"} {
				if !strings.Contains(after, want) {
					t.Errorf("%q must occur after <details>: %s", want, after)
				}
				if strings.Contains(before, want) {
					t.Errorf("%q must not occur before <details>: %s", want, before)
				}
			}

			if tc.wantReason != "" {
				if !strings.Contains(after, tc.wantReason) {
					t.Errorf("history reason %q must occur after <details>: %s", tc.wantReason, after)
				}
				if strings.Contains(before, tc.wantReason) {
					t.Errorf("history reason %q must not occur before <details>: %s", tc.wantReason, before)
				}
			} else if strings.Contains(out, "Not enough history to compare") {
				t.Error("history-available fixture must not render the no-history reason")
			}
		})
	}
}
