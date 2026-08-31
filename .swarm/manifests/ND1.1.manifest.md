# ND1 attempt 1 — manifest (STATUS: DONE)

## Task

Extend `detectNearDuplicatePairs` in
`internal/services/dataloader/near_duplicates.go` with two new candidate-
pair shapes per `.swarm/ND-RUN-SPEC.md`:

- Gap A: `isPendingPostedPair` falls back to the same affinity rule
  (HasPrefix either way, else `commonPrefixLen >= pendingPostedPrefixMinLen`)
  applied to normalized `OriginalDescription` when the Description-based
  check fails, catching pairs where the bank prettifies the posted side's
  Description.
- Gap B: new `isScheduledSettledPair`, checked last in `isCandidatePair`,
  pairs a scheduled bill pay / recurring autopay with a posted settlement
  that is NOT a `Check #NNN` row, using a 7-day window, same account,
  exactly-one-scheduled status split, exclusion of any check-shaped
  Description on either side, and a >=2-shared-token affinity check on
  tokenized Description (lowercased, split on non-alphanumeric runs,
  tokens <3 bytes and a fixed stopword list dropped).

Also updated the `list_duplicates` MCP tool description prose (no code
behavior change) to mention the new scheduled->autopay/ACH settlement
shape.

## Files changed

- `internal/services/dataloader/near_duplicates.go` — added
  `hasPendingPostedAffinity` helper (factored out of `isPendingPostedPair`,
  now also applied to OriginalDescription as a fallback for Gap A); added
  `isScheduledSettledPair`, `isScheduledStatus`, `tokenizeDescription`,
  `sharedTokenCount`, and `scheduledPaidTokenStopwords` for Gap B; wired
  `isScheduledSettledPair` as the last check in `isCandidatePair`; updated
  doc comments on `isCandidatePair` and `isPendingPostedPair` to describe
  the new shape/fallback. No changes to constants, `pairKey`, greedy
  pairing, `classify()`, or the status keyword lists.
- `internal/services/dataloader/near_duplicates_test.go` — added tests:
  Gap-A hit via orig-desc affinity when Description prefix fails, Gap-A
  non-hit with empty orig-desc on one side, Gap-B hits (insurance-shaped,
  Monroe-water-shaped), Gap-B misses (single shared token, both-posted,
  cross-account, 8-day window), and a check-shape regression test
  (scheduled + `Check #NNN` still pairs via the original classify()
  shape, unaffected by Gap B's exclusion). No existing test modified or
  removed.
- `internal/services/mcpsvc/admin/duplicates.go` — `list_duplicates` tool
  Description prose only: added a clause naming the scheduled bill
  pay/recurring autopay settling as a posted ACH/autopay (not-a-check)
  shape, so the word "autopay" appears. No code/behavior change.

## Verification

`go build ./...` — clean, no output.

`go test -count=1 ./internal/services/dataloader/` tail:
```
ok  	budget2/internal/services/dataloader	1.376s
```

`go vet ./internal/services/dataloader/... ./internal/services/mcpsvc/...`
— no output (clean).

Oracle (`.swarm/tier3/ND1/accept.sh`) full output:
```
== A1/A2/A3: oracle fixture + guard + real-data tests ==
ok  	budget2/internal/services/dataloader	0.020s
== A4: package suites ==
ok  	budget2/internal/services/dataloader	1.501s
ok  	budget2/internal/services/mcpsvc	0.034s
ok  	budget2/internal/services/mcpsvc/admin	4.010s
ok  	budget2/internal/services/mcpsvc/confirm	0.045s
ok  	budget2/internal/services/mcpsvc/curate	0.447s
ok  	budget2/internal/services/mcpsvc/ledger	0.368s
ok  	budget2/internal/services/mcpsvc/plan	5.357s
ok  	budget2/internal/services/mcpsvc/snapshot	0.004s
ok  	budget2/internal/services/mcpsvc/spend	0.704s
== A5: list_duplicates description names the scheduled→autopay shape ==
A5 ok
ORACLE PASS
```

No git commands run. Did not touch `.swarm/tier3/`, foreign territory
(`web/static/js/dashboard.js`, `web/templates/components/kpi-month-detail.html`,
`internal/templates/render_kpi_month_detail_test.go`, `budget2.old-1345`),
or run the compiled `budget2` binary.
