package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestGuardrailFloorJSONRoundtrip(t *testing.T) {
	settings := WhatIfSettings{Guardrails: &GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500.25}}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WhatIfSettings
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Guardrails.MinMonthlySpendingReal != 7500.25 {
		t.Fatal(string(raw))
	}
	var legacy WhatIfSettings
	if err = json.Unmarshal([]byte(`{"guardrails":{"enabled":true}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Guardrails.MinMonthlySpendingReal != 0 {
		t.Fatal("legacy enabled floor")
	}
	result := MonteCarloResult{FloorOutcome: &MonteCarloFloorOutcome{FloorFailed: true, MonthsObserved: 12, TotalFundedLivingReal: 90000.25}}
	raw, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var restored MonteCarloResult
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.FloorOutcome == nil || !reflect.DeepEqual(restored.FloorOutcome, result.FloorOutcome) {
		t.Fatal(string(raw))
	}
}
