package accounts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func newStore(t *testing.T) (string, *storage.Storage) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return dir, s
}

// TestLoad_MissingFileIsNotAnError pins the contract that "no accounts
// configured yet" is the normal starting state: an empty slice and a nil
// error, never an error the caller has to special-case.
func TestLoad_MissingFileIsNotAnError(t *testing.T) {
	_, s := newStore(t)

	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load with no accounts.json: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load returned %d accounts, want 0", len(got))
	}
	if got == nil {
		t.Error("Load returned a nil slice, want an empty one")
	}
}

// TestSaveLoad_RoundTripIncludingAnchors verifies every field survives a
// trip through the storage service, anchors included.
func TestSaveLoad_RoundTripIncludingAnchors(t *testing.T) {
	dir, s := newStore(t)

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	want := []models.Account{
		{
			ID:           "usaa-checking",
			Name:         "USAA Checking",
			Institution:  "USAA",
			Kind:         models.AccountKindChecking,
			FilePatterns: []string{"usaa-checking*.csv", "usaa"},
			Anchors: []models.BalanceAnchor{
				{Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 4210.55, Note: "statement"},
				{Date: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), Amount: 3980.10},
			},
			LowBalanceThreshold: 750,
			CreatedAt:           created,
			UpdatedAt:           created,
		},
		{
			ID:        "chase-card",
			Name:      "Chase Card",
			Kind:      models.AccountKindCredit,
			CreatedAt: created,
			UpdatedAt: created,
		},
	}

	if err := Save(s, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AccountsFile)); err != nil {
		t.Fatalf("accounts.json not written: %v", err)
	}

	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Load returned %d accounts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Name != want[i].Name ||
			got[i].Institution != want[i].Institution || got[i].Kind != want[i].Kind ||
			got[i].LowBalanceThreshold != want[i].LowBalanceThreshold {
			t.Errorf("account %d = %+v, want %+v", i, got[i], want[i])
		}
		if !got[i].CreatedAt.Equal(want[i].CreatedAt) || !got[i].UpdatedAt.Equal(want[i].UpdatedAt) {
			t.Errorf("account %d timestamps = %v/%v, want %v/%v",
				i, got[i].CreatedAt, got[i].UpdatedAt, want[i].CreatedAt, want[i].UpdatedAt)
		}
		if len(got[i].FilePatterns) != len(want[i].FilePatterns) {
			t.Errorf("account %d patterns = %v, want %v", i, got[i].FilePatterns, want[i].FilePatterns)
		}
		if len(got[i].Anchors) != len(want[i].Anchors) {
			t.Fatalf("account %d anchors = %d, want %d", i, len(got[i].Anchors), len(want[i].Anchors))
		}
		for j := range want[i].Anchors {
			wa, ga := want[i].Anchors[j], got[i].Anchors[j]
			if !ga.Date.Equal(wa.Date) || ga.Amount != wa.Amount || ga.Note != wa.Note {
				t.Errorf("account %d anchor %d = %+v, want %+v", i, j, ga, wa)
			}
		}
	}
}

// TestSaveLoad_EmptySliceRoundTrips covers deleting the last account: the
// file exists and reads back as empty, not as a decode failure.
func TestSaveLoad_EmptySliceRoundTrips(t *testing.T) {
	_, s := newStore(t)

	if err := Save(s, nil); err != nil {
		t.Fatalf("Save(nil): %v", err)
	}
	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load returned %d accounts, want 0", len(got))
	}
}

// TestLoad_AcceptsBareArrayFixture keeps a hand-written accounts.json
// readable whether it uses the canonical {"accounts": [...]} object or a
// bare array.
func TestLoad_AcceptsBareArrayFixture(t *testing.T) {
	dir, s := newStore(t)

	fixture := `[{"id":"a1","name":"A One","kind":"savings","file_patterns":["a1*.csv"]}]`
	if err := os.WriteFile(filepath.Join(dir, AccountsFile), []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a1" || got[0].Kind != models.AccountKindSavings {
		t.Fatalf("Load = %+v, want one savings account a1", got)
	}
}

