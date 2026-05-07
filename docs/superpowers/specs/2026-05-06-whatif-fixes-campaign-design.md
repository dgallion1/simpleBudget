# What-If Fix Campaign — Design

**Date:** 2026-05-06
**Author:** Claude (campaign design)
**Status:** Draft, awaiting review
**Predecessor:** `docs/whatif-math-audit-2026-05-05.md` (71 findings, F-001…F-071)

## Goal

Resolve audit findings that affect what users see in the projection,
without breaking anything that already works. The user's stated objective
is "improve projections so the user isn't confused" — the campaign is
re-scoped (2026-05-06) around user-visible accuracy and UI clarity, with
internal-only test-hardening (the 45 LOWs) deferred.

## Non-goals

- No re-architecture. Fix the bug; don't redesign the function.
- No new features. The audit found math issues, not feature gaps.
- No bumping past TY2025 unless TY2026 tables have published. The constants
  bump is a discrete decision, gated separately.
- No changes outside `internal/services/retirement/`, `internal/handlers/whatif/`,
  `internal/models/whatif.go`, and the relevant `web/templates/components/whatif/*.html`
  files. UI/copy fixes touch templates only.

## Scope — re-prioritized for user-visible impact

The 71 audit findings split by what the user notices:

| Tier | Findings | Treatment |
|------|----------|-----------|
| **P1 — User-visible math bugs** | F-001, F-018, F-026, F-029, F-049, F-057, F-065 | Formula fixes via TDD, one PR each (7 PRs) |
| **P2 — UI clarity / labels / docs** | F-063, F-070 | Rename / refresh, one PR each (2 PRs) |
| **P3 — Config gaps** | F-032, F-035, F-067 | One PR bundling year-boundary timing fixes |
| **P4 — Constants currency** | (TY2024 → TY2025) | Deferred decision; PR prepared not merged |
| **P5 — Test hardening (45 LOWs)** | F-007–F-017 (mostly), F-040, F-042–F-048, etc. | **Dropped from this campaign.** Internal regression guards; do not affect what the user sees. Revisit only if a future regression shows the gap was load-bearing. |

The other MEDIUMs that aren't in P1 (F-011, F-019, F-032, F-033, F-035,
F-036, F-062) are either subsumed into P3 (F-032, F-035) or were classified
MEDIUM because they're substantive test gaps but not output errors —
treated as P5 for this campaign.

### Locked decisions on ambiguous findings

- **F-035 (RMD timing).** Implement option (b) from the explanation: make
  RMD timing configurable (`start_of_year` / `mid_year` / `end_of_year`),
  default to `mid_year`. This removes the ambiguity that confuses users
  comparing this tool's output to other planners. Settings field added,
  surfaced in the UI as a small dropdown.
- **F-063 (guardrails mislabel).** Implement option (a): rename the UI
  label and tooltip from "Guyton-Klinger guardrails" to "Drop/rise
  guardrails (simple)" with a one-line tooltip explaining the trigger.
  Math unchanged. A separate follow-up ticket (`whatif-full-gk`) is filed
  for implementing actual G-K rules later if usage data justifies the
  larger work.

## Approach

### Batch by area, not by finding

The audit organized findings into 10 math areas. The fix campaign mirrors
that structure: one PR per area, bundling all formula fixes and test-gap
closures within that area. Plus two cross-cutting PRs (constants currency,
UI copy / doc cleanup).

**Rationale.** A per-finding PR strategy would produce 60+ PRs, most of which
share file context. Per-area batching:

- Lets each PR's tests share fixtures and helpers.
- Reduces review fatigue: one reviewer sees all the changes to `tax.go` at
  once, with the audit's findings as the requirements.
- Makes regression testing cheaper — each area's full test suite runs once
  per PR.
- Keeps PRs small enough to review (~5-10 findings each, ~200-500 lines).

### TDD per finding

Each finding becomes one or more failing tests, then a fix to make them pass.

For formula bugs (F-001, F-049, F-065, etc.):
1. Write a test that asserts the *correct* value (per the finding's
   "Recommended fix sketch" + worked example).
2. Run; confirm RED. The current code produces the wrong value.
3. Apply the fix.
4. Run; confirm GREEN. The new test passes; existing tests still pass.

