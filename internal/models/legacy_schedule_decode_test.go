package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// Schedules moved from year offsets to month offsets so the monthly StartDate
// rollover can shift them without rounding. Stored plans keep the legacy year
// keys, so every one of the three affected types must decode three shapes:
// legacy-only (converted ×12), new-only, and both present — where the NEW key
// always wins, because a document that already speaks months must never be
// rounded back to a year boundary.

func TestExpenseSourceLegacyYearDecode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		raw           string
		wantStart     int
		wantEnd       *int
		wantEndIsNil  bool
		wantOtherKept bool
	}{
		{
			name: "legacy only", raw: `{"id":"e","name":"Boat","amount":500,"start_year":1,"end_year":2}`,
			wantStart: 12, wantEnd: intp(24),
		},
		{
			name: "legacy end_year 0 means perpetual", raw: `{"id":"e","name":"Boat","amount":500,"start_year":0,"end_year":0}`,
			wantStart: 0, wantEndIsNil: true,
		},
		{
			name: "new only", raw: `{"id":"e","name":"Boat","amount":500,"start_month":11,"end_month":23}`,
			wantStart: 11, wantEnd: intp(23),
		},
		{
			name: "new only, explicit null end is perpetual", raw: `{"id":"e","name":"Boat","amount":500,"start_month":11,"end_month":null}`,
			wantStart: 11, wantEndIsNil: true,
		},
		{
			name: "mixed keys: new wins", raw: `{"id":"e","name":"Boat","amount":500,"start_year":9,"start_month":5,"end_year":9,"end_month":7}`,
			wantStart: 5, wantEnd: intp(7),
		},
		{
			name: "mixed keys: new zero wins", raw: `{"id":"e","name":"Boat","amount":500,"start_year":9,"start_month":0,"end_year":9,"end_month":0}`,
			wantStart: 0, wantEnd: intp(0),
		},
		{
			name: "neither key", raw: `{"id":"e","name":"Boat","amount":500}`,
			wantStart: 0, wantEndIsNil: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var es ExpenseSource
			if err := json.Unmarshal([]byte(tc.raw), &es); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if es.StartMonth != tc.wantStart {
				t.Errorf("StartMonth = %d, want %d", es.StartMonth, tc.wantStart)
			}
			switch {
			case tc.wantEndIsNil:
				if es.EndMonth != nil {
					t.Errorf("EndMonth = %d, want nil (perpetual)", *es.EndMonth)
				}
			case es.EndMonth == nil:
				t.Errorf("EndMonth = nil, want %d", *tc.wantEnd)
			case *es.EndMonth != *tc.wantEnd:
				t.Errorf("EndMonth = %d, want %d", *es.EndMonth, *tc.wantEnd)
			}
			if es.ID != "e" || es.Name != "Boat" || es.Amount != 500 {
				t.Errorf("non-schedule fields lost: %+v", es)
			}
		})
	}
}

func TestOneTimeExpenseLegacyYearDecode(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"legacy only", `{"id":"o","description":"Roof","year":3,"amount":1}`, 36},
		{"legacy zero", `{"id":"o","description":"Roof","year":0,"amount":1}`, 0},
		{"new only", `{"id":"o","description":"Roof","month":7,"amount":1}`, 7},
		{"new wins over legacy", `{"id":"o","description":"Roof","year":9,"month":5,"amount":1}`, 5},
		{"new zero wins over legacy", `{"id":"o","description":"Roof","year":9,"month":0,"amount":1}`, 0},
		{"negative month is kept as a past entry", `{"id":"o","description":"Roof","month":-4,"amount":1}`, -4},
		{"neither key", `{"id":"o","description":"Roof","amount":1}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e OneTimeExpense
			if err := json.Unmarshal([]byte(tc.raw), &e); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if e.Month != tc.want {
				t.Errorf("Month = %d, want %d", e.Month, tc.want)
			}
			if e.Description != "Roof" || e.Amount != 1 {
				t.Errorf("non-schedule fields lost: %+v", e)
			}
		})
	}
}

func TestBigTicketItemLegacyYearDecode(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"legacy only", `{"id":"b","name":"Car","amount":5000,"year":1,"type":"expense"}`, 12},
		{"legacy zero", `{"id":"b","name":"Car","amount":5000,"year":0,"type":"expense"}`, 0},
		{"new only", `{"id":"b","name":"Car","amount":5000,"month":5,"type":"expense"}`, 5},
		{"new wins over legacy", `{"id":"b","name":"Car","amount":5000,"year":9,"month":5,"type":"expense"}`, 5},
		{"new zero wins over legacy", `{"id":"b","name":"Car","amount":5000,"year":9,"month":0,"type":"expense"}`, 0},
		{"negative month is kept as a past entry", `{"id":"b","name":"Car","amount":5000,"month":-4,"type":"expense"}`, -4},
		{"neither key", `{"id":"b","name":"Car","amount":5000,"type":"expense"}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b BigTicketItem
			if err := json.Unmarshal([]byte(tc.raw), &b); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if b.Month != tc.want {
				t.Errorf("Month = %d, want %d", b.Month, tc.want)
			}
			if b.Name != "Car" || b.Amount != 5000 || b.Type != BigTicketExpense {
				t.Errorf("non-schedule fields lost: %+v", b)
			}
		})
	}
}

// The writer emits ONLY the month keys, so a file written today can never be
// read back through the legacy conversion path.
func TestScheduleMarshalOmitsLegacyKeys(t *testing.T) {
	end := 24
	s := &WhatIfSettings{
		ExpenseSources:  []ExpenseSource{{ID: "e", Name: "Boat", Amount: 500, StartMonth: 12, EndMonth: &end}},
		OneTimeExpenses: []OneTimeExpense{{ID: "o", Description: "Roof", Month: 7, Amount: 1}},
		BigTicketItems:  []BigTicketItem{{ID: "b", Name: "Car", Amount: 5000, Month: 5, Type: BigTicketExpense}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(raw)
	for _, key := range []string{`"start_year"`, `"end_year"`, `"year":`} {
		if strings.Contains(got, key) {
			t.Errorf("legacy key %s emitted: %s", key, got)
		}
	}
	for _, key := range []string{`"start_month":12`, `"end_month":24`, `"month":7`, `"month":5`} {
		if !strings.Contains(got, key) {
			t.Errorf("missing %s in %s", key, got)
		}
	}

	// A round trip of the new shape is stable: decoding what we just wrote
	// must not re-run any legacy conversion.
	var back WhatIfSettings
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.ExpenseSources[0].StartMonth != 12 || back.ExpenseSources[0].EndMonth == nil || *back.ExpenseSources[0].EndMonth != 24 {
		t.Errorf("expense round trip: %+v", back.ExpenseSources[0])
	}
	if back.OneTimeExpenses[0].Month != 7 || back.BigTicketItems[0].Month != 5 {
		t.Errorf("one-time/big-ticket round trip: %+v %+v", back.OneTimeExpenses[0], back.BigTicketItems[0])
	}
}

func intp(v int) *int { return &v }
