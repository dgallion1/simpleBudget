package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// A saved boost must survive settings JSON without appearing on legacy plans.
func TestLivingSpendingBoostJSON(t *testing.T) {
	var s WhatIfSettings
	if err := json.Unmarshal([]byte(`{"living_spending_boost":{"monthly_real":1000.01,"stop_month":"2036-09"}}`), &s); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"living_spending_boost":{"monthly_real":1000.01,"stop_month":"2036-09"}`) {
		t.Fatalf("boost lost in JSON round trip: %s", raw)
	}
	raw, err = json.Marshal(WhatIfSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "living_spending_boost") {
		t.Fatal("disabled boost must be omitted")
	}
}

func TestLivingSpendingBoostClone(t *testing.T) {
	if CloneLivingSpendingBoost(nil) != nil {
		t.Fatal("disabled clone changed")
	}
	original := &LivingSpendingBoost{MonthlyReal: 1000.01, StopMonth: "2036-09"}
	clone := CloneLivingSpendingBoost(original)
	if clone == original || *clone != *original {
		t.Fatal("clone must preserve values independently")
	}
	clone.MonthlyReal = 500
	clone.StopMonth = "2030-01"
	if original.MonthlyReal != 1000.01 || original.StopMonth != "2036-09" {
		t.Fatal("clone mutation leaked")
	}
}
