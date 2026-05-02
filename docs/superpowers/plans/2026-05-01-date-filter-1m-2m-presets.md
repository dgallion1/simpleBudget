# Add 1M and 2M Date-Filter Presets — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `1M` and `2M` preset buttons to the four pages that already expose month-based date-range presets (dashboard, insights, explorer, major-expenses), positioned just before the existing `3M` button.

**Architecture:** Small UI/JS change. Each page gets two new `<button>` elements that reuse the adjacent `3M` button's class list and call the page's existing preset helper with month counts of 1 and 2. The dashboard and insights preset helpers need two new switch cases. Explorer and major-expenses need their "detect-selected-range" iteration arrays updated so post-HTMX-swap highlighting picks up the new buttons; explorer should also clear stale highlighting when the current range no longer matches any preset, matching the existing major-expenses behavior.

**Tech Stack:** Go html/template, vanilla JS, HTMX, Tailwind CSS. No new dependencies. No backend changes (the `preset` query param is pass-through).

**Spec:** `docs/superpowers/specs/2026-05-01-date-filter-1m-2m-presets-design.md`

**Testing note:** This codebase has no JS test framework. Verification per task is `make build` (templates parse) plus a manual browser check on the affected page. The full Go test suite (`go test ./...`) should remain green throughout — no Go code is touched.

**Project safety requirements from `AGENTS.md`:**
- Before editing any JS function, run `gitnexus_impact({target: "<functionName>", direction: "upstream"})`, report direct callers / affected processes / risk level, and stop for user confirmation if risk is HIGH or CRITICAL.
- Before every commit, run `gitnexus_detect_changes()` and confirm the reported scope matches the files and functions for that task.
- If any GitNexus tool reports a stale index, run `npx gitnexus analyze` before continuing.

---

### Task 1: Dashboard — add 1M and 2M presets

**Files:**
- Modify: `web/templates/pages/dashboard.html` (insert two buttons between `YTD` and `3M`)
- Modify: `web/static/js/dashboard.js` (add two switch cases in `setPreset`)

- [ ] **Step 1: Run impact analysis for the dashboard helper**

Run:
```js
gitnexus_impact({target: "setPreset", direction: "upstream"})
```

Report the blast radius before editing. Expected risk: LOW, limited to dashboard date-filter preset interactions. If GitNexus reports HIGH or CRITICAL risk, stop and ask the user before editing.

- [ ] **Step 2: Add the two new buttons to the dashboard template**

In `web/templates/pages/dashboard.html`, locate the YTD-then-3M boundary and insert the new buttons between the closing `</button>` of YTD and the opening `<button>` of `3M`. Use `Edit` to replace this block:

Old:
```html
                <button type="button" onclick="setPreset('ytd')" data-preset="ytd"
                    class="preset-btn min-w-[2.5rem] px-2 py-1 text-sm rounded-md transition-colors bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600">
                    YTD
                </button>
                <button type="button" onclick="setPreset('3m')" data-preset="3m"
```

New:
```html
                <button type="button" onclick="setPreset('ytd')" data-preset="ytd"
                    class="preset-btn min-w-[2.5rem] px-2 py-1 text-sm rounded-md transition-colors bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600">
                    YTD
                </button>
                <button type="button" onclick="setPreset('1m')" data-preset="1m"
                    class="preset-btn min-w-[2.5rem] px-2 py-1 text-sm rounded-md transition-colors bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600">
                    1M
                </button>
                <button type="button" onclick="setPreset('2m')" data-preset="2m"
                    class="preset-btn min-w-[2.5rem] px-2 py-1 text-sm rounded-md transition-colors bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600">
                    2M
                </button>
                <button type="button" onclick="setPreset('3m')" data-preset="3m"
```

- [ ] **Step 3: Add the matching switch cases to dashboard.js**

In `web/static/js/dashboard.js`, locate the `setPreset` switch. Use `Edit` to replace this block:

Old:
```js
    switch (preset) {
        case 'ytd':
            start = new Date(end.getFullYear(), 0, 1);
            break;
        case '3m':
            start.setMonth(start.getMonth() - 3);
            break;
```

