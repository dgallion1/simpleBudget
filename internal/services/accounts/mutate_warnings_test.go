package accounts

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestMutate_LogsOverlapWarningsEvenWhenWriteFails is Attempt 3 (2026-08-19)
// regression coverage. saveTx (Mutate's save path) used to log
// OverlapWarnings only after a successful tx.WriteFile; Save/SaveWithWarnings
// always logged the already-computed warnings unconditionally, including
// when the write errored. This pins the restored unconditional behaviour
// through the Mutate path specifically, since that is where the regression
// was.
func TestMutate_LogsOverlapWarningsEvenWhenWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fixtures do not block root")
	}
	dir, s := newStore(t)

	// A CSV both accounts' patterns match, so OverlapWarnings has something
	// to say.
	if err := os.WriteFile(filepath.Join(dir, "acme-checking.csv"), []byte("Date,Description,Amount\n"), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	// Make the write fail: no write permission on the store's directory
	// means the save's temp-file create fails, but the (nonexistent, so
	// IsNotExist) accounts.json read that Mutate does first still succeeds.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod 0500 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	accts := []models.Account{
		{ID: "a-acme", Name: "Acme A", FilePatterns: []string{"acme"}},
		{ID: "b-acme", Name: "Acme B", FilePatterns: []string{"acme"}},
	}
	err := Mutate(s, func([]models.Account) ([]models.Account, error) {
		return accts, nil
	})
	if err == nil {
		t.Fatalf("Mutate succeeded despite a read-only store dir; the write should have failed")
	}

	logged := buf.String()
	if !strings.Contains(logged, "Warning:") || !strings.Contains(logged, "matches multiple accounts") {
		t.Errorf("expected the overlap warning to be logged even though the write failed; got log output: %q", logged)
	}
}
