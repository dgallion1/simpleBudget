package majorexpenses

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func tx(date time.Time, amount float64, desc, displayName string, tt models.TransactionType) models.Transaction {
	return models.Transaction{
		Date:            date,
		Amount:          amount,
		Description:     desc,
		DisplayName:     displayName,
		TransactionType: tt,
	}
}

func TestMatchTransaction_KeywordSubstringCaseInsensitive(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "rent", Keywords: []string{"LANDLORD"}},
	}
	tr := tx(time.Now(), -2000, "ACH from My Landlord LLC", "", models.Outflow)

	id, ok := MatchTransaction(tr, defs)
	if !ok || id != "rent" {
		t.Errorf("expected match on rent, got %q ok=%v", id, ok)
	}
}

func TestMatchTransaction_FirstDefinitionWins(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "first", Keywords: []string{"chase"}},
		{ID: "second", Keywords: []string{"chase"}},
	}
	tr := tx(time.Now(), -100, "CHASE CARD PAYMENT", "", models.Outflow)

	id, _ := MatchTransaction(tr, defs)
	if id != "first" {
		t.Errorf("expected first def to win, got %q", id)
	}
}

func TestMatchTransaction_DisplayNameMatches(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "gym", Keywords: []string{"planet fitness"}},
	}
	tr := tx(time.Now(), -10, "ACH 12345", "Planet Fitness Membership", models.Outflow)

	id, ok := MatchTransaction(tr, defs)
	if !ok || id != "gym" {
		t.Errorf("expected match via DisplayName, got %q ok=%v", id, ok)
	}
}

func TestMatchTransaction_EmptyKeywordsIgnored(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "x", Keywords: []string{"", "  "}},
	}
	tr := tx(time.Now(), -100, "anything goes here", "", models.Outflow)

	if _, ok := MatchTransaction(tr, defs); ok {
		t.Error("empty keywords should not produce a match")
	}
}

func TestMatchTransaction_AmountOnlyWhenNoKeywords(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "car", Name: "Car Payment", ExpectedMin: 620, ExpectedMax: 630},
	}
	hit := tx(time.Now(), -625, "Check #996562", "", models.Outflow)
	miss := tx(time.Now(), -700, "Check #996563", "", models.Outflow)

	if id, ok := MatchTransaction(hit, defs); !ok || id != "car" {
		t.Errorf("expected amount-only match for $625, got id=%q ok=%v", id, ok)
	}
	if _, ok := MatchTransaction(miss, defs); ok {
		t.Error("$700 should not match amount-only def of $620–$630")
	}
}

func TestMatchTransaction_RangeIgnoredWhenKeywordPresent(t *testing.T) {
	// A def with keywords AND a true range (min < max) only matches via
	// keyword — the range is reserved for anomaly detection.
	defs := []models.MajorExpense{
		{ID: "x", Name: "X", Keywords: []string{"groceries"}, ExpectedMin: 620, ExpectedMax: 630},
	}
	tr := tx(time.Now(), -625, "Check #996562", "", models.Outflow)
	if _, ok := MatchTransaction(tr, defs); ok {
		t.Error("range with keyword should not match (range is anomaly-only when keyword present)")
	}
}

func TestMatchTransaction_KeywordPlusExactAmountIsAndFilter(t *testing.T) {
	// AND semantics: keyword AND exact amount must BOTH match.
	defs := []models.MajorExpense{
		{ID: "lucid", Name: "Lucid", Keywords: []string{"lucid"}, ExpectedMin: 1580, ExpectedMax: 1580},
	}
	matchBoth := tx(time.Now(), -1580, "LUCID MOTORS SUBSCRIPTION", "", models.Outflow)
	keywordOnly := tx(time.Now(), -50, "Lucid Coffee", "", models.Outflow)        // keyword yes, amount no
	amountOnly := tx(time.Now(), -1580, "Random vendor", "", models.Outflow)      // amount yes, keyword no
	checkOfRightAmount := tx(time.Now(), -1580, "Check #2358", "", models.Outflow) // amount yes, keyword no

	if _, ok := MatchTransaction(matchBoth, defs); !ok {
		t.Error("expected match when both keyword and amount match")
	}
	if _, ok := MatchTransaction(keywordOnly, defs); ok {
		t.Error("keyword alone should NOT match when an exact amount is specified")
	}
	if _, ok := MatchTransaction(amountOnly, defs); ok {
		t.Error("amount alone should NOT match when a keyword is specified")
	}
	if _, ok := MatchTransaction(checkOfRightAmount, defs); ok {
		t.Error("check of right amount but no keyword match should NOT match")
	}
}

