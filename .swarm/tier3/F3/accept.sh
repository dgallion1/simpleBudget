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

# --- the new branches must be DEFENDED, not merely correct -----------------
# Added at attempt 3. Attempt 2 shipped two correct-but-undefended branches,
# and the test named for one of them never reached it: DisableEncryption's walk
# also collects .encryption-verify, which sorts before a fixture called
# "aaa-broken.csv", so decryptedSoFar was never empty and the nothing-decrypted
# guard went unexercised. Deleting the guard left the whole suite green.
#
# Each check copies the tree, removes one branch, and requires the package
# suite to NOTICE. A green suite here is a failure.
mutate_and_expect_failure() {                      # python-snippet-file check-name
  local snippet="$1" name="$2" d
  d=$(mktemp -d)
  cp -r internal go.mod go.sum "$d"/ 2>/dev/null
  ( cd "$d" && python3 "$snippet" ) || {
    echo "CHECK $name: FAIL (could not apply mutation)"; FAILN=$((FAILN+1)); rm -rf "$d"; return
  }
  ( cd "$d" && go test -count=1 ./internal/services/storage/ >/dev/null 2>&1 )
  if (( $? != 0 )); then
    echo "CHECK $name: PASS (suite detected the mutation)"; PASSN=$((PASSN+1))
  else
    echo "CHECK $name: FAIL (suite passed with the branch removed)"; FAILN=$((FAILN+1))
  fi
  rm -rf "$d"
}

MUT_GUARD=$(mktemp); cat > "$MUT_GUARD" <<'MUTPY'
import re, sys
p = 'internal/services/storage/migration.go'
s = open(p).read()
m = re.search(r'\n[\t]+if len\(decryptedSoFar\) == 0 \{\n[\t]+return decryptErr\n[\t]+\}\n', s)
if not m:
    sys.exit(3)
open(p, 'w').write(s[:m.start()] + '\n' + s[m.end():])
MUTPY
mutate_and_expect_failure "$MUT_GUARD" "07-nothing-decrypted-guard-defended"
rm -f "$MUT_GUARD"

MUT_INV=$(mktemp); cat > "$MUT_INV" <<'MUTPY'
import re, sys
p = 'internal/services/storage/migration.go'
s = open(p).read()
i = s.find('func (s *Storage) rollbackDecryptionWithRecipient')
j = s.find('\n}\n', i)
if i < 0 or j < 0:
    sys.exit(3)
body = s[i:j]
m_inv = re.search(r'([\t]+)s\.invalidateCache\(path\)\n', body)
m_tr = re.search(r'([\t]+)\w+, err := encryptData\([^\n]*\)\n[\t]+if err != nil \{\n(?:.*?\n)*?[\t]+\}\n', body)
if not (m_inv and m_tr):
    sys.exit(3)
inv = m_inv.group(0)
body2 = body[:m_inv.start()] + body[m_inv.end():]
m_tr2 = re.search(re.escape(m_tr.group(0)), body2)
body2 = body2[:m_tr2.end()] + '\n' + inv + body2[m_tr2.end():]
open(p, 'w').write(s[:i] + body2 + s[j:])
MUTPY
mutate_and_expect_failure "$MUT_INV" "08-new-helper-placement-defended"
rm -f "$MUT_INV"

MUT_ENC=$(mktemp); cat > "$MUT_ENC" <<'MUTPY'
import re, sys
p = 'internal/services/storage/migration.go'
s = open(p).read()
i = s.find('func (s *Storage) rollbackDecryptionWithRecipient')
j = s.find('\n}\n', i)
if i < 0 or j < 0:
    sys.exit(3)
body = s[i:j]
m = re.search(r'(encrypted, err := encryptData\([^\n]*\)\n[\t]+if err != nil \{\n)[\t]+failed = append\(failed, path\)\n', body)
if not m:
    sys.exit(3)
open(p, 'w').write(s[:i] + body[:m.start()] + m.group(1) + body[m.end():] + s[j:])
MUTPY
mutate_and_expect_failure "$MUT_ENC" "09-encrypt-failure-reporting-defended"
rm -f "$MUT_ENC"

MUT_AW=$(mktemp); cat > "$MUT_AW" <<'MUTPY'
import re, sys
p = 'internal/services/storage/migration.go'
s = open(p).read()
i = s.find('func (s *Storage) rollbackDecryptionWithRecipient')
j = s.find('\n}\n', i)
if i < 0 or j < 0:
    sys.exit(3)
body = s[i:j]
m = re.search(r'(if err := s\.atomicWrite\([^\n]*\); err != nil \{\n)[\t]+failed = append\(failed, path\)\n', body)
if not m:
    sys.exit(3)
open(p, 'w').write(s[:i] + body[:m.start()] + m.group(1) + body[m.end():] + s[j:])
MUTPY
mutate_and_expect_failure "$MUT_AW" "10-atomicwrite-failure-reporting-defended"
rm -f "$MUT_AW"

# No separate race check here: checks 02 and 03 each run F1's and F2's oracles,
# and both of those run the package suite under -race against this same tree.
# A third run would cost five minutes per arm to re-establish what two other
# checks already established.

go build ./... >/dev/null 2>&1; ck "05-build" 0 "$?"
go vet ./... >/dev/null 2>&1;  ck "06-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 ))
