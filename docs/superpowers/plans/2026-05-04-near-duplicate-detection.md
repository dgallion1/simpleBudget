# Near-Duplicate Transaction Detection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-04-near-duplicate-detection-design.md`

**Goal:** Detect bill-pay → posted-check duplicate transaction pairs introduced by overlapping bank CSV exports, surface them with badges and a review panel, and let the user soft-suppress one side per pair without losing data.

**Architecture:** Detection runs as a post-dedup pass inside `DataLoader.LoadData`. Decisions persist to `data/duplicate_decisions.json` via `dl.store.WriteFile`. Suppression is applied via a new `(*TransactionSet).Active()` filter at aggregation call sites — the explorer keeps the raw set so the user can see and undo suppressions. Unresolved-count is server-rendered into every full-page payload via a shared helper, so the nav badge and dashboard alert use one source of truth.

**Tech Stack:** Go (`net/http` + `chi`), html/template, Tailwind CSS, JSON file persistence via existing `storage.Storage` (atomic write + optional encryption), Playwright for smoke.

**Open-question resolutions from spec §11:**
1. **Nav badge:** server-rendered via shared page-data helper — fewer moving parts than a hydration endpoint and matches existing `ActiveTab` pattern.
2. **Review UI:** neutral `Keep left` / `Keep right` for v1 — no recommended winner; consistent with "no auto-suppression" stance.
3. **Status header coverage:** use the spec §3 alias list verbatim for v1.

**File map (created or modified):**

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/models/transaction.go` | Modify | Add `Status`, `Suppressed`, `DuplicatePairKey` fields; add `(*TransactionSet).Active()` |
| `internal/services/dataloader/loader.go` | Modify | Add `Status` to `columnMappings`; parse `Status` in `loadCSVFile`; integrate detection into `LoadData`; expose unresolved/resolved pair lists and count |
| `internal/services/dataloader/duplicate_decisions.go` | Create | `DuplicateDecision` type, `LoadDuplicateDecisions`/`SaveDuplicateDecision`/`ClearDuplicateDecision` |
| `internal/services/dataloader/duplicate_decisions_test.go` | Create | Persistence round-trip + validation tests |
| `internal/services/dataloader/near_duplicates.go` | Create | `DuplicatePair` type, `detectNearDuplicatePairs`, `pairKey` helper |
| `internal/services/dataloader/near_duplicates_test.go` | Create | Heuristic positive/negative/triplet/idempotency tests |
| `internal/services/dataloader/loader_test.go` | Modify | End-to-end load test: detection + decision application |
| `internal/services/dataloader/major_expense_names.go` | Modify | Skip `Suppressed` transactions during label stamping |
| `internal/handlers/dashboard/handlers.go` | Modify | Switch aggregation to `data.Active()`; attach unresolved count to page data |
| `internal/handlers/insights/handlers.go` | Modify | Switch aggregation to `data.Active()`; attach unresolved count |
| `internal/handlers/whatif/handlers.go` | Modify | Switch aggregation to `data.Active()`; attach unresolved count |
| `internal/handlers/majorexpenses/handlers.go` | Modify | Switch aggregation to `data.Active()`; attach unresolved count |
| `internal/handlers/explorer/handlers.go` | Modify | Attach unresolved count to page data; explorer keeps raw set |
| `internal/handlers/duplicates/handlers.go` | Create | `/duplicates` GET, `/duplicates/resolve` POST, `/duplicates/undo` POST |
| `internal/handlers/duplicates/handlers_test.go` | Create | Handler-level coverage |
| `internal/templates/page_data.go` | Create | Shared `AttachDuplicateCount(pageData, loader)` helper |
| `cmd/server/main.go` | Modify | Initialize and register `duplicates` package routes |
| `web/templates/layouts/base.html` | Modify | Nav link with count badge, hidden when count is zero |
| `web/templates/components/alerts.html` | Modify | Add unresolved-duplicates alert when count > 0 |
| `web/templates/pages/duplicates.html` | Create | Two-tab review panel (unresolved/suppressed) |
| `web/templates/pages/explorer.html` | Modify | Render `dup?` and `suppressed dup` badges based on `Suppressed` / `DuplicatePairKey` |
| `CHANGELOG.md` | Modify | Add entry under current version |

---

## Task 1: Add Status / Suppressed / DuplicatePairKey fields to `Transaction`

**Files:**
- Modify: `internal/models/transaction.go:22-42`
- Modify: `internal/models/transaction.go` (add `Active` method on `TransactionSet`)
- Test: `internal/models/transaction_test.go` (create or extend)

**Why first:** every later task references these fields. Adding them is mechanical and zero-risk because they default to zero values.

- [ ] **Step 1: Write failing test for `Active()` filter**

Create or extend `internal/models/transaction_test.go` with:

```go
package models

import "testing"

func TestTransactionSet_Active_FiltersSuppressed(t *testing.T) {
	ts := NewTransactionSet([]Transaction{
		{Hash: "a", Amount: -10, Suppressed: false},
		{Hash: "b", Amount: -20, Suppressed: true},
		{Hash: "c", Amount: -30, Suppressed: false},
	})

	got := ts.Active()
	if got.Len() != 2 {
		t.Fatalf("expected 2 active, got %d", got.Len())
	}
	for _, tr := range got.Transactions {
		if tr.Suppressed {
			t.Errorf("Active() returned suppressed: %+v", tr)
		}
	}
}

func TestTransactionSet_Active_NoSuppression_PreservesAll(t *testing.T) {
	ts := NewTransactionSet([]Transaction{
		{Hash: "a", Amount: -10},
		{Hash: "b", Amount: -20},
	})
	got := ts.Active()
	if got.Len() != 2 {
		t.Errorf("expected 2 active, got %d", got.Len())
	}
}

func TestTransactionSet_Active_NilSafe(t *testing.T) {
	var ts *TransactionSet
	got := ts.Active()
	if got == nil || got.Len() != 0 {
		t.Errorf("Active() on nil should return empty set, got %+v", got)
	}
}

func TestTransaction_DuplicateFields_DefaultZero(t *testing.T) {
	tr := Transaction{}
	if tr.Status != "" || tr.Suppressed || tr.DuplicatePairKey != "" {
		t.Errorf("default values incorrect: %+v", tr)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL (compile error: unknown fields/methods)**

Run: `go test ./internal/models/... -run "TransactionSet_Active|DuplicateFields"`
Expected: FAIL with `unknown field Suppressed in Transaction` or similar.

- [ ] **Step 3: Add fields and method**

Edit `internal/models/transaction.go`. After line 33 (the `Hash` field), add:

```go
	// Status is the bank-reported lifecycle marker (e.g. "Posted",
	// "Scheduled Bill Pay"). Optional; populated when the source CSV
	// has a Status column. Used by near-duplicate detection.
	Status string `json:"status,omitempty"`

	// Suppressed is true when the user has resolved a near-duplicate
	// pair and chose to drop this side from totals/aggregations.
	// The transaction stays in the explorer view for audit/undo.
	Suppressed bool `json:"suppressed,omitempty"`

	// DuplicatePairKey is non-empty when this transaction is part of
	// an unresolved near-duplicate candidate pair. Used to render
	// "possible duplicate" badges and link to the review panel.
	DuplicatePairKey string `json:"duplicate_pair_key,omitempty"`
```

After the `Copy()` method (the last function in the file, around line 354), add:

```go
// Active returns a new TransactionSet with Suppressed transactions
// filtered out. Aggregation/reporting call sites should use this to
// avoid double-counting near-duplicate pairs the user has resolved.
// The explorer keeps the raw slice so users can see and undo
// suppressions. Safe on a nil receiver.
func (ts *TransactionSet) Active() *TransactionSet {
	if ts == nil {
		return &TransactionSet{}
	}
	result := &TransactionSet{}
	for _, t := range ts.Transactions {
		if !t.Suppressed {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/models/... -run "TransactionSet_Active|DuplicateFields" -v`
Expected: all four tests PASS.

- [ ] **Step 5: Run the full models package + the loader package to confirm no regression**

Run: `go test ./internal/models/... ./internal/services/dataloader/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/models/transaction.go internal/models/transaction_test.go
git commit -m "$(cat <<'EOF'
feat(models): add Status/Suppressed/DuplicatePairKey + TransactionSet.Active

Foundation for near-duplicate detection. Status is parsed from CSV in a
later task; Suppressed and DuplicatePairKey are derived during load.
Active() returns the non-suppressed view used by aggregation call sites.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Parse `Status` column from CSV rows

**Files:**
- Modify: `internal/services/dataloader/loader.go:32-77` (add `Status` to `columnMappings`)
- Modify: `internal/services/dataloader/loader.go:265-275` (parse `Status` in `loadCSVFile`)
- Test: `internal/services/dataloader/loader_test.go` (extend)

**Why now:** detection needs `Status`; the column already exists in the source data but is dropped on load.

- [ ] **Step 1: Write failing test**

Append to `internal/services/dataloader/loader_test.go`:

```go
func TestLoadCSVFile_PopulatesStatus(t *testing.T) {
	csv := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	_, loader, cleanup := setupTestDir(t, map[string]string{"bank.csv": csv})
	defer cleanup()

	got, err := loader.loadCSVFile(filepath.Join(loader.CSVDirectory, "bank.csv"))
	if err != nil {
		t.Fatalf("loadCSVFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(got))
	}
	if got[0].Status != "Scheduled Bill Pay" {
		t.Errorf("first row Status = %q, want %q", got[0].Status, "Scheduled Bill Pay")
	}
	if got[1].Status != "Posted" {
		t.Errorf("second row Status = %q, want %q", got[1].Status, "Posted")
	}
}
```

If the test file doesn't already import `path/filepath`, add it.

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/services/dataloader/... -run TestLoadCSVFile_PopulatesStatus -v`
Expected: FAIL — `Status` is empty.

- [ ] **Step 3: Add `Status` to columnMappings**

In `internal/services/dataloader/loader.go`, edit the `columnMappings` map. After the `Credit` entry (around line 76), add:

```go
	"Status": {
		"status", "Status", "STATUS",
		"transaction status", "Transaction Status", "TRANSACTION STATUS",
		"state", "State",
	},
```

- [ ] **Step 4: Parse `Status` in `loadCSVFile`**

In `internal/services/dataloader/loader.go`, find the block in `loadCSVFile` that parses Description/Category (around lines 268-275). Immediately after the Category parsing block:

```go
		// Parse Category (optional)
		if idx, ok := colIndex["Category"]; ok && idx < len(record) {
			t.Category = strings.TrimSpace(record[idx])
		}
```

…add:

```go
		// Parse Status (optional). Used by near-duplicate detection
		// to distinguish scheduled bill-pays from posted checks.
		if idx, ok := colIndex["Status"]; ok && idx < len(record) {
			t.Status = strings.TrimSpace(record[idx])
		}
```

- [ ] **Step 5: Run test — expect PASS**

Run: `go test ./internal/services/dataloader/... -run TestLoadCSVFile_PopulatesStatus -v`
Expected: PASS.

- [ ] **Step 6: Run full dataloader test suite**

Run: `go test ./internal/services/dataloader/...`
Expected: PASS — adding a never-required column should not break any other test.

- [ ] **Step 7: Commit**

```bash
git add internal/services/dataloader/loader.go internal/services/dataloader/loader_test.go
git commit -m "$(cat <<'EOF'
feat(dataloader): parse CSV Status column into Transaction.Status

Required by near-duplicate detection (bill-pay vs posted-check
classification). Status is optional; missing column is tolerated.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Persistence layer — `data/duplicate_decisions.json`

**Files:**
- Create: `internal/services/dataloader/duplicate_decisions.go`
- Create: `internal/services/dataloader/duplicate_decisions_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/services/dataloader/duplicate_decisions_test.go`:

```go
package dataloader

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateDecisionsPath(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	want := filepath.Join(loader.CSVDirectory, "duplicate_decisions.json")
	if got := loader.duplicateDecisionsPath(); got != want {
		t.Errorf("duplicateDecisionsPath() = %q, want %q", got, want)
	}
}

func TestLoadDuplicateDecisions_NoFile(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries", len(got))
	}
}

func TestLoadDuplicateDecisions_ValidFile(t *testing.T) {
	doc := duplicateDecisionsDoc{
		Decisions: map[string]DuplicateDecision{
			"key1": {
				KeptHash:       "ha",
				SuppressedHash: "hb",
				Outcome:        DuplicateOutcomeKeptWinner,
				DecidedAt:      time.Date(2026, 5, 4, 10, 30, 0, 0, time.UTC),
			},
		},
	}
	data, _ := json.Marshal(doc)
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": string(data),
	})
	defer cleanup()

	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d, ok := got["key1"]
	if !ok {
		t.Fatalf("missing key1 in %+v", got)
	}
	if d.KeptHash != "ha" || d.SuppressedHash != "hb" || d.Outcome != DuplicateOutcomeKeptWinner {
		t.Errorf("round-trip mismatch: %+v", d)
	}
}

