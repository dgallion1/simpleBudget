package transfers

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func day(d int) time.Time {
	return time.Date(2026, 5, d, 0, 0, 0, 0, time.UTC)
}

// txn builds a row the way the loader hands one to Classify: account
// stamped, StableID assigned, type not yet decided.
func txn(stableID, account, desc string, amount float64, d int) models.Transaction {
	return models.Transaction{
		Date:        day(d),
		Amount:      amount,
		Description: desc,
		AccountID:   account,
		StableID:    stableID,
	}
}

// byID indexes a classified slice so assertions can name a row rather than
// depend on slice position.
func byID(t *testing.T, txns []models.Transaction) map[string]models.Transaction {
	t.Helper()
	out := make(map[string]models.Transaction, len(txns))
	for _, x := range txns {
		out[x.StableID] = x
	}
	return out
}

func TestClassify_AutoPairsCleanCrossAccountPair(t *testing.T) {
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -2000, 4),
		txn("usaa|1", "usaa", "DEPOSIT FROM SCHWAB", 2000, 6),
		txn("usaa|2", "usaa", "Wegmans", -84.12, 6),
	}

	res := Classify(in, nil, nil)

	if len(res.Transactions) != 3 {
		t.Fatalf("Classify returned %d rows, want 3 (classification never drops)", len(res.Transactions))
	}
	if res.Paired != 2 {
		t.Fatalf("Paired = %d, want 2 (both legs)", res.Paired)
	}
	if res.External != 0 {
		t.Fatalf("External = %d, want 0", res.External)
	}
	if len(res.Suspected) != 0 {
		t.Fatalf("Suspected = %d, want 0: %+v", len(res.Suspected), res.Suspected)
	}

	rows := byID(t, res.Transactions)
	debit, credit := rows["schwab|1"], rows["usaa|1"]
	for _, leg := range []models.Transaction{debit, credit} {
		if leg.TransactionType != models.Transfer {
			t.Errorf("%s: type = %q, want %q", leg.StableID, leg.TransactionType, models.Transfer)
		}
		if leg.TransferClass != ClassPaired {
			t.Errorf("%s: class = %q, want %q", leg.StableID, leg.TransferClass, ClassPaired)
		}
	}
	if debit.TransferPairKey == "" {
		t.Fatal("paired legs must carry a pair key")
	}
	if debit.TransferPairKey != credit.TransferPairKey {
		t.Errorf("legs carry different pair keys: %q vs %q", debit.TransferPairKey, credit.TransferPairKey)
	}
	if want := PairKeyFor("schwab|1", "usaa|1"); debit.TransferPairKey != want {
		t.Errorf("pair key = %q, want %q", debit.TransferPairKey, want)
	}
	if grocery := rows["usaa|2"]; grocery.TransactionType == models.Transfer {
		t.Error("an unrelated row must not be typed Transfer")
	}
}

func TestClassify_SameAccountEqualAmountDoesNotPair(t *testing.T) {
	// A transfer crosses accounts. Money that leaves and returns inside
	// one account has not moved, and pairing it would erase a real
	// expense and a real credit at once.
	in := []models.Transaction{
		txn("usaa|1", "usaa", "USAA FUNDS TRANSFER", -750, 10),
		txn("usaa|2", "usaa", "USAA FUNDS TRANSFER REVERSAL", 750, 11),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0 -- same-account rows must never pair", res.Paired)
	}
	if len(res.Suspected) != 0 {
		t.Fatalf("Suspected = %d, want 0 -- same-account rows are not candidates", len(res.Suspected))
	}
	for _, x := range res.Transactions {
		if x.TransferPairKey != "" {
			t.Errorf("%s carries pair key %q", x.StableID, x.TransferPairKey)
		}
		if x.TransferClass == ClassPaired {
			t.Errorf("%s classified paired", x.StableID)
		}
	}
}

