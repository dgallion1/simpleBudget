// Package accounts owns the Account sidecar: reading and writing
// data/accounts.json through internal/services/storage (so age encryption
// applies exactly as it does for every other sidecar), and matching a CSV
// file's basename to the account that owns it.
//
// Matching is deliberately order-independent: accounts are considered in
// ascending ID order, so the same file always resolves to the same account
// no matter what order the caller happens to hold them in. See GLOSSARY.md
// ("Account").
package accounts

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

// AccountsFile is the sidecar's basename inside the data directory.
const AccountsFile = "accounts.json"

// accountsDoc is the persisted shape of accounts.json. The wrapping object
// matches the convention the other list-shaped sidecars already use
// (major_expenses.json, duplicate_decisions.json): keeping the file's top
// level an object leaves room for additional configuration later without
// breaking older files. Load also accepts a bare JSON array so a
// hand-written fixture in either shape reads correctly.
type accountsDoc struct {
	Accounts []models.Account `json:"accounts"`
}

// Path returns the location of the accounts sidecar for a storage service.
func Path(s *storage.Storage) string {
	return filepath.Join(s.BaseDir(), AccountsFile)
}

// Load reads accounts.json through the storage service. A missing file is
// not an error: it returns an empty slice and nil, because "no accounts
// configured yet" is the normal starting state, not a failure.
func Load(s *storage.Storage) ([]models.Account, error) {
	if s == nil {
		return nil, fmt.Errorf("accounts: nil storage")
	}
	data, err := s.ReadFile(Path(s))
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Account{}, nil
		}
		return nil, err
	}
	return decode(data)
}

// mu serializes every Mutate sequence over accounts.json against every
// other one, the same job dataloader's writeMu does for its own sidecars.
// It is a package-level lock rather than one hung off *storage.Storage
// because every caller in this process shares the same accounts.json --
// there is exactly one sidecar to serialize, not one per Storage value.
//
// It is taken OUTSIDE storage's BeginShared, never the reverse, and nothing
// held while it is locked ever waits on storage's exclusive hold: Mutate's
// only storage calls are the transaction's own ReadFile/WriteFile, which do
// not block behind a restore any differently than the shared hold already
// does.
var mu sync.Mutex

// Mutate serializes a load-modify-save of the accounts sidecar. fn receives
// the currently stored accounts and returns the set to save. The load, fn and
// the save happen inside one held section, so concurrent mutations do not lose
// each other's writes and a restore cannot land between the load and the save.
// An error from fn aborts the mutation without saving.
func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error {
	if s == nil {
		return fmt.Errorf("accounts: nil storage")
	}
	mu.Lock()
	defer mu.Unlock()

	tx := s.BeginShared()
	defer tx.Release()

	accts, err := loadTx(tx, s)
	if err != nil {
		return err
	}
	next, err := fn(accts)
	if err != nil {
		return err
	}
	_, err = saveTx(tx, s, next)
	return err
}

// loadTx is Load's body, reading through an already-open shared transaction
// instead of taking Storage's own read path, so a Mutate section's load
// happens inside the same held section as its save.
func loadTx(tx *storage.SharedTx, s *storage.Storage) ([]models.Account, error) {
	data, err := tx.ReadFile(Path(s))
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Account{}, nil
		}
		return nil, err
	}
	return decode(data)
}

// saveTx is SaveWithWarnings' body, writing through an already-open shared
// transaction instead of Storage's own write path. Warnings are logged the
// same way Save logs them, so a Mutate caller sees the same observable
// warning behaviour a direct Save would have produced.
func saveTx(tx *storage.SharedTx, s *storage.Storage, accts []models.Account) ([]string, error) {
	if err := Validate(accts); err != nil {
		return nil, err
	}
	warnings := OverlapWarnings(accts, existingCSVBasenames(s))
	for _, w := range warnings {
		log.Printf("Warning: %s", w)
	}

	if accts == nil {
		accts = []models.Account{}
	}
	data, err := json.MarshalIndent(accountsDoc{Accounts: accts}, "", "  ")
	if err != nil {
		return warnings, err
	}
	if err := tx.WriteFile(Path(s), data, 0644); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// decode parses the sidecar's bytes, accepting either the canonical
// {"accounts": [...]} object or a bare [...] array.
func decode(data []byte) ([]models.Account, error) {
	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	if trimmed == "" {
		return []models.Account{}, nil
	}
	if trimmed[0] == '[' {
		var list []models.Account
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("invalid accounts file: %w", err)
		}
		if list == nil {
			list = []models.Account{}
		}
		return list, nil
	}
	var doc accountsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid accounts file: %w", err)
	}
	if doc.Accounts == nil {
		doc.Accounts = []models.Account{}
	}
	return doc.Accounts, nil
}

