package history

import "testing"

func TestDefaultDataExists(t *testing.T) {
	data := DefaultData()

	if len(data) < 90 {
		t.Errorf("Expected at least 90 years of data, got %d", len(data))
	}

	// Check first year
	if data[0].Year != 1928 {
		t.Errorf("Expected first year 1928, got %d", data[0].Year)
	}

	// Check data is sequential
	for i := 1; i < len(data); i++ {
		if data[i].Year != data[i-1].Year+1 {
			t.Errorf("Data not sequential at index %d: %d followed by %d", i-1, data[i-1].Year, data[i].Year)
		}
	}
}

func TestSequence(t *testing.T) {
	data := DefaultData()

	// Valid sequence
	seq := Sequence(data, 1950, 30)
	if seq == nil {
		t.Fatal("Expected valid sequence for 1950")
	}
	if len(seq) != 30 {
		t.Errorf("Expected 30 years, got %d", len(seq))
	}
	if seq[0].Year != 1950 {
		t.Errorf("Expected first year 1950, got %d", seq[0].Year)
	}

	// Invalid start year (before data)
	seq = Sequence(data, 1900, 30)
	if seq != nil {
		t.Error("Expected nil for year before data")
	}

	// Not enough data remaining
	seq = Sequence(data, 2020, 30)
	if seq != nil {
		t.Error("Expected nil when not enough data remaining")
	}
}

func TestAvailableStartYears(t *testing.T) {
	data := DefaultData()
	years := AvailableStartYears(data, 30)

	if len(years) == 0 {
		t.Fatal("Expected some available years")
	}

	// First available year should be 1928
	if years[0] != 1928 {
		t.Errorf("First year should be 1928, got %d", years[0])
	}

	// Should not include years that don't have 30 years of remaining data
	lastYear := years[len(years)-1]
	if lastYear > data[len(data)-1].Year-29 {
		t.Errorf("Last available year %d is too recent", lastYear)
	}
}

func TestStats(t *testing.T) {
	avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev := Stats(DefaultData())

	// Stock average should be positive (historically ~10%)
	if avgStock < 5 || avgStock > 15 {
		t.Errorf("Stock average %f outside reasonable range [5, 15]", avgStock)
	}

	// Bond average should be positive (historically ~5%)
	if avgBond < 1 || avgBond > 10 {
		t.Errorf("Bond average %f outside reasonable range [1, 10]", avgBond)
	}

	// Cash average should be positive (historically ~3%)
	if avgCash < 1 || avgCash > 6 {
		t.Errorf("Cash average %f outside reasonable range [1, 6]", avgCash)
	}

	// Inflation average should be positive (historically ~3%)
	if avgInflation < 1 || avgInflation > 6 {
		t.Errorf("Inflation average %f outside reasonable range [1, 6]", avgInflation)
	}

	// Stock standard deviation should be meaningful
	if stockStdDev < 10 || stockStdDev > 25 {
		t.Errorf("Stock std dev %f outside reasonable range [10, 25]", stockStdDev)
	}

	// Bond standard deviation should be meaningful
	if bondStdDev < 5 || bondStdDev > 15 {
		t.Errorf("Bond std dev %f outside reasonable range [5, 15]", bondStdDev)
	}
}

// F-057: AvailableStartYears for a 30-year horizon over 1928-2024
// (97 years of data) must produce 97 - 30 + 1 = 68 starting years
// (1928 through 1995 inclusive). Pre-fix produced 67 (excluded 1995).
func TestAvailableStartYears_F057_OffByOne(t *testing.T) {
	years := AvailableStartYears(DefaultData(), 30)
	if len(years) != 68 {
		t.Errorf("30-year horizon: got %d start years, want 68", len(years))
	}
	if len(years) > 0 && years[0] != 1928 {
		t.Errorf("first start year = %d; want 1928", years[0])
	}
	if len(years) > 0 && years[len(years)-1] != 1995 {
		t.Errorf("last start year = %d; want 1995", years[len(years)-1])
	}
}

func TestAvailableStartYears_F057_FullHistoryHorizon(t *testing.T) {
	// 97-year horizon over 97 years of data: exactly 1 start year.
	years := AvailableStartYears(DefaultData(), 97)
	if len(years) != 1 {
		t.Errorf("97-year horizon: got %d start years, want 1", len(years))
	}
	if len(years) > 0 && years[0] != 1928 {
		t.Errorf("only start year = %d; want 1928", years[0])
	}
}

func TestAvailableStartYears_F057_LongerThanHistory(t *testing.T) {
	// Horizon longer than data: zero start years, no panic.
	years := AvailableStartYears(DefaultData(), 98)
	if len(years) != 0 {
		t.Errorf("98-year horizon: got %d start years, want 0", len(years))
	}
}
