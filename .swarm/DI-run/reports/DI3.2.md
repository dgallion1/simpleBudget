# DI3 attempt 2 — scoped F1 correction

Status: implementation complete; frozen for renewed named checks and gate. This is not an acceptance or accessibility verdict. No deployment.

## Scope and exact delta

Read the updated task-3 brief, canonical design ruling, ACCESSIBILITY (all 17 points), and `.swarm/DI-run/verdicts/DI3.1.checker-a11y.verdict`. Applied the conceded WCAG 2.5.3 finding only.

Relative to immutable `/tmp/budget2-DI3-release.26XjRf`, the sole production change is removal of `aria-label="Choose CSV or backup ZIP files to import"` from `#drop-zone` in `web/templates/pages/dashboard.html`. Its name now derives from unchanged visible contents: “Or drop CSV or backup ZIP files anywhere on this page, or click to browse”. Role, tabindex, file input, visible copy, classes, layout and JavaScript are unchanged. Money, reporting interfaces/consumers, charts and palette are unchanged; DI3.1 inventory and evidence remain applicable. No Go symbols changed, so no new semantic-reference work was needed.

Promoted `/tmp/DI3-a11y.mBCqsi/focused.cjs` into durable `.swarm/DI-run/reports/DI3-label-name.cjs`: exact rendered visible text, computed accessible-name matching and accessibility snapshot, explicit axe `label-content-name-mismatch`, keyboard focus and actual file-chooser opening with Enter (light) and Space (dark). File selection is empty; no import occurs. Harness dependency paths can be overridden with DI3_PLAYWRIGHT, DI3_CHROME and DI3_AXE; DI3_BASE defaults to the dedicated loopback port and rejects 8080/8081.

## Red/green and scoped verification

Commands ran from `/home/darrell/bin/ai/budget2/.worktrees/dashboard-insights` with the RTK proxy and scoped sandbox escalation required by this host.

- RED: `rtk proxy env DI3_LABEL_OUTPUT=.swarm/DI-run/reports/DI3.2-red.json node .swarm/DI-run/reports/DI3-label-name.cjs` — exit 1 before template change. Both themes returned matchingName 0 and explicit `label-content-name-mismatch` targeting `#drop-zone`; visible copy and keyboard behavior already matched expectations.
- GREEN: `rtk proxy env DI3_LABEL_OUTPUT=.swarm/DI-run/reports/DI3.2-green.json node .swarm/DI-run/reports/DI3-label-name.cjs` — exit 0 after correction. Both themes returned matchingName 1, exact unchanged visible copy, no violations for the explicit rule, and successful native file-chooser opening. Repeated after restoring original indentation.
- `rtk proxy go test ./internal/handlers/dashboard -run '^TestDI3' -count=1` — exit 0, package ok.
- `rtk proxy go build ./...` — exit 0.
- `rtk proxy make css-verify` — exit 0, `tailwind.css is up to date`; existing Browserslist stale-data warning only. No dependency update or stylesheet regeneration.
- `rtk proxy diff -qr /tmp/budget2-DI3-release.26XjRf/web web` — exit 1 solely for the Dashboard template; static CSS/JS unchanged.
- `rtk proxy diff -qr /tmp/budget2-DI3-release.26XjRf/cmd cmd` — exit 0.
- `rtk proxy diff -qr --exclude=data /tmp/budget2-DI3-release.26XjRf/internal internal` — exit 0. An initial directory-name comparison reported local `internal/config/data`; its contents were not read and it is excluded from source comparison. The release's root built artifact is excluded throughout.

## Isolation and bounded review

Fresh headless Chromium contexts, synthetic data only, dedicated `127.0.0.1:18774`, live debug templates, 390×900 in light and dark. Used the previously built DI3.1 binary `/tmp/budget2-DI3-server-final` with BUDGET_DEBUG=true and synthetic directories under `/tmp/budget2-DI3-visual.eCMIJ3`; template/static directories explicitly pointed at this worktree. No existing browser sessions or real/demo ports accessed. Dedicated runtime stopped after verification. No full-suite, viewport-polish, chart-palette or financial reruns in this tightly scoped attempt.

Guidance used: Impeccable Operate/distill/craft-floor kept the incumbent layout and copy intact; TDD supplied the focused red/green regression; browser guidance used the authorized isolated headless fallback. No new brainstorming or approval cycle, subagents, git-state operations, ledger/spec/verdict changes or deployment.

The cumulative manifest retains all DI3.1 paths and adds the focused harness, red/green JSON, this report and the DI3.2 manifest. New attempt-two delta: one template attribute removal plus these five evidence/harness paths. Source is frozen at handoff; renewed independent named reviews and gate belong to the lead.
