package ledger

import (
	"fmt"
	"strings"
	"testing"
)

// R10 gap 2: anchorConsequences and resolveConsequences produce the text a
// human reads before approving. R4 binds the approval to the exact
// operation, but that binding is only as good as this prose: if it drifts to
// describe a different amount, date or verdict than the one about to be
// written, the binding would faithfully authorize an operation the human
// misread.
//
// These tests assert on the load-bearing FACTS the text must contain --
// account id, amount, date, pair key, verdict -- not on the sentences around
// them, so a copyedit or a typo fix cannot break them. Only a change that
// drops or swaps one of those facts should fail these.

// TestAnchorConsequencesNamesTheAccountAmountAndDate pins the three facts
// set_balance_anchor is about to write: which account, what balance, as of
// what day. It checks two very different inputs so a fixed/duplicated
// literal in the implementation could not accidentally satisfy it.
func TestAnchorConsequencesNamesTheAccountAmountAndDate(t *testing.T) {
	cases := []struct {
		id     string
		date   string
		amount float64
	}{
		{"checking", "2026-08-15", 4210.55},
		{"brokerage-9", "2019-01-31", -523.10},
	}
	for _, c := range cases {
		text := anchorConsequences(c.id, c.date, c.amount)
		if !strings.Contains(text, c.id) {
			t.Errorf("anchorConsequences(%q, %q, %v) = %q, does not name the account", c.id, c.date, c.amount, text)
		}
		amtStr := fmt.Sprintf("%.2f", c.amount)
		if !strings.Contains(text, amtStr) {
			t.Errorf("anchorConsequences(%q, %q, %v) = %q, does not state the amount %s", c.id, c.date, c.amount, text, amtStr)
		}
		if !strings.Contains(text, c.date) {
			t.Errorf("anchorConsequences(%q, %q, %v) = %q, does not state the date %s", c.id, c.date, c.amount, text, c.date)
		}
	}
}

// TestAnchorConsequencesDistinguishesAmounts is the discrimination case R4's
// binding depends on this prose for: a human approving "the anchor would be
// 500.00" must not be shown identical text (or text that merely also
// mentions 500.00) for a write of 5000.00.
func TestAnchorConsequencesDistinguishesAmounts(t *testing.T) {
	small := anchorConsequences("checking", "2026-08-15", 500.00)
	big := anchorConsequences("checking", "2026-08-15", 5000.00)
	if small == big {
		t.Fatal("500.00 and 5000.00 render identical consequence text")
	}
	if strings.Contains(small, "5000.00") {
		t.Errorf("the 500.00 preview also states 5000.00: %q", small)
	}
	if !strings.Contains(big, "5000.00") {
		t.Errorf("the 5000.00 preview does not state 5000.00: %q", big)
	}
}

// TestResolveConsequencesNamesThePairKeyAndVerdict pins the two facts
// resolve_transfer is about to write: which pair, and which verdict. It
// checks that the confirm and reject renderings each state their own
// verdict and do not also carry the other one, since a pair confirmed by
// mistake silently erases real income or spending.
func TestResolveConsequencesNamesThePairKeyAndVerdict(t *testing.T) {
	const key = "2026-08-10|checking|-777.77|schwab|777.77"

	confirmText := resolveConsequences(key, "confirm")
	if !strings.Contains(confirmText, key) {
		t.Errorf("resolveConsequences(confirm) = %q, does not name the pair key", confirmText)
	}
	if !strings.Contains(confirmText, "CONFIRMED") {
		t.Errorf("resolveConsequences(confirm) = %q, does not state the CONFIRMED verdict", confirmText)
	}
	if strings.Contains(confirmText, "REJECTED") {
		t.Errorf("resolveConsequences(confirm) = %q, also states the opposite (REJECTED) verdict", confirmText)
	}

	rejectText := resolveConsequences(key, "reject")
	if !strings.Contains(rejectText, key) {
		t.Errorf("resolveConsequences(reject) = %q, does not name the pair key", rejectText)
	}
	if !strings.Contains(rejectText, "REJECTED") {
		t.Errorf("resolveConsequences(reject) = %q, does not state the REJECTED verdict", rejectText)
	}
	if strings.Contains(rejectText, "CONFIRMED") {
		t.Errorf("resolveConsequences(reject) = %q, also states the opposite (CONFIRMED) verdict", rejectText)
	}

	if confirmText == rejectText {
		t.Fatal("confirm and reject render identical consequence text")
	}
}

// TestResolveConsequencesDistinguishesPairKeys guards the other half of the
// binding: two different pairs must not render the same text, or an approval
// for one pair could be mistaken for another.
func TestResolveConsequencesDistinguishesPairKeys(t *testing.T) {
	a := resolveConsequences("pair-a", "confirm")
	b := resolveConsequences("pair-b", "confirm")
	if a == b {
		t.Fatal("two different pair keys render identical consequence text")
	}
	if strings.Contains(a, "pair-b") {
		t.Errorf("pair-a's preview also names pair-b: %q", a)
	}
}
