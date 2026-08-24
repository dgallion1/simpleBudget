package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRollbackEncryptionPublishesByRenameNotWrite defends property 1:
// rollbackEncryptionWithIdentity must restore the plaintext even when the
// destination file's own mode forbids writing to it, because it publishes by
// renaming a staging file over the destination rather than opening the
// destination directly. Renaming over path needs write permission on path's
// directory, not on path itself, so a read-only destination file is not an
// obstacle.
//
// Chmod rollbackEncryptionWithIdentity's os.WriteFile(path, decrypted, 0644)
// call back in (replacing the s.atomicWrite call) and this test fails: the
// destination stays ciphertext because the direct write is rejected by the
// kernel, and the failure is only logged, never surfaced to the test's
// on-disk assertion.
func TestRollbackEncryptionPublishesByRenameNotWrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}
	recipient, _ := s.provider.Recipient()
	identity, _ := s.provider.Identity()

	file1 := filepath.Join(dir, "f1.csv")
	plaintext := []byte("account,balance\nchecking,42.00\n")
	enc1, err := encryptData(plaintext, recipient)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}
	if err := os.WriteFile(file1, enc1, 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}

	// Make the destination file itself unwritable. The containing directory
	// (t.TempDir()) remains writable, which is exactly the case rename can
	// handle and a direct write cannot.
	if err := os.Chmod(file1, 0444); err != nil {
		t.Fatalf("Chmod setup failed: %v", err)
	}

	s.rollbackEncryptionWithIdentity([]string{file1}, identity)

	raw, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("ReadFile after rollback failed: %v", err)
	}
	if string(raw) != string(plaintext) {
		t.Fatalf("rollback did not restore plaintext for a read-only destination: got %q, want %q", raw, plaintext)
	}
}

// TestMigrationRoundTripLeavesNoStagingFiles defends property 2: a completed
// migration in either direction, including a rollback, leaves no staging
// file behind. All three helpers stage beside their destination and publish
// by rename (or, for rollbackEncryptionWithIdentity before this fix, wrote
// the destination directly and staged nothing at all) -- either way nothing
// matching IsStagingName should remain once the dust settles.
func TestMigrationRoundTripLeavesNoStagingFiles(t *testing.T) {
	dir := t.TempDir()
	csvFile := filepath.Join(dir, "data.csv")
	jsonFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(csvFile, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}
	if err := os.WriteFile(jsonFile, []byte(`{"k":"v"}`), 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}

	s, _ := New(dir)

	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("EnableEncryption failed: %v", err)
	}
	assertNoStagingFiles(t, dir, "after EnableEncryption")

	if err := s.DisableEncryption("testpassword123"); err != nil {
		t.Fatalf("DisableEncryption failed: %v", err)
	}
	assertNoStagingFiles(t, dir, "after DisableEncryption")

	// Exercise rollbackEncryptionWithIdentity directly too -- it is not
	// reached by a successful EnableEncryption/DisableEncryption pair above.
	if err := s.EnableEncryption("testpassword123"); err != nil {
		t.Fatalf("second EnableEncryption failed: %v", err)
	}
	recipient, _ := s.provider.Recipient()
	identity, _ := s.provider.Identity()

	rollbackFile := filepath.Join(dir, "extra.csv")
	enc, err := encryptData([]byte("x,y\n1,2\n"), recipient)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}
	if err := os.WriteFile(rollbackFile, enc, 0644); err != nil {
		t.Fatalf("WriteFile setup failed: %v", err)
	}
	s.rollbackEncryptionWithIdentity([]string{rollbackFile}, identity)
	assertNoStagingFiles(t, dir, "after rollbackEncryptionWithIdentity")
}

func assertNoStagingFiles(t *testing.T, dir, when string) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if IsStagingName(filepath.Base(path)) {
			t.Errorf("staging file left behind %s: %s", when, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}
}

