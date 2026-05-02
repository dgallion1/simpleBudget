# Whatif "Recently Removed" Purge — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-item permanent-delete (purge) action to the "Recently Removed" UI sections for income sources, expense sources, and big-ticket items on the Whatif page.

**Architecture:** Three new service methods (`PurgeRemovedIncomeSource`, `PurgeRemovedExpenseSource`, `PurgeRemovedBigTicketItem`) modeled on the existing `Restore*` methods. Three new `DELETE /whatif/<resource>/{id}/purge` HTTP routes. Three new HTTP handlers identical in shape to `handleWhatIfRestoreIncome`. Three template updates that add a red trash X button next to the existing green restore arrow with `hx-confirm` for native browser confirmation.

**Tech Stack:** Go 1.x, chi router, HTMX, Tailwind CSS templates.

**Spec:** `docs/superpowers/specs/2026-05-02-whatif-recently-removed-purge-design.md`

---

## File Structure

| File | Change | Owner |
|------|--------|-------|
| `internal/services/retirement/settings.go` | Add 3 `Purge*` methods | Task 1, 2, 3 |
| `internal/services/retirement/settings_crud_test.go` | Add tests for `Purge*` | Task 1, 2, 3 |
| `internal/handlers/whatif/handlers.go` | Add 3 routes | Task 4 |
| `internal/handlers/whatif/handlers_income_expense.go` | Add 3 handlers | Task 4 |
| `internal/handlers/whatif/handlers_test.go` | Add handler tests | Task 4 |
| `web/templates/components/whatif/income-sources-list.html` | Add purge button | Task 5 |
| `web/templates/components/whatif/expense-sources-list.html` | Add purge button | Task 5 |
| `web/templates/components/whatif/bigticket-card.html` | Add purge button | Task 5 |

---

### Task 1: Service — `PurgeRemovedIncomeSource`

**Files:**
- Modify: `internal/services/retirement/settings.go` (insert after `RestoreIncomeSource`, around line 617)
- Test: `internal/services/retirement/settings_crud_test.go` (append at end)

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/settings_crud_test.go`:

```go
func TestPurgeRemovedIncomeSource_HappyPath(t *testing.T) {
	sm := newTestSM(t)
	src := models.IncomeSource{ID: "p-1", Name: "Old Pension", Amount: 100, Type: models.IncomeFixed}
	if _, err := sm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	if _, err := sm.RemoveIncomeSource("p-1"); err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}

	settings, err := sm.PurgeRemovedIncomeSource("p-1")
	if err != nil {
		t.Fatalf("PurgeRemovedIncomeSource: %v", err)
	}
	if len(settings.RemovedIncomeSources) != 0 {
		t.Fatalf("expected RemovedIncomeSources empty, got %+v", settings.RemovedIncomeSources)
	}
	if len(settings.IncomeSources) != 0 {
		t.Fatalf("active list should remain empty, got %+v", settings.IncomeSources)
	}

	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.RemovedIncomeSources) != 0 {
		t.Fatalf("purge not persisted: %+v", reloaded.RemovedIncomeSources)
	}
}

func TestPurgeRemovedIncomeSource_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.PurgeRemovedIncomeSource("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}

func TestPurgeRemovedIncomeSource_ActiveOnlyIDNotFound(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.AddIncomeSource(models.IncomeSource{ID: "active-only", Name: "Wage", Amount: 5000}); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}

	_, err := sm.PurgeRemovedIncomeSource("active-only")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.IncomeSources) != 1 || s.IncomeSources[0].ID != "active-only" {
		t.Errorf("active list mutated: %+v", s.IncomeSources)
	}
}

