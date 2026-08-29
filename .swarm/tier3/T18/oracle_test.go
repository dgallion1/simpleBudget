package plan

// T18 oracle probes — owned by .swarm/tier3/T18, copied in by accept.sh.
// Compiles only once models.OneTimeExpense and WhatIfSettings.OneTimeExpenses
// exist, so a featureless tree fails the oracle at compile time.

import (
	"testing"

	"budget2/internal/models"
)

func t18base(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := baseSettings()
	s.InflationRate = 0
	s.ProjectionYears = 10
	return s
}

func t18run(t *testing.T, s *models.WhatIfSettings) AnalysisView {
	t.Helper()
	v, err := RunWithOverrides(s, Overrides{})
	if err != nil {
		t.Fatalf("RunWithOverrides: %v", err)
	}
	return v
}

func TestT18Oracle_ExpenseHitsItsYearOnly(t *testing.T) {
	without := t18run(t, t18base(t))
	s := t18base(t)
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 3, Amount: 50_000}}
	with := t18run(t, s)

	for y := range without.Years {
		delta := with.Years[y].Expenses - without.Years[y].Expenses
		if y == 3 {
			if delta < 49_950 || delta > 50_050 {
				t.Fatalf("year 3 expense delta = %v, want ~50000 (inflation 0)", delta)
			}
		} else if delta > 1 || delta < -1 {
			t.Fatalf("year %d expense delta = %v, want 0", y, delta)
		}
	}
	if with.Headline.FinalBalance > without.Headline.FinalBalance-40_000 {
		t.Fatalf("final balance %v not reduced by expense (baseline %v)",
			with.Headline.FinalBalance, without.Headline.FinalBalance)
	}
}

func TestT18Oracle_YearZeroUninflated(t *testing.T) {
	without := t18run(t, t18base(t))
	s := t18base(t)
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "car", Year: 0, Amount: 50_000}}
	with := t18run(t, s)
	delta := with.Years[0].Expenses - without.Years[0].Expenses
	if delta < 49_950 || delta > 50_050 {
		t.Fatalf("year 0 delta = %v, want ~50000", delta)
	}
}

func TestT18Oracle_SameYearExpensesSum(t *testing.T) {
	one := t18base(t)
	one.OneTimeExpenses = []models.OneTimeExpense{{Description: "a", Year: 3, Amount: 50_000}}
	two := t18base(t)
	two.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "b", Year: 3, Amount: 20_000},
		{Description: "c", Year: 3, Amount: 30_000},
	}
	vOne, vTwo := t18run(t, one), t18run(t, two)
	d := vOne.Years[3].Expenses - vTwo.Years[3].Expenses
	if d > 1 || d < -1 {
		t.Fatalf("split expenses differ from single: %v", d)
	}
}

func TestT18Oracle_TodayDollarsInflateWithCPI(t *testing.T) {
	base := t18base(t)
	base.InflationRate = 3
	without := t18run(t, base)
	s := t18base(t)
	s.InflationRate = 3
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 3, Amount: 50_000}}
	with := t18run(t, s)
	delta := with.Years[3].Expenses - without.Years[3].Expenses
	if delta < 50_000*1.02 {
		t.Fatalf("year 3 delta = %v under 3%% CPI; amount must be today's dollars inflated to its year (> 51000)", delta)
	}
	if delta > 50_000*1.20 {
		t.Fatalf("year 3 delta = %v implausibly large", delta)
	}
}

func TestT18Oracle_ValidationRejectsMalformedOnly(t *testing.T) {
	// Malformed entries (impossible values) are still hard errors.
	cases := []models.OneTimeExpense{
		{Description: "neg", Year: 3, Amount: -1},
		{Description: "early", Year: -1, Amount: 1000},
	}
	for _, c := range cases {
		s := t18base(t)
		s.OneTimeExpenses = []models.OneTimeExpense{c}
		if _, err := RunWithOverrides(s, Overrides{}); err == nil {
			t.Fatalf("expected validation error for %+v, got nil", c)
		}
	}
	s := t18base(t)
	s.OneTimeExpenses = []models.OneTimeExpense{}
	if _, err := RunWithOverrides(s, Overrides{}); err != nil {
		t.Fatalf("empty list must be valid: %v", err)
	}
}

// Spec change after attempt 2 (lead decision): an entry at or beyond the
// projection horizon is DORMANT, never fatal. It must not error anywhere in
// the projection path — shrinking the horizon under an existing entry is a
// legitimate user action and must not brick the page — and it must
// contribute nothing while dormant.
func TestT18Oracle_OutOfHorizonEntryIsDormantNotFatal(t *testing.T) {
	baseline := t18run(t, t18base(t))

	s := t18base(t)
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "late", Year: 10, Amount: 1000}} // == ProjectionYears
	v := t18run(t, s) // must not error
	if v.Headline.FinalBalance != baseline.Headline.FinalBalance {
		t.Fatalf("dormant out-of-horizon entry changed the projection: %v vs %v",
			v.Headline.FinalBalance, baseline.Headline.FinalBalance)
	}

	// Horizon-shrink: an entry that was valid under a 10-year horizon sits at
	// year 8 while the horizon drops to 5. Run must succeed and match the
	// 5-year no-expense baseline.
	short := t18base(t)
	short.ProjectionYears = 5
	shortBaseline := t18run(t, short)

	shrunk := t18base(t)
	shrunk.ProjectionYears = 5
	shrunk.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 8, Amount: 50_000}}
	sv := t18run(t, shrunk) // must not error
	if sv.Headline.FinalBalance != shortBaseline.Headline.FinalBalance {
		t.Fatalf("dormant entry (year 8, horizon 5) changed the projection: %v vs %v",
			sv.Headline.FinalBalance, shortBaseline.Headline.FinalBalance)
	}
}