New:
```js
    switch (preset) {
        case 'ytd':
            start = new Date(end.getFullYear(), 0, 1);
            break;
        case '1m':
            start.setMonth(start.getMonth() - 1);
            break;
        case '2m':
            start.setMonth(start.getMonth() - 2);
            break;
        case '3m':
            start.setMonth(start.getMonth() - 3);
            break;
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: success, no template-parse errors.

- [ ] **Step 5: Manual browser verification (dashboard)**

Run: `make run` (or however the dev server starts) and open `/dashboard`. Confirm:
- The preset row reads: `YTD  1M  2M  3M  6M  12M  All`
- Click `1M`: `start` input updates to today − 1 month, `end` updates to today, KPIs and charts refresh, `1M` becomes the highlighted (indigo) button
- Click `2M`: same behavior with 2-month offset, `2M` becomes highlighted
- Click `3M` (regression check): still works, `3M` highlighted
- Confirm light-mode and dark-mode visual parity between the new buttons and `3M`

- [ ] **Step 6: Run GitNexus change detection and commit**

Run `gitnexus_detect_changes()` before committing. Expected scope: `web/templates/pages/dashboard.html`, `web/static/js/dashboard.js`, and the `setPreset` helper only.

```bash
git add web/templates/pages/dashboard.html web/static/js/dashboard.js
git commit -m "feat(dashboard): add 1M and 2M date-range presets"
```

---

### Task 2: Insights — add 1M and 2M presets

**Files:**
- Modify: `web/templates/pages/insights.html` (insert two buttons before `3M`; add two switch cases in `setInsightPreset`)

- [ ] **Step 1: Run impact analysis for the insights helper**

Run:
```js
gitnexus_impact({target: "setInsightPreset", direction: "upstream"})
```

Report the blast radius before editing. Expected risk: LOW, limited to insights date-filter preset interactions. If GitNexus reports HIGH or CRITICAL risk, stop and ask the user before editing.

- [ ] **Step 2: Add the two new buttons to the insights template**

In `web/templates/pages/insights.html`, locate the `Quick:` label and the first preset button. Use `Edit` to replace this block:

Old:
```html
                <span class="text-sm text-gray-500 dark:text-gray-400">Quick:</span>
                <button type="button" onclick="setInsightPreset('3m')" data-preset="3m"
                        class="insight-preset-btn px-3 py-1 text-sm rounded-md transition-colors {{if eq .Preset "3m"}}bg-indigo-600 text-white{{else}}bg-gray-100 dark:bg-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600{{end}}">
                    3M
                </button>
```

New:
```html
                <span class="text-sm text-gray-500 dark:text-gray-400">Quick:</span>
                <button type="button" onclick="setInsightPreset('1m')" data-preset="1m"
                        class="insight-preset-btn px-3 py-1 text-sm rounded-md transition-colors {{if eq .Preset "1m"}}bg-indigo-600 text-white{{else}}bg-gray-100 dark:bg-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600{{end}}">
                    1M
                </button>
                <button type="button" onclick="setInsightPreset('2m')" data-preset="2m"
                        class="insight-preset-btn px-3 py-1 text-sm rounded-md transition-colors {{if eq .Preset "2m"}}bg-indigo-600 text-white{{else}}bg-gray-100 dark:bg-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600{{end}}">
                    2M
                </button>
                <button type="button" onclick="setInsightPreset('3m')" data-preset="3m"
                        class="insight-preset-btn px-3 py-1 text-sm rounded-md transition-colors {{if eq .Preset "3m"}}bg-indigo-600 text-white{{else}}bg-gray-100 dark:bg-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600{{end}}">
                    3M
                </button>