func TestLoadDuplicateDecisions_InvalidJSON(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"duplicate_decisions.json": "{{not json",
	})
	defer cleanup()
	if _, err := loader.LoadDuplicateDecisions(); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveDuplicateDecision_RoundTrip(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	dec := DuplicateDecision{
		KeptHash:       "h1",
		SuppressedHash: "h2",
		Outcome:        DuplicateOutcomeKeptWinner,
		DecidedAt:      time.Now().UTC(),
	}
	if err := loader.SaveDuplicateDecision("pairA", dec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loader.LoadDuplicateDecisions()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got["pairA"].KeptHash != "h1" {
		t.Errorf("expected h1, got %+v", got["pairA"])
	}
}

func TestSaveDuplicateDecision_KeptBoth(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	dec := DuplicateDecision{
		Outcome:   DuplicateOutcomeKeptBoth,
		DecidedAt: time.Now().UTC(),
	}
	if err := loader.SaveDuplicateDecision("pairB", dec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, _ := loader.LoadDuplicateDecisions()
	if got["pairB"].Outcome != DuplicateOutcomeKeptBoth {
		t.Errorf("expected kept_both, got %+v", got["pairB"])
	}
}

func TestSaveDuplicateDecision_EmptyKeyRejected(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now()})
	if err == nil {
		t.Error("expected error for empty pair key")
	}
}

func TestSaveDuplicateDecision_UnknownOutcomeRejected(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("k", DuplicateDecision{Outcome: "weird"})
	if err == nil {
		t.Error("expected error for unknown outcome")
	}
}

func TestSaveDuplicateDecision_KeptWinnerRequiresBothHashes(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	err := loader.SaveDuplicateDecision("k", DuplicateDecision{
		Outcome:   DuplicateOutcomeKeptWinner,
		KeptHash:  "h1",
		DecidedAt: time.Now(),
	})
	if err == nil {
		t.Error("expected error for kept_winner missing suppressed_hash")
	}
}

func TestClearDuplicateDecision(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	loader.SaveDuplicateDecision("k1", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now(),
	})
	loader.SaveDuplicateDecision("k2", DuplicateDecision{
		Outcome: DuplicateOutcomeKeptBoth, DecidedAt: time.Now(),
	})
	if err := loader.ClearDuplicateDecision("k1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := loader.LoadDuplicateDecisions()
	if _, ok := got["k1"]; ok {
		t.Error("k1 should be cleared")
	}
	if _, ok := got["k2"]; !ok {
		t.Error("k2 should remain")
	}
}

