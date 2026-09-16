package whatif

import (
	"fmt"
	"net/http"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

// The schedule forms speak calendar months (<input type="month">, "YYYY-MM")
// while the plan stores month offsets from its StartDate. This file holds the
// ONE conversion between the two: the offset arithmetic comes from
// retirement.MonthOffset (the exported face of the same monthsBetween the
// rollover shift uses) and every month NAMED back to the user comes from
// models.CalendarMonthLabel, the single label formatter. No other rule for
// either direction exists anywhere.

// monthFieldLabel names a month form field the way its <label> does, so an
// error message points at the control the user can see.
func monthFieldLabel(field string) string {
	switch field {
	case "start_month":
		return "Start month"
	case "end_month":
		return "Through month"
	default:
		return "Month"
	}
}

// parseMonthOffset reads the calendar month in form field `field` and returns
// its offset in months from startDate (the plan start). A month before the
// plan start is rejected: offsets are measured forward from the start, so an
// earlier month has no representation and silently clamping it would move the
// user's date. The rejection message names the plan-start month itself rather
// than an offset, because that is what the form shows.
func parseMonthOffset(r *http.Request, field, startDate string) (int, error) {
	raw := strings.TrimSpace(r.FormValue(field))
	if raw == "" {
		return 0, fmt.Errorf("%s is required — pick a calendar month", monthFieldLabel(field))
	}
	offset, ok := retirement.MonthOffset(startDate, raw)
	if !ok {
		return 0, fmt.Errorf("%s %q is not a calendar month (YYYY-MM)", monthFieldLabel(field), raw)
	}
	if offset < 0 {
		start := models.CalendarMonthLabel(startDate, 0)
		if start == "" {
			return 0, fmt.Errorf("%s cannot be before the plan start", monthFieldLabel(field))
		}
		return 0, fmt.Errorf("%s must be %s or later (the plan starts that month)", monthFieldLabel(field), start)
	}
	return offset, nil
}

// parseMonthRange parses the start_month / end_month pair the income and
// expense forms submit. end_month is labelled "Through" and is the LAST month
// the entry is included in, so it is stored as offset+1 — the model's EndMonth
// is the first month WITHOUT the entry. A blank end_month is perpetual (nil).
func parseMonthRange(r *http.Request, startDate string) (startMonth int, endMonth *int, errMsg string) {
	startMonth, err := parseMonthOffset(r, "start_month", startDate)
	if err != nil {
		return 0, nil, err.Error()
	}
	if strings.TrimSpace(r.FormValue("end_month")) == "" {
		return startMonth, nil, ""
	}
	through, err := parseMonthOffset(r, "end_month", startDate)
	if err != nil {
		return 0, nil, err.Error()
	}
	if through < startMonth {
		return 0, nil, "Through month cannot be before the start month"
	}
	end := through + 1
	return startMonth, &end, ""
}

// planStartDate returns the saved plan's StartDate — the anchor every month
// input on the page was rendered against, and therefore the anchor a
// submitted month must be converted against.
func planStartDate() (string, error) {
	settings, err := retirementMgr.Load()
	if err != nil {
		return "", err
	}
	return settings.StartDate, nil
}
