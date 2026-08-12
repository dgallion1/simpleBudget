package anomalies

import (
	"testing"
	"time"

	"budget2/internal/models"
)

// newTxn builds a Transaction with Hash and derived fields (Month, Week,
// Year, ...) populated via the exported Transaction.ComputeHash /
// ComputeDerivedFields methods, matching how the real loader stamps
// transactions.
func newTxn(id string, date time.Time, desc, displayName string, amount float64, category string, ttype models.TransactionType) models.Transaction {
	t := models.Transaction{
		ID:              id,
		Date:            date,
		Amount:          amount,
		Description:     desc,
		DisplayName:     displayName,
		Category:        category,
		TransactionType: ttype,
	}
	t.Hash = t.ComputeHash()
	t.ComputeDerivedFields()
	return t
}

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// plantedFixture is the return value of buildPlantedFixture: the
// synthetic transaction set plus the documented ground truth every
// detector test asserts against.
type plantedFixture struct {
	Set models.TransactionSet

	// PlantedAnomalyHashes are the Hash values of the exactly-5 planted
	// high-side anomalies documented below. Every detector test that runs
	// against this fixture asserts these 5 (and only these 5, ±1 allowed
	// false positive per ANALYTICS_PORT_SPEC.md P2 acceptance) are
	// flagged.
	PlantedAnomalyHashes []string

	// NetflixFirstHash is the Hash of the very first NETFLIX charge
	// (small, normal amount) — used to assert NETFLIX is never flagged
	// new_merchant despite being a long-standing recurring group.
	NetflixFirstHash string

	// IncomeHashes / SuppressedHashes are transactions that must never
	// appear in Detect's output.
	IncomeHashes     []string
	SuppressedHashes []string
}

