package explorer

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestImportOneFile_ProductionWriteBindingIsExclusive pins the production
// binding of importDeps.write to store.CreateExclusive rather than
// store.WriteFile.
//
// Unlike TestImportOneFile_ConcurrentCreateBetweenCheckAndWriteSkipsWithoutOverwrite,
// which stubs deps.write to simulate a race deterministically, this test
// calls defaultImportDeps() with NO stub at all and drives genuinely
// concurrent calls to importOneFile for the same destination name.
// store.CreateExclusive's test-and-create is a single atomic OS step (a hard
// link — see storage.createExclusive), so exactly one goroutine can ever
// win; every other goroutine must observe an error satisfying
// errors.Is(err, os.ErrExist) and report "skipped". store.WriteFile has no
// such step: it always succeeds and silently overwrites, so if
// defaultImportDeps ever reverts to it, every goroutine in this test would
// report "imported" instead of exactly one, and this test fails.
//
// Non-flakiness: the assertion is a count of "imported" outcomes, which is
// binary — exactly 1 under the real binding, more than 1 under a reverted
// one — and does not depend on which goroutine wins, on the raced file's
// final content, or on scheduling order; it only depends on whether the
// underlying OS create is atomic, which os.Link is. Ten independent rounds
// of sixteen goroutines each are run (see acceptance criteria: 20x -race
// runs of the whole test, on top of these in-test rounds) to give the race
// detector many chances to observe contention.
func TestImportOneFile_ProductionWriteBindingIsExclusive(t *testing.T) {
	const goroutines = 16
	const rounds = 10

	for round := 0; round < rounds; round++ {
		dataDir, importDir := setupImportScanEnv(t)
		src := seedImportFile(t, importDir, "race.csv", importCSV)

		deps := defaultImportDeps() // no stub: the real production binding

		var ready sync.WaitGroup
		var start sync.WaitGroup
		ready.Add(goroutines)
		start.Add(1)

		outcomes := make([]importOutcome, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(i int) {
				defer wg.Done()
				ready.Done()
				start.Wait()
				// deleteSource=false: the race this test targets is the
				// destination write, not the source delete, and keeping the
				// source out of it avoids a second, unrelated source of
				// nondeterminism (which goroutine's delete attempt runs
				// first) that has nothing to do with the write binding.
				outcomes[i] = importOneFile("race.csv", false, deps)
			}(i)
		}
		// Release all goroutines together so they contend on the same
		// destination as tightly as possible.
		ready.Wait()
		start.Done()
		wg.Wait()

		imported, skipped, other := 0, 0, 0
		for _, o := range outcomes {
			switch o.Status {
			case "imported":
				imported++
			case "skipped":
				skipped++
				if o.Reason != "already exists in the data folder" {
					t.Errorf("round %d: skipped outcome had unexpected reason %q", round, o.Reason)
				}
			default:
				other++
				t.Errorf("round %d: unexpected outcome %+v", round, o)
			}
		}

		if imported != 1 {
			t.Fatalf("round %d: %d of %d goroutines reported imported (want exactly 1) — "+
				"defaultImportDeps().write is no longer exclusive", round, imported, goroutines)
		}
		if skipped != goroutines-1 {
			t.Fatalf("round %d: %d goroutines reported skipped, want %d", round, skipped, goroutines-1)
		}
		if other != 0 {
			t.Fatalf("round %d: %d goroutines reported neither imported nor skipped", round, other)
		}

		mustExist(t, src, "deleteSource was false; the source must survive regardless of who won the race")

		got, err := store.ReadFile(filepath.Join(dataDir, "race.csv"))
		if err != nil {
			t.Fatalf("round %d: ReadFile destination: %v", round, err)
		}
		if string(got) != importCSV {
			t.Errorf("round %d: destination content = %q, want the untouched source content", round, got)
		}
	}
}
