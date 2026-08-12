package anomalies

import (
	"math"
	"reflect"
	"testing"
	"time"

	"budget2/internal/models"
)

// hashesOf returns the set of Hash values present in got.
func hashesOf(got []Anomaly) map[string]Anomaly {
	m := make(map[string]Anomaly, len(got))
	for _, a := range got {
		m[a.Hash] = a
	}
	return m
}

func TestDetect_PlantedFixture_FlagsFiveWithAtMostOneFalsePositive(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)
	byHash := hashesOf(got)

	var missed []string
	for _, h := range fx.PlantedAnomalyHashes {
		if _, ok := byHash[h]; !ok {
			missed = append(missed, h)
		}
	}
	if len(missed) != 0 {
		t.Fatalf("expected all 5 planted anomalies flagged, missed %d: %v\nfull result: %+v", len(missed), missed, got)
	}

	planted := make(map[string]bool, len(fx.PlantedAnomalyHashes))
	for _, h := range fx.PlantedAnomalyHashes {
		planted[h] = true
	}
	falsePositives := 0
	for _, a := range got {
		if !planted[a.Hash] {
			falsePositives++
			t.Logf("false positive: %+v", a)
		}
	}
	if falsePositives > 1 {
		t.Fatalf("expected <=1 false positive, got %d\nfull result: %+v", falsePositives, got)
	}
}

func TestDetect_PlantedFixture_DominantConstantChargeDoesNotCollapseCategoryMAD(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)

	noiseDescriptions := map[string]bool{
		"HULU": true, "GYM PLUS": true, "ICLOUD STORAGE": true, "AUDIBLE": true,
		"DISNEY PLUS": true, "YOUTUBE PREMIUM": true, "APPLE MUSIC": true, "PARAMOUNT PLUS": true,
	}
	for _, a := range got {
		if noiseDescriptions[a.Description] {
			t.Errorf("Subscriptions background-noise row flagged (rule (a) regression): %+v", a)
		}
	}
}

func TestDetect_SmallGroup_ImmaterialWobbleNotFlagged(t *testing.T) {
	// n=5 merchant group, median ~$22, tight cluster (MAD=1) so a $30
	// wobble clears the z-threshold but not the materiality floor
	// (deviation $8 < max(0.5*22, $10) = $11).
	txns := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "CORNER DELI WOBBLE", "", -21.00, "Dining", models.Outflow),
		newTxn("2", d(2025, time.January, 8), "CORNER DELI WOBBLE", "", -22.00, "Dining", models.Outflow),
		newTxn("3", d(2025, time.January, 15), "CORNER DELI WOBBLE", "", -22.00, "Dining", models.Outflow),
		newTxn("4", d(2025, time.January, 22), "CORNER DELI WOBBLE", "", -23.00, "Dining", models.Outflow),
		newTxn("5", d(2025, time.January, 29), "CORNER DELI WOBBLE", "", -30.00, "Dining", models.Outflow),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	for _, a := range got {
		if a.Hash == txns[4].Hash {
			t.Fatalf("expected immaterial $30 wobble NOT flagged, got %+v", a)
		}
	}
}

func TestDetect_SmallGroup_MaterialOutlierFlagged(t *testing.T) {
	// Same tight n=5 cluster, but the 5th value is a $200 outlier — well
	// past the materiality floor.
	txns := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "CORNER DELI MATERIAL", "", -21.00, "Dining", models.Outflow),
		newTxn("2", d(2025, time.January, 8), "CORNER DELI MATERIAL", "", -22.00, "Dining", models.Outflow),
		newTxn("3", d(2025, time.January, 15), "CORNER DELI MATERIAL", "", -22.00, "Dining", models.Outflow),
		newTxn("4", d(2025, time.January, 22), "CORNER DELI MATERIAL", "", -23.00, "Dining", models.Outflow),
		newTxn("5", d(2025, time.January, 29), "CORNER DELI MATERIAL", "", -200.00, "Dining", models.Outflow),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	byHash := hashesOf(got)
	a, ok := byHash[txns[4].Hash]
	if !ok {
		t.Fatalf("expected material $200 outlier flagged, got %+v", got)
	}
	if a.Method != "mad_merchant" {
		t.Errorf("expected method mad_merchant, got %q", a.Method)
	}
	if a.Severity != "high" {
		t.Errorf("expected severity high, got %q (score %v)", a.Severity, a.Score)
	}
}

