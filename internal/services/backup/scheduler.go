package backup

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Run starts the daily-tick scheduler. It blocks until ctx is cancelled.
// On entry it runs SnapshotIfStale once, then ticks every hour.
func (s *Service) Run(ctx context.Context, maxAge time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	s.runWith(ctx, ticker.C, maxAge)
}

// runWith is the testable variant: callers inject the tick channel.
func (s *Service) runWith(ctx context.Context, ticks <-chan time.Time, maxAge time.Duration) {
	if s.Enabled() {
		if err := s.SnapshotIfStale(ctx, maxAge); err != nil {
			fmt.Fprintf(os.Stderr, "backup: initial snapshot: %v\n", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if !s.Enabled() {
				continue
			}
			if err := s.SnapshotIfStale(ctx, maxAge); err != nil {
				fmt.Fprintf(os.Stderr, "backup: scheduled snapshot: %v\n", err)
			}
		}
	}
}
