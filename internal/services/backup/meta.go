package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const metaFileName = "last_backup.json"

// Meta is the on-disk record of the most recent successful backup plus
// the most recent attempt outcome.
type Meta struct {
	// TS / FileCount / TotalBytes / Encrypted reflect the most recent
	// SUCCESSFUL backup. They are not overwritten by a failed attempt.
	TS         string `json:"ts"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	Encrypted  bool   `json:"encrypted"`

	// LastError and LastAttemptTS reflect the most recent attempt
	// (success or failure). LastError is empty when the most recent
	// attempt succeeded.
	LastError     string `json:"last_error"`
	LastAttemptTS string `json:"last_attempt_ts"`
}

func metaPath(dir string) string { return filepath.Join(dir, metaFileName) }

func loadMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(metaPath(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, nil
		}
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("backup: parse meta: %w", err)
	}
	return m, nil
}

func writeMetaAtomic(dir string, m Meta) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := metaPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, metaPath(dir))
}

func writeMetaSuccess(dir string, m Meta) error {
	m.LastError = ""
	if m.LastAttemptTS == "" {
		m.LastAttemptTS = m.TS
	}
	return writeMetaAtomic(dir, m)
}

// writeMetaFailure preserves prior successful TS/FileCount/TotalBytes/
// Encrypted and only updates LastError + LastAttemptTS.
func writeMetaFailure(dir, reason string, now time.Time) error {
	prior, err := loadMeta(dir)
	if err != nil {
		// Best-effort: even if prior is unreadable, still record the failure.
		prior = Meta{}
	}
	prior.LastError = reason
	prior.LastAttemptTS = now.UTC().Format("20060102_150405")
	return writeMetaAtomic(dir, prior)
}
