package dataloader

// ND1 oracle — staged into internal/services/dataloader/ by
// .swarm/tier3/ND1/accept.sh for the duration of one run, then removed.
// Asserts acceptance criteria A1 (seven verbatim settlement pairs), A2
// (guards) and A3 (real-data regression) of .swarm/ND-RUN-SPEC.md.
// All fixture field values are verbatim from the CSVs named in comments.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func nd1Tx(date string, amount float64, desc, orig, status, account string) models.Transaction {
	d, _ := time.Parse("2006-01-02", date)
	t := models.Transaction{
		Date:                d,
		Amount:              amount,
		Description:         desc,
		OriginalDescription: orig,
		Status:              status,
		AccountID:           account,
		TransactionType:     models.Outflow,
	}
	t.Hash = t.ComputeHash()
	return t
}

// nd1Pairs: the seven live undetected pairs, verbatim. Sources:
// usaa-credit_2026-04-18_to_2026-05-04.csv, usaa-credit_2026-05-01_to_2026-07-12.csv,
// usaa-credit_2026-03-01_to_2026-04-29.csv, usaa-checking_2026-02-05_to_2026-05-22.csv,
// usaa-checking_2026-05-05_to_2026-07-13.csv.
var nd1Pairs = []struct {
	name string
	a, b models.Transaction
}{
	{"bjs-wholesale-634.40",
		nd1Tx("2026-05-02", -634.40, "BJS WHOLESALE #0075", "BJS WHOLESALE #0075", "Pending", "usaa-credit"),
		nd1Tx("2026-05-02", -634.40, "BJ's Wholesale", "BJS WHOLESALE #0075      WEBSTER      NY", "Posted", "usaa-credit")},
	{"bjs-membership-64.50",
		nd1Tx("2026-05-01", -64.50, "BJS MEMBERSHIP", "BJS MEMBERSHIP", "Pending", "usaa-credit"),
		nd1Tx("2026-05-01", -64.50, "Membership", "BJS MEMBERSHIP           800-257-2582 MA", "Posted", "usaa-credit")},
	{"grubhub-fiveguys-59",
		nd1Tx("2026-05-01", -59.00, "GRUBHUB*FIVEGUYS", "GRUBHUB*FIVEGUYS", "Pending", "usaa-credit"),
		nd1Tx("2026-05-02", -59.00, "Five Guys via Grubhub", "GRUBHUB*FIVEGUYS         GRUBHUB.COM  IL", "Posted", "usaa-credit")},
	{"grubhub-fiveguys-58",
		nd1Tx("2026-07-12", -58.00, "GRUBHUB*FIVEGUYS", "GRUBHUB*FIVEGUYS", "Pending", "usaa-credit"),
		nd1Tx("2026-07-13", -58.00, "Five Guys via Grubhub", "GRUBHUB*FIVEGUYS         GRUBHUB.COM  IL", "Posted", "usaa-credit")},
	{"grammarly-144",
		nd1Tx("2026-04-29", -144.00, "GRAMMARLY CO*UK0GWQL", "GRAMMARLY CO*UK0GWQL", "Pending", "usaa-credit"),
		nd1Tx("2026-04-29", -144.00, "Grammarly.com", "GRAMMARLY CO*UK0GWQL     GRAMMARLY.COMCA", "Posted", "usaa-credit")},
	{"usaa-insurance-464.99",
		nd1Tx("2026-05-04", -464.99, "USAA INSURANCE BILL PAYMENT", "", "Recurring Scheduled Bill Pay", "usaa-checking"),
		nd1Tx("2026-05-05", -464.99, "USAA Property and Casualty Insurance", "USAA P&C         AUTOPAY    ***********1380", "Posted", "usaa-checking")},
	{"monroe-water-20",
		nd1Tx("2026-05-22", -20.00, "Monroe County Water Authority", "", "Scheduled Bill Pay", "usaa-checking"),
		nd1Tx("2026-05-22", -20.00, "Monroe Water", "MONROE WATER     ONLINE PMT ***********7POS", "Posted", "usaa-checking")},
}

// A1: each pair detected in isolation.
func TestOracleND1_SettlementPairs(t *testing.T) {
	for _, tc := range nd1Pairs {
		t.Run(tc.name, func(t *testing.T) {
			pairs := detectNearDuplicatePairs([]models.Transaction{tc.a, tc.b})
			if len(pairs) != 1 {
				t.Fatalf("verbatim rows: expected 1 pair, got %d", len(pairs))
			}
			if pairs[0].Key == "" {
				t.Error("pair key should be non-empty")
			}
		})
	}
}