func TestPurgeRemovedIncomeSource_PreservesOtherEntries(t *testing.T) {
	sm := newTestSM(t)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := sm.AddIncomeSource(models.IncomeSource{ID: id, Name: id, Amount: 100}); err != nil {
			t.Fatalf("AddIncomeSource %s: %v", id, err)
		}
		if _, err := sm.RemoveIncomeSource(id); err != nil {
			t.Fatalf("RemoveIncomeSource %s: %v", id, err)
		}
	}

	settings, err := sm.PurgeRemovedIncomeSource("b")
	if err != nil {
		t.Fatalf("PurgeRemovedIncomeSource: %v", err)
	}
	if len(settings.RemovedIncomeSources) != 2 {
		t.Fatalf("expected 2 remaining, got %d: %+v", len(settings.RemovedIncomeSources), settings.RemovedIncomeSources)
	}
	if settings.RemovedIncomeSources[0].ID != "a" || settings.RemovedIncomeSources[1].ID != "c" {
		t.Errorf("order not preserved: %+v", settings.RemovedIncomeSources)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedIncomeSource -v`
Expected: FAIL — "PurgeRemovedIncomeSource undefined"

- [ ] **Step 3: Implement `PurgeRemovedIncomeSource`**

Insert after the `RestoreIncomeSource` function in `internal/services/retirement/settings.go` (after the closing `}` near line 617, before `// UpdateIncomeSource`):

```go
// PurgeRemovedIncomeSource permanently removes an income source from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedIncomeSources. Does not touch the active IncomeSources list.
func (sm *SettingsManager) PurgeRemovedIncomeSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.IncomeSource, 0, len(settings.RemovedIncomeSources))
	purged := false
	for _, source := range settings.RemovedIncomeSources {
		if source.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, source)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed income source %s not found", id)}
	}
	settings.RemovedIncomeSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedIncomeSource -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat(whatif): add PurgeRemovedIncomeSource service method"
```

---

### Task 2: Service — `PurgeRemovedExpenseSource`

**Files:**
- Modify: `internal/services/retirement/settings.go` (insert after `RestoreExpenseSource`)
- Test: `internal/services/retirement/settings_crud_test.go` (append at end)

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/settings_crud_test.go`:

```go
func TestPurgeRemovedExpenseSource_HappyPath(t *testing.T) {
	sm := newTestSM(t)
	src := models.ExpenseSource{ID: "px-1", Name: "Old Rent", Amount: 2000, StartYear: 0}
	if _, err := sm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	if _, err := sm.RemoveExpenseSource("px-1"); err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}

	settings, err := sm.PurgeRemovedExpenseSource("px-1")
	if err != nil {
		t.Fatalf("PurgeRemovedExpenseSource: %v", err)
	}
	if len(settings.RemovedExpenseSources) != 0 {
		t.Fatalf("expected RemovedExpenseSources empty, got %+v", settings.RemovedExpenseSources)
	}
	if len(settings.ExpenseSources) != 0 {
		t.Fatalf("active list should remain empty, got %+v", settings.ExpenseSources)
	}

	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.RemovedExpenseSources) != 0 {
		t.Fatalf("purge not persisted: %+v", reloaded.RemovedExpenseSources)
	}
}

func TestPurgeRemovedExpenseSource_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.PurgeRemovedExpenseSource("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}

func TestPurgeRemovedExpenseSource_ActiveOnlyIDNotFound(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.AddExpenseSource(models.ExpenseSource{ID: "active-only", Name: "Rent", Amount: 1000, StartYear: 0}); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}

	_, err := sm.PurgeRemovedExpenseSource("active-only")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.ExpenseSources) != 1 || s.ExpenseSources[0].ID != "active-only" {
		t.Errorf("active list mutated: %+v", s.ExpenseSources)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedExpenseSource -v`
Expected: FAIL — "PurgeRemovedExpenseSource undefined"

- [ ] **Step 3: Implement `PurgeRemovedExpenseSource`**

Insert after the `RestoreExpenseSource` function in `internal/services/retirement/settings.go`:

```go
// PurgeRemovedExpenseSource permanently removes an expense source from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedExpenseSources. Does not touch the active ExpenseSources list.
func (sm *SettingsManager) PurgeRemovedExpenseSource(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.ExpenseSource, 0, len(settings.RemovedExpenseSources))
	purged := false
	for _, source := range settings.RemovedExpenseSources {
		if source.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, source)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed expense source %s not found", id)}
	}
	settings.RemovedExpenseSources = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedExpenseSource -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat(whatif): add PurgeRemovedExpenseSource service method"