For test-gap LOWs (most of F-007 through F-017, F-040, F-042-44, F-045-48,
F-050-52, F-054-56, F-061, F-064, F-066-69, F-071):
1. Write the test that exercises the missed boundary.
2. Run; if it passes, that's confirmation the formula is correct under that
   boundary — keep the test as a regression guard. If it fails, that's a
   newly discovered bug — promote to formula-fix flow above and update the
   audit doc to reflect the upgrade.
3. Commit the test.

### One PR per finding (P1 + P2), one bundle PR for P3

Ten PRs, in priority order (highest user-visible impact first):

| # | PR | Finding(s) | What changes for the user |
|---|----|-----------|---------------------------|
| 1 | `fix-f001-age65-deduction` | F-001 | Tax line drops by ~$429/yr for retirees ≥65 (Single). |
| 2 | `fix-f065-chain-rebase` | F-065 | 30-year chain scenarios with `SpendingDeclineRate > 0` show ~$179K lower late-life expenses. |
| 3 | `fix-f049-rmd-reinvest-basis` | F-049 | Long-term LTCG correctly reflects after-tax RMD basis. ~$375 per $10K reinvested at 22% marginal. |
| 4 | `fix-f057-backtest-window` | F-057 | Historical backtest shows 68 sequences instead of 67; 1995–2024 window now included. |
| 5 | `fix-f026-zero-cola` | F-026 | Setting SS COLA to 0% honors the input (was silently substituting 2%). |
| 6 | `fix-f029-spousal-display` | F-029 | Spousal benefit display flag and dollar amount correct for already-claiming primary. |
| 7 | `fix-f018-mfs-taxable-ss` | F-018 | MFS filers get correct § 86 thresholds ($0 lived-with-spouse; Single thresholds lived-apart). |
| 8 | `fix-f063-guardrails-label` | F-063 | UI says "Drop/rise guardrails (simple)" with honest tooltip. Math unchanged. |
| 9 | `fix-f070-verification-doc` | F-070 | `docs/what-if-retirement-verification.md` reference numbers refreshed for current code. |
| 10 | `fix-config-gaps-year-boundary` | F-032, F-035, F-067 | (a) RMD start age handles SECURE 2.0 age-75 bump for projections crossing 2033. (b) RMD timing configurable. (c) Healthcare ACA→Medicare transition handled month-precise. |

Optional / deferred:

| # | PR | Notes |
|---|----|-------|
| 11 | `bump-tax-tables-ty2025` | Constants currency: TY2024 → TY2025. **Held — user decides timing.** |

Total: **10 PRs in scope**, plus the deferred bump.

If the user later wants to close the test-coverage LOWs, that's a separate
campaign (one big "test hardening" PR per area).

### Branch strategy

Single umbrella branch, linear history. `feat/whatif-fixes` accumulates one
or more commits per area in priority order. Each area's commit-set is one
"PR unit" — a logically reviewable group of fixes with shared context — but
the work lands as a contiguous span of commits on the same branch rather
than as isolated PR branches.

Rationale: each area depends on the prior area's audit-doc line-citation
state (the doc-cleanup PR refreshes citations after each area lands). A
linear branch lets the next area subagent see the latest baseline without
rebase overhead. When the user is ready to push, the umbrella branch can
ship as one large PR or be split via `git rebase -i` into per-area PRs for
review purposes.

The constants-bump (PR 12) is exception: it lands as a separate branch
`feat/whatif-fixes-constants-bump` from the umbrella tip, so it can be held
back from merge while the rest ships.

### Subagent-driven execution, area-grained

One subagent dispatch per area (not per finding). The subagent receives:

- The area's relevant findings (full bodies, copied from audit doc).
- The relevant Go files to modify.
- Per-finding TDD instructions (write test → run RED → fix → run GREEN → commit).
- Pre-commit hook expectations (full Go test suite + GitNexus refresh, ~25-90s).

Same review pattern as the audit:

1. Spec compliance review: did the subagent fix every finding it was given?
   No extras, no skipped items.
2. Code quality review: are the fixes minimal? Tests target real boundaries?
   Did the subagent introduce regressions?

