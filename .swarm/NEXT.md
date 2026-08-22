# NEXT — state of the File Manager swarm run

Rewritten 2026-08-16. A relaunched session starts cold; this file is the handoff.

Revised 2026-08-21: P16 added, and step 1 rewritten. It told you to verify the
session was routed through a LiteLLM gateway that no longer exists — following
it as written would have stopped a relaunched session from dispatching anything.

## Where things stand

Branch `fix/review-aug16`. **The File Manager run is COMPLETE.** All 15 tasks
are accepted with verified evidence; `swarm/gate.sh done` exits 0. The final
accessibility pass found no regressions.

P15's blocker was resolved by user ruling 2026-08-16c: an Anthropic-family
model substituted for the unavailable `worker-local` as the second blind
implementer. The `worker-local` infrastructure problem described below is no
longer live: the gateway it depended on was dropped 2026-08-19 and
`worker-local` is a Claude Haiku agent now.

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| P12 | Multi-file upload | 1 | accepted, merged, pushed |
| P13 | Sortable columns | 2 | **accepted at attempt 2**, committed `ce963e2` |
| P14 | `ImportDirectory` config + scan endpoint | 2 | **accepted at attempt 1**, committed `04ba148` |
| P15 | Import execute + source delete | 3 | **accepted at attempt 2**, committed `0a8225a` |

Three runs have finished since: accounts & transfers (A0–A9), the review-fix
run (R1–R13) and run S (S1–S5). All rows are accepted. See
`.swarm/NEXT-accounts.md`, `ACCOUNTS_TRANSFERS_SPEC.md` and
`REVIEW_AUG20_SPEC.md`.

