package storage

import (
	"bytes"
	"context"
	"errors"
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

	payload, err := s.encodeForWrite(path, data)
	if err != nil {
		return err
	}
	err = s.atomicWrite(path, payload, perm)

	// Invalidate again now that the rename has published (or failed to
	// publish) the new bytes. This is what orders the cache against the
	// write: any read still holding pre-write bytes sampled an older
	// generation and is now barred from installing them. Unconditional --
	// a failed write leaves the file's state unproven, and re-reading is
	// cheaper than guessing.
	s.invalidateCache(path)
	return err
}

// encodeForWrite invalidates path's cache entry and returns the bytes that
// should land on disk, encrypting them when the store is encrypted and
// unlocked. Caller holds mu. Split out of writeFileLocked so that every write
// path — atomic replace and atomic create-if-absent alike — applies one
// encryption rule rather than two that can drift.
func (s *Storage) encodeForWrite(path string, data []byte) ([]byte, error) {
	// Invalidate before staging, so no reader serves the old bytes from cache
	// while the new ones are on their way to disk. invalidateCache, not a bare
	// delete: it also bumps the generation, which is what bars an in-flight
	// read from publishing pre-write bytes it has already loaded.
	s.invalidateCache(path)

	// Skip encryption for certain files
	if s.shouldSkipEncryption(path) {
		return data, nil
	}

	// Encrypt if enabled and unlocked
	if s.encrypted && s.provider != nil && s.provider.IsUnlocked() {
		if isAgeEncrypted(data) {
			// Already encrypted (e.g. restoring a backup blob). Pass through.
			return data, nil
		}
		recipient, err := s.provider.Recipient()
		if err != nil {
			return nil, fmt.Errorf("failed to get recipient: %w", err)
		}
		encrypted, err := encryptData(data, recipient)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt: %w", err)
		}
		return encrypted, nil
	}

	return data, nil
}

// CreateExclusive writes data to path only when path does not already exist,
// returning an error satisfying errors.Is(err, os.ErrExist) when it does.
//
// Unlike Stat-then-WriteFile, the test and the create are one indivisible
// step. That sequence has two failure modes this does not: two concurrent
// callers can both observe "absent" and then overwrite each other, and a Stat
// that fails for any reason other than non-existence (a permission error, an
// I/O error) reads as "absent" and proceeds to overwrite.
func (s *Storage) CreateExclusive(path string, data []byte, perm os.FileMode) error {
	s.dataMu.RLock()
	defer s.dataMu.RUnlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	payload, err := s.encodeForWrite(path, data)
	if err != nil {
		return err
	}
	err = createExclusive(path, payload, perm)

	// Same post-publish invalidation as writeFileLocked: this is a write
	// path, and on the EEXIST branch it is a write path that just proved
	// the file's on-disk state is not what the caller assumed.
	s.invalidateCache(path)
	return err
}

// StagingSuffix separates a staging file's destination-derived prefix from
// the random component os.CreateTemp appends, in both createExclusive below
// and atomicWrite further down: <destination-base>+StagingSuffix+<random>.
// It is exported, and IsStagingName below is built on it, so that a
// consumer needing to recognise (or, in a test, construct) a staging name
// derives it from here instead of duplicating the separator as a string
// literal in another package — the two cannot drift apart because there is
// only one definition.
//
// legacyStagingSuffix is the pre-fix staging name, <destination-base>.tmp,
// used by both functions before concurrent writers could otherwise share
// one staging file (see atomicWrite's doc comment). Neither function
// produces it anymore, but a crash before this change could have orphaned
// one in a real data directory, so IsStagingName still recognises it.
const (
	StagingSuffix       = ".tmp-"
	legacyStagingSuffix = ".tmp"
)

// IsStagingName reports whether base is the name of a staging file produced
// by createExclusive or atomicWrite: <destination-base>+StagingSuffix+
// <decimal random>, e.g. "sidecar.json.tmp-2496562633". It also recognises
// the legacyStagingSuffix form for staging files orphaned by a crash before
// staging names were randomised.
//
// Consumers that need to treat a leftover staging file specially — notably
// backup.SkipPredicate, which must exclude one from a snapshot and from
// restore-time pruning — call this instead of matching a string literal of
// their own, so a future change to the staging convention is a compile-time
// change here rather than a silent behavior change somewhere else.
func IsStagingName(base string) bool {
	if strings.HasSuffix(base, legacyStagingSuffix) {
		return true
	}
	i := strings.LastIndex(base, StagingSuffix)
	if i < 0 {
		return false
	}
	random := base[i+len(StagingSuffix):]
	if random == "" {
		return false
	}
	for _, r := range random {
		if r < '0' || r > '9' {
			// Not the decimal suffix os.CreateTemp generates — a legitimate
			// file name that merely contains StagingSuffix as a substring
			// (e.g. "report.tmp-notes.txt") is not a staging leftover.
			return false
		}
	}
	return true
}

