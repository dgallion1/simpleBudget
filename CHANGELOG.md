# Changelog

## Unreleased

### Storage — the data-directory gate closes the phase-4a prune race (2026-08-15)
- **The bug**: a restore rewrites the data directory and then prunes everything the archive did not contain. Any write landing during or between those steps — an MCP curation tool, a duplicate resolution, a page handler — was deleted by the prune, and the writer was told its write succeeded. Carried as a known gap since phase 4a; shipping `restore_backup` made it reachable without the browser. Design: `docs/superpowers/specs/2026-08-15-data-directory-gate-design.md`.
- **The fix is one RWMutex at the storage layer**, not a hold each writer must remember to take. Every data-directory content write already funnels through `*storage.Storage` (`WriteFile`/`Remove`/`MkdirAll`), so gating there catches writers by construction — "a new writer did not know there was a hold to take" is how this bug arrived. `Storage.BeginExclusive()` returns an `ExclusiveWriter` that holds the directory until `Release`; because `sync.RWMutex` is not reentrant, its methods go through unexported `*Locked` bodies, the same shape `backup.Service.snapshotLocked` uses. `Release` is idempotent, and a released handle refuses to write rather than writing outside the exclusion.
- **Lock order is settings rewrite gate → data directory → snapshot hold, and it is forced, not chosen.** `SettingsManager.SaveWithRevision` holds the manager's lock across its write through `Storage`, so a restore taking data-then-settings is the other half of an ABBA deadlock: restore holds the data directory waiting for the settings lock, in-flight save holds the settings lock waiting for the data directory. The first version of this change had that order; the regression test staged as an already-in-flight save caught it. An earlier version of that same test probed from *inside* the critical section and passed against the broken order — past the window where the cycle forms. Both facts are recorded in the design doc.
- **The safety snapshot now runs inside the hold.** Taken the other way round, a write landing between the snapshot and the hold would be pruned *and* missing from the safety archive — the only failure with no copy anywhere.
- **Reads are deliberately not gated**: a reader can still see a partly rewritten tree, which a page reload settles. Gating reads would put every page render in contention with a restore.
- **Two bypasses are documented rather than silently left**: `settings/auto_backup.json` is written with `os.WriteFile` (routing it through `Storage` would encrypt a file `loadEnabled` must read before unlock), so a `SetEnabled` concurrent with a restore can still be lost; the `cache/` write bypasses `Storage` too but is skip-listed and never pruned.
- **Residual, unchanged**: the gate makes each write atomic against a restore, not each read-modify-write. A tool that read before the restore and writes after keeps its file but carries stale content — the same last-writer-wins residual `BeginExternalRewrite` already documents.
- **Also**: `restore.FromZip` now reports a nil `Store` as `ErrNoStore` instead of panicking, which is what `Deps` always claimed it did.

### MCP — `list_backups` and guarded `restore_backup` (2026-08-15)
- **The two tools phase 4b deferred now ship.** `list_backups` reads the backup directory back — archives newest first, each with name, timestamp and size — and `restore_backup` rewrites the data directory from one of them behind the confirm-token guard built in 4b. Twenty-six MCP tools now, nine of them housekeeping, two of them guarded. Design and the reasoning for reversing the deferral: `docs/superpowers/specs/2026-08-15-restore-backup-design.md`.
- **What the guard buys, stated where it cannot be missed**: a token forces a model to decide twice with a preview in between; it does not prove a human agreed, because a model can mint and redeem one inside a single turn. That limit is in the tool description, in the preview text the tool returns, and in the README. The browser's Backup page is still the path with a human actually in the loop. `restore_backup` uses the args-binding shutdown could not — the token carries the archive name, so a token minted for one archive is refused for another.
- **`backup.Service.List()` is the public archive listing** the 4b design named as the missing prerequisite (`listBackupTimes` was unexported and the Backup Status page globbed inline). Newest-first is contract, and `Archive.Name` is a bare filename by construction, since it is what restore takes. The Backup Status page's `snapshot_count` now counts through the same method, so the page and the tool cannot disagree about what an archive is — the old glob counted `budget_backup_NOT_A_DATE.zip`, the service does not.
- **Name resolution lives in the service, not the tool**: `restore.Service.FromArchive(ctx, name)` validates against `backup.ValidArchiveName` (prefix, `.zip` suffix, parseable timestamp, plus an explicit `filepath.Base` check kept as belt-and-braces) and then delegates to the existing `FromZip`. New sentinels `ErrBadArchiveName`, `ErrNoSuchArchive`, `ErrArchiveUnreadable` and `ErrNoBackupDir` — the last because an empty `BackupDir` would otherwise resolve a name against the process working directory.
- **A failed restore says whether data can have changed.** Everything is validated and the safety snapshot taken before the first write, so every failure except `ErrWriteFailed` leaves the data directory untouched; `ErrWriteFailed` reports that it may be partly rewritten and points at the snapshot. The redeem precedes the restore, so the token is spent either way and the error says so.
- **One restore service, not two**: `handlers/backup.Initialize` now returns the `*restore.Service` it builds and `cmd/server` hands that instance to the MCP server, so a tool-driven restore and a browser-driven one contend for the same snapshot hold and settings gate.
- **Tests** drive a real `mcp.Client` over the in-memory transport against a real archive from a real `Snapshot`: the preview call is asserted not to touch the data directory, and replay and name-mismatch are asserted not to restore, using a sentinel file a prune would have deleted. Mutation-checked — dropping the args binding, letting the preview restore, and dropping the newest-first reversal each fail their test.
- **Not fixed, and not new**: the phase-4a prune race (an MCP data-dir write taking neither the snapshot hold nor the settings gate can be pruned by a concurrent restore) is now reachable without the browser. It wants a design pass, not a patch.

### What-If Budget Analysis — Explain RMD-Driven Surplus (2026-05-08)
- **The confusion**: at late ages (e.g. year 29 / age ~97), the steady-state "Monthly Gap" panel can show a large green "+$59K Surplus" because the IRS-mandated RMD (~$92K/mo at age 97 with the small uniform-lifetime divisor, compounded over 29 years of tax-deferred growth, in nominal dollars) far exceeds the user's $22K/mo nominal expense need. Users (correctly) wondered whether that "surplus" disappears or whether the projection accounts for it being reinvested.
- **What the projection actually does** (already correct in `internal/services/retirement/calculator.go:791-806` `reinvestRequiredRMDToTaxableState` and the no-need branch at lines 859-865): the gross RMD is withdrawn from tax-deferred and taxed as ordinary income; the after-tax remainder is added to the taxable account with that net amount as cost basis (F-049 fix); the taxable account then continues generating dividends/cap gains and growing — so portfolio longevity in the projection chart already reflects the reinvestment. The surplus number was an accurate cash-flow snapshot, but the framing implied "discretionary cash" when the cash is actually mandatory and gets recycled.
- **UI fix in `web/templates/components/whatif/budget-analysis.html`**: when `SteadyStateGap < 0` AND `SteadyStateRMD > 0`, two explanations now appear: (1) a `title=` hover tooltip on the "Surplus" caption, and (2) a small italic note beneath the panel — "Surplus is driven by your mandatory RMD exceeding spending. After-tax excess is reinvested into your taxable account, extending portfolio life (already shown in the projection chart)." Conditional on both flags so non-RMD surpluses (e.g. a delayed pension overshooting expenses early in retirement) keep the existing minimal display.
- **Why two affordances instead of one**: the hover-only `title=` matches the codebase's prevailing tooltip pattern (`expense-sources-list.html`, `income-sources-list.html`, etc.) but is invisible on touch devices and easy to miss; the italic note guarantees the explanation reaches users who don't hover. Both render only in the RMD-driven case to avoid clutter for ordinary scenarios.
- **No calculator changes**: the underlying math was already right. This is pure UX clarification of an existing correct number.

### Internal-Transfer Filtering — User-Configurable + Default Broker Coverage (2026-05-04)
- **The bug behind the user report**: a $20K Schwab MoneyLink credit (`SCHWAB BROKERAGE MONEYLINK ***1115`, +$20,000.00, category `Investments`) was rendering as the #1 entry on the dashboard's "Top Spending" bar chart. Two compounding issues — (1) the classifier (`internal/services/classifier/classifier.go:88-104`) treats positive non-income amounts as `Outflow` because the description didn't match any `IncomeKeywords` and the category `"Investments"` isn't in `IncomeCategories`; (2) `buildMerchantsChartData` (`internal/handlers/dashboard/handlers.go:1250`) sums `math.Abs(t.Amount)` for every Outflow with no internal-transfer guard. Net effect: a brokerage-to-bank transfer (an inflow) became a $20K spending bar. The Major Expense donut hid it (the `total > 0` guard at `handlers.go:1064` skips negative net spend after the sign flip), but the merchants chart had no equivalent guard.
- **Fix shape**: filter the transaction at *load time* via `filterInternalTransfers` in `internal/services/dataloader/loader.go` so it never reaches any chart. Two new sources of patterns:
  - **Hardcoded broker patterns** added to `classifier.InternalTransferPatterns`: `schwab moneylink`, `schwab brokerage`, `fid bkg svc llc`, `vanguard buy investment`, `vanguard sell`, `etrade ach`, `e*trade ach`, `coinbase ach`, `coinbase inc.`, `robinhood ach`, `interactive brokers`. Distinctive enough that real merchant payments are unlikely to match. Fresh installs handle the major US brokers without configuration. New `TestIsInternalTransfer_BrokerPatterns` table-tests all eleven.
  - **User-configurable filters**: `MajorExpense` gained an `IsInternalTransfer bool` flag (`internal/models/major_expense.go`). When set, transactions matched by that expense's keyword/amount rules (via `majorexpenses.MatchTransaction`, the same matcher the Major Expenses page uses) are dropped at load time alongside the hardcoded patterns. Surface in the UI: a "Treat as internal transfer" checkbox on both the add form and per-row edit form in `web/templates/pages/major-expenses.html`, plus a sixth bullet in the existing "How matching works" disclosure. The new `parseFormBool` helper reads the checkbox; `parseExpenseForm` rejects pin-only entries with the flag set ("internal-transfer filter needs at least one keyword or an amount range to match against") since pin-only would be a no-op filter. `UpdateMajorExpense` copies the flag.
- **Why a flag on `MajorExpense` and not a separate file**: reuses the existing keyword-matching UI, list/edit/delete plumbing, and search. The "Ignore" entry pattern users were already creating manually now has actual filtering teeth. Cost: one bool field, ~15 lines of loader logic, three template edits.
- **Pipeline ordering note**: `filterInternalTransfers` runs before classification and before `applyMajorExpenseNames`, but it now reads `LoadMajorExpenses()` itself. `MatchTransaction` only needs `Description`/`DisplayName`/`abs(Amount)`, all of which are stable pre-classification. Major-expense load failure inside the filter is non-fatal — logs and falls back to the hardcoded list — so a corrupt `major_expenses.json` can't break CSV ingestion.
- **Test coverage**: `TestFilterInternalTransfers_UserFlaggedMajorExpense` proves a flagged expense drops its keyword match; `TestFilterInternalTransfers_NonFlaggedMajorExpenseDoesNotFilter` is the regression guard that catches accidentally over-eager filtering — a regular major expense (Groceries → Wegmans) must NOT be silently dropped from the dataset just because it's a major expense. `TestParseExpenseForm_IsInternalTransfer` covers the three form-parsing paths (checkbox on, missing, transfer flag without match rule). All passing; existing 54 test files stay green.

