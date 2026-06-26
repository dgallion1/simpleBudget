package storage

import "errors"

// ErrIncorrectCredentials is returned when an unlock or encryption-change
// attempt fails because the supplied credentials cannot decrypt the
// verification payload. Callers should match it with errors.Is rather than
// inspecting the error text.
var ErrIncorrectCredentials = errors.New("incorrect credentials")

// ErrSSHKeyEncrypted is returned when an SSH private key is passphrase-
// protected and no passphrase (or an incorrect one) was supplied. It wraps
// the underlying parse failure so callers can detect the condition with
// errors.Is instead of string-matching the third-party error message.
var ErrSSHKeyEncrypted = errors.New("SSH key is encrypted, passphrase required")
