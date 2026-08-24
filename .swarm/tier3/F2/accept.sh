#!/usr/bin/env bash
# Oracle for F2 — the migration helpers must publish atomically, and must not
# stage at the legacy fixed name. Run with cwd set to the tree under test.
#
# Plants its own in-package test and removes it, so it does not depend on what
# either blind arm names its tests.
set -u
PKG=internal/services/storage
PLANTED="$PKG/zz_oracle_f2_test.go"
PASSN=0; FAILN=0
ck() { if [[ "$2" == "$3" ]]; then echo "CHECK $1: PASS"; PASSN=$((PASSN+1));
       else echo "CHECK $1: FAIL (want $2, got $3)"; FAILN=$((FAILN+1)); fi; }
cleanup() { rm -f "$PLANTED"; }
trap cleanup EXIT

cat > "$PLANTED" <<'GO'
package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A read-only destination is how this repo already injects write failures
// elsewhere (see the CI workflow's note on running as an unprivileged user).
// It also happens to discriminate exactly the property under test: os.WriteFile
// on a 0444 file fails with EACCES, while staging beside it and renaming over
// it succeeds, because rename needs write permission on the DIRECTORY, not on
// the target. So this passes only for a writer that publishes by rename.
func TestOracleF2RollbackPublishesByRename(t *testing.T) {
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

	path := filepath.Join(dir, "ledger.csv")
	secret := []byte("account,balance\nchecking,4242.42\n")
	if err := s.WriteFile(path, secret, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !isAgeEncrypted(raw) {
		t.Fatalf("precondition: file is not encrypted on disk")
	}

	// Make the destination unwritable. A direct os.WriteFile cannot publish
	// here; a stage-and-rename can.
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0644) }()

	s.rollbackEncryptionWithIdentity([]string{path}, identity)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after rollback: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("rollback did not restore the plaintext through a read-only destination: got %q", got)
	}
}

// No staging leftovers may survive a completed migration, in either direction.
func TestOracleF2NoStagingLeftovers(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i, name := range []string{"a.csv", "b.csv", "c.json"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x,y\n1,2\n"), 0644); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if IsStagingName(e.Name()) {
			t.Errorf("staging leftover after a completed round trip: %s", e.Name())
		}
	}
}
GO

go test -count=1 -run 'TestOracleF2' ./"$PKG"/ >/tmp/f2_oracle.out 2>&1
rc=$?
ck "01-atomic-publish-and-no-leftovers" 0 "$rc"
(( rc == 0 )) || sed -n '1,25p' /tmp/f2_oracle.out

rm -f "$PLANTED"

# Structural: the legacy fixed staging name must be gone from this file. A
# naming convention is structural by nature, so this check is too, and it says
# so rather than pretending to be behavioural.
if grep -qE '\+ "\.tmp"' "$PKG"/migration.go; then
  echo "CHECK 02-no-legacy-fixed-staging-name: FAIL (migration.go still stages at path + \".tmp\")"; FAILN=$((FAILN+1))
else
  echo "CHECK 02-no-legacy-fixed-staging-name: PASS"; PASSN=$((PASSN+1))
fi

# F1's properties must survive: this task edits the same helpers.
go test -count=1 -run 'Cache|Migration|Rollback|Encryption' ./"$PKG"/ >/dev/null 2>&1
ck "03-f1-properties-intact" 0 "$?"

go test -race -count=1 -timeout 600s ./"$PKG"/ >/tmp/f2_race.out 2>&1
race_rc=$?
ck "04-package-suite-race" 0 "$race_rc"
(( race_rc == 0 )) || tail -20 /tmp/f2_race.out

go build ./... >/dev/null 2>&1; ck "05-build" 0 "$?"
go vet ./... >/dev/null 2>&1;  ck "06-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 ))