func TestLoad_CorruptFileIsAnError(t *testing.T) {
	dir, s := newStore(t)

	if err := os.WriteFile(filepath.Join(dir, AccountsFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := Load(s); err == nil {
		t.Error("Load on a corrupt accounts.json returned nil error")
	}
}

func TestSave_RejectsDuplicateIDsAndEmptyNames(t *testing.T) {
	tests := []struct {
		name  string
		accts []models.Account
	}{
		{
			name: "duplicate IDs",
			accts: []models.Account{
				{ID: "dup", Name: "First"},
				{ID: "dup", Name: "Second"},
			},
		},
		{
			name:  "empty name",
			accts: []models.Account{{ID: "nameless", Name: "  "}},
		},
		{
			name:  "empty ID",
			accts: []models.Account{{ID: "", Name: "No ID"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, s := newStore(t)
			if err := Save(s, tt.accts); err == nil {
				t.Fatal("Save accepted an invalid account set")
			}
			if _, err := os.Stat(filepath.Join(dir, AccountsFile)); !os.IsNotExist(err) {
				t.Error("Save wrote accounts.json despite rejecting the input")
			}
		})
	}
}

// TestSaveWithWarnings_OverlappingPatternsWarnButSave pins the design's
// distinction: an overlap is advisory, not fatal, because first match still
// wins deterministically by ID order.
func TestSaveWithWarnings_OverlappingPatternsWarnButSave(t *testing.T) {
	dir, s := newStore(t)

	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte("Date,Description,Amount\n"), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	accts := []models.Account{
		{ID: "b-broad", Name: "Broad", Kind: models.AccountKindOther, FilePatterns: []string{"*.csv"}},
		{ID: "a-usaa", Name: "USAA", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	}

	warnings, err := SaveWithWarnings(s, accts)
	if err != nil {
		t.Fatalf("SaveWithWarnings: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load returned %d accounts, want 2 (the save must still happen)", len(got))
	}
	if id := MatchFile(accts, "usaa-checking.csv"); id != "a-usaa" {
		t.Errorf("MatchFile with overlapping patterns = %q, want %q (lowest ID wins)", id, "a-usaa")
	}
}

func TestSaveWithWarnings_NoOverlapNoWarning(t *testing.T) {
	dir, s := newStore(t)
	if err := os.WriteFile(filepath.Join(dir, "usaa-checking.csv"), []byte("Date,Description,Amount\n"), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	warnings, err := SaveWithWarnings(s, []models.Account{
		{ID: "a-usaa", Name: "USAA", FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "b-chase", Name: "Chase", FilePatterns: []string{"chase*.csv"}},
	})
	if err != nil {
		t.Fatalf("SaveWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}
}

func TestMatchFile(t *testing.T) {
	// Deliberately NOT in ID order: the result must not depend on how the
	// caller happens to hold the slice.
	accts := []models.Account{
		{ID: "z-catchall", Name: "Catch All", FilePatterns: []string{"*.csv"}},
		{ID: "m-usaa", Name: "USAA", FilePatterns: []string{"USAA-Checking*.CSV"}},
		{ID: "a-amex", Name: "Amex", FilePatterns: []string{"amex"}},
		{ID: "n-broken", Name: "Broken Glob", FilePatterns: []string{"[unclosed"}},
		{ID: "p-blank", Name: "Blank Patterns", FilePatterns: []string{"", "   "}},
	}

	tests := []struct {
		name     string
		accts    []models.Account
		basename string
		want     string
	}{
		{"glob match, case-insensitive both ways", accts, "usaa-checking-2026.csv", "m-usaa"},
		{"substring fallback", accts, "AMEX-2026-08.csv", "a-amex"},
		{"malformed glob falls back to substring", accts, "[unclosed-stuff.csv", "n-broken"},
		{"first match wins by ascending ID", accts, "zzz-unknown.csv", "z-catchall"},
		{"empty accounts", nil, "anything.csv", ""},
		{"empty basename", accts, "", ""},
		{"no match", []models.Account{
			{ID: "a", Name: "A", FilePatterns: []string{"chase*.csv"}},
			{ID: "b", Name: "B", FilePatterns: []string{"usaa*.csv"}},
		}, "vanguard-2026.csv", ""},
		{"account with no patterns never matches", []models.Account{
			{ID: "a", Name: "A"},
		}, "a.csv", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchFile(tt.accts, tt.basename); got != tt.want {
				t.Errorf("MatchFile(%q) = %q, want %q", tt.basename, got, tt.want)
			}
		})
	}
}

// TestMatchFile_DeterministicRegardlessOfSliceOrder is the property the
// ID sort exists for: shuffling the input cannot change the answer.
func TestMatchFile_DeterministicRegardlessOfSliceOrder(t *testing.T) {
	a := models.Account{ID: "aaa", Name: "A", FilePatterns: []string{"*.csv"}}
	b := models.Account{ID: "bbb", Name: "B", FilePatterns: []string{"*.csv"}}

	if got := MatchFile([]models.Account{a, b}, "x.csv"); got != "aaa" {
		t.Errorf("MatchFile([a,b]) = %q, want %q", got, "aaa")
	}
	if got := MatchFile([]models.Account{b, a}, "x.csv"); got != "aaa" {
		t.Errorf("MatchFile([b,a]) = %q, want %q", got, "aaa")
	}
}

// TestMatchFile_DoesNotReorderCallerSlice guards the copy in sortedByID.
func TestMatchFile_DoesNotReorderCallerSlice(t *testing.T) {
	accts := []models.Account{
		{ID: "zzz", Name: "Z", FilePatterns: []string{"z*.csv"}},
		{ID: "aaa", Name: "A", FilePatterns: []string{"a*.csv"}},
	}
	MatchFile(accts, "a.csv")
	if accts[0].ID != "zzz" {
		t.Errorf("MatchFile reordered the caller's slice: %q is now first", accts[0].ID)
	}
}

func TestFind(t *testing.T) {
	accts := []models.Account{
		{ID: "one", Name: "One"},
		{ID: "two", Name: "Two"},
	}
	if got := Find(accts, "two"); got == nil || got.Name != "Two" {
		t.Errorf("Find(two) = %+v, want the Two account", got)
	}
	if got := Find(accts, "three"); got != nil {
		t.Errorf("Find(three) = %+v, want nil", got)
	}
	if got := Find(accts, ""); got != nil {
		t.Errorf("Find(\"\") = %+v, want nil", got)
	}
}
