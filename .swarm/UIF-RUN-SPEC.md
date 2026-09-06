# UIF — scoped U-run follow-up

Approved scope: the user's subscription badge repair request, not a redesign.
Constitution: ACCESSIBILITY.md (all 17 points).

| Task | Tier | Checks | Attempt | Scope |
| --- | --- | --- | --- | --- |
| UIF1 | 2 | a11y | 1 | Insights subscription initial and label flex sizing |

## UIF1 acceptance criteria

- Render the first Unicode code point of MajorExpenseName, falling back to
  Description; render ? when both are empty. Never render a list in the badge.
- Preserve full label text and existing tooltip/link semantics. Long labels
  shrink and truncate; badge and dollar column retain their widths without
  overlapping labels. Use existing CSS utilities only.
- Change only web/templates/pages/insights.html and a focused actual-renderer
  regression test, plus this spec, ledger row, and manifest.
- Preserve shared slice helper, financial logic, and all unrelated changes.
- Run focused renderer tests red then green, go test ./internal/templates,
  and go build ./.... No server restarts or git state changes.
- Lead owns demo restart, headless-browser verification, independent a11y
  checking, and gate acceptance. Leave ledger pending verification.

## Evidence

Gate manifest: .swarm/manifests/UIF1.1.files (all changed paths).
