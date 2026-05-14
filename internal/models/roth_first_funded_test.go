package models

import (
	"encoding/json"
	"testing"
)

func TestWhatIfSettings_RothFirstFundedYear_JSONRoundtrip(t *testing.T) {
	original := WhatIfSettings{RothFirstFundedYear: 2026}
	raw, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded WhatIfSettings
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.RothFirstFundedYear != 2026 {
		t.Fatalf("got %d, want 2026", decoded.RothFirstFundedYear)
	}
}

func TestWhatIfSettings_RothFirstFundedYear_DefaultZero(t *testing.T) {
	s := WhatIfSettings{}
	if s.RothFirstFundedYear != 0 {
		t.Fatalf("zero-value default: got %d, want 0", s.RothFirstFundedYear)
	}
}

func TestWhatIfSettings_RothFirstFundedYear_OmitemptyWhenZero(t *testing.T) {
	s := WhatIfSettings{}
	raw, err := json.Marshal(&s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(raw); contains(got, "roth_first_funded_year") {
		t.Fatalf("expected omitempty to drop zero value, got: %s", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
