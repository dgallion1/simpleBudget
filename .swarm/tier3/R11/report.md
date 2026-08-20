# Tier 3 divergence report — R11

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## Divergence
### wt-primary output
```

### 1. build

### 2. vet

### 3. atomicWrite must not stage at a name derived only from the destination
no fixed staging name in storage.go — ok

### 4. dayOf and the balance API must be untouched (blast-radius guard)
balance.go unchanged — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/storage	1.138s

### 6. the storage package's own suite under -race
ok  	budget2/internal/services/storage	182.456s

### 7. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	37.454s
ok  	budget2/cmd/server	28.989s
ok  	budget2/cmd/validate	1.041s
ok  	budget2/internal/config	1.024s
ok  	budget2/internal/handlers/accounts	5.642s
ok  	budget2/internal/handlers/approval	1.026s
ok  	budget2/internal/handlers/backup	164.743s
ok  	budget2/internal/handlers/dashboard	10.417s
ok  	budget2/internal/handlers/duplicates	1.031s
ok  	budget2/internal/handlers/explorer	10.813s
ok  	budget2/internal/handlers/insights	7.367s
ok  	budget2/internal/handlers/majorexpenses	2.952s
ok  	budget2/internal/handlers/transfers	5.030s
ok  	budget2/internal/handlers/whatif	30.318s
ok  	budget2/internal/http	1.034s
ok  	budget2/internal/models	1.049s
ok  	budget2/internal/services/accounts	1.053s
ok  	budget2/internal/services/amazon	1.034s
ok  	budget2/internal/services/anomalies	1.115s
ok  	budget2/internal/services/backup	1.662s
ok  	budget2/internal/services/classifier	1.041s
ok  	budget2/internal/services/dataloader	2.176s
ok  	budget2/internal/services/insights	1.046s
ok  	budget2/internal/services/majorexpenses	1.041s
ok  	budget2/internal/services/mcpsvc	1.534s
ok  	budget2/internal/services/mcpsvc/admin	12.199s
ok  	budget2/internal/services/mcpsvc/confirm	1.089s
ok  	budget2/internal/services/mcpsvc/curate	2.656s
ok  	budget2/internal/services/mcpsvc/ledger	2.807s
ok  	budget2/internal/services/mcpsvc/plan	5.724s
ok  	budget2/internal/services/mcpsvc/snapshot	1.033s
ok  	budget2/internal/services/mcpsvc/spend	8.678s
ok  	budget2/internal/services/merchants	1.033s
ok  	budget2/internal/services/metrics	1.031s
ok  	budget2/internal/services/pricecreep	1.040s
ok  	budget2/internal/services/restore	2.158s
ok  	budget2/internal/services/retirement	20.756s
ok  	budget2/internal/services/retirement/analysis	18.253s
ok  	budget2/internal/services/retirement/completeness	1.043s
ok  	budget2/internal/services/retirement/engine	1.081s
ok  	budget2/internal/services/retirement/history	1.042s
ok  	budget2/internal/services/retirement/overrides	1.047s
ok  	budget2/internal/services/retirement/prepare	1.061s
ok  	budget2/internal/services/storage	200.318s
ok  	budget2/internal/services/transfers	1.033s
ok  	budget2/internal/templates	9.610s
ok  	budget2/internal/testutil	1.043s
ok  	budget2/internal/version	1.033s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### wt-alt output
```

### 1. build

### 2. vet

### 3. atomicWrite must not stage at a name derived only from the destination
no fixed staging name in storage.go — ok

### 4. dayOf and the balance API must be untouched (blast-radius guard)
balance.go unchanged — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/storage	1.140s

### 6. the storage package's own suite under -race
ok  	budget2/internal/services/storage	183.342s

