package models

import "testing"

func TestCalendarMonth(t *testing.T) {
	for _, tc := range []struct {
		name      string
		startDate string
		offset    int
		want      string
		wantLabel string
	}{
		{"offset 11 from 2026-10", "2026-10", 11, "2027-09", "Sep 2027"},
		{"offset 0 is the start month", "2026-10", 0, "2026-10", "Oct 2026"},
		{"year aligned", "2026-10", 12, "2027-10", "Oct 2027"},
		{"negative offset is a past month", "2026-10", -1, "2026-09", "Sep 2026"},
		{"negative offset crossing a year", "2026-10", -16, "2025-06", "Jun 2025"},
		{"unparseable start date", "not-a-month", 11, "", ""},
		{"empty start date", "", 0, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CalendarMonth(tc.startDate, tc.offset); got != tc.want {
				t.Errorf("CalendarMonth(%q, %d) = %q, want %q", tc.startDate, tc.offset, got, tc.want)
			}
			if got := CalendarMonthLabel(tc.startDate, tc.offset); got != tc.wantLabel {
				t.Errorf("CalendarMonthLabel(%q, %d) = %q, want %q", tc.startDate, tc.offset, got, tc.wantLabel)
			}
		})
	}
}
