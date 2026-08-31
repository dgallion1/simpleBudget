package dataloader

// DP1 oracle — staged into internal/services/dataloader/ by
// .swarm/tier3/DP1/accept.sh for the duration of one run, then removed.
// Asserts acceptance criteria A1 (verbatim Harbor Freight fixture + guards)
// and A2 (real-data regression) of .swarm/DP-RUN-SPEC.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func dp1Tx(date string, amount float64, desc, status, account string) models.Transaction {
	d, _ := time.Parse("2006-01-02", date)
	t := models.Transaction{
		Date:            d,
		Amount:          amount,
		Description:     desc,
		Status:          status,
		AccountID:       account,
		TransactionType: models.Outflow,
	}
	t.Hash = t.ComputeHash()
	return t
}

// dp1Pending/dp1Posted are the two live rows from the repo data set,
// verbatim (usaa-credit_2026-04-18_to_2026-05-04.csv line 12 and
// usaa-credit_2026-05-01_to_2026-07-12.csv line 183).
func dp1Pending(account string) models.Transaction {
	t := dp1Tx("2026-05-01", -188.98, "Harbor Freight Tools USA", "Pending", account)
	t.OriginalDescription = "Harbor Freight Tools USA"
	t.Category = "Category Pending"
	return t
}

func dp1Posted(account string) models.Transaction {
	t := dp1Tx("2026-05-01", -188.98, "Harbor Freight Tools", "Posted", account)
	t.OriginalDescription = "HARBOR FREIGHT TOOLS3185 PENFIELD     NY"
	t.Category = "Home Improvement"
	return t
}

func TestOracleDP1_HarborFreightPair(t *testing.T) {
	pairs := detectNearDuplicatePairs([]models.Transaction{
		dp1Pending("usaa-credit"), dp1Posted("usaa-credit"),
	})
	if len(pairs) != 1 {
		t.Fatalf("verbatim Harbor Freight pending/posted rows: expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestOracleDP1_Guards(t *testing.T) {
	cases := []struct {
		name string
		txns []models.Transaction
	}{
		{"cross-account", []models.Transaction{dp1Pending("usaa-credit"), dp1Posted("usaa-checking")}},
		{"window-exceeded-4-days", func() []models.Transaction {
			p := dp1Posted("usaa-credit")
			d, _ := time.Parse("2006-01-02", "2026-05-05")
			p.Date = d
			p.Hash = p.ComputeHash()
			return []models.Transaction{dp1Pending("usaa-credit"), p}
		}()},
		{"alien-description", func() []models.Transaction {
			p := dp1Pending("usaa-credit")
			p.Description = "Home Depot"
			p.OriginalDescription = "Home Depot"
			p.Hash = p.ComputeHash()
			return []models.Transaction{p, dp1Posted("usaa-credit")}
		}()},
		{"both-pending", func() []models.Transaction {
			a := dp1Pending("usaa-credit")
			b := dp1Pending("usaa-credit")
			b.Description = "Harbor Freight Tools"
			// Distinct OriginalDescription so shape 2 (same-day reimport)
			// cannot fire; this isolates shape 3's status-split rule.
			b.OriginalDescription = "Harbor Freight Tools"
			b.Hash = b.ComputeHash()
			return []models.Transaction{a, b}
		}()},
		{"both-posted-differing-original", func() []models.Transaction {
			a := dp1Posted("usaa-credit")
			b := dp1Posted("usaa-credit")
			b.Description = "Harbor Freight Tools USA"
			b.OriginalDescription = "Harbor Freight Tools USA"
			b.Hash = b.ComputeHash()
			return []models.Transaction{a, b}
		}()},
		{"empty-status-not-pending-side", func() []models.Transaction {
			a := dp1Pending("usaa-credit")
			a.Status = ""
			a.Hash = a.ComputeHash()
			return []models.Transaction{a, dp1Posted("usaa-credit")}
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectNearDuplicatePairs(tc.txns); len(got) != 0 {
				t.Errorf("expected 0 pairs, got %d", len(got))
			}
		})
	}
}

// TestOracleDP1_KeptBothLucidEV guards the recorded kept_both decision's
// pair: two Posted rows with identical descriptions must still be detected
// exactly as before (shape 2), so the decision keeps binding.
func TestOracleDP1_KeptBothLucidEV(t *testing.T) {
	const desc = "Lucid Moto Ev Chargin Lucidmotors Cca"
	a := dp1Tx("2026-04-14", -25, desc, "Posted", "usaa-credit")
	a.OriginalDescription = desc
	a.Hash = a.ComputeHash()
	b := dp1Tx("2026-04-13", -25, desc, "Posted", "usaa-credit")
	b.OriginalDescription = desc
	b.Hash = b.ComputeHash()
	if got := detectNearDuplicatePairs([]models.Transaction{a, b}); len(got) != 1 {
		t.Fatalf("kept_both Lucid EV shape regressed: expected 1 pair, got %d", len(got))
	}
}

// TestOracleDP1_RealData loads a copy of the repo's live data directory and
// asserts every recorded decision still binds and exactly one NEW pair (the
// Harbor Freight one) enters the queue. Fails on any re-pairing regression.
func TestOracleDP1_RealData(t *testing.T) {
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
	unresolved := loader.UnresolvedDuplicates()
	if len(unresolved) != 11 {
		t.Fatalf("expected exactly 11 unresolved pairs on live data (calibrated 2026-08-30), got %d: %+v", len(unresolved), unresolved)
	}
	hfFound := false
	for _, p := range unresolved {
		pendings := 0
		for _, side := range []models.Transaction{p.Left, p.Right} {
			if strings.Contains(strings.ToLower(side.Status), "pending") {
				pendings++
			}
			if side.Amount == -188.98 && side.Date.Format("2006-01-02") == "2026-05-01" &&
				strings.HasPrefix(strings.ToLower(side.Description), "harbor freight tools") {
				hfFound = true
			}
		}
		if pendings != 1 {
			t.Errorf("pair %s: expected exactly one Pending side, got %d (%q/%q)",
				p.Key, pendings, p.Left.Status, p.Right.Status)
		}
	}
	if !hfFound {
		t.Error("Harbor Freight -188.98 2026-05-01 pair not among the unresolved queue")
	}
	if got := len(loader.ResolvedDuplicates()); got != 5 {
		t.Errorf("resolved decisions no longer bind: expected 5, got %d", got)
	}
	if got := len(loader.KeptBothDuplicates()); got != 1 {
		t.Errorf("kept_both decisions no longer bind: expected 1, got %d", got)
	}
}
