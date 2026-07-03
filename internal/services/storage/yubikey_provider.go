package storage

import (
	"bufio"
	"bytes"
	"fmt"
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

// YubiKeySlot represents a YubiKey slot that can be used for age encryption
type YubiKeySlot struct {
	Slot      string `json:"slot"`      // Slot number (e.g., "1", "2")
	Name      string `json:"name"`      // Descriptive name
	Serial    string `json:"serial"`    // YubiKey serial number
	Recipient string `json:"recipient"` // age1yubikey1... recipient
}

// NewYubiKeyProvider creates a new YubiKey provider
// identityStr should be the AGE-PLUGIN-YUBIKEY-... identity string
// recipientStr should be the age1yubikey1... recipient string
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

	// Extract recipient from identity string if present as a comment
	p.extractRecipientFromIdentity()

	// Parse the identity string to get identity and recipient
	if err := p.loadIdentity(); err != nil {
		return nil, err
	}

	return p, nil
}

// NewYubiKeyProviderWithRecipient creates a new YubiKey provider with explicit recipient
func NewYubiKeyProviderWithRecipient(identityStr, recipientStr string) (*YubiKeyProvider, error) {
	if identityStr == "" {
		return nil, fmt.Errorf("YubiKey identity string is required")
	}

	if !IsYubiKeyPluginInstalled() {
		return nil, fmt.Errorf("age-plugin-yubikey is not installed")
	}

	p := &YubiKeyProvider{
		identityStr:  identityStr,
		recipientStr: recipientStr,
	}

	if err := p.loadIdentity(); err != nil {
		return nil, err
	}

	return p, nil
}

// extractRecipientFromIdentity looks for a recipient comment in the identity string
// age-plugin-yubikey outputs identity files in this format:
// # created: 2024-01-01T00:00:00Z
// # recipient: age1yubikey1...
// AGE-PLUGIN-YUBIKEY-...
func (p *YubiKeyProvider) extractRecipientFromIdentity() {
	scanner := bufio.NewScanner(strings.NewReader(p.identityStr))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# recipient:") {
			p.recipientStr = strings.TrimSpace(strings.TrimPrefix(line, "# recipient:"))
			return
		}
		if strings.HasPrefix(line, "#recipient:") {
			p.recipientStr = strings.TrimSpace(strings.TrimPrefix(line, "#recipient:"))
			return
		}
	}
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

	// Parse the recipient string
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

// ListYubiKeySlots returns information about available YubiKey slots
func ListYubiKeySlots() ([]YubiKeySlot, error) {
	if !IsYubiKeyPluginInstalled() {
		return nil, nil
	}

	// Run age-plugin-yubikey --list to get available slots
	cmd := exec.Command("age-plugin-yubikey", "--list")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Plugin might not have a YubiKey connected
		return nil, nil
	}

	var slots []YubiKeySlot
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Parse lines like "age1yubikey1..." (recipient lines)
		if strings.HasPrefix(line, "age1yubikey1") {
			slots = append(slots, YubiKeySlot{
				Recipient: line,
			})
		}
	}

	return slots, nil
}

// DetectYubiKeyIdentities attempts to list available YubiKey recipients
func DetectYubiKeyIdentities() ([]string, error) {
	if !IsYubiKeyPluginInstalled() {
		return nil, nil
	}

	slots, err := ListYubiKeySlots()
	if err != nil {
		return nil, err
	}

	var recipients []string
	for _, slot := range slots {
		if slot.Recipient != "" {
			recipients = append(recipients, slot.Recipient)
		}
	}

	return recipients, nil
}

// YubiKeySetupResult contains the result of setting up a YubiKey identity
type YubiKeySetupResult struct {
	Identity  string `json:"identity"`  // AGE-PLUGIN-YUBIKEY-... identity string
	Recipient string `json:"recipient"` // age1yubikey1... recipient string
	FullText  string `json:"full_text"` // Complete output including comments
}

// YubiKeyInfo contains information about connected YubiKeys
type YubiKeyInfo struct {
	Serial       string `json:"serial"`
	HasIdentity  bool   `json:"has_identity"`
	SetupCommand string `json:"setup_command"`
}

// DetectYubiKeys returns information about connected YubiKeys
func DetectYubiKeys() ([]YubiKeyInfo, error) {
	if !IsYubiKeyPluginInstalled() {
		return nil, fmt.Errorf("age-plugin-yubikey is not installed")
	}

	// Use ykman to detect YubiKeys if available, otherwise parse from age-plugin-yubikey output
	cmd := exec.Command("age-plugin-yubikey", "--list-all")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Even if this fails, we might have a YubiKey - just no keys configured
	_ = cmd.Run()

	// Try to get serial from ykman if available
	ykmanCmd := exec.Command("ykman", "list", "--serials")
	var ykmanOut bytes.Buffer
	ykmanCmd.Stdout = &ykmanOut

	var keys []YubiKeyInfo
	if err := ykmanCmd.Run(); err == nil {
		scanner := bufio.NewScanner(&ykmanOut)
		for scanner.Scan() {
			serial := strings.TrimSpace(scanner.Text())
			if serial != "" {
				keys = append(keys, YubiKeyInfo{
					Serial:       serial,
					HasIdentity:  false,
					SetupCommand: fmt.Sprintf("age-plugin-yubikey --generate --serial %s", serial),
				})
			}
		}
	}

	// Check which ones have identities
	existingRecipients, _ := DetectYubiKeyIdentities()
	for i := range keys {
		for _, r := range existingRecipients {
			// If we have any recipients, mark the first key as having identity
			// (In practice, users typically have one YubiKey)
			if len(r) > 0 {
				keys[i].HasIdentity = true
				break
			}
		}
	}

	// If ykman isn't available but we detected a YubiKey via the interactive prompt,
	// return a generic entry
	if len(keys) == 0 {
		keys = append(keys, YubiKeyInfo{
			Serial:       "",
			HasIdentity:  len(existingRecipients) > 0,
			SetupCommand: "age-plugin-yubikey --generate",
		})
	}

	return keys, nil
}

// SetupYubiKey is deprecated - YubiKey setup requires terminal interaction
// This now returns an error with instructions for the user
func SetupYubiKey() (*YubiKeySetupResult, error) {
	return nil, fmt.Errorf("YubiKey setup requires terminal interaction. Please run 'age-plugin-yubikey --generate' in your terminal, then refresh this page")
}

// GetYubiKeyIdentityForRecipient retrieves the identity for a known recipient
// by running age-plugin-yubikey --identity
func GetYubiKeyIdentityForRecipient(recipient string) (string, error) {
	if !IsYubiKeyPluginInstalled() {
		return "", fmt.Errorf("age-plugin-yubikey is not installed")
	}

	// Try to list identities and find one matching the recipient
	cmd := exec.Command("age-plugin-yubikey", "--identity")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get YubiKey identity: %w", err)
	}

	output := stdout.String()

	// Check if this output contains the recipient we're looking for
	if strings.Contains(output, recipient) {
		return strings.TrimSpace(output), nil
	}

	// If not found, return all identities and let the caller handle it
	return strings.TrimSpace(output), nil
}
