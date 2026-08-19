# NEXT — state of the accounts & transfers run (A0–A9)

Written 2026-08-16. Branch `fix/review-aug16`, nothing pushed.
Read `.swarm/NEXT.md` too — it carries the headless dispatch recipe and its
traps, which still apply.

## RUN COMPLETE — 2026-08-16

All ten tasks accepted. `swarm/gate.sh done` exits 0 across all 25 ledger rows
(P1–P15 from the File Manager run plus A0–A9). Branch `fix/review-aug16`,
committed, **not pushed**.

| Task | Scope | Tier | Accepted at |
|------|-------|------|-------------|
| A0 | GLOSSARY vocabulary | 1 | attempt 1 `496dd45` |
| A1 | StableID + sidecar migration | 3 | attempt 4 `1680e46` |
| A2 | Account model + loader attribution | 3 | attempt 2 `ce69515` |
| A3 | Transfer classification | 3 | attempt 2 `e9ad6ae` |
| A4 | Balances (anchor, freshness, drift) | 2 | attempt 1 `a27d25d` |
| A5 | Funding projection | 2 | attempt 2 (in `80abbcc`/`6cb5da3`) |
| A6 | Accounts settings UI | 1 | attempt 1 `e918fb2` |
| A7 | Transfers page + explorer badge | 2 | attempt 1 `80abbcc` |
| A8 | Dashboard card, projection line, banner | 1 | attempt 3 `347103c` |
| A9 | MCP ledger tools + search fix | 2 | attempt 2 `6bc508d` |

Final accessibility pass: `.swarm/verdicts/FINALA.1.checker-a11y.verdict`. It
returned FAIL on contrast; one of its three claims held and was fixed
(`a957974`), two did not survive checking — see below.

## What the run actually delivers

A Schwab→USAA transfer no longer double-counts. Previously a transfer whose
description missed the substring patterns inflated expenses on the debit leg
and was classified Income on the credit leg; one that DID match vanished
entirely. Now both legs are classified `Transfer`, stay visible in the ledger,
and are excluded from income and expenses — including in the dashboard's
cumulative cash-flow chart and the MCP spend tools, both of which had to be
fixed explicitly because neither filters by type the way the rest of the app
does.

## Open follow-ups (task chips raised, not part of this run)

1. **Accounts delete-confirm Cancel button does not cancel** — it re-POSTs and
   re-renders the same panel. **Verified NOT destructive**: `handlers.go:172`
   returns before any deletion whenever `confirm != "yes"`. A final-pass
   verdict described it as "effectively confirms"; that reading is wrong.
   Also in that chip: an unconditional `data-focus-target` that always focuses
   the ID input regardless of which field errored, and one vacuous test loop.
2. **Pre-existing File Manager a11y violations** (from the prior run).
3. **`handleImport`'s render path is untested.**
4. **`text-green-600` income figures elsewhere in the app** measure 3.30:1 on
   white. The new transfers page was fixed to `green-700` (5.02:1); the
   pre-existing instances were deliberately not swept up.
5. **`/insights` and `/whatif` have no `<h1>`** — ruled out of A8's scope.
6. **`admin/undo.go:57`** pre-checks `decisions[key]` without the
   `legacyPairKeysFor` aliasing `ClearDuplicateDecision` applies, so a
   pre-StableID decision makes `undo_resolve` claim there is nothing to undo.
   Fails loudly.
7. **`aliases.json` is still Hash-keyed** on both ends — not broken by A1, but
   orphanable by exactly the description reformat A1 exists to prevent.

## Two final-pass claims that did NOT survive checking

Recorded so nobody "fixes" them later:

- The **accounts card uses no emerald at all** (amber-700 / red-700 / gray), so
  the verdict's "accounts card emerald is the core blocker" is misattributed.
- The **emerald-600 in the transfers success panel is a decorative
  `aria-hidden` icon**, and its adjacent text is emerald-700 (5.21:1). At
  3.58:1 the icon clears point 7's ≥3:1 threshold for non-text elements.

## Rulings this run has already produced

In `ACCOUNTS_TRANSFERS_SPEC.md` under "Rulings":

- **2026-08-16d** — validating an oracle only against a featureless tree proves
  the checks discriminate, not that they are satisfiable. Validate at BOTH
  ends. A2's oracle shipped three defects because of this.
- **2026-08-16e** — a loop-based probe assertion must be preceded by a count
  assertion, or it passes vacuously on an empty set.
- **2026-08-16f** — when a task changes how stored data is keyed, the oracle
  must assert on an existing consumer's observable output, not just the new
  accessor. A1's two implementations both passed 10/10 and were not equivalent:
  one silently broke pin resolution everywhere the user sees it.

## Environment fixes already applied

- `swarm/tier3-setup.sh` and `tier3-compare.sh` now create and resolve Tier-3
  worktrees **outside** the repo (`$TMPDIR/swarm-worktrees/<repo>/<task>/`,
  override with `SWARM_WT_ROOT`). Nested worktrees broke blind isolation: a
  worker's Edit/Write calls landed in the parent repo while its Bash writes
  went to the worktree. Committed in the agents2 repo as `0cb6e21`.
- `.gitignore` in budget2 excludes `.swarm/tier3/*/wt-*/` and `.swarm/work/`.

## Still-open infrastructure debt

- **`worker-local` remains unavailable.** The Spark has no Qwen vLLM unit; port
  8000 is OpenJarvis's own auth-required server, and `litellm-config.yaml`
  points at the unresolvable `spark.local:8000`. Every Tier-3 task in this run
  has used an Anthropic substitute under ruling 2026-08-16c. `judge-local` has
  the same dependency, so a three-judge dispute is still unservable.
- `litellm-config.yaml` has a `worker-zai` entry pointing straight at
  `api.z.ai` (needs `GLM_API_KEY`), unused. It exists precisely for "if
  OpenRouter degrades" and would decouple the GLM family from OpenRouter
  credits.
