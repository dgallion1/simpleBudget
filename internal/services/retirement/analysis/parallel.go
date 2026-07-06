package analysis

import "sync"

// parallelIndexed runs fn(i) for i in [0,n) on at most workers goroutines.
// A panic in fn is captured and re-thrown on the CALLING goroutine after all
// workers finish, so callers' panic semantics match a sequential loop and
// HTTP middleware (e.g. chi's Recoverer) can recover it instead of the
// process dying from an unrecovered goroutine panic. Only the first panic
// value is re-thrown; the remaining indices still complete, so fixed-slot
// result arrays are fully populated for the survivors.
func parallelIndexed(n, workers int, fn func(i int)) {
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

	var (
		panicOnce sync.Once
		panicked  bool
		panicVal  any
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
							panicOnce.Do(func() {
								panicked = true
								panicVal = r
							})
						}
					}()
					fn(i)
				}()
			}
		}()
	}
	wg.Wait()

	// wg.Wait() establishes happens-before with every worker, so reading
	// panicked/panicVal here is race-free.
	if panicked {
		panic(panicVal)
	}
}

// ParallelIndexed exposes parallelIndexed for the retirement orchestrator,
// which fans out its analysis branches with the same panic-capture contract.
func ParallelIndexed(n, workers int, fn func(i int)) {
	parallelIndexed(n, workers, fn)
}
