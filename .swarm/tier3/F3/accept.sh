#!/usr/bin/env bash
# Oracle for F3 — a DisableEncryption that fails partway must not leave
# plaintext on disk. Run with cwd set to the tree under test.
#
# Plants its own in-package test and removes it, so it does not depend on what
# either blind arm names its tests.
#
# NOTE on a lesson from F2: this oracle contains no structural grep. F2's
# check 02 grepped for a string literal and was evaded by spelling the same
# thing through a variable, while also firing on prose in comments. Every check
# here is behavioural.
set -u
PKG=internal/services/storage
PLANTED="$PKG/zz_oracle_f3_test.go"
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
	"strings"
	"testing"
)

// seedEncrypted builds a store with three encrypted data files and corrupts the
// last one in walk order so that it still looks encrypted -- isAgeEncrypted
// only inspects the header -- but cannot be decrypted. filepath.Walk is
// lexical, so "zz.csv" is reached after the other two have been decrypted,
// which is what makes the partial-failure state deterministic.
func seedPartialFailure(t *testing.T) (*Storage, string, []string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	good := []string{filepath.Join(dir, "aa.csv"), filepath.Join(dir, "bb.csv")}
	for i, p := range good {
		if err := s.WriteFile(p, []byte("account,balance\nrow,100.0"+string(rune('0'+i))+"\n"), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	bad := filepath.Join(dir, "zz.csv")
	if err := os.WriteFile(bad, append([]byte(ageHeader), []byte("\nnot actually decryptable\n")...), 0644); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	if !isAgeEncrypted(mustRead(t, bad)) {
		t.Fatalf("precondition: corrupt seed does not read as encrypted")
	}
	return s, dir, good
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", p, err)
	}
	return b
}

// The property. A DisableEncryption that fails partway must not leave files it
// already decrypted sitting as plaintext on disk: the call reports failure and
// s.encrypted stays true, so the user is entitled to believe the data is still
// encrypted. Every data file must still be encrypted afterwards.
func TestOracleF3PartialFailureLeavesNoPlaintext(t *testing.T) {
	s, _, good := seedPartialFailure(t)

	err := s.DisableEncryption("testpassword123")
	if err == nil {
		t.Fatalf("DisableEncryption unexpectedly succeeded on an undecryptable file")
	}

	for _, p := range good {
		if !isAgeEncrypted(mustRead(t, p)) {
			t.Errorf("%s is plaintext on disk after a failed DisableEncryption", filepath.Base(p))
		}
	}
}

// The failure must be legible. The error names the file that could not be
// decrypted; if anything could not be restored, it says so rather than leaving
// the caller to discover plaintext later.
func TestOracleF3PartialFailureIsReported(t *testing.T) {
	s, _, _ := seedPartialFailure(t)

	err := s.DisableEncryption("testpassword123")
	if err == nil {
		t.Fatalf("DisableEncryption unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "zz.csv") {
		t.Errorf("error does not name the file that failed: %v", err)
	}
}

// The store must not claim to be decrypted when it is not.
func TestOracleF3PartialFailureKeepsEncryptedState(t *testing.T) {
	s, dir, _ := seedPartialFailure(t)

	_ = s.DisableEncryption("testpassword123")

	if !s.encrypted {
		t.Errorf("s.encrypted is false after a failed DisableEncryption")
	}
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		t.Errorf("marker file removed despite the failure: %v", err)
	}
}

// The success path must be untouched.
func TestOracleF3SuccessPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	p := filepath.Join(dir, "aa.csv")
	want := []byte("account,balance\nchecking,77.77\n")
	if err := s.WriteFile(p, want, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption on a clean store: %v", err)
	}
	if isAgeEncrypted(mustRead(t, p)) {
		t.Errorf("file still encrypted after a successful DisableEncryption")
	}
	if s.encrypted {
		t.Errorf("s.encrypted still true after a successful DisableEncryption")
	}
}
GO

go test -count=1 -run 'TestOracleF3' ./"$PKG"/ >/tmp/f3_oracle.out 2>&1
rc=$?
ck "01-partial-failure-behaviour" 0 "$rc"
(( rc == 0 )) || sed -n '1,30p' /tmp/f3_oracle.out

rm -f "$PLANTED"

# F1 and F2 both fixed these same helpers. Their oracles are the regression
# guard: this task must not undo either.
bash .swarm/tier3/F1/accept.sh >/tmp/f3_f1.out 2>&1
ck "02-f1-oracle-still-green" 0 "$?"
(( PASSN >= 0 )) && tail -2 /tmp/f3_f1.out | head -1
bash .swarm/tier3/F2/accept.sh >/tmp/f3_f2.out 2>&1
ck "03-f2-oracle-still-green" 0 "$?"
tail -2 /tmp/f3_f2.out | head -1

# No separate race check here: checks 02 and 03 each run F1's and F2's oracles,
# and both of those run the package suite under -race against this same tree.
# A third run would cost five minutes per arm to re-establish what two other
# checks already established.

go build ./... >/dev/null 2>&1; ck "05-build" 0 "$?"
go vet ./... >/dev/null 2>&1;  ck "06-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 ))
