# Tier 3 divergence report — A1

| worktree | oracle exit |
|----------|-------------|
| wt-glm   | 0 |
| wt-local | 0 |

## No behavioral divergence
```
CHECK 1 build: PASS
CHECK 2 vet: PASS
CHECK 3 existing-tests: PASS
CHECK 4 probe-compiles: PASS
CHECK 5 stable-id-format: PASS
CHECK 6 occurrence-index-separates-collisions: PASS
CHECK 7 unassigned-uses-file-prefix: PASS
CHECK 8 stable-id-uses-post-flip-amount: PASS
CHECK 9 legacy-pin-still-resolves: PASS
CHECK 10 pin-rewrites-to-stable-id: PASS
SUMMARY: 10 passed, 0 failed
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Adjudication

Both implementations pass all 10 oracle checks with byte-identical output. They
are NOT equivalent, and the oracle could not tell them apart.

**wt-local wins on correctness.** Both rekey the pins sidecar to StableID on
save. But three shared lookups index that map by `Transaction.Hash`:

- `internal/services/insights/trends.go:38` — `if id, ok := pins[t.Hash]`
- `internal/services/majorexpenses/engine.go:125` — `if id, ok := pins[first.Hash]`
- `majorexpenses.AnnotateRecurringPayments` (same pattern)

wt-local replaced all of them with `models.ResolveByIdentity` (StableID →
legacy Hash). **wt-glm left all three untouched** — its manifest is six files
and includes neither package. So under wt-glm, the moment the pins file is
rekeyed, every one of those lookups misses and pins silently stop resolving in
the dashboard, explorer, insights and the MCP spend tools. The pin is still on
disk; it just stops being found. No error, no log line.

The oracle passed both because it verifies `PinFor` — the new resolution path —
and never exercises the three legacy call sites that consume the same map.
This is the same shape as ruling 2026-08-16a: green tests over a feature that
is broken where the user actually sees it.

wt-local also covers two stores wt-glm did not touch at all
(`duplicate_decisions.go`, `near_duplicates.go`), and its blast-radius analysis
found the 11 non-test callers of `LoadTransactionPins` that made the risk
visible in the first place. It changed one pre-existing test
(`handlers/majorexpenses/coverage_test.go`) that asserted `pins[hash] != ""` —
an assertion that encodes the old key convention and is exactly what behavior 3
invalidates. That is the right reason to change a test.

Both proved their legacy fallback by deleting it and observing named test
failures. Neither reintroduced the description into the identity, and both left
`ComputeHash` and `deduplicateTransactions` untouched — wt-local additionally
asserts dedup is unchanged in both directions (same hash + different StableIDs
still collapses; different hashes + identical StableID still does not).

Nothing was grafted from wt-glm.

## Oracle gap to fix before this pattern recurs

The A1 oracle tests the new resolution helper but not the legacy consumers of
the same data. A probe that asserted "a pinned transaction still shows its
major-expense name after a rekey, as rendered by the insights path" would have
failed wt-glm. Recorded as ruling 2026-08-16f.

RESOLUTION: wt-local adopted wholesale, no synthesis. Chosen on correctness, not style: wt-glm rekeys the pins sidecar to StableID while leaving three Hash-indexed lookups (insights/trends.go:38, majorexpenses/engine.go:125, AnnotateRecurringPayments) unchanged, which silently breaks pin resolution everywhere the user sees it. The oracle passed both 10/10 and could not distinguish them.