// createExclusive stages the payload beside its destination and publishes it
// with a hard link. Link, not rename: rename silently replaces an existing
// destination, link fails with EEXIST. That is what makes this both atomic
// against a concurrent creator and all-or-nothing against a crash.
func createExclusive(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Staged in the destination directory so the link stays inside one
	// filesystem, and under a unique name so concurrent callers cannot
	// collide on the staging file itself.
	f, err := os.CreateTemp(dir, filepath.Base(path)+StagingSuffix+"*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}

	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("storage: %s already exists: %w", path, os.ErrExist)
		}
		return err
	}
	return nil
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

// atomicWrite writes data to a file atomically using a temp file. The
// staging file is created under a unique name (mirroring createExclusive
// above) so that concurrent writers to the same destination — permitted by
// WriteFile's shared lock — never share a staging file: each writer gets its
// own temp file, so neither a spurious rename failure nor torn content from
// two writers' bytes interleaving in one staging file can occur.
//
// The rename below replaces whatever currently sits at path, symlink or not:
// if path is a symlink, the rename removes the link and puts a regular file
// in its place, so the file the link used to point at is left untouched and
// silently stops receiving further writes. That is the contract, not an
// oversight — resolving the symlink first would relocate staging to the
// target's directory, reopening the atomicity reasoning above for a
// filesystem this function does not control, cannot stay atomic if the
// target lives on a different filesystem than the link, and would add a
// resolve-to-rename race to the most safety-critical write path in the
// package. Files under Storage are expected to be real files; symlinks in
// the data directory are not honoured.
func (s *Storage) atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Staged in the destination directory (same filesystem, so the rename
	// below is atomic) under a unique name so concurrent callers cannot
	// collide on the staging file itself.
	f, err := os.CreateTemp(dir, filepath.Base(path)+StagingSuffix+"*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	// If the rename below succeeds this is a no-op (nothing left at
	// tmpPath); if we return early on an error this cleans up the staging
	// file so a failed write doesn't litter the data directory.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}

	// Rename, unlike Link, replaces an existing destination, which is what
	// gives atomicWrite its rewrite semantics.
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

// SharedTx holds the data directory shared for the length of one
// read-modify-write, so a restore's exclusive hold cannot be granted partway
// through it.
//
// The problem it solves: WriteFile takes the shared lock for the write alone.
// A sidecar update reads a JSON file, edits the decoded value, and writes it
// back, and only the last of those three steps was inside the lock. A restore
// acquiring the exclusive hold in between replaces the file on disk, and then
// the blocked writer wakes up and persists a value it derived from the
// pre-restore contents — resurrecting decisions, pins, aliases, or major
// expenses the restore had just rolled back, with the restore reporting
// success.
//
// Its WriteFile bypasses the lock the transaction already holds, for the same
// reason ExclusiveWriter's does: sync.RWMutex is not reentrant, so a second
// RLock while a restore is queued for the write lock blocks forever. Nothing
// inside a transaction may call the plain Storage write methods.
//
// ReadFile does not need that treatment — Storage's read path takes no data
// lock at all — but is provided so a transaction reads and writes through one
// handle rather than two, and so the read is unambiguously inside the hold.
//
// Scope rule, inherited from dataMu: a transaction may span storage calls and
// pure computation on their contents, and must not span a call into another
// service. Holding it across the settings manager would put a shared holder
// behind the settings rewrite gate that a restore takes ahead of the exclusive
// hold, which is the ABBA deadlock the gate ordering exists to prevent.
type SharedTx struct {
	s        *Storage
	released bool
}

// BeginShared holds the data directory shared until Release, admitting other
// shared holders but excluding a restore. Callers MUST Release, normally by
// defer.
//
// Lock order where a caller takes more than one: the caller's own
// serialization for these sequences (dataloader's writeMu) -> this. Never the
// reverse, and nothing that holds the data lock in either mode may then wait
// on that serialization.
func (s *Storage) BeginShared() *SharedTx {
	s.dataMu.RLock()
	return &SharedTx{s: s}
}

// Release gives the shared hold back. Safe to call more than once so a
// deferred Release cannot double-unlock a mutex after an explicit one.
func (t *SharedTx) Release() {
	if t == nil || t.released {
		return
	}
	t.released = true
	t.s.dataMu.RUnlock()
}

// ReadFile reads through the transaction. Same semantics as Storage.ReadFile.
func (t *SharedTx) ReadFile(path string) ([]byte, error) {
	if t.released {
		return nil, fmt.Errorf("storage: read attempted after the shared hold was released")
	}
	return t.s.ReadFile(path)
}

// WriteFile writes through the transaction. Same semantics as
// Storage.WriteFile, minus the locking it would deadlock on.
func (t *SharedTx) WriteFile(path string, data []byte, perm os.FileMode) error {
	if t.released {
		return fmt.Errorf("storage: write attempted after the shared hold was released")
	}
	return t.s.writeFileLocked(path, data, perm)
}