// buildPlantedFixture constructs an ~18-month (2025-01 through 2026-06)
// synthetic TransactionSet with the following planted truths (per
// ANALYTICS_PORT_SPEC.md §4 / P2 testing standard):
//
//   - Monthly NETFLIX, 1st of each month, 18 occurrences, category
//     "Subscriptions", stepped +20% drift every 6 months: $15.00
//     (2025-01 .. 2025-06), $18.00 (2025-07 .. 2025-12), $21.60 (2026-01
//     .. 2026-06). This is a legitimate gradual recurring increase, not
//     an anomaly, and is also the P3 price-creep fixture input; P2 only
//     asserts it is never flagged by Detect.
//   - Twice-monthly ACME PAYROLL income, 1st and 15th of each month, 36
//     occurrences, +$2500.00, TransactionType Income, category "Income".
//     Income is never considered (Detect only looks at Amount < 0).
//   - Weekly SPOTIFY, every 7 days starting 2025-01-05, 78 occurrences,
//     constant -$10.99, category "Subscriptions". Combined with NETFLIX,
//     SPOTIFY dominates the "Subscriptions" category by row count
//     (96 of 104 Subscriptions rows), which is the dominant-constant-
//     charge regression case: rule (a) must exclude both qualifying
//     groups (n=18, n=78) from the category baseline, leaving only the
//     8 one-off "Subscriptions" background-noise rows below.
//   - 8 one-off "Subscriptions" background-noise rows (distinct
//     merchants, n=1 each, never qualify as a merchant group): HULU $45,
//     GYM PLUS $52, ICLOUD STORAGE $38, AUDIBLE $60, DISNEY PLUS $41,
//     YOUTUBE PREMIUM $55, APPLE MUSIC $48, PARAMOUNT PLUS $50. Median of
//     this category-pool baseline is $49, MAD is $5 — none of these
//     clear z > 3.5, so none are flagged. Without rule (a), the
//     Subscriptions category median would be dominated by SPOTIFY's
//     constant $10.99 (median = 10.99, MAD = 0), and the MAD == 0
//     fallback (x > 3*10.99 = 32.97) would false-flag all 8 of these
//     rows — this is the regression rule (a) exists to prevent.
//   - ~10 categories total, each with background noise:
//     Subscriptions (above), Groceries, Dining, Utilities, Shopping,
//     Healthcare (each: baseline noise + exactly one planted anomaly,
//     see below), plus HomeImprovement, Insurance, Transportation,
//     Entertainment (pure background noise, no anomalies, ~8 rows each).
//   - EXACTLY 5 planted high-side anomalies, each >= 5x its category's
//     typical (median) spend, one per category:
//     -- 2025-03-10 Groceries "WHOLESALE CLUB BULK ORDER" $550.00
//     (baseline median $100, 5.5x)
//     -- 2025-06-14 Dining "STEAKHOUSE BLOWOUT DINNER" $330.00
//     (baseline median $60, 5.5x)
//     -- 2025-09-02 Utilities "EMERGENCY HVAC REPAIR BILL" $825.00
//     (baseline median $150, 5.5x)
//     -- 2025-12-20 Shopping "HOLIDAY ELECTRONICS SPLURGE" $440.00
//     (baseline median $80, 5.5x)
//     -- 2026-04-05 Healthcare "SPECIALIST PROCEDURE COPAY" $1100.00
//     (baseline median $200, 5.5x)
//     Each baseline (10 rows per category) is a tight cluster around its
//     median (see buildCategoryWithAnomaly), so MAD is small, z clears
//     3.5 by a wide margin, and the materiality floor and direction rule
//     are trivially cleared.
func buildPlantedFixture(t *testing.T) plantedFixture {
	t.Helper()

	var txns []models.Transaction
	seen := make(map[string]bool)
	add := func(tx models.Transaction) models.Transaction {
		if seen[tx.Hash] {
			t.Fatalf("fixture bug: duplicate Hash %q for description %q date %s amount %.2f", tx.Hash, tx.Description, tx.Date, tx.Amount)
		}
		seen[tx.Hash] = true
		txns = append(txns, tx)
		return tx
	}

	// --- Monthly NETFLIX, stepped +20% drift ---
	var netflixFirstHash string
	month := d(2025, time.January, 1)
	for i := 0; i < 18; i++ {
		amount := -15.00
		switch {
		case i >= 12:
			amount = -21.60
		case i >= 6:
			amount = -18.00
		}
		tx := newTxn("netflix", month, "NETFLIX.COM", "", amount, "Subscriptions", models.Outflow)
		add(tx)
		if i == 0 {
			netflixFirstHash = tx.Hash
		}
		month = month.AddDate(0, 1, 0)
	}

	// --- Twice-monthly ACME PAYROLL income ---
	var incomeHashes []string
	pay := d(2025, time.January, 1)
	for i := 0; i < 18; i++ {
		first := d(pay.Year(), pay.Month(), 1)
		fifteenth := d(pay.Year(), pay.Month(), 15)
		tx1 := newTxn("payroll", first, "ACME PAYROLL", "", 2500.00, "Income", models.Income)
		tx2 := newTxn("payroll", fifteenth, "ACME PAYROLL", "", 2500.00, "Income", models.Income)
		add(tx1)
		add(tx2)
		incomeHashes = append(incomeHashes, tx1.Hash, tx2.Hash)
		pay = pay.AddDate(0, 1, 0)
	}

	// --- Weekly SPOTIFY, constant charge ---
	spDate := d(2025, time.January, 5)
	end := d(2026, time.June, 30)
	for !spDate.After(end) {
		add(newTxn("spotify", spDate, "SPOTIFY", "", -10.99, "Subscriptions", models.Outflow))
		spDate = spDate.AddDate(0, 0, 7)
	}

	// --- 8 one-off "Subscriptions" background-noise rows ---
	noiseSubs := []struct {
		date   time.Time
		desc   string
		amount float64
	}{
		{d(2025, time.February, 3), "HULU", -45.00},
		{d(2025, time.April, 11), "GYM PLUS", -52.00},
		{d(2025, time.May, 20), "ICLOUD STORAGE", -38.00},
		{d(2025, time.July, 7), "AUDIBLE", -60.00},
		{d(2025, time.September, 14), "DISNEY PLUS", -41.00},
		{d(2025, time.November, 2), "YOUTUBE PREMIUM", -55.00},
		{d(2026, time.January, 18), "APPLE MUSIC", -48.00},
		{d(2026, time.March, 9), "PARAMOUNT PLUS", -50.00},
	}
	for _, n := range noiseSubs {
		add(newTxn("noise-sub", n.date, n.desc, "", n.amount, "Subscriptions", models.Outflow))
	}

	// --- 4 pure-noise categories, 8 rows each, no anomalies ---
	pureNoise := []struct {
		category string
		base     float64
		merchant string
	}{
		{"HomeImprovement", 120, "HARDWARE STORE"},
		{"Insurance", 90, "AUTO INSURANCE CO"},
		{"Transportation", 40, "CITY TRANSIT"},
		{"Entertainment", 30, "MOVIE THEATER"},
	}
	jitter := []float64{-5, -3, -1, 0, 1, 2, 4, 5}
	// Each row gets a distinct trailing token (ALPHA..THETA) so every row
	// is its own singleton merchant group (n=1, never qualifying) rather
	// than accidentally merging under the token-subset rule — a bare
	// "HARDWARE STORE" token pair is a subset of "HARDWARE STORE
	// DOWNTOWN", which would transitively bridge unrelated rows into one
	// qualifying group and change which method (mad_category vs
	// mad_merchant) exercises this fixture.
	distinctSuffix := []string{"ALPHA", "BETA", "GAMMA", "DELTA", "EPSILON", "ZETA", "ETA", "THETA"}
	for _, pn := range pureNoise {
		start := d(2025, time.January, 15)
		for i, j := range jitter {
			date := start.AddDate(0, i*2, 0)
			desc := pn.merchant + " " + distinctSuffix[i]
			add(newTxn("pure-noise", date, desc, "", -(pn.base + j), pn.category, models.Outflow))
		}
	}

	// --- 5 categories: baseline + exactly one planted 5.5x anomaly ---
	type anomalySpec struct {
		category   string
		baseMedian float64
		anomDate   time.Time
		anomDesc   string
		anomAmount float64
	}
	specs := []anomalySpec{
		{"Groceries", 100, d(2025, time.March, 10), "WHOLESALE CLUB BULK ORDER", -550.00},
		{"Dining", 60, d(2025, time.June, 14), "STEAKHOUSE BLOWOUT DINNER", -330.00},
		{"Utilities", 150, d(2025, time.September, 2), "EMERGENCY HVAC REPAIR BILL", -825.00},
		{"Shopping", 80, d(2025, time.December, 20), "HOLIDAY ELECTRONICS SPLURGE", -440.00},
		{"Healthcare", 200, d(2026, time.April, 5), "SPECIALIST PROCEDURE COPAY", -1100.00},
	}
	// Baseline jitter as a fraction of the category median, tight enough
	// that MAD stays small and none of the baseline rows themselves clear
	// z > 3.5, sorted ascending. The 10 rows per category are split
	// across 4 shared merchant identities (group sizes 3/3/2/2, indexed
	// by i%4) rather than 10 fully distinct one-off merchants: each
	// group stays under the n=4 qualifying threshold (so all 10 rows
	// still land in the category pool, unaffected by rule (a)), but
	// critically the new_merchant check only ever examines a group's
	// EARLIEST-dated member. Assigning ascending dates by increasing i
	// means each group's earliest member is its lowest-index (and here,
	// most-negative-jitter, so at-or-below-median) row — e.g. group 0's
	// earliest is i=0 (frac -0.05), never i=4 or i=8. This guarantees no
	// baseline row's amount is ever a new_merchant candidate, regardless
	// of where the global 95th percentile happens to fall, without
	// requiring 10 distinct merchant identities per category.
	baselineFrac := []float64{-0.05, -0.04, -0.03, -0.02, -0.01, 0.00, 0.00, 0.01, 0.02, 0.03}
	groupNames := []string{"MERCHANT ALPHA", "MERCHANT BETA", "MERCHANT GAMMA", "MERCHANT DELTA"}
	var plantedHashes []string
	for si, spec := range specs {
		start := d(2025, time.January, 8).AddDate(0, si, 0)
		for i, frac := range baselineFrac {
			date := start.AddDate(0, 0, i*5)
			amount := -(spec.baseMedian * (1 + frac))
			desc := spec.category + " " + groupNames[i%len(groupNames)]
			add(newTxn("cat-noise", date, desc, "", amount, spec.category, models.Outflow))
		}
		tx := newTxn("planted-anomaly", spec.anomDate, spec.anomDesc, "", spec.anomAmount, spec.category, models.Outflow)
		add(tx)
		plantedHashes = append(plantedHashes, tx.Hash)
	}

	// --- A couple of Suppressed transactions that would otherwise be
	// flagged (large, high-side) to confirm Active() filtering. ---
	var suppressedHashes []string
	supp := newTxn("suppressed", d(2025, time.May, 1), "SUPPRESSED OUTLIER CHARGE", "", -999.00, "Groceries", models.Outflow)
	supp.Suppressed = true
	add(supp)
	suppressedHashes = append(suppressedHashes, supp.Hash)

	return plantedFixture{
		Set:                  models.TransactionSet{Transactions: txns},
		PlantedAnomalyHashes: plantedHashes,
		NetflixFirstHash:     netflixFirstHash,
		IncomeHashes:         incomeHashes,
		SuppressedHashes:     suppressedHashes,
	}
}