// Save validates and writes accounts.json back through the storage service.
//
// Duplicate IDs and empty names are errors -- both break the account's
// contract as a stable identity for transactions and decisions. Overlapping
// FilePatterns are NOT: two accounts whose patterns both match a file that
// currently exists is a warning, since first match still wins
// deterministically by ID order. Save logs those warnings; a caller that
// wants to show them to the user calls SaveWithWarnings instead.
func Save(s *storage.Storage, accts []models.Account) error {
	warnings, err := SaveWithWarnings(s, accts)
	for _, w := range warnings {
		log.Printf("Warning: %s", w)
	}
	return err
}

// SaveWithWarnings is Save with the pattern-overlap warnings returned to the
// caller rather than only logged. Warnings are returned on success; the save
// still happens.
func SaveWithWarnings(s *storage.Storage, accts []models.Account) ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("accounts: nil storage")
	}
	if err := Validate(accts); err != nil {
		return nil, err
	}
	warnings := OverlapWarnings(accts, existingCSVBasenames(s))

	if accts == nil {
		accts = []models.Account{}
	}
	data, err := json.MarshalIndent(accountsDoc{Accounts: accts}, "", "  ")
	if err != nil {
		return warnings, err
	}
	if err := s.WriteFile(Path(s), data, 0644); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// Validate reports the conditions that make an account set unusable:
// a duplicate or empty ID, or an empty Name.
func Validate(accts []models.Account) error {
	seen := make(map[string]bool, len(accts))
	for _, a := range accts {
		id := strings.TrimSpace(a.ID)
		if id == "" {
			return fmt.Errorf("account %q has an empty ID", a.Name)
		}
		if seen[id] {
			return fmt.Errorf("duplicate account ID %q", id)
		}
		seen[id] = true
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("account %q has an empty name", id)
		}
	}
	return nil
}

// OverlapWarnings returns one warning per basename that more than one
// account's FilePatterns match. First match still wins by ID order, so this
// is advisory: it tells the user their patterns are ambiguous, it does not
// make the load non-deterministic.
func OverlapWarnings(accts []models.Account, basenames []string) []string {
	if len(accts) < 2 || len(basenames) == 0 {
		return nil
	}
	ordered := sortedByID(accts)
	var warnings []string
	for _, name := range basenames {
		var matched []string
		for _, a := range ordered {
			if accountMatches(a, name) {
				matched = append(matched, a.ID)
			}
		}
		if len(matched) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"file %q matches multiple accounts (%s); %q wins by ID order",
				name, strings.Join(matched, ", "), matched[0]))
		}
	}
	return warnings
}

// MatchFile returns the ID of the first account whose FilePatterns match
// basename, with accounts considered in ascending ID order so the result is
// deterministic regardless of the input slice's order. It returns "" when
// nothing matches -- an unassigned file, which still loads.
func MatchFile(accts []models.Account, basename string) string {
	if len(accts) == 0 || strings.TrimSpace(basename) == "" {
		return ""
	}
	for _, a := range sortedByID(accts) {
		if accountMatches(a, basename) {
			return a.ID
		}
	}
	return ""
}

// Find returns the account with the given ID, or nil. The returned pointer
// addresses a copy, so callers cannot mutate the caller's slice through it.
func Find(accts []models.Account, id string) *models.Account {
	if id == "" {
		return nil
	}
	for i := range accts {
		if accts[i].ID == id {
			a := accts[i]
			return &a
		}
	}
	return nil
}

// accountMatches reports whether any of the account's patterns match the
// basename: case-insensitive path.Match glob first, plain substring as a
// fallback so a pattern like "usaa" matches "usaa-checking-2026.csv"
// without the user having to write globs.
func accountMatches(a models.Account, basename string) bool {
	name := strings.ToLower(strings.TrimSpace(basename))
	if name == "" {
		return false
	}
	for _, p := range a.FilePatterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		// path.Match's only error is ErrBadPattern; a malformed pattern
		// falls through to the substring test rather than failing the load.
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

// sortedByID returns a copy of accts in ascending ID order, leaving the
// caller's slice untouched.
func sortedByID(accts []models.Account) []models.Account {
	out := make([]models.Account, len(accts))
	copy(out, accts)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// existingCSVBasenames lists the CSV files currently in the data directory,
// which is the set OverlapWarnings is checked against ("patterns that both
// match a file that currently exists"). A glob failure yields no warnings
// rather than blocking the save.
func existingCSVBasenames(s *storage.Storage) []string {
	files, err := filepath.Glob(filepath.Join(s.BaseDir(), "*.csv"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	sort.Strings(out)
	return out
}
