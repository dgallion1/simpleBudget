package merchants

import (
	"reflect"
	"testing"

	"budget2/internal/models"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"trailing order number", "NETFLIX.COM 123", "NETFLIX.COM"},
		{"different order number, same merchant", "NETFLIX.COM 456", "NETFLIX.COM"},
		{"all-digit description", "8834912207", ""},
		{"mixed tokens keep digits inside token", "7-ELEVEN 4821 MAIN ST", "7-ELEVEN MAIN ST"},
		{"whitespace collapse", "ACME   COFFEE\tSTORE  ", "ACME COFFEE STORE"},
		{"already clean passthrough", "ACME COFFEE", "ACME COFFEE"},
		{"lowercase gets uppercased", "acme coffee", "ACME COFFEE"},
		{"multiple standalone numeric tokens dropped", "SQ *ACME 001 002", "SQ *ACME"},
		{"standalone asterisk token dropped", "SQ * COFFEE SHOP", "SQ COFFEE SHOP"},
		{"standalone hash token dropped", "ACME # 12", "ACME"},
		{"punctuation-only description", "*", ""},
		{"mixed punctuation-only tokens dropped", "-- ** ##", ""},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.in)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalize_SameKeyForOrderNumberVariants(t *testing.T) {
	a := Normalize("NETFLIX.COM 123")
	b := Normalize("NETFLIX.COM 456")
	if a != b {
		t.Fatalf("expected same normalized key, got %q and %q", a, b)
	}
}

// groupCount returns the number of distinct canonical groups produced
// by GroupKeys for the given keys.
func groupCount(t *testing.T, groups map[string]string) int {
	t.Helper()
	seen := make(map[string]bool)
	for _, canon := range groups {
		seen[canon] = true
	}
	return len(seen)
}

func TestGroupKeys_MergePositives(t *testing.T) {
	t.Run("two-token subset merges", func(t *testing.T) {
		keys := []string{"ACME COFFEE", "ACME COFFEE STORE"}
		got := GroupKeys(keys)
		if got["ACME COFFEE"] != got["ACME COFFEE STORE"] {
			t.Errorf("expected merge, got %#v", got)
		}
		if n := groupCount(t, got); n != 1 {
			t.Errorf("expected 1 group, got %d", n)
		}
	})

	t.Run("chain A subset B subset C merges transitively", func(t *testing.T) {
		keys := []string{
			"ACME COFFEE",
			"ACME COFFEE STORE",
			"ACME COFFEE STORE #12",
		}
		got := GroupKeys(keys)
		if n := groupCount(t, got); n != 1 {
			t.Errorf("expected 1 group via transitive chain, got %d: %#v", n, got)
		}
	})

	t.Run("identical keys merge trivially", func(t *testing.T) {
		keys := []string{"ACME COFFEE", "ACME COFFEE"}
		got := GroupKeys(keys)
		if got["ACME COFFEE"] == "" {
			t.Fatalf("expected canonical key, got empty")
		}
		if n := groupCount(t, got); n != 1 {
			t.Errorf("expected 1 group, got %d", n)
		}
	})
}

