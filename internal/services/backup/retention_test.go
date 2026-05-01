package backup

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func makeBackup(t *testing.T, dir string, ts time.Time) string {
	t.Helper()
	name := backupNamePrefix + ts.UTC().Format("20060102_150405") + backupNameSuffix
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("dummy"), 0600); err != nil { t.Fatal(err) }
	return name
}

func listBackups(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil { t.Fatal(err) }
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == backupNameSuffix {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestRetention_KeepsLast7Daily(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// 10 distinct days, one backup per day.
	for i := 0; i < 10; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// Daily window keeps days 0..6 (7 entries). Days 7,8,9 must be pruned
	// unless they happen to be the newest in their ISO week — for May 2026
	// 7 days back from the 1st falls inside the previous week so it survives
	// as that week's representative. Worst case: 7 daily + up to 4 weekly.
	if len(got) < 7 || len(got) > 7+4 {
		t.Fatalf("expected 7..11 backups after daily retention, got %d: %v", len(got), got)
	}
}

func TestRetention_KeepsLast4WeeklyOlderThanDaily(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// 60 days back, 6 weeks beyond the daily window.
	for i := 0; i < 60; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// 7 daily + 4 weekly (older than the daily window) + at most 3 monthly
	// (older than the weekly window). Upper bound 14 ish.
	if len(got) < 7+4 || len(got) > 7+4+3+1 {
		t.Fatalf("expected ~11..15 backups, got %d: %v", len(got), got)
	}
}

func TestRetention_KeepsLast3MonthlyOlderThanWeekly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// Daily backups for 7 days + a single backup per month for the last 6 months.
	for i := 0; i < 7; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	for m := 1; m <= 6; m++ {
		makeBackup(t, dir, now.AddDate(0, -m, 0))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// 7 daily + at most 3 monthly survivors from the 6 we made.
	if len(got) < 7 || len(got) > 7+3+4 {
		t.Fatalf("expected ~7..14 backups, got %d: %v", len(got), got)
	}
}

func TestRetention_NoBackupsIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := applyRetention(dir, time.Now()); err != nil { t.Fatal(err) }
}

func TestRetention_IgnoresNonBackupFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(filepath.Join(dir, "last_backup.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "random.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	makeBackup(t, dir, now)
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(dir, "last_backup.json")); err != nil {
		t.Errorf("retention should not touch last_backup.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "random.txt")); err != nil {
		t.Errorf("retention should not touch random files: %v", err)
	}
}
