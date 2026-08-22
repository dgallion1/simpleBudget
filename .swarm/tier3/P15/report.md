# Tier 3 divergence report — P15

| worktree | oracle exit |
|----------|-------------|
| wt-glm   | 0 |
| wt-local | 0 |

## No behavioral divergence
```
CHECK 1 build: PASS
CHECK 2 unit-tests: PASS
CHECK 3 server-boots: PASS
CHECK 4 scan-lists-only-direct-csvs: PASS
CHECK 5 import-keeps-source: PASS
CHECK 6 import-deletes-source: PASS
CHECK 7 collision-skips-and-keeps-source: PASS
CHECK 8 traversal-name-rejected: PASS
CHECK 9 non-csv-not-imported: PASS
CHECK 10 symlink-target-survives: PASS
CHECK 11 failed-write-keeps-source: PASS
CHECK 12 empty-batch-is-400-and-inert: PASS
CHECK 13 mixed-batch-deletes-only-imported: PASS
SUMMARY: 13 passed, 0 failed
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Adjudication

Both implementations passed all 13 oracle checks with byte-identical output, so
the choice was made on code quality, not behavior. Both also agreed on the two
judgment calls the brief left open: a failed `os.Remove` after a successful
write is reported as imported-with-a-reason rather than rejected, and a 400
empty batch returns before the per-file loop.

Two differences decided it, both in wt-local's favour:

1. **The readback guard is genuinely tested.** The design doc's testing section
   asks for "a store stub whose readback returns short bytes". wt-local added a
   four-function `importDeps` seam and tests a short readback and a readback
   error directly, then mutation-tested the guard — deleting the
   `len(back) != len(data)` check made the test fail with the source gone, and
   it was restored and re-run green. wt-glm declined the seam as scope creep and
   covers the readback only indirectly, via the shared `removeSource=false` path
   exercised by the failed-write test, plus a pin on the reason string. Its
   reasoning is defensible, but the readback is precisely the guard the design
   doc calls out as "what makes 'fully saved' mean something", and an untested
   guard is the one most likely to rot.

2. **`isDirectChild` resolves symlinks on both sides**, so a symlinked import
   folder (a symlinked `~/Downloads`, or `/tmp` where it is a link) still
   imports, while anything reached by leaving the folder still does not.
   wt-glm's `filepath.Dir` comparison would reject a symlinked import folder
   outright. wt-local pins both directions with tests.

wt-local also carries broader coverage (26 test funcs vs 12), at the cost of a
larger diff (904 vs 662 insertions) and the DI seam. The seam is justified by
the spec's explicit ask.

Nothing was grafted from wt-glm: its distinctive choices were the two above,
and both were resolved against it.

Merged result independently re-verified in the main tree by the lead: `go build`
exit 0, `gofmt -l` clean on both touched Go files, and the oracle re-run
standalone reporting 13 passed, 0 failed.

RESOLUTION: wt-local adopted wholesale, no synthesis; chosen for a directly-tested (and mutation-tested) readback guard and symlink-resolving isDirectChild, both of which wt-glm lacked. Merged to fix/review-aug16; oracle re-run on the merged tree passes 13/13.
