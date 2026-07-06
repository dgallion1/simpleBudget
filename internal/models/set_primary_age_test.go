package models

import "testing"

// SetPrimaryAge must keep CurrentAge and the primary person's BirthMonth in
// agreement so prepare.ComputeAges derives the same age the caller asked for.
func TestSetPrimaryAge_PinsBirthMonthToMatch(t *testing.T) {
	s := DefaultWhatIfSettings()
	s.SetPrimaryAge(70)

	if s.CurrentAge != 70 {
		t.Fatalf("CurrentAge = %d, want 70", s.CurrentAge)
	}
	got, err := DeriveAgeAtStartDate(s.StartDate, s.Persons[0].BirthMonth)
	if err != nil {
		t.Fatalf("DeriveAgeAtStartDate: %v", err)
	}
	if got != 70 {
		t.Fatalf("derived age from pinned BirthMonth = %d, want 70", got)
	}
}

func TestSetPrimaryAge_NoPrimaryPerson(t *testing.T) {
	s := DefaultWhatIfSettings()
	s.Persons = nil
	s.SetPrimaryAge(64)
	if s.CurrentAge != 64 {
		t.Fatalf("CurrentAge = %d, want 64 even with no primary person", s.CurrentAge)
	}
}