func TestGroupKeys_MergeNegatives(t *testing.T) {
	t.Run("SQ prefixed distinct merchants stay separate", func(t *testing.T) {
		keys := []string{"SQ *COFFEE SHOP A", "SQ *COFFEE SHOP B"}
		got := GroupKeys(keys)
		if got["SQ *COFFEE SHOP A"] == got["SQ *COFFEE SHOP B"] {
			t.Errorf("expected separate groups, got merged: %#v", got)
		}
		if n := groupCount(t, got); n != 2 {
			t.Errorf("expected 2 groups, got %d", n)
		}
	})

	t.Run("bare SQ does not bridge unrelated merchants", func(t *testing.T) {
		keys := []string{"SQ", "SQ *COFFEE SHOP A", "SQ *PIZZA PLACE B"}
		got := GroupKeys(keys)
		if n := groupCount(t, got); n != 3 {
			t.Errorf("expected 3 groups, got %d: %#v", n, got)
		}
		// each key remains its own canonical group (no cross merges)
		if got["SQ *COFFEE SHOP A"] == got["SQ *PIZZA PLACE B"] {
			t.Errorf("SQ bridged two unrelated merchants: %#v", got)
		}
		if got["SQ"] == got["SQ *COFFEE SHOP A"] || got["SQ"] == got["SQ *PIZZA PLACE B"] {
			t.Errorf("bare SQ merged into a rich group: %#v", got)
		}
	})

	t.Run("all-digit key stays isolated among rich keys", func(t *testing.T) {
		keys := []string{
			"ACME COFFEE",
			"ACME COFFEE STORE",
			"WIDGET WORLD DOWNTOWN",
			"8834912207", // normalizes to "" upstream; here we pass the
			// already-normalized empty key directly to GroupKeys to
			// exercise the guard in isolation.
		}
		// Simulate what Normalize would produce for the all-digit
		// description: empty string.
		keys[3] = ""
		got := GroupKeys(keys)
		if got["ACME COFFEE"] != got["ACME COFFEE STORE"] {
			t.Errorf("expected ACME COFFEE variants to merge, got %#v", got)
		}
		if got[""] == got["ACME COFFEE"] || got[""] == got["WIDGET WORLD DOWNTOWN"] {
			t.Errorf("empty key merged into a rich group: %#v", got)
		}
		if n := groupCount(t, got); n != 3 {
			t.Errorf("expected 3 groups (ACME cluster, WIDGET, empty), got %d: %#v", n, got)
		}
	})

	t.Run("two bare SQ keys merge on exact equality", func(t *testing.T) {
		keys := []string{"SQ", "SQ"}
		got := GroupKeys(keys)
		if n := groupCount(t, got); n != 1 {
			t.Errorf("expected 1 group for identical bare keys, got %d", n)
		}
	})

	t.Run("two empty keys merge on exact equality", func(t *testing.T) {
		keys := []string{"", ""}
		got := GroupKeys(keys)
		if n := groupCount(t, got); n != 1 {
			t.Errorf("expected 1 group for identical empty keys, got %d", n)
		}
	})
}

func TestGroupKeys_CanonicalSelection(t *testing.T) {
	t.Run("most token-rich member wins", func(t *testing.T) {
		keys := []string{"ACME COFFEE", "ACME COFFEE STORE", "ACME COFFEE STORE #12"}
		got := GroupKeys(keys)
		want := "ACME COFFEE STORE #12"
		for _, k := range keys {
			if got[k] != want {
				t.Errorf("GroupKeys(%v)[%q] = %q, want %q", keys, k, got[k], want)
			}
		}
	})

	t.Run("ties broken lexicographically", func(t *testing.T) {
		// Two keys with equal token counts that are subsets of each
		// other only if identical; construct a tie via three keys of
		// equal richness where two qualify as the joint richest via a
		// shared smaller member.
		keys := []string{"ACME COFFEE", "BETA COFFEE ACME", "ACME COFFEE ZETA"}
		// "ACME COFFEE" (2 tokens) is a subset of both "ACME COFFEE
		// ZETA" (3 tokens: ACME, COFFEE, ZETA) — merges.
		// "BETA COFFEE ACME" (3 tokens: BETA, COFFEE, ACME) is NOT a
		// superset of "ACME COFFEE" in the sense required... actually
		// ACME COFFEE tokens {ACME,COFFEE} IS a subset of {BETA,COFFEE,ACME}.
		// So all three merge into one group; richest tied members are
		// "BETA COFFEE ACME" and "ACME COFFEE ZETA" (3 tokens each).
		// Lexicographic tie-break: "ACME COFFEE ZETA" < "BETA COFFEE ACME".
		got := GroupKeys(keys)
		want := "ACME COFFEE ZETA"
		for _, k := range keys {
			if got[k] != want {
				t.Errorf("GroupKeys(%v)[%q] = %q, want %q (tie-break)", keys, k, got[k], want)
			}
		}
	})
}

func TestGroupKeys_StableAcrossInputOrder(t *testing.T) {
	forward := []string{
		"ACME COFFEE",
		"ACME COFFEE STORE",
		"ACME COFFEE STORE #12",
		"SQ",
		"SQ *COFFEE SHOP A",
		"SQ *PIZZA PLACE B",
		"WIDGET WORLD DOWNTOWN",
		"",
	}
	reversed := make([]string, len(forward))
	for i, k := range forward {
		reversed[len(forward)-1-i] = k
	}

	gotForward := GroupKeys(forward)
	gotReversed := GroupKeys(reversed)

	if !reflect.DeepEqual(gotForward, gotReversed) {
		t.Errorf("GroupKeys not stable across input order:\nforward:  %#v\nreversed: %#v", gotForward, gotReversed)
	}
}