func TestDetect_LowSideOutlier_NotFlagged(t *testing.T) {
	// n=4 merchant group, median ~$99-100, one dramatic low-side value
	// ($5). Rule (c): low-side deviations are never anomalies.
	txns := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "BIG BOX STORE", "", -98.00, "Shopping", models.Outflow),
		newTxn("2", d(2025, time.January, 8), "BIG BOX STORE", "", -102.00, "Shopping", models.Outflow),
		newTxn("3", d(2025, time.January, 15), "BIG BOX STORE", "", -100.00, "Shopping", models.Outflow),
		newTxn("4", d(2025, time.January, 22), "BIG BOX STORE", "", -5.00, "Shopping", models.Outflow),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	for _, a := range got {
		if a.Hash == txns[3].Hash {
			t.Fatalf("expected low-side $5 outlier NOT flagged by MAD methods, got %+v", a)
		}
	}
}

func TestDetect_IncomeNeverFlagged(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)
	byHash := hashesOf(got)
	for _, h := range fx.IncomeHashes {
		if a, ok := byHash[h]; ok {
			t.Fatalf("income transaction flagged: %+v", a)
		}
	}
}

func TestDetect_SuppressedNeverFlagged(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)
	byHash := hashesOf(got)
	for _, h := range fx.SuppressedHashes {
		if a, ok := byHash[h]; ok {
			t.Fatalf("suppressed transaction flagged: %+v", a)
		}
	}
}

func TestDetect_ExpenseFilter_ExcludesNonOutflowOrPositiveAmount(t *testing.T) {
	// Expense filter is TransactionType == Outflow AND Amount < 0. A
	// refund-style row (Outflow but positive Amount) and a
	// negative-amount row tagged Income must both be excluded — not just
	// from the flagged results, but from the peer-group baselines
	// (median/MAD) themselves, so they can't skew other transactions'
	// scores either.
	group := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "STEADY MERCHANT", "", -50.00, "Shopping", models.Outflow),
		newTxn("2", d(2025, time.January, 8), "STEADY MERCHANT", "", -51.00, "Shopping", models.Outflow),
		newTxn("3", d(2025, time.January, 15), "STEADY MERCHANT", "", -49.00, "Shopping", models.Outflow),
		newTxn("4", d(2025, time.January, 22), "STEADY MERCHANT", "", -50.00, "Shopping", models.Outflow),
	}
	refund := newTxn("refund", d(2025, time.January, 29), "STEADY MERCHANT", "", 500.00, "Shopping", models.Outflow)
	mistagged := newTxn("mistagged-income", d(2025, time.February, 5), "STEADY MERCHANT", "", -500.00, "Shopping", models.Income)

	txns := append(append([]models.Transaction{}, group...), refund, mistagged)
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)

	byHash := hashesOf(got)
	if _, ok := byHash[refund.Hash]; ok {
		t.Errorf("refund row (Outflow, positive amount) must be excluded, got flagged")
	}
	if _, ok := byHash[mistagged.Hash]; ok {
		t.Errorf("negative-amount Income row must be excluded, got flagged")
	}
	// The refund/mistagged rows must not have skewed the "STEADY
	// MERCHANT" group's own members into looking anomalous either — if
	// they leaked into the group's median/MAD computation, the $50ish
	// baseline rows would suddenly look like low-side or high-side
	// outliers against a population containing $500/-$500.
	for _, tx := range group {
		if a, ok := byHash[tx.Hash]; ok {
			t.Errorf("baseline STEADY MERCHANT row unexpectedly flagged (peer stats leaked non-expense rows): %+v", a)
		}
	}
}