func TestClearDuplicateDecision_Missing_NoError(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()
	if err := loader.ClearDuplicateDecision("never-saved"); err != nil {
		t.Errorf("clear of missing key should not error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./internal/services/dataloader/... -run DuplicateDecision -v`
Expected: FAIL — types and methods don't exist.

- [ ] **Step 3: Implement persistence layer**

Create `internal/services/dataloader/duplicate_decisions.go`:

```go
package dataloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const duplicateDecisionsFile = "duplicate_decisions.json"

// Outcome string constants for DuplicateDecision.Outcome.
const (
	DuplicateOutcomeKeptWinner = "kept_winner"
	DuplicateOutcomeKeptBoth   = "kept_both"
)

// DuplicateDecision records the user's resolution of a single
// near-duplicate candidate pair. Outcome controls how the loader
// applies it on subsequent loads:
//
//   - kept_winner: SuppressedHash is excluded from aggregations
//     via Transaction.Suppressed = true.
//   - kept_both: both transactions stay live; the pair is no longer
//     re-flagged as a candidate.
type DuplicateDecision struct {
	KeptHash       string    `json:"kept_hash,omitempty"`
	SuppressedHash string    `json:"suppressed_hash,omitempty"`
	Outcome        string    `json:"outcome"`
	DecidedAt      time.Time `json:"decided_at"`
}

// duplicateDecisionsDoc is the on-disk wire format. Keeping decisions
// nested under a "decisions" key leaves room for future top-level
// metadata (schema version, etc.) without a breaking change.
type duplicateDecisionsDoc struct {
	Decisions map[string]DuplicateDecision `json:"decisions"`
}

func (dl *DataLoader) duplicateDecisionsPath() string {
	return filepath.Join(dl.CSVDirectory, duplicateDecisionsFile)
}

// LoadDuplicateDecisions reads the pairKey → DuplicateDecision map
// from disk. Returns an empty map if the file does not exist.
func (dl *DataLoader) LoadDuplicateDecisions() (map[string]DuplicateDecision, error) {
	path := dl.duplicateDecisionsPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]DuplicateDecision), nil
		}
		return nil, err
	}
	var doc duplicateDecisionsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid duplicate_decisions file: %w", err)
	}
	if doc.Decisions == nil {
		return make(map[string]DuplicateDecision), nil
	}
	return doc.Decisions, nil
}

// SaveDuplicateDecision writes a decision keyed by pairKey, replacing
// any prior decision under the same key. Validates outcome and the
// hash invariants for kept_winner.
func (dl *DataLoader) SaveDuplicateDecision(pairKey string, decision DuplicateDecision) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	if err := validateDuplicateDecision(decision); err != nil {
		return err
	}
	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		return err
	}
	if decision.DecidedAt.IsZero() {
		decision.DecidedAt = time.Now().UTC()
	}
	decisions[pairKey] = decision
	return dl.writeDecisions(decisions)
}

// ClearDuplicateDecision removes a decision. No-op if the key isn't
// present. Used by the review panel's "Undo" button.
func (dl *DataLoader) ClearDuplicateDecision(pairKey string) error {
	if pairKey == "" {
		return fmt.Errorf("pair key is required")
	}
	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		return err
	}
	if _, ok := decisions[pairKey]; !ok {
		return nil
	}
	delete(decisions, pairKey)
	return dl.writeDecisions(decisions)
}

func (dl *DataLoader) writeDecisions(decisions map[string]DuplicateDecision) error {
	doc := duplicateDecisionsDoc{Decisions: decisions}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.duplicateDecisionsPath(), data, 0644)
}

func validateDuplicateDecision(d DuplicateDecision) error {
	switch d.Outcome {
	case DuplicateOutcomeKeptWinner:
		if d.KeptHash == "" || d.SuppressedHash == "" {
			return fmt.Errorf("kept_winner requires both kept_hash and suppressed_hash")
		}
	case DuplicateOutcomeKeptBoth:
		// kept_both ignores hashes; nothing to validate.
	default:
		return fmt.Errorf("unknown outcome %q (want %q or %q)",
			d.Outcome, DuplicateOutcomeKeptWinner, DuplicateOutcomeKeptBoth)
	}
	return nil
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/services/dataloader/... -run DuplicateDecision -v`
Expected: all 11 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/dataloader/duplicate_decisions.go internal/services/dataloader/duplicate_decisions_test.go
git commit -m "$(cat <<'EOF'
feat(dataloader): persistence for duplicate decisions

Adds DuplicateDecision type and load/save/clear methods backed by
duplicate_decisions.json via dl.store.WriteFile (atomic + encryption-
aware). Validation enforces outcome enum and the kept_winner hash
invariant.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Detection algorithm — `detectNearDuplicatePairs`

**Files:**
- Create: `internal/services/dataloader/near_duplicates.go`
- Create: `internal/services/dataloader/near_duplicates_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/services/dataloader/near_duplicates_test.go`:

```go
package dataloader

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func makeTx(date string, amount float64, desc, status string) models.Transaction {
	d, _ := time.Parse("2006-01-02", date)
	t := models.Transaction{
		Date:            d,
		Amount:          amount,
		Description:     desc,
		Status:          status,
		TransactionType: models.Outflow,
	}
	t.Hash = t.ComputeHash()
	return t
}

func TestDetect_PositiveLucidCase(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Key == "" {
		t.Error("pair key should be non-empty")
	}
}

func TestDetect_NegativeTooFarApart(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-27", -1580.43, "Check #996583", "Posted"), // 8 days
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeBothChecks(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Check #996582", "Posted"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeBothBillPays(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Toyota", "Pending"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs, got %d", len(got))
	}
}

func TestDetect_NegativeOppositeSign(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", 1580.43, "Check #996583", "Posted")
	check.TransactionType = models.Income
	txns := []models.Transaction{billPay, check}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (opposite signs), got %d", len(got))
	}
}

func TestDetect_NegativeWrongAmount(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.44, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 0 {
		t.Errorf("expected 0 pairs (different cents), got %d", len(got))
	}
}

func TestDetect_PositiveEmptyStatusOnBillPaySide(t *testing.T) {
	// Bill-pay side has no status; check side has Posted. Should pair.
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", ""),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 1 {
		t.Errorf("expected 1 pair, got %d", len(got))
	}
}

func TestDetect_PositiveEmptyStatusOnCheckSide(t *testing.T) {
	// Check side has no status (description-only signal). Should pair.
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", ""),
	}
	if got := detectNearDuplicatePairs(txns); len(got) != 1 {
		t.Errorf("expected 1 pair, got %d", len(got))
	}
}

func TestDetect_TripletPicksClosestDate(t *testing.T) {
	// Three same-amount transactions, mixed roles. The bill-pay should
	// pair with the check that's closest in date (3/20), not the one
	// that's farther (3/24).
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	closeCheck := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")
	farCheck := makeTx("2026-03-24", -1580.43, "Check #996590", "Posted")
	txns := []models.Transaction{billPay, closeCheck, farCheck}

	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly 1 pair, got %d: %+v", len(pairs), pairs)
	}
	gotHashes := map[string]bool{pairs[0].Left.Hash: true, pairs[0].Right.Hash: true}
	if !gotHashes[billPay.Hash] || !gotHashes[closeCheck.Hash] {
		t.Errorf("expected pair (billPay, closeCheck), got %+v", gotHashes)
	}
	if gotHashes[farCheck.Hash] {
		t.Error("farCheck should not be in any pair")
	}
}

func TestDetect_PairKeyIsOrderIndependent(t *testing.T) {
	billPay := makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay")
	check := makeTx("2026-03-20", -1580.43, "Check #996583", "Posted")

	a := detectNearDuplicatePairs([]models.Transaction{billPay, check})
	b := detectNearDuplicatePairs([]models.Transaction{check, billPay})

	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected 1 pair each, got %d / %d", len(a), len(b))
	}
	if a[0].Key != b[0].Key {
		t.Errorf("pair key should be order-independent: %q vs %q", a[0].Key, b[0].Key)
	}
}

func TestDetect_Idempotency(t *testing.T) {
	txns := []models.Transaction{
		makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
		makeTx("2026-03-20", -1580.43, "Check #996583", "Posted"),
	}
	a := detectNearDuplicatePairs(txns)
	b := detectNearDuplicatePairs(txns)
	if len(a) != 1 || len(b) != 1 || a[0].Key != b[0].Key {
		t.Errorf("detection should be idempotent: %+v vs %+v", a, b)
	}
}

func TestDetect_CheckRegexTolerance(t *testing.T) {
	// Various check-description shapes should all match.
	for _, desc := range []string{"Check #996583", "CHECK # 996583", "Check#996583", "Check #996583 cleared"} {
		txns := []models.Transaction{
			makeTx("2026-03-19", -1580.43, "Lucid", "Scheduled Bill Pay"),
			makeTx("2026-03-20", -1580.43, desc, "Posted"),
		}
		if got := detectNearDuplicatePairs(txns); len(got) != 1 {
			t.Errorf("desc %q should still match check pattern, got %d pairs", desc, len(got))
		}
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./internal/services/dataloader/... -run TestDetect -v`
Expected: FAIL — `detectNearDuplicatePairs` is undefined.

- [ ] **Step 3: Implement detection**

Create `internal/services/dataloader/near_duplicates.go`:

```go
package dataloader

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strings"

	"budget2/internal/models"
)

// Heuristic constants. Hardcoded for v1; spec §9 explicitly defers a
// settings UI until real-world false-positive data warrants tuning.
const (
	duplicateWindowDays = 7
)

// checkPrefixRE matches descriptions that look like a posted check
// reference. Anchored at the start (so it can't match arbitrary text
// containing "check #") but not at the end (so banks that append a
// payee or "cleared" suffix still match). Whitespace around the # is
// tolerated to handle export quirks like "CHECK # 996583".
var checkPrefixRE = regexp.MustCompile(`(?i)^check\s*#\s*\d+\b`)

var (
	billPayStatusKeywords = []string{"scheduled", "pending", "processing", "bill pay"}
	postedStatusKeywords  = []string{"posted", "cleared", "processed"}
)

// DuplicatePair is the public-facing detection result. Order of Left
// and Right is stable for a given input but is not otherwise
// meaningful — UI should treat them symmetrically.
type DuplicatePair struct {
	Key   string
	Left  models.Transaction
	Right models.Transaction
}

// detectNearDuplicatePairs scans transactions for bill-pay → posted-
// check candidate pairs as defined in spec §2.
//
// Pairing is greedy by smallest date difference: a transaction can
// appear in at most one pair, ties broken by lexicographically smaller
// partner hash for determinism.
func detectNearDuplicatePairs(txns []models.Transaction) []DuplicatePair {
	if len(txns) < 2 {
		return nil
	}

	// Index by amount-in-cents → outflow indexes only. Cents avoid
	// float-equality landmines; outflow filter avoids matching income.
	byCents := make(map[int64][]int)
	for i := range txns {
		t := txns[i]
		if t.TransactionType != models.Outflow {
			continue
		}
		if t.Amount >= 0 {
			continue
		}
		cents := int64(math.Round(math.Abs(t.Amount) * 100))
		byCents[cents] = append(byCents[cents], i)
	}

	used := make(map[int]bool)
	var pairs []DuplicatePair

	// Iterate cent buckets in deterministic key order.
	keys := make([]int64, 0, len(byCents))
	for k := range byCents {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, cents := range keys {
		idxs := byCents[cents]
		if len(idxs) < 2 {
			continue
		}
		// For each unused candidate, find the best partner: smallest
		// date diff, then lexicographically smaller partner hash.
		for _, i := range idxs {
			if used[i] {
				continue
			}
			bestJ := -1
			bestDiff := duplicateWindowDays + 1
			for _, j := range idxs {
				if j == i || used[j] {
					continue
				}
				if !isCandidatePair(txns[i], txns[j]) {
					continue
				}
				diff := dayDiff(txns[i].Date, txns[j].Date)
				if diff < 0 || diff > duplicateWindowDays {
					continue
				}
				if diff < bestDiff {
					bestDiff = diff
					bestJ = j
				} else if diff == bestDiff && bestJ >= 0 {
					if txns[j].Hash < txns[bestJ].Hash {
						bestJ = j
					}
				}
			}
			if bestJ < 0 {
				continue
			}
			used[i] = true
			used[bestJ] = true
			pairs = append(pairs, DuplicatePair{
				Key:   pairKey(txns[i].Hash, txns[bestJ].Hash),
				Left:  txns[i],
				Right: txns[bestJ],
			})
		}
	}
	return pairs
}

// isCandidatePair returns true if exactly one of (a, b) looks like a
// scheduled bill pay AND the other looks like a posted check.
func isCandidatePair(a, b models.Transaction) bool {
	aBP, aPC := classify(a)
	bBP, bPC := classify(b)
	return (aBP && bPC) || (aPC && bBP)
}

func classify(t models.Transaction) (billPay, postedCheck bool) {
	descIsCheck := checkPrefixRE.MatchString(strings.TrimSpace(t.Description))
	statusLower := strings.ToLower(strings.TrimSpace(t.Status))
	statusEmpty := statusLower == ""

	if descIsCheck {
		// Posted-check side: status must be empty or contain a posted
		// keyword. A check description with a "Scheduled" status (rare
		// but possible in bill-pay-with-physical-check exports) is
		// still treated as posted because the description trumps.
		if statusEmpty || containsAny(statusLower, postedStatusKeywords) {
			postedCheck = true
		}
	} else {
		// Bill-pay side: description does NOT look like a check.
		// Status must be empty or contain a bill-pay keyword.
		if statusEmpty || containsAny(statusLower, billPayStatusKeywords) {
			billPay = true
		}
	}
	return
}

func containsAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func dayDiff(a, b time.Time) int {
	const day = 24 * 60 * 60
	diff := a.Unix() - b.Unix()
	if diff < 0 {
		diff = -diff
	}
	return int(diff / day)
}

// pairKey is order-independent and deterministic. SHA-256 over
// `min|max` of the two hashes ensures (A,B) and (B,A) hash identically.
func pairKey(hashA, hashB string) string {
	lo, hi := hashA, hashB
	if hi < lo {
		lo, hi = hi, lo
	}
	sum := sha256.Sum256([]byte(lo + "|" + hi))
	return hex.EncodeToString(sum[:8])
}
```

Add the missing import for `time`:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"
)
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/services/dataloader/... -run TestDetect -v`
Expected: all 12 tests PASS.

- [ ] **Step 5: Run full dataloader package**

Run: `go test ./internal/services/dataloader/...`
Expected: PASS — pure-function detection has no side effects.

- [ ] **Step 6: Commit**

```bash
git add internal/services/dataloader/near_duplicates.go internal/services/dataloader/near_duplicates_test.go
git commit -m "$(cat <<'EOF'
feat(dataloader): detect near-duplicate bill-pay/check pairs

Tight heuristic from design §2: same outflow cents within ≤7 days
where one side matches scheduled-bill-pay markers and the other
matches the posted-check regex. Pair key is sha256(sorted-hash-pair),
deterministic and order-independent.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Integrate detection into `LoadData` + apply decisions

**Files:**
- Modify: `internal/services/dataloader/loader.go` (add detection step + state on `DataLoader`)
- Modify: `internal/services/dataloader/loader_test.go` (end-to-end test)

- [ ] **Step 1: Write failing integration test**

Append to `internal/services/dataloader/loader_test.go`:

```go
func TestLoadData_DetectsAndExposesUnresolvedPair(t *testing.T) {
	csvA := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n"
	csvB := "Date,Description,Amount,Status\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"a.csv": csvA, "b.csv": csvB,
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if loader.UnresolvedDuplicateCount() != 1 {
		t.Errorf("UnresolvedDuplicateCount = %d, want 1", loader.UnresolvedDuplicateCount())
	}
	tagged := 0
	for _, tr := range ts.Transactions {
		if tr.DuplicatePairKey != "" {
			tagged++
		}
		if tr.Suppressed {
			t.Errorf("no decision saved yet, no transaction should be Suppressed: %+v", tr)
		}
	}
	if tagged != 2 {
		t.Errorf("expected both sides tagged with DuplicatePairKey, got %d tagged", tagged)
	}
}

func TestLoadData_AppliesKeptWinnerDecision(t *testing.T) {
	csvA := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n"
	csvB := "Date,Description,Amount,Status\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"a.csv": csvA, "b.csv": csvB,
	})
	defer cleanup()

	// First load: discover pair and capture both hashes.
	ts1, err := loader.LoadData()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	var pk, billHash, checkHash string
	for _, tr := range ts1.Transactions {
		if strings.HasPrefix(strings.ToLower(tr.Description), "check") {
			checkHash = tr.Hash
		} else {
			billHash = tr.Hash
		}
		if tr.DuplicatePairKey != "" {
			pk = tr.DuplicatePairKey
		}
	}
	if pk == "" || billHash == "" || checkHash == "" {
		t.Fatalf("setup: pk=%q bill=%q check=%q", pk, billHash, checkHash)
	}

	// Save kept_winner: keep the check, suppress the bill-pay.
	if err := loader.SaveDuplicateDecision(pk, DuplicateDecision{
		KeptHash:       checkHash,
		SuppressedHash: billHash,
		Outcome:        DuplicateOutcomeKeptWinner,
	}); err != nil {
		t.Fatalf("save decision: %v", err)
	}

	// Second load: bill-pay should be Suppressed; pair no longer
	// counted as unresolved.
	ts2, err := loader.LoadData()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("UnresolvedDuplicateCount after decision = %d, want 0",
			loader.UnresolvedDuplicateCount())
	}
	for _, tr := range ts2.Transactions {
		if tr.Hash == billHash && !tr.Suppressed {
			t.Errorf("bill-pay should be Suppressed: %+v", tr)
		}
		if tr.Hash == checkHash && tr.Suppressed {
			t.Errorf("kept check should not be Suppressed: %+v", tr)
		}
		if tr.DuplicatePairKey != "" {
			t.Errorf("resolved pair should clear DuplicatePairKey: %+v", tr)
		}
	}

	// Active() should drop the suppressed bill-pay.
	if got := ts2.Active().Len(); got != 1 {
		t.Errorf("Active() len = %d, want 1", got)
	}
}

func TestLoadData_AppliesKeptBothDecision(t *testing.T) {
	csvA := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n"
	csvB := "Date,Description,Amount,Status\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"a.csv": csvA, "b.csv": csvB,
	})
	defer cleanup()

	ts1, _ := loader.LoadData()
	var pk string
	for _, tr := range ts1.Transactions {
		if tr.DuplicatePairKey != "" {
			pk = tr.DuplicatePairKey
		}
	}
	loader.SaveDuplicateDecision(pk, DuplicateDecision{Outcome: DuplicateOutcomeKeptBoth})

	ts2, _ := loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("UnresolvedDuplicateCount = %d, want 0",
			loader.UnresolvedDuplicateCount())
	}
	suppressed := 0
	tagged := 0
	for _, tr := range ts2.Transactions {
		if tr.Suppressed {
			suppressed++
		}
		if tr.DuplicatePairKey != "" {
			tagged++
		}
	}
	if suppressed != 0 {
		t.Errorf("kept_both should not suppress anything, got %d Suppressed", suppressed)
	}
	if tagged != 0 {
		t.Errorf("kept_both should clear DuplicatePairKey, got %d tagged", tagged)
	}
}

