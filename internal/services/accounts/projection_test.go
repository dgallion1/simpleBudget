package accounts

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

// rp builds a RecurringPayment for use as a stubbed recurring input. The
// projection must NOT depend on real detection heuristics, so tests stub the
// engine's output directly and assert projection maths, not the engine's.
//
// nextExpected is the scheduled future date the projection walks forward from;
// txns is the set of historical legs (only their AccountID matters here, used
// to filter the item to this account).
func rp(desc string, amount float64, frequency string, nextExpected time.Time, txns ...models.Transaction) models.RecurringPayment {
	return models.RecurringPayment{
		Description:  desc,
		Amount:       amount,
		Frequency:    frequency,
		NextExpected: nextExpected,
		Transactions: txns,
	}
}

// leg is a shorthand for a recurring historical leg belonging to acctID.
func leg(acctID string, when time.Time) models.Transaction {
	return models.Transaction{AccountID: acctID, Date: when, Amount: -1.0}
}

// inboundTransfer builds a confirmed inbound paired-transfer leg into acctID.
func inboundTransfer(acctID string, when time.Time, amount float64) models.Transaction {
	return models.Transaction{
		AccountID:       acctID,
		Date:            when,
		Amount:          amount,
		TransactionType: models.Transfer,
		TransferClass:   "paired",
	}
}

// TestProject_CrossingReportsFirstCrossingAndMinimum is the core happy path:
// a starting balance that crosses below the threshold within the 35-day
// window. The first crossing date and the minimum projected balance are
// reported, and the suggested top-up is the shortfall rounded up to the
// nearest $100.
func TestProject_CrossingReportsFirstCrossingAndMinimum(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	// A monthly $400 outflow due on 2026-09-01. Starting 800 - 400 = 400,
	// which is below the 500 threshold. The balance crosses on 2026-09-01.
	monthly := rp("rent", 400.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))
	txs := []models.Transaction{}

	got, err := Project(acct, txs, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.Available {
		t.Fatal("Available = false, want true (anchor exists at or before asOf)")
	}
	if !got.Crossing.Equal(mustDate(2026, 9, 1)) {
		t.Errorf("Crossing = %v, want 2026-09-01 (first day below threshold)", got.Crossing)
	}
	// Minimum is 400 (only one outflow in the 35-day window from 8/15).
	if !moneyEq(got.Minimum, 400.00) {
		t.Errorf("Minimum = %.4f, want 400.00", got.Minimum)
	}
	// Shortfall = 500 - 400 = 100, rounded up to nearest 100 = 100.
	if !moneyEq(got.SuggestedTopUp, 100.00) {
		t.Errorf("SuggestedTopUp = %.4f, want 100.00", got.SuggestedTopUp)
	}
}

// TestProject_NoCrossingReportsHealthy: the balance never dips below the
// threshold in the window. Crossing is zero, SuggestedTopUp is zero, and the
// caller can distinguish "healthy" from "unavailable".
func TestProject_NoCrossingReportsHealthy(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 5000.00}},
		LowBalanceThreshold: 500.00,
	}
	// A small $50 monthly outflow; balance stays well above 500.
	monthly := rp("streaming", 50.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.Available {
		t.Fatal("Available = false, want true")
	}
	if !got.Crossing.IsZero() {
		t.Errorf("Crossing = %v, want zero (no crossing)", got.Crossing)
	}
	if !moneyEq(got.SuggestedTopUp, 0) {
		t.Errorf("SuggestedTopUp = %.4f, want 0 (no shortfall)", got.SuggestedTopUp)
	}
	// Minimum is 5000 - 50 = 4950.
	if !moneyEq(got.Minimum, 4950.00) {
		t.Errorf("Minimum = %.4f, want 4950.00", got.Minimum)
	}
}