// A2: guards — each scenario must detect 0 pairs (except the check-shape
// regression at the bottom, which must stay at 1).
func TestOracleND1_Guards(t *testing.T) {
	ins, insPosted := nd1Pairs[5].a, nd1Pairs[5].b
	monroe := nd1Pairs[6].a

	reDate := func(tx models.Transaction, date string) models.Transaction {
		d, _ := time.Parse("2006-01-02", date)
		tx.Date = d
		tx.Hash = tx.ComputeHash()
		return tx
	}

	cases := []struct {
		name string
		txns []models.Transaction
	}{
		// Gap-A affinity never crosses accounts.
		{"gapA-cross-account", func() []models.Transaction {
			b := nd1Pairs[0].b
			b.AccountID = "usaa-checking"
			b.Hash = b.ComputeHash()
			return []models.Transaction{nd1Pairs[0].a, b}
		}()},
		// Gap-B window is 7 days: settle at day 8 → no pair.
		{"gapB-window-8-days", []models.Transaction{ins, reDate(insPosted, "2026-05-12")}},
		// Gap-B needs ≥2 shared tokens: Wire Fee (verbatim desc/orig,
		// usaa-checking bk_download.csv) shares 0 with Monroe scheduled.
		{"gapB-single-token", []models.Transaction{monroe,
			nd1Tx("2026-05-22", -20.00, "Wire Fee", "WIRE FEE", "Posted", "usaa-checking")}},
		// Gap-B needs a scheduled side: two Posted Monroe Water rows 3
		// days apart (differing orig suffix defeats shape 2) → no pair.
		{"gapB-both-posted", []models.Transaction{
			nd1Tx("2026-05-22", -20.00, "Monroe Water", "MONROE WATER     ONLINE PMT ***********7POS", "Posted", "usaa-checking"),
			nd1Tx("2026-05-25", -20.00, "Monroe Water", "MONROE WATER     ONLINE PMT ***********8POS", "Posted", "usaa-checking")}},
		// Distinct monthly subscriptions, both Posted, same price, 5 days
		// apart (verbatim 2025-09 rows) must never pair.
		{"google-one-vs-google-com", []models.Transaction{
			nd1Tx("2025-09-08", -26.99, "Google One", "Google One               650-2530000  CA", "Posted", "usaa-credit"),
			nd1Tx("2025-09-13", -26.99, "Google.com", "GOOGLE *Developer Pro    cc@google.comCA", "Posted", "usaa-credit")}},
		// Known out-of-scope miss stays out: pending→posted at 4 days is
		// outside the 3-day window even though orig-desc affinity holds
		// (verbatim 2025-11 Amazon rows).
		{"amazon-30.81-4-day-window", []models.Transaction{
			nd1Tx("2025-11-16", -30.81, "AMAZON MKTPLACE PMTS", "AMAZON MKTPLACE PMTS", "Pending", "usaa-credit"),
			nd1Tx("2025-11-20", -30.81, "Amazon", "AMAZON MKTPL*B02UM4581   Amzn.com/billWA", "Posted", "usaa-credit")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectNearDuplicatePairs(tc.txns); len(got) != 0 {
				t.Errorf("expected 0 pairs, got %d", len(got))
			}
		})
	}

	// Regression: the classify() bill-pay/check shape still fires exactly
	// once for the Hyundi pair (verbatim rows), byte-identical behavior.
	t.Run("check-shape-hyundi-regression", func(t *testing.T) {
		got := detectNearDuplicatePairs([]models.Transaction{
			nd1Tx("2026-05-15", -626.00, "Hyundi Motor Finance", "", "Scheduled Bill Pay", "usaa-checking"),
			nd1Tx("2026-05-19", -626.00, "Check #996593", "CHECK # 0000996593", "Posted", "usaa-checking")})
		if len(got) != 1 {
			t.Fatalf("check-settlement shape regressed: expected 1 pair, got %d", len(got))
		}
	})
}

// A3: real data. Loads a copy of the repo's live data directory and asserts
// the unresolved queue is EXACTLY the seven new pairs, and that every
// recorded decision still binds (16 kept_winner, 1 kept_both).
func TestOracleND1_RealData(t *testing.T) {
	src := filepath.Join("..", "..", "..", "data")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("real data dir not found at %s: %v", src, err)
	}
	tmp := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // cache/settings/uploads not needed by the loader path under test
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.New(tmp)
	if err != nil {
		t.Fatal(err)
	}
	loader := New(tmp, store)
	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("LoadData over data copy: %v", err)
	}

	want := map[string]bool{
		"2026-05-02|634.40": false,
		"2026-05-01|64.50":  false,
		"2026-05-01|59.00":  false, // pending side dates the pair
		"2026-07-12|58.00":  false,
		"2026-04-29|144.00": false,
		"2026-05-04|464.99": false, // scheduled side dates the pair
		"2026-05-22|20.00":  false,
	}
	unresolved := loader.UnresolvedDuplicates()
	var got []string
	for _, p := range unresolved {
		earliest := p.Left
		if p.Right.Date.Before(earliest.Date) {
			earliest = p.Right
		}
		amt := p.Left.Amount
		if amt < 0 {
			amt = -amt
		}
		sig := earliest.Date.Format("2006-01-02") + "|" + fmt.Sprintf("%.2f", amt)
		got = append(got, sig)
		if _, ok := want[sig]; ok {
			want[sig] = true
		}
	}
	sort.Strings(got)
	if len(unresolved) != len(want) {
		t.Errorf("expected exactly %d unresolved pairs on live data (calibrated 2026-08-31), got %d: %v",
			len(want), len(unresolved), got)
	}
	for sig, found := range want {
		if !found {
			t.Errorf("expected pair %s missing from the unresolved queue (got %v)", sig, got)
		}
	}
	if got := len(loader.ResolvedDuplicates()); got != 16 {
		t.Errorf("kept_winner decisions no longer bind: expected 16, got %d", got)
	}
	if got := len(loader.KeptBothDuplicates()); got != 1 {
		t.Errorf("kept_both decisions no longer bind: expected 1, got %d", got)
	}
}
