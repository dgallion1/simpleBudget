package accounts

// Tier-3 acceptance oracle for R1 (serialized accounts read-modify-write).
// Lead-authored before dispatch; copied into the package by accept.sh and
// removed afterwards. Both blind implementations are judged against THIS
// file, so neither worker may edit it.
//
// Required API (pinned in .swarm/briefs/R1.md):
//
//	func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error
//
// Mutate performs load -> fn -> save inside one held section. fn returning
// an error aborts without saving.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

func zzOracleR1Store(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := Save(s, []models.Account{{
		ID: "chk", Name: "Checking", Kind: models.AccountKindChecking,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return s
}

// Check 1 — the defect itself. N concurrent mutations, each adding a distinct
// anchor. Every one must survive; the bare Load/modify/Save this replaces
// loses most of them. Run under -race.
func TestZZOracleR1_ConcurrentMutateLosesNoWrites(t *testing.T) {
	s := zzOracleR1Store(t)
	const n = 32

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)
			errs <- Mutate(s, func(accts []models.Account) ([]models.Account, error) {
				for j := range accts {
					if accts[j].ID == "chk" {
						accts[j].Anchors = append(accts[j].Anchors, models.BalanceAnchor{
							Date: day, Amount: float64(i), Note: fmt.Sprintf("a%d", i),
						})
						return accts, nil
					}
				}
				return nil, fmt.Errorf("account chk vanished mid-run")
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Mutate returned an error: %v", err)
		}
	}

	got, err := Load(s)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	acct := Find(got, "chk")
	if acct == nil {
		t.Fatal("account chk is gone")
	}
	if len(acct.Anchors) != n {
		t.Fatalf("anchors = %d, want %d — concurrent mutations overwrote each other", len(acct.Anchors), n)
	}
	seen := make(map[string]bool, n)
	for _, a := range acct.Anchors {
		k := a.Date.Format("2006-01-02")
		if seen[k] {
			t.Fatalf("duplicate anchor date %s — a mutation was applied twice", k)
		}
		seen[k] = true
	}
}

// Check 2 — an aborting fn writes nothing.
func TestZZOracleR1_MutateErrorSavesNothing(t *testing.T) {
	s := zzOracleR1Store(t)
	want := fmt.Errorf("deliberate abort")

	err := Mutate(s, func(accts []models.Account) ([]models.Account, error) {
		for j := range accts {
			accts[j].Name = "CLOBBERED"
		}
		return accts, want
	})
	if err == nil {
		t.Fatal("Mutate returned nil for an fn that failed")
	}

	got, lerr := Load(s)
	if lerr != nil {
		t.Fatalf("Load: %v", lerr)
	}
	if acct := Find(got, "chk"); acct == nil || acct.Name != "Checking" {
		t.Fatalf("account was written despite the fn error: %+v", acct)
	}
}

// Check 3 — the restore-resurrection half of the defect, and the lock order.
// A restore takes storage's exclusive hold. If Mutate's load and save are not
// inside one shared hold, the exclusive hold is granted mid-section and the
// save that follows resurrects pre-restore state.
func TestZZOracleR1_MutateExcludesTheExclusiveHold(t *testing.T) {
	s := zzOracleR1Store(t)

	inFn := make(chan struct{})
	release := make(chan struct{})
	granted := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- Mutate(s, func(a []models.Account) ([]models.Account, error) {
			close(inFn)
			<-release
			return a, nil
		})
	}()
	<-inFn

	go func() {
		w := s.BeginExclusive()
		close(granted)
		w.Release()
	}()

	select {
	case <-granted:
		close(release)
		t.Fatal("BeginExclusive was granted while a Mutate section was open — " +
			"the accounts read-modify-write does not exclude a restore")
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	select {
	case <-granted:
	case <-time.After(5 * time.Second):
		t.Fatal("BeginExclusive was never granted after Mutate returned — the hold leaked")
	}
}