### Three Audit Fixes — Refund Sign, Income Stamping, Stale Hash (2026-05-03)
- **Refunds no longer inflate Major Expense totals**: the classifier intentionally keeps positive non-income amounts in the `Outflow` bucket so refunds/credits stay distinguishable from purchases by sign, but `internal/handlers/majorexpenses/handlers.go` and `internal/handlers/dashboard/handlers.go` were summing them with `t.AbsAmount()` — a $25 TARGET refund alongside a $50 TARGET purchase produced a $75 group total instead of $25. Per-group total, unmatched-bucket total, and the dashboard's pie-chart wedges all switched to `total += -t.Amount`, which treats negative outflows as positive spend and positive credits as a *reduction*. Net spend is now consistent across the Major Expenses page and the Dashboard donut. New regression `TestBuildPageData_PositiveCreditReducesGroupTotal` locks 50 - 25 = 25.
- **Income rows no longer get stamped with Major Expense names**: `applyMajorExpenseNames` in `internal/services/dataloader/major_expense_names.go` ran the keyword/pin matcher over *every* transaction, so an income row whose description happened to contain an expense keyword (e.g. "BOFA HOMELOANS REFUND") got `MajorExpenseName = "Mortgage"` stamped on it — and since `Transaction.Label()` prefers `MajorExpenseName` over `Description`, that row then displayed as "Mortgage" everywhere except the Major Expenses page itself (which already filters to outflows). Restricted the stamping pass to non-income transactions; new `TestApplyMajorExpenseNames_SkipsIncome` covers it.
- **Hashes recomputed after CC sign-flip**: `loadCSVFile` in `internal/services/dataloader/loader.go` was computing `t.Hash` *before* the post-load `usesCreditCardSignConvention` block flipped every amount, leaving the in-memory hash keyed on the pre-flip amount string. Two real consequences: (1) the same transaction imported from a bank-convention CSV (`-25`) and a CC-convention CSV (`+25`, then flipped to `-25`) hashed differently and survived `deduplicateTransactions`; (2) pins and Amazon enrichment were keyed on a value the rest of the app never used. Fix: recompute `Hash` inside the flip loop so dedup, pins, and enrichment all match the value the app actually shows. New `TestLoadCSVFile_HashReflectsPostFlipAmount` asserts `t.Hash == t.ComputeHash()` post-load.

### Social Security — Spousal Math Fixes & Income-List Visibility (2026-05-03)
- **Three calculation bugs in the spousal benefit pipeline fixed**, all in `internal/services/retirement/social_security.go`. The on-screen comparison table and the projection cash-flow both used the wrong formula for early spousal claims and silently dropped the spousal top-up entirely whenever the primary was "already claiming." User-found while auditing a real scenario (PIA $4,100, claim 67 with FRA 66, spouse claiming at 62): expected ~$1,332/mo from SSA's 32.5% rule, app was producing $1,050.
- **Bug 1 — wrong reduction formula**: `SpousalTopUp` called `AdjustedSSBenefit`, which implements the *worker's* own-benefit reduction (5/9 of 1% per month for the first 36 months early). SSA spousal benefits use a steeper rate: 25/36 of 1% per month for the first 36 months early, then 5/12 of 1% per month for additional months, with no delayed retirement credits past FRA. New `AdjustedSpousalBenefit(spousalPIA, spouseFRA, claimAge)` implements the SSA-correct formula and `SpousalTopUp` now calls it. At FRA 67 / claim 62 (60 months early): old code returned 70% × spousal PIA, new code returns the correct 65% — the difference between $1,400 and $1,300 on a $4,000 worker PIA, and the difference between matching SSA's published 32.5% rule or not.
- **Bug 2 — spousal top-up skipped when primary alreadyClaiming**: `projectedSocialSecurityIncome` had `if !alreadyClaiming && ss.FRABenefit > ss.SpouseFRABenefit { spouseBase = SpousalTopUp(...) }`. The `!alreadyClaiming` gate skipped top-up entirely whenever the *primary* (not the spouse) had already filed — exactly the case where SSA spousal eligibility is most relevant, since the worker must have filed for the spouse to claim. Removed the gate; spousal top-up now applies to whichever spouse hasn't yet claimed (locked-in benefits aren't recomputed).
- **Bug 3 — PIA-vs-actual-benefit mix-up**: when `alreadyClaiming`, the entered `FRABenefit` is the actual monthly check (UI label says "Your Monthly Benefit"), not the PIA. But the spousal top-up call passed it directly as `higherPIA`. For a primary who claimed at 67 with FRA 66 (+8% delayed credit), $4,100 actual benefit ↔ $3,796 true PIA — a 5% under/overstatement of every spousal calculation downstream. The existing `DerivedPIA(actualBenefit, fra, claimAge)` helper now back-derives the PIA whenever a spouse is alreadyClaiming, and the spousal top-up uses the derived PIA for both directions. `RunSSAnalysis` got the same treatment for the spouse-side comparison table.
- **Optimizer-derived SS now visible in the Income Sources list**: previously the SS Optimizer projected income flowed into the chart but never appeared as a line item, so users couldn't see what the projection was actually using. New `ProjectedSSEntries(*WhatIfSettings)` helper returns the primary + spouse SS rows with `MonthlyAmount`, `ClaimAge`, `StartMonth`, `AlreadyClaiming`, and `SpousalTopUp` flags. `projectedSocialSecurityIncome` was refactored to consume it, so the displayed list and the projection cash-flow can never drift from each other. Two new templates in `web/templates/components/whatif/income-sources-list.html`:
  - `whatif-projected-ss-item` — read-only indigo-accented row at the top of the income list. Shows "$X/mo" with "+ spousal" tag when top-up applies, plus "Already claiming at age N" or "Starts age N (yr X)" depending on AlreadyClaiming.
  - `whatif-income-source-item-excluded` — amber-accented strikethrough variant for manual sources matching the SS detection rule (e.g. "Christine SSI") when the optimizer is active. Tagged "excluded — handled by SS Optimizer" with a delete button to clean up the duplicate.
  Both rendered via two new template funcs in `internal/templates/render.go`: `projectedSSEntries` and `isSocialSecurityIncomeSource` (the existing detection rule, exported for template use). The OOB-swap copy in `web/templates/pages/whatif.html` got the same treatment so HTMX updates show consistent state.
- **Test coverage**: 7 new subtests in `TestAdjustedSpousalBenefit` (32.5% rule at age 62/FRA 67, FRA cap, no DRC, steeper-than-worker invariant, FRA 66 case, claim<62 clamping), 1 new subtest in `TestSpousalTopUp` (32.5% rule applied through top-up), 2 new regression subtests in `TestProjectedSocialSecurityIncome` ("spouse gets spousal top-up even when primary already claiming" and "spousal benefit applies SSA reduction not worker reduction"), and 6 new subtests in `TestProjectedSSEntries` (inactive returns nil, primary-only, alreadyClaiming uses entered actual, spousal-top-up flag, no-top-up when own higher, spouse omitted without claim age). The single existing `TestSpousalTopUp` "early claim reduces" subtest had been asserting on the wrong-formula value; updated to expect `AdjustedSpousalBenefit`. `ProjectedSSEntries` and `AdjustedSpousalBenefit` are at 100% line coverage; package total stays at 97.0%.
- **Net for the original audit case** (PIA $4,100 actual benefit / FRA 66 / claim 67 / spouse claiming at 62 with own PIA $1,500): app now projects $1,234/mo for the spouse instead of $1,050. Matches SSA's expected output once $4,100 is correctly interpreted as the actual benefit (true PIA $3,796) rather than as the PIA itself.

### Code-Review Polish — Backup Service & Major Expenses (2026-05-02)
- **`parseExpenseForm` no longer shadows Go 1.21 built-ins** in `internal/handlers/majorexpenses/handlers.go`: the local `min`/`max` parse-result variables were renamed to `expectedMin`/`expectedMax`. Functionally equivalent — the function had been working because Go scopes the shadows locally — but the linter flagged it and the new names also read better against the model fields they populate (`ExpectedMin` / `ExpectedMax`).
- **Silent create-and-pin failures are now logged**: in `handleAdd`, when the optional `pin_hash` is supplied and `loader.SetTransactionPin` fails, the response still embeds the existing `<!-- pin_hash %q ignored: %v -->` HTML comment for client-side debugging, but a `log.Printf("major-expenses: create-and-pin failed for hash=%q expense=%q: %v", …)` now also fires server-side so the operator notices in logs. The Add itself is still successful — pin failure does not roll back the create, matching the existing comment-documented behavior.
- **`backup.Service.mu` doc-comment expanded** in `internal/services/backup/service.go`: clarifies that the mutex serializes Snapshot writes plus the meta updates piggy-backed on each snapshot, that it's acquired non-blocking via `tryLock` so overlapping ticks degrade to a no-op rather than queuing, and that it must NOT be reused for unrelated state (the `enabled` flag has its own `RWMutex`). Future readers won't be tempted to grab the wrong lock.
- **`HandleBackupStatus` plaintext-meta read explained**: a comment now documents why `last_backup.json` is read directly with `os.ReadFile` instead of going through the storage layer — `BackupDir` defaults to an XDG path outside `DataDir`, so the storage layer's encryption never applies and the meta file is always plaintext. Avoids future "shouldn't this go through `store.ReadFile`?" confusion.
- **Verified during review (no code change needed)**:
  - `cmd/server/main.go` calls both `SnapshotIfStale` and `Run` with `24*time.Hour` (lines 240, 247) plus an unconditional `Snapshot()` on graceful shutdown — once-daily cadence with end-of-session capture is intentional.
  - The Major Expenses `major-expenses-results` partial uses `hx-swap-oob="innerHTML"` to refresh `#major-expenses-list-card` in the same response as the primary `#major-expenses-results` swap, so a pin/unpin from either card updates both atomically.
  - `DeleteMajorExpense` is now used only by two pre-archive tests in `internal/services/dataloader/major_expenses_test.go`; it's safe to remove once those tests migrate to `ArchiveMajorExpense`. Left in place for now per its existing deprecation notice.