func TestClassify_PatternlessCoincidenceIsSuggestedNotPaired(t *testing.T) {
	// $60 out of one account and $60 into another in the same week, with
	// nothing in either description saying "transfer". Auto-pairing this
	// would delete a real expense and a real deposit.
	in := []models.Transaction{
		txn("usaa|1", "usaa", "TARGET STORE 1123", -60, 8),
		txn("schwab|1", "schwab", "ZELLE FROM PAT", 60, 9),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0 -- no pattern backs this pair", res.Paired)
	}
	if res.External != 0 {
		t.Fatalf("External = %d, want 0", res.External)
	}
	if len(res.Suspected) != 1 {
		t.Fatalf("Suspected = %d, want 1: %+v", len(res.Suspected), res.Suspected)
	}
	s := res.Suspected[0]
	if s.Reason != ReasonAmountMatch {
		t.Errorf("Reason = %q, want %q", s.Reason, ReasonAmountMatch)
	}
	if s.PairKey != PairKeyFor("schwab|1", "usaa|1") {
		t.Errorf("PairKey = %q, want %q", s.PairKey, PairKeyFor("schwab|1", "usaa|1"))
	}
	for _, x := range res.Transactions {
		if x.TransactionType == models.Transfer {
			t.Errorf("%s was typed Transfer without a user decision", x.StableID)
		}
	}
}

func TestClassify_AmbiguousTieGoesToReview(t *testing.T) {
	// Two candidates exactly as close in date. Picking one would invent a
	// relationship the data does not establish.
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -500, 10),
		txn("usaa|1", "usaa", "DEPOSIT", 500, 9),
		txn("amex|1", "amex", "DEPOSIT", 500, 11),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0 -- an exact tie must not auto-pair", res.Paired)
	}
	if len(res.Suspected) != 2 {
		t.Fatalf("Suspected = %d, want 2 (one per tied candidate): %+v", len(res.Suspected), res.Suspected)
	}
	for _, s := range res.Suspected {
		if s.Reason != ReasonAmbiguous {
			t.Errorf("pair %s: Reason = %q, want %q", s.PairKey, s.Reason, ReasonAmbiguous)
		}
	}
	rows := byID(t, res.Transactions)
	// The pattern-matching leg is still known to be a transfer; it just
	// has no determinable counterparty, so it is external, not an expense.
	if got := rows["schwab|1"]; got.TransactionType != models.Transfer || got.TransferClass != ClassExternal {
		t.Errorf("schwab leg: type/class = %q/%q, want %q/%q",
			got.TransactionType, got.TransferClass, models.Transfer, ClassExternal)
	}
	for _, id := range []string{"usaa|1", "amex|1"} {
		if got := rows[id]; got.TransferPairKey != "" {
			t.Errorf("%s carries pair key %q despite the tie", id, got.TransferPairKey)
		}
	}
}

func TestClassify_ExternalLegHasNoPairKey(t *testing.T) {
	// A Vanguard contribution whose receiving CSV is not imported. The old
	// filter deleted this row; it must now be visible and typed.
	in := []models.Transaction{
		txn("usaa|1", "usaa", "VANGUARD BUY INVESTMENT", -1500, 3),
		txn("usaa|2", "usaa", "Wegmans", -22.10, 3),
	}

	res := Classify(in, nil, nil)

	if res.External != 1 {
		t.Fatalf("External = %d, want 1", res.External)
	}
	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0", res.Paired)
	}
	if len(res.Transactions) != 2 {
		t.Fatalf("Classify returned %d rows, want 2", len(res.Transactions))
	}
	rows := byID(t, res.Transactions)
	vg := rows["usaa|1"]
	if vg.TransactionType != models.Transfer {
		t.Errorf("type = %q, want %q", vg.TransactionType, models.Transfer)
	}
	if vg.TransferClass != ClassExternal {
		t.Errorf("class = %q, want %q", vg.TransferClass, ClassExternal)
	}
	if vg.TransferPairKey != "" {
		t.Errorf("external leg carries pair key %q, want none", vg.TransferPairKey)
	}
	if vg.Amount != -1500 {
		t.Errorf("amount = %v, want -1500 (classification must not renormalize)", vg.Amount)
	}
}

func TestClassify_MajorExpenseFlagActsAsAPattern(t *testing.T) {
	defs := []models.MajorExpense{{
		ID: "me-1", Name: "My broker", Keywords: []string{"my custom broker"},
		IsInternalTransfer: true,
	}}
	in := []models.Transaction{
		txn("usaa|1", "usaa", "MY CUSTOM BROKER ACH", -900, 12),
		txn("schwab|1", "schwab", "ACH CREDIT", 900, 13),
	}

	res := Classify(in, defs, nil)

	if res.Paired != 2 {
		t.Fatalf("Paired = %d, want 2 -- a flagged major expense gates pairing like a pattern", res.Paired)
	}
}