func TestMatchTransaction_DisambiguateByAmountWithSharedKeyword(t *testing.T) {
	// User's exact scenario: two checks (Lucid and Car) both match the
	// keyword "check", disambiguated by amount.
	defs := []models.MajorExpense{
		{ID: "lucid", Name: "Lucid", Keywords: []string{"check"}, ExpectedMin: 1580, ExpectedMax: 1580},
		{ID: "car", Name: "Car", Keywords: []string{"check"}, ExpectedMin: 626, ExpectedMax: 626},
	}
	lucidCheck := tx(time.Now(), -1580, "Check #2358", "", models.Outflow)
	carCheck := tx(time.Now(), -626, "Check #1111", "", models.Outflow)
	otherCheck := tx(time.Now(), -100, "Check #9999", "", models.Outflow)

	if id, _ := MatchTransaction(lucidCheck, defs); id != "lucid" {
		t.Errorf("$1580 check should map to lucid, got %q", id)
	}
	if id, _ := MatchTransaction(carCheck, defs); id != "car" {
		t.Errorf("$626 check should map to car, got %q", id)
	}
	if _, ok := MatchTransaction(otherCheck, defs); ok {
		t.Error("$100 check should not match either def")
	}
}

func TestMatchTransaction_KeywordAloneWithRangeStillKeywordOnly(t *testing.T) {
	// Range with keyword: range is anomaly-only, keyword is the matcher.
	defs := []models.MajorExpense{
		{ID: "rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000},
	}
	hit := tx(time.Now(), -3000, "MY LANDLORD INC", "", models.Outflow) // keyword yes, amount out of range
	miss := tx(time.Now(), -1700, "Random", "", models.Outflow)         // amount in range, no keyword

	if _, ok := MatchTransaction(hit, defs); !ok {
		t.Error("keyword should match even when amount is outside the anomaly range")
	}
	if _, ok := MatchTransaction(miss, defs); ok {
		t.Error("range with keyword should not match by amount alone")
	}
}

func TestMatchTransaction_ExactAmountTolerance(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "x", ExpectedMin: 1580.00, ExpectedMax: 1580.00},
	}
	// Within ±$0.01 → match (float precision tolerance)
	if _, ok := MatchTransaction(tx(time.Now(), -1580.005, "x", "", models.Outflow), defs); !ok {
		t.Error("should match within float-precision tolerance")
	}
	// $0.35 outside → no match (real-money difference)
	if _, ok := MatchTransaction(tx(time.Now(), -1580.35, "x", "", models.Outflow), defs); ok {
		t.Error("$0.35 difference should not match exact amount")
	}
}

func TestMatchTransaction_AmountOnlyRequiresBothBounds(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "x", Name: "X", ExpectedMin: 600, ExpectedMax: 0},
	}
	tr := tx(time.Now(), -625, "Check #1", "", models.Outflow)
	if _, ok := MatchTransaction(tr, defs); ok {
		t.Error("amount-only matching requires both min and max > 0")
	}
}

func TestDetectAnomalies_AmountOnlyDefsHaveNoAnomalies(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "car", Name: "Car", ExpectedMin: 620, ExpectedMax: 630},
	}
	groups := map[string][]models.Transaction{
		"car": {tx(time.Now(), -625, "Check #1", "", models.Outflow)},
	}
	if out := detectAnomalies(groups, defs, nil); len(out) != 0 {
		t.Errorf("amount-only def should produce no anomalies, got %+v", out)
	}
}

func TestMatchTransaction_NoMatch(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "x", Keywords: []string{"verizon"}},
	}
	tr := tx(time.Now(), -100, "starbucks", "", models.Outflow)

	if _, ok := MatchTransaction(tr, defs); ok {
		t.Error("expected no match")
	}
}

