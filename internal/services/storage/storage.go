package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// ageHeader is the prefix of Age-encrypted files
	ageHeader = "age-encryption.org"

	// markerFile indicates encryption is enabled
	markerFile = ".encrypted"

	// verifyFile is used to validate the password
	verifyFile = ".encryption-verify"

	// verifyMagic is the expected content in the verify file
	verifyMagic = `{"magic":"budget2-encryption-verify","version":1}`
)

// cacheEntry holds cached decrypted file content. Staleness is keyed on
// both modTime and size: mtime alone is unreliable on filesystems with
// coarse or frozen timestamp granularity (e.g. some WSL2 / network mounts),
// where two rapid rewrites can share an mtime. Size catches any
// length-changing edit regardless. (mtime+size is the same quick-check
// rsync and make use.)
type cacheEntry struct {
	data    []byte
	modTime int64
	size    int64
}

// Storage provides transparent encrypted/unencrypted file access
type Storage struct {
	baseDir   string
	encrypted bool
	provider  AuthProvider      // Current auth provider (nil if locked or not encrypted)
	config    *EncryptionConfig // Configuration for the auth method
	mu        sync.RWMutex
	cache     map[string]*cacheEntry
	cacheMu   sync.RWMutex

	// cacheGen counts cache-invalidating events. A read samples it before it
	// stats the file and may only publish what it read if the counter has not
	// moved since: mtime+size alone cannot tell a reader that the bytes it
	// holds were overtaken by a write mid-read (see invalidateCache).
	//
	// It is deliberately one counter for the whole store rather than one per
	// path. The cost is that a write to any path makes reads of other paths
	// in flight at that moment skip caching for one round; the benefit is
	// that there is no second map to keep in step with this one, and Lock's
	// wholesale clear is covered by the same bump.
	//
	// Guarded by cacheMu, alongside cache itself.
	cacheGen uint64

	// dataMu serializes ordinary data-directory mutations against a wholesale
	// rewrite of that directory -- today, a restore's write+prune phase.
	// Every write takes it shared; BeginExclusive takes it exclusively.
	//
	// It is deliberately NOT mu, which guards encryption state: a restore has
	// no business blocking an unlock, and conflating the two would make the
	// lock's meaning unstateable.
	//
	// The shared side is held for the duration of ONE storage call and never
	// across a call into another service. That is what keeps it deadlock-free
	// against the settings rewrite gate, which a restore holds at the same
	// time: no writer can be waiting on the settings lock while holding this
	// one.
	dataMu sync.RWMutex
}

// New creates a new Storage instance for the given base directory
func New(baseDir string) (*Storage, error) {
	s := &Storage{
		baseDir: baseDir,
		cache:   make(map[string]*cacheEntry),
	}

	// Check if encryption is enabled
	markerPath := filepath.Join(baseDir, markerFile)
	if _, err := os.Stat(markerPath); err == nil {
		s.encrypted = true

		// Load encryption configuration
		config, err := loadConfig(baseDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load encryption config: %w", err)
		}

		// Default to password if no config exists (backward compatibility)
		if config == nil {
			config = &EncryptionConfig{Method: AuthMethodPassword}
		}
		s.config = config
	}

	return s, nil
}

// BaseDir returns the base directory
func (s *Storage) BaseDir() string {
	return s.baseDir
}

// IsEncrypted returns true if the data directory is encrypted
func (s *Storage) IsEncrypted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.encrypted
}

// IsUnlocked returns true if encryption is not enabled or provider is unlocked
func (s *Storage) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.encrypted || (s.provider != nil && s.provider.IsUnlocked())
}

// Unlock decrypts the storage with the given credentials
// For password method, credentials is the password
// For SSH with passphrase, credentials is the passphrase
// For age identity and YubiKey, credentials may be empty
func (s *Storage) Unlock(credentials string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.encrypted {
		return nil // Nothing to unlock
	}

	// Create or get provider based on config
	provider, err := s.createProvider()
	if err != nil {
		return err
	}

	// Unlock the provider with credentials
	if provider.NeedsUnlock() {
		if err := provider.Unlock(credentials); err != nil {
			return err
		}
	}

	// Get identity for verification
	identity, err := provider.Identity()
	if err != nil {
		log.Printf("Storage unlock failed: could not get identity from provider: %v", err)
		return fmt.Errorf("failed to get identity: %w", err)
	}

	// Verify credentials by decrypting the verification file
	verifyPath := filepath.Join(s.baseDir, verifyFile)
	encrypted, err := os.ReadFile(verifyPath)
	if err != nil {
		log.Printf("Storage unlock failed: could not read verification file: %v", err)
		return fmt.Errorf("failed to read verification file: %w", err)
	}

	decrypted, err := decryptData(encrypted, identity)
	if err != nil {
		log.Printf("Storage unlock failed: incorrect credentials")
		provider.Lock()
		return ErrIncorrectCredentials
	}

	if string(decrypted) != verifyMagic {
		log.Printf("Storage unlock failed: verification magic mismatch")
		provider.Lock()
		return fmt.Errorf("%w (verification failed)", ErrIncorrectCredentials)
	}

	// Credentials verified, store provider
	log.Printf("Storage unlocked successfully")
	s.provider = provider
	return nil
}