func TestClassify_ConfirmedDecisionPairsWithoutAPattern(t *testing.T) {
	in := []models.Transaction{
		txn("usaa|1", "usaa", "TARGET STORE 1123", -60, 8),
		txn("schwab|1", "schwab", "ZELLE FROM PAT", 60, 9),
	}
	key := PairKeyFor("schwab|1", "usaa|1")
	decisions := map[string]Decision{key: {
		PairKey:   key,
		StableIDs: [2]string{"schwab|1", "usaa|1"},
		Verdict:   VerdictConfirm,
	}}

	res := Classify(in, nil, decisions)

	if res.Paired != 2 {
		t.Fatalf("Paired = %d, want 2 -- a confirmed decision overrides the pattern gate", res.Paired)
	}
	if len(res.Suspected) != 0 {
		t.Fatalf("Suspected = %d, want 0 -- a settled pair is not re-suggested", len(res.Suspected))
	}
	for _, x := range res.Transactions {
		if x.TransactionType != models.Transfer || x.TransferPairKey != key {
			t.Errorf("%s: type/key = %q/%q, want %q/%q", x.StableID, x.TransactionType, x.TransferPairKey, models.Transfer, key)
		}
	}
}

func TestClassify_RejectedDecisionIsNeverSuggestedAgain(t *testing.T) {
	in := []models.Transaction{
		txn("usaa|1", "usaa", "TARGET STORE 1123", -60, 8),
		txn("schwab|1", "schwab", "ZELLE FROM PAT", 60, 9),
	}
	key := PairKeyFor("schwab|1", "usaa|1")
	decisions := map[string]Decision{key: {
		PairKey:   key,
		StableIDs: [2]string{"schwab|1", "usaa|1"},
		Verdict:   VerdictReject,
	}}

	res := Classify(in, nil, decisions)

	if len(res.Suspected) != 0 {
		t.Fatalf("Suspected = %d, want 0 -- a rejected pair is never re-suggested: %+v", len(res.Suspected), res.Suspected)
	}
	if res.Paired != 0 || res.External != 0 {
		t.Fatalf("Paired/External = %d/%d, want 0/0", res.Paired, res.External)
	}
}

func TestClassify_RejectedPairIsNotAutoPairedEither(t *testing.T) {
	// The user said these two are not a transfer. A pattern hit must not
	// override that.
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -2000, 4),
		txn("usaa|1", "usaa", "DEPOSIT FROM SCHWAB", 2000, 6),
	}
	key := PairKeyFor("schwab|1", "usaa|1")
	decisions := map[string]Decision{key: {
		PairKey: key, StableIDs: [2]string{"schwab|1", "usaa|1"}, Verdict: VerdictReject,
	}}

	res := Classify(in, nil, decisions)

	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0 -- a rejected pair must not auto-pair", res.Paired)
	}
	// The debit still matches a pattern, so it is a transfer with an
	// unknown counterparty rather than an expense.
	if res.External != 1 {
		t.Fatalf("External = %d, want 1", res.External)
	}
}

func TestClassify_DateWindowBoundary(t *testing.T) {
	cases := []struct {
		name       string
		creditDay  int
		wantPaired int
	}{
		{"four days apart pairs", 4 + WindowDays, 2},
		{"five days apart does not", 4 + WindowDays + 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := []models.Transaction{
				txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -300, 4),
				txn("usaa|1", "usaa", "DEPOSIT", 300, tc.creditDay),
			}
			res := Classify(in, nil, nil)
			if res.Paired != tc.wantPaired {
				t.Errorf("Paired = %d, want %d", res.Paired, tc.wantPaired)
			}
		})
	}
}

func TestClassify_ClosestDateWinsAmongSeveralCandidates(t *testing.T) {
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -400, 10),
		txn("usaa|1", "usaa", "DEPOSIT", 400, 13),
		txn("amex|1", "amex", "DEPOSIT", 400, 11),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 2 {
		t.Fatalf("Paired = %d, want 2", res.Paired)
	}
	rows := byID(t, res.Transactions)
	if rows["amex|1"].TransferClass != ClassPaired {
		t.Errorf("nearest candidate (amex|1, 1 day) should have paired, class = %q", rows["amex|1"].TransferClass)
	}
	if rows["usaa|1"].TransferClass == ClassPaired {
		t.Error("the farther candidate (usaa|1, 3 days) must not have paired")
	}
}

