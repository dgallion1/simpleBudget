# Exception-row description → Explorer link Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wrap the Description column of every Exceptions row in an `/explorer?search=…&type=Outflow` anchor so users can trace each exception back to the underlying bank transaction in the Explorer — closing the parity gap with matched-row descriptions.

**Architecture:** Presentation-only template change in `web/templates/pages/major-expenses.html`. Four `<td>` description cells (one per render path: `UnknownLarge` legacy, `AllUnmatched` current, `Anomalous`, `NewMerchants`) get a description anchor that mirrors the matched-row pattern at line ~752, plus `event.stopPropagation()` on each anchor so the existing row-level click handler that pre-fills the add form does not also fire. No engine, handler, or model code changes.

**Tech Stack:** Go `html/template`, existing render tests in `internal/templates/render_major_expenses_test.go`. URL escaping is provided by the built-in `urlquery` template function; in attribute context Go renders `+` as `&#43;`, matching the assertion convention at test line ~424.

**Spec:** `docs/superpowers/specs/2026-05-02-exception-description-explorer-link-design.md`.

---

### Task 1: Link exception-row descriptions to Explorer (4 cells, TDD)

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (4 description cells in the exceptions panel: lines ~954, ~988, ~1036, ~1084)
- Modify: `internal/templates/render_major_expenses_test.go` (extend `TestRenderMajorExpenses_WithEntriesAndExceptions` and `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming`)

The two tests above already cover all four exception render paths via existing fixtures:

| Render path | Test function | Fixture description | Expected encoded query |
|-------------|---------------|---------------------|------------------------|
| `UnknownLarge` legacy | `TestRenderMajorExpenses_WithEntriesAndExceptions` | `Random Big Purchase` | `Random&#43;Big&#43;Purchase` |
| `Anomalous` | `TestRenderMajorExpenses_WithEntriesAndExceptions` | `My Landlord LLC` | `My&#43;Landlord&#43;LLC` |
| `NewMerchants` | `TestRenderMajorExpenses_WithEntriesAndExceptions` | `Brand New Store` | `Brand&#43;New&#43;Store` |
| `AllUnmatched` | `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming` | `Big Unknown Charge` | `Big&#43;Unknown&#43;Charge` |

No new fixtures are required.

- [ ] **Step 1: Add failing assertions to `TestRenderMajorExpenses_WithEntriesAndExceptions`**

Open `internal/templates/render_major_expenses_test.go`. Locate the existing matched-row anchor block (the test currently asserts `href="/explorer?search=Landlord&#43;LLC&type=Outflow"` for matched rows around line 424). Immediately **after** that block and before the closing `}` of the test (around line 452), insert:

