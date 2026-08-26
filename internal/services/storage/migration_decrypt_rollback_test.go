package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDisableEncryptionPartialFailureRestoresEncryptedInvariant defends the
// fix for the confidentiality defect: when the decrypt loop in
// DisableEncryption fails partway through, every file it had already
// decrypted before the failure must end up re-encrypted, so a failed
// DisableEncryption never leaves plaintext data files sitting on disk while
// s.encrypted still (correctly) reports true.
//
// The forced failure uses isAgeEncrypted's actual check: it only inspects
// the header, so a file containing ageHeader followed by garbage reads as
// "already encrypted" (and is therefore left alone by EnableEncryption, and
// picked up by DisableEncryption's scan) but cannot actually be decrypted.
// Its name is chosen to sort lexically last so that filepath.Walk visits the
// two genuine data files first -- they are fully decrypted by the time the
// broken file's turn comes and triggers the failure.
func TestDisableEncryptionPartialFailureRestoresEncryptedInvariant(t *testing.T) {
	dir := t.TempDir()

	aFile := filepath.Join(dir, "a.csv")
	bFile := filepath.Join(dir, "b.csv")
	brokenFile := filepath.Join(dir, "zzz-broken.csv")

	aContent := []byte("account,balance\nchecking,111.11\n")
	bContent := []byte("account,balance\nsavings,222.22\n")
	// Valid header, invalid body: isAgeEncrypted reports true (header match)
	// but decryptData will fail (not a real age payload).
	brokenContent := []byte(ageHeader + "-v1\nnot a real age payload\n")

	if err := os.WriteFile(aFile, aContent, 0644); err != nil {
		t.Fatalf("WriteFile a.csv failed: %v", err)
	}
	if err := os.WriteFile(bFile, bContent, 0644); err != nil {
		t.Fatalf("WriteFile b.csv failed: %v", err)
	}
	if err := os.WriteFile(brokenFile, brokenContent, 0644); err != nil {
		t.Fatalf("WriteFile zzz-broken.csv failed: %v", err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// EnableEncryption skips brokenFile: encryptFileWithRecipient treats
	// anything that already looks age-encrypted (by header) as done.
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	rawA, err := os.ReadFile(aFile)
	if err != nil {
		t.Fatalf("ReadFile a.csv failed: %v", err)
	}
	if !isAgeEncrypted(rawA) {
		t.Fatalf("precondition failed: a.csv not encrypted after EnableEncryption")
	}
	rawBroken, err := os.ReadFile(brokenFile)
	if err != nil {
		t.Fatalf("ReadFile zzz-broken.csv failed: %v", err)
	}
	if string(rawBroken) != string(brokenContent) {
		t.Fatalf("precondition failed: broken file was modified by EnableEncryption")
	}

	// The failing call.
	err = s.DisableEncryption("testpassword123")
	if err == nil {
		t.Fatal("expected DisableEncryption to fail on the broken file")
	}
	if !strings.Contains(err.Error(), "zzz-broken.csv") {
		t.Errorf("error does not name the file that failed to decrypt: %v", err)
	}
	if !strings.Contains(err.Error(), "re-encrypted") {
		t.Errorf("error does not report the restore outcome: %v", err)
	}

	// Declared state must stay honest: still encrypted.
	if !s.IsEncrypted() {
		t.Error("s.encrypted flipped to false despite a failed DisableEncryption")
	}
	if _, statErr := os.Stat(filepath.Join(dir, markerFile)); statErr != nil {
		t.Errorf("marker file missing after failed DisableEncryption: %v", statErr)
	}

	// The two files that were already decrypted before the failure must be
	// back to ciphertext on disk -- not left as plaintext.
	for _, f := range []string{aFile, bFile} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("ReadFile(%s) failed: %v", f, err)
		}
		if !isAgeEncrypted(raw) {
			t.Errorf("%s left as plaintext on disk after failed DisableEncryption -- confidentiality invariant broken", f)
		}
	}

	// The restore must not have corrupted the data: decrypting again (via a
	// clean, successful DisableEncryption) must reproduce the originals.
	// Fix the broken file so the retry can succeed, then confirm round-trip.
	if err := os.Remove(brokenFile); err != nil {
		t.Fatalf("Remove broken file failed: %v", err)
	}
	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("retry DisableEncryption failed: %v", err)
	}
	gotA, err := os.ReadFile(aFile)
	if err != nil {
		t.Fatalf("ReadFile a.csv after retry failed: %v", err)
	}
	if string(gotA) != string(aContent) {
		t.Errorf("a.csv content corrupted by restore round-trip: got %q want %q", gotA, aContent)
	}
	gotB, err := os.ReadFile(bFile)
	if err != nil {
		t.Fatalf("ReadFile b.csv after retry failed: %v", err)
	}
	if string(gotB) != string(bContent) {
		t.Errorf("b.csv content corrupted by restore round-trip: got %q want %q", gotB, bContent)
	}
}