func TestClassify_SameSideAmountsDoNotPair(t *testing.T) {
	// Two debits of the same size in different accounts are not a
	// transfer; a transfer has one leg of each sign.
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -300, 4),
		txn("usaa|1", "usaa", "SCHWAB MONEYLINK TRANSFER", -300, 5),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 0 {
		t.Fatalf("Paired = %d, want 0 -- same-sign rows are not a transfer", res.Paired)
	}
	if res.External != 2 {
		t.Fatalf("External = %d, want 2 -- both still match a transfer pattern", res.External)
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	res := Classify(nil, nil, nil)
	if len(res.Transactions) != 0 || len(res.Suspected) != 0 || res.Classified() != 0 {
		t.Fatalf("empty input produced %+v", res)
	}
}

func TestPairKeyFor_OrderIndependentAndShort(t *testing.T) {
	a, b := PairKeyFor("x", "y"), PairKeyFor("y", "x")
	if a != b {
		t.Errorf("PairKeyFor is order dependent: %q vs %q", a, b)
	}
	if len(a) != 12 {
		t.Errorf("pair key length = %d, want 12", len(a))
	}
	if PairKeyFor("x", "y") == PairKeyFor("x", "z") {
		t.Error("different pairs produced the same key")
	}
}

// permutations returns every ordering of in, by value so the caller's slice
// is never aliased into more than one result.
func permutations(in []models.Transaction) [][]models.Transaction {
	if len(in) <= 1 {
		cp := make([]models.Transaction, len(in))
		copy(cp, in)
		return [][]models.Transaction{cp}
	}
	var out [][]models.Transaction
	for i := range in {
		rest := make([]models.Transaction, 0, len(in)-1)
		rest = append(rest, in[:i]...)
		rest = append(rest, in[i+1:]...)
		for _, p := range permutations(rest) {
			perm := append([]models.Transaction{in[i]}, p...)
			out = append(out, perm)
		}
	}
	return out
}

// abcCounterexample is the fixture from the transfer-pairing order-dependence
// defect: A is a pattern-hit debit with two same-side credits in its window,
// B four days out and C one day out. Closest-date says A pairs with C, B is
// left over. All rows share one magnitude ($100.00) so they land in the same
// byCents bucket, which is exactly the condition that let the old
// slice-order-first-claim bug misfire.
func abcCounterexample() []models.Transaction {
	return []models.Transaction{
		txn("chk|1", "chk", "INTERNAL TRANSFER TO SAVINGS", -100.00, 10),
		txn("sav|1", "sav", "DEPOSIT", 100.00, 14), // 4 days from A
		txn("sav|2", "sav", "DEPOSIT", 100.00, 11), // 1 day from A
	}
}

// resultFingerprint reduces a Result to the parts an order-independence check
// cares about: what every row was typed and keyed as, what the review queue
// holds, and the row tallies. Suspected is compared as a set (by pair key)
// because Suspected's own slice order is already independently pinned by the
// sort in Classify and is not what this fingerprint is checking.
type resultFingerprint struct {
	rows      map[string]string // StableID -> "type/class/pairKey"
	suspected map[string]string // PairKey -> reason
	paired    int
	external  int
}

func fingerprint(res Result) resultFingerprint {
	fp := resultFingerprint{
		rows:      make(map[string]string, len(res.Transactions)),
		suspected: make(map[string]string, len(res.Suspected)),
		paired:    res.Paired,
		external:  res.External,
	}
	for _, x := range res.Transactions {
		fp.rows[x.StableID] = string(x.TransactionType) + "/" + x.TransferClass + "/" + x.TransferPairKey
	}
	for _, s := range res.Suspected {
		fp.suspected[s.PairKey] = s.Reason
	}
	return fp
}