### Amazon Order Enrichment — Bank Charges Now Show What You Actually Bought (2026-05-02)
- **Problem the existing UI couldn't solve**: every bank charge from Amazon arrives as `AMAZON.COM*ABC123`, `AMZN MKTP US`, `AMAZON MARKETPLACE`, etc. — the row in the Explorer told you the date and amount but never *what* the order was. Users with an "Amazon" major expense saw it bucketed correctly but the per-row label was just "Amazon", and a $133.32 line told you nothing about what shipped. The user pulled their Amazon order export to a local directory (2,221 shipments across `Order History.csv` retail + `Digital Content Orders.csv` digital, 88 grouped shipment-charges visible after deduping) and asked for descriptions to be augmented from that.
- **New per-transaction `EnrichedDescription` field on `models.Transaction`** sits between `MajorExpenseName` and `Description` in storage but **wins over `MajorExpenseName` in `Label()`** — per-transaction signals are strictly more specific than rule-based group names, and aggregation reads `MajorExpenseName` directly so grouping is unaffected by display ordering. Final precedence: `DisplayName → EnrichedDescription → MajorExpenseName → Description`. `FilterBySearch` was extended to match against `EnrichedDescription` too, so typing a product name (e.g. "rosemary") now finds the bank row that bought it.
- **New `internal/services/amazon` package** parses both Amazon CSVs into a single `Shipment` model. Retail rows are grouped by `(Order ID, Ship Date)` because a multi-item order ships in one or more shipments and *each shipment* corresponds to one bank charge — not one charge per item, not one charge per Order ID. Digital rows group by Order ID and skip rows where `Transaction Amount == 0` (gift-card / free items that never hit the user's bank). Header parsing is case-insensitive over a column-name index so future Amazon export schema reshuffles don't break it; amount parsing handles `$1,234.50`, `'-1.98'` (negative pricing-discount fields treated as 0), and `Not Applicable` literals.
- **Matcher (`amazon.Match`)** uses three strategies in order, with shipment consumption so multi-charge orders don't double-attribute: (1) exact-amount match within ±5 days of `Ship Date` (single-shipment, the common case); (2) sum across shipments of one Order ID matches the bank amount within window (split-shipment orders billed as one charge); (3) `MatchByDescription` for Order IDs embedded in the bank text — rare but unambiguous when present. Ambiguity is treated as a non-match: when two distinct shipments at the same amount fall in the same window, neither is matched (refusing to guess beats wrong attribution). Transactions without `amazon`/`amzn` in the description are not even considered. On the user's data, 176 of 204 Amazon-keyword transactions matched (86% coverage). Label format: `Amazon: <first product>` for single-product shipments, `Amazon: <first product> +N more` for multi-product/multi-shipment, with first-product truncated to 80 runes + ellipsis to keep table layouts intact.
- **Persistence model — `data/amazon_enrichment.json`**: a `map[txHash]label` written by the CLI and read by the dataloader. Lives next to `aliases.json` and `transaction_pins.json` so it inherits the same encryption-at-rest path. Re-running the CLI overwrites the file, so the workflow is idempotent: download a fresh Amazon export, rerun, replace. Tx hash is computed from `Date|Description|Amount` *before* any classification or dedupe, so the CLI and the running server produce identical hashes for the same row (verified: 176/176 hash matches between offline enrichment and live load).
- **Dataloader integration**: `applyAmazonEnrichment` is the third stamp pass in `LoadData()`, after `applyAliases` and `applyMajorExpenseNames`. Missing file is non-fatal (users without Amazon data see no behavior change). Storage's modtime-keyed `ReadFile` cache invalidates on file replacement, so a fresh enrichment run is picked up by the next request without restarting the server (after the binary itself has been rebuilt with this code).
- **CLI: `cmd/enrich-amazon`** — `--amazon-dir <path>` (required), `--data-dir` (default `./data`), `--window` (default 5 days), `--dry-run`, `--preview N`. Auto-unlocks encrypted storage via `BUDGET2_PASSWORD` env var or interactive TTY prompt (using `golang.org/x/term`); reports keyword-tx count, per-strategy match counts, coverage %, and sample labels before writing. Dry-run skips the write entirely. Single-pass — no incremental update model, intentionally; the cost of regenerating the full map from current inputs is trivial and the simpler model removes a whole class of "stale entry" bugs.
- **Initial precedence bug caught during verification**: first cut had `MajorExpenseName` ahead of `EnrichedDescription` in `Label()`. Looked correct in unit tests but failed against real data — every Amazon transaction had `MajorExpenseName = "Amazon"` from the user's existing major-expense rule, which shadowed all 176 enriched labels. Diagnosed by running an in-process load and printing each transaction's hash, description, and `Label()` — hashes lined up perfectly (176/176 matches), but `Label()` returned "Amazon" for every row. Fix was a one-line precedence swap. Lesson preserved in the field-precedence comment so the next person editing `Label()` knows why per-txn beats group.
- **Templates**: Explorer (`web/templates/pages/explorer.html:638`) and Major Expenses (`web/templates/pages/major-expenses.html`) already render `.Label`, so they pick up enrichment automatically. Insights still uses `.Description` directly in several spots and is unaffected by this change — separate cleanup, deferred.
- **Test coverage**: 19 new tests across model precedence (5 in `internal/models/transaction_test.go`), CSV parser (8 in `internal/services/amazon/order_test.go`: single item, multi-item shipment grouping, split-shipment by ship date, cancelled rows, digital zero-transaction skip, amount/time parsing, sort), matcher (10 in `matcher_test.go`: non-Amazon ignored, exact-amount, out-of-window, mismatch, ambiguity-skipped, multi-shipment sum, multi-product label, truncation, consumed-shipment-not-reused, MatchByDescription happy + already-matched paths), dataloader (`amazon_enrichment_test.go`: round-trip, missing file no-op, stamp + Label() integration, invalid JSON), and CLI (`cmd/enrich-amazon/main_test.go`: end-to-end with synthetic Amazon + bank CSVs in tempdir, dry-run no-write, isAmazonDesc).
- **Workflow**:
  ```bash
  BUDGET2_PASSWORD='…' go run ./cmd/enrich-amazon \
    --amazon-dir ~/path/to/amazon-export \
    --data-dir ./data --dry-run    # inspect first
  # drop --dry-run when the summary looks right; restart the server
  ```

### Backup — Encryption-Aware Manual Download, Break-Glass Plaintext Export, Open-in-File-Manager (2026-05-02)
- **Manual backup downloads now mirror on-disk state** instead of always producing plaintext: previously `HandleBackup` (`internal/handlers/backup/handlers.go`) opened every file via `store.OpenFile` (which decrypts) and the comment claimed "Backup files are always unencrypted for portability." The result was that a user with full at-rest encryption enabled would download a ZIP full of plaintext CSVs into `~/Downloads`, silently defeating the point of encryption (real-world report: "I downloaded the backup, it's not encrypted"). The handler now reads bytes verbatim with `os.Open`, matching the auto-snapshot path in `internal/services/backup/snapshot.go:164`. Encrypted store → ZIP entries are age-encrypted blobs; plain store → plaintext ZIP. Restore already validated this contract — encrypted entries are rejected unless the destination store is encrypted and unlocked, so manual + auto backups are now byte-identical and round-trip cleanly.
- **Skip rules aligned with auto-snapshot**: the manual download now also excludes the `cache/` subdirectory and `*.tmp` atomicWrite leftovers (in addition to the existing `.encrypted` / `.encryption-verify` markers). ZIP entry paths are now forward-slashed via `filepath.ToSlash` for cross-platform portability.
- **Break-glass plaintext export**: new `POST /backup/plaintext` handler (`HandleBackupPlaintext`) provides the old "always plaintext" behavior as a deliberate, friction-y opt-in. Refuses if storage isn't encrypted (use the regular Backup) or is locked. Password-method users must re-enter their encryption password (verified through `store.Unlock`); Age/SSH/YubiKey users must type the literal phrase `EXPORT` since there's no equivalent re-auth flow. Walks the data dir via `store.OpenFile` (decrypting on read) into a streamed ZIP. Logs `PLAINTEXT EXPORT initiated` and `PLAINTEXT EXPORT complete` with method, filename, file count, and total bytes for audit. UI: red "Plaintext Export" button next to Backup/Restore (rendered only when `IsEncrypted && !IsLocked`), opens a modal with a red warning panel and the appropriate confirmation field. Esc closes; submission downloads the resulting blob as `budget_plaintext_<timestamp>.zip`.
- **"Open in file manager" + click-to-copy on backup-dir path**: the Automatic Backups card's `Location:` line was previously unselectable text. The `<code>` element is now click-to-copy (uses `navigator.clipboard.writeText` with a text-selection fallback), and a small "Open" button next to it POSTs to the new `/backup/open-dir` endpoint, which spawns `xdg-open` / `open` / `explorer` based on `runtime.GOOS`. The handler reads the path exclusively from `cfg.BackupDir` (no client input) so it cannot be coerced into opening arbitrary directories. POST-only matches the existing `/backup/auto-enabled` pattern.
- **Stale README copy fixed**: README claimed "Backups are unencrypted: Downloaded backup ZIPs are plain files for portability" and listed "Backup downloads (for portability)" in the Not-Encrypted column. Both updated to reflect the new mirror-on-disk behavior plus the break-glass option. The 2026-05-01 automatic-backups spec is left as-is (historical record); the README and this CHANGELOG are now the canonical statement of current behavior.
- **Test coverage**: 6 new tests in `internal/handlers/backup/handlers_test.go`. `TestHandleBackup_EncryptedStorePreservesOnDiskBytes` is a regression test that fails if a future change re-introduces plaintext leakage from `/backup` (asserts zip bytes are byte-identical to on-disk and pass `storage.IsAgeEncryptedData`). `TestHandleBackup_SkipsCacheAndTmp` documents the new skip rules. `TestHandleBackupPlaintext_RejectsWhenNotEncrypted` / `…_RejectsBadPassword` / `…_RejectsMissingPassword` / `…_DecryptsWithCorrectPassword` cover the break-glass endpoint's gating and happy path. All 9 existing `HandleBackup*` tests still pass.

### Major Expenses — Exception-Row Drill-Down to Explorer (2026-05-02)
- **Each exception row's Description now links to the Explorer**: previously, exception rows in all three buckets (Unmatched, Anomalous, New Merchants) rendered the Description column as plain text — there was no path from an exception back to the underlying bank transaction. Users seeing a row like `Home support → ✓ Home support 📌` had no way to verify what produced it. The Description in every exception row now wraps the visible label in `<a href="/explorer?search={{urlquery $rawText}}&type=Outflow" onclick="event.stopPropagation()">…</a>`, mirroring the matched-row pattern at `web/templates/pages/major-expenses.html:752`. Click the description to land in the Explorer pre-filtered to that bank text; the row-level click handler that pre-fills the add form is unaffected (the anchor's `event.stopPropagation()` keeps both behaviors clean).
- **Four cells touched, one pattern**: identical anchor inserted into the legacy `UnknownLarge` cell (~line 954), the current `AllUnmatched` cell (~line 988), the `Anomalous` Description cell (~line 1036), and the `NewMerchants` cell (~line 1084). Each location's preceding date format (`.Transaction.Date.Format` vs `.Date.Format` vs `.FirstSeen.Format`) is what disambiguated the otherwise-identical cell strings during the edit.
- **Test coverage**: 7 new assertions across two existing render tests — `TestRenderMajorExpenses_WithEntriesAndExceptions` gains anchor-href assertions for `Random Big Purchase`, `My Landlord LLC`, and `Brand New Store` plus a `stopPropagation` count check; `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming` gains anchor-href assertions for `Big Unknown Charge` (over threshold) and `Tiny Coffee` (sub-threshold, dimmed) plus a count check. URL-encoded space asserted as `&#43;` per html/template's attribute-context escaping (matches the existing matched-row test convention).
- **Spec**: `docs/superpowers/specs/2026-05-02-exception-description-explorer-link-design.md`. **Plan**: `docs/superpowers/plans/2026-05-02-exception-description-explorer-link.md`.

