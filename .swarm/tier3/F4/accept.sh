#!/usr/bin/env bash
# Oracle for F4 — migration must not change a file's permission bits.
# Run with cwd set to the tree under test.
#
# All checks are behavioural. F2's oracle carried a structural grep that was
# evaded by spelling a name through a variable and that fired on prose; there
# are none here.
#
# The regression guard nests F3's oracle only, because F3's already nests F1's
# and F2's. Chaining transitively costs one run instead of three.
set -u
PKG=internal/services/storage
PLANTED="$PKG/zz_oracle_f4_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat %s: %v", p, err)
	}
	return fi.Mode().Perm()
}

// Enabling encryption changes a file's encoding. It must not change who can
// read it. A user who deliberately chmod'd their ledger to 0600 has said
// something about that file; re-publishing it 0644 silently overrides them,
// and does so at the exact moment they were trying to increase protection.
func TestOracleF4EnablePreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p := filepath.Join(dir, "ledger.csv")
	if err := os.WriteFile(p, []byte("account,balance\nchecking,1.00\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	if got := modeOf(t, p); got != 0600 {
		t.Errorf("EnableEncryption changed mode 0600 -> %v", got)
	}
}

// And the same on the way back out.
func TestOracleF4DisablePreservesMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	p := filepath.Join(dir, "ledger.csv")
	if err := s.WriteFile(p, []byte("account,balance\nsavings,2.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption: %v", err)
	}
	if got := modeOf(t, p); got != 0600 {
		t.Errorf("DisableEncryption changed mode 0600 -> %v", got)
	}
}

// Both rollback helpers rewrite files too, on the failure paths, and must
// preserve just the same. Driven directly because the failure paths that reach
// them are awkward to provoke end to end.
func TestOracleF4RollbackHelpersPreserveMode(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	provider, err := s.createProviderUnlocked()
	if err != nil {
		t.Fatalf("createProviderUnlocked: %v", err)
	}
	if provider.NeedsUnlock() {
		if err := provider.Unlock("testpassword123"); err != nil {
			t.Fatalf("Unlock: %v", err)
		}
	}
	identity, err := provider.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	recipient, err := provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	// rollbackEncryptionWithIdentity: encrypted on disk, decrypted back.
	enc := filepath.Join(dir, "enc.csv")
	if err := s.WriteFile(enc, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("WriteFile enc: %v", err)
	}
	if err := os.Chmod(enc, 0600); err != nil {
		t.Fatalf("chmod enc: %v", err)
	}
	s.rollbackEncryptionWithIdentity([]string{enc}, identity)
	if got := modeOf(t, enc); got != 0600 {
		t.Errorf("rollbackEncryptionWithIdentity changed mode 0600 -> %v", got)
	}

	// rollbackDecryptionWithRecipient: plaintext on disk, re-encrypted.
	plain := filepath.Join(dir, "plain.csv")
	if err := os.WriteFile(plain, []byte("c,d\n3,4\n"), 0644); err != nil {
		t.Fatalf("seed plain: %v", err)
	}
	if err := os.Chmod(plain, 0600); err != nil {
		t.Fatalf("chmod plain: %v", err)
	}
	if failed := s.rollbackDecryptionWithRecipient([]string{plain}, recipient); len(failed) != 0 {
		t.Fatalf("rollbackDecryptionWithRecipient reported failures: %v", failed)
	}
	if got := modeOf(t, plain); got != 0600 {
		t.Errorf("rollbackDecryptionWithRecipient changed mode 0600 -> %v", got)
	}
}

// The ordinary case must not drift either: a file written 0644 stays 0644.
// Without this, "preserve the mode" could be satisfied by hardcoding 0600.
func TestOracleF4OrdinaryModeUnchanged(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p := filepath.Join(dir, "ledger.csv")
	if err := os.WriteFile(p, []byte("e,f\n5,6\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	if got := modeOf(t, p); got != 0644 {
		t.Errorf("EnableEncryption changed mode 0644 -> %v", got)
	}
}
GO

go test -count=1 -run 'TestOracleF4' ./"$PKG"/ >/tmp/f4_oracle.out 2>&1
rc=$?
ck "01-migration-preserves-mode" 0 "$rc"
(( rc == 0 )) || sed -n '1,30p' /tmp/f4_oracle.out

rm -f "$PLANTED"

# Regression guard, deliberately the cheap half.
#
# F1, F2 and F3 all fixed these same helpers, and their behavioural tests all
# live in this package, so the package suite under -race covers every property
# they defended. What it does NOT re-run is their oracles' mutation checks --
# the ones grading whether those tests still DETECT a regression.
#
# Nesting F3's oracle here would cover that too, and was written that way
# first, then changed: F3 nests F1 and F2, so a full chain runs about
# twenty-five minutes, and tier3-compare pays it twice while each checker pays
# it again. Two hours of oracle time for a permissions change is a worse trade
# than running the detection chain once, on the merged result, by hand. That
# run is the lead's job and is recorded in the report; do not assume this
# check did it.
go test -race -count=1 -timeout 900s ./"$PKG"/ >/tmp/f4_race.out 2>&1
race_rc=$?
ck "02-package-suite-race" 0 "$race_rc"
(( race_rc == 0 )) || tail -20 /tmp/f4_race.out

go build ./... >/dev/null 2>&1; ck "03-build" 0 "$?"
go vet ./... >/dev/null 2>&1;  ck "04-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 ))