func TestDetectAnomalies_BelowMin(t *testing.T) {
	defs := []models.MajorExpense{{ID: "rent", Name: "Rent", Keywords: []string{"rent"}, ExpectedMin: 1500, ExpectedMax: 2000}}
	groups := map[string][]models.Transaction{
		"rent": {tx(time.Now(), -1000, "rent", "", models.Outflow)},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 1 || out[0].MajorExpenseID != "rent" {
		t.Errorf("expected 1 below-min anomaly, got %+v", out)
	}
}

func TestDetectAnomalies_AboveMax(t *testing.T) {
	defs := []models.MajorExpense{{ID: "rent", Keywords: []string{"rent"}, ExpectedMin: 1500, ExpectedMax: 2000}}
	groups := map[string][]models.Transaction{
		"rent": {tx(time.Now(), -3000, "rent", "", models.Outflow)},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 1 {
		t.Errorf("expected 1 above-max anomaly, got %d", len(out))
	}
}

func TestDetectAnomalies_BothBoundsZeroSkipsCheck(t *testing.T) {
	defs := []models.MajorExpense{{ID: "x", ExpectedMin: 0, ExpectedMax: 0}}
	groups := map[string][]models.Transaction{
		"x": {tx(time.Now(), -99999, "x", "", models.Outflow)},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 0 {
		t.Errorf("expected no anomalies when both bounds are 0, got %d", len(out))
	}
}

func TestDetectAnomalies_OnlyMinSet(t *testing.T) {
	defs := []models.MajorExpense{{ID: "x", Keywords: []string{"x"}, ExpectedMin: 100, ExpectedMax: 0}}
	groups := map[string][]models.Transaction{
		"x": {
			tx(time.Now(), -50, "x", "", models.Outflow),     // below → flag
			tx(time.Now(), -999999, "x", "", models.Outflow), // above ignored (no max)
		},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 1 {
		t.Errorf("expected 1 below-min anomaly, got %d", len(out))
	}
}

func TestDetectAnomalies_OnlyMaxSet(t *testing.T) {
	defs := []models.MajorExpense{{ID: "x", Keywords: []string{"x"}, ExpectedMin: 0, ExpectedMax: 100}}
	groups := map[string][]models.Transaction{
		"x": {
			tx(time.Now(), -50, "x", "", models.Outflow),  // below ignored (no min)
			tx(time.Now(), -150, "x", "", models.Outflow), // above → flag
		},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 1 {
		t.Errorf("expected 1 above-max anomaly, got %d", len(out))
	}
}

func TestDetectAnomalies_DeterministicOrder(t *testing.T) {
	// Defs in fixed order, multiple groups each with anomalies; output must
	// honor def order.
	defs := []models.MajorExpense{
		{ID: "a", Name: "A", Keywords: []string{"a"}, ExpectedMax: 10},
		{ID: "b", Name: "B", Keywords: []string{"b"}, ExpectedMax: 10},
	}
	groups := map[string][]models.Transaction{
		"b": {tx(time.Now(), -100, "b", "", models.Outflow)},
		"a": {tx(time.Now(), -100, "a", "", models.Outflow)},
	}
	out := detectAnomalies(groups, defs, nil)
	if len(out) != 2 || out[0].MajorExpenseID != "a" || out[1].MajorExpenseID != "b" {
		t.Errorf("expected order [a,b], got %+v", out)
	}
}

func TestDetectUnknownLarge_RespectsThreshold(t *testing.T) {
	now := time.Now()
	unmatched := []models.Transaction{
		tx(now, -50, "small", "", models.Outflow),
		tx(now, -150, "big", "", models.Outflow),
	}
	out := detectUnknownLarge(unmatched, 100)
	if len(out) != 1 || out[0].Transaction.Description != "big" {
		t.Errorf("expected only 'big' to be flagged, got %+v", out)
	}
}

func TestDetectUnknownLarge_IncomeIgnored(t *testing.T) {
	now := time.Now()
	unmatched := []models.Transaction{
		tx(now, 5000, "paycheck", "", models.Income),
	}
	out := detectUnknownLarge(unmatched, 100)
	if len(out) != 0 {
		t.Error("income should not be flagged as unknown-large expense")
	}
}

func TestDetectUnknownLarge_ZeroThresholdDisables(t *testing.T) {
	out := detectUnknownLarge([]models.Transaction{
		tx(time.Now(), -1_000_000, "x", "", models.Outflow),
	}, 0)
	if len(out) != 0 {
		t.Error("threshold <= 0 should disable check")
	}
}

func TestDetectNewMerchants_FirstSeenInWindow(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	mid := now.Add(-10 * 24 * time.Hour)

	ts := models.NewTransactionSet([]models.Transaction{
		tx(old, -100, "Old Corp", "", models.Outflow),
		tx(mid, -25, "New Cafe", "", models.Outflow),
		tx(now, -75, "New Cafe", "", models.Outflow), // dup, should be deduped
		tx(now, -200, "Another New", "", models.Outflow),
	})

	out := detectNewMerchants(ts, 30*24*time.Hour)
	if len(out) != 2 {
		t.Fatalf("expected 2 unique new merchants, got %d: %+v", len(out), out)
	}
	// Sorted by date ascending
	if out[0].Description != "new cafe" {
		t.Errorf("expected 'new cafe' first (earlier date), got %q", out[0].Description)
	}
	if !out[0].FirstSeen.Equal(mid) {
		t.Errorf("expected FirstSeen = mid, got %v", out[0].FirstSeen)
	}
}

func TestDetectNewMerchants_SeenBeforeCutoffNotEmitted(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-5 * 24 * time.Hour)

	ts := models.NewTransactionSet([]models.Transaction{
		tx(old, -100, "Costco", "", models.Outflow),
		tx(recent, -100, "Costco", "", models.Outflow), // same merchant, NOT new
	})

	out := detectNewMerchants(ts, 30*24*time.Hour)
	if len(out) != 0 {
		t.Errorf("expected no new merchants (Costco was seen before cutoff), got %+v", out)
	}
}

func TestDetectNewMerchants_DescriptionNormalization(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-5 * 24 * time.Hour)

	ts := models.NewTransactionSet([]models.Transaction{
		tx(old, -100, "STARBUCKS  COFFEE", "", models.Outflow),
		tx(recent, -100, "  starbucks coffee  ", "", models.Outflow),
	})

	out := detectNewMerchants(ts, 30*24*time.Hour)
	if len(out) != 0 {
		t.Errorf("normalization should match different casings/whitespace; got %+v", out)
	}
}

func TestDetectNewMerchants_EmptySetReturnsNil(t *testing.T) {
	if out := detectNewMerchants(nil, 30*24*time.Hour); out != nil {
		t.Errorf("nil ts should return nil, got %+v", out)
	}
	ts := models.NewTransactionSet(nil)
	if out := detectNewMerchants(ts, 30*24*time.Hour); out != nil {
		t.Errorf("empty ts should return nil, got %+v", out)
	}
}

func TestDetectNewMerchants_IgnoresIncome(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := models.NewTransactionSet([]models.Transaction{
		tx(now, 5000, "Paycheck Acme Corp", "", models.Income),
	})
	out := detectNewMerchants(ts, 30*24*time.Hour)
	if len(out) != 0 {
		t.Errorf("income should not be flagged as new merchant, got %+v", out)
	}
}

func TestDetectNewMerchants_ZeroWindowDisables(t *testing.T) {
	now := time.Now()
	ts := models.NewTransactionSet([]models.Transaction{tx(now, -100, "x", "", models.Outflow)})
	if out := detectNewMerchants(ts, 0); out != nil {
		t.Errorf("zero window should disable, got %+v", out)
	}
}

func TestMatch_PinOverridesKeywordMatch(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "amazon-default", Keywords: []string{"amazon"}},
		{ID: "amazon-books"},
		{ID: "amazon-household"},
	}
	tr := models.Transaction{Date: time.Now(), Amount: -50, Description: "Amazon order", Hash: "h1", TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{tr})

	res := Match(ts, defs, MatchOptions{Pins: map[string]string{"h1": "amazon-books"}})

	if got := len(res.Groups["amazon-books"]); got != 1 {
		t.Errorf("expected pinned txn in amazon-books, got %d", got)
	}
	if got := len(res.Groups["amazon-default"]); got != 0 {
		t.Errorf("keyword match should be overridden by pin, got %d", got)
	}
	if !res.PinnedHashes["h1"] {
		t.Error("expected hash to be marked as pinned")
	}
}

func TestMatch_PinFallsBackWhenTargetMissing(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "amazon", Keywords: []string{"amazon"}},
	}
	tr := models.Transaction{Date: time.Now(), Amount: -50, Description: "Amazon order", Hash: "h1", TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{tr})

	// Pin points to an expense that no longer exists. Should fall back
	// to keyword/amount matching.
	res := Match(ts, defs, MatchOptions{Pins: map[string]string{"h1": "deleted-expense"}})

	if got := len(res.Groups["amazon"]); got != 1 {
		t.Errorf("expected fallback to keyword match, got %d", got)
	}
	if res.PinnedHashes["h1"] {
		t.Error("orphan pin should not mark hash as pinned")
	}
}

