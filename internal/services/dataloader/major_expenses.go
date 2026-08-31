package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/storage"
)

const majorExpensesFile = "major_expenses.json"

// testHookAfterExpenseLoad, when non-nil, is called by AddMajorExpense after
// it has loaded the active list and while its sequence is still open. It exists
// so a test can prove that a concurrent ArchiveMajorExpense really blocks for
// the whole load->modify->save sequence rather than merely usually losing the
// race. Nil in production; the only writer is a test in this package.
var testHookAfterExpenseLoad func()

// testHookMidArchive, when non-nil, is called by ArchiveMajorExpense after it
// has written the archive file but BEFORE it writes the shortened active list
// — the exact window in which a concurrent writer used to be able to save a
// stale active list and leave one expense in both files. It exists so a test
// can prove the whole three-file sequence is one critical section rather than
// merely usually winning the race. Nil in production; the only writer is a
// test in this package.
var testHookMidArchive func()

// majorExpensesPath returns the path to the major_expenses.json file
func (dl *DataLoader) majorExpensesPath() string {
	return filepath.Join(dl.CSVDirectory, majorExpensesFile)
}

// LoadMajorExpenses reads the user-declared major expenses from disk.
// Returns an empty slice if the file does not exist.
func (dl *DataLoader) LoadMajorExpenses() ([]models.MajorExpense, error) {
	tx, done := dl.beginWrite()
	defer done()
	return dl.loadMajorExpensesLocked(tx)
}

// loadMajorExpensesLocked is LoadMajorExpenses' body. Caller holds the
// sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) loadMajorExpensesLocked(tx *storage.SharedTx) ([]models.MajorExpense, error) {
	path := dl.majorExpensesPath()
	data, err := tx.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var store models.MajorExpenseStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("invalid major_expenses file: %w", err)
	}
	return store.Expenses, nil
}

// SaveMajorExpenses persists the entire list to disk.
func (dl *DataLoader) SaveMajorExpenses(list []models.MajorExpense) error {
	tx, done := dl.beginWrite()
	defer done()
	return dl.saveMajorExpensesLocked(tx, list)
}

// saveMajorExpensesLocked is SaveMajorExpenses' body. Caller holds the
// sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) saveMajorExpensesLocked(tx *storage.SharedTx, list []models.MajorExpense) error {
	if list == nil {
		list = []models.MajorExpense{}
	}
	store := models.MajorExpenseStore{Expenses: list}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal major expenses: %w", err)
	}
	return tx.WriteFile(dl.majorExpensesPath(), data, 0644)
}

