# DI6 attempt 2 — worker report

STATUS: DONE; frozen for independent review. No ledger verdict written.

## Scope and mechanism attribution

Read DI6-brief.md, ACCESSIBILITY.md and DI6.1.checker-tests.verdict before edits.
The user's explicit attempt-2 authorization overrides the brief's original
CSS prohibition. Only the generated web/static/css/tailwind.css and
web/static/js/page-refresh.test.cjs changed, plus this report and manifest.
No production JavaScript, markup, server or financial code was changed.

CSS freshness was caught by **lead integration** (`make css-verify` in the
baseline+DI6 release), also independently confirmed by checker-tests through
`make check`. It was not a browser styling defect. The attempt-1 class-presence
audit did not establish generated-artifact freshness; its conclusion that no
CSS build was needed is superseded by this report.

The restart coverage deficiency was caught by **primary checker mutation
testing**: removing epoch comparison survived the original tests. Submitted
production behavior was correct. The new durable test starts with a pending
dirty notice at revision seven, receives epoch restarted/revision zero, requires
the old notice to disappear without reload or automatic dialog, then requires
a new dirty notice at restarted/revision one. This also proves dirty protection
survives rebaselining and the old high counter cannot suppress new signals.

## CSS generation and release reproduction

Used the existing standalone Tailwind 3.4.17 binary from the lead's frozen
release, copied into ignored tmp/, rather than downloading or updating tooling.
Commands (all shell commands used `rtk proxy`):

- `make COMMIT=69e484a css` — exit 0.
- `make COMMIT=69e484a css-verify` — exit 0, `tailwind.css is up to date`.

COMMIT is supplied to avoid the Makefile's git-describe invocation. No git
commands/state changes were used. The pinned binary emitted its existing
outdated-caniuse-lite advisory; no dependency update was performed.

Copied only the frozen CSS input tree (web/, Makefile, tailwind.config.js and
pinned binary) from /tmp/budget2-DI6-release.XArdi6 into the independent
/tmp/di6-attempt2-css.aykWUh directory, then overlaid the generated stylesheet
and changed JS test. The lead's running full-integration directory was not
modified. This uses baseline 69e484a+DI6 inputs, excluding DI1/DI2 changes.

- `make -C /tmp/di6-attempt2-css.aykWUh COMMIT=69e484a css-verify` — exit 0,
  `tailwind.css is up to date`.
- cmp of the isolated and working production page-refresh.js — exit 0.
- Working CSS and isolated regenerated tmp/tailwind.check.css SHA256 both:
  `6661373d4a2f2e55f9dc9c4756bcfa38bce8866812025775763ca00410e43e6b`.
- A byte comparison asserted that removing exactly
  `.outline{outline-style:solid}` from the newly generated CSS yields the
  frozen attempt-1 stylesheet byte-for-byte. Assertion passed; no other delta.

Lead was notified immediately after this verification that DI2's template/JS
hold could be released. No subsequent CSS regeneration was performed.

## Mutation red / actual-code green

The mutation was applied only to the production source string returned to the
VM harness; no production file was edited. Reproduction from the worktree:

```bash
rtk proxy node - <<'JS'
const fs = require('node:fs');
const read = fs.readFileSync;
fs.readFileSync = function(path, ...args) {
  const result = read.call(this, path, ...args);
  if (String(path).endsWith('/page-refresh.js')) {
    const token = ' || state.epoch !== baseline.epoch';
    if (typeof result !== 'string' || !result.includes(token))
      throw Error('Mutation target missing');
    return result.replace(token, '');
  }
  return result;
};
require('./web/static/js/page-refresh.test.cjs');
JS
```

RED: exit 1; six original tests passed, the new seventh test failed specifically
with `restart must remove the old epoch notice`, `1 !== 0`.

GREEN: `node --test web/static/js/page-refresh.test.cjs` — exit 0, 7/7 passed
against actual, unchanged production code.

## Final scoped verification

- `go test -race -count=1 ./internal/services/uirefresh ./internal/services/mcpsvc`
  — exit 0, both packages passed.
- `go test -count=1 ./cmd/server -run TestDI6` — exit 0; mounted route, rendered
  dashboard script inclusion and served script test passed.
- `go build -o /tmp/di6-attempt2-server ./cmd/server` — exit 0; binary not run.
- `go vet ./internal/services/uirefresh ./internal/services/mcpsvc` — exit 0.

Production SHA256 values were identical before and after attempt 2:

- page-refresh.js: e4f515e7adcd11c4bad42d5ec1207a37809203e19899c25591d9574cb3e5aca8
- cmd/server/main.go: 75e55eafc49ee2a1044003bc01221c911cf6ce6a696079fcac3a11b20776fd3e
- mcpsvc/server.go: 934dd599792d76842b17bc153e08cfd4483e20165df8b8ce59c9a74cef622b82
- uirefresh/refresh.go: 08462c261a0fb94f60aa253100c017e0c12f994c49e4e3eb6957dbbf07eb3763

No Go symbol edits, so no new caller analysis was needed; attempt-1 caller
evidence remains in DI6.1.md. No new UI behavior was introduced. The generated
delta is the unused outline utility scanned from an existing style.outline
property; the rendered notice and its inline focus styling remain unchanged.
The lead reported attempt-1 a11y PASS; that independent verdict is not authored
or rewritten here. No subagents, ledger edits, real-data calls or restarts.

## Cumulative handoff

`.swarm/DI-run/manifests/DI6.2.files` includes every attempt-1 source/evidence
path plus the generated CSS and attempt-2 report/manifest. Apply this cumulative
manifest to baseline 69e484a for the DI6-only release. Prior report remains
available at `.swarm/DI-run/reports/DI6.1.md` with the correction above.
Ignored tmp/ contains only build/check tooling artifacts; release deliverables
are the cumulative manifest paths. Worker territory is frozen at return.