Merged with `master` 2026-08-22. `master` brought three rows this branch had no
part in — P16 (storage read-cache write ordering, landed as PR #32), P17
(SA4023 dead decrypt path) and P18 (CI workflow on pull requests) — all three
written directly in a lead session and carried in the ledger as `pending`,
unverified. They are kept as `pending` here rather than backfilled.

`gate.sh done` therefore exits 1 on four lines: R9, which sits at `no-change`
because no defect existed, plus P16–P18. No verdict was fabricated to make
either an honest non-finding or somebody else's unverified work look like an
acceptance.

## Follow-ups this run deliberately did not absorb

1. **Pre-existing accessibility violations on the File Manager page.** Unlabelled
   toggle checkboxes, an unnamed SVG delete button, and low-contrast text, all
   byte-identical at the run's starting point. Counts disagree between the two
   audits — the P13 axe run reported 20, the final pass reported 6 (5 contrast
   plus the unnamed button). The discrepancy is unreconciled; whoever takes the
   follow-up should re-count rather than trust either number.
2. **`handleImport`'s render call is untested.** Every `handleImport` test runs
   with `renderer == nil` (JSON fallback), so no Go test executes
   `handlers.go:601`. Live curl confirmed correct behavior today, but an edit
   changing only the template name would ship an empty body with a green suite —
   the same failure shape as ruling 2026-08-16a. Cheap fix: one handler test
   using `setupTestEnvWithRenderer` asserting the body contains "Import
   finished".

## How to dispatch (this session learned it the hard way)

This lead session was **not** gateway-routed, so the Agent tool could not
resolve `worker-glm` / `checker-glm`. Workers and checkers were dispatched as
headless subprocesses instead, which works:

```bash
cd /home/darrell/bin/ai/budget2 && \
env -i HOME="$HOME" PATH="$PATH" TERM=dumb \
  claude -p --agent worker-coder --permission-mode acceptEdits \
    --allowedTools "Read,Write,Edit,Glob,Grep,Bash" \
    --add-dir /home/darrell/bin/ai/budget2 < brief.md
```

The gateway env vars this block used to carry are gone — see the note on
independence lanes below; `ANTHROPIC_BASE_URL` is not something to set or to
check.

**Independence is a lane, not a vendor.** The LiteLLM gateway was dropped on
2026-08-19; there is no proxy and no local endpoint, and the `worker-glm` /
`checker-glm` aliases are gone. Every agent runs on Claude, with the model
chosen per agent in `.claude/agents/*.md` frontmatter. The second opinion now
comes from a different **job** and a different **model tier**: the primary
verifier asks "does this meet the criteria?" and cites the command proving each
one; `checker-second` asks "what would make this wrong?", defaults to FAIL on
ambiguity, and is doing its job badly if it never disagrees.

`gate.sh` still enforces two distinct `FAMILY` values mechanically. Write
`anthropic` for the primary verifier and `adversarial` for `checker-second`;
judges write `anthropic`, `adversarial`, and `impact`. `glm` and `local` still
validate only so pre-2026-08-19 verdicts keep parsing — writing either today
satisfies the gate and verifies nothing. Two PASSes are weaker evidence than
the old cross-vendor pair; that reduction was accepted deliberately (user
decision 2026-08-19).

Four traps, all hit at least once:

1. **`env -i` is required.** Without it the child inherits this session's
   host-auth socket, silently uses host credentials, and dies with
   `400 No connected db`.
2. **`cd` into the repo first, and pass `--add-dir`.** A worker launched from
   `agents2` cannot run the Go toolchain against budget2; the `rtk` hook then
   prints a misleading `Go build: Success` for a tree with no `go.mod`. The
   first P14 worker correctly refused to claim green and proved the false
   success with a planted syntax error.
3. **`--allowedTools` is variadic** — pass it as ONE comma-separated string and
   feed the prompt on **stdin**, or the brief's words get parsed as tool rules.
4. **The Anthropic-family checker** (`checker-tests`) has no agent definition;
   dispatch it via the lead session's own Agent tool, which is genuinely
   Anthropic-routed, and have it write `FAMILY: anthropic`.

### checker-second misreports its own family

**SUPERSEDED 2026-08-19** — kept as the record of why the gate checks
`FAMILY` at all. There is no gateway and no GLM routing now; see the
independence-lane note above for what to write today.

`checker-second` routes to Z.ai GLM (verified: querying the `checker-glm` alias
returns "Created by Z.ai... GLM model family"), but in long agentic contexts it
sometimes writes `FAMILY: anthropic`, which silently defeats the two-family
quorum. It did this on both P13 runs and got it right on P14.

**Always include the routing fact in the checker brief** — that which model ran
is infrastructure the dispatcher knows and the model cannot observe. Do NOT edit
a verdict file to fix it; re-dispatch with the fact stated. Wording used:

> This session is routed through a LiteLLM gateway. Your agent definition
> specifies model `checker-glm`, which the gateway resolves to Z.ai's GLM.
> Therefore your `FAMILY:` field MUST be exactly `glm`. This says nothing about
> what your verdict should be.

## The P15 blocker — `worker-local` cannot resolve

**SUPERSEDED 2026-08-19** — historical. `worker-local` is a Claude Haiku
agent now, and the Tier-3 second arm differs by model tier, not by vendor.

Tier 3 requires two blind implementations from different families:
`worker-coder` (GLM, available) and `worker-local` (Qwen on the Spark,
**unavailable**). Established 2026-08-16:

- `spark.local` does not resolve from nix3; only
  `spark.otter-lungfish.ts.net` (100.127.56.27) does.
  `litellm-config.yaml` still points `worker-local` at
  `http://spark.local:8000/v1` with `api_key: "unused"`.
- Something *is* listening on spark:8000 and it answers
  `{"error":"Unauthorized"}` — i.e. a vLLM with an API key. The only
  vLLM-backed unit running is `openjarvis.service` ("OpenJarvis API server
  (vLLM-backed)"). There is **no** separate Qwen3-32B unit on the box
  (`systemctl --user list-unit-files | grep -i vllm` finds nothing).
- So the swarm's `worker-local` would have to be repointed at OpenJarvis's own
  inference server, using a key from `~/.openjarvis/.env`, contending with it
  for the GPU.

That is a change to personal infrastructure and was deliberately NOT made
autonomously. Options, for the user to choose:

1. **Bring up a dedicated Qwen vLLM** on another port and repoint
   `worker-local` at `spark.otter-lungfish.ts.net:<port>`. Faithful to
   CLAUDE.md, $0 marginal cost, costs GPU memory alongside OpenJarvis.
2. **Repoint `worker-local` at the existing vLLM** (tailnet hostname + real
   API key). Cheapest to set up; couples the swarm to a personal service and
   whatever model it serves, which may not be Qwen.
3. **Substitute an Anthropic-family model** as P15's second blind implementer.
   No infra change and preserves Tier 3's intent (two independent blind
   implementations from different families), but deviates from CLAUDE.md's
   literal text and from its `$0 worker-local` cost discipline.
4. **Defer P15** and proceed to the accounts/transfers run.

Note `judge-local` has the same dependency, so a Tier-2 dispute needing three
judges is also currently unservable.

## P15 mechanics, once unblocked

`swarm/tier3-setup.sh P15` cuts its two blind worktrees from `HEAD` — run it
only now that P14 has merged, or the oracle's scan check fails in both
worktrees for reasons unrelated to either implementation.

The oracle at `.swarm/tier3/P15/accept.sh` (13 checks) was written and
validated before dispatch: 3 pass on a tree without the feature, 10 fail,
output byte-identical across runs. It asserts on filesystem effects and HTTP
status codes rather than response bodies, because `tier3-compare.sh` diffs the
two worktrees' output literally. Every safety check also requires a non-404
status so an unimplemented endpoint cannot satisfy it vacuously. The request
wire format is pinned in the design doc §3 — both blind workers must get it.

## Before `gate.sh done`

Run `checker-a11y` across the File Manager page, then review every
lead-authored file. **Known and already triaged:** an axe run during P13
verification found 20 pre-existing WCAG violations on that page (unlabelled
toggle checkboxes, unnamed SVG delete buttons, low-contrast size text), all
byte-identical at HEAD and none introduced by this run. Recorded in the design
doc; they deserve their own task rather than being folded into P15.

## Decisions already made — do not relitigate

Recorded in `docs/superpowers/specs/2026-08-12-file-manager-import-design.md`,
including rulings 2026-08-12a, **2026-08-16a** (P13 attempt 1 rejected: the
swap target is a different template — both checkers passed broken work by
reading the include chain instead of the handler) and **2026-08-16b** (P13's
three one-token handler changes are in scope).

- Browsers cannot delete a file chosen via `<input type="file">`. That is why
  the source delete needs a server-side folder import at all.
- The import folder is pinned to one configured directory, default
  `~/Downloads`, overridable by `BUDGET2_IMPORT_DIR`.
- Name collisions skip. No overwrite, no auto-rename; the source is never
  deleted for a file that was not imported.
- No file-level dedup — `LoadData` already dedups transactions across enabled
  files.

## Verification lesson worth keeping

A checker may not conclude that an htmx swap preserves anything by reading the
template include; it must establish which template the *handler* renders and,
where feasible, assert against the endpoint's real response body. Both
attempt-1 checkers failed this and passed a feature that was visibly broken
after a single click. The cheap decisive test:

```bash
curl -s -X POST http://127.0.0.1:<port>/explorer/files/toggle \
  -d "filename=<one>.csv&enabled=true" | grep -c "data-sort-btn"
```

0 means broken, non-zero means fixed.