// AddMajorExpense appends a new entry, stamping CreatedAt/UpdatedAt, and
// returns the resulting slice.
func (dl *DataLoader) AddMajorExpense(me models.MajorExpense) ([]models.MajorExpense, error) {
	tx, done := dl.beginWrite()
	defer done()
	list, err := dl.loadMajorExpensesLocked(tx)
	if err != nil {
		return nil, err
	}
	if testHookAfterExpenseLoad != nil {
		testHookAfterExpenseLoad()
	}
	now := time.Now().UTC()
	me.CreatedAt = now
	me.UpdatedAt = now
	list = append(list, me)
	if err := dl.saveMajorExpensesLocked(tx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateMajorExpense replaces the entry with matching ID. Fields copied
// from updates: Name, Keywords, ExpectedMin, ExpectedMax, Notes,
// IsInternalTransfer, ExcludeFromPlanSync. ID and CreatedAt are preserved
// from the existing entry; UpdatedAt is bumped.
func (dl *DataLoader) UpdateMajorExpense(id string, updates models.MajorExpense) ([]models.MajorExpense, error) {
	tx, done := dl.beginWrite()
	defer done()
	list, err := dl.loadMajorExpensesLocked(tx)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range list {
		if list[i].ID == id {
			list[i].Name = updates.Name
			list[i].Keywords = updates.Keywords
			list[i].ExpectedMin = updates.ExpectedMin
			list[i].ExpectedMax = updates.ExpectedMax
			list[i].Notes = updates.Notes
			list[i].IsInternalTransfer = updates.IsInternalTransfer
			list[i].ExcludeFromPlanSync = updates.ExcludeFromPlanSync
			list[i].UpdatedAt = time.Now().UTC()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("major expense not found: %s", id)
	}
	if err := dl.saveMajorExpensesLocked(tx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteMajorExpense removes the entry with matching ID.
//
// Deprecated: use ArchiveMajorExpense, which preserves the definition
// and pin attachments in deleted_major_expenses.json so they can be
// restored. DeleteMajorExpense is retained only for tests of pre-archive
// behavior and may be removed in a future cleanup pass.
func (dl *DataLoader) DeleteMajorExpense(id string) ([]models.MajorExpense, error) {
	tx, done := dl.beginWrite()
	defer done()
	list, err := dl.loadMajorExpensesLocked(tx)
	if err != nil {
		return nil, err
	}
	out := make([]models.MajorExpense, 0, len(list))
	for _, me := range list {
		if me.ID != id {
			out = append(out, me)
		}
	}
	if err := dl.saveMajorExpensesLocked(tx, out); err != nil {
		return nil, err
	}
	return out, nil
}

const deletedMajorExpensesFile = "deleted_major_expenses.json"

// deletedMajorExpensesPath returns the path to the
// deleted_major_expenses.json archive file.
func (dl *DataLoader) deletedMajorExpensesPath() string {
	return filepath.Join(dl.CSVDirectory, deletedMajorExpensesFile)
}

// LoadDeletedMajorExpenses reads the archive of soft-deleted major
// expenses. Returns an empty slice if the file does not exist.
func (dl *DataLoader) LoadDeletedMajorExpenses() ([]models.DeletedMajorExpense, error) {
	tx, done := dl.beginWrite()
	defer done()
	return dl.loadDeletedMajorExpensesLocked(tx)
}

// loadDeletedMajorExpensesLocked is LoadDeletedMajorExpenses' body. Caller
// holds the sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) loadDeletedMajorExpensesLocked(tx *storage.SharedTx) ([]models.DeletedMajorExpense, error) {
	path := dl.deletedMajorExpensesPath()
	data, err := tx.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var store models.DeletedMajorExpenseStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("invalid deleted_major_expenses file: %w", err)
	}
	return store.Deleted, nil
}

// SaveDeletedMajorExpenses persists the entire archive list to disk.
func (dl *DataLoader) SaveDeletedMajorExpenses(list []models.DeletedMajorExpense) error {
	tx, done := dl.beginWrite()
	defer done()
	return dl.saveDeletedMajorExpensesLocked(tx, list)
}

// saveDeletedMajorExpensesLocked is SaveDeletedMajorExpenses' body. Caller
// holds the sequence opened by beginWrite and passes its transaction.
func (dl *DataLoader) saveDeletedMajorExpensesLocked(tx *storage.SharedTx, list []models.DeletedMajorExpense) error {
	if list == nil {
		list = []models.DeletedMajorExpense{}
	}
	store := models.DeletedMajorExpenseStore{Deleted: list}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return tx.WriteFile(dl.deletedMajorExpensesPath(), data, 0644)
}

// ArchiveMajorExpense soft-deletes the active expense with matching ID:
// it moves the full definition into deleted_major_expenses.json along
// with a snapshot of every transaction hash that was pinned to it, then
// removes the expense from the active list and removes those pins from
// transaction_pins.json.
//
// Write order is archive → active list → pins, so a crash mid-operation
// leaves the user with a recoverable duplicate (an entry in both lists)
// rather than data loss. A concurrent writer can no longer produce that
// duplicate: the whole sequence is one critical section, held against both
// concurrent writers and a restore.
// RestoreMajorExpense reverses this.
//
// Returns os-style "not found" sentinel error if no active expense has
// the given ID.
func (dl *DataLoader) ArchiveMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	tx, done := dl.beginWrite()
	defer done()

	active, err := dl.loadMajorExpensesLocked(tx)
	if err != nil {
		return err
	}
	var (
		target    models.MajorExpense
		targetIdx = -1
	)
	for i, me := range active {
		if me.ID == id {
			target = me
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return fmt.Errorf("major expense not found: %s", id)
	}

	pins, err := dl.loadTransactionPinsLocked(tx)
	if err != nil {
		return err
	}
	pinnedHashes := make([]string, 0)
	for hash, eid := range pins {
		if eid == id {
			pinnedHashes = append(pinnedHashes, hash)
		}
	}

	deleted, err := dl.loadDeletedMajorExpensesLocked(tx)
	if err != nil {
		return err
	}
	deleted = append(deleted, models.DeletedMajorExpense{
		Expense:      target,
		DeletedAt:    time.Now().UTC(),
		PinnedHashes: pinnedHashes,
	})
	if err := dl.saveDeletedMajorExpensesLocked(tx, deleted); err != nil {
		return err
	}

	if testHookMidArchive != nil {
		testHookMidArchive()
	}

	out := make([]models.MajorExpense, 0, len(active)-1)
	out = append(out, active[:targetIdx]...)
	out = append(out, active[targetIdx+1:]...)
	if err := dl.saveMajorExpensesLocked(tx, out); err != nil {
		return err
	}

	if len(pinnedHashes) > 0 {
		for _, h := range pinnedHashes {
			delete(pins, h)
		}
		if err := dl.writePinsLocked(tx, pins); err != nil {
			return err
		}
	}
	return nil
}

// RestoreMajorExpense reverses ArchiveMajorExpense: it moves the entry
// out of deleted_major_expenses.json back into major_expenses.json and
// re-pins each captured hash.
//
// Pin restoration is non-destructive: a hash that has been pinned to a
// different expense after the archive event keeps its current pin. Only
// hashes that are currently unpinned are restored.
//
// Returns an error if no archived entry has the given ID, or if an
// active expense with the same ID already exists (which would shadow the
// restore).
func (dl *DataLoader) RestoreMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	tx, done := dl.beginWrite()
	defer done()

	deleted, err := dl.loadDeletedMajorExpensesLocked(tx)
	if err != nil {
		return err
	}
	var (
		entry models.DeletedMajorExpense
		idx   = -1
	)
	for i, d := range deleted {
		if d.Expense.ID == id {
			entry = d
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("deleted major expense not found: %s", id)
	}

	active, err := dl.loadMajorExpensesLocked(tx)
	if err != nil {
		return err
	}
	for _, me := range active {
		if me.ID == id {
			return fmt.Errorf("active major expense with id %s already exists", id)
		}
	}
	active = append(active, entry.Expense)
	if err := dl.saveMajorExpensesLocked(tx, active); err != nil {
		return err
	}

	if len(entry.PinnedHashes) > 0 {
		pins, err := dl.loadTransactionPinsLocked(tx)
		if err != nil {
			return err
		}
		changed := false
		for _, h := range entry.PinnedHashes {
			if _, taken := pins[h]; taken {
				continue
			}
			pins[h] = id
			changed = true
		}
		if changed {
			if err := dl.writePinsLocked(tx, pins); err != nil {
				return err
			}
		}
	}

	out := make([]models.DeletedMajorExpense, 0, len(deleted)-1)
	out = append(out, deleted[:idx]...)
	out = append(out, deleted[idx+1:]...)
	return dl.saveDeletedMajorExpensesLocked(tx, out)
}

// DiscardDeletedMajorExpense permanently removes an archived entry. The
// definition and its captured pin hashes are gone; this cannot be undone.
func (dl *DataLoader) DiscardDeletedMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	tx, done := dl.beginWrite()
	defer done()

	deleted, err := dl.loadDeletedMajorExpensesLocked(tx)
	if err != nil {
		return err
	}
	out := make([]models.DeletedMajorExpense, 0, len(deleted))
	found := false
	for _, d := range deleted {
		if d.Expense.ID == id {
			found = true
			continue
		}
		out = append(out, d)
	}
	if !found {
		return fmt.Errorf("deleted major expense not found: %s", id)
	}
	return dl.saveDeletedMajorExpensesLocked(tx, out)
}
