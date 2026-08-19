# Tier 3 divergence report — R4

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## Divergence
### wt-primary output
```

### 1. build

### 2. vet

### 3. API surface — Create/Find must carry an operation id
internal/services/mcpsvc/confirm/approvals.go:80:func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error) {
internal/services/mcpsvc/confirm/approvals.go:133:func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool) {

### 4. all four guarded call sites pass an operation id
-- askForApproval / awaitApproval call sites:
internal/services/mcpsvc/ledger/resolve.go:119:			if res, asked := askForApproval(deps, req, "resolve_transfer", key, token,
internal/services/mcpsvc/ledger/resolve.go:136:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "resolve_transfer", key, token); viaBrowser {
internal/services/mcpsvc/ledger/anchor.go:118:			if res, asked := askForApproval(deps, req, "set_balance_anchor", id, token,
internal/services/mcpsvc/ledger/anchor.go:135:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "set_balance_anchor", id, token); viaBrowser {
internal/services/mcpsvc/admin/restore.go:151:			if res, asked := askForApproval(deps, req, "restore_backup", name, token,
internal/services/mcpsvc/admin/restore.go:177:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "restore_backup", name, token); viaBrowser {
internal/services/mcpsvc/admin/shutdown.go:91:			if res, asked := askForApproval(deps, req, "shutdown_server", "", token,
internal/services/mcpsvc/admin/shutdown.go:103:		} else if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "shutdown_server", "", token); viaBrowser {
all four sites bound — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/mcpsvc/confirm	1.613s

### 6. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	37.832s
ok  	budget2/cmd/server	29.128s
ok  	budget2/cmd/validate	1.050s
ok  	budget2/internal/config	1.033s
ok  	budget2/internal/handlers/accounts	5.323s
ok  	budget2/internal/handlers/approval	1.034s
ok  	budget2/internal/handlers/backup	194.627s
ok  	budget2/internal/handlers/dashboard	9.996s
ok  	budget2/internal/handlers/duplicates	1.034s
ok  	budget2/internal/handlers/explorer	10.462s
ok  	budget2/internal/handlers/insights	7.255s
ok  	budget2/internal/handlers/majorexpenses	2.923s
ok  	budget2/internal/handlers/transfers	5.147s
ok  	budget2/internal/handlers/whatif	30.316s
ok  	budget2/internal/http	1.039s
ok  	budget2/internal/models	1.052s
ok  	budget2/internal/services/accounts	1.064s
ok  	budget2/internal/services/amazon	1.034s
ok  	budget2/internal/services/anomalies	1.095s
ok  	budget2/internal/services/backup	1.731s
ok  	budget2/internal/services/classifier	1.038s
ok  	budget2/internal/services/dataloader	2.147s
ok  	budget2/internal/services/insights	1.041s
ok  	budget2/internal/services/majorexpenses	1.035s
ok  	budget2/internal/services/mcpsvc	1.523s
ok  	budget2/internal/services/mcpsvc/admin	12.229s
ok  	budget2/internal/services/mcpsvc/confirm	1.088s
ok  	budget2/internal/services/mcpsvc/curate	2.665s
ok  	budget2/internal/services/mcpsvc/ledger	1.912s
ok  	budget2/internal/services/mcpsvc/plan	5.640s
ok  	budget2/internal/services/mcpsvc/snapshot	1.039s
ok  	budget2/internal/services/mcpsvc/spend	8.494s
ok  	budget2/internal/services/merchants	1.036s
ok  	budget2/internal/services/metrics	1.032s
ok  	budget2/internal/services/pricecreep	1.036s
ok  	budget2/internal/services/restore	2.201s
ok  	budget2/internal/services/retirement	20.313s
ok  	budget2/internal/services/retirement/analysis	18.304s
ok  	budget2/internal/services/retirement/completeness	1.033s
ok  	budget2/internal/services/retirement/engine	1.065s
ok  	budget2/internal/services/retirement/history	1.037s
ok  	budget2/internal/services/retirement/overrides	1.054s
ok  	budget2/internal/services/retirement/prepare	1.045s
ok  	budget2/internal/services/storage	231.062s
ok  	budget2/internal/services/transfers	1.033s
ok  	budget2/internal/templates	9.583s
ok  	budget2/internal/testutil	1.033s
ok  	budget2/internal/version	1.045s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### wt-alt output
```