// createProvider creates an AuthProvider based on the current configuration
func (s *Storage) createProvider() (AuthProvider, error) {
	if s.config == nil {
		// Default to password for backward compatibility
		return NewPasswordProvider(), nil
	}

	switch s.config.Method {
	case AuthMethodPassword:
		return NewPasswordProvider(), nil
	case AuthMethodAge:
		return NewAgeProvider(s.config.AgeIdentityPath)
	case AuthMethodSSH:
		return NewSSHProvider(s.config.SSHKeyPath)
	case AuthMethodYubiKey:
		if s.config.YubiKeyRecipient != "" {
			return NewYubiKeyProviderWithRecipient(s.config.YubiKeyIdentity, s.config.YubiKeyRecipient)
		}
		return NewYubiKeyProvider(s.config.YubiKeyIdentity)
	default:
		return nil, fmt.Errorf("unknown auth method: %s", s.config.Method)
	}
}

// Lock clears the encryption key and cached data from memory
func (s *Storage) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.provider != nil {
		s.provider.Lock()
		s.provider = nil
	}

	// Clear cache for security - don't keep decrypted data in memory. The
	// generation bump matters as much as the clear: it stops a read that
	// decrypted before the lock from publishing that plaintext back into the
	// fresh map afterwards.
	s.cacheMu.Lock()
	s.cache = make(map[string]*cacheEntry)
	s.cacheGen++
	s.cacheMu.Unlock()
}

// invalidateCache drops any cached entry for path and advances the cache
// generation.
//
// Mutations invalidate on BOTH sides: before, so no reader serves the old
// bytes while the new ones are in flight, and after, so a reader that already
// sampled the old file cannot install what it saw once the mutation lands.
// The second half is the load-bearing one. Without it a reader could stat the
// file, read the old payload, watch a whole write complete, and then publish
// the old payload keyed to the old mtime and size -- which, on a filesystem
// with coarse or frozen timestamps and a same-length rewrite, still matches
// the new file and so is served forever.
func (s *Storage) invalidateCache(path string) {
	s.cacheMu.Lock()
	delete(s.cache, path)
	s.cacheGen++
	s.cacheMu.Unlock()
}

// cacheGeneration samples the generation a reader must still match in order
// to publish. Sample it before stat'ing the file, not after.
func (s *Storage) cacheGeneration() uint64 {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return s.cacheGen
}

// publishCache installs entry for path, unless a cache-invalidating event has
// happened since gen was sampled -- in which case the entry describes a file
// state that no longer exists and is dropped. Losing a cache fill costs one
// re-read; installing a stale one costs correctness.
func (s *Storage) publishCache(path string, gen uint64, entry *cacheEntry) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cacheGen != gen {
		return
	}
	s.cache[path] = entry
}

// ReadFile reads and optionally decrypts a file, using cache when possible.
// It is a context-less convenience wrapper around ReadFileContext for callers
// not on an HTTP request path (background jobs, CLIs, tests).
func (s *Storage) ReadFile(path string) ([]byte, error) {
	return s.ReadFileContext(context.Background(), path)
}

