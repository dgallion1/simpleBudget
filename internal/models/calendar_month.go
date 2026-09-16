package models

// CalendarMonth and CalendarMonthLabel are the ONE pair of functions that turn
// a schedule offset (months from a plan's StartDate) into a calendar month.
// Every surface that shows or accepts a scheduled date — templates, analysis
// notes, handlers — goes through them, so a date can never disagree with
// itself between two renderers (the dual-formatter defect class).

// CalendarMonth returns the "2006-01" calendar month that is offset months
// after startDate. offset may be negative (a past, dormant entry). An
// unparseable startDate returns "".
func CalendarMonth(startDate string, offset int) string {
	start, err := ParseYearMonth(startDate)
	if err != nil {
		return ""
	}
	return start.AddDate(0, offset, 0).Format(yearMonthLayout)
}

// CalendarMonthLabel returns the human-readable "Jan 2006" label for the
// calendar month offset months after startDate. offset may be negative. An
// unparseable startDate returns "", and callers print nothing in that case.
func CalendarMonthLabel(startDate string, offset int) string {
	start, err := ParseYearMonth(startDate)
	if err != nil {
		return ""
	}
	return start.AddDate(0, offset, 0).Format("Jan 2006")
}
