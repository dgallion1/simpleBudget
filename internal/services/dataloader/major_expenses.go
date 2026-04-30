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
		return err
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