// ReadFileContext is ReadFile with caller-supplied cancellation. The
// underlying os.ReadFile + age decryption cannot be interrupted mid-call, so
// ctx provides fail-fast semantics: if the caller has already cancelled (e.g.
// the HTTP client disconnected) the read returns ctx.Err() before touching
// disk rather than doing wasted work.
func (s *Storage) ReadFileContext(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Sample the cache generation BEFORE stat'ing. Everything observed from
	// here on -- the stat and the bytes -- belongs to this generation, and
	// publishCache refuses the entry if a write moved it on in the meantime.
	// Sampling after the stat would reopen the window this closes.
	gen := s.cacheGeneration()

	// Get file modification time
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	modTime := info.ModTime().UnixNano()
	size := info.Size()

	// Check cache first
	s.cacheMu.RLock()
	if entry, ok := s.cache[path]; ok && entry.modTime == modTime && entry.size == size {
		// Cache hit - return copy to prevent mutation
		data := make([]byte, len(entry.data))
		copy(data, entry.data)
		s.cacheMu.RUnlock()
		return data, nil
	}
	s.cacheMu.RUnlock()

	// Cache miss - read from disk
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Check if file is encrypted
	if isAgeEncrypted(data) {
		if s.provider == nil || !s.provider.IsUnlocked() {
			return nil, fmt.Errorf("file is encrypted but storage is locked")
		}
		identity, err := s.provider.Identity()
		if err != nil {
			return nil, fmt.Errorf("failed to get identity: %w", err)
		}
		data, err = decryptData(data, identity)
		if err != nil {
			log.Printf("Warning: failed to decrypt %s: %v", path, err)
			return nil, fmt.Errorf("decryption failed for %s: %w", path, err)
		}
	}

	// Store in cache, unless a write overtook this read since gen was sampled
	s.publishCache(path, gen, &cacheEntry{data: data, modTime: modTime, size: size})

	// Return a copy to prevent mutation
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// WriteFile writes and optionally encrypts a file. Context-less convenience
// wrapper around WriteFileContext for non-request callers.
func (s *Storage) WriteFile(path string, data []byte, perm os.FileMode) error {
	return s.WriteFileContext(context.Background(), path, data, perm)
}

// WriteFileContext is WriteFile with caller-supplied cancellation. As with
// ReadFileContext, encryption + the atomic write cannot be interrupted
// mid-call, so ctx fails fast before any work when already cancelled.
func (s *Storage) WriteFileContext(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	return s.writeFileLocked(path, data, perm)
}

// writeFileLocked is WriteFileContext's body without the data-directory lock.
// Callers must already hold dataMu, shared or exclusive. It exists because
// sync.RWMutex is not reentrant: a restore holding the exclusive lock would
// deadlock on its own writes if they went through the public method. Same
// shape as backup.Service's snapshotLocked.
func (s *Storage) writeFileLocked(path string, data []byte, perm os.FileMode) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Invalidate before staging, so no reader serves the old bytes from cache
	// while the new ones are on their way to disk. The encryption failures
	// below return before touching disk, so they need no second invalidation.
	s.invalidateCache(path)

	// Skip encryption for certain files
	if !s.shouldSkipEncryption(path) {
		// Encrypt if enabled and unlocked
		if s.encrypted && s.provider != nil && s.provider.IsUnlocked() {
			if isAgeEncrypted(data) {
				// Already encrypted (e.g. restoring a backup blob). Pass through.
			} else {
				recipient, err := s.provider.Recipient()
				if err != nil {
					return fmt.Errorf("failed to get recipient: %w", err)
				}
				encrypted, err := encryptData(data, recipient)
				if err != nil {
					return fmt.Errorf("failed to encrypt: %w", err)
				}
				data = encrypted
			}
		}
	}

	err := s.atomicWrite(path, data, perm)

	// Invalidate again now that the rename has published (or failed to
	// publish) the new bytes. This is what orders the cache against the
	// write: any read still holding pre-write bytes sampled an older
	// generation and is now barred from installing them. Unconditional --
	// a failed write leaves the file's state unproven, and re-reading is
	// cheaper than guessing.
	s.invalidateCache(path)
	return err
}

// OpenFile returns a reader for a potentially encrypted file. Context-less
// convenience wrapper around OpenFileContext.
func (s *Storage) OpenFile(path string) (io.ReadCloser, error) {
	return s.OpenFileContext(context.Background(), path)
}

// OpenFileContext is OpenFile with caller-supplied cancellation.
func (s *Storage) OpenFileContext(ctx context.Context, path string) (io.ReadCloser, error) {
	data, err := s.ReadFileContext(ctx, path)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// atomicWrite writes data to a file atomically using a temp file
func (s *Storage) atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write to temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}

// IsEncryptionStateFile reports whether base names one of the files that
// record the store's encryption state (.encrypted, .encryption-verify,
// .encryption-config.json). These files define how the store unlocks, so
// they must never be archived into backups, written from restore archives,
// or pruned during a restore.
func IsEncryptionStateFile(base string) bool {
	return base == markerFile || base == verifyFile || base == configFile
}