### 7. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	37.661s
ok  	budget2/cmd/server	29.080s
ok  	budget2/cmd/validate	1.048s
ok  	budget2/internal/config	1.032s
ok  	budget2/internal/handlers/accounts	5.445s
ok  	budget2/internal/handlers/approval	1.026s
ok  	budget2/internal/handlers/backup	167.125s
ok  	budget2/internal/handlers/dashboard	10.381s
ok  	budget2/internal/handlers/duplicates	1.031s
ok  	budget2/internal/handlers/explorer	10.946s
ok  	budget2/internal/handlers/insights	7.423s
ok  	budget2/internal/handlers/majorexpenses	3.026s
ok  	budget2/internal/handlers/transfers	5.204s
ok  	budget2/internal/handlers/whatif	30.451s
ok  	budget2/internal/http	1.034s
ok  	budget2/internal/models	1.045s
ok  	budget2/internal/services/accounts	1.043s
ok  	budget2/internal/services/amazon	1.035s
ok  	budget2/internal/services/anomalies	1.096s
ok  	budget2/internal/services/backup	1.697s
ok  	budget2/internal/services/classifier	1.041s
ok  	budget2/internal/services/dataloader	2.148s
ok  	budget2/internal/services/insights	1.045s
ok  	budget2/internal/services/majorexpenses	1.033s
ok  	budget2/internal/services/mcpsvc	1.530s
ok  	budget2/internal/services/mcpsvc/admin	12.149s
ok  	budget2/internal/services/mcpsvc/confirm	1.084s
ok  	budget2/internal/services/mcpsvc/curate	2.678s
ok  	budget2/internal/services/mcpsvc/ledger	2.894s
ok  	budget2/internal/services/mcpsvc/plan	5.595s
ok  	budget2/internal/services/mcpsvc/snapshot	1.032s
ok  	budget2/internal/services/mcpsvc/spend	8.308s
ok  	budget2/internal/services/merchants	1.040s
ok  	budget2/internal/services/metrics	1.041s
ok  	budget2/internal/services/pricecreep	1.033s
ok  	budget2/internal/services/restore	2.157s
ok  	budget2/internal/services/retirement	20.749s
ok  	budget2/internal/services/retirement/analysis	18.249s
ok  	budget2/internal/services/retirement/completeness	1.048s
ok  	budget2/internal/services/retirement/engine	1.059s
ok  	budget2/internal/services/retirement/history	1.033s
ok  	budget2/internal/services/retirement/overrides	1.048s
ok  	budget2/internal/services/retirement/prepare	1.043s
ok  	budget2/internal/services/storage	198.064s
ok  	budget2/internal/services/transfers	1.031s
ok  	budget2/internal/templates	9.511s
ok  	budget2/internal/testutil	1.042s
ok  	budget2/internal/version	1.040s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### diff (primary vs alt)
```diff
13c13
< ok  	budget2/internal/services/storage	1.138s
---
> ok  	budget2/internal/services/storage	1.140s
16c16
< ok  	budget2/internal/services/storage	182.456s
---
> ok  	budget2/internal/services/storage	183.342s
19,23c19,23
< ok  	budget2/cmd/enrich-amazon	37.454s
< ok  	budget2/cmd/server	28.989s
< ok  	budget2/cmd/validate	1.041s
< ok  	budget2/internal/config	1.024s
< ok  	budget2/internal/handlers/accounts	5.642s
---
> ok  	budget2/cmd/enrich-amazon	37.661s
> ok  	budget2/cmd/server	29.080s
> ok  	budget2/cmd/validate	1.048s
> ok  	budget2/internal/config	1.032s
> ok  	budget2/internal/handlers/accounts	5.445s
25,26c25,26
< ok  	budget2/internal/handlers/backup	164.743s
< ok  	budget2/internal/handlers/dashboard	10.417s
---
> ok  	budget2/internal/handlers/backup	167.125s
> ok  	budget2/internal/handlers/dashboard	10.381s
28,32c28,32
< ok  	budget2/internal/handlers/explorer	10.813s
< ok  	budget2/internal/handlers/insights	7.367s
< ok  	budget2/internal/handlers/majorexpenses	2.952s
< ok  	budget2/internal/handlers/transfers	5.030s
< ok  	budget2/internal/handlers/whatif	30.318s
---
> ok  	budget2/internal/handlers/explorer	10.946s
> ok  	budget2/internal/handlers/insights	7.423s
> ok  	budget2/internal/handlers/majorexpenses	3.026s
> ok  	budget2/internal/handlers/transfers	5.204s
> ok  	budget2/internal/handlers/whatif	30.451s
34,38c34,38
< ok  	budget2/internal/models	1.049s
< ok  	budget2/internal/services/accounts	1.053s
< ok  	budget2/internal/services/amazon	1.034s
< ok  	budget2/internal/services/anomalies	1.115s
< ok  	budget2/internal/services/backup	1.662s
---
> ok  	budget2/internal/models	1.045s
> ok  	budget2/internal/services/accounts	1.043s
> ok  	budget2/internal/services/amazon	1.035s
> ok  	budget2/internal/services/anomalies	1.096s
> ok  	budget2/internal/services/backup	1.697s
40,66c40,66
< ok  	budget2/internal/services/dataloader	2.176s
< ok  	budget2/internal/services/insights	1.046s
< ok  	budget2/internal/services/majorexpenses	1.041s
< ok  	budget2/internal/services/mcpsvc	1.534s
< ok  	budget2/internal/services/mcpsvc/admin	12.199s
< ok  	budget2/internal/services/mcpsvc/confirm	1.089s
< ok  	budget2/internal/services/mcpsvc/curate	2.656s
< ok  	budget2/internal/services/mcpsvc/ledger	2.807s
< ok  	budget2/internal/services/mcpsvc/plan	5.724s
< ok  	budget2/internal/services/mcpsvc/snapshot	1.033s
< ok  	budget2/internal/services/mcpsvc/spend	8.678s
< ok  	budget2/internal/services/merchants	1.033s
< ok  	budget2/internal/services/metrics	1.031s
< ok  	budget2/internal/services/pricecreep	1.040s
< ok  	budget2/internal/services/restore	2.158s
< ok  	budget2/internal/services/retirement	20.756s
< ok  	budget2/internal/services/retirement/analysis	18.253s
< ok  	budget2/internal/services/retirement/completeness	1.043s
< ok  	budget2/internal/services/retirement/engine	1.081s
< ok  	budget2/internal/services/retirement/history	1.042s
< ok  	budget2/internal/services/retirement/overrides	1.047s
< ok  	budget2/internal/services/retirement/prepare	1.061s
< ok  	budget2/internal/services/storage	200.318s
< ok  	budget2/internal/services/transfers	1.033s
< ok  	budget2/internal/templates	9.610s
< ok  	budget2/internal/testutil	1.043s
< ok  	budget2/internal/version	1.033s
---
> ok  	budget2/internal/services/dataloader	2.148s
> ok  	budget2/internal/services/insights	1.045s
> ok  	budget2/internal/services/majorexpenses	1.033s
> ok  	budget2/internal/services/mcpsvc	1.530s
> ok  	budget2/internal/services/mcpsvc/admin	12.149s
> ok  	budget2/internal/services/mcpsvc/confirm	1.084s
> ok  	budget2/internal/services/mcpsvc/curate	2.678s
> ok  	budget2/internal/services/mcpsvc/ledger	2.894s
> ok  	budget2/internal/services/mcpsvc/plan	5.595s
> ok  	budget2/internal/services/mcpsvc/snapshot	1.032s
> ok  	budget2/internal/services/mcpsvc/spend	8.308s
> ok  	budget2/internal/services/merchants	1.040s
> ok  	budget2/internal/services/metrics	1.041s
> ok  	budget2/internal/services/pricecreep	1.033s
> ok  	budget2/internal/services/restore	2.157s
> ok  	budget2/internal/services/retirement	20.749s
> ok  	budget2/internal/services/retirement/analysis	18.249s
> ok  	budget2/internal/services/retirement/completeness	1.048s
> ok  	budget2/internal/services/retirement/engine	1.059s
> ok  	budget2/internal/services/retirement/history	1.033s
> ok  	budget2/internal/services/retirement/overrides	1.048s
> ok  	budget2/internal/services/retirement/prepare	1.043s
> ok  	budget2/internal/services/storage	198.064s
> ok  	budget2/internal/services/transfers	1.031s
> ok  	budget2/internal/templates	9.511s
> ok  	budget2/internal/testutil	1.042s
> ok  	budget2/internal/version	1.040s
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

RESOLUTION: winner = wt-primary, merged by patch. Both arms passed the oracle
(exit 0 / exit 0), touched the same single file, and produced BYTE-IDENTICAL
production code -- a diff across storage.go yields zero non-comment differing
lines. Only comment prose differs. wt-primary was taken on documentation
quality: its comments state WHY Rename rather than Link (Rename replaces an
existing destination, which is what gives atomicWrite its rewrite semantics;
Link fails EEXIST, which is what createExclusive wants and atomicWrite must
not have). wt-alt's comments are mechanical ("Write the data", "Close the
file") and would leave the next reader to rediscover that reasoning.

Both arms independently reached that Rename/Link conclusion, which is the one
place where copying the neighbouring createExclusive pattern wholesale would
have broken every rewrite in the application. That agreement is the most
useful signal this comparison produced.

PROCESS FINDING, correcting the lesson recorded after R4. R4's resolution
concluded Tier 3 was wasted there because the LEAD over-specified the brief.
R11's brief deliberately did the opposite -- it pinned only what the oracle
must call and left the mechanism, the failure-path cleanup and the Rename/Link
choice open -- and the two arms STILL converged byte-for-byte. So the real
condition is not "did the lead over-constrain the brief" but "is the solution
space genuinely open". A strong in-file precedent (createExclusive, 50 lines
away) collapses it just as effectively as a pinned API. Better rule for
assigning Tier 3: ask whether a competent implementer, given the brief AND the
surrounding code, could reasonably arrive at materially different designs. If
the answer is no, use Tier 2 and spend the budget on verification instead.
Both R4 and R11 answer no in hindsight; R1, where the arms diverged on whether
OverlapWarnings survived the save path, answers yes.

MERGE NOTE: patch-applied, not file-copied. The R11 worktrees branch from
040f4b3, which predates R10's accepted test files and R12's halted work. An
overlap check (comm against the main tree's modified files) returned empty and
git apply --3way reported a clean apply, so no hand-merge was needed this time
-- unlike R4, where a file copy would have silently reverted R1's and R7's
accepted work.
