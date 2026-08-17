# NEXT — state of the accounts & transfers run (A0–A9)

Written 2026-08-16. Branch `fix/review-aug16`, nothing pushed.
Read `.swarm/NEXT.md` too — it carries the headless dispatch recipe and its
traps, which still apply.

## Blocked on

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

## Status

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| A0 | GLOSSARY vocabulary | 1 | **accepted** `496dd45` |
| A1 | StableID + sidecar migration | 3 | merged `6435ed5`, **NOT accepted** — needs Tier-2 dual-family |
| A2 | Account model + loader attribution | 3 | **accepted** `ce69515` |
| A3 | Transfer classification | 3 | not started — oracle not yet written |
| A4 | Balances (anchor, freshness, drift) | 2 | **accepted** `a27d25d` |
| A5 | Funding projection | 2 | not started |
| A6 | Accounts settings UI | 1 | **incomplete** — see below |
| A7 | Transfers page | 2 | not started |
| A8 | Dashboard card + banner | 1 | not started |
| A9 | MCP tools | 2 | not started |

## Resume sequence, once credits are restored

1. **A1 verification.** Both blind implementations are already built, compared
   (10/10 each), adjudicated and merged — only the two Tier-2 checkers still
   need to run, at **attempt 2**. Briefs: reuse the A2 checker pattern; include
   the routing fact (see `.swarm/NEXT.md`) so `checker-second` writes
   `FAMILY: glm`. Verdict files go to `.swarm/verdicts/A1.2.<checker>.verdict`.
   Then `gate.sh check A1`, escalate-scan, ledger → accepted.

2. **A6.** Partial work is in the worktree `.swarm/work/A6`
   (`internal/handlers/accounts/`, `cmd/server/main.go`,
   `web/templates/layouts/base.html`, `web/templates/pages/accounts.html`).
   Its first worker exited 0 having written only `handlers.go`; the
   continuation then died on the 402. It needs finishing and has never been
   verified. Brief: `.swarm/briefs/A6.md`.

3. Then A3 (Tier 3, oracle must be written first), A5, A7, A8, A9.

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