func TestMatch_PinSuppressesAnomalyFlag(t *testing.T) {
	// User pinned a $3000 transaction to a Rent expense expecting $1500-$2000.
	// Without the pin, this would be flagged as an anomaly. With the pin,
	// the user has explicitly accepted it — anomaly should NOT fire.
	defs := []models.MajorExpense{
		{ID: "rent", Name: "Rent", Keywords: []string{"unrelated"}, ExpectedMin: 1500, ExpectedMax: 2000},
	}
	pinned := models.Transaction{Date: time.Now(), Amount: -3000, Description: "Anything", Hash: "h1", TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{pinned})

	res := Match(ts, defs, MatchOptions{Pins: map[string]string{"h1": "rent"}})

	if got := len(res.Groups["rent"]); got != 1 {
		t.Fatalf("pinned txn should be in rent group, got %d", got)
	}
	if got := len(res.Exceptions.Anomalous); got != 0 {
		t.Errorf("pinned txn should NOT be flagged as anomalous, got %d: %+v", got, res.Exceptions.Anomalous)
	}
}

func TestMatch_PinIgnoredWhenHashEmpty(t *testing.T) {
	defs := []models.MajorExpense{{ID: "x"}}
	tr := models.Transaction{Date: time.Now(), Amount: -50, Hash: "", TransactionType: models.Outflow}
	ts := models.NewTransactionSet([]models.Transaction{tr})

	res := Match(ts, defs, MatchOptions{Pins: map[string]string{"": "x"}})
	if got := len(res.Unmatched); got != 1 {
		t.Errorf("empty-hash transaction should not pin, expected unmatched, got groups %+v", res.Groups)
	}
}