```

---

### Task 3: Service — `PurgeRemovedBigTicketItem`

**Files:**
- Modify: `internal/services/retirement/settings.go` (insert after `RestoreBigTicketItem`)
- Test: `internal/services/retirement/settings_crud_test.go` (append at end)

- [ ] **Step 1: Write the failing tests**

Append to `internal/services/retirement/settings_crud_test.go`:

```go
func TestPurgeRemovedBigTicketItem_HappyPath(t *testing.T) {
	sm := newTestSM(t)
	item := models.BigTicketItem{ID: "bt-1", Name: "Old Boat", Amount: 50000, Year: 2030, Type: "expense"}
	if _, err := sm.AddBigTicketItem(item); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	if _, err := sm.RemoveBigTicketItem("bt-1"); err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}

	settings, err := sm.PurgeRemovedBigTicketItem("bt-1")
	if err != nil {
		t.Fatalf("PurgeRemovedBigTicketItem: %v", err)
	}
	if len(settings.RemovedBigTicketItems) != 0 {
		t.Fatalf("expected RemovedBigTicketItems empty, got %+v", settings.RemovedBigTicketItems)
	}
	if len(settings.BigTicketItems) != 0 {
		t.Fatalf("active list should remain empty, got %+v", settings.BigTicketItems)
	}

	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reloaded.RemovedBigTicketItems) != 0 {
		t.Fatalf("purge not persisted: %+v", reloaded.RemovedBigTicketItems)
	}
}

func TestPurgeRemovedBigTicketItem_NotFound(t *testing.T) {
	sm := newTestSM(t)
	_, err := sm.PurgeRemovedBigTicketItem("ghost")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}
}

func TestPurgeRemovedBigTicketItem_ActiveOnlyIDNotFound(t *testing.T) {
	sm := newTestSM(t)
	if _, err := sm.AddBigTicketItem(models.BigTicketItem{ID: "active-only", Name: "Boat", Amount: 5000, Year: 2030, Type: "expense"}); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}

	_, err := sm.PurgeRemovedBigTicketItem("active-only")
	if err == nil {
		t.Fatal("expected ScenarioNotFoundError, got nil")
	}
	var notFoundErr *ScenarioNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected *ScenarioNotFoundError, got %T: %v", err, err)
	}

	s, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.BigTicketItems) != 1 || s.BigTicketItems[0].ID != "active-only" {
		t.Errorf("active list mutated: %+v", s.BigTicketItems)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedBigTicketItem -v`
Expected: FAIL — "PurgeRemovedBigTicketItem undefined"

- [ ] **Step 3: Implement `PurgeRemovedBigTicketItem`**

Insert after the `RestoreBigTicketItem` function in `internal/services/retirement/settings.go` (around line 1175, before `// slugify`):

```go
// PurgeRemovedBigTicketItem permanently removes a big ticket item from the
// removed list. Returns ScenarioNotFoundError if the ID is not in
// RemovedBigTicketItems. Does not touch the active BigTicketItems list.
func (sm *SettingsManager) PurgeRemovedBigTicketItem(id string) (*models.WhatIfSettings, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	settings, err := sm.loadInternal()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.BigTicketItem, 0, len(settings.RemovedBigTicketItems))
	purged := false
	for _, item := range settings.RemovedBigTicketItems {
		if item.ID == id {
			purged = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !purged {
		return nil, &ScenarioNotFoundError{Err: fmt.Errorf("removed big ticket item %s not found", id)}
	}
	settings.RemovedBigTicketItems = filtered

	if err := sm.saveInternal(settings); err != nil {
		return nil, err
	}

	return settings, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestPurgeRemovedBigTicketItem -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_crud_test.go
git commit -m "feat(whatif): add PurgeRemovedBigTicketItem service method"
```

---

### Task 4: HTTP Handlers + Routes + Tests

**Files:**
- Modify: `internal/handlers/whatif/handlers_income_expense.go` (append 3 handlers)
- Modify: `internal/handlers/whatif/handlers.go` (add 3 routes in `RegisterRoutes`, lines ~503/508/522)
- Test: `internal/handlers/whatif/handlers_test.go` (append tests)

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/handlers/whatif/handlers_test.go`:

```go
func TestHandleWhatIfPurgeIncome(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.IncomeSource{ID: "purge-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	if _, err := rm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	if _, err := rm.RemoveIncomeSource("purge-inc-1"); err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/purge-inc-1/purge", nil, map[string]string{"id": "purge-inc-1"})
	handleWhatIfPurgeIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedIncomeSources) != 0 {
		t.Errorf("expected RemovedIncomeSources empty, got %+v", settings.RemovedIncomeSources)
	}
}