func TestGroupTransactions_DisplayNameOverDescription(t *testing.T) {
	ts := []models.Transaction{
		{ID: "1", Description: "NETFLIX.COM 123", DisplayName: "Streaming Bill"},
		{ID: "2", Description: "NETFLIX.COM 456"},
	}
	got := GroupTransactions(ts)

	// "1" keys off DisplayName -> "STREAMING BILL"; "2" keys off
	// Description -> "NETFLIX.COM". Neither is a subset of the other
	// (different single tokens... "NETFLIX.COM" is 1 token, "STREAMING
	// BILL" is 2 tokens) so per the guard a 1-token key only merges on
	// exact equality; they must land in separate groups.
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d: %#v", len(got), got)
	}

	found1, found2 := false, false
	for _, txns := range got {
		for _, tx := range txns {
			if tx.ID == "1" {
				found1 = true
			}
			if tx.ID == "2" {
				found2 = true
			}
		}
	}
	if !found1 || !found2 {
		t.Fatalf("expected both transactions present across groups, got %#v", got)
	}
}

func TestGroupTransactions_PreservesOrderWithinGroup(t *testing.T) {
	ts := []models.Transaction{
		{ID: "1", Description: "ACME COFFEE"},
		{ID: "2", Description: "ACME COFFEE STORE"},
		{ID: "3", Description: "ACME COFFEE STORE #12"},
	}
	got := GroupTransactions(ts)
	if len(got) != 1 {
		t.Fatalf("expected 1 group, got %d: %#v", len(got), got)
	}
	var group []models.Transaction
	for _, v := range got {
		group = v
	}
	if len(group) != 3 {
		t.Fatalf("expected 3 transactions in group, got %d", len(group))
	}
	wantOrder := []string{"1", "2", "3"}
	for i, tx := range group {
		if tx.ID != wantOrder[i] {
			t.Errorf("group[%d].ID = %q, want %q (order not preserved)", i, tx.ID, wantOrder[i])
		}
	}
}

func TestGroupTransactions_DeterministicCanonical(t *testing.T) {
	ts := []models.Transaction{
		{ID: "1", Description: "ACME COFFEE"},
		{ID: "2", Description: "ACME COFFEE STORE"},
	}
	got1 := GroupTransactions(ts)
	got2 := GroupTransactions(ts)

	keys1 := make([]string, 0, len(got1))
	for k := range got1 {
		keys1 = append(keys1, k)
	}
	keys2 := make([]string, 0, len(got2))
	for k := range got2 {
		keys2 = append(keys2, k)
	}
	if !reflect.DeepEqual(keys1, keys2) {
		t.Errorf("canonical key not deterministic across calls: %v vs %v", keys1, keys2)
	}
	if len(keys1) != 1 || keys1[0] != "ACME COFFEE STORE" {
		t.Errorf("expected canonical key ACME COFFEE STORE, got %v", keys1)
	}
}

func TestGroupTransactions_EmptyInput(t *testing.T) {
	got := GroupTransactions(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map for empty input, got %#v", got)
	}
}

// TestGroupKeys_DiamondSubsetMergesIntoOneGroup pins the intended
// transitive-closure semantics for a "diamond" shape: "ACME COFFEE" (A)
// and "ACME STORE" (B) are NOT subsets of each other (A has token STORE
// missing, B has token COFFEE missing), but both are subsets of "ACME
// COFFEE STORE" (C). Because GroupKeys compares every pair — including
// A-C and B-C — both A and C, and B and C, merge directly, which pulls A
// and B into the same union-find group transitively even though A and B
// never merge with each other directly. All three must land in one group.
// (P1 acceptance reviewer nit: pin this so a future refactor that only
// checks adjacent/chain subsets, instead of all pairs, doesn't silently
// change this behavior.)
func TestGroupKeys_DiamondSubsetMergesIntoOneGroup(t *testing.T) {
	keys := []string{"ACME COFFEE", "ACME STORE", "ACME COFFEE STORE"}
	got := GroupKeys(keys)

	if got["ACME COFFEE"] != got["ACME STORE"] || got["ACME STORE"] != got["ACME COFFEE STORE"] {
		t.Fatalf("expected diamond subset case to merge into 1 group, got %#v", got)
	}
	if n := groupCount(t, got); n != 1 {
		t.Errorf("expected 1 group for diamond subset case, got %d: %#v", n, got)
	}
	// The richest member (3 tokens) is the canonical key.
	if want := "ACME COFFEE STORE"; got["ACME COFFEE"] != want {
		t.Errorf("expected canonical key %q, got %q", want, got["ACME COFFEE"])
	}
}