func TestLoadData_CorruptDecisionsFile_DoesNotBlockLoad(t *testing.T) {
	csvA := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"a.csv":                    csvA,
		"duplicate_decisions.json": "{{not json",
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData should not block on corrupt decisions: %v", err)
	}
	if ts.Len() != 2 {
		t.Errorf("expected 2 transactions, got %d", ts.Len())
	}
	// Detection still happened, decisions just couldn't be applied.
	if loader.UnresolvedDuplicateCount() != 1 {
		t.Errorf("UnresolvedDuplicateCount = %d, want 1",
			loader.UnresolvedDuplicateCount())
	}
}
```

If the test file doesn't already import `strings`, add it.

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test ./internal/services/dataloader/... -run TestLoadData_ -v`
Expected: FAIL — `UnresolvedDuplicateCount` is undefined.

- [ ] **Step 3: Add per-load state to `DataLoader`**

In `internal/services/dataloader/loader.go`, find the `DataLoader` struct (around line 25-30). Add:

```go
type DataLoader struct {
	CSVDirectory          string
	enabledFiles          map[string]bool
	store                 *storage.Storage
	FilteredTransferCount int

	// Populated by every LoadData call. Read-only for callers.
	unresolvedDuplicates []DuplicatePair
	resolvedDuplicates   []DuplicatePair
}
```

(Adjust based on the actual existing fields — keep order, just add the two new slice fields.)

Add accessor methods at the end of the file:

```go
// UnresolvedDuplicateCount returns the number of candidate pairs that
// have not yet been resolved by the user. Recomputed on every LoadData.
func (dl *DataLoader) UnresolvedDuplicateCount() int {
	return len(dl.unresolvedDuplicates)
}

// UnresolvedDuplicates returns the candidate pairs awaiting user
// review, in detection order.
func (dl *DataLoader) UnresolvedDuplicates() []DuplicatePair {
	out := make([]DuplicatePair, len(dl.unresolvedDuplicates))
	copy(out, dl.unresolvedDuplicates)
	return out
}

// ResolvedDuplicates returns the kept_winner pairs the user has
// already resolved, sourced from the most recent load. The Left side
// is the kept transaction; Right is the suppressed one.
func (dl *DataLoader) ResolvedDuplicates() []DuplicatePair {
	out := make([]DuplicatePair, len(dl.resolvedDuplicates))
	copy(out, dl.resolvedDuplicates)
	return out
}
```

- [ ] **Step 4: Hook detection into `LoadData`**

In `LoadData` (around line 122-185), find this block:

```go
	// Preprocess: filter transfers, classify, deduplicate
	allTransactions = dl.filterInternalTransfers(allTransactions)
	allTransactions = classifier.ClassifyTransactions(allTransactions)
	allTransactions = dl.deduplicateTransactions(allTransactions)

	// Apply user-assigned aliases
	allTransactions = dl.applyAliases(allTransactions)
```

Insert AFTER `deduplicateTransactions` and BEFORE `applyAliases`:

```go
	// Detect near-duplicate pairs and apply user decisions. Failure
	// modes are non-fatal: a corrupt decisions file still allows the
	// load to complete, just with all candidates marked unresolved.
	allTransactions = dl.applyDuplicateDetection(allTransactions)
```

Now add the helper at the end of `loader.go`:

```go
// applyDuplicateDetection runs near-duplicate detection, loads any
// saved decisions, and stamps Suppressed / DuplicatePairKey on the
// transactions accordingly. Caches unresolved/resolved pairs on the
// loader for handlers to read.
func (dl *DataLoader) applyDuplicateDetection(txns []models.Transaction) []models.Transaction {
	dl.unresolvedDuplicates = nil
	dl.resolvedDuplicates = nil

	pairs := detectNearDuplicatePairs(txns)
	if len(pairs) == 0 {
		return txns
	}

	decisions, err := dl.LoadDuplicateDecisions()
	if err != nil {
		log.Printf("Warning: could not load duplicate decisions: %v", err)
		decisions = nil
	}

	// Build hash → index lookup once.
	idxByHash := make(map[string]int, len(txns))
	for i, t := range txns {
		idxByHash[t.Hash] = i
	}

	for _, pair := range pairs {
		decision, resolved := decisions[pair.Key]
		if !resolved {
			// Tag both sides for badge rendering.
			if i, ok := idxByHash[pair.Left.Hash]; ok {
				txns[i].DuplicatePairKey = pair.Key
			}
			if i, ok := idxByHash[pair.Right.Hash]; ok {
				txns[i].DuplicatePairKey = pair.Key
			}
			dl.unresolvedDuplicates = append(dl.unresolvedDuplicates, pair)
			continue
		}
		switch decision.Outcome {
		case DuplicateOutcomeKeptWinner:
			if i, ok := idxByHash[decision.SuppressedHash]; ok {
				txns[i].Suppressed = true
			}
			// Keep the user-side roles in the resolved list: Left = kept.
			leftKept := pair.Left
			rightSuppressed := pair.Right
			if pair.Left.Hash == decision.SuppressedHash {
				leftKept, rightSuppressed = pair.Right, pair.Left
			}
			dl.resolvedDuplicates = append(dl.resolvedDuplicates, DuplicatePair{
				Key:   pair.Key,
				Left:  leftKept,
				Right: rightSuppressed,
			})
		case DuplicateOutcomeKeptBoth:
			// No-op: leave both transactions live and untagged.
		}
	}
	return txns
}
```

