# NEXT — state of the File Manager swarm run

Rewritten 2026-08-16. A relaunched session starts cold; this file is the handoff.

## Where things stand

Branch `fix/review-aug16`. P13 and P14 are accepted and committed. **P15 is
blocked on infrastructure** — see "The P15 blocker" below.

| Task | Scope | Tier | Status |
|------|-------|------|--------|
| P12 | Multi-file upload | 1 | accepted, merged, pushed |
| P13 | Sortable columns | 2 | **accepted at attempt 2**, committed `ce963e2` |
| P14 | `ImportDirectory` config + scan endpoint | 2 | **accepted at attempt 1**, committed `04ba148` |
| P15 | Import execute + source delete | 3 | pending — oracle written, BLOCKED |

`gate.sh done` currently fails listing P15 (correct). The ledger also carries
the accounts/transfers run's tasks once those are appended — see
`ACCOUNTS_TRANSFERS_SPEC.md`.

## How to dispatch (this session learned it the hard way)

This lead session was **not** gateway-routed, so the Agent tool could not
resolve `worker-glm` / `checker-glm`. Workers and checkers were dispatched as
headless subprocesses instead, which works:

```bash
cd /home/darrell/bin/ai/budget2 && \
env -i HOME="$HOME" PATH="$PATH" TERM=dumb \
  ANTHROPIC_BASE_URL=http://localhost:4000 \
  ANTHROPIC_AUTH_TOKEN=sk-swarm-local \
  claude -p --agent worker-coder --permission-mode acceptEdits \
    --allowedTools "Read,Write,Edit,Glob,Grep,Bash" \
    --add-dir /home/darrell/bin/ai/budget2 < brief.md
```

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