```

- [ ] **Step 3: Add the matching switch cases to setInsightPreset**

In the same file, locate the switch in `setInsightPreset`. Use `Edit` to replace this block:

Old:
```js
    switch(preset) {
        case '3m':
            start.setMonth(start.getMonth() - 3);
            break;
```

New:
```js
    switch(preset) {
        case '1m':
            start.setMonth(start.getMonth() - 1);
            break;
        case '2m':
            start.setMonth(start.getMonth() - 2);
            break;
        case '3m':
            start.setMonth(start.getMonth() - 3);
            break;
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: success, no template-parse errors.

- [ ] **Step 5: Manual browser verification (insights)**

Open `/insights`. Confirm:
- The preset row reads: `1M  2M  3M  6M  12M  All`
- Click `1M`: page reloads insights wrapper via HTMX with start = today − 1 month, end = today, URL updates to include `&preset=1m`, `1M` becomes the highlighted (indigo) button
- Click `2M`: same behavior with 2-month offset and `&preset=2m`
- Reload `/insights?preset=1m` directly: server-rendered HTML highlights `1M` on first paint (validates the Go-template `{{if eq .Preset "1m"}}` branch)
- Reload `/insights?preset=2m`: same for `2M`
- Confirm light/dark visual parity

- [ ] **Step 6: Run GitNexus change detection and commit**

Run `gitnexus_detect_changes()` before committing. Expected scope: `web/templates/pages/insights.html` and the `setInsightPreset` helper only.

```bash
git add web/templates/pages/insights.html
git commit -m "feat(insights): add 1M and 2M date-range presets"
```

---

### Task 3: Explorer — add 1M and 2M presets

**Files:**
- Modify: `web/templates/pages/explorer.html` (insert two buttons before `3M`; update `detectSelectedDateRange`)

- [ ] **Step 1: Run impact analysis for the explorer detection helper**

Run:
```js
gitnexus_impact({target: "detectSelectedDateRange", direction: "upstream"})
```

Report the blast radius before editing. Expected risk: LOW to MEDIUM, limited to explorer date-filter active-state highlighting after load and HTMX swaps. If GitNexus reports HIGH or CRITICAL risk, stop and ask the user before editing.

- [ ] **Step 2: Add the two new buttons to the explorer template**

In `web/templates/pages/explorer.html`, locate the step-back arrow and the `3M` button. Use `Edit` to replace this block:

Old:
```html
                            <button type="button" onclick="stepDateRange(-1)" title="Step back" aria-label="Step back"
                                class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"></path>
                                </svg>
                            </button>
                            <button type="button" onclick="setDateRange(3)" data-months="3"
                                class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">3M</button>
```

New:
```html
                            <button type="button" onclick="stepDateRange(-1)" title="Step back" aria-label="Step back"
                                class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"></path>
                                </svg>
                            </button>
                            <button type="button" onclick="setDateRange(1)" data-months="1"
                                class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">1M</button>
                            <button type="button" onclick="setDateRange(2)" data-months="2"
                                class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">2M</button>
                            <button type="button" onclick="setDateRange(3)" data-months="3"
                                class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">3M</button>
```

- [ ] **Step 3: Update the detect-selected-range logic**

In the same file, locate `detectSelectedDateRange`. First, make non-matching ranges clear stale selection:

Old:
```js
        // If end date isn't max, no button matches
        if (endDate !== maxDate) return;
```

New:
```js
        // If end date isn't max, no button matches
        if (endDate !== maxDate) {
            updateDateRangeButtons(-1);
            return;
        }
```

Then update the iteration:

Old:
```js
        // Check for 3, 6, 12 month ranges
        const end = new Date(maxDate);
        for (const months of [3, 6, 12]) {
```

New:
```js
        // Check for 1, 2, 3, 6, 12 month ranges
        const end = new Date(maxDate);
        for (const months of [1, 2, 3, 6, 12]) {
```

Finally, clear stale selection when no preset matches:

Old:
```js
        }
    }
```

New:
```js
        }

        updateDateRangeButtons(-1);
    }
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: success.

- [ ] **Step 5: Manual browser verification (explorer)**

Open `/explorer`. Confirm:
- The preset row reads: `←  1M  2M  3M  6M  12M  All  →`
- Click `1M`: start = today − 1 month, end = today, page filters refresh via HTMX, `1M` highlighted indigo
- Click `2M`: same with 2-month offset, `2M` highlighted
- Click `1M` then click step-forward `→` then step-back `←`: after the round-trip, confirm `1M` highlights again. If a stepped range does not match any preset, confirm stale preset highlighting is cleared.
- Click `3M` (regression): still works, `3M` highlighted
- Confirm light/dark visual parity

- [ ] **Step 6: Run GitNexus change detection and commit**

Run `gitnexus_detect_changes()` before committing. Expected scope: `web/templates/pages/explorer.html` and the `detectSelectedDateRange` helper only.

```bash
git add web/templates/pages/explorer.html
git commit -m "feat(explorer): add 1M and 2M date-range presets"
```

---

### Task 4: Major Expenses — add 1M and 2M presets

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (insert two buttons before `3M`; update `meDetectSelectedDateRange`)

- [ ] **Step 1: Run impact analysis for the major-expenses detection helper**

Run:
```js
gitnexus_impact({target: "meDetectSelectedDateRange", direction: "upstream"})
```

Report the blast radius before editing. Expected risk: LOW to MEDIUM, limited to major-expenses date-filter active-state highlighting after load and HTMX swaps. If GitNexus reports HIGH or CRITICAL risk, stop and ask the user before editing.

- [ ] **Step 2: Add the two new buttons to the major-expenses template**

In `web/templates/pages/major-expenses.html`, locate the step-back arrow and the `3M` button. Use `Edit` to replace this block:

Old:
```html
                <button type="button" onclick="meStepDateRange(-1)" title="Step back" aria-label="Step back"
                    class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"></path></svg>
                </button>
                <button type="button" onclick="meSetDateRange(3)" data-months="3"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">3M</button>
```

New:
```html
                <button type="button" onclick="meStepDateRange(-1)" title="Step back" aria-label="Step back"
                    class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"></path></svg>
                </button>
                <button type="button" onclick="meSetDateRange(1)" data-months="1"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">1M</button>
                <button type="button" onclick="meSetDateRange(2)" data-months="2"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">2M</button>
                <button type="button" onclick="meSetDateRange(3)" data-months="3"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">3M</button>
```

- [ ] **Step 3: Update the detect-selected-range iteration**

In the same file, locate `meDetectSelectedDateRange` and its preset iteration. Use `Edit` to replace:

Old:
```js
    for (const months of [3, 6, 12]) {
```

New:
```js
    for (const months of [1, 2, 3, 6, 12]) {
```

- [ ] **Step 4: Verify build**

Run: `make build`
Expected: success.

- [ ] **Step 5: Manual browser verification (major-expenses)**

Open `/major-expenses`. Confirm:
- The preset row reads: `←  1M  2M  3M  6M  12M  All  →`
- Click `1M`: start = today − 1 month, end = today, expense table refreshes via HTMX, `1M` highlighted indigo
- Click `2M`: same with 2-month offset, `2M` highlighted
- Click `3M` (regression): still works, `3M` highlighted
- Confirm light/dark visual parity

- [ ] **Step 6: Run GitNexus change detection and commit**

Run `gitnexus_detect_changes()` before committing. Expected scope: `web/templates/pages/major-expenses.html` and the `meDetectSelectedDateRange` helper only.

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): add 1M and 2M date-range presets"
```

---

### Task 5: Final cross-page verification and Go test sanity check

**Files:** none

- [ ] **Step 1: Run the full Go test suite**

Run: `go test ./...`
Expected: all packages pass (no Go code was touched).

- [ ] **Step 2: Run final gitnexus_detect_changes (per project `AGENTS.md`)**

Run the GitNexus change detector one more time against the final working tree to confirm only the expected template/JS files changed and no Go symbols were unexpectedly affected.
Expected: changes confined to:
- `web/templates/pages/dashboard.html`
- `web/templates/pages/insights.html`
- `web/templates/pages/explorer.html`
- `web/templates/pages/major-expenses.html`
- `web/static/js/dashboard.js`

No Go symbols, processes, or clusters should appear in the impact report.

- [ ] **Step 3: Final visual sweep**

In a single browser session, walk all four pages back-to-back:
1. `/dashboard` — click `1M`, then `2M`
2. `/insights` — click `1M`, then `2M`
3. `/explorer` — click `1M`, then `2M`
4. `/major-expenses` — click `1M`, then `2M`

For each, confirm:
- New buttons render in the correct position (immediately before `3M`)
- Idle, hover, and active (selected) states match `3M` exactly in both light and dark mode
- Date inputs update to the expected values (today and today − N months, where N = 1 or 2)
- Page content (KPIs / charts / table / insights) refreshes after each click

- [ ] **Step 4: No commit needed**

If all checks pass, the per-task commits from Tasks 1–4 are the final state. If any visual regression is found, revert or amend the relevant task's commit before finalizing.