// TestDisableEncryptionPartialFailureNothingDecryptedYet defends the
// no-op-restore edge case: if the very first file the loop touches is the
// one that fails, there is nothing to roll back, and the error returned
// must still name the failing file without claiming a restore happened.
//
// The fixture name matters. DisableEncryption's walk collects every
// age-encrypted file, which includes ".encryption-verify" (storage.go's
// verifyFile) -- and filepath.Walk visits lexically, so a broken file named
// e.g. "aaa-broken.csv" sorts AFTER ".encryption-verify" and is not actually
// the first file decrypted: the verify file decrypts successfully first,
// decryptedSoFar is never empty, and this test would silently be exercising
// the OTHER branch (see TestDisableEncryptionPartialFailureRestoresEncryptedInvariant)
// while claiming to cover this one. Naming it ".a-broken.csv" -- a leading
// dot plus an early letter -- sorts it before ".encryption-verify" so it is
// genuinely the first (and only) file the loop reaches.
func TestDisableEncryptionPartialFailureNothingDecryptedYet(t *testing.T) {
	dir := t.TempDir()

	// Named to sort before ".encryption-verify", so it is genuinely the
	// first (and only) file the loop reaches -- see doc comment above.
	brokenFile := filepath.Join(dir, ".a-broken.csv")
	brokenContent := []byte(ageHeader + "-v1\nnot a real age payload\n")
	if err := os.WriteFile(brokenFile, brokenContent, 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}

	err = s.DisableEncryption("testpassword123")
	if err == nil {
		t.Fatal("expected DisableEncryption to fail on the broken file")
	}
	if !strings.Contains(err.Error(), ".a-broken.csv") {
		t.Errorf("error does not name the failing file: %v", err)
	}
	// Nothing was decrypted before this failure, so nothing was re-encrypted
	// either -- the error must not claim otherwise. Without the
	// `len(decryptedSoFar) == 0` guard in DisableEncryption, this branch
	// falls through to the general path and reports "the 0 file(s) already
	// decrypted before this failure were successfully re-encrypted", which
	// claims an action that did not take place.
	if strings.Contains(err.Error(), "re-encrypted") {
		t.Errorf("error claims a re-encryption happened when nothing was decrypted yet: %v", err)
	}
	if !s.IsEncrypted() {
		t.Error("s.encrypted flipped to false despite a failed DisableEncryption")
	}
}

