package majorexpenses

import "testing"

func TestBuildTrackingVerdict(t *testing.T) {
	t.Run("no spend is neutral", func(t *testing.T) {
		v := BuildTrackingVerdict(0, 0, 0)
		if v.Health != TrackingNeutral {
			t.Errorf("Health = %q, want neutral", v.Health)
		}
		if v.HasSpend {
			t.Errorf("HasSpend = true, want false")
		}
	})

	t.Run("mostly tracked is green", func(t *testing.T) {
		v := BuildTrackingVerdict(9000, 1000, 3) // 90%
		if v.Health != TrackingGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if !v.HasSpend {
			t.Errorf("HasSpend = false, want true")
		}
		if v.TrackedPercent != 90 {
			t.Errorf("TrackedPercent = %v, want 90", v.TrackedPercent)
		}
		if v.DeclaredTotal != 9000 || v.UnmatchedTotal != 1000 || v.UnmatchedCount != 3 {
			t.Errorf("figures = (%v,%v,%d), want (9000,1000,3)", v.DeclaredTotal, v.UnmatchedTotal, v.UnmatchedCount)
		}
	})

	t.Run("partially tracked is amber", func(t *testing.T) {
		v := BuildTrackingVerdict(6000, 4000, 12) // 60%
		if v.Health != TrackingAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
	})

	t.Run("mostly untracked is red", func(t *testing.T) {
		v := BuildTrackingVerdict(3000, 7000, 25) // 30%
		if v.Health != TrackingRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
	})

	t.Run("exactly at thresholds", func(t *testing.T) {
		if v := BuildTrackingVerdict(80, 20, 1); v.Health != TrackingGreen { // 80%
			t.Errorf("80%% Health = %q, want green", v.Health)
		}
		if v := BuildTrackingVerdict(50, 50, 1); v.Health != TrackingAmber { // 50%
			t.Errorf("50%% Health = %q, want amber", v.Health)
		}
	})
}