func TestAnnotateRecurringPayments_PinWins(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "kw", Name: "Keyword Match", Keywords: []string{"netflix"}},
		{ID: "pin", Name: "Pinned Match"},
	}
	pinned := models.Transaction{Hash: "h1", Description: "NETFLIX SUBSCRIPTION", Amount: -15}
	in := []models.RecurringPayment{{Description: "netflix", Transactions: []models.Transaction{pinned}}}

	out := AnnotateRecurringPayments(in, defs, map[string]string{"h1": "pin"})
	if got := out[0].MajorExpenseName; got != "Pinned Match" {
		t.Errorf("expected pin to win, got %q", got)
	}
}

func TestAnnotateRecurringPayments_FallsBackToKeyword(t *testing.T) {
	defs := []models.MajorExpense{
		{ID: "kw", Name: "Keyword Match", Keywords: []string{"netflix"}},
	}
	tr := models.Transaction{Hash: "h1", Description: "NETFLIX SUBSCRIPTION", Amount: -15}
	in := []models.RecurringPayment{{Description: "netflix", Transactions: []models.Transaction{tr}}}

	out := AnnotateRecurringPayments(in, defs, nil)
	if got := out[0].MajorExpenseName; got != "Keyword Match" {
		t.Errorf("expected keyword fallback, got %q", got)
	}
}

func TestAnnotateRecurringPayments_NoMatchYieldsEmpty(t *testing.T) {
	defs := []models.MajorExpense{{ID: "rent", Keywords: []string{"landlord"}}}
	tr := models.Transaction{Hash: "h1", Description: "Random Coffee Shop", Amount: -5}
	in := []models.RecurringPayment{{Description: "coffee", Transactions: []models.Transaction{tr}}}

	out := AnnotateRecurringPayments(in, defs, nil)
	if got := out[0].MajorExpenseName; got != "" {
		t.Errorf("expected empty annotation, got %q", got)
	}
}