### Major Expenses — Reconciliation, Soft-Delete & Create-and-Pin (2026-05-02)
- **The "matched vs actual" gap is no longer invisible**: the page implied that Total + "Unmatched over $100 (0)" accounted for everything, but `match.Unmatched` could still hold dozens of sub-threshold outflows that summed to hundreds of dollars (real-world example: Dashboard reported $6,351.57 for the last month while Major Expenses showed $5,136 — a $1,215.57 silent gap, all under the $100 floor). The list-card header now renders an amber badge `Unmatched: $X · N txns` whenever `len(match.Unmatched) > 0`. The badge is an `<a href="#major-expenses-bucket-unknown-large">` so a click jumps straight to the bucket. Tooltip: *"Outflows in window not matched to any major expense. Dashboard total = Declared + Unmatched."*
- **The Unmatched bucket now shows every unmatched outflow**, not just the ≥$100 subset: the existing "Unmatched over $100" `<details>` was renamed to "Unmatched (N)" and now iterates a new `AllUnmatched` slice (the full `match.Unmatched` list, sorted by `AbsAmount()` desc in `buildPageData`). Rows ≥ threshold render in red as before; rows under threshold render dimmed (`opacity-70` + gray amount) so the priority items still float visually but the long tail is finally surfaced and pinnable. Bucket sub-label clarifies: *"sorted by amount; rows under $100 are dimmed"*. The detail row keeps its existing id `major-expenses-bucket-unknown-large` so existing render tests and HTMX swap-state restoration still resolve. The legacy `UnknownLarge`-only render path is kept as a fallback for fixtures that don't pass `AllUnmatched`.
- **Pin picker pre-selects the existing pin**: the picker template used to hardcode `<option value="" disabled selected>Pin to…</option>` regardless of whether the row was already pinned, so on every page load you saw "Pin to…" even for rows that were already pinned. The picker now accepts `CurrentPin` from the dict at the call site and renders `selected` on the matching `<option>` (placeholder is selected only when `CurrentPin` is empty). All three call sites (UnknownLarge, Anomalous, NewMerchants) pass `(index $.PinMap .Hash)`. The new `PinMap` template key is the already-loaded `pins map[string]string` from `LoadTransactionPins()` — no extra disk read.
- **New-merchant rows now show match state**: the New Merchants list previously rendered every first-seen merchant identically with a "Pin to…" dropdown, even when the row was already keyword-matched to a major expense (just shown in the bucket because the merchant was new). Rows that resolve in the new `MatchedHashToExpenseID` map (a server-side inversion of `match.Groups`) now render an emerald checkmark + jump-link to the matched expense, with a 📌 badge if the match is via a pin. Truly-unmatched rows still render the dropdown so the user can pin them. The matched-state row is dimmed (`opacity-75`) since it's informational, not actionable.
- **"+ Create new from this…" sentinel option in the pin picker**: the previous flow to create a new major expense from an exception row required a click-row-to-prefill → submit-form dance, and the resulting expense matched only by *keyword* — there was no atomic "make a new expense AND pin this specific transaction to it" path. The picker now appends `<option value="__new__">+ Create new from this…</option>`. A `change` listener intercepts `__new__` (the form's `hx-trigger="change[target.value!='__new__']"` already gates HTMX), runs the existing prefill flow with the row's hash stamped into a hidden `pin_hash` input on `#major-expenses-add-form`, and the form submit creates the expense AND pins the originating transaction in one round-trip. `handleAdd` was extended to read the optional `pin_hash` form field and call `loader.SetTransactionPin(hash, me.ID)` after `AddMajorExpense` succeeds; pin failure logs a comment but doesn't roll back the create. The hidden input is cleared whenever the add panel is opened from the `[+]` toggle (blank-slate path), and an emerald hint *"Will also pin the originating transaction to this new expense."* appears under the form when `pin_hash` is set.
- **Soft-delete archive with restore**: `handleDelete` previously called `DeleteMajorExpense` followed by `PrunePinsForMissingExpenses`, which silently and permanently stripped every pin pointing at the deleted expense from `transaction_pins.json`. There was no record of what had been pinned, so transactions that the user had carefully categorized via pins (e.g., 8 specific Amazon rows pinned to "Amazon — Books") simply lost their categorization on delete. New `data/deleted_major_expenses.json` archive holds `{Expense, DeletedAt, PinnedHashes []string}` for every soft-deleted expense; `handleDelete` now calls a single `loader.ArchiveMajorExpense(id)` that captures the definition + pin hashes, removes the active entry, and detaches those specific pins. Write order is `archive → active list → pins` so a crash mid-operation leaves a recoverable duplicate rather than data loss. Restore (`POST /major-expenses/{id}/restore`) reverses the archive: the definition reappears in the active list, and each captured hash is re-pinned only if it isn't currently pinned to a different expense (no-clobber policy — if you pinned hash X to A, deleted A, pinned X to B, then restored A, X stays pinned to B). Discard (`DELETE /major-expenses/deleted/{id}`) is the no-undo permanent removal. Page-level "Deleted" panel renders below the active list when `len(.Deleted) > 0`, sorted most-recently-deleted first, with Restore (emerald) and Discard (red, `hx-confirm="Permanently discard X? This cannot be undone."`) per row.
- **Note on historical pins**: this change is prospective. Pins lost to deletes that happened before this commit are unrecoverable — the old `PrunePinsForMissingExpenses` had no audit trail.
- **Old `DeleteMajorExpense` deprecated, not removed**: the loader still exposes it (with a doc-comment deprecation notice pointing at `ArchiveMajorExpense`) for the few existing tests of pre-archive behavior. `PrunePinsForMissingExpenses` API is unchanged but no longer called by any handler — it remains as a defensive cleanup path.
- **Defensive nil-map handling in templates**: every new map passed via `buildPageData` (`PinMap`, `MatchedHashToExpenseID`, `ExpenseByID`) is guarded with `{{if $.MapName}}{{$x = index $.MapName .Key}}{{end}}` patterns so render-test fixtures that don't supply the new keys still render. The full production map is always passed; this is purely a robustness measure for older test fixtures.
- **Test coverage**: 9 new dataloader tests (`TestArchiveMajorExpense_CapturesDefinitionAndPins`, `TestArchiveMajorExpense_NoPins`, `TestArchiveMajorExpense_NotFound`, `TestRestoreMajorExpense_RestoresPinsAndDefinition`, `TestRestoreMajorExpense_DoesNotClobberCurrentPins`, `TestRestoreMajorExpense_NotFound`, `TestRestoreMajorExpense_RejectsDuplicateActiveID`, `TestDiscardDeletedMajorExpense_PermanentRemoval`, `TestDiscardDeletedMajorExpense_NotFound`); 6 new handler tests (`TestHandleDelete_ArchivesExpenseAndPins` replacing the old `TestHandleDelete_PrunesOrphanPins`, `TestHandleRestore_ReturnsExpenseAndPinsToActive`, `TestHandleDiscard_RemovesFromArchive`, `TestHandleAdd_WithPinHash_PinsImmediately`, `TestBuildPageData_UnmatchedTotalAndCount`, `TestBuildPageData_MatchedHashToExpenseID_Inverted`); 3 new template render tests (`TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming`, `TestRenderMajorExpenses_UnmatchedBadgeAndDeletedPanel`, `TestRenderMajorExpenses_PinPickerNewSentinelAndCurrent`).
- **Plan**: `~/.claude/plans/we-have-talked-about-dazzling-noodle.md`.

### Date-Range Filters — 1M and 2M Presets (2026-05-02)
- **`1M` and `2M` quick-range buttons added to every page that already had month-based presets**: dashboard, insights, explorer, and major-expenses. The new buttons sit immediately before `3M` so the chronological progression reads `YTD · 1M · 2M · 3M · 6M · 12M · All` on dashboard and `1M · 2M · 3M · 6M · 12M · All` on the other three. Each button reuses the adjacent `3M` button's class list verbatim so light/dark and active-state styling stay in lockstep with the existing presets.
- **Per-page wiring**: dashboard adds `case '1m'` / `'2m'` to `setPreset` in `web/static/js/dashboard.js`. Insights adds the same pair to `setInsightPreset` (template-inline) plus the matching `{{if eq .Preset "1m"}}` / `"2m"` Go-template ternaries so a direct `?preset=1m` URL load still server-renders the correct active-state highlight. Explorer and major-expenses extend their existing `setDateRange(months)` / `meSetDateRange(months)` helpers (which already accept any month count) and update their detect-selected-range iteration arrays from `[3, 6, 12]` to `[1, 2, 3, 6, 12]`.
- **Explorer detection hardening (drive-by)**: `detectSelectedDateRange` now calls `updateDateRangeButtons(-1)` both in the `endDate !== maxDate` early-return path and at end-of-function when no preset matched, bringing it in line with `meDetectSelectedDateRange` in major-expenses, which already had equivalent guards. Without this, a stepped date range that no longer aligned with any preset would leave the previously-selected button incorrectly highlighted.
- **Backend pass-through**: no Go changes — the `preset` query param on `/insights` is already an opaque pass-through used only for active-state highlighting; the handler reads it via `r.URL.Query().Get("preset")` and echoes it to the template with no allow-list check, so `?preset=1m` and `?preset=2m` round-trip cleanly without any handler edit.
- **Doc cleanup**: the `DefaultDateRange` comment in `internal/models/user_profile.go` updated from `// ytd, 3m, 6m, 12m, all` to include `1m` and `2m` (the field is currently dormant — no handler reads it — but the comment is the closest-thing to a canonical preset list and was the only stale doc-string in the tree).
- **Spec**: `docs/superpowers/specs/2026-05-01-date-filter-1m-2m-presets-design.md`. **Plan**: `docs/superpowers/plans/2026-05-01-date-filter-1m-2m-presets.md`.

### Major Expenses — Collapsible Table & Compact Add Affordance (2026-05-01)
- **"Your Major Expenses" list card is now a compact table instead of a stack of always-expanded forms**: with 10+ declared expenses the card had become a long scroll of identical-looking edit forms; users had to scan past every keyword/min/max field just to read names and totals. The card now renders as a `<table id="major-expenses-list">` with one `<tbody data-expense-id="..." data-open="false">` per expense — a summary row (chevron · Name · Matched · Total · ✕) plus a hidden detail row carrying the edit form and the matched-transactions table flat. One click on the chevron reveals everything for that expense; closed-by-default keeps the list scannable.
- **Header now surfaces a running total**: the card title row reads `Total: $X · N declared · [+]`, where the total is the sum of `Summaries[].Total` for the active date window. New `TotalDeclared` key on the page context (computed in `buildPageData` with a 4-line loop over the existing summaries — no new server-side data shapes). The header value carries `data-total-declared="{X.XX}"` so the render test asserts the precise printed value, not just attribute presence.
- **Add form moved behind a `[+]` icon**: the always-visible add form was eating ~40% of the card vertical space when not in use. It now sits inside a `<details id="major-expenses-add-panel">` with a screen-reader-only `<summary>`; the indigo `[+]` button in the header toggles `panel.open` and mirrors `aria-expanded`, focuses the Name input on open, and auto-collapses on successful submit via the existing `hx-on::after-request`. Empty-state copy updated to "No major expenses declared yet. Click the + above to add your first one."
- **Group-based unified search**: the search input that filtered the old per-row `<form>` elements now iterates `tbody[data-expense-id]` groups. Each group's summary row carries the same `data-search` attribute (name + keywords + notes); each matched-txn row carries its own. A query that matches the summary metadata leaves the row collapsed (the matching data is already visible); a query that matches a contained transaction force-opens the row so the matching txn is reachable without an extra click. Matches are counted as before in the status badge ("X expenses · Y exceptions").
- **HTMX swap state survives mutations**: `htmx:beforeSwap` now snapshots BOTH the open `<details id>` set AND the open `tbody[data-expense-id]` set; `htmx:afterSwap` restores both, then re-applies the active search. So editing a keyword on row 7 → list re-renders → row 7 stays open; changing the date range → all previously-open rows stay open if the expense still exists in the new window.
- **Jump-to-existing and exception-row prefill open their target panels first**: clicking an anomalous-exception expense link now opens the target row's `<tbody>` before scrolling-into-view + amber-ring highlight (previously the row was just a `<form>`, always visible). Clicking a Unmatched/NewMerchant row to prefill the add form now opens the add panel first — without this, focus would move to a hidden input. Both paths use the new `setRowOpen(tbody, open)` helper so `data-open` and the chevron's `aria-expanded` stay in lockstep.
- **Edit form keeps every field including Name**: rename was previously inline in the row; it's now in the detail-row form along with keywords, min, max, and notes. The summary row shows the name as plain text. Existing PUT handler validation is unchanged.
- **HTML validity improvement**: the old layout wrapped each `<tr>` in a `<form>`, which is invalid HTML5 (`<form>` is not a permitted child of `<tbody>`). The new layout puts the form inside the detail-row `<td colspan="5">` instead. The mutation routing (`#major-expenses-results` target + OOB swap of `#major-expenses-list-card`) is preserved.
- **CSS rule lives in the global stylesheet**: the row-collapse selector `#major-expenses-list tbody[data-open="false"] > tr.major-expense-detail-row { display: none; }` is in `web/static/css/styles.css`, not inline. An inline `<style>` would have been re-injected on every list-card OOB swap, accumulating duplicate rules and briefly flashing the detail row open between swap and style evaluation.
- **JS hardening**: the bulk-pin and exception-row prefill handlers now null-guard each `querySelector` result before reading `.value` or calling `.focus()`. Previously a partial OOB swap that left the filter form or add form without one of its named inputs would throw `TypeError` and abort the click handler, stranding focus.
- **Test coverage**: new `TestBuildPageData_TotalDeclared` (handler) verifies `TotalDeclared == sum(Summaries[].Total)` for two seeded outflows. Existing render tests (`TestRenderMajorExpenses_*`) updated to assert the new structure: `tbody[data-expense-id]`, `tr id="major-expense-item-{ID}"`, `tr id="major-expense-detail-{ID}"`, `form id="major-expense-edit-{ID}"`, chevron `aria-expanded`/`aria-controls`, `[+]` toggle id, add panel id, and `data-total-declared="4800.00"` precise value. Spec at `docs/superpowers/specs/2026-05-01-major-expenses-collapsible-table-design.md`.

### Major Expenses — Persist Disclosure Open State Across Swaps (2026-04-30)
- **Bucket disclosures no longer collapse on every pin**: each HTMX swap replaced the entire `<details>` element wholesale, so any bucket the user had opened (Unmatched / Anomalous / New merchants) closed itself the moment they pinned a transaction. Now a `htmx:beforeSwap` listener snapshots the IDs of every open `<details>` inside `#major-expenses-results` / `#major-expenses-list-card`, and `htmx:afterSwap` reapplies `open=true` to each one whose ID is back. Because we only set true and never false, the server-side auto-open-when-PinnedCount-greater-than-0 logic still wins for newly-pinned expenses.
- **Stable IDs added**: `major-expenses-bucket-unknown-large`, `major-expenses-bucket-anomalous`, `major-expenses-bucket-new-merchants`, and per-expense `major-expense-matched-{ID}`. The render test now asserts these so a future template edit can't silently regress to the closing-bucket behavior.

### Major Expenses — Pin-Only Targets (2026-04-30)
- **Allow expenses with no keywords AND no amount range**: previously `parseExpenseForm` rejected an expense with empty keywords and `Min/Max == 0`, even though the per-transaction-pinning feature was originally pitched around the "Amazon — Books" / "Amazon — Household" / "Amazon — Gifts" sub-bucket use case (no field on the transaction reliably distinguishes those, so users want to pin manually). The validation now accepts that configuration as the fourth matching mode and rejects only the genuinely-broken partial-range cases (only Min, or only Max, with no keyword). Error copy updated to reflect the new option.
- **"How matching works" help adds the Pin-only mode** as a fourth bullet, and the Keywords-input placeholder now reads `leave blank for amount-only or pin-only` instead of just `match by amount only`.
- **Test coverage**: new `TestHandleAdd_PinOnlyTargetAccepted` verifies the empty-keywords + zero-range path round-trips through the handler and lands as a stored expense; the existing `TestHandleAdd_ValidationErrors` table dropped the now-valid `empty keywords without amount range` case and added an `empty keywords with only max` case that still must reject (partial range).

### Major Expenses — Bulk Pin & Filter Persistence (2026-04-30)
- **Pin every visible exception in one click**: pinning transactions one-by-one re-rendered the panel after each `POST /major-expenses/pins`, blowing away the user's exception search filter and forcing them to retype it for the next pin. New `POST /major-expenses/pins/bulk` accepts `expense_id` + repeated `hashes` form values and writes the whole batch in one disk round-trip via the new `DataLoader.SetTransactionPins(map[string]string)`. A small amber toolbar appears under the search box when the filter narrows the visible set ("Pin all 7 matching → [select expense] [Apply]"), collects every visible row's `data-hash`, and fires `htmx.ajax('POST', '/major-expenses/pins/bulk')` so the existing OOB swap pipeline handles the refresh.
- **Search filter survives HTMX swaps**: a single `htmx:afterSwap` listener on `document.body` now reapplies the active exception filter after every swap (single pin, bulk pin, add, delete). Without this, the search input retained its value but every row reappeared because filter state lived only on per-row `style="display: none"` attributes that the swap replaced.
- **`data-hash` exposed on every exception row**: each `<tr.major-expenses-exception-row>` now carries `data-hash="{{.Transaction.Hash}}"` so the bulk-pin toolbar can collect visible hashes without parsing the embedded `<select>` form values. Required across all three exception buckets (UnknownLarge / Anomalous / NewMerchants).
- **Tailwind class-toggle correctness**: the toolbar uses `hidden` only as the static class and the JS toggles both `hidden` and `flex` together, sidestepping the `display` cascade ambiguity when both utilities sit on the same element.
- **Test coverage**: 2 new dataloader tests (`TestSetTransactionPins_BulkWrite` covering existing-pin preservation + empty-hash skip, `TestSetTransactionPins_RemovesAndDedupes` covering no-op and empty-expense removal) and 3 new handler tests (`TestHandleBulkPin_Success`, `TestHandleBulkPin_RejectsEmptyHashList`, `TestHandleBulkPin_RejectsUnknownExpense`).

### Major Expenses — Pin Picker UX (2026-04-30)
- **Disambiguated, sorted "Pin to…" dropdown**: the picker used to render bare `Name` per option, so 4 different "Home Improvement" entries (different merchants) collapsed to 4 identical-looking rows. New `ExpenseOption` struct + `buildExpenseOptions` helper sort options alphabetically (case-insensitive) and append ` — <first keyword>` only when a name collides with another entry, so unique names like `Cellphone` stay short while `Home Improvement — Lowe's` / `Home Improvement — The Home Depot` are distinguishable. Sorting also makes a freshly-added entry land in its alphabetical position rather than at the end of an unsorted list.
- **Native `<option title>` for clipped labels**: each option now carries `title="{{.Label}}"` so hovering reveals the full label as a browser tooltip when the popup width clips long merchant names.
- **`min-w-[14rem]` on the select**: the closed picker shows just `Pin to…` (small), but browsers size the open popup to the closed select's width on some platforms, clipping options. A 14rem floor keeps the popup readable without forcing a wide closed state.
- **Auto-open matched-transactions disclosure when pins exist**: each `ExpenseSummary` now carries `PinnedCount`. The summary line shows `· 📌 N pinned` next to `N matched`, and the per-entry `<details>` opens by default when `PinnedCount > 0` so the 📌 row + `unpin` button are visible without a click — addresses "I pinned an exception and now I can't find it."
- **Test coverage**: new `TestBuildExpenseOptions_SortsAndDisambiguates` pins the sort + dedup contract (case-insensitive collision counting, suffix only on collisions, alphabetical order). Existing render tests updated to carry the new `PinnedCount` field on the local `summary` test struct.

### Major Expenses — Cross-Page Surfacing (2026-04-30)
- **Explorer "Major Expense" column + filter**: the transaction list at `/explorer` now shows each row's matched major-expense name (indigo badge) in a new column between Category and Amount. A new "Major Expense" filter dropdown narrows the table to one expense's transactions and composes with the existing Category / Type / Date / Search filters. Same HTMX form, no extra round-trips. Both `handleExplorer` and `handleTransactionsPartial` apply the annotation via a new `annotateAndFilterByMajorExpense` helper that loads expenses + pins, runs `Match()`, builds a hash → name lookup, and optionally narrows the set when `?majorExpense=<id>` is passed. Empty-state colspan bumped from 6 to 7.
- **Insights badge on recurring payments**: each detected recurring payment in the Bills & Recurring table on `/insights` now shows a small indigo badge with its mapped major expense (if any) next to the description. New `RecurringPayment.MajorExpenseName` field; new `majorexpenses.AnnotateRecurringPayments` helper that honors pins on the first transaction and falls back to keyword/amount matching otherwise. Annotation is wired into both `calculateInsights` (full page) and `handleRecurringPartial` (HTMX refresh) via a small `annotateRecurringWithMajorExpense` wrapper that handles a nil loader gracefully so existing unit tests still run.
- **Dashboard "Spending by Major Expense" pie chart**: a new chart card on `/dashboard` next to the existing Category card. Renders user-declared expenses as slices of total outflow spending, with an "Unmatched" residual at the end so the totals add up to the period's total. New endpoint `GET /dashboard/charts/data/major-expense` and parallel builder `buildMajorExpenseChartData` that mirrors `buildCategoryChartData`'s pie-chart output shape — auto-discovered by the existing `[data-chart-url]` Plotly bootstrap.
- **Test coverage**: 6 new engine tests for `AnnotateRecurringPayments` (pin wins, fallback to keyword, empty / nil / missing-Transactions edge cases, orphan-pin fallback) and 2 new dashboard chart tests (empty-data short-circuit, all-unmatched residual = total).

### Major Expenses — Per-Transaction Pinning (2026-04-30)
- **Pin transactions to a specific expense regardless of keyword/amount**: a new `data/transaction_pins.json` (hash → expense ID) lets the user explicitly assign individual transactions to a major expense. Solves the "I have 60 Amazon orders, none of them have category-distinguishing descriptions" problem: create empty Amazon-Books / Amazon-Household / Amazon-Gifts entries (Name only, no keyword, no amount) and pin each Amazon row to the right one. Pins survive re-imports because they're keyed by the existing `Transaction.Hash` (date+description+amount).
- **Engine pin precedence**: `Match()` consults pins FIRST and only falls back to keyword/amount matching when a transaction has no pin or its pin points to a deleted expense (orphan pins are pruned automatically on `DeleteMajorExpense`). Pinned transactions are never flagged as anomalous since the user explicitly accepted them — anomaly detection skips `pinned[Hash]`.
- **UI**: each unmatched / new-merchant / anomalous row now has a "Pin to…" `<select>` dropdown listing every existing major expense; choosing one fires `POST /major-expenses/pins` with `{hash, expense_id}` and OOB-refreshes the list + exceptions panel. Click delegation ignores `select`/`option` clicks so the dropdown doesn't accidentally trigger row pre-fill. Anomalous rows keep both affordances: dropdown to "Move to existing", body click to "Move to new", expense-name link to "Edit existing".
- **Unpin from matched-transactions list**: each pinned transaction in an expense's "Show matched transactions" disclosure is prefixed with 📌 and gets a small "unpin" button that fires `DELETE /major-expenses/pins/{hash}`. Once unpinned the transaction either reverts to keyword/amount matching for that expense or returns to Unmatched.
- **Storage layer**: `LoadTransactionPins`, `SetTransactionPin`, `ClearTransactionPin`, and `PrunePinsForMissingExpenses` mirror the existing aliases.json pattern (`dl.store.ReadFile/WriteFile`, `os.IsNotExist` empty default, no mutex). `MatchOptions.Pins` and `MatchResult.PinnedHashes` thread the data through the engine.
- **Test coverage**: 11 storage tests (round-trip, overwrite, empty-hash rejection, prune-orphans), 4 engine tests (pin overrides keyword, fallback when target missing, suppresses anomaly, ignores empty hash), 4 handler tests (pin success / unknown-expense / empty-hash / unpin / delete-cascade-prunes-pins). Existing detectAnomalies tests updated to pass `nil` pinned map.

### Major Expenses — UX Refinements (2026-04-30)
- **Inline edit form was rounding to integers**: per-item Min/Max inputs used `%.0f` + `step="1"`, so editing any field on an existing entry overwrote stored cents. A def saved as `1580.43 / 1580.43` would silently become `1580 / 1580` on the next change event, breaking exact-amount matches against transactions like `$1580.43`. Now uses `%.2f` + `step="0.01"`. Add-form Min/Max inputs got the same treatment.
- **Anomalous rows are now click-to-reclassify**: previously a click jumped to the matching expense's edit form. Now the row click pre-fills the add form (so a `$626` check sitting in Lucid's anomalous bucket can be moved to a new "Hyundai" entry in one click). The expense-name link inside the row still scrolls to and highlights the existing entry — handled first in the document-level click handler so it doesn't fall through to the row-level pre-fill.
- **Smarter click-fill**: clicking a row used to always pre-fill `Min == Max == amount`, which silently trapped the user into AND-filter mode. Now the pre-fill is description-aware: check-like descriptions (matching `\bcheck\b` or pure digits) clear keywords and pin the exact amount; everything else pre-fills the keyword and leaves Min/Max blank, so the keyword catches every similar transaction by default. Users who want a tighter range can add it manually.
- **Per-entry matched-transactions disclosure**: each major-expense item now exposes a "Show matched transactions (N)" details element that lists every matched transaction (date, description, amount), most-recent first. New `Transactions` field on the per-expense `ExpenseSummary` carries the matched slice from handler to template.
- **How matching works help text rewrite**: the four matching modes (keyword only, exact amount only, keyword + exact amount AND filter, range) are now spelled out under the add form, including the user's "Lucid `check` / `1580.00` and Car `check` / `626.00` coexist" example.