// TestGroupKeys_SpacedAsteriskNoLongerBridges pins the FIXED behavior
// (Ruling 2026-08-12, ANALYTICS_PORT_SPEC.md §2 as amended) for
// Square-style descriptions with a spaced asterisk. Normalize now drops
// standalone punctuation-only tokens exactly like all-digit tokens, so
// "SQ * COFFEE SHOP" normalizes to {SQ, COFFEE, SHOP} (the "*" token is
// gone) and "SQ * PIZZA PLACE" to {SQ, PIZZA, PLACE}.
//
// Two assertions:
//  1. Two-key case: neither of the two 3-token keys is a subset of the
//     other (they diverge in 2 of 3 tokens, sharing only "SQ"), so they
//     do not merge.
//  2. Three-key case: adding a bare "SQ *" row — which previously
//     normalized to the 2-token degenerate key {SQ, *} and bridged the
//     two rich keys together via the token-subset rule — now normalizes
//     to plain "SQ" (1 token). A 1-token key is degenerate under the
//     guard and merges only on exact equality, so it does not bridge
//     either rich key. All three keys land in separate groups: the
//     bridge is closed.
func TestGroupKeys_SpacedAsteriskNoLongerBridges(t *testing.T) {
	coffeeRaw := "SQ * COFFEE SHOP"
	pizzaRaw := "SQ * PIZZA PLACE"
	bareRaw := "SQ *"

	coffee := Normalize(coffeeRaw)
	pizza := Normalize(pizzaRaw)
	bare := Normalize(bareRaw)

	if want := "SQ COFFEE SHOP"; coffee != want {
		t.Fatalf("Normalize(%q) = %q, want %q", coffeeRaw, coffee, want)
	}
	if want := "SQ PIZZA PLACE"; pizza != want {
		t.Fatalf("Normalize(%q) = %q, want %q", pizzaRaw, pizza, want)
	}
	if want := "SQ"; bare != want {
		t.Fatalf("Normalize(%q) = %q, want %q", bareRaw, bare, want)
	}

	t.Run("two spaced-asterisk keys stay separate", func(t *testing.T) {
		got := GroupKeys([]string{coffee, pizza})
		if got[coffee] == got[pizza] {
			t.Errorf("expected separate groups, got merged: %#v", got)
		}
		if n := groupCount(t, got); n != 2 {
			t.Errorf("expected 2 groups, got %d: %#v", n, got)
		}
	})

	t.Run("bare SQ from spaced asterisk does not bridge the two rich keys", func(t *testing.T) {
		got := GroupKeys([]string{coffee, pizza, bare})
		if n := groupCount(t, got); n != 3 {
			t.Errorf("expected 3 groups (bridge closed), got %d: %#v", n, got)
		}
		if got[coffee] == got[pizza] {
			t.Errorf("bare SQ bridged the two rich keys together: %#v", got)
		}
		if got[bare] == got[coffee] || got[bare] == got[pizza] {
			t.Errorf("bare SQ merged into a rich group: %#v", got)
		}
	})
}

func TestGroupTransactions_SuppressedNotSkipped(t *testing.T) {
	// GroupTransactions does not filter Suppressed transactions; callers
	// are responsible for calling TransactionSet.Active() first if they
	// want suppressed rows excluded.
	ts := []models.Transaction{
		{ID: "1", Description: "ACME COFFEE", Suppressed: true},
	}
	got := GroupTransactions(ts)
	total := 0
	for _, txns := range got {
		total += len(txns)
	}
	if total != 1 {
		t.Errorf("expected suppressed transaction to still be grouped, got %d total", total)
	}
}
