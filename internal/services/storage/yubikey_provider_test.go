package storage

import (
	"testing"

	"filippo.io/age"
)

// TestYubiKeyProviderStruct tests YubiKeyProvider methods by constructing
// the struct directly (bypassing the constructor which requires the plugin).
func TestYubiKeyProviderMethod(t *testing.T) {
	p := &YubiKeyProvider{}
	if p.Method() != AuthMethodYubiKey {
		t.Errorf("expected %s, got %s", AuthMethodYubiKey, p.Method())
	}
}

func TestYubiKeyProviderNeedsUnlock(t *testing.T) {
	p := &YubiKeyProvider{}
	if p.NeedsUnlock() {
		t.Error("YubiKey provider should not need unlock")
	}
}

func TestYubiKeyProviderIsUnlocked(t *testing.T) {
	p := &YubiKeyProvider{}

	// No identity loaded => not unlocked
	if p.IsUnlocked() {
		t.Error("should not be unlocked without identity")
	}
}

func TestYubiKeyProviderUnlock(t *testing.T) {
	p := &YubiKeyProvider{}
	// Unlock is a no-op
	if err := p.Unlock("anything"); err != nil {
		t.Errorf("Unlock should be a no-op: %v", err)
	}
}

func TestYubiKeyProviderLock(t *testing.T) {
	p := &YubiKeyProvider{
		identityStr:  "AGE-PLUGIN-YUBIKEY-test",
		recipientStr: "age1yubikey1test",
	}

	// Lock should clear identity and recipient fields
	p.Lock()

	if p.identity != nil {
		t.Error("identity should be nil after Lock")
	}
	if p.recipient != nil {
		t.Error("recipient should be nil after Lock")
	}
}

func TestYubiKeyProviderDisplayInfo(t *testing.T) {
	p := &YubiKeyProvider{}
	info := p.DisplayInfo()
	if info != "YubiKey hardware key" {
		t.Errorf("unexpected DisplayInfo: %q", info)
	}
}

func TestYubiKeyProviderIdentityNil(t *testing.T) {
	p := &YubiKeyProvider{}
	_, err := p.Identity()
	if err == nil {
		t.Error("expected error when identity is nil")
	}
}

func TestYubiKeyProviderIdentityLoaded(t *testing.T) {
	// Use an X25519 identity as stand-in to test the success path
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	p := &YubiKeyProvider{identity: id}
	got, err := p.Identity()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("identity should not be nil")
	}
}

func TestYubiKeyProviderRecipientNil(t *testing.T) {
	p := &YubiKeyProvider{}
	_, err := p.Recipient()
	if err == nil {
		t.Error("expected error when recipient is nil")
	}
}

func TestYubiKeyProviderRecipientLoaded(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	p := &YubiKeyProvider{recipient: id.Recipient()}
	got, err := p.Recipient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("recipient should not be nil")
	}
}

func TestYubiKeyProviderIsUnlockedWithIdentity(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	p := &YubiKeyProvider{identity: id}
	if !p.IsUnlocked() {
		t.Error("should be unlocked when identity is loaded")
	}
}

func TestYubiKeyProviderLockClearsLoadedFields(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("failed to generate identity: %v", err)
	}
	p := &YubiKeyProvider{
		identity:  id,
		recipient: id.Recipient(),
	}
	if !p.IsUnlocked() {
		t.Error("should be unlocked before Lock")
	}
	p.Lock()
	if p.IsUnlocked() {
		t.Error("should be locked after Lock")
	}
	_, err = p.Identity()
	if err == nil {
		t.Error("expected error after Lock")
	}
	_, err = p.Recipient()
	if err == nil {
		t.Error("expected error after Lock")
	}
}

func TestYubiKeyProviderExtractRecipientFromIdentity(t *testing.T) {
	tests := []struct {
		name        string
		identityStr string
		wantRecip   string
	}{
		{
			name:        "standard format with space",
			identityStr: "# created: 2024-01-01T00:00:00Z\n# recipient: age1yubikey1abc123\nAGE-PLUGIN-YUBIKEY-1XXXXX",
			wantRecip:   "age1yubikey1abc123",
		},
		{
			name:        "no space after colon",
			identityStr: "# created: 2024-01-01T00:00:00Z\n#recipient: age1yubikey1def456\nAGE-PLUGIN-YUBIKEY-1XXXXX",
			wantRecip:   "age1yubikey1def456",
		},
		{
			name:        "no recipient comment",
			identityStr: "# created: 2024-01-01T00:00:00Z\nAGE-PLUGIN-YUBIKEY-1XXXXX",
			wantRecip:   "",
		},
		{
			name:        "empty string",
			identityStr: "",
			wantRecip:   "",
		},
		{
			name:        "only comments no recipient",
			identityStr: "# created: 2024-01-01\n# name: my key\n",
			wantRecip:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &YubiKeyProvider{identityStr: tt.identityStr}
			p.extractRecipientFromIdentity()
			if p.recipientStr != tt.wantRecip {
				t.Errorf("got recipient %q, want %q", p.recipientStr, tt.wantRecip)
			}
		})
	}
}