### Commit & PR conventions

- One commit per finding when possible (e.g., `fix(whatif): F-001 add age-65+ standard deduction`).
- Test-only commits use `test(whatif): F-NNN <description>`.
- PR titles: `fix(whatif): area N — <area name>` with body listing each F-NNN
  resolved + "Closes audit findings F-NNN, F-NNN, ..." for traceability.
- Each PR's body must reference `docs/whatif-math-audit-2026-05-05.md` so the
  audit remains the canonical specification of what was fixed and why.

### Test naming

- Boundary tests: `Test<Function>_<BoundaryName>` (e.g., `TestCalculateFederalTax_BracketBoundary11600`).
- Worked-example regression tests: `Test<Function>_AuditWE_<id>` (e.g.,
  `TestCalculateFederalTax_AuditWE_1_1`). These pin the audit's worked
  examples as permanent regression guards.

### Out-of-band: constants-currency decision

The TY2024 → TY2025 bump is a *content update*, not a bug fix. It's gated by
two questions only the user can answer:

1. **Do we want to be on TY2025 or wait for TY2026?** Rev. Proc. 2024-40 has
   TY2025 published; TY2026 typically publishes in Nov 2026.
2. **What's the timing?** Bumping mid-cycle means users get TY2025 numbers
   in scenarios they had been running with TY2024. Differences are small
   in absolute terms but propagate forward through the projection.

The campaign defers this decision: PR 12 prepares the bump but doesn't merge
without explicit go-ahead. The audit's Appendix A serves as the diff source.

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Fix introduces a regression caught only at runtime (e.g., handler returns wrong response). | Pre-commit hook runs full `go test ./...`. Each PR also runs the handlers package test suite. |
| Fix breaks an existing test that asserts wrong behavior. | When a fix breaks an existing test, treat the existing test's expectation as suspect. Read the test's source comment / git blame. If it asserted wrong behavior to match buggy code, update the test. Otherwise stop and escalate. |
| Two PRs touch the same file (e.g., PRs 1 and 7 both touch `tax.go`). | Linear ordering. Each subagent dispatch sees the prior PR's merged code as baseline. |
| Audit doc's line numbers go stale as code is reformatted. | Out of scope for this campaign — citations remain valid against `b978aa9`. The audit doc remains a snapshot artifact of that code state. |
| Constants bump (PR 11) produces unexpected output drift. | Side-by-side regression: live page values from `docs/what-if-retirement-verification.md` post-fix (refreshed by PR 9) become the new baseline. Document expected drift in PR 11 before merging. |
| F-035 (configurable RMD timing) breaks existing saved scenarios. | Default new field to `mid_year` for new scenarios; for existing saved scenarios without the field, use `start_of_year` (current code's implicit behavior) so saved projections don't change. |
| F-026 (zero-COLA) — fixing the silent 2% substitution may surprise users who relied on the silent default. | The fix preserves a 2% default when the field is unset / never touched; only respects 0% when the user explicitly enters 0. |

## Process

1. Spec approved by delegation ("continue as you see fit"); decisions on
   F-035 and F-063 documented above. User may override before any PR
   merges.
2. Move to `writing-plans` skill to produce a step-by-step plan covering
   the 10 in-scope PRs.
3. Execute via subagent-driven-development, one PR at a time.
4. After all 10 PRs land, decide on the constants bump (PR 11).

## Open questions

None at design time. Mechanics locked:

- Scope: 10 user-visible findings (P1 + P2 + P3). Test hardening (P5) and
  constants bump (P4) deferred.
- Granularity: one PR per finding for P1/P2; one bundled PR for P3.
- Workflow: per-PR subagent dispatch, two-stage review.
- Branch strategy: linear commits on `feat/whatif-fixes`.
- Order: F-001 → F-065 → F-049 → F-057 → F-026 → F-029 → F-018 → F-063
  → F-070 → P3 bundle.
- F-035: configurable timing, default mid-year.
- F-063: rename only; full G-K filed as separate ticket.

## Approval

The next step is `writing-plans` to produce a plan with one section per
PR. The plan owns per-finding TDD instructions and subagent dispatch
templates.
