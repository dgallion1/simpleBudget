// Package storage is the encrypted-at-rest persistence layer for user
// state (aliases, duplicate decisions, Amazon enrichment, what-if
// settings, etc.). It wraps filippo.io/age and supports several auth
// methods — passphrase, SSH agent, raw age recipient, and YubiKey — and
// owns the on-disk encryption-config file, key derivation, identity
// management, and the read/write/migrate primitives every other service
// uses to persist user data.
package storage