func TestDetect_EmptySet_NoPanic(t *testing.T) {
	got := Detect(models.TransactionSet{})
	if got == nil {
		t.Fatalf("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}

func TestDetect_SingleTransactionCategories_NoPanic(t *testing.T) {
	// Each category/merchant group here has exactly one row — below both
	// minCategoryRows and minMerchantGroupRows, so mad_category/
	// mad_merchant never run. The point of this test is that Detect
	// doesn't panic on a percentile/median computation over a
	// single-element (or two-element) population; new_merchant may or
	// may not fire depending on relative magnitude, which is not what
	// this test is checking.
	txns := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "LONE WOLF MERCHANT", "", -50.00, "OnlyOne", models.Outflow),
		newTxn("2", d(2025, time.January, 2), "ANOTHER LONE MERCHANT", "", -75.00, "AlsoOnlyOne", models.Outflow),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	t.Logf("result (no panic is the assertion): %+v", got)
}

func TestDetect_NewMerchant_FirstSeenAbovePercentileFlagged(t *testing.T) {
	var txns []models.Transaction
	start := d(2025, time.January, 1)
	// 20 typical transactions around $50, distinct single-occurrence
	// merchants/categories so none qualify as a merchant group or a
	// category baseline (keeps this test isolated to new_merchant).
	names := []string{
		"A STORE", "B STORE", "C STORE", "D STORE", "E STORE",
		"F STORE", "G STORE", "H STORE", "I STORE", "J STORE",
		"K STORE", "L STORE", "M STORE", "N STORE", "O STORE",
		"P STORE", "Q STORE", "R STORE", "S STORE", "T STORE",
	}
	amounts := []float64{48, 49, 50, 51, 52, 47, 53, 46, 54, 45, 55, 44, 56, 43, 57, 42, 58, 41, 59, 40}
	for i, name := range names {
		date := start.AddDate(0, 0, i)
		txns = append(txns, newTxn("typical", date, name, "", -amounts[i], "Misc"+name, models.Outflow))
	}
	gadget := newTxn("gadget", start.AddDate(0, 0, 25), "GADGET STORE", "", -900.00, "Electronics", models.Outflow)
	txns = append(txns, gadget)

	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	byHash := hashesOf(got)
	a, ok := byHash[gadget.Hash]
	if !ok {
		t.Fatalf("expected first-seen $900 GADGET STORE flagged, got %+v", got)
	}
	if a.Method != "new_merchant" {
		t.Errorf("expected method new_merchant, got %q", a.Method)
	}
	if a.MadZ != 0 {
		t.Errorf("expected MadZ 0 for new_merchant, got %v", a.MadZ)
	}
}

func TestDetect_NewMerchant_LongStandingGroupNeverFlagged(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)
	for _, a := range got {
		if a.Hash == fx.NetflixFirstHash && a.Method == "new_merchant" {
			t.Fatalf("expected NETFLIX (long-standing recurring group) never flagged new_merchant, got %+v", a)
		}
	}
}

func TestDetect_Dedupe_HigherScoreMethodWins(t *testing.T) {
	// Merchant group "SURPRISE VENDOR" qualifies (n=4). Its first
	// transaction is both the group's dramatic outlier (huge mad_merchant
	// z) and, being unusually large and first-seen, also clears the
	// new_merchant percentile test. mad_merchant's z-score dwarfs the
	// new_merchant ratio, so the deduped result must report mad_merchant.
	var txns []models.Transaction
	surprise := newTxn("surprise", d(2025, time.January, 1), "SURPRISE VENDOR", "", -500.00, "Shopping", models.Outflow)
	txns = append(txns,
		surprise,
		newTxn("surprise2", d(2025, time.February, 1), "SURPRISE VENDOR", "", -50.00, "Shopping", models.Outflow),
		newTxn("surprise3", d(2025, time.March, 1), "SURPRISE VENDOR", "", -52.00, "Shopping", models.Outflow),
		newTxn("surprise4", d(2025, time.April, 1), "SURPRISE VENDOR", "", -48.00, "Shopping", models.Outflow),
	)
	// Filler transactions to keep p95 well below $500 (so the huge first
	// transaction also clears the new_merchant test) without themselves
	// forming qualifying groups or a >=6-row category baseline (kept
	// under minCategoryRows and each merchant single-occurrence).
	fillerNames := []string{"F1", "F2", "F3", "F4", "F5"}
	for i, n := range fillerNames {
		txns = append(txns, newTxn("filler", d(2025, time.May, i+1), n+" MART", "", -55.00, "Filler", models.Outflow))
	}

	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	byHash := hashesOf(got)

	// Exactly one Anomaly for the surprise transaction.
	count := 0
	for _, a := range got {
		if a.Hash == surprise.Hash {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 Anomaly for the dual-method transaction, got %d\nfull result: %+v", count, got)
	}

	a, ok := byHash[surprise.Hash]
	if !ok {
		t.Fatalf("expected surprise transaction flagged, got %+v", got)
	}
	if a.Method != "mad_merchant" {
		t.Errorf("expected higher-scoring method mad_merchant to win dedupe, got %q (score %v)", a.Method, a.Score)
	}
}

func TestDetect_Determinism(t *testing.T) {
	fx := buildPlantedFixture(t)
	got1 := Detect(fx.Set)
	got2 := Detect(fx.Set)
	if !reflect.DeepEqual(got1, got2) {
		t.Fatalf("Detect not deterministic across runs:\nrun1: %+v\nrun2: %+v", got1, got2)
	}
}

func TestDetect_ResultsSortedByScoreDescending(t *testing.T) {
	fx := buildPlantedFixture(t)
	got := Detect(fx.Set)
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Fatalf("results not sorted by score descending at index %d: %v then %v", i, got[i-1].Score, got[i].Score)
		}
		if got[i-1].Score == got[i].Score && got[i-1].Hash > got[i].Hash {
			t.Fatalf("tie-break not ascending by hash at index %d: %v then %v", i, got[i-1].Hash, got[i].Hash)
		}
	}
}

