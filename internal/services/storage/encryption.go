package storage

import (
	"bytes"
	"io"

	"filippo.io/age"
)

// encryptData encrypts data using Age with the given recipient
// Accepts any age.Recipient implementation (ScryptRecipient, X25519Recipient, SSH, etc.)
func encryptData(data []byte, recipient age.Recipient) ([]byte, error) {
	var buf bytes.Buffer

	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, err
	}

	if _, err := w.Write(data); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decryptData decrypts Age-encrypted data using the given identity
// Accepts any age.Identity implementation (ScryptIdentity, X25519Identity, SSH, etc.)
func decryptData(data []byte, identity age.Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(r)
}
