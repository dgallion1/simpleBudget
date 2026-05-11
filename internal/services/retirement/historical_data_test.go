package retirement

import "testing"

func TestGetHistoricalReturns(t *testing.T) {
	got := GetHistoricalReturns()
	if len(got) == 0 {
		t.Fatal("GetHistoricalReturns returned empty slice")
	}
	// Sanity: every entry should have a non-zero year.
	for i, y := range got {
		if y.Year == 0 {
			t.Errorf("entry %d has zero year", i)
			break
		}
	}
}

func TestGetHistoricalSequence(t *testing.T) {
	all := GetHistoricalReturns()
	if len(all) == 0 {
		t.Skip("no historical data to base sequence on")
	}
	first := all[0].Year

	t.Run("returns requested length", func(t *testing.T) {
		seq := GetHistoricalSequence(first, 5)
		if len(seq) == 0 {
			t.Fatal("expected non-empty sequence")
		}
		if len(seq) > 5 {
			t.Errorf("got %d entries, want at most 5", len(seq))
		}
	})

	t.Run("start year before data wraps or empties", func(t *testing.T) {
		// Just exercise the wrapper; behaviour is up to history.Sequence
		_ = GetHistoricalSequence(first-10, 3)
	})
}
