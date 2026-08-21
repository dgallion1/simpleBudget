# Tier 3 divergence report — S2

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## No behavioral divergence
```
== S2 oracle ==
PASS  go build ./...
PASS  go vet ./...
PASS  oracle: order independence / closest date / no drop
PASS  transfers package tests
PASS  dataloader package tests
PASS  full suite
ORACLE: PASS
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

## Lead review of the divergence (2026-08-20)

The oracle agrees on both arms, so the choice was made by reading the two
implementations. They are not equivalent; the oracle's fixture is simply too
small to separate them.

**wt-primary (worker-coder) — chosen.** Builds the whole pattern-gated edge set
symmetrically (every structurally valid pair, no canonical-index filter), sorts
edges by `(distance, PairKeyFor(...))`, and resolves them one distance layer at
a time: within a layer, a row with degree > 1 is ambiguous and every edge
touching it goes to review; the degree-1 edges pair. Nothing in the pass reads a
slice position, so order independence holds by construction rather than by
fixture.

**wt-alt (worker-local) — rejected, two defects the oracle does not reach:**

1. *Lost review-queue entries.* Its phase 1 gates ambiguity detection on
   `if paired[i] || !pattern[i] { continue }`, but the pass's own gate is
   `pattern[i] || pattern[j]`. A row WITHOUT a pattern hit that has two equally
   near pattern-hit candidates is therefore never marked ambiguous, never
   auto-paired (phase 2 requires `len(best) == 1`), and never queued. Today's
   code queues exactly that row. Silent loss of a review-queue entry.
2. *Order dependence, one level down.* Phase 2 canonicalizes each determinable
   pair with `if i < j` — a SLICE-INDEX test. When row `i`'s unique nearest is
   `j` and `j < i`, the pair is registered only if `j` independently names `i`
   back. Whether a candidate pair reaches the greedy assignment step therefore
   depends on where the two rows landed in the input slice, which is the exact
   property S2 exists to remove.

Neither defect fires on the four-row oracle fixture, which is why both arms
returned identical output. This is the N-version comparison doing its job: the
divergence was in the code, not in the observable behaviour of one small case.

RESOLUTION: merge wt-primary as-is, with one lead edit at merge time — the edge
builder's `for cents, idxs := range byCents { _ = cents ... }` becomes
`for _, idxs := range byCents`, a dead loop variable with no behavioural effect.
Nothing is grafted from wt-alt. The merged result is re-verified as attempt 2
under the Tier-2 dual-checker protocol, and the two defects above are named in
the checker briefs so both lanes probe them specifically.

### Evidence for defect 1 (run by the lead, not by either arm)

Probe fixture — two pattern-hit outflows on `chk`, one non-pattern inflow on
`sav` exactly equidistant (2 days) from both, so the inflow is genuinely tied
and no rule can say which outflow it settles:

| tree | outcome |
|------|---------|
| pre-fix `HEAD` | pairs **P1–N** (P1 is simply first in the slice), queue empty |
| wt-alt | pairs **P2–N** (whichever pair key sorts first), queue empty |
| wt-primary | pairs nothing; **both** P1–N and P2–N queued `ambiguous` |

wt-alt does not fix the defect here, it re-keys it: the arbitrary winner moves
from "first index" to "lowest hash". The user still never sees the ambiguity.
wt-primary is the only arm that routes a real tie to the human.

Note a deliberate semantic consequence of wt-primary, called out for the
checkers: on this fixture the pre-fix code auto-paired P1–N and the fixed code
does not. That is the intended reading of the documented rule — the old answer
was order-dependent, and an ambiguity the algorithm cannot resolve belongs in
the review queue rather than being settled by slice position. Both lanes should
confirm this does not contradict any written criterion or existing test.
