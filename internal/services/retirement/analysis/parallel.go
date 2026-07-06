package analysis

import (
	"log"
	"runtime/debug"
	"sync"
)

// ParallelIndexed runs fn(i) for i in [0,n) on at most workers goroutines.
// It is the single bounded fan-out used by every analysis loop (sensitivity,
// failure points, Monte Carlo, backtest) and by the retirement orchestrator's
// branch fan-out.
//
// A panic in fn is captured and re-thrown on the CALLING goroutine after all
// workers finish, so callers' panic semantics match a sequential loop and
// HTTP middleware (e.g. chi's Recoverer) can recover it instead of the
// process dying from an unrecovered goroutine panic. Only the first panic
// value is re-thrown, but EVERY panic — including the worker's stack, which
// would otherwise be lost in the hop to the calling goroutine — is logged at
// capture time, so production logs always show the faulting frame and no
// concurrent secondary panic disappears silently. The remaining indices
// still complete, so fixed-slot result arrays are fully populated for the
// survivors.
func ParallelIndexed(n, workers int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}

	idx := make(chan int, n)
	for i := 0; i < n; i++ {
		idx <- i
	}
	close(idx)

	// First panic value wins; panicVal != nil is the "panicked" signal
	// (recover() in the guard below never yields nil: Go converts
	// panic(nil) to *runtime.PanicNilError).
	var (
		panicMu  sync.Mutex
		panicVal any
	)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("analysis: parallel worker panic (index %d): %v\n%s", i, r, debug.Stack())
							panicMu.Lock()
							if panicVal == nil {
								panicVal = r
							}
							panicMu.Unlock()
						}
					}()
					fn(i)
				}()
			}
		}()
	}
	wg.Wait()

	// wg.Wait() establishes happens-before with every worker, so reading
	// panicVal here is race-free.
	if panicVal != nil {
		panic(panicVal)
	}
}
