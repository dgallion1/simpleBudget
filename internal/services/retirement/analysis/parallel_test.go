package analysis

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelIndexedRunsAllSlots(t *testing.T) {
	const n = 100
	got := make([]int, n)
	ParallelIndexed(n, 7, func(i int) { got[i] = i * 2 })
	for i, v := range got {
		if v != i*2 {
			t.Fatalf("slot %d = %d, want %d", i, v, i*2)
		}
	}
}

func TestParallelIndexedWorkerBounds(t *testing.T) {
	// workers > n and workers < 1 must both be tolerated.
	var calls atomic.Int32
	ParallelIndexed(3, 64, func(int) { calls.Add(1) })
	if calls.Load() != 3 {
		t.Fatalf("workers>n: calls = %d, want 3", calls.Load())
	}
	calls.Store(0)
	ParallelIndexed(3, 0, func(int) { calls.Add(1) })
	if calls.Load() != 3 {
		t.Fatalf("workers=0: calls = %d, want 3", calls.Load())
	}
}

func TestParallelIndexedNonPositiveN(t *testing.T) {
	called := false
	ParallelIndexed(0, 4, func(int) { called = true })
	ParallelIndexed(-5, 4, func(int) { called = true })
	if called {
		t.Fatal("fn must not be called for n <= 0")
	}
}

// TestParallelIndexedPanicRethrownOnCaller pins the panic contract: a
// panic in fn (perturb.go panics by design) must resurface on the CALLING
// goroutine with the same value — so HTTP middleware can recover it — and
// all non-panicking indices must still complete, with no worker
// goroutines left behind.
func TestParallelIndexedPanicRethrownOnCaller(t *testing.T) {
	const n = 50
	const panicIdx = 17
	sentinel := "retirement: perturbation produced invalid settings"
	var completed atomic.Int32
	baseline := runtime.NumGoroutine()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected the worker panic to resurface on the calling goroutine")
		}
		if r != sentinel {
			t.Fatalf("panic value = %v, want %v", r, sentinel)
		}
		if got := completed.Load(); got != n-1 {
			t.Fatalf("completed = %d, want %d (all non-panicking work must finish)", got, n-1)
		}
		// ParallelIndexed only returns after wg.Wait, so no worker may
		// outlive the call. Allow a few retries for exiting goroutines to
		// be reaped by the scheduler.
		for range 50 {
			if runtime.NumGoroutine() <= baseline {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		if now := runtime.NumGoroutine(); now > baseline {
			t.Fatalf("goroutines leaked: %d before, %d after", baseline, now)
		}
	}()

	ParallelIndexed(n, 4, func(i int) {
		if i == panicIdx {
			panic(sentinel)
		}
		completed.Add(1)
	})
	t.Fatal("unreachable: ParallelIndexed must re-panic on the caller")
}

// TestParallelIndexedFirstPanicWins: with several panicking indices, the
// caller sees exactly one of the panic values (the first captured), and
// every non-panicking index still completes.
func TestParallelIndexedFirstPanicWins(t *testing.T) {
	const n = 20
	panickers := map[int]bool{3: true, 9: true, 15: true}
	var completed atomic.Int32

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic to resurface")
		}
		s, ok := r.(string)
		if !ok || s[:5] != "boom-" {
			t.Fatalf("panic value = %v, want one of the boom-* sentinels", r)
		}
		if got := completed.Load(); got != int32(n-len(panickers)) {
			t.Fatalf("completed = %d, want %d", got, n-len(panickers))
		}
	}()

	ParallelIndexed(n, 3, func(i int) {
		if panickers[i] {
			panic("boom-" + string(rune('0'+i)))
		}
		completed.Add(1)
	})
	t.Fatal("unreachable: ParallelIndexed must re-panic on the caller")
}
