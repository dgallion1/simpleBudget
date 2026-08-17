package models

import (
	"testing"
	"time"
)

func TestStableIDFor_Format(t *testing.T) {
	date := time.Date(2025, 5, 4, 13, 45, 0, 0, time.UTC)
	got := StableIDFor("usaa-checking", date, -1234, 0)
	want := "usaa-checking|2025-05-04|-1234|0"
	if got != want {
		t.Errorf("StableIDFor() = %q, want %q", got, want)
	}
}

// TestStableIDFor_VariesWithOccurrence pins the one degree of freedom the
// identity has left after the description came out of it: two rows that agree
// on account, day and cents must still be told apart.
func TestStableIDFor_VariesWithOccurrence(t *testing.T) {
	date := time.Date(2025, 5, 4, 0, 0, 0, 0, time.UTC)
	first := StableIDFor("usaa-checking", date, -1234, 0)
	second := StableIDFor("usaa-checking", date, -1234, 1)
	if first == second {
		t.Fatalf("occurrence index did not change the id: both %q", first)
	}
	if want := "usaa-checking|2025-05-04|-1234|1"; second != want {
		t.Errorf("occurrence 1 = %q, want %q", second, want)
	}
}

// TestStableIDFor_Deterministic covers the property every sidecar store
// depends on: the same inputs must produce the same key on every load, and
// the time of day must not leak into it.
func TestStableIDFor_Deterministic(t *testing.T) {
	morning := time.Date(2025, 5, 4, 0, 0, 0, 0, time.UTC)
	evening := time.Date(2025, 5, 4, 23, 59, 59, 0, time.UTC)
	a := StableIDFor("acct", morning, 999, 2)
	b := StableIDFor("acct", morning, 999, 2)
	c := StableIDFor("acct", evening, 999, 2)
	if a != b {
		t.Errorf("not deterministic: %q vs %q", a, b)
	}
	if a != c {
		t.Errorf("time of day leaked into the id: %q vs %q", a, c)
	}
}

// TestStableIDFor_UnassignedSlot documents the file:<basename> form used for
// rows whose CSV matched no account.
func TestStableIDFor_UnassignedSlot(t *testing.T) {
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got := StableIDFor("file:mystery.csv", date, -500, 0)
	want := "file:mystery.csv|2026-01-02|-500|0"
	if got != want {
		t.Errorf("StableIDFor() = %q, want %q", got, want)
	}
}

func TestAmountCents(t *testing.T) {
	cases := []struct {
		amount float64
		want   int64
	}{
		{-12.34, -1234},
		{12.34, 1234},
		{0, 0},
		{0.1 + 0.2, 30}, // 0.30000000000000004 must not truncate to 29
		{-0.005, -1},    // rounds away from zero, like the rest of the app
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		if got := AmountCents(c.amount); got != c.want {
			t.Errorf("AmountCents(%v) = %d, want %d", c.amount, got, c.want)
		}
	}
}

// TestResolveByIdentity covers the whole precedence ladder in one place: the
// StableID wins when present, the legacy Hash is the fallback, and a
// transaction in neither form resolves to nothing.
func TestResolveByIdentity(t *testing.T) {
	txn := Transaction{Hash: "legacyhash", StableID: "acct|2025-05-04|-1234|0"}

	if _, _, ok := ResolveByIdentity(map[string]string{}, txn); ok {
		t.Error("empty map resolved")
	}
	if _, _, ok := ResolveByIdentity[string](nil, txn); ok {
		t.Error("nil map resolved")
	}

	legacy := map[string]string{"legacyhash": "expense-a"}
	got, key, ok := ResolveByIdentity(legacy, txn)
	if !ok || got != "expense-a" {
		t.Fatalf("legacy fallback failed: got %q ok=%v", got, ok)
	}
	if key != "legacyhash" {
		t.Errorf("matched key = %q, want the legacy hash", key)
	}

	both := map[string]string{"legacyhash": "expense-a", "acct|2025-05-04|-1234|0": "expense-b"}
	got, key, ok = ResolveByIdentity(both, txn)
	if !ok || got != "expense-b" {
		t.Fatalf("StableID did not win: got %q ok=%v", got, ok)
	}
	if key != txn.StableID {
		t.Errorf("matched key = %q, want the StableID", key)
	}

	unknown := Transaction{Hash: "other", StableID: "other|2025-05-04|-1|0"}
	if _, _, ok := ResolveByIdentity(both, unknown); ok {
		t.Error("unrelated transaction resolved")
	}
}
