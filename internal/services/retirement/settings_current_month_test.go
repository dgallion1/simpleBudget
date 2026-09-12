package retirement

import (
	"context"
	"encoding/json"
	"path/filepath"
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

// TestCurrentMonthLegacyPlanMigration pins D2's fix: a plan whose only
// "legacy" trait is the absent use_current_month key (persons already fully
// specified, no healthcare to link) still advances StartDate to the current
// month in memory -- UseCurrentMonth defaults to true when the key is
// absent -- but must NOT report changed, since nothing on disk actually
// needs correcting. Reporting changed here used to force a write-back on
// every such load (see settings.go's D2 comment).
func TestCurrentMonthLegacyPlanMigration(t *testing.T) {
	sm := &SettingsManager{}
	s, changed, err := sm.decodeSettings([]byte(`{"start_date":"2020-01","persons":[{"id":"you","name":"You","role":"primary","birth_month":"1960-09"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.StartDate != time.Now().Format("2006-01") {
		t.Fatalf("legacy plan stayed at %s", s.StartDate)
	}
	if !s.UseCurrentMonth {
		t.Fatal("absent use_current_month key must still mean UseCurrentMonth=true in memory")
	}
	if changed {
		t.Fatal("absent use_current_month key alone must not report changed (D2)")
	}
}

// m9 (F2): a brand-new plan -- the missing-settings-file branch of
// loadInternalContext -- must default UseCurrentMonth to true so it follows
// the current month from the start, matching every other interactive plan.
func TestNewPlanDefaultsUseCurrentMonth(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(root, store)

	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !settings.UseCurrentMonth {
		t.Fatal("new plan (missing settings file) must default UseCurrentMonth=true")
	}
}

// m10 (F3): saveInternal must resolve the current month before writing --
// saving a settings object with UseCurrentMonth=true and a stale StartDate
// must persist the CURRENT month to disk, not the stale one.
func TestSaveInternalResolvesCurrentMonth(t *testing.T) {
	root := t.TempDir()
	store, err := storage.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(root, store)

	settings := models.DefaultWhatIfSettings()
	settings.UseCurrentMonth = true
	settings.StartDate = "2000-01"
	settings.Persons = []models.Person{
		{ID: "you", Name: "You", Role: models.PersonRolePrimary, BirthMonth: "1960-01"},
	}

	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := store.ReadFile(filepath.Join(root, "whatif.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var onDisk map[string]interface{}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("json.Unmarshal on-disk settings: %v", err)
	}

	wantMonth := time.Now().Format("2006-01")
	if onDisk["start_date"] != wantMonth {
		t.Fatalf("on-disk start_date = %v, want current month %q (stale StartDate was not resolved on save)", onDisk["start_date"], wantMonth)
	}
}
