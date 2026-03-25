package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWhatIfSettings_ScenarioChainSerialization(t *testing.T) {
	settings := &WhatIfSettings{
		ScenarioName: "Test",
		ScenarioChain: []ScenarioChainLink{
			{ScenarioFilename: "whatif_post-ss.json", TransitionAge: 70},
			{ScenarioFilename: "whatif_late.json", TransitionAge: 80},
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WhatIfSettings
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.ScenarioChain) != 2 {
		t.Fatalf("expected 2 chain links, got %d", len(decoded.ScenarioChain))
	}
	if decoded.ScenarioChain[0].TransitionAge != 70 {
		t.Errorf("expected transition age 70, got %d", decoded.ScenarioChain[0].TransitionAge)
	}
	if decoded.ScenarioChain[1].ScenarioFilename != "whatif_late.json" {
		t.Errorf("expected whatif_late.json, got %s", decoded.ScenarioChain[1].ScenarioFilename)
	}
}

func TestWhatIfSettings_EmptyChainOmitted(t *testing.T) {
	settings := &WhatIfSettings{ScenarioName: "Test"}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), "scenario_chain") {
		t.Error("empty scenario_chain should be omitted from JSON")
	}
}