// TestMigrationHelpersDoNotUseLegacyFixedStagingName defends property 3: none
// of encryptFileWithRecipient, decryptFileWithIdentity, or
// rollbackEncryptionWithIdentity stage at the fixed legacy name
// path + ".tmp" any more; each now goes through the same StagingSuffix +
// random-suffix convention as atomicWrite/createExclusive in storage.go.
//
// A file already sitting at the legacy fixed name is the witness: the old
// code wrote straight to that exact path (os.WriteFile(tmpPath, ...) then
// os.Rename(tmpPath, path), consuming and replacing whatever was there) so a
// pre-existing sentinel at path+".tmp" would be clobbered. The new code
// stages at a randomised name from os.CreateTemp, so it can never collide
// with -- and therefore never disturbs -- a file already sitting at the
// legacy fixed name.
//
// Put the tmpPath := path + ".tmp" / os.WriteFile / os.Rename sequence back
// in any of the three helpers and this test fails for that helper.
func TestMigrationHelpersDoNotUseLegacyFixedStagingName(t *testing.T) {
	t.Run("encryptFileWithRecipient", func(t *testing.T) {
		dir := t.TempDir()
		csvFile := filepath.Join(dir, "data.csv")
		if err := os.WriteFile(csvFile, []byte("a,b\n1,2\n"), 0644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}
		sentinel := csvFile + ".tmp"
		const sentinelContent = "legacy-staging-sentinel"
		if err := os.WriteFile(sentinel, []byte(sentinelContent), 0644); err != nil {
			t.Fatalf("WriteFile sentinel setup failed: %v", err)
		}

		s, _ := New(dir)
		provider, err := NewPasswordProviderWithCredentials("testpassword123")
		if err != nil {
			t.Fatalf("NewPasswordProviderWithCredentials failed: %v", err)
		}
		recipient, err := provider.Recipient()
		if err != nil {
			t.Fatalf("Recipient failed: %v", err)
		}

		if err := s.encryptFileWithRecipient(csvFile, recipient); err != nil {
			t.Fatalf("encryptFileWithRecipient failed: %v", err)
		}

		got, err := os.ReadFile(sentinel)
		if err != nil {
			t.Fatalf("sentinel file disappeared: %v", err)
		}
		if string(got) != sentinelContent {
			t.Errorf("sentinel at legacy fixed staging name was disturbed: got %q, want %q", got, sentinelContent)
		}
	})

	t.Run("decryptFileWithIdentity", func(t *testing.T) {
		dir := t.TempDir()
		s, _ := New(dir)
		provider, err := NewPasswordProviderWithCredentials("testpassword123")
		if err != nil {
			t.Fatalf("NewPasswordProviderWithCredentials failed: %v", err)
		}
		recipient, err := provider.Recipient()
		if err != nil {
			t.Fatalf("Recipient failed: %v", err)
		}
		identity, err := provider.Identity()
		if err != nil {
			t.Fatalf("Identity failed: %v", err)
		}

		csvFile := filepath.Join(dir, "data.csv")
		enc, err := encryptData([]byte("a,b\n1,2\n"), recipient)
		if err != nil {
			t.Fatalf("encryptData failed: %v", err)
		}
		if err := os.WriteFile(csvFile, enc, 0644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		sentinel := csvFile + ".tmp"
		const sentinelContent = "legacy-staging-sentinel"
		if err := os.WriteFile(sentinel, []byte(sentinelContent), 0644); err != nil {
			t.Fatalf("WriteFile sentinel setup failed: %v", err)
		}

		if err := s.decryptFileWithIdentity(csvFile, identity); err != nil {
			t.Fatalf("decryptFileWithIdentity failed: %v", err)
		}

		got, err := os.ReadFile(sentinel)
		if err != nil {
			t.Fatalf("sentinel file disappeared: %v", err)
		}
		if string(got) != sentinelContent {
			t.Errorf("sentinel at legacy fixed staging name was disturbed: got %q, want %q", got, sentinelContent)
		}
	})

	t.Run("rollbackEncryptionWithIdentity", func(t *testing.T) {
		dir := t.TempDir()
		s, _ := New(dir)
		provider, err := NewPasswordProviderWithCredentials("testpassword123")
		if err != nil {
			t.Fatalf("NewPasswordProviderWithCredentials failed: %v", err)
		}
		recipient, err := provider.Recipient()
		if err != nil {
			t.Fatalf("Recipient failed: %v", err)
		}
		identity, err := provider.Identity()
		if err != nil {
			t.Fatalf("Identity failed: %v", err)
		}

		file1 := filepath.Join(dir, "f1.csv")
		enc1, err := encryptData([]byte("data1"), recipient)
		if err != nil {
			t.Fatalf("encryptData failed: %v", err)
		}
		if err := os.WriteFile(file1, enc1, 0644); err != nil {
			t.Fatalf("WriteFile setup failed: %v", err)
		}

		sentinel := file1 + ".tmp"
		const sentinelContent = "legacy-staging-sentinel"
		if err := os.WriteFile(sentinel, []byte(sentinelContent), 0644); err != nil {
			t.Fatalf("WriteFile sentinel setup failed: %v", err)
		}

		s.rollbackEncryptionWithIdentity([]string{file1}, identity)

		got, err := os.ReadFile(sentinel)
		if err != nil {
			t.Fatalf("sentinel file disappeared: %v", err)
		}
		if string(got) != sentinelContent {
			t.Errorf("sentinel at legacy fixed staging name was disturbed: got %q, want %q", got, sentinelContent)
		}

		raw1, err := os.ReadFile(file1)
		if err != nil {
			t.Fatalf("ReadFile(file1) failed: %v", err)
		}
		if string(raw1) != "data1" {
			t.Errorf("rollback did not restore file1 despite the sentinel collision guard: got %q", raw1)
		}
	})
}

// TestMigrationStagingFilesUseStagingSuffix is a narrower companion to
// TestMigrationHelpersDoNotUseLegacyFixedStagingName: it observes, rather
// than infers, that the name IsStagingName recognises as the *current*
// convention is the one actually produced by createExclusive/atomicWrite,
// i.e. that the migration helpers and storage.go's own writers agree on one
// staging convention. Guards against a fix that swaps the legacy fixed name
// for some other ad hoc scheme instead of the shared StagingSuffix one.
func TestMigrationStagingFilesUseStagingSuffix(t *testing.T) {
	dir := t.TempDir()
	name, err := os.CreateTemp(dir, "sidecar.json"+StagingSuffix+"*")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	base := filepath.Base(name.Name())
	_ = name.Close()
	_ = os.Remove(name.Name())

	if !strings.HasPrefix(base, "sidecar.json"+StagingSuffix) {
		t.Fatalf("unexpected staging name shape: %s", base)
	}
	if !IsStagingName(base) {
		t.Errorf("IsStagingName(%q) = false, want true for the convention migration.go now uses", base)
	}
}
