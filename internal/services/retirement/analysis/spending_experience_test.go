package analysis

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"budget2/internal/models"
)

func spendingExperienceOracle(t *testing.T, scale float64) *models.SpendingPathOutcome {
	t.Helper()
	tr := newSpendingExperienceTracker(90, 1)
	type obs struct {
		adjusted, funded, shortfall float64
		depleted                    bool
	}
	months := []obs{
		{100, 100, 0, false}, {100, 100, 0, false}, {80, 80, 0, false},
		{80, 80, 0, false}, {100, 100, 0, false}, {71, 70, 1, true}, {100, 0, 150, true},
	}
	for _, m := range months {
		tr.observe(spendingMonthObservation{
			Planned: 100 * scale, Adjusted: m.adjusted * scale,
			Funded: m.funded * scale, Shortfall: m.shortfall * scale,
			CPI: scale, Depleted: m.depleted,
		})
	}
	return tr.result()
}

func TestSpendingExperienceHandCalculatedMonths(t *testing.T) {
	got := spendingExperienceOracle(t, 1)
	if got == nil {
		t.Fatal("valid monthly oracle was rejected")
	}
	if got.MonthsObserved != 7 || got.NearTermMonths != 7 ||
		got.NearTermFundedLivingReal != 530 || got.MinFundedMonthlyReal != 0 ||
		got.MinFundedMonth != 7 || got.FirstCutMonth != 3 ||
		got.MonthsBelowPlan != 4 || got.LongestBelowPlanMonths != 2 ||
		got.FloorShortfallMonths != 4 || got.LongestFloorShortfallMonths != 2 ||
		got.MaxCutReal != 100 || got.MaxCutPct != 100 || got.LargestFloorGapReal != 90 ||
		got.UnpaidObligationMonths != 1 || got.DepletionMonth != 6 ||
		got.FloorShortfallMonthsAfterDepletion != 2 || !got.BelowPlanAtEnd {
		t.Fatalf("monthly oracle mismatch: %+v", got)
	}

	doubled := spendingExperienceOracle(t, 2)
	if !reflect.DeepEqual(got, doubled) {
		t.Fatalf("real-dollar outcome changed with nominal scale: base=%+v doubled=%+v", got, doubled)
	}
}

func TestSpendingExperienceDepletedButFloorFunded(t *testing.T) {
	tr := newSpendingExperienceTracker(90, 1)
	for month := 1; month <= 12; month++ {
		tr.observe(spendingMonthObservation{Planned: 100, Adjusted: 100, Funded: 100, CPI: 1, Depleted: month >= 3})
	}
	got := tr.result()
	if got == nil || got.DepletionMonth != 3 || got.FloorShortfallMonthsAfterDepletion != 0 ||
		got.FloorShortfallMonths != 0 || got.UnpaidObligationMonths != 0 {
		t.Fatalf("depletion was confused with a funding failure: %+v", got)
	}
}

func TestSpendingExperienceRaisedAdjustedBudget(t *testing.T) {
	tr := newSpendingExperienceTracker(90, 1)
	tr.observe(spendingMonthObservation{Planned: 100, Adjusted: 120, Funded: 110, Shortfall: 10, CPI: 1})
	got := tr.result()
	if got == nil || got.MonthsBelowPlan != 0 || got.FloorShortfallMonths != 0 || got.UnpaidObligationMonths != 0 {
		t.Fatalf("absorbed reduction above plan was misclassified: %+v", got)
	}
}

func TestSpendingExperienceCutAboveFloorOnly(t *testing.T) {
	tr := newSpendingExperienceTracker(90, 1)
	tr.observe(spendingMonthObservation{Planned: 100, Adjusted: 100, Funded: 95, Shortfall: 5, CPI: 1})
	got := tr.result()
	if got == nil || got.MonthsBelowPlan != 1 || got.MaxCutReal != 5 || got.FloorShortfallMonths != 0 || got.UnpaidObligationMonths != 0 {
		t.Fatalf("above-floor living cut classification mismatch: %+v", got)
	}
}

func TestSpendingExperienceScheduledChangesAreNotCuts(t *testing.T) {
	for _, boundary := range []string{"boost expiry", "phase change"} {
		t.Run(boundary, func(t *testing.T) {
			tr := newSpendingExperienceTracker(9000, 1)
			for _, month := range []spendingMonthObservation{
				{Planned: 12000, Adjusted: 12000, Funded: 12000, CPI: 1},
				{Planned: 12000, Adjusted: 12000, Funded: 12000, CPI: 1},
				{Planned: 11000, Adjusted: 11000, Funded: 11000, CPI: 1},
				{Planned: 11000, Adjusted: 10500, Funded: 10500, CPI: 1},
			} {
				tr.observe(month)
			}
			got := tr.result()
			if got == nil || got.FirstCutMonth != 4 || got.MonthsBelowPlan != 1 || got.MaxCutReal != 500 {
				t.Fatalf("scheduled %s classification mismatch: %+v", boundary, got)
			}
		})
	}
}

func TestSpendingExperienceInvalidObservation(t *testing.T) {
	for _, bad := range []spendingMonthObservation{
		{Planned: math.NaN(), Adjusted: 100, Funded: 100, CPI: 1},
		{Planned: 100, Adjusted: 100, Funded: math.Inf(1), CPI: 1},
		{Planned: 100, Adjusted: 100, Funded: 100, CPI: 0},
		{Planned: 100, Adjusted: 100, Funded: -1, CPI: 1},
		{Planned: math.MaxFloat64, Adjusted: 100, Funded: 100, CPI: math.SmallestNonzeroFloat64},
	} {
		tr := newSpendingExperienceTracker(90, 1)
		tr.observe(bad)
		if got := tr.result(); got != nil {
			t.Fatalf("invalid observation qualified: %+v", got)
		}
	}
}

func TestSpendingExperienceMonteCarloObservationParity(t *testing.T) {
	s := floorRegressionSettings()
	s.ProjectionYears = 2
	in := engineInput(t, s)
	base := DefaultMonteCarloConfig()
	base.ReturnVolatility, base.CrashProbability = 0, 0
	base.SpendingShockProb, base.HealthShockProb = 0, 0
	base.LongevityVariation = 0
	base.MinMonthlySpendingReal = 100
	observed := *base
	observed.SpendingExperienceYears = 1

	without := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(281)), base)
	with := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(281)), &observed)
	if with.SpendingOutcome == nil || with.SpendingOutcome.MonthsObserved != 24 || with.SpendingOutcome.NearTermMonths != 12 {
		t.Fatalf("full-horizon spending observation missing: %+v", with.SpendingOutcome)
	}
	without.SpendingOutcome, with.SpendingOutcome = nil, nil
	if !reflect.DeepEqual(without, with) {
		t.Fatalf("observation changed seeded outcome:\nwithout=%+v\nwith=%+v", without, with)
	}
}

func TestSpendingExperienceJSONOmittedUnlessRequested(t *testing.T) {
	raw, err := json.Marshal(models.MonteCarloResult{})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || containsJSONField(raw, "spending_outcome") {
		t.Fatalf("disabled observation was serialized: %s", raw)
	}
	raw, err = json.Marshal(models.MonteCarloResult{SpendingOutcome: &models.SpendingPathOutcome{MonthsObserved: 12}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSONField(raw, "spending_outcome") {
		t.Fatalf("requested observation was omitted: %s", raw)
	}
}

func containsJSONField(raw []byte, field string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, ok := fields[field]
	return ok
}