// TestProject_NoAnchorIsUnavailableNotZero pins the contract: with no anchor
// at or before asOf, the projection is unavailable -- NOT a projection from
// zero. An unknown balance and a zero balance are different facts and the UI
// renders them differently.
func TestProject_NoAnchorIsUnavailableNotZero(t *testing.T) {
	acct := models.Account{ID: "usaa", Name: "usaa"} // no anchors
	monthly := rp("rent", 400.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.Available {
		t.Error("Available = true, want false (no anchor at or before asOf)")
	}
	if !got.Crossing.IsZero() {
		t.Errorf("Crossing = %v, want zero when unavailable", got.Crossing)
	}
	if !moneyEq(got.Minimum, 0) {
		t.Errorf("Minimum = %.4f, want 0 when unavailable", got.Minimum)
	}
	if !moneyEq(got.SuggestedTopUp, 0) {
		t.Errorf("SuggestedTopUp = %.4f, want 0 when unavailable", got.SuggestedTopUp)
	}
	if got.HasReference {
		t.Error("HasReference = true, want false when unavailable")
	}
}

// TestProject_DefaultThresholdWhenAccountThresholdZero: when the account's
// LowBalanceThreshold is zero, the projection uses
// DefaultLowBalanceThreshold (500). A balance that dips just below 500
// crosses; the reported Threshold is 500 so the UI can label its line.
func TestProject_DefaultThresholdWhenAccountThresholdZero(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		// LowBalanceThreshold zero -> default 500 applies.
		Anchors: []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 700.00}},
	}
	// Monthly $300 outflow on 9/1: 700 - 300 = 400, below the default 500.
	monthly := rp("rent", 300.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !moneyEq(got.Threshold, DefaultLowBalanceThreshold) {
		t.Errorf("Threshold = %.4f, want default %.4f", got.Threshold, DefaultLowBalanceThreshold)
	}
	if !got.Crossing.Equal(mustDate(2026, 9, 1)) {
		t.Errorf("Crossing = %v, want 2026-09-01 (400 < default 500)", got.Crossing)
	}
}

// TestProject_OverrideThresholdRespected: a non-zero account threshold is
// used as-is, and a balance between the default and the override does NOT
// cross.
func TestProject_OverrideThresholdRespected(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 1000.00}},
		LowBalanceThreshold: 900.00, // higher than the default 500
	}
	// Monthly $150 outflow on 9/1: 1000 - 150 = 850, below the 900 override
	// but above the default 500. The default would miss this; the override
	// must catch it.
	monthly := rp("rent", 150.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !moneyEq(got.Threshold, 900.00) {
		t.Errorf("Threshold = %.4f, want 900.00 (override)", got.Threshold)
	}
	if !got.Crossing.Equal(mustDate(2026, 9, 1)) {
		t.Errorf("Crossing = %v, want 2026-09-01 (850 < override 900)", got.Crossing)
	}
}

// TestProject_TopUpRoundingAtExactBoundary: a shortfall that is an exact
// multiple of $100 rounds up to itself (ceil of an exact multiple is itself).
// Shortfall = 500 - 400 = 100 -> top-up 100.
func TestProject_TopUpRoundingAtExactBoundary(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	// $400 outflow -> minimum 400 -> shortfall exactly 100 -> top-up 100.
	monthly := rp("rent", 400.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !moneyEq(got.SuggestedTopUp, 100.00) {
		t.Errorf("SuggestedTopUp = %.4f, want 100.00 (exact multiple, ceil is itself)", got.SuggestedTopUp)
	}
}

// TestProject_TopUpRoundingJustPastBoundary: a shortfall just above a $100
// multiple rounds up to the next $100. Shortfall = 500 - 350 = 150 -> top-up
// 200.
func TestProject_TopUpRoundingJustPastBoundary(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	// $450 outflow -> minimum 350 -> shortfall 150 -> ceil(1.5)*100 = 200.
	monthly := rp("rent", 450.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !moneyEq(got.SuggestedTopUp, 200.00) {
		t.Errorf("SuggestedTopUp = %.4f, want 200.00 (150 just past 100, rounds up)", got.SuggestedTopUp)
	}
}

// TestProject_ReferenceMedianOddCount: with three prior inbound paired
// transfers, the reference amount is the middle (median) value.
func TestProject_ReferenceMedianOddCount(t *testing.T) {
	acct := models.Account{
		ID:      "usaa",
		Name:    "usaa",
		Anchors: []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 10000.00}},
	}
	txs := []models.Transaction{
		inboundTransfer("usaa", mustDate(2026, 5, 1), 1000.00),
		inboundTransfer("usaa", mustDate(2026, 6, 1), 2000.00),
		inboundTransfer("usaa", mustDate(2026, 7, 1), 3000.00),
	}

	got, err := Project(acct, txs, mustDate(2026, 8, 15), nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.HasReference {
		t.Fatal("HasReference = false, want true (3 prior inbound transfers)")
	}
	if !moneyEq(got.ReferenceAmount, 2000.00) {
		t.Errorf("ReferenceAmount = %.4f, want 2000.00 (median of 1000/2000/3000)", got.ReferenceAmount)
	}
}

// TestProject_ReferenceMedianEvenCount: with four prior inbound paired
// transfers, the reference amount is the mean of the two middle values.
func TestProject_ReferenceMedianEvenCount(t *testing.T) {
	acct := models.Account{
		ID:      "usaa",
		Name:    "usaa",
		Anchors: []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 10000.00}},
	}
	txs := []models.Transaction{
		inboundTransfer("usaa", mustDate(2026, 4, 1), 1000.00),
		inboundTransfer("usaa", mustDate(2026, 5, 1), 2000.00),
		inboundTransfer("usaa", mustDate(2026, 6, 1), 2500.00),
		inboundTransfer("usaa", mustDate(2026, 7, 1), 3000.00),
	}

	got, err := Project(acct, txs, mustDate(2026, 8, 15), nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.HasReference {
		t.Fatal("HasReference = false, want true (4 prior inbound transfers)")
	}
	// Median of {1000,2000,2500,3000} = (2000+2500)/2 = 2250.
	if !moneyEq(got.ReferenceAmount, 2250.00) {
		t.Errorf("ReferenceAmount = %.4f, want 2250.00 (mean of two middle)", got.ReferenceAmount)
	}
}

