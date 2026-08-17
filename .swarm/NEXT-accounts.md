# NEXT — state of the accounts & transfers run (A0–A9)

Written 2026-08-16. Branch `fix/review-aug16`, nothing pushed.
Read `.swarm/NEXT.md` too — it carries the headless dispatch recipe and its
traps, which still apply.

## Status as of the latest session

**Six of ten tasks accepted:** A0, A1, A2, A4, A6. Remaining: **A3** (Tier 3),
A5, A7, A8, A9. `gate.sh done` fails on those five, correctly.

A1 took four attempts and hit the constitutional hard stop after two Tier-3
failures; the user authorized a narrow fourth attempt, which passed both
families. Both failures were found by the anthropic checker while the glm
checker passed — see rulings 2026-08-16f and 2026-08-16g.

## Next task: A3 (transfer classification, Tier 3)

The last Tier-3 task and the heart of the feature. Before dispatch its oracle
must be written AND validated at both ends (ruling 2026-08-16d), and — this is
the one that matters most here — it must assert on an **existing consumer's
observable output**, not just on the new classifier (ruling 2026-08-16f).
Concretely: it is not enough to check that a pair is classified `Transfer`; the
oracle must show that `metrics.Calculate`'s income and expense totals actually
exclude those rows, because that is where a user would see the bug.

Acceptance criteria are in `ACCOUNTS_TRANSFERS_SPEC.md`'s task table. A3 also
replaces `filterInternalTransfers`, so it touches the dataloader critical glob
— it is already Tier 3, so no further escalation applies.

## Historical: the credit blocker (resolved)

**OpenRouter credits are exhausted** — account shows `total_credits: 150`,
`total_usage: 150.08`. Verify with:

```bash
curl -s https://openrouter.ai/api/v1/credits -H "Authorization: Bearer $OPENROUTER_API_KEY"
```

Every cloud model in `litellm-config.yaml` routes through OpenRouter, so this
takes out `worker-coder`/`checker-second` (GLM) **and** the haiku-backed
`checker-content`/`checker-a11y` at once. Symptom: HTTP 402 "You requested up
to 32000 tokens, but can only afford N". Small requests still succeed, which
makes it look intermittent — Claude Code dispatches agents at
`max_tokens: 32000`.

The key's own limit ($20, $14.13 used) is NOT the binding constraint. Raising
it will not help; the account needs credits.

User chose to top up. Once credits are back, no config change is needed.

## Task table

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| A0 | GLOSSARY vocabulary | 1 | **accepted** `496dd45` |
| A1 | StableID + sidecar migration | 3 | **accepted at attempt 4** `1680e46` |
| A2 | Account model + loader attribution | 3 | **accepted at attempt 2** `ce69515` |
| A3 | Transfer classification | 3 | pending — oracle not yet written |
| A4 | Balances (anchor, freshness, drift) | 2 | **accepted** `a27d25d` |
| A5 | Funding projection | 2 | pending |
| A6 | Accounts settings UI | 1 | **accepted** `e918fb2` |
| A7 | Transfers page | 2 | pending |
| A8 | Dashboard card + banner | 1 | pending |
| A9 | MCP tools | 2 | pending |

Follow-ups raised as separate task chips, not part of this run: the dead
Cancel control on the accounts delete-confirm panel plus its focus
misdirection and one vacuous test loop; the pre-existing File Manager a11y
violations; and `handleImport`'s untested render path.

Known, outside A1's scope: `admin/undo.go:57` pre-checks `decisions[key]`
without the `legacyPairKeysFor` aliasing that `ClearDuplicateDecision`
applies, so a pre-StableID decision makes `undo_resolve` claim there is
nothing to undo. Fails loudly. And `aliases.json` is still Hash-keyed on both
ends — not broken by A1, but orphanable by exactly the description reformat
A1 exists to prevent.

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
