# DI4 attempt 2 — F1 correction and author evidence

STATUS: DONE. Source frozen at handoff for independent review. This is not a checker verdict or gate acceptance.

## Controlling scope

Read the complete DI4.1 checker-tests, checker-second and checker-a11y verdicts, task-4 brief, all ACCESSIBILITY.md points, and the canonical appended concession ruling. Only conceded F1 is corrected. Existing a11y observations O2/O3 and other checker probes remain outside this attempt; their promotion belongs to DI5.

The detector already returns all 22 series in the checkers' real 66-transaction fixture. Attempt 1 introduced false scope copy, not row truncation. Both live recurring scope statements now say: “All detected recurring series are shown without a row limit.” The same false maximum was removed from DI4.1.md. No detector, handler, view-model, arithmetic, grouping, date/reference, JavaScript, accessibility behavior, layout class or stylesheet change.

## Files changed in this attempt

- web/templates/pages/insights.html: one scope sentence, shared by full/HX responses.
- web/templates/components/insights-investigation.html: one scope sentence in the standalone recurring partial.
- .swarm/DI-run/reports/DI4.1.md: corrected the corresponding author claim.
- internal/handlers/insights/recurring_scope_di4_test.go: durable promotion of TestCheckerDI4RecurringLimitWording from /tmp/DI4-checker-tests.xGOWIK/internal/handlers/insights/zz_checker_di4_test.go, retaining its 22 merchant names, amounts, dates and full/HX/standalone counterexample. Added direct real-detector count and exactly-once checks for every merchant name, plus HTTP status checks and named subtests. Other checker probes were not copied.
- This report, five logs, and the two attempt-2 manifests.

The cumulative .swarm/DI-run/manifests/DI4.2.files contains every DI4.1 path plus this attempt's files. It can overlay the original immutable /tmp/budget2-DI4-base.KPl7oY without depending on a live whole-tree copy. The worker-contract .swarm/manifests/DI4.2.files is identical. Prior manifests remain unchanged.

## Test-first evidence

Used the previously read TDD skill: promote the executable failure, observe it on unchanged application templates, then make the copy correction.

Exact underlying test command before and after:
`go test -count=1 -v ./internal/handlers/insights -run "^TestCheckerDI4RecurringLimitWording$"`

Executed from the worktree through:
`rtk proxy sh -c 'mkdir -p .swarm/DI-run/reports/DI4.2; go test -count=1 -v ./internal/handlers/insights -run "^TestCheckerDI4RecurringLimitWording$" > .swarm/DI-run/reports/DI4.2/red.log 2>&1; di42_result=$?; cat .swarm/DI-run/reports/DI4.2/red.log; exit "$di42_result"'`

Red: exit 1. All three subtests reported detector=22, rendered rows=22 and all 22 names checked, then failed specifically on false detector-limit wording. There was no fixture/count/name failure. See DI4.2/red.log.

After the two template sentences and author report were corrected:
`rtk proxy sh -c 'gofmt -w internal/handlers/insights/recurring_scope_di4_test.go; go test -count=1 -v ./internal/handlers/insights -run "^TestCheckerDI4RecurringLimitWording$" > .swarm/DI-run/reports/DI4.2/green.log 2>&1; di42_result=$?; cat .swarm/DI-run/reports/DI4.2/green.log; exit "$di42_result"'`

Green: exit 0. Full, HX and standalone each still have 22 real rows and each merchant name exactly once, without the false cap. See DI4.2/green.log. The tests execute actual loader/detector/handler/Go-template rendering, not source-text assertions or substituted markup.

## Verification and self-review

- `go test ./internal/handlers/insights ./internal/templates ./internal/services/insights -count=1`: exit 0; handlers 0.540s, templates 1.145s, service 0.007s. Includes the promoted regression and existing DI1/DI2/DI4 focused regressions. DI4.2/focused.log.
- `go build ./...`: exit 0. DI4.2/build.log.
- `make css-verify`: exit 0, “tailwind.css is up to date.” No CSS regeneration needed; only the existing outdated caniuse-lite advisory appeared. DI4.2/css-verify.log.
These commands ran through `rtk proxy sh -c` with output redirected to the named logs; command exit status was preserved.
- Inspected both one-line template diffs against the immutable attempt-1 checker snapshot and the corrected report/test. Neither HTML structure nor classes changed. No existing production Go symbol was edited.
- A cmp loop against /tmp/DI4-checker-tests.xGOWIK confirms the other seven prior internal/web manifest files are byte-identical: handlers.go, investigation.go, investigation_di4_test.go, insights.js, tailwind.css, anomalies-section.html and pricecreep-section.html.
- `rtk proxy diff -qr /tmp/budget2-DI4-base.KPl7oY/internal/services internal/services`: no differences. Detector and financial/period services remain the accepted baseline.
- The two template `diff -u` commands each show only the intended sentence replacement (expected diff exit 1).
- No browser/visual-polish loop or full-suite repeat for this narrow text-only correction. The changed views were rendered by the promoted full/HX/standalone regression; attempt-1 independent a11y results remain separately attributed, not re-issued by this worker.

## Isolation and handoff

No git operations, deployment, server launch, real/demo endpoint or connector access, real data access, subagents, ledger/spec/verdict edits, or unrelated repairs. All new transaction data exists only in Go test temporary directories. Scoped escalated apply_patch was used for the authorized worktree, following the established direct-tool Landlock workaround.

No blocker or contract question remains. Source is frozen. Independent reviewers and the lead gate determine acceptance; no worker PASS verdict was authored.
