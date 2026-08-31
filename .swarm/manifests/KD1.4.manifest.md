# KD1 attempt 4 — scoped fix, ruling KD-2026-08-31a

Scope authorized by the user: exactly two changes, nothing else. Both
delivered; `handlers.go` and `kpis.html` were NOT touched this attempt
(they remain exactly as attempt 3 left them).

## Change 1 — the missing trim marker

`web/templates/components/kpi-detail.html`, line 93, one line changed:

```diff
-                    {{$noCoverage := and (eq .Type "healthcare") .HealthcareNoCoverageInRange}}
+                    {{$noCoverage := and (eq .Type "healthcare") .HealthcareNoCoverageInRange -}}
```

Right-trim only (`-}}`), not `{{-` on the left. This was verified
mechanically, not by eye, before being trusted: left-trimming this action
would ALSO remove the whitespace gap between it and the preceding
`{{$isRate := .IsRate}}` line — a gap master's own template has at that
exact position (between the last var-decl and the next action) — which
would have under-shot master's baseline by one line instead of matching it.
Right-trim only removes the gap between this action and the following
`{{range .Monthly}}`, which is exactly the one NEW gap attempt 3 introduced
(checker-tests' KD1.3 verdict, "the branch emits FIVE" vs master's four).
The coordinator's own example (`{{- $noCoverage := ... }}`, left-trim) was
explicitly flagged as illustrative only ("verify against master, not by
eye"); the byte-diff below confirms right-trim-only is the correct fix and
the example would have been wrong.

## Verification — byte-diff against master (the actual command + output)

Technique: render `/dashboard/kpi/{income,expenses,savings,savings-rate}`
through the real chi router + real `templates.New` renderer, over
`defaultRows()` and `start=2025-01-01&end=2025-03-31` (the same fixture
`checker-tests`' KD1.3 twin-dump used), once against this tree's
`web/templates` (with the fix) and once against a scratch copy of
`web/templates` whose `kpi-detail.html` is `git show
master:web/templates/components/kpi-detail.html` spliced in (every other
template file identical, straight from this tree, since none of them
differ from master). Master content sanity-checked first
(`grep -c "healthcare\|HealthcareNoCoverageInRange"` on the scratch file →
`0`, confirming it really is the pre-KD1 baseline, not an accidental copy
of the branch file).

This was done with a TEMPORARY test file
(`internal/handlers/dashboard/zz_verify_byteidentical_test.go`) — run once,
its output captured below, then DELETED before this manifest was written
(confirmed absent: `git status --porcelain` shows no such file, and no
`.go` file whose name doesn't already appear in `KD1.4.files` was left in
`internal/handlers/dashboard/`). It is not part of the deliverable; the
deliverable tripwire is Change 2 below.

```
$ go test -count=1 -run TestZZVerifyByteIdenticalToMaster -v ./internal/handlers/dashboard/
=== RUN   TestZZVerifyByteIdenticalToMaster
    zz_verify_byteidentical_test.go:84: income: byte-identical, 6608 bytes
    zz_verify_byteidentical_test.go:84: expenses: byte-identical, 6612 bytes
    zz_verify_byteidentical_test.go:84: savings: byte-identical, 7065 bytes
    zz_verify_byteidentical_test.go:84: savings-rate: byte-identical, 7065 bytes
--- PASS: TestZZVerifyByteIdenticalToMaster (0.03s)
PASS
ok  	budget2/internal/handlers/dashboard	0.035s
```

Followed by an independent, external byte-compare of the four dumped
response bodies (written by the same temporary test to
`/tmp/.../scratchpad/kd1_bytecmp/{kind}_{branch,master}.html`) — the
technique named in the coordinator's message
(`.swarm/verdicts/KD1.3.checker-tests.verdict`'s twin-dump method):

```
$ for k in income expenses savings savings-rate; do
    cmp "$SCRATCH/${k}_branch.html" "$SCRATCH/${k}_master.html"; echo "$k exit: $?"
  done
income exit: 0
expenses exit: 0
savings exit: 0
savings-rate exit: 0
```

`cmp` exits 0 with NO output on a byte-for-byte match — confirmed for all
four pre-existing kinds. (`diff -q` would report identically; `cmp` was
used since it is the exact tool named in the KD1.3 verdict's own evidence.)

## Change 2 — the whitespace tripwire test

`internal/handlers/dashboard/handlers_http_test.go`: one new test,
`TestHandleKPIDetail_Expenses_WhitespaceOnlyLineCountMatchesMasterBaseline`.
Renders `/dashboard/kpi/expenses` over `defaultRows()` /
`2025-01-01..2025-03-31` (the same fixture used for the byte-diff above),
counts whitespace-only lines (`strings.TrimSpace(line) == ""` over
`strings.Split(body, "\n")`), and asserts the count equals a pinned
constant, `masterBaseline = 19`.

**Calibration (oracle-calibration rule — pin master's own number, not an
assumed "should be zero"):** computed by running the SAME counting method
(a small Go snippet using `strings.Split(body, "\n")` +
`strings.TrimSpace`) over the dumped `expenses_master.html` produced by
Change 1's verification run:

```
$ go run /tmp/count_ws.go "$SCRATCH/expenses_master.html"
19
$ go run /tmp/count_ws.go "$SCRATCH/expenses_branch.html"
19
```

Both master and this tree's (fixed) branch agree at 19 — consistent with
the `cmp` byte-identity result above (same content ⇒ same count, by
construction). Two counting conventions were checked and both agree
master-vs-branch (recorded so a future reader isn't confused by a
different number appearing in a shell one-liner):
- `strings.Split(body, "\n")` (what the test uses): **19** for both —
  includes the empty trailing element `Split` produces after the
  response's final `\n` byte (confirmed present via `tail -c 5 | xxd` →
  `...div>\n`).
- `grep -cE '^[[:space:]]*$'` (does not count a phantom line after a
  trailing newline): **18** for both.

The test uses the Go-native method (matching exactly what the assertion
itself computes), so `masterBaseline = 19` is the correct pin for THAT
method — not 18, which would be the grep number instead.

`/tmp/count_ws.go` was a throwaway script, deleted after use; not part of
any deliverable.

## Full verification

```
$ bash .swarm/tier3/KD1/accept.sh
== KD-2026-08-30d reconciliation + regressions ==
--- PASS: TestOracleKD1_LivingSignedRowsReconcile
--- PASS: TestOracleKD1_HealthcareSignedRowsReconcile
--- PASS: TestOracleKD1_ExportMatchesSignedRows
--- PASS: TestOracleKD1_MonthDrillMatchesSignedRow
--- PASS: TestOracleKD1_NoCoverageDashRegression
ok  	budget2/internal/handlers/dashboard	0.033s
== package suites ==
ok  	budget2/internal/handlers/dashboard	0.818s
ok  	budget2/internal/services/metrics	0.003s
ORACLE PASS

$ go build ./...
Go build: Success

$ go vet ./...
(clean, no output)

$ gofmt -l internal/handlers/dashboard/handlers_http_test.go
(no output — gofmt-clean; kpi-detail.html is not a Go file, gofmt N/A)

$ grep -n "Health Insurance" internal/handlers/dashboard/handlers.go
0 matches for 'Health Insurance'   [K6 grep gate: unchanged, handlers.go not touched this attempt]

$ go test -count=1 ./internal/handlers/dashboard/ ./internal/services/metrics/
ok  	budget2/internal/handlers/dashboard	0.784s
ok  	budget2/internal/services/metrics	0.003s

$ go test -count=1 -run TestHandleKPIDetail_Expenses_WhitespaceOnlyLineCountMatchesMasterBaseline -v ./internal/handlers/dashboard/
--- PASS: TestHandleKPIDetail_Expenses_WhitespaceOnlyLineCountMatchesMasterBaseline (0.01s)
```

## Scope confirmation

`internal/handlers/dashboard/handlers.go` and
`web/templates/components/kpis.html` were NOT touched this attempt (no
`Edit`/`Write` call against either file in this session) — `git status
--porcelain` shows them modified only because of the CUMULATIVE diff from
attempts 1–3, unchanged relative to the attempt-3 state. Exactly two files
changed this attempt, matching `KD1.4.files`:
- `web/templates/components/kpi-detail.html` — one line (added `-}}`).
- `internal/handlers/dashboard/handlers_http_test.go` — one new test
  (addition only, no existing test edited).

No leftover scratch/verification files: `git status --porcelain` shows no
untracked `.go` file under `internal/handlers/dashboard/`; the temporary
`zz_verify_byteidentical_test.go` and `/tmp/count_ws.go` were both deleted
after use.

## Notes for checkers

- Per ruling KD-2026-08-31a, checker-a11y is not expected to re-run this
  attempt (the diff is whitespace-only; attempt-3's a11y PASS covers the
  rendered pixels, which are unchanged — confirmed by the `cmp`
  byte-identity above covering the pre-existing four kinds, and the
  healthcare/living markup attempt-3 already passed a11y on is untouched
  bytes this attempt too).
- Acceptance per the ruling: oracle PASS (above) + checker-tests
  byte-identity twin-dump PASS + checker-second PASS.
