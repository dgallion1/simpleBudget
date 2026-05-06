# What-If Fix Campaign — Design

**Date:** 2026-05-06
**Author:** Claude (campaign design)
**Status:** Draft, awaiting review
**Predecessor:** `docs/whatif-math-audit-2026-05-05.md` (71 findings, F-001…F-071)

## Goal

Resolve every actionable finding from the what-if math audit. Land formula
fixes, close test-coverage gaps, decide the constants-currency question, and
update misleading UI copy — without breaking anything that already works.

## Non-goals

- No re-architecture. Fix the bug; don't redesign the function.
- No new features. The audit found math issues, not feature gaps.
- No bumping past TY2025 unless TY2026 tables have published. The constants
  bump is a discrete decision, gated separately.
- No changes outside `internal/services/retirement/`, `internal/handlers/whatif/`,
  `internal/models/whatif.go`, and the relevant `web/templates/components/whatif/*.html`
  files. UI/copy fixes touch templates only.

## Scope (all 71 findings)

| Severity | Count | Treatment |
|----------|-------|-----------|
| HIGH | 0 | n/a |
| MEDIUM | 14 | Formula fixes via TDD per finding |
| LOW | 45 | Test-coverage gaps closed by adding the missing edge-case tests |
| INFO | 12 | Mostly currency notes / doc copy tweaks; some no-ops |

The 14 MEDIUMs include both real formula bugs (F-001, F-018, F-026, F-029,
F-049, F-057, F-065 — 7 items) and substantive test-coverage gaps that an
auditor flagged as MEDIUM because they could mask future regressions
(F-011, F-019, F-032, F-033, F-035, F-036, F-062 — 7 items). These get the
same TDD treatment as the formula bugs, just with the test as the deliverable
rather than the test-then-fix.

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

### One PR per area

Ten area PRs, in this priority order (highest impact first):

| Order | PR | Area | Findings | Notes |
|-------|----|------|----------|-------|
| 1 | `whatif-fixes-area-1` | Federal & state tax | F-001 (M), F-007–F-017 (L) | F-001 is the highest-impact formula fix. |
| 2 | `whatif-fixes-area-7` | Taxable account & withdrawals | F-049 (M), F-045–F-048, F-050–F-052 (L) | F-049 silently under-collects LTCG. |
| 3 | `whatif-fixes-area-10` | Chain, healthcare, budget-fit | F-065 (M), F-066–F-071 (L) | F-065 is the largest dollar-magnitude error. |
| 4 | `whatif-fixes-area-2` | Specialized tax surcharges | F-018, F-019 (M), F-020–F-024 (L/I) | F-018 fixes MFS taxable SS handling. |
| 5 | `whatif-fixes-area-3` | Social Security | F-026, F-029 (M), F-025, F-027–F-031 (L/I) | F-026 (zero-COLA inexpressible) and F-029 (display bug) are real UX bugs. |
| 6 | `whatif-fixes-area-4` | RMD | F-032, F-033, F-035, F-036 (M), F-034 (L) | SECURE 2.0 age-75 transition handling. |
| 7 | `whatif-fixes-area-9` | Backtest, MC, guardrails | F-057 (M), F-062 (M), F-058–F-061, F-063, F-064 (L/I) | F-057 (off-by-one window). F-063 (UI copy) split out — see PR 11. |
| 8 | `whatif-fixes-area-5` | PV & compounding | F-037, F-038, F-039, F-040, F-041 (L/I) | All LOWs/INFOs — b978aa9 already fixed the substantive issues. |
| 9 | `whatif-fixes-area-6` | Living-expense projection | F-042, F-043, F-044 (L) | F-043 is a dead-code observation; resolve by deletion or annotation. |
| 10 | `whatif-fixes-area-8` | Roth conversion | F-053–F-056 (L/I) | All test gaps + observations. |

Plus two cross-cutting PRs:

| Order | PR | Findings / scope |
|-------|----|------------------|
| 11 | `whatif-fixes-doc-cleanup` | F-063 (guardrails copy), other INFO copy items, audit doc "Codebase audited at" SHA refresh after each area lands. |
| 12 | `whatif-fixes-constants-bump` | Constants currency: TY2024 → TY2025 federal brackets / LTCG / std deduction. Standalone decision; may defer until TY2026 publishes. |

Total: **12 PRs**.

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
| Fix introduces a regression caught only at runtime (e.g., handler returns wrong response). | Pre-commit hook runs full `go test ./...`. Each area PR also runs the handlers package test suite. |
| Fix breaks an existing test that asserts wrong behavior. | When a fix breaks an existing test, treat the existing test's expectation as suspect. Read the test's source comment / git blame. If it asserted wrong behavior to match buggy code, update the test. Otherwise stop and escalate. |
| Two areas fix the same function (e.g., F-049 and F-068 both touch `calculator.go`). | Branch order respects priority. Later branches rebase on earlier-merged work. Subagent prompt includes the latest-merged commit SHA so it knows the baseline. |
| Audit doc's line numbers go stale as code is reformatted. | After each area PR merges, refresh the audit doc's line citations as part of `whatif-fixes-doc-cleanup` (PR 11). |
| Constants bump produces unexpected output drift. | Run a side-by-side regression: the live page values from `docs/what-if-retirement-verification.md` (currently anchored to TY2024 brackets). Document expected drift in PR 12 before merging. |

## Process

1. Approve this spec. (Awaiting user.)
2. Move to `writing-plans` skill to produce a step-by-step plan covering
   the 12 PRs.
3. Execute via subagent-driven-development, one area at a time.
4. After all 10 area PRs merge, decide on the constants bump (PR 12).
5. Refresh audit doc line citations and ship doc-cleanup (PR 11).

## Open questions

None at design time. Mechanics decisions locked above:

- Scope: every actionable finding (14 MEDIUMs + 45 LOWs + 12 INFOs).
- Granularity: 10 area PRs + 2 cross-cutting PRs.
- Workflow: per-area subagent dispatch, two-stage review.
- Branch strategy: umbrella branch with per-area children.
- Order: by audit-finding impact (Areas 1, 7, 10 first).
- Constants bump: deferred decision, gated on TY2026 timing.

## Approval

After this spec is approved, the next step is `writing-plans` to produce a
plan with one section per PR. The plan will own per-finding TDD instructions
and subagent dispatch templates.
