package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLifetimeJSONRoundTrip(t *testing.T) {
	s := DefaultWhatIfSettings()
	if err := json.Unmarshal([]byte(`{"lifetime":{"version":1,"accounts":[{"id":"cash","owner_id":"p","legal_type":"cash","tax_treatment":"taxable","opening_value":123.45}]}}`), s); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["lifetime"] == nil {
		t.Fatal("lifetime settings lost in JSON round-trip")
	}
}

func TestLifetimeNilCompatibility(t *testing.T) {
	raw, err := json.Marshal(DefaultWhatIfSettings())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["lifetime"]; ok {
		t.Fatal("legacy settings must omit lifetime")
	}
}

func TestLifetimeYTDJSONRequiresExplicitAmounts(t *testing.T) {
	cases := []struct {
		name     string
		complete string
		fields   []string
		target   func() any
	}{
		{"rule", `{"rule_id":"r","employee_regular":0,"employee_catch_up":0,"employer":0,"matching_paid":0}`, []string{"employee_regular", "employee_catch_up", "employer", "matching_paid"}, func() any { return &ContributionYTD{} }},
		{"job", `{"job_id":"j","gross_wages":0,"eligible_pay":0}`, []string{"gross_wages", "eligible_pay"}, func() any { return &JobYTD{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.complete), tc.target()); err != nil {
				t.Fatal("explicit zeros rejected", err)
			}
			for _, field := range tc.fields {
				var row map[string]json.RawMessage
				if err := json.Unmarshal([]byte(tc.complete), &row); err != nil {
					t.Fatal(err)
				}
				delete(row, field)
				raw, err := json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, tc.target()); err == nil {
					t.Errorf("omitted %s accepted", field)
				}
				row[field] = json.RawMessage("null")
				raw, err = json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, tc.target()); err == nil {
					t.Errorf("null %s accepted", field)
				}
			}
		})
	}
}

func TestAggregateLifetimeYearRejectsTenCentHouseholdFabrication(t *testing.T) {
	y := AggregateLifetimeYear([]LifetimeMonthOutcome{{Year: 2026, Month: 1, Accounts: []AccountReconciliation{{AccountID: "large", Opening: 1_000_000, Closing: 999_999.90}}}})
	if y.Available {
		t.Fatalf("accepted $0.10 raw wealth mismatch as rounding adjustment: %+v", y)
	}
}

func TestAggregateLifetimeYearAccountDisplayAdjustment(t *testing.T) {
	y := AggregateLifetimeYear([]LifetimeMonthOutcome{{Year: 2026, Month: 1, ExternalIncome: .004, Accounts: []AccountReconciliation{{AccountID: "cash", AccountName: "Household reserve", Opening: .004, Deposits: .004, Closing: .008}}}})
	if !y.Available || len(y.Accounts) != 1 {
		t.Fatalf("summary unavailable: %+v", y)
	}
	if y.Accounts[0].RoundingAdjustment == 0 {
		t.Fatalf("missing account display adjustment: %+v", y.Accounts[0])
	}
}
func TestLP1SecondMatchTierUsesCanonicalJSONKeys(t *testing.T) {
	raw, err := json.Marshal(MatchTier{FromPercent: 1, ToPercent: 2, MatchPercent: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"FromPercent":1`) ||
		!strings.Contains(string(raw), `"ToPercent":2`) ||
		!strings.Contains(string(raw), `"MatchPercent":50`) {
		t.Fatalf("persisted match tier uses noncanonical keys: %s", raw)
	}
	var got MatchTier
	if err := json.Unmarshal([]byte(`{"FromPercent":1,"ToPercent":2,"MatchPercent":50}`), &got); err != nil {
		t.Fatal(err)
	}
	if got != (MatchTier{FromPercent: 1, ToPercent: 2, MatchPercent: 50}) {
		t.Fatalf("canonical settings input lost match tier: %#v", got)
	}
}
