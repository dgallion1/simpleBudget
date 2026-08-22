# Tier 3 divergence report — A2

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
CHECK 5 accounts-store-roundtrip: PASS
CHECK 6 missing-accounts-file-not-an-error: PASS
CHECK 7 match-file-first-match-by-id: PASS
CHECK 8 loader-stamps-account-id: PASS
CHECK 9 credit-kind-forces-sign-flip: PASS
CHECK 10 non-credit-kind-leaves-heuristic-alone: PASS
SUMMARY: 10 passed, 0 failed
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Oracle defect found and repaired mid-flight

The first blind worker reported two bugs in the lead-authored probe, both
verified independently and both real:

1. `probeDataDir` did `return dir, storage.New(dir)`, but
   `storage.New(baseDir string) (*Storage, error)` returns two values — a
   compile error that would have failed BOTH implementations identically at
   check 4, producing a zero diff that looked like agreement.
2. `TestProbeA2_LoaderStampsAccountID` wrote identical content to both fixture
   files. `Transaction.Hash` is `sha256(date|lowercased desc|amount)` with no
   file or account component, so exact-hash dedup collapsed the two files and
   the "at least one row stamped" assertion was unsatisfiable regardless of
   implementation.

A third defect was found while fixing those: the probe called
`dl.Transactions()`, an accessor absent from the brief's pinned API. `LoadData`
already returns `*models.TransactionSet`, so the pin was unnecessary — one
worker had invented the accessor purely to satisfy it.

Why pre-dispatch validation missed them: a tree without the `accounts` package
fails `probe-compiles` for a legitimate reason, which masks a probe that also
does not compile on its own terms. **Validating an oracle only against a
featureless tree proves the checks are discriminating, not that they are
satisfiable.** The repaired oracle was re-validated at both ends: 3 pass / 7
fail on the featureless main tree, and 10 pass / 0 fail against a real
implementation.

## Adjudication

With the repaired oracle both implementations pass all 10 checks with
byte-identical output, so the choice was made on code quality, not behavior.

Two differences decided it, both in wt-local's favour:

1. **Blast radius.** wt-glm changed `loadCSVFile(filePath string)` to take an
   extra `accountKind` parameter and updated 10 existing call sites across
   `loader_test.go` and `coverage_test.go`. wt-local left that signature alone,
   delegating to a new `loadCSVFileForAccount(filePath, nil)`, and touched
   **zero** existing tests. Same behavior, strictly less disturbance to a
   critical-glob package.
2. **It implements the brief's overlap warning.** The brief requires a
   pattern-overlap between two accounts to be "a warning surfaced to the
   caller, not an error", but the pinned signature is `Save(...) error`.
   wt-local added `SaveWithWarnings(...) ([]string, error)` with `Save`
   delegating to it, satisfying both the pin and the requirement. wt-glm
   deferred the warning to task A6 as out of scope — defensible, but it leaves
   a stated requirement unmet in this task.

wt-local additionally ran `go test -race` on the touched packages (both
implementations added mutex-guarded loader state, so this is load-bearing),
tolerates both a `{"accounts": [...]}` wrapper and a bare-array fixture, and
resets `UnassignedCount` on every `LoadDataContext` exit path including the
early returns.

Both proved the credit-kind override discriminates, by different and equally
sound means: wt-glm by mutation-testing both branches, wt-local by asserting
the heuristic premise (`if usesCreditCardSignConvention(raw) { t.Fatal }`) so
the test cannot silently degrade, then running the same fixture through all
five account kinds.

Nothing was grafted from wt-glm. Its distinctive choices were the signature
change and the deferred warning, and both were resolved against it. Its
`Transactions()` accessor existed only to satisfy the defective probe and is
not needed.

Worth carrying forward: wt-local flagged that
`internal/handlers/explorer/handlers.go:477` constructs
`dataloader.New(importDir, store)` where the CSV directory is NOT
`store.BaseDir()`, so folder-import files are matched against the user's real
accounts. That is the intended behavior but it is an accident waiting to be
mis-edited; A6 and A7 should not assume CSV dir == data dir.

RESOLUTION: wt-local adopted wholesale, no synthesis; chosen for a strictly smaller blast radius (no existing signature changed, zero existing tests touched) and for implementing the pattern-overlap warning the brief required. Oracle defect repaired mid-flight and re-validated at both ends before adjudication.
