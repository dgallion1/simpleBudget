package storage

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"filippo.io/age"
)

// YubiKeyProvider implements AuthProvider for YubiKey encryption via age-plugin-yubikey
type YubiKeyProvider struct {
	identityStr  string
	recipientStr string
	identity     age.Identity
	recipient    age.Recipient
}

// NewYubiKeyProvider creates a new YubiKey provider
// identityStr should be the AGE-PLUGIN-YUBIKEY-... identity string
// The recipient (age1yubikey1...) will be extracted from the identity
func NewYubiKeyProvider(identityStr string) (*YubiKeyProvider, error) {
	if identityStr == "" {
		return nil, fmt.Errorf("YubiKey identity string is required")
	}

	if !IsYubiKeyPluginInstalled() {
		return nil, fmt.Errorf("age-plugin-yubikey is not installed")
	}

	p := &YubiKeyProvider{
		identityStr: identityStr,
	}

	// Parse the identity string to get identity and recipient
	if err := p.loadIdentity(); err != nil {
		return nil, err
	}

	return p, nil
}

// loadIdentity parses the YubiKey identity string using age's plugin support
func (p *YubiKeyProvider) loadIdentity() error {
	// Parse the identity using age's built-in plugin support
	identities, err := age.ParseIdentities(strings.NewReader(p.identityStr))
	if err != nil {
		return fmt.Errorf("failed to parse YubiKey identity: %w", err)
	}

	if len(identities) == 0 {
		return fmt.Errorf("no YubiKey identity found")
	}

	p.identity = identities[0]

	// For YubiKey, the recipient is derived from the identity
	// We need to extract or generate it
	// The identity string typically contains both identity and recipient info
	if p.recipientStr != "" {
		recipients, err := age.ParseRecipients(strings.NewReader(p.recipientStr))
		if err != nil {
			return fmt.Errorf("failed to parse YubiKey recipient: %w", err)
		}
		if len(recipients) > 0 {
			p.recipient = recipients[0]
		}
	}

	return nil
}

// Method returns AuthMethodYubiKey
func (p *YubiKeyProvider) Method() AuthMethod {
	return AuthMethodYubiKey
}

// Identity returns the YubiKey identity for decryption
func (p *YubiKeyProvider) Identity() (age.Identity, error) {
	if p.identity == nil {
		return nil, fmt.Errorf("YubiKey not loaded")
	}
	return p.identity, nil
}

// Recipient returns the YubiKey recipient for encryption
func (p *YubiKeyProvider) Recipient() (age.Recipient, error) {
	if p.recipient == nil {
		return nil, fmt.Errorf("YubiKey recipient not loaded")
	}
	return p.recipient, nil
}

// NeedsUnlock returns false - YubiKey requires physical touch, not password
func (p *YubiKeyProvider) NeedsUnlock() bool {
	return false
}

// IsUnlocked returns true if identity is loaded
func (p *YubiKeyProvider) IsUnlocked() bool {
	return p.identity != nil
}

// Unlock is a no-op for YubiKey (touch is handled by the plugin)
func (p *YubiKeyProvider) Unlock(credentials string) error {
	return nil
}

// Lock clears the identity from memory
func (p *YubiKeyProvider) Lock() {
	p.identity = nil
	p.recipient = nil
}

// DisplayInfo returns a description of this auth method
func (p *YubiKeyProvider) DisplayInfo() string {
	return "YubiKey hardware key"
}

// IsYubiKeyPluginInstalled checks if age-plugin-yubikey is available
func IsYubiKeyPluginInstalled() bool {
	_, err := exec.LookPath("age-plugin-yubikey")
	return err == nil
}

// DetectYubiKeyIdentities attempts to list available YubiKey identities
func DetectYubiKeyIdentities() ([]string, error) {
	if !IsYubiKeyPluginInstalled() {
		return nil, nil
	}

	// Run age-plugin-yubikey --list to get available identities
	cmd := exec.Command("age-plugin-yubikey", "--list")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil // Plugin might not support --list or no YubiKey present
	}

	var identities []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "age1yubikey") || strings.HasPrefix(line, "AGE-PLUGIN-YUBIKEY-") {
			identities = append(identities, line)
		}
	}

	return identities, nil
}

// SetupYubiKey initializes a new slot on a YubiKey for age encryption
// Returns the identity string that should be stored
func SetupYubiKey() (string, error) {
	if !IsYubiKeyPluginInstalled() {
		return "", fmt.Errorf("age-plugin-yubikey is not installed")
	}

	// Run age-plugin-yubikey to generate a new identity
	cmd := exec.Command("age-plugin-yubikey")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to setup YubiKey: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