// TestProject_ReferenceNoPriorTransfers: with no confirmed inbound paired
// transfers, HasReference is false and ReferenceAmount is zero -- the caller
// reports "no prior transfers" rather than inventing a number.
func TestProject_ReferenceNoPriorTransfers(t *testing.T) {
	acct := models.Account{
		ID:      "usaa",
		Name:    "usaa",
		Anchors: []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 10000.00}},
	}
	// Outbound transfers and external transfers and other accounts' transfers
	// do NOT count as inbound paired history.
	txs := []models.Transaction{
		// outbound (amount negative) paired leg -- not inbound.
		{AccountID: "usaa", Date: mustDate(2026, 6, 1), Amount: -2000.00,
			TransactionType: models.Transfer, TransferClass: "paired"},
		// external -- not paired.
		{AccountID: "usaa", Date: mustDate(2026, 6, 2), Amount: 2000.00,
			TransactionType: models.Transfer, TransferClass: "external"},
		// someone else's paired inbound -- not this account.
		{AccountID: "schwab", Date: mustDate(2026, 6, 3), Amount: 2000.00,
			TransactionType: models.Transfer, TransferClass: "paired"},
		// a non-transfer income row -- not a transfer at all.
		{AccountID: "usaa", Date: mustDate(2026, 6, 4), Amount: 2000.00,
			TransactionType: models.Income},
	}

	got, err := Project(acct, txs, mustDate(2026, 8, 15), nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got.HasReference {
		t.Error("HasReference = true, want false (no qualifying inbound paired transfers)")
	}
	if !moneyEq(got.ReferenceAmount, 0) {
		t.Errorf("ReferenceAmount = %.4f, want 0 when no history", got.ReferenceAmount)
	}
}

