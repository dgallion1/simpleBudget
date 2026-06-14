package insights

import (
	"testing"

	"budget2/internal/models"
)

func TestBuildPaceVerdict(t *testing.T) {
	t.Run("nil velocity is neutral with no data", func(t *testing.T) {
		v := BuildPaceVerdict(nil)
		if v.Health != PaceNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasData {
			t.Errorf("HasData = true, want false")
		}
	})

	t.Run("below usual pace is green", func(t *testing.T) {
		v := BuildPaceVerdict(&models.SpendingVelocity{
			DailyAverage: 80, HistoricalDaily: 100, MonthProjection: 2400, BurnRateChange: -20,
		})
		if v.Health != PaceGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if !v.IsBelow || v.IsAbove {
			t.Errorf("IsBelow/IsAbove = (%v,%v), want (true,false)", v.IsBelow, v.IsAbove)
		}
		if v.DailyAverage != 80 || v.MonthProjection != 2400 {
			t.Errorf("figures = (%v,%v), want (80,2400)", v.DailyAverage, v.MonthProjection)
		}
	})

	t.Run("slightly elevated pace is amber", func(t *testing.T) {
		v := BuildPaceVerdict(&models.SpendingVelocity{BurnRateChange: 6})
		if v.Health != PaceAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
		if !v.IsAbove {
			t.Errorf("IsAbove = false, want true")
		}
	})

	t.Run("at-or-on usual pace (zero) is amber, not above or below", func(t *testing.T) {
		v := BuildPaceVerdict(&models.SpendingVelocity{BurnRateChange: 0})
		if v.Health != PaceAmber {
			t.Errorf("Health = %q, want amber (mirrors the Daily Spending tile semantic)", v.Health)
		}
		if v.IsAbove || v.IsBelow {
			t.Errorf("IsAbove/IsBelow = (%v,%v), want both false at exactly 0", v.IsAbove, v.IsBelow)
		}
	})

	t.Run("well above usual pace is red", func(t *testing.T) {
		v := BuildPaceVerdict(&models.SpendingVelocity{BurnRateChange: 25})
		if v.Health != PaceRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
		if !v.IsAbove {
			t.Errorf("IsAbove = false, want true")
		}
	})
}