// TestRollbackDecryptionWithRecipientReportsUnrestorableFiles defends
// property 2 of the fix directly: rollbackDecryptionWithRecipient must not
// swallow a file it cannot re-encrypt -- it has to come back in the
// returned list, by basename, the same way the caller reports the file that
// failed to decrypt in the first place.
func TestRollbackDecryptionWithRecipientReportsUnrestorableFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient failed: %v", err)
	}
	identity, err := s.provider.Identity()
	if err != nil {
		t.Fatalf("Identity failed: %v", err)
	}

	// good.csv: plaintext, restorable.
	goodFile := filepath.Join(dir, "good.csv")
	if err := os.WriteFile(goodFile, []byte("x,y\n1,2\n"), 0644); err != nil {
		t.Fatalf("WriteFile good.csv failed: %v", err)
	}

	// already.csv: still encrypted (e.g. never actually decrypted); must be
	// left alone and not reported as failed.
	alreadyFile := filepath.Join(dir, "already.csv")
	encAlready, err := encryptData([]byte("m,n\n3,4\n"), recipient)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}
	if err := os.WriteFile(alreadyFile, encAlready, 0644); err != nil {
		t.Fatalf("WriteFile already.csv failed: %v", err)
	}

	// unreadable.csv: a dangling symlink, not a chmod-0000 file. The helper's
	// first move on each path is os.ReadFile, and a symlink whose target does
	// not exist fails that call at the open step (ENOENT) regardless of who
	// is running the test -- there are no permission bits involved for root's
	// CAP_DAC_OVERRIDE to bypass, unlike the chmod 0000 fixture this replaced,
	// which root reads straight through. Either way the helper "cannot even
	// confirm its state, let alone re-encrypt it."
	unreadableFile := filepath.Join(dir, "unreadable.csv")
	if err := os.Symlink(filepath.Join(dir, "unreadable.csv.missing-target"), unreadableFile); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	failed := s.rollbackDecryptionWithRecipient([]string{goodFile, alreadyFile, unreadableFile}, recipient)

	// Full path, not basename: the error built from this list tells the user
	// these files are still plaintext and to handle them by hand, so it has
	// to say where they are. Changed from the arm's original basename
	// assertion when the losing arm's full-path reporting was grafted in;
	// see TestRollbackDecryptionReportsFullPaths.
	if len(failed) != 1 || failed[0] != unreadableFile {
		t.Fatalf("expected failed=[%s], got %v", unreadableFile, failed)
	}

	rawGood, err := os.ReadFile(goodFile)
	if err != nil {
		t.Fatalf("ReadFile good.csv failed: %v", err)
	}
	if !isAgeEncrypted(rawGood) {
		t.Errorf("good.csv not re-encrypted")
	}
	decGood, err := decryptData(rawGood, identity)
	if err != nil {
		t.Fatalf("decryptData(good.csv) failed: %v", err)
	}
	if string(decGood) != "x,y\n1,2\n" {
		t.Errorf("good.csv content mismatch after restore: got %q", decGood)
	}

	rawAlready, err := os.ReadFile(alreadyFile)
	if err != nil {
		t.Fatalf("ReadFile already.csv failed: %v", err)
	}
	if !isAgeEncrypted(rawAlready) || string(rawAlready) != string(encAlready) {
		t.Errorf("already-encrypted file was disturbed by rollbackDecryptionWithRecipient")
	}
}

// TestRollbackDecryptionReportsFullPaths defends the *content* of the failure
// report, not merely its existence.
//
// The error this feeds tells the user that named files are still plaintext on
// disk and that they must be dealt with by hand. A bare basename does not say
// where they are, and the user is being asked to act on data they have just
// been told is exposed. The two Tier-3 arms split on exactly this: the winning
// arm reported filepath.Base, the losing one reported the full path, and the
// losing one was right. Revert `failed = append(failed, path)` to
// filepath.Base and this test fails; nothing else in the suite does.
func TestRollbackDecryptionReportsFullPaths(t *testing.T) {
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
	recipient, err := provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient: %v", err)
	}

	// A file whose current state cannot even be read is reported as
	// unrestored, which is the branch that produces a path in the list. A
	// dangling symlink forces os.ReadFile to fail at open (ENOENT), and it
	// does so for every uid including root: chmod 0000 does not, because
	// root's CAP_DAC_OVERRIDE reads straight through the permission bits --
	// which is exactly why this test used to skip itself under root instead
	// of exercising this branch there.
	unreadable := filepath.Join(dir, "exposed.csv")
	if err := os.Symlink(filepath.Join(dir, "exposed.csv.missing-target"), unreadable); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	failed := s.rollbackDecryptionWithRecipient([]string{unreadable}, recipient)
	if len(failed) != 1 {
		t.Fatalf("want 1 unrestored file, got %d: %v", len(failed), failed)
	}
	if !strings.Contains(failed[0], dir) {
		t.Errorf("report names %q, which does not say where the file is; want a path under %q", failed[0], dir)
	}
}

// TestRollbackDecryptionReportsPathOnEncryptFailure defends
// rollbackDecryptionWithRecipient's second failure branch: when re-encrypting
// a previously-decrypted file fails because encryptData itself errors (as
// opposed to the file being unreadable, or the subsequent atomicWrite
// failing), the file's full path must still land in the returned failed
// slice rather than being silently dropped.
//
// failingRecipient (defined in migration_cache_test.go) is an age.Recipient
// whose Wrap always errors, which is the only way to reach this branch from
// a test: every real recipient in this package succeeds. Passing it in place
// of a working recipient forces encryptData to fail while the ReadFile and
// isAgeEncrypted checks ahead of it both succeed normally.
//
// Drop `failed = append(failed, path)` from the encryptData error branch and
// this test fails; nothing else in the suite does.
func TestRollbackDecryptionReportsPathOnEncryptFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Plaintext, not already encrypted, so rollbackDecryptionWithRecipient's
	// isAgeEncrypted check does not short-circuit before reaching
	// encryptData.
	plainFile := filepath.Join(dir, "plain.csv")
	plaintext := []byte("account,balance\nchecking,42.00\n")
	if err := os.WriteFile(plainFile, plaintext, 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}

	failed := s.rollbackDecryptionWithRecipient([]string{plainFile}, failingRecipient{})

	if len(failed) != 1 || failed[0] != plainFile {
		t.Fatalf("expected failed=[%s], got %v", plainFile, failed)
	}

	// No write was ever attempted (encryptData failed before atomicWrite is
	// reached), so the file on disk must be exactly the plaintext that was
	// there before the call.
	raw, err := os.ReadFile(plainFile)
	if err != nil {
		t.Fatalf("ReadFile after rollback failed: %v", err)
	}
	if string(raw) != string(plaintext) {
		t.Errorf("plain.csv content changed by a failed re-encrypt attempt: got %q, want %q", raw, plaintext)
	}
}