// TestProject_OtherAccountsRecurringExcluded: a recurring item whose legs all
// belong to another account must NOT affect this account's projection. The
// projection filters the recurring engine's output to this account only.
func TestProject_OtherAccountsRecurringExcluded(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 5000.00}},
		LowBalanceThreshold: 500.00,
	}
	// A large recurring outflow that belongs ENTIRELY to schwab. If the
	// projection failed to filter by account, usaa's balance would cross.
	schwabOnly := rp("schwab-rent", 4000.00, "monthly", mustDate(2026, 9, 1),
		leg("schwab", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{schwabOnly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.Available {
		t.Fatal("Available = false, want true")
	}
	// usaa's balance should be unchanged at 5000 -- the schwab recurring item
	// was excluded entirely.
	if !got.Crossing.IsZero() {
		t.Errorf("Crossing = %v, want zero (schwab's recurring must not move usaa)", got.Crossing)
	}
	if !moneyEq(got.Minimum, 5000.00) {
		t.Errorf("Minimum = %.4f, want 5000.00 (no recurring applied to usaa)", got.Minimum)
	}
}

// TestProject_MixedRecurringFiltersByAccount: a recurring item with legs on
// BOTH accounts is relevant to usaa (one leg is ours) and is applied to usaa;
// a purely-schwab item is not. This guards the per-leg filter, not a naive
// "all or nothing" check.
func TestProject_MixedRecurringFiltersByAccount(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 800.00}},
		LowBalanceThreshold: 500.00,
	}
	// A recurring item with one usaa leg and one schwab leg. It qualifies for
	// usaa (recurringBelongsToAccount is true) and is applied to usaa. The
	// schwab leg is just evidence of membership, not an additional outflow.
	mixed := rp("split-rent", 300.00, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)),
		leg("schwab", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{mixed})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// 800 - 300 = 500, which is AT the threshold (not strictly below), so no
	// crossing. But the minimum is 500, confirming the outflow WAS applied.
	if !got.Crossing.IsZero() {
		t.Errorf("Crossing = %v, want zero (500 is at threshold, not below)", got.Crossing)
	}
	if !moneyEq(got.Minimum, 500.00) {
		t.Errorf("Minimum = %.4f, want 500.00 (300 outflow applied to usaa)", got.Minimum)
	}
}

// TestProject_DoesNotMutateInputs guards that Project is advisory: it must not
// reorder or alter the caller's account anchors or transaction slice. This is
// the mechanical half of "advisory only -- never writes anything": not only
// does it not touch the ledger/sidecar, it does not mutate its inputs either.
func TestProject_DoesNotMutateInputs(t *testing.T) {
	acct := models.Account{
		ID:   "usaa",
		Name: "usaa",
		Anchors: []models.BalanceAnchor{
			{Date: mustDate(2026, 8, 15), Amount: 3000.00},
			{Date: mustDate(2026, 8, 1), Amount: 1000.00}, // out of order
		},
		LowBalanceThreshold: 500.00,
	}
	txs := []models.Transaction{
		inboundTransfer("usaa", mustDate(2026, 6, 1), 2000.00),
	}
	recurring := []models.RecurringPayment{
		rp("rent", 100.00, "monthly", mustDate(2026, 9, 1),
			leg("usaa", mustDate(2026, 7, 1))),
	}
	anchorsBefore := append([]models.BalanceAnchor(nil), acct.Anchors...)
	txsBefore := append([]models.Transaction(nil), txs...)
	recBefore := append([]models.RecurringPayment(nil), recurring...)

	if _, err := Project(acct, txs, mustDate(2026, 8, 20), recurring); err != nil {
		t.Fatalf("Project: %v", err)
	}

	for i := range anchorsBefore {
		if !acct.Anchors[i].Date.Equal(anchorsBefore[i].Date) || acct.Anchors[i].Amount != anchorsBefore[i].Amount {
			t.Errorf("anchors mutated at %d: got %+v, want %+v", i, acct.Anchors[i], anchorsBefore[i])
		}
	}
	if len(txs) != len(txsBefore) {
		t.Errorf("txs length changed: got %d, want %d", len(txs), len(txsBefore))
	}
	for i := range txsBefore {
		if txs[i] != txsBefore[i] {
			t.Errorf("txs mutated at %d", i)
		}
	}
	if len(recurring) != len(recBefore) {
		t.Errorf("recurring length changed: got %d, want %d", len(recurring), len(recBefore))
	}
}

