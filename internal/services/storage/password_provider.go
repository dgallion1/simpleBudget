package storage

import (
	"fmt"

	"filippo.io/age"
)

// PasswordProvider implements AuthProvider for password-based encryption
type PasswordProvider struct {
	identity  *age.ScryptIdentity
	recipient *age.ScryptRecipient
}

// NewPasswordProvider creates a new password provider (unlocked state)
func NewPasswordProvider() *PasswordProvider {
	return &PasswordProvider{}
}

// NewPasswordProviderWithCredentials creates a password provider with the password already set
func NewPasswordProviderWithCredentials(password string) (*PasswordProvider, error) {
	if len(password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}
	p := NewPasswordProvider()
	if err := p.Unlock(password); err != nil {
		return nil, err
	}
	return p, nil
}

// Method returns AuthMethodPassword
func (p *PasswordProvider) Method() AuthMethod {
	return AuthMethodPassword
}

// Identity returns the scrypt identity for decryption
func (p *PasswordProvider) Identity() (age.Identity, error) {
	if p.identity == nil {
		return nil, fmt.Errorf("password provider is locked")
	}
	return p.identity, nil
}

// Recipient returns the scrypt recipient for encryption
func (p *PasswordProvider) Recipient() (age.Recipient, error) {
	if p.recipient == nil {
		return nil, fmt.Errorf("password provider is locked")
	}
	return p.recipient, nil
}

// NeedsUnlock returns true - passwords always need to be provided
func (p *PasswordProvider) NeedsUnlock() bool {
	return true
}

// IsUnlocked returns true if the password has been provided
func (p *PasswordProvider) IsUnlocked() bool {
	return p.identity != nil
}

// Unlock sets the password and creates identity/recipient
func (p *PasswordProvider) Unlock(password string) error {
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return fmt.Errorf("failed to create identity: %w", err)
	}

	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return fmt.Errorf("failed to create recipient: %w", err)
	}

	p.identity = identity
	p.recipient = recipient
	return nil
}

// Lock clears the password credentials from memory
func (p *PasswordProvider) Lock() {
	p.identity = nil
	p.recipient = nil
}

// DisplayInfo returns a description of this auth method
func (p *PasswordProvider) DisplayInfo() string {
	return "Password-based encryption (scrypt)"
}
