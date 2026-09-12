package plan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/retirement"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkerFailingLifetimeManager(t *testing.T) *retirement.SettingsManager {
	t.Helper()
	sm := newTestManager(t)
	s, err := sm.Load()
	if err != nil {
		t.Fatal(err)
	}
	prior := 106000.0
	s.StartDate, s.ProjectionYears, s.CurrentAge = "2026-01", 1, 75
	s.MonthlyLivingExpenses, s.InflationRate = 0, 0
	s.Persons = []models.Person{{ID: "older", Name: "Older", Role: models.PersonRolePrimary, BirthMonth: "1951-01"}}
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.RMDTiming = models.RMDTimingStartOfYear
	s.Lifetime = &models.LifetimeSettings{Version: 1, Accounts: []models.LifetimeAccount{
		{ID: "cash", OwnerID: "older", LegalType: "cash", TaxTreatment: "taxable", OpeningValue: 200, CashPercent: 100},
		{ID: "broker", OwnerID: "older", LegalType: "brokerage", TaxTreatment: "taxable", StockPercent: 100},
		{ID: "ira", OwnerID: "older", LegalType: "ira", TaxTreatment: "traditional", OpeningValue: 106000, PriorDecemberValue: nil, StockPercent: 100},
	}, CashPolicy: models.LifetimeCashPolicy{ReserveAccountID: "cash", SurplusAccountID: "broker", WithdrawalOrder: []string{"cash", "broker", "ira"}}}
	_ = prior // deliberately missing history is the deterministic CalculationError trigger
	if err := sm.Save(s); err != nil {
		t.Fatal(err)
	}
	return sm
}

func checkerCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCheckerPublicToolsCalculationErrorPrecedence(t *testing.T) {
	sm := checkerFailingLifetimeManager(t)
	cs := connect(t, Deps{Settings: sm, Snapshots: snapshot.New(sm.SettingsDir(), t.TempDir())})

	analysis := checkerCall(t, cs, "get_analysis", map[string]any{})
	if analysis.IsError {
		t.Fatalf("get_analysis transport/tool error: %v", analysis.Content)
	}
	var ao analysisOutput
	b, _ := json.Marshal(analysis.StructuredContent)
	if err := json.Unmarshal(b, &ao); err != nil {
		t.Fatal(err)
	}
	if ao.Analysis.CalculationError == "" {
		t.Fatalf("get_analysis omitted CalculationError: %+v", ao.Analysis)
	}
	if ao.Analysis.Headline.FinalBalance != 0 || ao.Analysis.Headline.Survives {
		t.Fatalf("get_analysis leaked success headline: %+v", ao.Analysis)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"get_months", map[string]any{"from_month": 0, "to_month": 0}},
		{"run_scenario", map[string]any{"overrides": map[string]any{"projection_years": 1}}},
	} {
		res := checkerCall(t, cs, tc.name, tc.args)
		if !res.IsError || !strings.Contains(strings.ToLower(res.Content[0].(*mcp.TextContent).Text), "calculation") {
			t.Errorf("%s should return explicit calculation error: IsError=%v content=%v", tc.name, res.IsError, res.Content)
		}
	}

	apply := checkerCall(t, cs, "apply_changes", map[string]any{"overrides": map[string]any{"projection_years": 1}})
	if !apply.IsError {
		t.Fatalf("apply_changes returned successful payload after saved calculation failed: %+v", apply.StructuredContent)
	}
	if len(apply.Content) == 0 || !strings.Contains(strings.ToLower(apply.Content[0].(*mcp.TextContent).Text), "saved") {
		t.Fatalf("apply_changes error must state settings were saved: %v", apply.Content)
	}
}
