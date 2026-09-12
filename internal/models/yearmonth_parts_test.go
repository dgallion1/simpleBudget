package models

import (
	"testing"
	"time"
)

// ParseYearMonthParts is the allocation-free fast path that replaced
// time.Parse("2006-01", …) inside the projection loop (it was 13% of engine
// CPU: healthcare cost lookups and ParseStartYear re-parsed constant strings
// every month). It must accept and reject exactly what time.Parse did.
func TestParseYearMonthParts_MatchesTimeParse(t *testing.T) {
	inputs := []string{
		"2026-09", "2024-01", "1999-12", "0000-01", "9999-12", "2026-06",
		"", " ", "2026", "2026-", "2026-9", "2026-1", "202-01", "20260-01",
		"2026-00", "2026-13", "2026-99", "2026/09", "2026 09", "2026-09 ",
		" 2026-09", "2026-09-01", "2026-09x", "x2026-09", "abcd-ef", "-026-09",
		"+026-09", "2026-0a", "２０２６-09", "2026-０9", "20\x0026-09", "2026-09\n",
	}
	for _, in := range inputs {
		ref, refErr := time.Parse("2006-01", in)
		year, month, ok := ParseYearMonthParts(in)
		if ok != (refErr == nil) {
			t.Errorf("%q: ok=%v but time.Parse err=%v", in, ok, refErr)
			continue
		}
		if !ok {
			continue
		}
		if year != ref.Year() || time.Month(month) != ref.Month() {
			t.Errorf("%q: got %d-%02d, time.Parse got %d-%02d", in, year, month, ref.Year(), ref.Month())
		}
	}
}

func TestParseYearMonthParts_NoAllocations(t *testing.T) {
	var sink int
	if allocs := testing.AllocsPerRun(100, func() { sink, _, _ = ParseYearMonthParts("2026-09") }); allocs != 0 || sink != 2026 {
		t.Fatalf("ParseYearMonthParts allocates %.1f/op; want 0", allocs)
	}
}