func TestNewYubiKeyProviderEmptyIdentity(t *testing.T) {
	_, err := NewYubiKeyProvider("")
	if err == nil {
		t.Error("expected error for empty identity")
	}
}

func TestNewYubiKeyProviderWithRecipientEmptyIdentity(t *testing.T) {
	_, err := NewYubiKeyProviderWithRecipient("", "age1yubikey1test")
	if err == nil {
		t.Error("expected error for empty identity")
	}
}

func TestIsYubiKeyPluginInstalledReturnsBool(t *testing.T) {
	// Just verify it returns without panic; result depends on system
	result := IsYubiKeyPluginInstalled()
	_ = result
}

func TestSetupYubiKeyReturnsError(t *testing.T) {
	result, err := SetupYubiKey()
	if err == nil {
		t.Error("expected error from SetupYubiKey")
	}
	if result != nil {
		t.Error("expected nil result from SetupYubiKey")
	}
}

// TestNewYubiKeyProviderWithInvalidIdentity tests the constructor with an invalid
// identity string. When the plugin is installed, this exercises the loadIdentity
// error path. When not installed, it exercises the plugin-not-installed error.
func TestNewYubiKeyProviderWithInvalidIdentity(t *testing.T) {
	_, err := NewYubiKeyProvider("not-a-valid-identity-string")
	if err == nil {
		t.Error("expected error for invalid identity string")
	}
}

func TestNewYubiKeyProviderWithRecipientInvalidIdentity(t *testing.T) {
	_, err := NewYubiKeyProviderWithRecipient("not-a-valid-identity-string", "age1yubikey1test")
	if err == nil {
		t.Error("expected error for invalid identity string")
	}
}

// TestListYubiKeySlots exercises the function regardless of whether the plugin
// is installed. If installed, it runs the actual plugin command (which will
// return empty results without a YubiKey connected). If not installed, it
// returns nil early.
func TestListYubiKeySlotsExercise(t *testing.T) {
	slots, err := ListYubiKeySlots()
	if !IsYubiKeyPluginInstalled() {
		if err != nil {
			t.Errorf("should not error when plugin not installed: %v", err)
		}
		if slots != nil {
			t.Error("should return nil when plugin not installed")
		}
	} else {
		// Plugin installed but no hardware; should not error, may return empty
		if err != nil {
			t.Errorf("unexpected error with plugin installed: %v", err)
		}
		// slots may be nil or empty - both are valid without hardware
	}
}

