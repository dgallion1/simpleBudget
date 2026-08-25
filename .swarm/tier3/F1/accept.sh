#!/usr/bin/env bash
# Oracle for F1 — migration.go must not leave plaintext resident in the cache.
# Run with cwd set to the tree under test.
#
# It plants its OWN in-package test and removes it afterwards, so it does not
# depend on what either blind arm decides to name its tests. The arms are free
# to structure their own coverage however they like; this grades the property.
set -u
PKG=internal/services/storage
PLANTED="$PKG/zz_oracle_f1_test.go"
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

// cachedFor returns the cached bytes for path, and whether an entry exists.
func cachedFor(s *Storage, path string) ([]byte, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	e, ok := s.cache[path]
	if !ok {
		return nil, false
	}
	return e.data, true
}

// Enabling encryption must not leave the pre-migration plaintext sitting in
// the cache. The file on disk is ciphertext now; the plaintext the user just
// encrypted must not still be readable out of process memory until something
// unrelated happens to evict it.
func TestOracleF1EnableDropsPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")
	secret := []byte("ACCOUNT,BALANCE\nchecking,12345.67\n")
	if err := s.WriteFile(path, secret, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := s.ReadFile(path); err != nil { // populate the cache
		t.Fatalf("ReadFile: %v", err)
	}
	if _, ok := cachedFor(s, path); !ok {
		t.Fatalf("precondition failed: nothing cached before migration")
	}
	genBefore := s.cacheGeneration()

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}

	if data, ok := cachedFor(s, path); ok && bytes.Contains(data, []byte("12345.67")) {
		t.Errorf("plaintext still resident in cache after EnableEncryption")
	}
	if s.cacheGeneration() == genBefore {
		t.Errorf("cache generation did not advance across EnableEncryption")
	}
	// The file must genuinely have been encrypted, or the test proves nothing.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(raw): %v", err)
	}
	if !isAgeEncrypted(raw) {
		t.Fatalf("precondition failed: file on disk is not encrypted")
	}
	// And the data must still be readable through the store.
	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after migration: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("content changed across migration: got %q", got)
	}
}

// The same must hold in the other direction.
func TestOracleF1DisableDropsStaleEntry(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption: %v", err)
	}
	path := filepath.Join(dir, "ledger.csv")
	secret := []byte("ACCOUNT,BALANCE\nsavings,98765.43\n")
	if err := s.WriteFile(path, secret, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := s.ReadFile(path); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if _, ok := cachedFor(s, path); !ok {
		t.Fatalf("precondition failed: nothing cached before migration")
	}
	genBefore := s.cacheGeneration()

	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption: %v", err)
	}

	if _, ok := cachedFor(s, path); ok {
		t.Errorf("cache entry survived DisableEncryption")
	}
	if s.cacheGeneration() == genBefore {
		t.Errorf("cache generation did not advance across DisableEncryption")
	}
	got, err := s.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after migration: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("content changed across migration: got %q", got)
	}
}

// Regression guard: the fix must not turn the cache into a no-op. An ordinary
// uncontended read still caches.
func TestOracleF1NormalReadStillCaches(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, "plain.csv")
	if err := s.WriteFile(path, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := s.ReadFile(path); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if _, ok := cachedFor(s, path); !ok {
		t.Errorf("ordinary read no longer caches")
	}
}
GO

go test -count=1 -run 'TestOracleF1' ./"$PKG"/ >/tmp/f1_oracle.out 2>&1
rc=$?
ck "01-migration-cache-residency" 0 "$rc"
if (( rc != 0 )); then
  echo "--- planted test output ---"; sed -n '1,40p' /tmp/f1_oracle.out
fi

# Remove the planted file BEFORE measuring the package's own suite. Without
# this, check 02 runs with the fixtures still in the package and grades them
# rather than the arm's own tests -- which is how it reported FAIL against an
# unmodified tree during validation, for a reason that had nothing to do with
# the tree.
rm -f "$PLANTED"

# The package's own suite must stay green, under the race detector: this
# touches locking, and the whole point of the surrounding work was ordering.
# Timeout raised 600s -> 1800s on 2026-08-24 (ruling 2026-08-24c): on a
# loaded 4-CPU container the race suite needs ~1030s and passes with no
# races; 600s was a machine-speed assumption from the author's box, and it
# made this check fail on hardware where the suite is genuinely green.
go test -race -count=1 -timeout 1800s ./"$PKG"/ >/tmp/f1_race.out 2>&1
race_rc=$?
ck "02-package-suite-race" 0 "$race_rc"
(( race_rc == 0 )) || tail -20 /tmp/f1_race.out

# --- the placement must be DEFENDED, not merely correct --------------------
# Added at attempt 3. Attempt 2 grafted the pre-transform placement into all
# three helpers and shipped a test for one of them, so reintroducing the losing
# arm's mistake in either of the other two passed the whole suite silently.
# A correct line no test defends is one refactor away from being wrong again,
# and this is the specific line whose absence is the confidentiality bug.
#
# Each check copies the tree, moves that helper's first invalidateCache below
# its fallible transform -- exactly the wt-alt placement -- and requires the
# package suite to NOTICE. Detection is the property under test, so a green
# suite here is a failure.
mutate_and_expect_failure() {                      # fn-name check-name
  local fn="$1" name="$2" d
  d=$(mktemp -d)
  cp -r internal go.mod go.sum "$d"/ 2>/dev/null
  python3 - "$d" "$fn" <<'MUT'
import re, sys
d, fn = sys.argv[1], sys.argv[2]
p = d + '/internal/services/storage/migration.go'
s = open(p).read()
i = s.find('func (s *Storage) ' + fn)
j = s.find('\n}\n', i)
body = s[i:j]
m_inv = re.search(r'([\t]+)s\.invalidateCache\(path\)\n', body)
m_tr = re.search(r'([\t]+)\w+, err := (?:encryptData|decryptData)\([^\n]*\)\n[\t]+if err != nil \{\n[\t]+(?:return err|continue)\n[\t]+\}\n', body)
if not (m_inv and m_tr):
    sys.exit(3)
inv = m_inv.group(0)
body2 = body[:m_inv.start()] + body[m_inv.end():]
m_tr2 = re.search(re.escape(m_tr.group(0)), body2)
body2 = body2[:m_tr2.end()] + '\n' + inv + body2[m_tr2.end():]
open(p, 'w').write(s[:i] + body2 + s[j:])
MUT
  local mrc=$?
  if (( mrc != 0 )); then
    echo "CHECK $name: FAIL (could not apply mutation, rc=$mrc)"; FAILN=$((FAILN+1)); rm -rf "$d"; return
  fi
  ( cd "$d" && go test -count=1 ./internal/services/storage/ >/dev/null 2>&1 )
  if (( $? != 0 )); then
    echo "CHECK $name: PASS (suite detected the mutation)"; PASSN=$((PASSN+1))
  else
    echo "CHECK $name: FAIL (suite passed with the losing arm's placement restored)"; FAILN=$((FAILN+1))
  fi
  rm -rf "$d"
}
mutate_and_expect_failure encryptFileWithRecipient      "05-encrypt-placement-defended"
mutate_and_expect_failure decryptFileWithIdentity       "06-decrypt-placement-defended"
mutate_and_expect_failure rollbackEncryptionWithIdentity "07-rollback-placement-defended"

# Nothing else in the repo may break.
go build ./... >/dev/null 2>&1; ck "03-build" 0 "$?"
go vet ./... >/dev/null 2>&1;  ck "04-vet" 0 "$?"

echo "---"
echo "passed=$PASSN failed=$FAILN"
(( FAILN == 0 ))