### 1. build

### 2. vet

### 3. API surface — Create/Find must carry an operation id
internal/services/mcpsvc/confirm/approvals.go:75:func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error) {
internal/services/mcpsvc/confirm/approvals.go:126:func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool) {

### 4. all four guarded call sites pass an operation id
-- askForApproval / awaitApproval call sites:
internal/services/mcpsvc/ledger/resolve.go:119:			if res, asked := askForApproval(deps, req, "resolve_transfer", key, token,
internal/services/mcpsvc/ledger/resolve.go:136:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "resolve_transfer", key, token); viaBrowser {
internal/services/mcpsvc/ledger/anchor.go:118:			if res, asked := askForApproval(deps, req, "set_balance_anchor", id, token,
internal/services/mcpsvc/ledger/anchor.go:135:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "set_balance_anchor", id, token); viaBrowser {
internal/services/mcpsvc/admin/restore.go:151:			if res, asked := askForApproval(deps, req, "restore_backup", name, token,
internal/services/mcpsvc/admin/restore.go:177:		if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "restore_backup", name, token); viaBrowser {
internal/services/mcpsvc/admin/shutdown.go:91:			if res, asked := askForApproval(deps, req, "shutdown_server", "", token,
internal/services/mcpsvc/admin/shutdown.go:103:		} else if d, waitErr, viaBrowser := awaitApproval(ctx, deps, "shutdown_server", "", token); viaBrowser {
all four sites bound — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/mcpsvc/confirm	1.613s

### 6. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	40.682s
ok  	budget2/cmd/server	30.841s
ok  	budget2/cmd/validate	1.071s
ok  	budget2/internal/config	1.062s
ok  	budget2/internal/handlers/accounts	5.903s
ok  	budget2/internal/handlers/approval	1.055s
ok  	budget2/internal/handlers/backup	174.559s
ok  	budget2/internal/handlers/dashboard	10.820s
ok  	budget2/internal/handlers/duplicates	1.052s
ok  	budget2/internal/handlers/explorer	11.196s
ok  	budget2/internal/handlers/insights	7.993s
ok  	budget2/internal/handlers/majorexpenses	3.231s
ok  	budget2/internal/handlers/transfers	5.369s
ok  	budget2/internal/handlers/whatif	33.737s
ok  	budget2/internal/http	1.050s
ok  	budget2/internal/models	1.051s
ok  	budget2/internal/services/accounts	1.055s
ok  	budget2/internal/services/amazon	1.034s
ok  	budget2/internal/services/anomalies	1.102s
ok  	budget2/internal/services/backup	1.717s
ok  	budget2/internal/services/classifier	1.044s
ok  	budget2/internal/services/dataloader	2.130s
ok  	budget2/internal/services/insights	1.053s
ok  	budget2/internal/services/majorexpenses	1.036s
ok  	budget2/internal/services/mcpsvc	1.555s
ok  	budget2/internal/services/mcpsvc/admin	13.315s
ok  	budget2/internal/services/mcpsvc/confirm	1.106s
ok  	budget2/internal/services/mcpsvc/curate	3.054s
ok  	budget2/internal/services/mcpsvc/ledger	1.931s
ok  	budget2/internal/services/mcpsvc/plan	6.313s
ok  	budget2/internal/services/mcpsvc/snapshot	1.049s
ok  	budget2/internal/services/mcpsvc/spend	9.117s
ok  	budget2/internal/services/merchants	1.034s
ok  	budget2/internal/services/metrics	1.024s
ok  	budget2/internal/services/pricecreep	1.043s
ok  	budget2/internal/services/restore	2.153s
ok  	budget2/internal/services/retirement	23.424s
ok  	budget2/internal/services/retirement/analysis	20.451s
ok  	budget2/internal/services/retirement/completeness	1.024s
ok  	budget2/internal/services/retirement/engine	1.077s
ok  	budget2/internal/services/retirement/history	1.036s
ok  	budget2/internal/services/retirement/overrides	1.060s
ok  	budget2/internal/services/retirement/prepare	1.066s
ok  	budget2/internal/services/storage	209.759s
ok  	budget2/internal/services/transfers	1.044s
ok  	budget2/internal/templates	10.570s
ok  	budget2/internal/testutil	1.064s
ok  	budget2/internal/version	1.029s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### diff (primary vs alt)
```diff
7,8c7,8
< internal/services/mcpsvc/confirm/approvals.go:80:func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error) {
< internal/services/mcpsvc/confirm/approvals.go:133:func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool) {
---
> internal/services/mcpsvc/confirm/approvals.go:75:func (a *Approvals) Create(tool, subject, opID, title, detail string) (*Pending, error) {
> internal/services/mcpsvc/confirm/approvals.go:126:func (a *Approvals) Find(tool, subject, opID string) (*Pending, bool) {
26,42c26,42
< ok  	budget2/cmd/enrich-amazon	37.832s
< ok  	budget2/cmd/server	29.128s
< ok  	budget2/cmd/validate	1.050s
< ok  	budget2/internal/config	1.033s
< ok  	budget2/internal/handlers/accounts	5.323s
< ok  	budget2/internal/handlers/approval	1.034s
< ok  	budget2/internal/handlers/backup	194.627s
< ok  	budget2/internal/handlers/dashboard	9.996s
< ok  	budget2/internal/handlers/duplicates	1.034s
< ok  	budget2/internal/handlers/explorer	10.462s
< ok  	budget2/internal/handlers/insights	7.255s
< ok  	budget2/internal/handlers/majorexpenses	2.923s
< ok  	budget2/internal/handlers/transfers	5.147s
< ok  	budget2/internal/handlers/whatif	30.316s
< ok  	budget2/internal/http	1.039s
< ok  	budget2/internal/models	1.052s
< ok  	budget2/internal/services/accounts	1.064s
---
> ok  	budget2/cmd/enrich-amazon	40.682s
> ok  	budget2/cmd/server	30.841s
> ok  	budget2/cmd/validate	1.071s
> ok  	budget2/internal/config	1.062s
> ok  	budget2/internal/handlers/accounts	5.903s
> ok  	budget2/internal/handlers/approval	1.055s
> ok  	budget2/internal/handlers/backup	174.559s
> ok  	budget2/internal/handlers/dashboard	10.820s
> ok  	budget2/internal/handlers/duplicates	1.052s
> ok  	budget2/internal/handlers/explorer	11.196s
> ok  	budget2/internal/handlers/insights	7.993s
> ok  	budget2/internal/handlers/majorexpenses	3.231s
> ok  	budget2/internal/handlers/transfers	5.369s
> ok  	budget2/internal/handlers/whatif	33.737s
> ok  	budget2/internal/http	1.050s
> ok  	budget2/internal/models	1.051s
> ok  	budget2/internal/services/accounts	1.055s
44,73c44,73
< ok  	budget2/internal/services/anomalies	1.095s
< ok  	budget2/internal/services/backup	1.731s
< ok  	budget2/internal/services/classifier	1.038s
< ok  	budget2/internal/services/dataloader	2.147s
< ok  	budget2/internal/services/insights	1.041s
< ok  	budget2/internal/services/majorexpenses	1.035s
< ok  	budget2/internal/services/mcpsvc	1.523s
< ok  	budget2/internal/services/mcpsvc/admin	12.229s
< ok  	budget2/internal/services/mcpsvc/confirm	1.088s
< ok  	budget2/internal/services/mcpsvc/curate	2.665s
< ok  	budget2/internal/services/mcpsvc/ledger	1.912s
< ok  	budget2/internal/services/mcpsvc/plan	5.640s
< ok  	budget2/internal/services/mcpsvc/snapshot	1.039s
< ok  	budget2/internal/services/mcpsvc/spend	8.494s
< ok  	budget2/internal/services/merchants	1.036s
< ok  	budget2/internal/services/metrics	1.032s
< ok  	budget2/internal/services/pricecreep	1.036s
< ok  	budget2/internal/services/restore	2.201s
< ok  	budget2/internal/services/retirement	20.313s
< ok  	budget2/internal/services/retirement/analysis	18.304s
< ok  	budget2/internal/services/retirement/completeness	1.033s
< ok  	budget2/internal/services/retirement/engine	1.065s
< ok  	budget2/internal/services/retirement/history	1.037s
< ok  	budget2/internal/services/retirement/overrides	1.054s
< ok  	budget2/internal/services/retirement/prepare	1.045s
< ok  	budget2/internal/services/storage	231.062s
< ok  	budget2/internal/services/transfers	1.033s
< ok  	budget2/internal/templates	9.583s
< ok  	budget2/internal/testutil	1.033s
< ok  	budget2/internal/version	1.045s
---
> ok  	budget2/internal/services/anomalies	1.102s
> ok  	budget2/internal/services/backup	1.717s
> ok  	budget2/internal/services/classifier	1.044s
> ok  	budget2/internal/services/dataloader	2.130s
> ok  	budget2/internal/services/insights	1.053s
> ok  	budget2/internal/services/majorexpenses	1.036s
> ok  	budget2/internal/services/mcpsvc	1.555s
> ok  	budget2/internal/services/mcpsvc/admin	13.315s
> ok  	budget2/internal/services/mcpsvc/confirm	1.106s
> ok  	budget2/internal/services/mcpsvc/curate	3.054s
> ok  	budget2/internal/services/mcpsvc/ledger	1.931s
> ok  	budget2/internal/services/mcpsvc/plan	6.313s
> ok  	budget2/internal/services/mcpsvc/snapshot	1.049s
> ok  	budget2/internal/services/mcpsvc/spend	9.117s
> ok  	budget2/internal/services/merchants	1.034s
> ok  	budget2/internal/services/metrics	1.024s
> ok  	budget2/internal/services/pricecreep	1.043s
> ok  	budget2/internal/services/restore	2.153s
> ok  	budget2/internal/services/retirement	23.424s
> ok  	budget2/internal/services/retirement/analysis	20.451s
> ok  	budget2/internal/services/retirement/completeness	1.024s
> ok  	budget2/internal/services/retirement/engine	1.077s
> ok  	budget2/internal/services/retirement/history	1.036s
> ok  	budget2/internal/services/retirement/overrides	1.060s
> ok  	budget2/internal/services/retirement/prepare	1.066s
> ok  	budget2/internal/services/storage	209.759s
> ok  	budget2/internal/services/transfers	1.044s
> ok  	budget2/internal/templates	10.570s
> ok  	budget2/internal/testutil	1.064s
> ok  	budget2/internal/version	1.029s
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

RESOLUTION: winner = wt-primary, merged by patch (not by file copy). Both arms
passed the oracle (exit 0 / exit 0) and touched an identical nine files. The
production code of the two arms is BYTE-IDENTICAL: a diff across all six
production files yields zero non-comment differing lines. They differ only in
comment prose and in incidental test-fixture strings; both test files carry the
same twelve test functions with the same names and the same length. wt-primary
was taken on the sole remaining merit, documentation quality -- its comments
name the concrete failure (approving 500.00 while 5000.00 is written; the
opposite verdict on one transfer pair) where wt-alt's state the rule abstractly.

Process finding, recorded because it matters more than the choice: this Tier-3
produced no meaningful divergence and therefore bought almost nothing. The brief
pinned the exact Create/Find signatures -- necessary, because a shared oracle
must call a known API -- and additionally named all four call sites and the opID
to use. That over-constrained the task to the point where two blind
implementations could hardly differ. Contrast R1, where the brief pinned only
Mutate's signature and left the save path's internals free: there the two arms
diverged on whether OverlapWarnings survived, which is exactly the kind of
finding N-version exists to surface. Lesson for future Tier-3 assignment: pin
the oracle's API surface and nothing more. If the brief can only be satisfied
one way, use Tier 2 and spend the budget on verification instead.

Both arms independently discovered an error in the brief: it claimed
askForApproval/awaitApproval live only in ledger/approval.go, when
admin/approval.go holds a second, textually identical pair taking the admin
package's own Deps. Neither arm treated it as licence to widen scope; both made
the minimal edit needed to compile and flagged it. The brief has been corrected.

MERGE NOTE: this merge was NOT a file copy. The R4 worktrees branch from
1a8636d, which predates R1's and R7's accepted work, and R4 changes
ledger/anchor.go and ledger/resolve.go -- the same two files. Copying wt-primary
over the tree would have silently reverted R1's attempt-3 load-failure fix and
R7's write-path re-validation, and neither task's tests are in R4's oracle, so
nothing would have caught it. Seven files were applied with git apply --3way;
the two overlapping files were hand-merged by threading `token` through the four
askForApproval/awaitApproval call sites on top of the current content. Post-merge
greps confirm all three pieces of prior work survive: 4 "Failed to load accounts"
sites (R1), 1 "no longer a suspected transfer" guard (R7), 1 "cannot reload
accounts before writing" message (R1).