// TestRollbackDecryptionReportsPathOnAtomicWriteFailure defends
// rollbackDecryptionWithRecipient's third failure branch: when encryptData
// succeeds but s.atomicWrite fails to publish the re-encrypted bytes, the
// file's full path must still land in the returned failed slice.
//
// atomicWrite publishes by renaming a staging file it creates with
// os.CreateTemp inside the destination's own directory, under the name
// <destination-basename> + StagingSuffix + <random digits> (see
// StagingSuffix's doc comment in storage.go). This used to be forced by
// chmodding the containing directory 0500 so os.CreateTemp could not create
// the staging file -- but that is a DAC permission check, and root bypasses
// DAC permission checks (CAP_DAC_OVERRIDE), so as root the chmod never
// actually blocked the write and the test self-skipped via
// os.Geteuid() == 0, leaving this branch undefended in exactly the
// environment (root containers) it runs in. This version forces the same
// os.CreateTemp failure a different way: it names the data file with a
// basename of exactly 255 bytes, the NAME_MAX limit ext4 and tmpfs enforce
// per path component (both t.TempDir() resolves to on Linux). A 255-byte
// name is itself a valid, creatable, readable filename -- but atomicWrite's
// staging name, built by appending StagingSuffix and a random suffix to
// that same basename, is necessarily longer than NAME_MAX, so
// os.CreateTemp fails with ENAMETOOLONG. That is a kernel-enforced
// filesystem limit, not a permission bit, so it fails identically for uid 0
// and for an ordinary user -- there is nothing here for root to bypass.
//
// Drop `failed = append(failed, path)` from the atomicWrite error branch and
// this test fails; nothing else in the suite does.
func TestRollbackDecryptionReportsPathOnAtomicWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NAME_MAX arithmetic below assumes POSIX per-component filename limits; windows path-length semantics differ")
	}

	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}
	recipient, err := s.provider.Recipient()
	if err != nil {
		t.Fatalf("Recipient failed: %v", err)
	}

	// Exactly NAME_MAX (255) bytes: the file itself is valid, but
	// atomicWrite's staging name -- this basename + StagingSuffix + random
	// digits -- is not (see doc comment above).
	base := strings.Repeat("a", 255-len(".csv")) + ".csv"
	if len(base) != 255 {
		t.Fatalf("test bug: basename is %d bytes, want 255", len(base))
	}
	plainFile := filepath.Join(dir, base)
	plaintext := []byte("account,balance\nchecking,77.00\n")
	if err := os.WriteFile(plainFile, plaintext, 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}
	// Confirm the setup assumption directly: the 255-byte name is readable,
	// so rollbackDecryptionWithRecipient's leading os.ReadFile succeeds and
	// the forced failure lands in the atomicWrite branch specifically, not
	// the read branch (TestRollbackDecryptionReportsFullPaths) or the
	// encrypt branch (TestRollbackDecryptionReportsPathOnEncryptFailure).
	if _, err := os.ReadFile(plainFile); err != nil {
		t.Fatalf("precondition failed: 255-byte filename is not readable: %v", err)
	}

	failed := s.rollbackDecryptionWithRecipient([]string{plainFile}, recipient)

	if len(failed) != 1 || failed[0] != plainFile {
		t.Fatalf("expected failed=[%s], got %v", plainFile, failed)
	}

	// atomicWrite's staging file was never created (let alone renamed into
	// place), so the original plaintext must be untouched.
	raw, err := os.ReadFile(plainFile)
	if err != nil {
		t.Fatalf("ReadFile after rollback failed: %v", err)
	}
	if string(raw) != string(plaintext) {
		t.Errorf("plain.csv content changed by a failed atomicWrite attempt: got %q, want %q", raw, plaintext)
	}
}
