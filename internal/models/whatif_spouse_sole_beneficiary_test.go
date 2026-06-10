package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsSpouseSoleBeneficiary_NilDefaultsTrue(t *testing.T) {
	s := &WhatIfSettings{}
	if !s.IsSpouseSoleBeneficiary() {
		t.Error("nil SpouseSoleBeneficiary should default to true")
	}
}

func TestIsSpouseSoleBeneficiary_ExplicitFalse(t *testing.T) {
	f := false
	s := &WhatIfSettings{SpouseSoleBeneficiary: &f}
	if s.IsSpouseSoleBeneficiary() {
		t.Error("explicit false should return false")
	}
}

func TestIsSpouseSoleBeneficiary_ExplicitTrue(t *testing.T) {
	tr := true
	s := &WhatIfSettings{SpouseSoleBeneficiary: &tr}
	if !s.IsSpouseSoleBeneficiary() {
		t.Error("explicit true should return true")
	}
}

func TestSpouseSoleBeneficiary_JSONAbsentDefaultsTrue(t *testing.T) {
	var s WhatIfSettings
	if err := json.Unmarshal([]byte(`{}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.SpouseSoleBeneficiary != nil {
		t.Error("absent key should leave SpouseSoleBeneficiary nil")
	}
	if !s.IsSpouseSoleBeneficiary() {
		t.Error("absent key should default IsSpouseSoleBeneficiary to true")
	}
}

func TestSpouseSoleBeneficiary_JSONExplicitFalse(t *testing.T) {
	var s WhatIfSettings
	if err := json.Unmarshal([]byte(`{"spouse_sole_beneficiary": false}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.IsSpouseSoleBeneficiary() {
		t.Error("explicit JSON false should return false")
	}
}

func TestSpouseSoleBeneficiary_RoundTripFalse(t *testing.T) {
	f := false
	s := &WhatIfSettings{SpouseSoleBeneficiary: &f}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"spouse_sole_beneficiary":false`) {
		t.Errorf("expected field in JSON, got: %s", string(data))
	}
	var s2 WhatIfSettings
	if err := json.Unmarshal(data, &s2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s2.IsSpouseSoleBeneficiary() {
		t.Error("round-trip false should still return false")
	}
}

func TestSpouseSoleBeneficiary_RoundTripTrue(t *testing.T) {
	tr := true
	s := &WhatIfSettings{SpouseSoleBeneficiary: &tr}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"spouse_sole_beneficiary":true`) {
		t.Errorf("expected field in JSON, got: %s", string(data))
	}
	var s2 WhatIfSettings
	if err := json.Unmarshal(data, &s2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s2.IsSpouseSoleBeneficiary() {
		t.Error("round-trip true should still return true")
	}
}

func TestSpouseSoleBeneficiary_NilOmittedFromJSON(t *testing.T) {
	s := &WhatIfSettings{}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "spouse_sole_beneficiary") {
		t.Errorf("nil SpouseSoleBeneficiary should be omitted from JSON, got: %s", string(data))
	}
}