func TestHandleWhatIfPurgeIncome_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeIncome(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfPurgeExpense(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.ExpenseSource{ID: "purge-exp-1", Name: "Test", Amount: 500, StartYear: 0}
	if _, err := rm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	if _, err := rm.RemoveExpenseSource("purge-exp-1"); err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/purge-exp-1/purge", nil, map[string]string{"id": "purge-exp-1"})
	handleWhatIfPurgeExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedExpenseSources) != 0 {
		t.Errorf("expected RemovedExpenseSources empty, got %+v", settings.RemovedExpenseSources)
	}
}

func TestHandleWhatIfPurgeExpense_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeExpense(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfPurgeBigTicket(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "purge-bt-1", Name: "Test", Amount: 10000, Year: 2030, Type: "expense"}
	if _, err := rm.AddBigTicketItem(item); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	if _, err := rm.RemoveBigTicketItem("purge-bt-1"); err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/purge-bt-1/purge", nil, map[string]string{"id": "purge-bt-1"})
	handleWhatIfPurgeBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedBigTicketItems) != 0 {
		t.Errorf("expected RemovedBigTicketItems empty, got %+v", settings.RemovedBigTicketItems)
	}
}

func TestHandleWhatIfPurgeBigTicket_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeBigTicket(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed item, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail (compile error)**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfPurge -v`
Expected: FAIL — "undefined: handleWhatIfPurgeIncome" (etc.)

- [ ] **Step 3: Add the handlers**

Append to `internal/handlers/whatif/handlers_income_expense.go`:

```go
func handleWhatIfPurgeIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to purge income source: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfPurgeExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedExpenseSource(id)
	if err != nil {
		renderError(w, "Failed to purge expense source: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfPurgeBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.PurgeRemovedBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to purge big ticket item: "+err.Error(), statusForScenarioOperationError(err))
		return
	}

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}
```

- [ ] **Step 4: Register the routes**

In `internal/handlers/whatif/handlers.go` inside `RegisterRoutes`, add three lines.

After the existing line `r.Post("/whatif/income/{id}/restore", handleWhatIfRestoreIncome)` add:
```go
r.Delete("/whatif/income/{id}/purge", handleWhatIfPurgeIncome)
```

After the existing line `r.Post("/whatif/expense/{id}/restore", handleWhatIfRestoreExpense)` add:
```go
r.Delete("/whatif/expense/{id}/purge", handleWhatIfPurgeExpense)
```

After the existing line `r.Post("/whatif/bigticket/{id}/restore", handleWhatIfRestoreBigTicket)` add:
```go
r.Delete("/whatif/bigticket/{id}/purge", handleWhatIfPurgeBigTicket)
```

- [ ] **Step 5: Run handler tests to verify they pass**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfPurge -v`
Expected: PASS (6 tests)

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: All packages PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/whatif/handlers.go internal/handlers/whatif/handlers_income_expense.go internal/handlers/whatif/handlers_test.go
git commit -m "feat(whatif): add purge HTTP handlers and routes for Recently Removed"
```

---

### Task 5: Templates — Add Purge Buttons

**Files:**
- Modify: `web/templates/components/whatif/income-sources-list.html` (block `whatif-removed-income-sources`)
- Modify: `web/templates/components/whatif/expense-sources-list.html` (block `whatif-removed-expense-sources`)
- Modify: `web/templates/components/whatif/bigticket-card.html` (block `whatif-removed-bigticket`)

- [ ] **Step 1: Update income removed-list template**

In `web/templates/components/whatif/income-sources-list.html`, locate the existing restore button inside `whatif-removed-income-sources` (currently a single `<button hx-post=".../restore">`). Replace just the restore `<button>...</button>` element with a wrapping div that contains both restore and purge buttons.

Find this block:
```html
        <button hx-post="/whatif/income/{{.ID}}/restore" hx-target="#whatif-results"
            class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore income source {{.Name}}">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
            </svg>
        </button>