- [ ] **Step 5: Run tests — expect PASS**

Run: `go test ./internal/services/dataloader/... -run TestLoadData_ -v`
Expected: all four new tests PASS.

- [ ] **Step 6: Run full dataloader package**

Run: `go test ./internal/services/dataloader/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/services/dataloader/loader.go internal/services/dataloader/loader_test.go
git commit -m "$(cat <<'EOF'
feat(dataloader): integrate near-duplicate detection into LoadData

LoadData runs detection after exact-hash dedup, applies any saved
user decisions, and exposes UnresolvedDuplicates / ResolvedDuplicates
/ UnresolvedDuplicateCount for handlers and templates.

Suppressed transactions stay in the slice (the explorer is an audit
view); aggregation call sites use TransactionSet.Active() in a later
task to actually exclude them from totals.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Skip suppressed transactions in major-expense matching

**Files:**
- Modify: `internal/services/dataloader/major_expense_names.go:43-65`
- Modify: `internal/services/dataloader/major_expense_names_test.go` (add test)

**Why now:** the label-stamping pass runs inside `LoadData` (so it sees `Suppressed=true` already set by Task 5). We don't want a suppressed Lucid bill-pay to inflate the "Lucid" major expense bucket.

- [ ] **Step 1: Write failing test**

Append to `internal/services/dataloader/major_expense_names_test.go`:

```go
func TestApplyMajorExpenseNames_SkipsSuppressed(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": `[{"id":"e1","name":"Lucid","keywords":["lucid"]}]`,
	})
	defer cleanup()

	// Two transactions matching "Lucid" — one suppressed, one not.
	txns := []models.Transaction{
		{
			Hash: "h1", Description: "Lucid", Amount: -1580.43,
			TransactionType: models.Outflow, Suppressed: true,
		},
		{
			Hash: "h2", Description: "Lucid", Amount: -1580.43,
			TransactionType: models.Outflow,
		},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "" {
		t.Errorf("suppressed transaction should not be labeled, got %q",
			got[0].MajorExpenseName)
	}
	if got[1].MajorExpenseName != "Lucid" {
		t.Errorf("active transaction should be labeled, got %q",
			got[1].MajorExpenseName)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/services/dataloader/... -run TestApplyMajorExpenseNames_SkipsSuppressed -v`
Expected: FAIL — currently the suppressed transaction also gets the "Lucid" label.

- [ ] **Step 3: Implement the skip**

In `internal/services/dataloader/major_expense_names.go`, find the loop body (around lines 43-63). The current top of the loop is:

```go
	for i := range transactions {
		t := transactions[i]
		// Major Expenses is an outflow concept. Skip income so a paycheck
		// or refund whose description happens to contain an expense
		// keyword (e.g. "BOFA HOMELOANS REFUND") doesn't get stamped and
		// then surface as that expense via Transaction.Label().
		if t.TransactionType == models.Income {
			continue
		}
```

Add a second skip immediately after the income check:

```go
		// Suppressed transactions are the losing side of a resolved
		// near-duplicate pair. Excluding them here keeps Major Expense
		// totals consistent with the dashboard's Active() aggregation.
		if t.Suppressed {
			continue
		}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/services/dataloader/... -run TestApplyMajorExpenseNames -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/dataloader/major_expense_names.go internal/services/dataloader/major_expense_names_test.go
git commit -m "$(cat <<'EOF'
feat(dataloader): skip Suppressed transactions in major-expense labeling

A suppressed near-duplicate is the losing side of a resolved pair;
labeling it would let it surface in the Major Expenses page and
inflate that bucket's total even though the dashboard excludes it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Switch dashboard aggregation to `Active()`

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go` (multiple `loader.LoadData()` call sites)

**Note on test coverage:** existing dashboard tests use empty/non-suppressed fixtures, so behavior is unchanged for them. The end-to-end coverage that proves suppression actually excludes from totals lives in `loader_test.go` (already added in Task 5 via `Active().Len()`).

- [ ] **Step 1: Update dashboard handlers**

In `internal/handlers/dashboard/handlers.go`, every call site that does `loader.LoadData()` followed by aggregation (e.g. `data.FilterByDateRange(...)`) should switch to `data.Active()` first.

Concrete lines to change (line numbers are approximate; search by pattern):

Around line 134-135:
```go
	filtered := data.FilterByDateRange(startDate, endDate)
```
becomes:
```go
	filtered := data.Active().FilterByDateRange(startDate, endDate)
```

Around line 144 (`calculateComparison(data, ...)`): the `data` argument is the raw set used inside helper functions. Switch the argument:
```go
		periodComparison = calculateComparison(data.Active(), startDate, endDate, comparison, settings)
```

Around line 188:
```go
	filtered := data.FilterByDateRange(startDate, endDate)
```
becomes:
```go
	filtered := data.Active().FilterByDateRange(startDate, endDate)
```

Around line 196:
```go
		periodComparison = calculateComparison(data, startDate, endDate, comparison, settings)
```
becomes:
```go
		periodComparison = calculateComparison(data.Active(), startDate, endDate, comparison, settings)
```

Around line 233:
```go
	filtered := data.FilterByDateRange(startDate, endDate)
```
becomes:
```go
	filtered := data.Active().FilterByDateRange(startDate, endDate)
```

Repeat for any remaining `loader.LoadData()` call sites in the file. Use `grep -n "loader.LoadData\|filterByDateRange\|FilterByDateRange" internal/handlers/dashboard/handlers.go` to find them all. Each `data.FilterByDateRange(...)` and each function argument that takes a raw aggregation set should become `data.Active().FilterByDateRange(...)` or `data.Active()`.

The drilldown handler around line 269 may need different treatment — read the function body and decide whether the explorer-style "show me everything" view or the aggregation-style view applies. Drilldowns that show transaction lists for the user to click into may want the raw set; drilldowns that show totals want `Active()`. Default to `Active()` for any totals/sums.

- [ ] **Step 2: Run dashboard tests**

Run: `go test ./internal/handlers/dashboard/...`
Expected: PASS — fixtures don't use Suppressed, so behavior is unchanged.

- [ ] **Step 3: Build the binary to confirm no compile errors**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/dashboard/handlers.go
git commit -m "$(cat <<'EOF'
refactor(dashboard): aggregate via TransactionSet.Active()

Excludes Suppressed near-duplicate losers from totals, charts, and
KPI calculations while leaving the raw set available for explorer-
style views. Behavior unchanged for users without resolved pairs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Switch insights aggregation to `Active()`

**Files:**
- Modify: `internal/handlers/insights/handlers.go`

- [ ] **Step 1: Find aggregation call sites**

Run: `grep -n "loader.LoadData\|FilterByType\|FilterByDateRange\|GroupBy" internal/handlers/insights/handlers.go`

For each handler that loads data and feeds it into totals/aggregations (look for `FilterByType(models.Outflow)`, `FilterByDateRange`, `GroupByMonth`, `SumAmount`, `SumAbsAmount`, `MonthlyTotals`, etc.), replace `data.FilterBy...` with `data.Active().FilterBy...`.

The pattern is identical to Task 7: every `data := loader.LoadData()` is followed by some aggregation. Replace the aggregation chain's first link with `data.Active()`.

- [ ] **Step 2: Run insights tests**

Run: `go test ./internal/handlers/insights/...`
Expected: PASS.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/insights/handlers.go
git commit -m "$(cat <<'EOF'
refactor(insights): aggregate via TransactionSet.Active()

Same rationale as dashboard: Suppressed near-duplicate losers should
not flow through KPI calculations or trend charts.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Switch what-if and major-expenses aggregation to `Active()`

**Files:**
- Modify: `internal/handlers/whatif/handlers.go`
- Modify: `internal/handlers/majorexpenses/handlers.go`

- [ ] **Step 1: Switch whatif call sites**

Run: `grep -n "loader.LoadData\|FilterByType\|FilterByDateRange\|GroupBy" internal/handlers/whatif/handlers.go`

For each aggregation chain rooted in a `loader.LoadData()`-derived `*TransactionSet`, insert `.Active()` before the first filter/group call. Skip places where the result feeds an explorer-style detail view (none in whatif as of design time).

- [ ] **Step 2: Switch majorexpenses call sites**

Run: `grep -n "loader.LoadData\|FilterByType\|FilterByDateRange\|GroupBy" internal/handlers/majorexpenses/handlers.go`

Same pattern. The Major Expenses summary table is an aggregation (totals per declared expense), so the underlying iteration must use `Active()`. The exception/unmatched list is also an aggregation (totals → buckets). Pin assignment views — if any iterate transactions for display — keep the raw set so the user can pin a suppressed transaction if they ever need to.

- [ ] **Step 3: Run handler tests**

Run: `go test ./internal/handlers/whatif/... ./internal/handlers/majorexpenses/...`
Expected: PASS.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/whatif/handlers.go internal/handlers/majorexpenses/handlers.go
git commit -m "$(cat <<'EOF'
refactor(whatif,majorexpenses): aggregate via TransactionSet.Active()

Same rationale as dashboard/insights: keep Suppressed near-duplicate
losers out of totals while preserving explorer/audit visibility.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Shared page-data helper for `UnresolvedDuplicateCount`

**Files:**
- Create: `internal/templates/page_data.go`
- Create: `internal/templates/page_data_test.go`

**Why a helper:** the nav badge (Task 12) and dashboard alert (Task 13) both need the count, and it should appear on every full-page render. A single helper keeps the Go-side wiring identical across handlers.

- [ ] **Step 1: Write failing test**

Create `internal/templates/page_data_test.go`:

```go
package templates

import (
	"testing"
)

type fakeCountSource struct{ n int }

func (f fakeCountSource) UnresolvedDuplicateCount() int { return f.n }

func TestAttachDuplicateCount_SetsKey(t *testing.T) {
	pageData := map[string]interface{}{"Title": "Dashboard"}
	AttachDuplicateCount(pageData, fakeCountSource{n: 3})
	if got := pageData["UnresolvedDuplicateCount"]; got != 3 {
		t.Errorf("UnresolvedDuplicateCount = %v, want 3", got)
	}
}

func TestAttachDuplicateCount_NilSourceSetsZero(t *testing.T) {
	pageData := map[string]interface{}{}
	AttachDuplicateCount(pageData, nil)
	if got := pageData["UnresolvedDuplicateCount"]; got != 0 {
		t.Errorf("UnresolvedDuplicateCount = %v, want 0", got)
	}
}

func TestAttachDuplicateCount_NilMap_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil map should not panic, got: %v", r)
		}
	}()
	AttachDuplicateCount(nil, fakeCountSource{n: 1})
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/templates/... -run AttachDuplicateCount -v`
Expected: FAIL — function undefined.

- [ ] **Step 3: Implement helper**

Create `internal/templates/page_data.go`:

```go
package templates

// DuplicateCountSource is the minimal contract handlers need to attach
// the unresolved-duplicate count to a page-data map. Implemented by
// *dataloader.DataLoader.
type DuplicateCountSource interface {
	UnresolvedDuplicateCount() int
}

// AttachDuplicateCount sets pageData["UnresolvedDuplicateCount"] from
// the source. Safe with a nil source (writes 0) and a nil map (no-op).
// Handlers should call this before rendering any full-page template
// so the nav badge and dashboard alert see the same value.
func AttachDuplicateCount(pageData map[string]interface{}, src DuplicateCountSource) {
	if pageData == nil {
		return
	}
	count := 0
	if src != nil {
		count = src.UnresolvedDuplicateCount()
	}
	pageData["UnresolvedDuplicateCount"] = count
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/templates/... -run AttachDuplicateCount -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/templates/page_data.go internal/templates/page_data_test.go
git commit -m "$(cat <<'EOF'
feat(templates): AttachDuplicateCount helper for shared nav/alert data

Single helper that handlers call to attach UnresolvedDuplicateCount
to their page-data map. Enables both the nav badge and the dashboard
alert to read from one source without per-handler boilerplate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Wire `AttachDuplicateCount` into every full-page handler

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go`
- Modify: `internal/handlers/explorer/handlers.go`
- Modify: `internal/handlers/whatif/handlers.go`
- Modify: `internal/handlers/insights/handlers.go`
- Modify: `internal/handlers/majorexpenses/handlers.go`

- [ ] **Step 1: Identify each `renderer.Render(w, "base", ...)` call**

Run: `grep -rn 'renderer.Render(w, "base"' internal/handlers/ --include="*.go"`

For each match, the line immediately before is typically a `pageData := map[string]interface{}{...}` declaration. The fix pattern is:

```go
	pageData := map[string]interface{}{ ... }
	templates.AttachDuplicateCount(pageData, loader)
	renderer.Render(w, "base", pageData)
```

If `pageData` already exists with a different name (e.g. `data`), use that name. If the handler builds the map inside an HTMX-vs-full-page branch, attach the count only on the full-page branch — partial renders don't render the nav.

- [ ] **Step 2: Add the import where needed**

Each modified file needs:

```go
import "budget2/internal/templates"
```

…added to its import block (most already have this).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run handler tests**

Run: `go test ./internal/handlers/...`
Expected: PASS — adding a key to the page data doesn't change template output for tests that compare against rendered HTML, because the nav rendering hasn't been touched yet.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/explorer/handlers.go internal/handlers/whatif/handlers.go internal/handlers/insights/handlers.go internal/handlers/majorexpenses/handlers.go
git commit -m "$(cat <<'EOF'
feat(handlers): attach UnresolvedDuplicateCount to every full-page render

Every full-page handler now calls templates.AttachDuplicateCount before
rendering the base layout, so the nav badge (next task) sees a value.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Nav link with count badge in `base.html`

**Files:**
- Modify: `web/templates/layouts/base.html` (after the Major Expenses link, around line 90-92)

- [ ] **Step 1: Edit the nav**

In `web/templates/layouts/base.html`, find the Major Expenses link block (around lines 90-92):

```html
                    <a href="/major-expenses" class="px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 transition-colors {{if eq .ActiveTab "major-expenses"}}bg-white/20{{end}}">
                        Major Expenses
                    </a>
```

Immediately after it (before the File Manager link), insert:

```html
                    {{if gt (.UnresolvedDuplicateCount | int) 0}}
                    <a href="/duplicates" class="relative px-3 py-2 rounded-md text-sm font-medium hover:bg-white/10 transition-colors {{if eq .ActiveTab "duplicates"}}bg-white/20{{end}}">
                        Duplicates
                        <span class="ml-1 inline-flex items-center justify-center px-2 py-0.5 text-xs font-semibold rounded-full bg-amber-500 text-white">{{.UnresolvedDuplicateCount}}</span>
                    </a>
                    {{end}}
```

Note: `int` is a registered template func in this project (verify with `grep -n '"int":' internal/templates/`). If not registered, replace `(.UnresolvedDuplicateCount | int)` with just `.UnresolvedDuplicateCount` — `gt` works on integers natively in `html/template`.

- [ ] **Step 2: Verify the template-func dependency**

Run: `grep -rn '"int"\|FuncMap\b' internal/templates/`

If no `int` registration exists, simplify the conditional in the template to:

```html
                    {{if .UnresolvedDuplicateCount}}
```

(Go's `html/template` treats a zero int as false.)

Either form is acceptable; pick whichever matches the codebase's existing FuncMap conventions.

- [ ] **Step 3: Build and run unit tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4: Manual smoke (optional but recommended)**

```bash
go run ./cmd/server
# Then in another terminal:
curl -s http://localhost:PORT/dashboard | grep -A2 "Duplicates"
```

Expected: with no duplicates in test data, no `Duplicates` link appears. If you have a CSV with the Lucid case, the link appears with `(1)` badge.

- [ ] **Step 5: Commit**

```bash
git add web/templates/layouts/base.html
git commit -m "$(cat <<'EOF'
feat(ui): nav badge for unresolved duplicate count

Shown only when UnresolvedDuplicateCount > 0; reads from the shared
page-data helper so every full-page render includes it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Dashboard alert card for unresolved duplicates

**Files:**
- Modify: `web/templates/components/alerts.html`
- Modify: `internal/handlers/dashboard/handlers.go` (ensure pageData has the count — already done in Task 11)

The existing alerts component renders from `.Alerts`. We have two options: extend the `.Alerts` data to include duplicate-review entries (cleanly typed) or add a dedicated block for duplicates inside the alerts component. The dedicated block keeps the alert wiring trivial and avoids touching every alert producer.

- [ ] **Step 1: Add a dedicated duplicates block in `alerts.html`**

In `web/templates/components/alerts.html`, find the outer `{{define "alerts"}}` block. Wrap or extend the existing card to add a duplicates section. The minimal change: add a sibling block after the Spending Alerts card for a Duplicates Review card.

Replace the file's contents with:

```html
{{define "alerts"}}
{{if or .Alerts .UnresolvedDuplicateCount}}
<div class="space-y-4">
{{if .Alerts}}
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
    <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-3 flex items-center">
        <svg class="w-5 h-5 mr-2 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z">
            </path>
        </svg>
        Spending Alerts
    </h3>
    <div class="space-y-2">
        {{range $idx, $alert := .Alerts}}
        <a href="/explorer?start={{if .Date}}{{.Date.Format "2006-01-02"}}{{end}}&end={{if .Date}}{{.Date.Format "2006-01-02"}}{{end}}&type=Outflow{{if .Detail}}&search={{urlEncode .Detail}}{{end}}"
           class="block rounded-lg {{if eq .Severity "warning"}}bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 hover:bg-amber-100 dark:hover:bg-amber-900/50{{else if eq .Severity "error"}}bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-700 hover:bg-red-100 dark:hover:bg-red-900/50{{else}}bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-700 hover:bg-blue-100 dark:hover:bg-blue-900/50{{end}} transition-colors">
            <div class="flex items-start p-3">
                <div class="flex-shrink-0 mr-3">
                    {{if eq .Severity "warning"}}
                    <svg class="w-5 h-5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z">
                        </path>
                    </svg>
                    {{else if eq .Severity "error"}}
                    <svg class="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                            d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                    </svg>
                    {{else}}
                    <svg class="w-5 h-5 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                            d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                    </svg>
                    {{end}}
                </div>
                <div class="flex-1 min-w-0">
                    <p class="text-sm font-medium {{if eq .Severity "warning"}}text-amber-800 dark:text-amber-200{{else if eq .Severity "error"}}text-red-800 dark:text-red-200{{else}}text-blue-800 dark:text-blue-200{{end}}">
                        {{.Title}}
                    </p>
                    <p class="text-sm {{if eq .Severity "warning"}}text-amber-600 dark:text-amber-300{{else if eq .Severity "error"}}text-red-600 dark:text-red-300{{else}}text-blue-600 dark:text-blue-300{{end}}">
                        {{.Message}}
                    </p>
                </div>
                <div class="flex-shrink-0 ml-2">
                    <svg class="w-5 h-5 {{if eq .Severity "warning"}}text-amber-400{{else if eq .Severity "error"}}text-red-400{{else}}text-blue-400{{end}}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
                    </svg>
                </div>
            </div>
        </a>
        {{end}}
    </div>
</div>
{{end}}
{{if .UnresolvedDuplicateCount}}
<a href="/duplicates" class="block bg-white dark:bg-gray-800 rounded-lg shadow p-4 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors">
    <div class="flex items-center">
        <div class="flex-shrink-0 mr-3">
            <svg class="w-5 h-5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"></path>
            </svg>
        </div>
        <div class="flex-1 min-w-0">
            <p class="text-sm font-medium text-gray-800 dark:text-gray-100">
                {{.UnresolvedDuplicateCount}} possible duplicate {{if eq .UnresolvedDuplicateCount 1}}pair needs{{else}}pairs need{{end}} review
            </p>
            <p class="text-sm text-gray-600 dark:text-gray-300">
                Bill-pay and posted-check entries from overlapping CSV exports.
            </p>
        </div>
        <div class="flex-shrink-0 ml-2 text-amber-500">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
            </svg>
        </div>
    </div>
</a>
{{end}}
</div>
{{end}}
{{end}}
```

- [ ] **Step 2: Build and test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/templates/components/alerts.html
git commit -m "$(cat <<'EOF'
feat(ui): dashboard alert card for unresolved duplicate pairs

Renders a clickable card linking to /duplicates whenever
UnresolvedDuplicateCount > 0, alongside existing spending alerts.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Duplicates handler package — `GET /duplicates` (skeleton)

**Files:**
- Create: `internal/handlers/duplicates/handlers.go`
- Create: `internal/handlers/duplicates/handlers_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/handlers/duplicates/handlers_test.go`:

```go
package duplicates

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGetDuplicates_RendersUnresolvedTab(t *testing.T) {
	// Initialize wires loader (nil-safe path) and renderer (we use raw
	// template-free fallback so the test doesn't depend on web embed).
	Initialize(nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/duplicates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Duplicates") {
		t.Errorf("body missing 'Duplicates' marker: %s", body)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/handlers/duplicates/... -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement handler skeleton**

Create `internal/handlers/duplicates/handlers.go`:

```go
// Package duplicates serves the near-duplicate review panel.
package duplicates

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"budget2/internal/services/dataloader"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
)

// Initialize wires package dependencies. Both arguments may be nil in
// tests (the page falls back to a JSON-encoded payload when there is
// no renderer).
func Initialize(l *dataloader.DataLoader, r *templates.Renderer) {
	loader = l
	renderer = r
}

// RegisterRoutes registers all duplicates routes.
func RegisterRoutes(r chi.Router) {
	r.Get("/duplicates", handlePage)
	r.Post("/duplicates/resolve", handleResolve)
	r.Post("/duplicates/undo", handleUndo)
}

func handlePage(w http.ResponseWriter, r *http.Request) {
	pageData := buildPageData()
	templates.AttachDuplicateCount(pageData, loader)

	if renderer != nil {
		renderer.Render(w, "base", pageData)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pageData)
}

func buildPageData() map[string]interface{} {
	pageData := map[string]interface{}{
		"Title":     "Duplicates",
		"ActiveTab": "duplicates",
	}
	if loader == nil {
		// Fallback empty values so the template always has the keys.
		pageData["Unresolved"] = []dataloader.DuplicatePair{}
		pageData["Resolved"] = []dataloader.DuplicatePair{}
		return pageData
	}
	// Trigger a load so detection state is fresh; ignore the
	// transaction set itself (the panel doesn't render rows directly).
	if _, err := loader.LoadData(); err != nil {
		log.Printf("duplicates: failed to load data: %v", err)
	}
	pageData["Unresolved"] = loader.UnresolvedDuplicates()
	pageData["Resolved"] = loader.ResolvedDuplicates()
	return pageData
}

func handleResolve(w http.ResponseWriter, r *http.Request) {
	if loader == nil {
		http.Error(w, "loader not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pairKey := r.FormValue("pair_key")
	outcome := r.FormValue("outcome")
	keptHash := r.FormValue("kept_hash")
	suppressedHash := r.FormValue("suppressed_hash")

	if pairKey == "" {
		http.Error(w, "missing pair_key", http.StatusBadRequest)
		return
	}
	dec := dataloader.DuplicateDecision{
		Outcome:        outcome,
		KeptHash:       keptHash,
		SuppressedHash: suppressedHash,
	}
	if err := loader.SaveDuplicateDecision(pairKey, dec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/duplicates", http.StatusSeeOther)
}

func handleUndo(w http.ResponseWriter, r *http.Request) {
	if loader == nil {
		http.Error(w, "loader not initialized", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pairKey := r.FormValue("pair_key")
	if pairKey == "" {
		http.Error(w, "missing pair_key", http.StatusBadRequest)
		return
	}
	if err := loader.ClearDuplicateDecision(pairKey); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/duplicates", http.StatusSeeOther)
}
```

- [ ] **Step 4: Run skeleton test — expect PASS**

Run: `go test ./internal/handlers/duplicates/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/duplicates/handlers.go internal/handlers/duplicates/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(handlers): duplicates package skeleton with resolve/undo

GET /duplicates renders the review panel page data (template added
in next task). POST /duplicates/resolve writes a DuplicateDecision;
POST /duplicates/undo clears one. Both 303 back to /duplicates.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Resolve / undo handler tests with a real loader

**Files:**
- Modify: `internal/handlers/duplicates/handlers_test.go`

- [ ] **Step 1: Add tests that exercise resolve and undo end-to-end**

Extend the import block at the top of `internal/handlers/duplicates/handlers_test.go` so it includes everything below (do NOT add a second `import` block — Go disallows that):

```go
import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)
```

Then append to `internal/handlers/duplicates/handlers_test.go`:

```go

func newLoaderWithFixture(t *testing.T) (*dataloader.DataLoader, func()) {
	t.Helper()
	tmp, err := os.MkdirTemp("", "dup_handlers")
	if err != nil {
		t.Fatalf("tmpdir: %v", err)
	}
	csv := "Date,Description,Amount,Status\n" +
		"2026-03-19,Lucid,-1580.43,Scheduled Bill Pay\n" +
		"2026-03-20,Check #996583,-1580.43,Posted\n"
	if err := os.WriteFile(filepath.Join(tmp, "bank.csv"), []byte(csv), 0644); err != nil {
		os.RemoveAll(tmp)
		t.Fatalf("write csv: %v", err)
	}
	store, err := storage.New(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		t.Fatalf("storage: %v", err)
	}
	return dataloader.New(tmp, store), func() { os.RemoveAll(tmp) }
}

func TestResolve_KeptWinner_PersistsAndRedirects(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("first load: %v", err)
	}
	pairs := loader.UnresolvedDuplicates()
	if len(pairs) != 1 {
		t.Fatalf("expected 1 unresolved pair, got %d", len(pairs))
	}
	left, right := pairs[0].Left, pairs[0].Right

	form := url.Values{}
	form.Set("pair_key", pairs[0].Key)
	form.Set("outcome", dataloader.DuplicateOutcomeKeptWinner)
	form.Set("kept_hash", left.Hash)
	form.Set("suppressed_hash", right.Hash)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/duplicates" {
		t.Errorf("Location = %q, want /duplicates", got)
	}

	if _, err := loader.LoadData(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("UnresolvedDuplicateCount after resolve = %d, want 0",
			loader.UnresolvedDuplicateCount())
	}
}

func TestResolve_KeptBoth_StopsFlagging(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)
	loader.LoadData()
	pairs := loader.UnresolvedDuplicates()

	form := url.Values{}
	form.Set("pair_key", pairs[0].Key)
	form.Set("outcome", dataloader.DuplicateOutcomeKeptBoth)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Errorf("kept_both should clear unresolved count, got %d",
			loader.UnresolvedDuplicateCount())
	}
}

func TestResolve_BadPairKey_ReturnsBadRequest(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)

	form := url.Values{}
	form.Set("outcome", dataloader.DuplicateOutcomeKeptBoth)
	// pair_key intentionally omitted

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/resolve",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestUndo_ClearsDecisionAndReflagsOnReload(t *testing.T) {
	loader, cleanup := newLoaderWithFixture(t)
	defer cleanup()
	Initialize(loader, nil)
	loader.LoadData()
	pairs := loader.UnresolvedDuplicates()
	pk := pairs[0].Key

	loader.SaveDuplicateDecision(pk, dataloader.DuplicateDecision{
		Outcome:        dataloader.DuplicateOutcomeKeptWinner,
		KeptHash:       pairs[0].Left.Hash,
		SuppressedHash: pairs[0].Right.Hash,
	})
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 0 {
		t.Fatalf("setup: expected resolved, got %d unresolved",
			loader.UnresolvedDuplicateCount())
	}

	form := url.Values{}
	form.Set("pair_key", pk)

	r := chi.NewRouter()
	RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/duplicates/undo",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	loader.LoadData()
	if loader.UnresolvedDuplicateCount() != 1 {
		t.Errorf("after undo, expected 1 unresolved, got %d",
			loader.UnresolvedDuplicateCount())
	}
}
```

- [ ] **Step 2: Run tests — expect PASS**

Run: `go test ./internal/handlers/duplicates/... -v`
Expected: all four new tests PASS.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/duplicates/handlers_test.go
git commit -m "$(cat <<'EOF'
test(duplicates): end-to-end resolve/undo handler coverage

Real loader + temp-dir fixture exercises the full path from POST
through persistence to a subsequent LoadData reading the decision.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Duplicates page template

**Files:**
- Create: `web/templates/pages/duplicates.html`

The template renders two sections (unresolved + suppressed), each card showing both transactions side-by-side with three (or one) action buttons.

- [ ] **Step 1: Create the template**

Create `web/templates/pages/duplicates.html`:

```html
{{define "page-content"}}
<div class="space-y-6">
    <header>
        <h1 class="text-2xl font-semibold text-gray-800 dark:text-gray-100">Duplicate Review</h1>
        <p class="text-sm text-gray-600 dark:text-gray-300 mt-1">
            Bill-pay and posted-check entries that may represent the same payment.
            Pick one to keep, or mark them as both real.
        </p>
    </header>

    <section>
        <h2 class="text-lg font-medium text-gray-800 dark:text-gray-100 mb-3">
            Unresolved
            {{if .Unresolved}}<span class="ml-2 text-sm text-gray-500">({{len .Unresolved}})</span>{{end}}
        </h2>
        {{if .Unresolved}}
        <div class="space-y-3">
            {{range .Unresolved}}
            <div id="{{.Key}}" class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {{template "dup-card-side" .Left}}
                    {{template "dup-card-side" .Right}}
                </div>
                <div class="mt-4 flex flex-wrap gap-2">
                    <form method="post" action="/duplicates/resolve" class="inline">
                        <input type="hidden" name="pair_key" value="{{.Key}}">
                        <input type="hidden" name="outcome" value="kept_winner">
                        <input type="hidden" name="kept_hash" value="{{.Left.Hash}}">
                        <input type="hidden" name="suppressed_hash" value="{{.Right.Hash}}">
                        <button type="submit" class="px-3 py-1.5 rounded bg-indigo-600 text-white text-sm hover:bg-indigo-700">
                            Keep left
                        </button>
                    </form>
                    <form method="post" action="/duplicates/resolve" class="inline">
                        <input type="hidden" name="pair_key" value="{{.Key}}">
                        <input type="hidden" name="outcome" value="kept_winner">
                        <input type="hidden" name="kept_hash" value="{{.Right.Hash}}">
                        <input type="hidden" name="suppressed_hash" value="{{.Left.Hash}}">
                        <button type="submit" class="px-3 py-1.5 rounded bg-indigo-600 text-white text-sm hover:bg-indigo-700">
                            Keep right
                        </button>
                    </form>
                    <form method="post" action="/duplicates/resolve" class="inline">
                        <input type="hidden" name="pair_key" value="{{.Key}}">
                        <input type="hidden" name="outcome" value="kept_both">
                        <button type="submit" class="px-3 py-1.5 rounded border border-gray-300 dark:border-gray-600 text-sm text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700">
                            Both real (stop flagging)
                        </button>
                    </form>
                </div>
            </div>
            {{end}}
        </div>
        {{else}}
        <p class="text-sm text-gray-500 dark:text-gray-400">No unresolved candidate pairs.</p>
        {{end}}
    </section>

    <section>
        <h2 class="text-lg font-medium text-gray-800 dark:text-gray-100 mb-3">
            Suppressed
            {{if .Resolved}}<span class="ml-2 text-sm text-gray-500">({{len .Resolved}})</span>{{end}}
        </h2>
        {{if .Resolved}}
        <div class="space-y-3">
            {{range .Resolved}}
            <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div>
                        <p class="text-xs text-gray-500 mb-1">Kept</p>
                        {{template "dup-card-side" .Left}}
                    </div>
                    <div class="opacity-60">
                        <p class="text-xs text-gray-500 mb-1">Suppressed</p>
                        {{template "dup-card-side" .Right}}
                    </div>
                </div>
                <div class="mt-4">
                    <form method="post" action="/duplicates/undo" class="inline">
                        <input type="hidden" name="pair_key" value="{{.Key}}">
                        <button type="submit" class="px-3 py-1.5 rounded border border-gray-300 dark:border-gray-600 text-sm text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700">
                            Undo
                        </button>
                    </form>
                </div>
            </div>
            {{end}}
        </div>
        {{else}}
        <p class="text-sm text-gray-500 dark:text-gray-400">No suppressed pairs.</p>
        {{end}}
    </section>
</div>
{{end}}

{{define "dup-card-side"}}
<div class="border border-gray-200 dark:border-gray-700 rounded p-3">
    <div class="text-sm font-medium text-gray-800 dark:text-gray-100">{{.Description}}</div>
    <div class="text-xs text-gray-500 dark:text-gray-400 mt-1">
        {{.Date.Format "2006-01-02"}}
        {{if .Status}}<span class="ml-2 px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-700">{{.Status}}</span>{{end}}
    </div>
    <div class="text-sm text-red-600 dark:text-red-400 mt-1">${{printf "%.2f" .Amount}}</div>
    <div class="text-xs text-gray-400 mt-1">{{.SourceFile}}</div>
</div>
{{end}}
```

- [ ] **Step 2: Verify the renderer picks up the new template**

Most projects with `web/embed.go` auto-discover templates by glob. Confirm with:

```bash
grep -n "ParseGlob\|Templates\b\|page-content" internal/templates/*.go web/embed.go
```

If the renderer expects a specific naming convention (e.g. each page defines a `{{define "page-content"}}` block consumed by the base layout), the template above already follows it. If it expects a different block name, update accordingly.

- [ ] **Step 3: Manual smoke**

```bash
go run ./cmd/server
```

Visit `http://localhost:PORT/duplicates`. With CSV data containing the Lucid case, expect to see one card with `Keep left`, `Keep right`, `Both real`. Click `Keep right`, verify the card moves to "Suppressed" with an `Undo` button.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/duplicates.html
git commit -m "$(cat <<'EOF'
feat(ui): duplicate review panel template

Two sections (Unresolved / Suppressed) with side-by-side cards and
neutral Keep-left / Keep-right / Both-real action buttons. Undo
appears on resolved pairs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: Wire duplicates package in `cmd/server/main.go`

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Initialize and register routes**

In `cmd/server/main.go`, find the block where other handlers are initialized (around line 80-90):

```go
	dashboard.Initialize(loader, renderer, retirementMgr)
	explorer.Initialize(loader, renderer, cfg, store)
	whatif.Initialize(loader, renderer, retirementMgr)
	insights.Initialize(loader, renderer)
	majorexpenses.Initialize(loader, renderer)
```

Add immediately after:

```go
	duplicates.Initialize(loader, renderer)
```

Find the route-registration block (around line 140-150):

```go
		dashboard.RegisterRoutes(r)
		explorer.RegisterRoutes(r)
		whatif.RegisterRoutes(r)
		insights.RegisterRoutes(r)
		majorexpenses.RegisterRoutes(r)
```

Add:

```go
		duplicates.RegisterRoutes(r)
```

Add the import to the import block at the top:

```go
	"budget2/internal/handlers/duplicates"
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Smoke server**

Run: `go run ./cmd/server`
Visit `/duplicates` — expect the page to render. Visit `/dashboard` — expect the new alert and nav badge to appear (assuming a duplicate-bearing CSV is loaded).

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(server): register duplicates handler routes

/duplicates, /duplicates/resolve, /duplicates/undo are now wired into
the chi router alongside the other handler packages.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: Explorer transaction-row badges

**Files:**
- Modify: `web/templates/pages/explorer.html` (or whichever component renders transaction rows)

- [ ] **Step 1: Find the transaction-row template fragment**

Run: `grep -rn "Description\|MajorExpenseName\|Amount\b" web/templates/pages/explorer.html | head -30`

Identify the row that renders each transaction. The fragment usually iterates over `.Transactions` or `.Rows`.

- [ ] **Step 2: Add the badge near the description**

Inside the row's description cell, append:

```html
{{if .DuplicatePairKey}}
<a href="/duplicates#{{.DuplicatePairKey}}" class="ml-2 inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200 hover:bg-amber-200">
    dup?
</a>
{{end}}
{{if .Suppressed}}
<span class="ml-2 inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-300 line-through">
    suppressed dup
</span>
{{end}}
```

If the explorer also renders amount/description with a fade-style class, gate the fade on `.Suppressed` so the row visually de-emphasizes:

```html
<tr class="{{if .Suppressed}}opacity-50{{end}}">
```

Place this on whatever row element wraps each transaction.

- [ ] **Step 3: Build and run**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Manual smoke**

With the Lucid fixture loaded, visit `/explorer` and confirm:
- Both Lucid-pair rows show `dup?` until resolved.
- After resolving, the suppressed row shows `suppressed dup` with faded styling.
- Clicking `dup?` jumps to the right card on `/duplicates`.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/templates/pages/explorer.html
git commit -m "$(cat <<'EOF'
feat(ui): explorer badges for unresolved + suppressed duplicates

Unresolved-pair members get a 'dup?' badge linking to the review
panel anchor; suppressed losers get a faded 'suppressed dup' badge
so the user always sees what's been hidden from totals.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 19: Manual end-to-end smoke + CHANGELOG

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: End-to-end smoke**

```bash
go build ./... && go test ./...
```

Expected: clean build, all tests pass.

Then run the server with a CSV containing the Lucid case:

```bash
go run ./cmd/server
```

Click through:

1. Dashboard shows "1 possible duplicate pair needs review →" alert card.
2. Nav shows "Duplicates (1)" with amber badge.
3. Click into `/duplicates` → see the Lucid 3/19 ↔ Check #996583 3/20 card with three buttons.
4. Click `Keep right` (the Posted check).
5. Redirected back; the pair moves into the Suppressed section with `Undo`.
6. Nav badge disappears; dashboard alert disappears.
7. Major Expenses page total for "Lucid" no longer double-counts.
8. Explorer shows the suppressed Lucid row with faded `suppressed dup` badge.
9. Click `Undo` from `/duplicates` → pair returns to Unresolved; nav badge reappears.

Capture issues if any; cycle back to the relevant task to fix.

- [ ] **Step 2: Update CHANGELOG**

Open `CHANGELOG.md` and add an entry under the current version's `### Added` (or create a new unreleased section). Use the existing changelog tone. Example entry:

```markdown
### Added
- **Near-duplicate transaction detection.** Bill-pay and posted-check
  pairs from overlapping CSV exports are now detected (≤7-day window,
  same outflow cents) and surfaced via a `/duplicates` review panel,
  a dashboard alert, and a nav badge. Resolving a pair soft-suppresses
  one side from totals while keeping it visible in the explorer for
  audit/undo.
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "$(cat <<'EOF'
docs: changelog for near-duplicate detection feature

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 4: Final verification**

```bash
git log --oneline -25
go test ./...
```

Expected: all tests pass; commit history shows ~19 small, focused commits.

---

## Plan execution notes

- **Task ordering invariant:** every commit leaves the tree compiling and tests passing. Tasks 1-5 build foundations; Tasks 6-9 are mechanical aggregation switches that change behavior only when suppressed rows exist; Tasks 10-18 are UI/handler wiring; Task 19 is final verification.

- **What can be split into a separate branch if scope feels heavy:** Tasks 1-6 (data + detection + persistence, no UI) could ship as a "feature-flagged off" first PR — there are no user-visible effects until the UI tasks merge. The user did not request feature flagging, but if this plan ends up being too large to land in one branch, that's the natural seam.

- **Subagent dispatch hint:** Tasks 7, 8, 9 are highly parallel (independent files, no shared state). Tasks 12, 13, 16, 18 (template-only changes) can also be parallelized once Task 11 has landed.

- **Open-question revisit triggers:**
  - Q1 (nav data source): if `AttachDuplicateCount` ends up requiring more than ~5 handler edits or hits HTMX partial-render inconsistencies, switch to the HTMX endpoint approach.
  - Q2 (review UI neutrality): if early dogfooding shows users always pick the Posted side, add a recommendation in a follow-up.
  - Q3 (status aliases): if real CSVs surface unsupported status names, append to `columnMappings["Status"]` and the keyword lists in `near_duplicates.go`.
