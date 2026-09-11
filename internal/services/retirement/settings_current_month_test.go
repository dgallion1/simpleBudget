package retirement

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func TestCurrentMonthLoadRefreshesCachedAges(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(root, store)
	s := models.DefaultWhatIfSettings()
	if err := json.Unmarshal([]byte(`{"use_current_month":true,"start_date":"2000-01","persons":[{"id":"you","name":"You","role":"primary","birth_month":"1960-09"},{"id":"spouse","name":"Spouse","role":"spouse","birth_month":"1962-12"}]}`), s); err != nil {
		t.Fatal(err)
	}
	sm.cache = s
	for _, withRevision := range []bool{false, true} {
		var got *models.WhatIfSettings
		if withRevision {
			got, _, err = sm.LoadContextWithRevision(context.Background())
		} else {
			got, err = sm.Load()
		}
		if err != nil {
			t.Fatal(err)
		}
		month := time.Now().Format("2006-01")
		if got.StartDate != month {
			t.Fatalf("start date = %s, want current month %s", got.StartDate, month)
		}
		for _, p := range got.Persons {
			want, err := models.DeriveAgeAtStartDate(month, p.BirthMonth)
			if err != nil {
				t.Fatal(err)
			}
			if got.PersonAge(p.ID) != want {
				t.Errorf("%s age = %d, want %d", p.ID, got.PersonAge(p.ID), want)
			}
		}
		if got.CurrentAge != got.PersonAge("you") || got.SpouseAge != got.PersonAge("spouse") {
			t.Fatal("derived engine ages are stale")
		}
	}
	if sm.cache.StartDate != "2000-01" {
		t.Fatal("load mutated shared cache")
	}
}

func TestCurrentMonthLegacyPlanMigration(t *testing.T) {
	sm := &SettingsManager{}
	s, changed, err := sm.decodeSettings([]byte(`{"start_date":"2020-01","persons":[{"id":"you","name":"You","role":"primary","birth_month":"1960-09"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || s.StartDate != time.Now().Format("2006-01") {
		t.Fatalf("legacy plan stayed at %s", s.StartDate)
	}
}