func TestClassify_ClosestDatePairingIsOrderIndependent(t *testing.T) {
	// A fourth, unrelated row rides along so the fixture exercises a
	// classify pass with something outside the pattern-gated component
	// too, without changing the pairing outcome for A/B/C.
	base := append(abcCounterexample(), txn("chk|2", "chk", "Wegmans", -22.10, 10))

	var want resultFingerprint
	for i, perm := range permutations(base) {
		res := Classify(perm, nil, nil)
		got := fingerprint(res)
		if i == 0 {
			want = got
			continue
		}
		if len(got.rows) != len(want.rows) {
			t.Fatalf("permutation %d: %d rows fingerprinted, want %d", i, len(got.rows), len(want.rows))
		}
		for id, w := range want.rows {
			if g := got.rows[id]; g != w {
				t.Errorf("permutation %d: row %s = %q, want %q (order-dependent result)", i, id, g, w)
			}
		}
		if len(got.suspected) != len(want.suspected) {
			t.Errorf("permutation %d: suspected = %v, want %v", i, got.suspected, want.suspected)
		}
		for key, w := range want.suspected {
			if g := got.suspected[key]; g != w {
				t.Errorf("permutation %d: suspected[%s] = %q, want %q", i, key, g, w)
			}
		}
		if got.paired != want.paired || got.external != want.external {
			t.Errorf("permutation %d: Paired/External = %d/%d, want %d/%d", i, got.paired, got.external, want.paired, want.external)
		}
	}
}

func TestClassify_ABCCounterexamplePairsClosestInEveryOrder(t *testing.T) {
	// Pinned to the specific counterexample that exposed the defect: A
	// must pair with C (one day away) in every input order, and B (four
	// days away) must never consume A first just because it happened to
	// be classified first.
	wantKey := PairKeyFor("chk|1", "sav|2")
	for i, perm := range permutations(abcCounterexample()) {
		res := Classify(perm, nil, nil)
		rows := byID(t, res.Transactions)

		a, b, c := rows["chk|1"], rows["sav|1"], rows["sav|2"]
		if a.TransferClass != ClassPaired || a.TransferPairKey != wantKey {
			t.Errorf("permutation %d: A = class %q key %q, want %q/%q", i, a.TransferClass, a.TransferPairKey, ClassPaired, wantKey)
		}
		if c.TransferClass != ClassPaired || c.TransferPairKey != wantKey {
			t.Errorf("permutation %d: C = class %q key %q, want %q/%q", i, c.TransferClass, c.TransferPairKey, ClassPaired, wantKey)
		}
		if b.TransferClass == ClassPaired || b.TransferPairKey != "" {
			t.Errorf("permutation %d: B was consumed (class %q key %q), want unpaired", i, b.TransferClass, b.TransferPairKey)
		}
		if res.Paired != 2 {
			t.Errorf("permutation %d: Paired = %d, want 2 (A and C only)", i, res.Paired)
		}
	}
}

func TestClassify_PermutationsNeverDropOrDuplicateRows(t *testing.T) {
	base := append(abcCounterexample(), txn("chk|2", "chk", "Wegmans", -22.10, 10))
	wantIDs := make(map[string]bool, len(base))
	for _, x := range base {
		wantIDs[x.StableID] = true
	}

	for i, perm := range permutations(base) {
		res := Classify(perm, nil, nil)
		if len(res.Transactions) != len(base) {
			t.Fatalf("permutation %d: Classify returned %d rows, want %d", i, len(res.Transactions), len(base))
		}
		seen := make(map[string]bool, len(base))
		for _, x := range res.Transactions {
			if seen[x.StableID] {
				t.Errorf("permutation %d: row %s appeared more than once", i, x.StableID)
			}
			seen[x.StableID] = true
			if !wantIDs[x.StableID] {
				t.Errorf("permutation %d: unexpected row %s in output", i, x.StableID)
			}
		}
		for id := range wantIDs {
			if !seen[id] {
				t.Errorf("permutation %d: row %s missing from output", i, id)
			}
		}
	}
}

func TestClassify_DoesNotMutateInput(t *testing.T) {
	in := []models.Transaction{
		txn("schwab|1", "schwab", "SCHWAB MONEYLINK TRANSFER", -2000, 4),
		txn("usaa|1", "usaa", "DEPOSIT FROM SCHWAB", 2000, 6),
	}

	res := Classify(in, nil, nil)

	if res.Paired != 2 {
		t.Fatalf("Paired = %d, want 2", res.Paired)
	}
	for _, x := range in {
		if x.TransactionType != "" || x.TransferClass != "" || x.TransferPairKey != "" {
			t.Errorf("Classify mutated its input row %s: %+v", x.StableID, x)
		}
	}
}
