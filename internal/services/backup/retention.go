package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// applyRetention prunes <dir> to: last 7 calendar days (one survivor per
// day, the newest), then for older entries the last 4 ISO weeks (one per
// week, the newest), then for entries older than that the last 3 calendar
// months (one per month, the newest). Anything else is deleted.
func applyRetention(dir string, now time.Time) error {
	entries, err := listBackupTimes(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	keep := selectKeepers(entries, now.UTC())

	keepSet := make(map[string]bool, len(keep))
	for _, e := range keep {
		keepSet[e.path] = true
	}
	for _, e := range entries {
		if keepSet[e.path] {
			continue
		}
		if err := os.Remove(e.path); err != nil {
			return fmt.Errorf("retention: remove %s: %w", e.path, err)
		}
	}
	return nil
}

type backupEntry struct {
	path string
	ts   time.Time
}

func listBackupTimes(dir string) ([]backupEntry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, backupNamePrefix+"*"+backupNameSuffix))
	if err != nil {
		return nil, err
	}
	out := make([]backupEntry, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		stamp := strings.TrimSuffix(strings.TrimPrefix(base, backupNamePrefix), backupNameSuffix)
		ts, err := time.Parse("20060102_150405", stamp)
		if err != nil {
			continue
		}
		out = append(out, backupEntry{path: m, ts: ts.UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.Before(out[j].ts) })
	return out, nil
}

// selectKeepers returns the subset of entries that survive retention.
//
//   - Daily tier  (last 7 calendar days): newest entry per (year, day-of-year).
//   - Weekly tier (older than daily, last 4 ISO weeks): newest per (ISO year, ISO week).
//   - Monthly tier(older than weekly, last 3 calendar months): newest per (year, month).
func selectKeepers(entries []backupEntry, now time.Time) []backupEntry {
	dailyCutoff := now.AddDate(0, 0, -7)
	weeklyCutoff := dailyCutoff.AddDate(0, 0, -7*4)
	monthlyCutoff := weeklyCutoff.AddDate(0, -3, 0)

	type bucketKey struct {
		tier int // 1=daily, 2=weekly, 3=monthly
		k1   int
		k2   int
	}
	bestPerBucket := make(map[bucketKey]backupEntry)

	for _, e := range entries {
		var key bucketKey
		switch {
		case !e.ts.Before(dailyCutoff):
			key = bucketKey{1, e.ts.Year(), e.ts.YearDay()}
		case !e.ts.Before(weeklyCutoff):
			y, w := e.ts.ISOWeek()
			key = bucketKey{2, y, w}
		case !e.ts.Before(monthlyCutoff):
			key = bucketKey{3, e.ts.Year(), int(e.ts.Month())}
		default:
			continue // older than monthly window — drop
		}
		prev, ok := bestPerBucket[key]
		if !ok || e.ts.After(prev.ts) {
			bestPerBucket[key] = e
		}
	}
	out := make([]backupEntry, 0, len(bestPerBucket))
	for _, e := range bestPerBucket {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.After(out[j].ts) })
	return out
}
