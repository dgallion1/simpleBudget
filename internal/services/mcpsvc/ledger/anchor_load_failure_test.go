package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/mcpsvc/confirm"
)

// Attempt 3 (2026-08-19) regression coverage: applyAnchor previously gave a
// distinct message for a LoadAccounts failure ("cannot reload accounts
// before writing: %w" -- no snapNote, no "confirmation token has been
// spent" wording) versus a SaveAccounts failure (wrapped in the snapNote
// and the "confirmation token has been spent" wording, since the write --
// and the token's effect -- was actually attempted). Folding the load and
// the save into one accounts.Mutate closure collapsed both into the save
// wording.
//
// These call applyAnchor directly (same package) so a corrupt accounts.json
// can be staged AFTER a valid token is minted: going through the public
// set_balance_anchor tool would fail earlier, at registerSetBalanceAnchor's
// own LoadAccounts pre-check (used to validate the account exists before
// minting/redeeming), which is a different call site not in this task's
// scope and would test the wrong code path.

func TestApplyAnchor_LoadFailureMessageIsDistinct(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})

	date, err := time.Parse("2006-01-02", "2026-08-15")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	tokenArgs := anchorTokenArgs{AccountID: "checking", Date: "2026-08-15", Amount: 4210.55}
	token, _, err := deps.Confirm.Mint("set_balance_anchor", tokenArgs)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Corrupt accounts.json AFTER minting: applyAnchor's own Mutate load
	// (not the outer pre-check) is what must fail here. ensureSnapshot
	// copies raw bytes without parsing, so it is unaffected.
	if err := os.WriteFile(filepath.Join(dir, accounts.AccountsFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("corrupt accounts.json: %v", err)
	}

	_, _, err = applyAnchor(deps, "checking", date, 4210.55, "", tokenArgs, token, confirm.NotAsked)
	if err == nil {
		t.Fatal("applyAnchor succeeded despite a corrupt accounts.json")
	}
	if !strings.Contains(err.Error(), "cannot reload accounts before writing") {
		t.Errorf("error = %q, want it to name the load failure with the original distinct wording", err.Error())
	}
	if strings.Contains(err.Error(), "confirmation token has been spent") {
		t.Errorf("error = %q, a bare load failure must not carry the save-failure's spent-token wording", err.Error())
	}
}

func TestApplyAnchor_SaveFailureMessageIsUnchanged(t *testing.T) {
	deps, dir := newDeps(t)
	seedAccounts(t, deps, []models.Account{{
		ID:   "checking",
		Name: "Checking",
		Kind: models.AccountKindChecking,
	}})

	date, err := time.Parse("2006-01-02", "2026-08-15")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	tokenArgs := anchorTokenArgs{AccountID: "checking", Date: "2026-08-15", Amount: 4210.55}
	token, _, err := deps.Confirm.Mint("set_balance_anchor", tokenArgs)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	if os.Geteuid() == 0 {
		t.Skip("permission fixtures do not block root")
	}
	// Pre-create the snapshot directory so ensureSnapshot's MkdirAll (a
	// no-op once it exists) and its .bak write (which happens inside that
	// already-existing directory, not dir itself) both succeed once dir is
	// read-only. Only the accounts.json write inside dir must fail.
	if err := os.MkdirAll(filepath.Join(dir, "snapshots"), 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod 0500 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, _, err = applyAnchor(deps, "checking", date, 4210.55, "", tokenArgs, token, confirm.NotAsked)
	if err == nil {
		t.Fatal("applyAnchor succeeded despite a read-only store directory")
	}
	if !strings.Contains(err.Error(), "confirmation token has been spent either way") {
		t.Errorf("error = %q, want the original save-failure wording preserved", err.Error())
	}
	if strings.Contains(err.Error(), "cannot reload accounts before writing") {
		t.Errorf("error = %q, a save failure must not carry the load-failure message", err.Error())
	}
}