func TestAnnotateRecurringPayments_OrphanPinIgnored(t *testing.T) {
	defs := []models.MajorExpense{{ID: "kw", Name: "KW", Keywords: []string{"netflix"}}}
	tr := models.Transaction{Hash: "h1", Description: "NETFLIX", Amount: -15}
	in := []models.RecurringPayment{{Description: "netflix", Transactions: []models.Transaction{tr}}}

	// Pin points to a deleted expense; should fall through to keyword.
	out := AnnotateRecurringPayments(in, defs, map[string]string{"h1": "deleted"})
	if got := out[0].MajorExpenseName; got != "KW" {
		t.Errorf("expected fallback to keyword when pin orphaned, got %q", got)
	}
}

func TestAnnotateRecurringPayments_EmptyInput(t *testing.T) {
	if out := AnnotateRecurringPayments(nil, nil, nil); out != nil {
		t.Errorf("nil input should return nil, got %+v", out)
	}
	if out := AnnotateRecurringPayments([]models.RecurringPayment{}, nil, nil); len(out) != 0 {
		t.Errorf("empty input should return empty, got %+v", out)
	}
}

func TestAnnotateRecurringPayments_SkipsEmptyTransactions(t *testing.T) {
	defs := []models.MajorExpense{{ID: "x", Name: "X", Keywords: []string{"foo"}}}
	in := []models.RecurringPayment{{Description: "foo"}} // no Transactions slice

	out := AnnotateRecurringPayments(in, defs, nil)
	if got := out[0].MajorExpenseName; got != "" {
		t.Errorf("entry without transactions should not be annotated, got %q", got)
	}
}

func TestMatch_Integration(t *testing.T) {
	now := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)

	defs := []models.MajorExpense{
		{ID: "rent", Name: "Rent", Keywords: []string{"landlord"}, ExpectedMin: 1500, ExpectedMax: 2000},
	}

	ts := models.NewTransactionSet([]models.Transaction{
		tx(old, -1700, "Landlord LLC", "", models.Outflow),    // matched, in range
		tx(now, -3000, "Landlord LLC", "", models.Outflow),    // matched, anomalous
		tx(now, -250, "Random Merchant", "", models.Outflow),  // unknown large
		tx(now, -10, "tiny coffee", "", models.Outflow),       // unmatched but small
		tx(old, -50, "tiny coffee", "", models.Outflow),       // makes coffee not new
		tx(now, -500, "Brand New Store", "", models.Outflow),  // new merchant + unknown large
		tx(now, 5000, "Paycheck", "", models.Income),          // income, ignored for unknown-large
	})

	res := Match(ts, defs, MatchOptions{
		UnknownLargeThreshold: 100,
		NewMerchantWindow:     30 * 24 * time.Hour,
	})

	if got := len(res.Groups["rent"]); got != 2 {
		t.Errorf("expected 2 transactions in rent group, got %d", got)
	}
	if got := len(res.Exceptions.Anomalous); got != 1 {
		t.Errorf("expected 1 anomalous, got %d", got)
	}
	if got := len(res.Exceptions.UnknownLarge); got != 2 {
		t.Errorf("expected 2 unknown-large (Random Merchant + Brand New Store), got %d", got)
	}
	if got := len(res.Exceptions.NewMerchants); got != 2 {
		t.Errorf("expected 2 new merchants (random merchant + brand new store), got %d: %+v", got, res.Exceptions.NewMerchants)
	}
	for _, nm := range res.Exceptions.NewMerchants {
		if nm.Description == "paycheck" {
			t.Error("income (paycheck) should not appear in new merchants")
		}
	}
	if res.Exceptions.Threshold != 100 || res.Exceptions.NewWindowDays != 30 {
		t.Errorf("expected threshold=100, window_days=30; got %+v", res.Exceptions)
	}
}

func TestMatch_NilTransactionSet(t *testing.T) {
	res := Match(nil, nil, MatchOptions{})
	if len(res.Groups) != 0 || len(res.Unmatched) != 0 {
		t.Errorf("nil ts should yield empty result")
	}
}

func TestNormalizeDescription(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"  ":             "",
		"Hello":          "hello",
		"  HELLO  WORLD ": "hello world",
		"foo\tbar  baz":  "foo bar baz",
	}
	for in, want := range cases {
		if got := normalizeDescription(in); got != want {
			t.Errorf("normalizeDescription(%q) = %q, want %q", in, got, want)
		}
	}
}