```go
	// Each exception bucket's Description column links to the Explorer
	// pre-filtered to that bank text (Outflow), giving users a path back
	// to the underlying bank transaction. The anchor must stop click
	// propagation so the row-level click handler (which pre-fills the
	// add form) does not also fire. Spaces are URL-encoded as "+" and
	// then HTML-escaped as "&#43;" by html/template in attribute context.
	if !strings.Contains(html, `<a href="/explorer?search=Random&#43;Big&#43;Purchase&type=Outflow"`) {
		t.Errorf("expected unknown-large description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=My&#43;Landlord&#43;LLC&type=Outflow"`) {
		t.Errorf("expected anomalous description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=Brand&#43;New&#43;Store&type=Outflow"`) {
		t.Errorf("expected new-merchant description to link to /explorer, got html=%s", html)
	}
	// One stopPropagation per exception description anchor (3 in this
	// fixture: UnknownLarge, Anomalous, NewMerchants). The matched-row
	// anchor does NOT use stopPropagation (no row click handler there),
	// so the count is a tight lower bound on the exception anchors.
	if got := strings.Count(html, `onclick="event.stopPropagation()"`); got < 3 {
		t.Errorf("expected at least 3 stopPropagation handlers on exception description anchors, got %d. html=%s", got, html)
	}
```

- [ ] **Step 2: Add failing assertions to `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming`**

In the same test file, locate `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming` (starts around line 35). Immediately **before** its closing `}` (around line 71, after the existing "Unmatched over $" assertion), insert:

```go
	// AllUnmatched render path: each row's Description must link back
	// to the Explorer for the bank text, with click-propagation stopped
	// so the row's pre-fill handler does not fire alongside navigation.
	if !strings.Contains(html, `<a href="/explorer?search=Big&#43;Unknown&#43;Charge&type=Outflow"`) {
		t.Errorf("expected AllUnmatched description to link to /explorer, got html=%s", html)
	}
	if !strings.Contains(html, `<a href="/explorer?search=Tiny&#43;Coffee&type=Outflow"`) {
		t.Errorf("expected sub-threshold AllUnmatched description to link to /explorer, got html=%s", html)
	}
	if got := strings.Count(html, `onclick="event.stopPropagation()"`); got < 2 {
		t.Errorf("expected stopPropagation handler on each AllUnmatched description anchor (2 rows), got %d. html=%s", got, html)
	}
```

- [ ] **Step 3: Run tests to confirm they fail**

```bash
go test ./internal/templates/ -run "WithEntriesAndExceptions|UnmatchedBucketShowsAllRowsWithDimming" -v
```

Expected: FAIL. Both tests should report `expected … to link to /explorer …` errors. The other assertions in those tests must still pass (only the new ones fail).

If any **other** assertion fails, stop — the test file edit was wrong. Re-read the file and correct before proceeding.

- [ ] **Step 4: Edit `UnknownLarge` legacy cell (~line 954) in `web/templates/pages/major-expenses.html`**

Use `Edit` with the date line above included for uniqueness (line 954's bare cell string is identical to line 988's, but the preceding date line differs: `.Transaction.Date.Format` here vs `.Date.Format` at 988).

Replace exactly:

```
                    <td class="px-2 py-1 dark:text-gray-300">{{.Transaction.Date.Format "2006-01-02"}}</td>
                    <td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>
```

With:

```
                    <td class="px-2 py-1 dark:text-gray-300">{{.Transaction.Date.Format "2006-01-02"}}</td>
                    <td class="px-2 py-1 dark:text-gray-200"><a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
                        class="text-blue-600 dark:text-blue-400 hover:underline"
                        title="Show this transaction in the Explorer"
                        onclick="event.stopPropagation()">{{$label}}</a></td>
```

- [ ] **Step 5: Edit `AllUnmatched` cell (~line 988) in `web/templates/pages/major-expenses.html`**

Replace exactly (the date variable is `.Date`, not `.Transaction.Date`, which uniquely distinguishes this from Step 4):

```
                    <td class="px-2 py-1 dark:text-gray-300">{{.Date.Format "2006-01-02"}}</td>
                    <td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>
```

With:

```
                    <td class="px-2 py-1 dark:text-gray-300">{{.Date.Format "2006-01-02"}}</td>
                    <td class="px-2 py-1 dark:text-gray-200"><a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
                        class="text-blue-600 dark:text-blue-400 hover:underline"
                        title="Show this transaction in the Explorer"
                        onclick="event.stopPropagation()">{{$label}}</a></td>
```

- [ ] **Step 6: Edit `Anomalous` Description cell (~line 1036) in `web/templates/pages/major-expenses.html`**

The Anomalous bucket uses `$desc` (not `$label`) for the visible text, but the spec keeps that convention. Replace exactly:

```
                <td class="px-2 py-1 dark:text-gray-300">{{$desc}}</td>
```

With:

```
                <td class="px-2 py-1 dark:text-gray-300"><a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
                    class="text-blue-600 dark:text-blue-400 hover:underline"
                    title="Show this transaction in the Explorer"
                    onclick="event.stopPropagation()">{{$desc}}</a></td>
```

(`$rawText` is already defined for this bucket on the same line as `$desc`, near line 1024: `{{$desc := .Transaction.Label}}{{$rawText := or .Transaction.DisplayName .Transaction.Description}}`. Do not redefine.)

- [ ] **Step 7: Edit `NewMerchants` cell (~line 1084) in `web/templates/pages/major-expenses.html`**

Use the preceding date line (`.FirstSeen.Format`, unique to this bucket) for the Edit's anchor. Replace exactly:

```
                <td class="px-2 py-1 dark:text-gray-300">{{.FirstSeen.Format "2006-01-02"}}</td>
                <td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>
```

With:

```
                <td class="px-2 py-1 dark:text-gray-300">{{.FirstSeen.Format "2006-01-02"}}</td>
                <td class="px-2 py-1 dark:text-gray-200"><a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
                    class="text-blue-600 dark:text-blue-400 hover:underline"
                    title="Show this transaction in the Explorer"
                    onclick="event.stopPropagation()">{{$label}}</a></td>
```

- [ ] **Step 8: Run the targeted tests to confirm they now pass**

```bash
go test ./internal/templates/ -run "WithEntriesAndExceptions|UnmatchedBucketShowsAllRowsWithDimming" -v
```

Expected: PASS for both `TestRenderMajorExpenses_WithEntriesAndExceptions` and `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming`.

- [ ] **Step 9: Run the full templates package tests**

```bash
go test ./internal/templates/ -v
```

Expected: all tests PASS. If any unrelated test fails, stop and investigate — the template edit must not have changed any other rendered behavior.

- [ ] **Step 10: Run the full test suite**

```bash
go test ./...
```

Expected: all packages PASS. The pre-commit hook will run this anyway, so catching it here saves a round-trip.

- [ ] **Step 11: Commit**

```bash
git add web/templates/pages/major-expenses.html internal/templates/render_major_expenses_test.go
git commit -m "$(cat <<'EOF'
feat(major-expenses): link exception descriptions to Explorer

Each exception row's Description column (Unmatched legacy,
AllUnmatched, Anomalous, New Merchants) now wraps the visible
label in an <a href="/explorer?search=<bank-text>&type=Outflow">
anchor matching the matched-row pattern, with
event.stopPropagation() on the anchor so the row-level click
handler that pre-fills the add form does not fire alongside
navigation.

Closes the parity gap reported by the user: previously, exception
rows showed a redundant "Description -> matched-expense-name"
display with no path back to the bank transaction.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: pre-commit hook runs `go vet`, `staticcheck`, `govulncheck`, full `go test ./...`, and refreshes the GitNexus index. All green.

---

## Acceptance criteria (from spec, restated)

- All four exception description render paths carry the anchor.
- Clicking the description on any exception row in any of the three buckets opens the Explorer pre-filtered to outflows matching that description.
- Clicking the description does **not** trigger the add-form pre-fill (verified manually or via a Playwright check post-merge).
- The pinned-state display in the "Pin to…" column is unaffected.
- All existing render tests still pass; new render-test assertions added per Steps 1–2 pass.