```

Replace with:
```html
        <div class="flex items-center gap-1">
            <button hx-post="/whatif/income/{{.ID}}/restore" hx-target="#whatif-results"
                class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore income source {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
                </svg>
            </button>
            <button hx-delete="/whatif/income/{{.ID}}/purge" hx-target="#whatif-results"
                hx-confirm="Permanently delete {{.Name}}?"
                class="text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300" title="Permanently delete" aria-label="Permanently delete income source {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
```

- [ ] **Step 2: Update expense removed-list template**

In `web/templates/components/whatif/expense-sources-list.html`, perform the analogous replacement inside `whatif-removed-expense-sources` (the URLs become `/whatif/expense/...` and the aria-label says "expense source"):

Find:
```html
        <button hx-post="/whatif/expense/{{.ID}}/restore" hx-target="#whatif-results"
            class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore expense source {{.Name}}">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
            </svg>
        </button>
```

Replace with:
```html
        <div class="flex items-center gap-1">
            <button hx-post="/whatif/expense/{{.ID}}/restore" hx-target="#whatif-results"
                class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore expense source {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
                </svg>
            </button>
            <button hx-delete="/whatif/expense/{{.ID}}/purge" hx-target="#whatif-results"
                hx-confirm="Permanently delete {{.Name}}?"
                class="text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300" title="Permanently delete" aria-label="Permanently delete expense source {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
```

- [ ] **Step 3: Update big-ticket removed-list template**

In `web/templates/components/whatif/bigticket-card.html`, perform the analogous replacement inside `whatif-removed-bigticket`:

Find:
```html
        <button hx-post="/whatif/bigticket/{{.ID}}/restore" hx-target="#whatif-results"
            class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore big ticket {{.Name}}">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
            </svg>
        </button>
```

Replace with:
```html
        <div class="flex items-center gap-1">
            <button hx-post="/whatif/bigticket/{{.ID}}/restore" hx-target="#whatif-results"
                class="text-green-500 hover:text-green-700 dark:text-green-400 dark:hover:text-green-300" title="Restore" aria-label="Restore big ticket {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                        d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>
                </svg>
            </button>
            <button hx-delete="/whatif/bigticket/{{.ID}}/purge" hx-target="#whatif-results"
                hx-confirm="Permanently delete {{.Name}}?"
                class="text-red-500 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300" title="Permanently delete" aria-label="Permanently delete big ticket {{.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
```

- [ ] **Step 4: Run the full test suite once more**

Run: `go test ./...`
Expected: PASS — templates aren't directly tested, but a full build verifies templates parse via the renderer's startup.

- [ ] **Step 5: Commit**

```bash
git add web/templates/components/whatif/income-sources-list.html web/templates/components/whatif/expense-sources-list.html web/templates/components/whatif/bigticket-card.html
git commit -m "feat(whatif): add purge button to Recently Removed lists in UI"
```

---

### Task 6: Manual UI Verification

- [ ] **Step 1: Start dev server**

Run: `make run` (or whatever the project's standard dev command is — check `Makefile`).

- [ ] **Step 2: Open the Whatif page in a browser**

Navigate to the Whatif page. Confirm at least one item exists in each "Recently Removed" section (or remove an active item to populate one).

- [ ] **Step 3: Test happy path**

For each of income, expense, and big-ticket sections:
1. Click the red trash X next to a removed item.
2. Confirm the native browser dialog appears with the item name.
3. Click OK.
4. Verify the item disappears from the Recently Removed section.
5. Verify the section hides itself when empty.

- [ ] **Step 4: Test cancel path**

1. Click the red trash X.
2. Click Cancel in the dialog.
3. Verify nothing changes.

- [ ] **Step 5: Verify dark mode styling**

Toggle dark mode (if applicable). Confirm the new red trash X has appropriate dark-mode hover/text colors and is visually distinguishable from the green restore arrow.

- [ ] **Step 6: Stop dev server, no commit needed unless adjustments were made.**

---

## Self-Review Notes

- Spec coverage: every spec section maps to a task — service methods (Tasks 1-3), routes/handlers/handler tests (Task 4), templates (Task 5), manual verification (Task 6).
- Placeholder scan: clean — every code block is literal.
- Type consistency: `PurgeRemovedIncomeSource`, `PurgeRemovedExpenseSource`, `PurgeRemovedBigTicketItem` used identically across service, handler, and tests. Routes are `/whatif/<resource>/{id}/purge` everywhere. URL form `hx-delete=...` matches handler `r.Delete(...)`.