### Major Expenses — New Page (2026-04-30)
- **New page at `/major-expenses`** for declaring expenses you understand (rent, mortgage, gym, fixed-amount checks, etc.) so the app can group similar transactions and call out exceptions. Persisted as `data/major_expenses.json` alongside `aliases.json`; first-run is intentionally blank — no auto-seed from recurring detection.
- **Three exception buckets** computed against the full imported transaction set: unmatched outflows over $100, matched-but-anomalous-amount (transactions in a declared expense's group whose amount falls outside its expected range), and new merchants whose normalized description was not seen before the trailing 30-day window. Anomaly check runs only against keyword-matched transactions; range-only and exact-amount-only matches are in-range by definition. New merchants are deduped to first occurrence and exclude income transactions.
- **Four matching modes** in one model: (1) keyword only — case-insensitive substring on description / display-name; first-def wins; range, if set, is anomaly-only. (2) Min == Max only — match transactions of that exact dollar value within ±$0.01 (useful when descriptions vary, like checks). (3) **Keyword + Min == Max — AND filter**: both must match. Lets users disambiguate transactions that share a generic keyword like "check" by pinning each definition to its own dollar amount (e.g., Lucid `check`/`1580.00` and Car `check`/`626.00` coexist). (4) Min < Max range without a keyword — match anything in the range. Validation requires either keywords OR a Min/Max pair.
- **Click-to-pre-fill from exception rows**: clicking a row in Unmatched or New Merchants seeds the add form with min/max set to the exact transaction amount (cents preserved) and focuses the Name field; check-like descriptions auto-clear the keyword field so the user falls back to amount-only matching. Anomalous rows scroll to and highlight the matching list entry on the left for in-place range adjustment.
- **Searchable exceptions panel**: client-side filter across all three buckets matches description, major-expense name (for anomalous rows), amount (`625` finds `$625.35`), and date (`2025-12` finds that month). Auto-expands buckets that contain matches and shows `X of Y` count. Survives HTMX swaps via document-level event delegation.
- **HTMX-driven CRUD** with OOB swap on every mutation so a single response refreshes both the list card and the exceptions panel; full-page `GET /major-expenses` and partial `GET /major-expenses/exceptions` for polling/refresh. Inline edit on each list entry with `change delay:500ms` debounced PUT. Add form sits at the top of the left card with collapsible "How matching works" help text.
- **Test coverage**: 29 engine tests (matching, exact-amount tolerance, anomaly detection, new-merchant cutoff/dedup/normalization, OR-with-keyword), 11 storage round-trip + timestamp tests, 10 handler tests (CRUD success + 5-case validation table including amount-only-accepted), and 3 template render tests asserting click-fill / jump / OOB / search plumbing. New `internal/services/majorexpenses/` engine package, new `internal/handlers/majorexpenses/` handler package, new `web/templates/pages/major-expenses.html`. Nav entry added between Insights and File Manager.

### What-If Planner — Bug Fixes (2026-04-30)
- **Failure Thresholds: hide Investment Return card in allocation mode**: When `InvestmentReturn == 0` (the sentinel that tells the projector to derive returns from per-account asset allocation), `findReturnThreshold` was running its binary search against the literal 0 and rendering a meaningless card with `Current: 0.0%`, `Fails if below: 0.0%`, `Safety margin: 0.0 pts`. The threshold now returns nil in allocation mode so the card is omitted; the inflation, expenses, and portfolio thresholds still render as before.
- **Success-rate color tiers: 5-tier gradient instead of 90/75 binary**: Monte Carlo and Historical Backtest cards used a two-threshold scheme that painted everything below 75% red — so a 72.6% rate (planner-marginal) looked identical to a 30% rate (failing plan). New tiers track common retirement-planning convention: ≥90 green, ≥80 lime, ≥70 yellow, ≥60 orange, <60 red. Logic moved to two new template helpers (`successRateTextClass`, `successRateBarClass`) so the same mapping is reused across both cards.
- **Monte Carlo risk-event labels: consistent "% of runs" framing**: The card displayed three different units under the same `runs` suffix — `1.6/run` (avg crashes per simulation), `909 runs` (count of runs with ≥1 spending shock), `784 runs` (count of runs with ≥1 health event). All three now lead with `% of runs` so the denominator is consistent with the success rate above. Market Crashes additionally shows the avg-count-per-run as a secondary line (`avg 1.5 each`) since the count is the more interesting datum when most simulations see at least one crash. `percentOf` template helper widened to accept any numeric type so int counts can be passed directly.
- **Historical Backtesting: failures-first table order, survival count, clearer Failed label**: The "View All N Sequences" table sorted by Final Balance descending, so users scrolled past the best survivors before reaching the failed sequences that explain the sub-100% success rate. The table now puts failures first (sorted by depletion year, quickest first), then survivors ascending — reading top-to-bottom matches the "Worst → Best" mental model and the failures called out in the chips above. Added a `X of Y survived` subtitle under the historical-success percentage so the headline answers "did they all survive?" at a glance. Renamed the per-row label from `Failed (1966)` to `Failed in 1966` (with a `title` tooltip clarifying it's the depletion year) since the parenthetical was being misread as the start year. New `SurvivedCount` field on `HistoricalBacktestAnalysis`.

### Testing — Coverage Expansion (2026-04-30)
- **whatif**: 84.6% → 98.6% (+14.0 pts). ~85 new error-path tests covering ParseForm failures (real `%ZZ` percent-encoding, since the existing multipart-no-boundary trick is silently accepted by modern Go), `Save()` failures (chmod-based), `Load()` failures, and `runAnalysisWithCache` failures (corrupt scenario chain — a `whatif_corrupt.json` file referenced from the active scenario's chain that parses on file-existence but fails on Load). 11 handlers went from 70-78% to 100%: RothConversion, GlidePath, Guardrails, SocialSecurity, ResetPhases, UpdateChain, DeleteChainLink, plus all Delete/Restore handlers.
- **retirement**: 94.6% → 97.2% (+2.6 pts). New tests for the four scenario error types (`ScenarioChainValidationError`, `ScenarioValidationError`, `ScenarioNotFoundError`, `ScenarioConflictError` — `Error()`/`Unwrap()` 0% → 100%), `findPreparedScenarioPerson` (0% → 100%), `ensurePrimaryPerson`/`ensureSpousePerson` (42-57% → 100%), `UpdateSettingsWithPersons` (0% → 90.9%), plus coverage for `normalizeStartDate`, `inferHealthcarePersonLink`, `decodeSettings`, `loadInternal`, and `removeSpousePersons`.
- **New test infrastructure** (whatif): `setupTestEnvWithDir` (returns settings dir for chmod tests), `makeSaveFail` (chmod 0o500 with cleanup), `setupItemsThenBreakChain` (corrupt JSON via dangling chain link), `badEncodedRequest` (real ParseForm failures).
- **Remaining ceilings** (intentional): storage 85.3% / backup 87.4% (YubiKey hardware paths), cmd/validate 86.5% (`main()` ceiling), whatif's last 1.4% (`getSettingsHash` json.Marshal-of-struct uncoverable, `handleListScenarios` filepath.Glob err unreachable from hardcoded pattern, `handleSwitchScenario` Stat-only path not breakable via chmod).

### What-If Planner — Hardening (2026-04-29)
- **Cost styling consistency**: Healthcare per-person, ACA, Medicare, total healthcare, and active expense amounts now render in red (`text-red-600 dark:text-red-400`) rather than neutral gray, matching the project rule that cost numbers are visually distinct from neutral and income figures.
- **Restore-path safety**: `RestoreIncomeSource`, `RestoreExpenseSource`, and `RestoreBigTicketItem` now reject restoring an ID that already exists in the active list (HTTP 409) and surface a 404 when the ID is not in the removed list, instead of silently no-oping or producing duplicate active entries from a hand-edited file.
- **Icon-button accessibility**: Decorative SVGs in the income, expense, big-ticket, and scenario-chain list cards now carry `aria-hidden="true"`, and the icon-only delete/restore buttons gained `aria-label` so screen readers announce a meaningful action.
- **`handleWhatIfSettings` refactor**: The 363-line super-handler is now 41 lines orchestrating a declarative `fieldSpec` table (`internal/handlers/whatif/form_spec.go`). Per-field parse, bounds, and enum validation live in the spec; cross-field invariants (`tax_deferred + roth ≤ 100`, `stock + cash ≤ 100`) and per-account allocation clamping run as discrete post-passes. All error messages preserved byte-for-byte; existing 22 `TestHandleWhatIfSettings_*` tests pass unchanged.
- **`handlers.go` file split**: The 2,797-line monolith was split by domain into `handlers.go` (shared helpers, page handler, sync), `handlers_income_expense.go`, `handlers_healthcare.go`, `handlers_scenarios.go`, and `handlers_rates.go`. Pure file move — no exported APIs or behavior changed; `RegisterRoutes` still wires every route from one place.

### Documentation
- **What-If hardening note**: Added `docs/what-if-hardening-2026-04-29.md` documenting the structural refactor, file split, restore-path fix, a11y pass, and three Phase-1 over-claims that were investigated and dismissed (Load() TOCTOU, DeleteScenario race, glide-path/guardrails clamping).
- **What-If retirement verification**: Added a verification note documenting the Current Plan inputs, live page values, calculator cross-check, and test command used to confirm the retirement numbers shown on the What-If page.

### What-If Planner — New Features
- **Social Security claiming age optimizer**: New analysis panel lets users enter their PIA (monthly benefit at FRA) and compare claiming ages 62–70 with adjusted monthly benefits, cumulative totals at ages 80/85/90 with COLA, and breakeven ages between adjacent options. Supports spouse comparison. Selected primary and spouse claim ages now feed directly into retirement projections, and manual Social Security-like income sources are excluded while optimizer-driven projection is active to prevent double counting.
- **Glide path (time-based allocation shift)**: Asset allocation can now shift linearly from a start stock% to an end stock% over a configurable number of years. Applied uniformly to all account types across deterministic, Monte Carlo, and historical backtest projections.
- **Spending guardrails**: Portfolio-performance-based spending rules that automatically cut spending when the portfolio drops from its peak (floor) and raise spending when it rises above baseline (ceiling), with configurable min/max caps. Works in deterministic projection, Monte Carlo, and backtesting. Replaces adaptive spending when enabled.

### What-If Planner
- **Canonical people and start month**: What-If settings now persist `start_date` plus a canonical `persons` list instead of saving scenario-level age fields. Working ages are derived in memory from each person's `birth_month`, keeping chained scenarios and healthcare projections aligned to one age baseline.
- **Linked healthcare people**: Healthcare rows can now link to a canonical person with automatic name and age derivation, while manual healthcare entries remain supported for one-off cases.
- **What-If settings UI**: Replaced the old primary/spouse age inputs with a projection start month, editable person rows, derived age previews, and spouse-aware phase-reference normalization in the What-If settings card.
- **IRMAA lookback correction**: Current and near-term what-if projections now respect IRMAA's 2-year MAGI lookback instead of charging Medicare surcharges immediately from the current modeled income year. Budget-fit now surfaces an explicit current IRMAA estimate from modeled MAGI, while steady-state IRMAA is derived from a two-year-earlier MAGI estimate so the budget card stays aligned with the projection engine.
- **IRMAA baseline consistency**: Rebased the Medicare surcharge table onto the planner's 2024 tax baseline and applied the same year-based IRMAA inflation logic across deterministic projections, Monte Carlo runs, and historical backtests so equivalent scenarios no longer disagree by engine.
- **Projection assumptions summary**: Added a compact assumptions panel to the portfolio longevity card covering after-tax cash flow, average-cost taxable sales, annual Roth conversion timing, and the active monthly cash-flow timing mode.
- **Reconciliation table readability**: Improved the year-by-year projection table with sticky headers, alternating row emphasis, tabular numeric alignment, clearer `Gross Cash In` / `Portfolio Out` terminology, and a more visually distinct real-balance sublabel.
- **Tax breakdown labeling**: Renamed the yearly projection tax columns to `Taxes Incl. NIIT` and `NIIT Portion` so the explainability table makes clear that NIIT is already included in the total tax number.
- **Template coverage**: Added render tests covering the new projection assumptions summary and the updated reconciliation table copy.

## v1.9.1 - Test Coverage Expansion

### Testing
- **config**: Added 18 tests — 100% coverage (DefaultConfig, Load, ensureDirectories, LoadUserSettings, SaveUserSettings)
- **classifier**: Added tests — 100% coverage (ClassifyTransactions, IsInternalTransfer, IsPotentialIncome, containsAny)
- **metrics**: Added tests — 100% coverage (CalculateMetrics, CalculateComparison, PercentChange)
- **templates**: Added tests — 100% coverage (all 21 helper functions + render infrastructure)
- **models**: Added tests for transactions, healthcare, whatif, income sources, user profile — 99.5% coverage (2 unreachable dead-code guards)
- **version**: Added 12 tests — 89.7% coverage (remaining is VCS metadata only available at build time)
- **dataloader**: Added 40+ tests — 99.6% coverage (remaining is unreachable json.MarshalIndent on map[string]string)
- **dashboard**: Added 60+ HTTP handler tests — 100% coverage (all chart types, KPIs, drilldowns, exports)
- **insights**: Added 62+ tests — 98.2% coverage (all 6 handlers + recurring detection, trends, velocity)
- **retirement**: Added coverage gap tests — 98.3% coverage (income calc, tax estimation, Roth conversion, withdrawals, big-ticket expenses, chain settings, budget fit, threshold analysis)
- **explorer**: Added 63 tests — 99.0% coverage (all handlers, sort, pagination, file ops, alias CRUD)
- **whatif**: Added 222 tests — 84.8% coverage (all settings, income/expense/healthcare CRUD, scenarios, chains, spending phases)
- **backup**: Added 55 tests — 82.0% coverage (backup/restore, encryption enable/disable, auth methods, key detection)
- **storage**: Added 126 tests — 72.3% coverage (all providers except YubiKey, encryption roundtrips, migrations, caching)
- **cmd/server**: Added 30 tests — 81.2% coverage (middleware, setup, version, kill previous instance)
- **cmd/validate**: Added tests — 86.5% coverage (endpoint validation, error paths, subprocess exit tests)
- **internal/http**: Added 11 tests — 100% coverage (render, error response, date range parsing)
- **Refactoring for testability**: extracted `run()` from `cmd/server/main()`, injected `exitFunc` in backup, injected `readBuildInfo` in version, removed 2 unreachable dead-code guards in models
- **retirement (SS/NIIT/IRMAA)**: Added 43 tests for new tax features — SS provisional income thresholds, NIIT with non-qualified dividends, IRMAA MFS tiers, buildProjectionExplainability 42%→100%
- Overall project coverage: 44.5% → 93.6%

## v1.9.0 - Projection Realism & Explainability

### Breaking Changes
- **Monthly compounding**: Inflation, COLA, and healthcare cost escalation now compound monthly instead of stair-stepping annually. Existing projections will shift slightly — expenses accrue more smoothly within each year rather than jumping at year boundaries.

### New Features
- **Tax-aware projections**: Projection loop now estimates federal + state taxes monthly using a YTD accumulator with iterative convergence. Withdrawals from tax-deferred accounts, RMDs, dividends, and capital gains distributions all generate realistic tax drag.
- **Long-term capital gains tax rates**: Qualified dividends and long-term capital gains are taxed at preferential LTCG rates (0%/15%/20%) instead of ordinary income rates.
- **Taxable account modeling**: New settings for dividend yield, qualified dividend share, and capital gains distribution rate. Taxable account withdrawals use average cost basis to determine realized gains.
- **Projection timing**: New setting controls whether portfolio growth is applied before (start of month), around (mid-month), or after (end of month) monthly cash flows.
- **Real-dollar tracking**: Projection months now include cumulative inflation and real (inflation-adjusted) portfolio balance, expenses, and income.
- **Projection explainability**: New year-by-year breakdown table showing starting balance, growth, income, taxes, expenses, withdrawals, and ending balance for each projection year.
- **Chart event annotations**: Projection chart now marks key life events (Social Security start, Medicare transitions, RMD start, scenario chain transitions) as diamond markers.
- **Real/nominal chart toggle**: Projection chart supports switching between nominal and inflation-adjusted (today's dollars) views.

### Bug Fixes
- **Budget analysis subtitle**: Corrected subtitle to clarify that steady-state values are in future nominal dollars, not today's dollars.
- **Qualified dividend migration**: Settings migration now checks for JSON field presence instead of zero-value sentinel, preventing silent override when user intentionally sets qualified share to 0%.
- **Dashboard monthly chart wrong totals**: Monthly Income vs Expenses chart used `MonthlyTotals()` which summed raw transaction amounts then applied `math.Abs` to the monthly total. Mixed-sign outflow transactions within a month cancelled out, showing ~$5,500 instead of ~$27,800. Switched to `GroupByMonth()` + `SumAbsAmount()` to match KPI calculations. Same fix applied to Spending Trend chart.
- **Inflation disclaimer**: Added helper text to Monthly Living Expenses slider and Retirement Spending Phases card clarifying values are in today's dollars and inflation is applied during projection.

## v1.8.1 - Code Review Fixes

### Bug Fixes
- **Cumulative balance chart wrong**: Chart used raw `Amount` sign to determine income vs expense, but some bank CSVs export outflows as positive numbers. Now uses `TransactionType` field to determine direction, so outflows are always subtracted regardless of amount sign
- **Backtest depletion sort**: `yearsUntilDepletion` returned raw calendar year instead of 0 when `DepletionYear < StartYear`, causing wildly wrong sort ordering in edge cases
- **URL parameter double-append**: Chart loading in `charts.js` and `dashboard.js` used string concatenation (`url + '?' + params`) which produced malformed URLs when the base URL already contained a query string; now uses `URL`/`URLSearchParams` for safe merging
- **Chart fetch error handling**: `loadChart` and `refreshCharts` now check `response.ok` before parsing JSON, preventing silent failures that left "Loading chart..." displayed forever
- **Explorer redirect loop**: Filter persistence restore could loop infinitely if saved params were invalid; added a one-shot guard via sessionStorage
- **Dead `min()` function**: Removed user-defined `min(a, b int)` in `backtest.go` that shadowed the Go builtin

### Security Hardening
- **Alias input validation**: `handleAlias` now validates hash length (max 128) and display name length (max 200) to prevent storage bloat from malicious input
- **File delete path traversal**: `handleFileDelete` now reuses `sanitizeUploadFilename` instead of duplicated inline path-traversal checks

### Accessibility
- Projection chart toggle group now has `aria-label` and buttons track `aria-pressed` state
- Explorer step-back/forward buttons now have `aria-label` for screen reader users

### Code Quality
- Removed dead code: `portfolioBalances`, `monthlyAccountReturns`, `applyPortfolioGrowth`, `executePortfolioCashFlow`, `reinvestRequiredRMD`, `unrealizedGains` — superseded by tax-aware equivalents
- Renamed `mergeSimlarGroups` → `mergeSimilarGroups` (typo fix)
- Fixed import grouping in `render.go` (stdlib before third-party)
- Added trailing newlines to `expense-sources-list.html` and `income-sources-list.html`
- Alias endpoint returns `204 No Content` instead of empty `200 OK`

## v1.8.0 - Dashboard Redesign

### Changes

#### Dashboard
- Removed spending alerts panel (noisy, not actionable)
- Removed daily cash flow chart (too noisy) and weekly spending pattern chart (low value)
- Added **Spending Trend** chart: month-over-month spending change as percentage bars (green = decreased, red = increased) with hover showing actual dollar amounts for current and prior month
- Cumulative Balance chart now spans full width for better readability
- Charts now use direct fetch() for reliable updates on date range and preset changes (replaced fragile HTMX hx-swap="none" pattern)
- Removed `SpendingAlert` model type and `/dashboard/alerts` endpoint

#### Bug Fixes
- **Insights recurring freshness**: Active recurring payments now stay visible relative to the selected dataset end date instead of disappearing when imported data is old relative to wall-clock time
- **Insights historical windows**: Recurring detection now ignores transactions after the selected end date, so past views do not leak in subscriptions that started later
- **Historical backtest worst-year ranking**: Failed historical sequences are now ranked by years until depletion relative to their own start year instead of by absolute calendar year
- **Make fuzz target**: `make fuzz` now runs valid per-package fuzz commands and exits cleanly with guidance when the repo has no `Fuzz*` tests yet

## v1.7.0 - Scenario Chaining & Bug Fixes

### New Features

#### Scenario Chaining
- Chain multiple retirement scenarios to run sequentially with different assumptions at each life phase
- Example: run "Early Retirement" plan from age 60-70, then switch to "Post Social Security" plan from 70 onward
- Portfolio balances carry over between phases — only assumptions change (expenses, income, allocation, healthcare)
- Configure chains in the new "Scenario Chain" card on the What-If page: pick a scenario and a transition age
- Unlimited chain links with ascending transition ages
- Chain-aware across all analysis outputs: projection chart, Monte Carlo, historical backtest, sensitivity, and failure-point analysis
- Budget-fit, present-value, and RMD panels show a note when chain is active (chain support coming in a future release)
- Referential integrity: scenarios referenced in a chain cannot be deleted
- Chain validation on every save: if changing your current age invalidates a chain, it is automatically removed

#### Spending Phase Dollar Amounts
- Spending phase sliders now show both percentage and equivalent monthly dollar amount (e.g., "70% $6,440/mo")
- Updates live as the slider moves

### Bug Fixes
- **Projection depletion during tax-deferred delay**: A temporary shortfall where accessible accounts couldn't cover expenses but locked accounts still had funds was incorrectly treated as permanent depletion, stopping the projection early. Now only true depletion (total balance <= 0) stops the projection.
- **Invalid allocation blocking settings**: If per-account stocks + cash exceeded 100%, all settings changes were rejected with a 400 error. Now values are clamped automatically instead of blocking.
- **Dashboard date filter**: KPIs and alerts now update when the date range is changed
- **Insights date filter**: Date inputs now trigger page refresh on change
- **Recurring payment detection**: Recurring payments are now detected from full transaction history, so short date ranges no longer show $0

## v1.6.0 - Explorer Enhancements

### New Features

#### Transaction Renaming
- Double-click any transaction description to assign a custom display name
- Useful for cryptic entries like "Check #996574" - rename to "Plumber repair"
- Aliases stored in `aliases.json`, encrypted when encryption is enabled
- Original description shown in parentheses next to the alias
- Search matches both original description and custom name
- Clear the name to revert to the original description

#### Date Range Stepping
- Back/forward arrow buttons next to the 3M/6M/12M/All quick-range buttons
- Steps the entire date window forward or backward by its current duration
- Clamped to the min/max bounds of your data

#### Filter Persistence
- Explorer filter state (dates, search, category, sort) persists across tab changes
- Uses sessionStorage so settings survive navigation to other pages and back
- "Clear Filters" resets both the filters and the saved state

## v1.5.0 - Scenarios, Subscriptions & Budget Transparency

### New Features

#### What-If Scenarios
- Named scenarios let you explore different retirement plans without losing your current setup
- Create "Job Loss", "Early Retirement", etc. - each starts as a copy of the active plan
- Switch between scenarios instantly via dropdown at the top of the What-If page
- Rename or delete non-default scenarios; "Current Plan" is always preserved

#### Subscription Tracking (Insights)
- New dedicated Subscriptions section on the Insights page
- Automatically classifies recurring payments as subscriptions vs bills/retail
- Shows monthly subscription total with per-service breakdown
- KPI card added showing subscription cost at a glance

#### Monthly Budget Snapshot (What-If)
- Budget analysis now shows itemized expense and income breakdowns
- Each expense source listed with amount and notes (e.g., "ends year 3", "employer covered")
- Each income source listed with amount and start year
- Net cash flow summary at bottom

#### Healthcare Coverage Type Editing
- Coverage type (Employer/ACA/Medicare) is now a dropdown you can change directly
- Previously was a static label requiring removal and re-adding the person

### Improvements

- Recurring payment detection now uses fuzzy vendor matching — transactions with similar names (e.g., "Lucid" and "Lucidmotors.com") are merged into a single vendor group, so payments aren't missed due to inconsistent bank descriptions
- Amount-based recurring payment detection — payments with identical amounts at regular intervals are detected even when descriptions differ (e.g., check payments and direct bill pay to the same vendor)
- Go version updated to 1.26
- Income source number inputs now update results as you type (not just on blur)
- CSV upload merges new rows into existing files instead of overwriting (prevents data loss)

---

## v1.4.0 - Bug Fixes & Spouse Age Tracking

### Bug Fixes

#### Fixed Monthly Return Calculation
- **Critical fix**: Corrected compound interest calculation for monthly portfolio returns
- Old calculation used simple division (annual/12) instead of geometric conversion
- This was inflating projected returns by ~1.4%/year, compounding to 60% higher final balances
- Now uses correct formula: `monthly = (1 + annual)^(1/12) - 1`

#### RMD Calculations for Couples
- RMD calculations now correctly use the older spouse's age for joint accounts
- Fixed template type mismatch in healthcare timeline comparison

#### Per-Account Allocation Improvements
- Fixed issue where explicit 0% stock allocations were ignored
- Sensitivity analysis now uses effective return rate when in allocation mode
- Asset allocation changes now properly update projection charts

### New Features

#### Spouse Age Tracking
- Added spouse age tracking for retirement spending phases
- Enables more accurate RMD and healthcare cost projections for couples

### UI Improvements
- Show dollar amounts next to account types in asset allocation section
- Moved Income Sources card higher in what-if sidebar for better visibility
- Improved chart loading when settings change

---

## v1.3.0 - Per-Account Asset Allocation

### New Features

#### Per-Account Asset Allocation
- **Independent allocation per account type**: Tax-Deferred, Roth, and Taxable accounts can each have different stock/bond/cash allocations
- Example: Conservative 60/40 for Tax-Deferred, aggressive 90/10 for Taxable brokerage account
- Returns derived from historical means (~10.5% stocks, ~5.2% bonds, ~3.5% cash)
- Auto-rebalancing maintains target allocation each year
- Investment Return slider now acts as override (if set, applies flat rate to all accounts)

#### Enhanced Monte Carlo
- Monte Carlo simulation now uses per-account allocation
- Generates separate stock/bond/cash return sequences, then blends per account
- More realistic modeling of diversified portfolios with different risk profiles

### UI Improvements
- New "Asset Allocation by Account" section replaces single global allocation
- Each account shows Stocks/Cash inputs with calculated Bonds display
- Bond percentage updates dynamically as you adjust

---

## v1.2.0 - Enhanced Backtesting & Asset Allocation

### New Features

#### Configurable Asset Allocation
- **Stock/Bond/Cash allocation**: Set your own portfolio mix instead of fixed 60/40
- Stocks use S&P 500 historical returns
- Bonds use 10-year Treasury yields
- Cash uses 3-month T-bill rates
- Bond percentage computed automatically (100% - stocks - cash)

#### Historical T-Bill Returns
- Added 97 years of cash/money market returns (1928-2024)
- Source: NYU Stern/Damodaran historical returns database
- Enables accurate modeling of conservative portfolios

#### Inflation-Adjusted Results
- Backtesting now shows both nominal and real (inflation-adjusted) balances
- Real balance represents purchasing power in start-year dollars
- Cumulative inflation tracked throughout each simulation

#### Monte Carlo Asset Allocation
- Monte Carlo simulation now uses your stock/bond/cash allocation
- Separate return generation for each asset class with realistic volatility
- Stocks: ~11.7% mean, 19% standard deviation
- Bonds: ~5% mean, 8% standard deviation
- Cash: ~3.3% mean (low volatility)
- Flight-to-safety behavior: bonds rally during stock crashes

### UI Improvements
- New Asset Allocation section in Rate Assumptions card
- "Final (Real)" column in historical backtest results table
- Explanatory notes about inflation-adjusted values

---

## v1.1.0 - Multi-Account Tax Support

### New Features

#### 3-Bucket Portfolio Model
- **Roth Account Support**: Portfolio now tracks Tax-Deferred, Roth, and Taxable accounts separately
- Configurable allocation percentages for each account type
- Tax-efficient withdrawal ordering: RMD → Taxable → Roth → Tax-Deferred

#### Tax Bracket Modeling
- Full 2024 federal tax brackets for all filing statuses (Single, Married Filing Jointly, etc.)
- Inflation-adjusted brackets for multi-year projections
- State income tax rate configuration
- Marginal and effective tax rate calculations

#### Roth Conversion Strategy
- Model annual Roth conversions with configurable amounts
- Set start/end years for conversion window
- Conversions automatically move funds from Tax-Deferred to Roth bucket
- Track tax impact of conversions

#### Big Ticket Items
- Add one-time financial events (inheritance, home sale, large purchase)
- Configure as income or expense with year of occurrence
- Tax treatment options: None, Ordinary Income, Capital Gains
- Integrated into both standard projection and Monte Carlo simulation

#### Historical Backtesting
- Test your retirement plan against 97 years of actual market history (1928-2024)
- Uses real S&P 500 returns, bond yields, and inflation rates
- Identifies worst starting years (1929, 1966, 1973, 2000, 2008)
- Compare historical success rate vs Monte Carlo simulation
- Expandable table showing all historical sequences

### Technical Details
- New `tax.go` with TaxCalculator for federal/state tax calculations
- New `historical_data.go` with embedded market data
- New `backtest.go` for historical sequence testing
- Added comprehensive test coverage for tax and backtesting

---

## v1.0.0 - Initial Release

SimpleBudget is a local-first personal finance dashboard and retirement planning tool. All data stays on your computer - no cloud, no accounts, complete privacy.

### Features

#### Dashboard
- Real-time KPI tracking: income, expenses, net savings, savings rate with sparklines
- Interactive charts: monthly income vs expenses, spending by category, spending trend, top spending, cumulative balance
- Category breakdown with drill-down analysis
- Month-over-month spending trend with hover details (current/prior amounts, percentage change)
- CSV export of financial metrics

#### Data Explorer
- Search and filter transactions by date, category, amount, description
- Multi-file CSV import with automatic deduplication
- Flexible parsing handles most bank export formats
- File management with toggle inclusion and deletion

#### What-If Retirement Planner
- 30-year portfolio projections with Monte Carlo simulation (1000 runs)
- Sustainability scoring (0-100) with success probability
- Multiple income sources with COLA adjustments
- Multiple expense categories with inflation tracking
- Healthcare cost modeling for multiple household members
- RMD (Required Minimum Distribution) calculations
- Sequence risk and failure point analysis
- Go-Go/Slow-Go/No-Go spending phase modeling
- Auto-sync income patterns from transaction data

#### Insights
- Automatic recurring payment detection (subscriptions, bills)
- Income pattern analysis (weekly, biweekly, monthly)
- Category spending trends
- Spending velocity tracking

#### Security & Encryption
- **Password** - Simple scrypt-based encryption
- **Age Identity** - X25519 key pair for advanced users
- **SSH Key** - Use existing ed25519 or RSA keys
- **YubiKey** - Hardware security key support

All transaction files and settings are encrypted at rest.

#### Backup & Restore
- Create downloadable ZIP backups
- Restore from previous backups
- Test data for learning the interface

### Technical Details

- Single binary with embedded web assets
- No external database required (CSV/JSON file storage)
- Built with Go, HTMX, Tailwind CSS, Plotly.js
- Cross-platform: Linux, macOS (Intel & Apple Silicon), Windows

### Supported Platforms

| Platform | Architecture | Download |
|----------|--------------|----------|
| Linux | x64 | `budget2_1.0.0_linux_amd64.tar.gz` |
| Linux | ARM64 | `budget2_1.0.0_linux_arm64.tar.gz` |
| macOS | Intel | `budget2_1.0.0_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `budget2_1.0.0_darwin_arm64.tar.gz` |
| Windows | x64 | `budget2_1.0.0_windows_amd64.zip` |

### Getting Started

1. Download the appropriate archive for your platform
2. Extract and run `budget2` (or `budget2.exe` on Windows)
3. Open http://localhost:8080 in your browser
4. Import your bank's CSV transaction export

### Privacy

- 100% local - no data leaves your computer
- No telemetry, analytics, or external connections
- Standard file formats (CSV/JSON) - no vendor lock-in
