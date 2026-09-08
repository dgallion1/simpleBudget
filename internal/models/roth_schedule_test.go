package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRothScheduleJSONRoundTrip(t *testing.T) {
	want := RothConversionConfig{Enabled: true, StartYear: 0, EndYear: 2, PerYearOverrides: map[int]float64{0: 12345.67, 1: 0, 2: 8910.11}}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got RothConversionConfig
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedule lost on reload: got %#v want %#v", got, want)
	}
}