func TestDetect_DisplayNameFallbackPrecedence(t *testing.T) {
	txns := []models.Transaction{
		newTxn("1", d(2025, time.January, 1), "RAW DESC ONE", "Alias One", -21.00, "Dining", models.Outflow),
		newTxn("2", d(2025, time.January, 8), "RAW DESC TWO", "", -22.00, "Dining", models.Outflow),
		newTxn("3", d(2025, time.January, 15), "RAW DESC THREE", "", -22.00, "Dining", models.Outflow),
		newTxn("4", d(2025, time.January, 22), "RAW DESC FOUR", "", -23.00, "Dining", models.Outflow),
		newTxn("5", d(2025, time.January, 29), "RAW DESC FIVE", "", -200.00, "Dining", models.Outflow),
	}
	ts := models.TransactionSet{Transactions: txns}
	got := Detect(ts)
	if len(got) == 0 {
		t.Fatalf("expected at least the material outlier flagged")
	}
	for _, a := range got {
		if a.Hash == txns[0].Hash && a.Description != "Alias One" {
			t.Errorf("expected DisplayName precedence, got %q", a.Description)
		}
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		vals []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{5}, 5},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{4, 1, 3, 2}, 2.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := median(c.vals); got != c.want {
				t.Errorf("median(%v) = %v, want %v", c.vals, got, c.want)
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	// numpy linear interpolation: idx = 0.95*9 = 8.55 -> 90 + 0.55*(100-90) = 95.5
	got := percentile(vals, 95)
	want := 95.5
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("percentile(vals, 95) = %v, want %v", got, want)
	}
}

func TestIsMaterial(t *testing.T) {
	cases := []struct {
		name string
		x    float64
		med  float64
		want bool
	}{
		{"below dollar floor and ratio floor", 25, 20, false},   // dev 5, floor max(10,10)=10
		{"clears dollar floor", 31, 20, true},                   // dev 11 >= 10
		{"clears ratio floor on large median", 400, 200, true},  // dev 200 >= max(100,10)=100
		{"below ratio floor on large median", 250, 200, false},  // dev 50 < 100
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMaterial(c.x, c.med); got != c.want {
				t.Errorf("isMaterial(%v, %v) = %v, want %v", c.x, c.med, got, c.want)
			}
		})
	}
}