// TestProject_HorizonIs35Days: an outflow scheduled on day 35 (the last day of
// the window) is applied; one scheduled on day 36 is not. This pins the
// window length the spec fixes at 35 days.
func TestProject_HorizonIs35Days(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 5000.00}},
		LowBalanceThreshold: 500.00,
	}
	asOf := mustDate(2026, 8, 15)
	// Day 35 from 8/15 is 9/19 (8/15 + 35 days). A weekly outflow whose
	// NextExpected is 9/19 lands on the last day of the window -> applied.
	weekly := rp("weekly", 5000.00, "weekly", mustDate(2026, 9, 19),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, asOf, []models.RecurringPayment{weekly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// 5000 - 5000 = 0, below 500 -> crossing on 9/19.
	if !got.Crossing.Equal(mustDate(2026, 9, 19)) {
		t.Errorf("Crossing = %v, want 2026-09-19 (day-35 outflow must be applied)", got.Crossing)
	}
}

// TestProject_OutflowBeyondHorizonExcluded: an outflow scheduled on day 36
// (just past the 35-day window) is NOT applied -- the balance stays at the
// starting amount.
func TestProject_OutflowBeyondHorizonExcluded(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 5000.00}},
		LowBalanceThreshold: 500.00,
	}
	asOf := mustDate(2026, 8, 15)
	// Day 36 from 8/15 is 9/20. A weekly outflow whose NextExpected is 9/20
	// is outside the window -> not applied.
	weekly := rp("weekly", 5000.00, "weekly", mustDate(2026, 9, 20),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, asOf, []models.RecurringPayment{weekly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !got.Crossing.IsZero() {
		t.Errorf("Crossing = %v, want zero (day-36 outflow is outside the 35-day window)", got.Crossing)
	}
	if !moneyEq(got.Minimum, 5000.00) {
		t.Errorf("Minimum = %.4f, want 5000.00 (no outflow applied)", got.Minimum)
	}
}

// TestRoundUpToHundred unit-covers the rounding helper at the exact boundary
// and on either side, independent of the projection's wiring.
func TestRoundUpToHundred(t *testing.T) {
	cases := []struct {
		in, want float64
		name     string
	}{
		{0, 0, "zero shortfall"},
		{-50, 0, "negative shortfall (no funding needed)"},
		{100, 100, "exact 100 boundary (ceil of exact multiple is itself)"},
		{200, 200, "exact 200 boundary"},
		{101, 200, "just above 100 rounds up to 200"},
		{199, 200, "just below 200 rounds up to 200"},
		{250, 300, "mid-band rounds up to 300"},
		{1, 100, "one dollar rounds up to 100"},
	}
	for _, c := range cases {
		got := roundUpToHundred(c.in)
		if math.Abs(got-c.want) >= floatTolerance {
			t.Errorf("roundUpToHundred(%v) [%s] = %.4f, want %.4f", c.in, c.name, got, c.want)
		}
	}
}

// TestRoundUpToHundred_CentDustAtExactBoundary targets the specific defect
// this file was flagged for: cent-valued bank data whose true decimal
// shortfall is exactly $100 but whose float64 representation lands a hair
// above 100. math.Ceil applied to the bare float64 quotient turns that dust
// into a full extra hundred, over-recommending the top-up and risking an
// overdraft of the source account. Each fixture here currently returns 200
// against the unguarded ceil; the epsilon-guarded ceil must return 100.
//
// The shortfall values are produced the way Project actually produces them:
// threshold - (start - outflow), so the dust is real float64 dust, not a
// contrived literal. start=800.30 / outflow=400.30 and start=800.04 /
// outflow=400.04 are called out in the brief as ordinary data that fails
// today.
func TestRoundUpToHundred_CentDustAtExactBoundary(t *testing.T) {
	const threshold = 500.0
	cases := []struct {
		start, outflow float64
		name           string
	}{
		{800.30, 400.30, "800.30 start 400.30 outflow (brief fixture A)"},
		{800.04, 400.04, "800.04 start 400.04 outflow (brief fixture B)"},
		{800.60, 400.60, "800.60 start 400.60 outflow"},
		{800.10, 400.10, "800.10 start 400.10 outflow"},
		{800.90, 400.90, "800.90 start 400.90 outflow"},
	}
	for _, c := range cases {
		minimum := c.start - c.outflow
		shortfall := threshold - minimum
		got := roundUpToHundred(shortfall)
		if !moneyEq(got, 100.00) {
			t.Errorf("[%s] start=%.2f outflow=%.2f -> shortfall=%.20f topup=%.4f, want 100.00 (exact $100 boundary must not round up)",
				c.name, c.start, c.outflow, shortfall, got)
		}
	}
}

// TestRoundUpToHundred_CentDustSweep walks all 100 cent offsets so a
// regression at any unguarded comparison surfaces, not just the handful
// named above. For every cent c in [0,100), start - outflow == 400.00 in true
// decimal (shortfall exactly $100), but the float64 subtraction leaves dust
// above 100 for 24 of the 100 values with a single occurrence. None may
// return $200.
func TestRoundUpToHundred_CentDustSweep(t *testing.T) {
	const threshold = 500.0
	for c := 0; c < 100; c++ {
		start := 800.00 + float64(c)/100
		outflow := 400.00 + float64(c)/100
		minimum := start - outflow
		shortfall := threshold - minimum
		got := roundUpToHundred(shortfall)
		if !moneyEq(got, 100.00) {
			t.Errorf("cent=%d start=%.2f outflow=%.2f -> shortfall=%.20f topup=%.4f, want 100.00",
				c, start, outflow, shortfall, got)
		}
	}
}

// TestRoundUpToHundred_CentDustSweepWeekly repeats the sweep with five
// weekly occurrences of outflow/5 so the dust accumulates across
// subtractions (the brief reports this widens the failure count to 40/100).
// The true decimal shortfall is still exactly $100; the epsilon must still
// hold.
func TestRoundUpToHundred_CentDustSweepWeekly(t *testing.T) {
	const threshold = 500.0
	for c := 0; c < 100; c++ {
		outflow := 400.00 + float64(c)/100
		start := 800.00 + float64(c)/100
		occ := outflow / 5
		balance := start
		for i := 0; i < 5; i++ {
			balance -= occ
		}
		shortfall := threshold - balance
		got := roundUpToHundred(shortfall)
		if !moneyEq(got, 100.00) {
			t.Errorf("cent=%d weekly 5x outflow=%.2f -> shortfall=%.20f topup=%.4f, want 100.00",
				c, outflow, shortfall, got)
		}
	}
}

// TestRoundUpToHundred_GenuinelyPastBoundaryStillRoundsUp is the guard
// against over-correcting: a shortfall that is genuinely $100.01 (not dust)
// must still round up to $200. The epsilon only collapses sub-epsilon dust,
// so a cent-level overshoot stays above it.
func TestRoundUpToHundred_GenuinelyPastBoundaryStillRoundsUp(t *testing.T) {
	cases := []struct {
		in, want float64
		name     string
	}{
		{100.01, 200, "100.01 is genuinely past $100, rounds up to 200"},
		{100.50, 200, "100.50 rounds up to 200"},
		{199.99, 200, "199.99 rounds up to 200"},
		{200.01, 300, "200.01 rounds up to 300"},
	}
	for _, c := range cases {
		got := roundUpToHundred(c.in)
		if !moneyEq(got, c.want) {
			t.Errorf("[%s] roundUpToHundred(%.4f) = %.4f, want %.4f", c.name, c.in, got, c.want)
		}
	}
}

// TestProject_TopUpCentDustAtExactBoundary is an end-to-end guard: the
// projection's wiring (BalanceAt start -> Project minimum -> shortfall ->
// roundUpToHundred) must not turn a cent-valued exactly-$100 shortfall into a
// $200 top-up. start=800.30 / monthly outflow 400.30 / threshold 500 is the
// brief's primary fixture; without the epsilon guard it returns 200.00.
func TestProject_TopUpCentDustAtExactBoundary(t *testing.T) {
	acct := models.Account{
		ID:                  "usaa",
		Name:                "usaa",
		Anchors:             []models.BalanceAnchor{{Date: mustDate(2026, 8, 1), Amount: 800.30}},
		LowBalanceThreshold: 500.00,
	}
	// $400.30 outflow -> minimum 399.999...94 -> shortfall 100.000...06 -> topup 100.
	monthly := rp("rent", 400.30, "monthly", mustDate(2026, 9, 1),
		leg("usaa", mustDate(2026, 7, 1)))

	got, err := Project(acct, nil, mustDate(2026, 8, 15), []models.RecurringPayment{monthly})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !moneyEq(got.SuggestedTopUp, 100.00) {
		t.Errorf("SuggestedTopUp = %.4f, want 100.00 (cent-valued exactly-$100 shortfall must not round up); Minimum=%.20f shortfall=%.20f",
			got.SuggestedTopUp, got.Minimum, got.Threshold-got.Minimum)
	}
}
