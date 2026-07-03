package storage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// AgeProvider implements AuthProvider for Age identity file encryption
type AgeProvider struct {
	identityPath string
	identity     age.Identity
	recipient    age.Recipient
}

// NewAgeProvider creates a new Age identity provider
func NewAgeProvider(identityPath string) (*AgeProvider, error) {
	if identityPath == "" {
		return nil, fmt.Errorf("identity path is required")
	}

	// Expand home directory
	if strings.HasPrefix(identityPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		identityPath = filepath.Join(home, identityPath[2:])
	}

	p := &AgeProvider{
		identityPath: identityPath,
	}

	// Load identity immediately (age identity files are not password protected)
	if err := p.loadIdentity(); err != nil {
		return nil, err
	}

	return p, nil
}

// GenerateAgeIdentity creates a new Age identity file and returns a provider for it
func GenerateAgeIdentity(identityPath string) (*AgeProvider, error) {
	// Expand home directory
	if strings.HasPrefix(identityPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		identityPath = filepath.Join(home, identityPath[2:])
	}

	// Check if file already exists
	if _, err := os.Stat(identityPath); err == nil {
		return nil, fmt.Errorf("identity file already exists: %s", identityPath)
	}

	// Generate a new identity
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("failed to generate identity: %w", err)
	}

	// Create directory if needed
	dir := filepath.Dir(identityPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write identity file. A swallowed write/close error here could leave a
	// corrupt secret-key file that silently fails to decrypt later, so every
	// step is checked.
	f, err := os.OpenFile(identityPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create identity file: %w", err)
	}

	if _, err := fmt.Fprintf(f, "# created: %s\n", "budget2 encryption"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to write identity file header: %w", err)
	}
	if _, err := fmt.Fprintf(f, "# public key: %s\n", identity.Recipient().String()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to write identity file public key: %w", err)
	}
	if _, err := fmt.Fprintln(f, identity.String()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to write identity file key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("failed to close identity file: %w", err)
	}

	return &AgeProvider{
		identityPath: identityPath,
		identity:     identity,
		recipient:    identity.Recipient(),
	}, nil
}

// loadIdentity loads the identity from the identity file
func (p *AgeProvider) loadIdentity() error {
	f, err := os.Open(p.identityPath)
	if err != nil {
		return fmt.Errorf("failed to open identity file: %w", err)
	}
	defer func() { _ = f.Close() }()

	identities, err := age.ParseIdentities(f)
	if err != nil {
		return fmt.Errorf("failed to parse identity file: %w", err)
	}

	if len(identities) == 0 {
		return fmt.Errorf("no identities found in file")
	}

	// Use the first identity
	p.identity = identities[0]

	// Extract recipient from X25519 identity
	if x25519, ok := p.identity.(*age.X25519Identity); ok {
		p.recipient = x25519.Recipient()
	} else {
		return fmt.Errorf("unsupported identity type")
	}

	return nil
}

// Method returns AuthMethodAge
func (p *AgeProvider) Method() AuthMethod {
	return AuthMethodAge
}

// Identity returns the age identity for decryption
func (p *AgeProvider) Identity() (age.Identity, error) {
	if p.identity == nil {
		return nil, fmt.Errorf("age identity not loaded")
	}
	return p.identity, nil
}

// Recipient returns the age recipient for encryption
func (p *AgeProvider) Recipient() (age.Recipient, error) {
	if p.recipient == nil {
		return nil, fmt.Errorf("age recipient not loaded")
	}
	return p.recipient, nil
}

// NeedsUnlock returns false - age identity files don't require unlock
func (p *AgeProvider) NeedsUnlock() bool {
	return false
}

// IsUnlocked returns true if identity is loaded
func (p *AgeProvider) IsUnlocked() bool {
	return p.identity != nil
}

// Unlock is a no-op for age identities (they're not password protected)
func (p *AgeProvider) Unlock(credentials string) error {
	// Already loaded in constructor
	return nil
}

// Lock clears the identity from memory
func (p *AgeProvider) Lock() {
	p.identity = nil
	p.recipient = nil
}

// DisplayInfo returns a description of this auth method
func (p *AgeProvider) DisplayInfo() string {
	return fmt.Sprintf("Age identity file: %s", p.identityPath)
}

// GetPublicKey returns the public key string for the identity
func (p *AgeProvider) GetPublicKey() string {
	if p.recipient == nil {
		return ""
	}
	return p.recipient.(*age.X25519Recipient).String()
}

// DetectAgeIdentities searches common locations for age identity files
func DetectAgeIdentities() ([]string, error) {
	var identities []string

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Common locations for age keys
	searchPaths := []string{
		filepath.Join(home, ".config", "age", "keys.txt"),
		filepath.Join(home, ".age", "key.txt"),
		filepath.Join(home, ".age", "keys.txt"),
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			// Verify it's a valid identity file
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
					identities = append(identities, path)
					break
				}
			}
			_ = f.Close()
		}
	}

	return identities, nil
}
