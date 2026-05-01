package backup

import "time"

// Clock is the minimal time interface the backup service depends on.
// Tests inject a fake; production uses realClock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
