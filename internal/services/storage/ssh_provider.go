package storage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

// SSHProvider implements AuthProvider for SSH key encryption
type SSHProvider struct {
	keyPath    string
	passphrase string
	identity   age.Identity
	recipient  age.Recipient
}

// NewSSHProvider creates a new SSH key provider
func NewSSHProvider(keyPath string) (*SSHProvider, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("SSH key path is required")
	}

	// Expand home directory
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[2:])
	}

	p := &SSHProvider{
		keyPath: keyPath,
	}

	// Load the public key to get recipient
	if err := p.loadPublicKey(); err != nil {
		return nil, err
	}

	return p, nil
}

// loadPublicKey loads the SSH public key to create the recipient
func (p *SSHProvider) loadPublicKey() error {
	// Try to find public key (either .pub file or extract from private key)
	pubKeyPath := p.keyPath + ".pub"
	pubKeyData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key %s: %w", pubKeyPath, err)
	}

	recipient, err := agessh.ParseRecipient(string(pubKeyData))
	if err != nil {
		return fmt.Errorf("failed to parse SSH public key: %w", err)
	}

	p.recipient = recipient
	return nil
}

// loadIdentity loads the SSH private key with passphrase if needed
func (p *SSHProvider) loadIdentity() error {
	keyData, err := os.ReadFile(p.keyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH key: %w", err)
	}

	// The agessh library handles encrypted keys internally by prompting
	// For our use case, we need to decrypt the key ourselves if it's encrypted
	if p.passphrase != "" {
		// Decrypt the key if it's encrypted
		keyData, err = decryptSSHKey(keyData, []byte(p.passphrase))
		if err != nil {
			return fmt.Errorf("failed to decrypt SSH key: %w", err)
		}
	}

	identity, err := agessh.ParseIdentity(keyData)
	if err != nil {
		if strings.Contains(err.Error(), "encrypted") || strings.Contains(err.Error(), "passphrase") {
			return fmt.Errorf("SSH key is encrypted, passphrase required")
		}
		return fmt.Errorf("failed to parse SSH key: %w", err)
	}

	p.identity = identity
	return nil
}

// decryptSSHKey decrypts an encrypted SSH private key
func decryptSSHKey(keyData []byte, passphrase []byte) ([]byte, error) {
	// The ssh package handles passphrase-protected keys
	// For now, we require unencrypted keys or keys already decrypted
	// A full implementation would use golang.org/x/crypto/ssh to decrypt
	return nil, fmt.Errorf("encrypted SSH keys require passphrase decryption support (not yet implemented)")
}

// Method returns AuthMethodSSH
func (p *SSHProvider) Method() AuthMethod {
	return AuthMethodSSH
}

// Identity returns the SSH identity for decryption
func (p *SSHProvider) Identity() (age.Identity, error) {
	if p.identity == nil {
		return nil, fmt.Errorf("SSH key not unlocked")
	}
	return p.identity, nil
}

// Recipient returns the SSH recipient for encryption
func (p *SSHProvider) Recipient() (age.Recipient, error) {
	if p.recipient == nil {
		return nil, fmt.Errorf("SSH public key not loaded")
	}
	return p.recipient, nil
}

// NeedsUnlock returns true - SSH keys may need passphrase
func (p *SSHProvider) NeedsUnlock() bool {
	return true
}

// IsUnlocked returns true if identity is loaded
func (p *SSHProvider) IsUnlocked() bool {
	return p.identity != nil
}

// Unlock loads the SSH private key with the given passphrase
func (p *SSHProvider) Unlock(passphrase string) error {
	p.passphrase = passphrase
	return p.loadIdentity()
}

// Lock clears the identity from memory
func (p *SSHProvider) Lock() {
	p.identity = nil
	p.passphrase = ""
}

// DisplayInfo returns a description of this auth method
func (p *SSHProvider) DisplayInfo() string {
	return fmt.Sprintf("SSH key: %s", p.keyPath)
}

// SSHKeyInfo describes a detected SSH key
type SSHKeyInfo struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	Encrypted bool   `json:"encrypted"`
}

// DetectSSHKeys searches for SSH keys in ~/.ssh
func DetectSSHKeys() ([]SSHKeyInfo, error) {
	var keys []SSHKeyInfo

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	sshDir := filepath.Join(home, ".ssh")
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		if os.IsNotExist(err) {
			return keys, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Look for private key files (no .pub extension)
		if strings.HasSuffix(name, ".pub") {
			continue
		}

		// Common private key names
		if !strings.HasPrefix(name, "id_") && name != "identity" {
			continue
		}

		keyPath := filepath.Join(sshDir, name)

		// Check if public key exists
		if _, err := os.Stat(keyPath + ".pub"); err != nil {
			continue
		}

		info := SSHKeyInfo{
			Path: keyPath,
		}

		// Determine key type and encryption status
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}

		keyStr := string(keyData)
		if strings.Contains(keyStr, "RSA") {
			info.Type = "rsa"
		} else if strings.Contains(keyStr, "EC") {
			info.Type = "ecdsa"
		} else if strings.Contains(keyStr, "OPENSSH") {
			info.Type = "ed25519"
		}

		info.Encrypted = strings.Contains(keyStr, "ENCRYPTED")

		keys = append(keys, info)
	}

	return keys, nil
}

// IsSSHKeyEncrypted checks if an SSH key requires a passphrase
func IsSSHKeyEncrypted(keyPath string) (bool, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return false, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "ENCRYPTED") {
			return true, nil
		}
	}

	return false, nil
}
