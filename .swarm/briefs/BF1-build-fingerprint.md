# BF1 (né T13, renamed: ID collision with the import run) — Build fingerprint: detect a stale running server

Tier 1. Checks: `tests` (checker-tests, FAMILY anthropic). Worker: worker-coder, attempt 1.

## Problem

Sessions comparing the live app on :8080 against repo code cannot tell when the
server is running an older build. Go's automatic buildvcs stamp is WRONG for
binaries built inside `.claude/worktrees/*`: observed 2026-08-28, a binary
built in a worktree at commit 7f81437 carries `vcs.revision=ac67798…,
vcs.modified=true` — the PARENT checkout's dirty HEAD, because Go's VCS
detection walks up past the worktree's `.git` *file* to the main repo's
`.git` directory. So the fingerprint must come from ldflags evaluated by make
in the build directory, where `git` resolves the worktree correctly.

## Change (all additive; no behavior of any existing consumer may change)

### 1. Makefile
Near the existing `VERSION`/`BUILD_TIME` definitions add:
```make
# git describe runs in the build cwd, so a worktree build stamps the
# worktree's own HEAD. Do NOT rely on Go's buildvcs stamp instead: for
# builds under .claude/worktrees/* it records the PARENT checkout's HEAD
# and dirty flag (Go walks up past the worktree's .git file), which is
# exactly the staleness confusion this fingerprint exists to remove.
COMMIT := $(shell git describe --always --dirty 2>/dev/null || echo unknown)
```
and extend LDFLAGS with `-X budget2/internal/version.Commit=$(COMMIT)`.
LDFLAGS is shared by `build` and the `dist` targets — that is intended.

### 2. internal/version/version.go
- Add `Commit = "unknown"` to the ldflags var block.
- Add `Commit string \`json:"commit"\`` to `Info` (place after Version) and set
  `info.Commit = Commit` in `Get()`. Deliberately NO fallback to
  `vcs.revision` — that stamp lies for worktree builds (see above); an honest
  `"unknown"` (plain `go build`/`go run`/tests) beats a plausible wrong hash.
  Put that rationale in a comment on the field or assignment.
- Keep the existing VCS* fields exactly as they are (informational only).
- `String()`: leave as-is EXCEPT nothing — do not change it (its Commit line
  comes from VCSRevision today and tests pin that; changing it is out of
  scope).

### 3. /api/health — internal/handlers/backup/handlers.go `HandleHealth`
Return `{"status":"ok","commit":version.Commit}` (a `map[string]string` is
fine). The full build detail stays on the existing `/api/version`
(`version.Get()`), which now carries the new `commit` field automatically.
NOTE: `internal/handlers/whatif/handlers_live.go` has a comment (~line 14)
saying `/api/health` "returns only {\"status\":\"ok\"}" — read it in context
and update the wording so it stays true, without changing any code there.

### 4. X-Budget2-Build response header — cmd/server/main.go
Add a middleware alongside the existing `r.Use(...)` chain (before routes):
```go
// X-Budget2-Build lets any client (or a Claude session driving the
// browser) see which source build produced a response without a separate
// /api/version call — the one-glance stale-server check.
r.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Budget2-Build", version.Commit)
		next.ServeHTTP(w, req)
	})
})
```
Place it early enough that every route (including /static and /mcp) gets it.

### 5. MCP get_status — internal/services/mcpsvc/admin/status.go
Add to `statusOutput` (top, before DataDir):
```go
// Version/Commit identify the exact source build of the running server.
// Commit is "unknown" for a bare `go build` without the Makefile's ldflags.
Version string `json:"version"`
Commit  string `json:"commit"`
```
Set them in the handler from `version.Version` / `version.Commit` (import
`budget2/internal/version`). Extend the tool Description with one sentence:
compare `commit` against `git rev-parse --short HEAD` (or `git describe
--always --dirty`) to detect that the running server predates the code being
read. Adding OUTPUT FIELDS does not change the tool count — do NOT touch the
skill/README/server_test tool-registration want-lists — but DO check
`status_test.go` and any schema-asserting test and update them.

### 6. CLAUDE.md (repo root)
Add a short subsection under "Gotchas (this codebase)":
```markdown
Live server staleness:
- Before comparing the running app on :8080 (screenshots, browser-driving,
  MCP answers) against repo code, check the build fingerprint:
  `curl -s localhost:8080/api/health` → `.commit`, or the `X-Budget2-Build`
  response header, vs `git describe --always --dirty` in your checkout.
  A mismatch means the server predates (or postdates) the code you are
  reading — say so instead of debugging phantom differences.
- `commit` is stamped by `make build` ldflags. "unknown" = built without
  make. Do NOT trust `go version -m <binary>`'s vcs.revision for binaries
  built under `.claude/worktrees/*` — Go stamps the PARENT checkout's HEAD
  there.
```

## Tests (required)

- `internal/version`: extend `TestGet` (or add one) — `Info.Commit` equals the
  package `Commit` var, restored via t.Cleanup like the existing tests; JSON
  marshal of Info contains `"commit"`.
- `internal/handlers/backup`: test `HandleHealth` returns 200 with
  `status=="ok"` AND `commit==version.Commit` (there may be an existing health
  test — extend it, don't duplicate).
- `cmd/server`: find how the router/middleware is already tested (the package
  has tests). Add an assertion that a served request's response carries
  `X-Budget2-Build` equal to `version.Commit`. If no existing harness composes
  the middleware chain, test via the smallest real entry point available —
  do not stand up the full app on a fixed port; use httptest.
- `internal/services/mcpsvc/admin`: extend the existing get_status test(s) to
  assert `version`/`commit` fields are present and equal to the version
  package's values.
- Discrimination (Tier-1-proportionate): at least the health and get_status
  tests must compare against `version.Commit` (the variable), not a
  hard-coded literal, so they hold under any stamp; and setting
  `version.Commit` to a sentinel in a test (with cleanup) must flow through
  to the observed output — include one such sentinel-based assertion in the
  health test.

## Hard rules

- Work ONLY in /home/darrell/bin/ai/budget2/.claude/worktrees/build-fingerprint.
- LSP `incomingCalls`/`findReferences` before editing `HandleHealth`,
  `version.Get`, `statusOutput`, and before adding the Info field (find every
  test asserting Info's JSON shape).
- Do NOT touch `internal/services/retirement/engine/**` or anything else in
  `.swarm/critical.globs`. Do not start servers or bind :8080 (httptest only).
- Verification: `go build ./... && go vet ./... && go test ./... &&
  staticcheck ./...` plus `gofmt -l` on touched files; `make build` must also
  succeed in the worktree and `strings ./budget2 | grep -c <current git
  describe output>` must be ≥1 (then delete the built binary — it must not
  appear in the manifest or get committed).
- No pipes over test output without `set -o pipefail`.
- Write `.swarm/manifests/T13.1.files` (repo-relative, one per line). Never
  commit.

## Acceptance criteria

1. `make build` in the worktree stamps `version.Commit` with the worktree's
   own `git describe --always --dirty` (verified via strings on the binary).
2. `/api/health` reports `commit`; `/api/version` reports the new field;
   `X-Budget2-Build` present on served responses; `get_status` reports
   version+commit — each covered by a test that tracks `version.Commit`
   rather than a literal, with at least one sentinel-injection assertion.
3. No existing consumer breaks: full suite green, staticcheck clean, gofmt
   clean; the stale comment in handlers_live.go updated.
4. CLAUDE.md gains the staleness-check note.