// TestDetectYubiKeyIdentitiesExercise exercises the function with or without the plugin.
func TestDetectYubiKeyIdentitiesExercise(t *testing.T) {
	identities, err := DetectYubiKeyIdentities()
	if !IsYubiKeyPluginInstalled() {
		if err != nil {
			t.Errorf("should not error when plugin not installed: %v", err)
		}
		if identities != nil {
			t.Error("should return nil when plugin not installed")
		}
	} else {
		// Plugin installed but no hardware
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// TestDetectYubiKeysExercise exercises the function with or without the plugin.
func TestDetectYubiKeysExercise(t *testing.T) {
	keys, err := DetectYubiKeys()
	if !IsYubiKeyPluginInstalled() {
		if err == nil {
			t.Error("expected error when plugin not installed")
		}
	} else {
		// Plugin installed - should return at least a generic entry even without hardware
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(keys) == 0 {
			t.Error("should return at least one entry (generic) when plugin is installed")
		}
	}
}

// TestGetYubiKeyIdentityForRecipientExercise exercises the function.
func TestGetYubiKeyIdentityForRecipientExercise(t *testing.T) {
	_, err := GetYubiKeyIdentityForRecipient("age1yubikey1test")
	if !IsYubiKeyPluginInstalled() {
		if err == nil {
			t.Error("expected error when plugin not installed")
		}
	} else {
		// Plugin installed but no hardware; likely will error since no YubiKey is connected
		// We just exercise the code path - error is expected
		_ = err
	}
}

// TestCreateProviderYubiKeyMethodExercise tests the createProvider path for YubiKey
func TestCreateProviderYubiKeyMethodExercise(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Test createProvider with YubiKey method (no recipient)
	s.config = &EncryptionConfig{
		Method:          AuthMethodYubiKey,
		YubiKeyIdentity: "AGE-PLUGIN-YUBIKEY-1test",
	}
	_, err := s.createProvider()
	// Should fail regardless (invalid identity or no plugin)
	if err == nil {
		t.Error("expected error for invalid YubiKey identity")
	}

	// Test createProvider with YubiKey method (with recipient)
	s.config = &EncryptionConfig{
		Method:           AuthMethodYubiKey,
		YubiKeyIdentity:  "AGE-PLUGIN-YUBIKEY-1test",
		YubiKeyRecipient: "age1yubikey1test",
	}
	_, err = s.createProvider()
	if err == nil {
		t.Error("expected error for invalid YubiKey identity")
	}
}

// TestCreateProviderYubiKeyUnlockedExercise tests createProviderUnlocked for the YubiKey method
func TestCreateProviderYubiKeyUnlockedExercise(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)

	// Without recipient
	s.config = &EncryptionConfig{
		Method:          AuthMethodYubiKey,
		YubiKeyIdentity: "AGE-PLUGIN-YUBIKEY-1test",
	}
	_, err := s.createProviderUnlocked()
	if err == nil {
		t.Error("expected error for invalid YubiKey identity (no recipient)")
	}

	// With recipient
	s.config = &EncryptionConfig{
		Method:           AuthMethodYubiKey,
		YubiKeyIdentity:  "AGE-PLUGIN-YUBIKEY-1test",
		YubiKeyRecipient: "age1yubikey1test",
	}
	_, err = s.createProviderUnlocked()
	if err == nil {
		t.Error("expected error for invalid YubiKey identity (with recipient)")
	}
}

// TestYubiKeyProviderLoadIdentityInvalid tests loadIdentity with an invalid identity string
func TestYubiKeyProviderLoadIdentityInvalid(t *testing.T) {
	p := &YubiKeyProvider{
		identityStr: "this is not a valid identity",
	}
	err := p.loadIdentity()
	if err == nil {
		t.Error("expected error for invalid identity string")
	}
}

// TestYubiKeyProviderLoadIdentityEmpty tests loadIdentity with an empty string
func TestYubiKeyProviderLoadIdentityEmpty(t *testing.T) {
	p := &YubiKeyProvider{
		identityStr: "",
	}
	err := p.loadIdentity()
	if err == nil {
		t.Error("expected error for empty identity string")
	}
}

// TestYubiKeyProviderLoadIdentityCommentsOnly tests loadIdentity with only comments
func TestYubiKeyProviderLoadIdentityCommentsOnly(t *testing.T) {
	p := &YubiKeyProvider{
		identityStr: "# just a comment\n# another comment\n",
	}
	err := p.loadIdentity()
	if err == nil {
		t.Error("expected error for identity with only comments")
	}
}

func TestYubiKeySlotStruct(t *testing.T) {
	slot := YubiKeySlot{
		Slot:      "1",
		Name:      "test slot",
		Serial:    "12345678",
		Recipient: "age1yubikey1abc",
	}

	if slot.Slot != "1" {
		t.Errorf("unexpected Slot: %s", slot.Slot)
	}
	if slot.Serial != "12345678" {
		t.Errorf("unexpected Serial: %s", slot.Serial)
	}
}

func TestYubiKeyInfoStruct(t *testing.T) {
	info := YubiKeyInfo{
		Serial:       "12345678",
		HasIdentity:  true,
		SetupCommand: "age-plugin-yubikey --generate",
	}

	if !info.HasIdentity {
		t.Error("expected HasIdentity to be true")
	}
}

func TestYubiKeySetupResultStruct(t *testing.T) {
	result := YubiKeySetupResult{
		Identity:  "AGE-PLUGIN-YUBIKEY-1xxx",
		Recipient: "age1yubikey1xxx",
		FullText:  "full output",
	}

	if result.Identity != "AGE-PLUGIN-YUBIKEY-1xxx" {
		t.Errorf("unexpected Identity: %s", result.Identity)
	}
}
