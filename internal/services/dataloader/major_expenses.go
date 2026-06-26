package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"budget2/internal/models"
)

const majorExpensesFile = "major_expenses.json"

// majorExpensesPath returns the path to the major_expenses.json file
func (dl *DataLoader) majorExpensesPath() string {
	return filepath.Join(dl.CSVDirectory, majorExpensesFile)
}

// LoadMajorExpenses reads the user-declared major expenses from disk.
// Returns an empty slice if the file does not exist.
func (dl *DataLoader) LoadMajorExpenses() ([]models.MajorExpense, error) {
	path := dl.majorExpensesPath()
	data, err := dl.store.ReadFile(path)
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
	if list == nil {
		list = []models.MajorExpense{}
	}
	store := models.MajorExpenseStore{Expenses: list}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal major expenses: %w", err)
	}
	return dl.store.WriteFile(dl.majorExpensesPath(), data, 0644)
}

// AddMajorExpense appends a new entry, stamping CreatedAt/UpdatedAt, and
// returns the resulting slice.
func (dl *DataLoader) AddMajorExpense(me models.MajorExpense) ([]models.MajorExpense, error) {
	list, err := dl.LoadMajorExpenses()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	me.CreatedAt = now
	me.UpdatedAt = now
	list = append(list, me)
	if err := dl.SaveMajorExpenses(list); err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateMajorExpense replaces the entry with matching ID. Fields copied
// from updates: Name, Keywords, ExpectedMin, ExpectedMax, Notes. ID and
// CreatedAt are preserved from the existing entry; UpdatedAt is bumped.
func (dl *DataLoader) UpdateMajorExpense(id string, updates models.MajorExpense) ([]models.MajorExpense, error) {
	list, err := dl.LoadMajorExpenses()
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
			list[i].UpdatedAt = time.Now().UTC()
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("major expense not found: %s", id)
	}
	if err := dl.SaveMajorExpenses(list); err != nil {
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
	list, err := dl.LoadMajorExpenses()
	if err != nil {
		return nil, err
	}
	out := make([]models.MajorExpense, 0, len(list))
	for _, me := range list {
		if me.ID != id {
			out = append(out, me)
		}
	}
	if err := dl.SaveMajorExpenses(out); err != nil {
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
	path := dl.deletedMajorExpensesPath()
	data, err := dl.store.ReadFile(path)
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
	if list == nil {
		list = []models.DeletedMajorExpense{}
	}
	store := models.DeletedMajorExpenseStore{Deleted: list}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.deletedMajorExpensesPath(), data, 0644)
}

// ArchiveMajorExpense soft-deletes the active expense with matching ID:
// it moves the full definition into deleted_major_expenses.json along
// with a snapshot of every transaction hash that was pinned to it, then
// removes the expense from the active list and removes those pins from
// transaction_pins.json.
//
// Write order is archive → active list → pins, so a crash mid-operation
// leaves the user with a recoverable duplicate (an entry in both lists)
// rather than data loss. RestoreMajorExpense reverses this.
//
// Returns os-style "not found" sentinel error if no active expense has
// the given ID.
func (dl *DataLoader) ArchiveMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	active, err := dl.LoadMajorExpenses()
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

	pins, err := dl.LoadTransactionPins()
	if err != nil {
		return err
	}
	pinnedHashes := make([]string, 0)
	for hash, eid := range pins {
		if eid == id {
			pinnedHashes = append(pinnedHashes, hash)
		}
	}

	deleted, err := dl.LoadDeletedMajorExpenses()
	if err != nil {
		return err
	}
	deleted = append(deleted, models.DeletedMajorExpense{
		Expense:      target,
		DeletedAt:    time.Now().UTC(),
		PinnedHashes: pinnedHashes,
	})
	if err := dl.SaveDeletedMajorExpenses(deleted); err != nil {
		return err
	}

	out := make([]models.MajorExpense, 0, len(active)-1)
	out = append(out, active[:targetIdx]...)
	out = append(out, active[targetIdx+1:]...)
	if err := dl.SaveMajorExpenses(out); err != nil {
		return err
	}

	if len(pinnedHashes) > 0 {
		for _, h := range pinnedHashes {
			delete(pins, h)
		}
		data, err := json.MarshalIndent(pins, "", "  ")
		if err != nil {
			return err
		}
		if err := dl.store.WriteFile(dl.transactionPinsPath(), data, 0644); err != nil {
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
	deleted, err := dl.LoadDeletedMajorExpenses()
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

	active, err := dl.LoadMajorExpenses()
	if err != nil {
		return err
	}
	for _, me := range active {
		if me.ID == id {
			return fmt.Errorf("active major expense with id %s already exists", id)
		}
	}
	active = append(active, entry.Expense)
	if err := dl.SaveMajorExpenses(active); err != nil {
		return err
	}

	if len(entry.PinnedHashes) > 0 {
		pins, err := dl.LoadTransactionPins()
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
			data, err := json.MarshalIndent(pins, "", "  ")
			if err != nil {
				return err
			}
			if err := dl.store.WriteFile(dl.transactionPinsPath(), data, 0644); err != nil {
				return err
			}
		}
	}

	out := make([]models.DeletedMajorExpense, 0, len(deleted)-1)
	out = append(out, deleted[:idx]...)
	out = append(out, deleted[idx+1:]...)
	return dl.SaveDeletedMajorExpenses(out)
}

// DiscardDeletedMajorExpense permanently removes an archived entry. The
// definition and its captured pin hashes are gone; this cannot be undone.
func (dl *DataLoader) DiscardDeletedMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	deleted, err := dl.LoadDeletedMajorExpenses()
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
	return dl.SaveDeletedMajorExpenses(out)
}