// shouldSkipEncryption returns true for files that shouldn't be encrypted
func (s *Storage) shouldSkipEncryption(path string) bool {
	base := filepath.Base(path)

	// Skip marker, verify, and config files
	if IsEncryptionStateFile(base) {
		return true
	}

	// Skip cache directory (e.g., plotly.min.js)
	if strings.Contains(path, "/cache/") || strings.Contains(path, "\\cache\\") {
		return true
	}

	return false
}

// IsAgeEncryptedData reports whether data appears to be an age-encrypted
// payload by looking at its magic header. Used by callers (e.g. the backup
// restore handler) that need to detect encrypted blobs before deciding how
// to write them.
func IsAgeEncryptedData(data []byte) bool {
	return len(data) > len(ageHeader) && string(data[:len(ageHeader)]) == ageHeader
}

// isAgeEncrypted is kept for internal callers.
func isAgeEncrypted(data []byte) bool { return IsAgeEncryptedData(data) }

// Stat returns file info, useful for checking existence
func (s *Storage) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Glob returns files matching a pattern
func (s *Storage) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// MkdirAll creates a directory and all parents
func (s *Storage) MkdirAll(path string, perm os.FileMode) error {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	return os.MkdirAll(path, perm)
}

// Remove removes a file and invalidates its cache entry
func (s *Storage) Remove(path string) error {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	return s.removeLocked(path)
}

// removeLocked is Remove without the data-directory lock; see writeFileLocked.
func (s *Storage) removeLocked(path string) error {
	// Invalidated on both sides for the same reason as writeFileLocked: a
	// read in flight must not be able to install the removed file's bytes
	// after the unlink, where a same-size same-mtime recreation would then
	// match them.
	s.invalidateCache(path)
	err := os.Remove(path)
	s.invalidateCache(path)
	return err
}

// ExclusiveWriter holds the data directory against every other writer for as
// long as it is open. It is how a restore rewrites the directory without a
// concurrent write landing in the window between the rewrite and the prune --
// where the prune would delete a file the user had just created.
//
// Its methods bypass the lock they already hold, so nothing inside an
// exclusive section may call the plain Storage write methods: that is the one
// way to deadlock this.
//
// Reads are deliberately NOT excluded. A reader during a restore can see a
// partly rewritten tree, which is a display concern the browser resolves by
// reloading; making every page render contend with a restore would be a much
// larger change for a much smaller problem.
type ExclusiveWriter struct {
	s        *Storage
	released bool
}

// BeginExclusive blocks until every in-flight write has finished, then holds
// the data directory until Release. Callers MUST Release, normally by defer.
//
// Lock order where a caller takes more than one: settings rewrite gate ->
// this -> backup snapshot hold. A restore takes all three (see
// internal/services/restore). The gate comes first because
// SettingsManager.SaveWithRevision holds the manager's lock across its write
// through this Storage, so a caller taking this one first would be the other
// half of an ABBA deadlock.
func (s *Storage) BeginExclusive() *ExclusiveWriter {
	s.dataMu.Lock()
	return &ExclusiveWriter{s: s}
}

// Release gives the data directory back. Safe to call more than once so a
// deferred Release cannot double-unlock a mutex after an explicit one.
func (w *ExclusiveWriter) Release() {
	if w == nil || w.released {
		return
	}
	w.released = true
	w.s.dataMu.Unlock()
}

// WriteFile writes through the exclusive hold. Same semantics as
// Storage.WriteFile, minus the locking it would deadlock on.
func (w *ExclusiveWriter) WriteFile(path string, data []byte, perm os.FileMode) error {
	if w.released {
		return fmt.Errorf("storage: write attempted after the exclusive hold was released")
	}
	return w.s.writeFileLocked(path, data, perm)
}

// Remove removes through the exclusive hold.
func (w *ExclusiveWriter) Remove(path string) error {
	if w.released {
		return fmt.Errorf("storage: remove attempted after the exclusive hold was released")
	}
	return w.s.removeLocked(path)
}

// MkdirAll creates a directory through the exclusive hold.
func (w *ExclusiveWriter) MkdirAll(path string, perm os.FileMode) error {
	if w.released {
		return fmt.Errorf("storage: mkdir attempted after the exclusive hold was released")
	}
	return os.MkdirAll(path, perm)
}
