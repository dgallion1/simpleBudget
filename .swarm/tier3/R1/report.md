# Tier 3 divergence report — R1

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## Divergence
### wt-primary output
```

### 1. build

### 2. vet

### 3. API surface — accounts.Mutate must exist with the pinned signature
internal/services/accounts/accounts.go:80:func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error {

### 4. no unserialized load-modify-save left at the call sites
-- accounts.Load in handlers/MCP (each hit must be inside a Mutate fn or a read-only path):
internal/handlers/accounts/handlers.go:483:	accts, err := accounts.Load(store)
internal/services/mcpsvc/ledger/anchor_test.go:42:	accts, err := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:85:	accts, err := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:128:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:209:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:233:	accts, _ = accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:297:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/accounts.go:76:		accts, err := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/accounts.go:220:		accts, err := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/register.go:137:	return accounts.Load(s.store)
internal/services/mcpsvc/ledger/transfers.go:100:			accts, aErr := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/anchor.go:90:		accts, err := deps.Accounts.LoadAccounts()
-- direct Save calls outside the accounts service (expect none):
none — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/accounts	1.337s

### 6. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	40.718s
ok  	budget2/cmd/server	31.335s
ok  	budget2/cmd/validate	1.084s
ok  	budget2/internal/config	1.057s
ok  	budget2/internal/handlers/accounts	5.613s
ok  	budget2/internal/handlers/approval	1.047s
ok  	budget2/internal/handlers/backup	177.359s
ok  	budget2/internal/handlers/dashboard	10.934s
ok  	budget2/internal/handlers/duplicates	1.079s
ok  	budget2/internal/handlers/explorer	11.791s
ok  	budget2/internal/handlers/insights	8.206s
ok  	budget2/internal/handlers/majorexpenses	3.124s
ok  	budget2/internal/handlers/transfers	5.760s
ok  	budget2/internal/handlers/whatif	33.891s
ok  	budget2/internal/http	1.057s
ok  	budget2/internal/models	1.048s
ok  	budget2/internal/services/accounts	1.047s
ok  	budget2/internal/services/amazon	1.037s
ok  	budget2/internal/services/anomalies	1.090s
ok  	budget2/internal/services/backup	1.690s
ok  	budget2/internal/services/classifier	1.062s
ok  	budget2/internal/services/dataloader	2.123s
ok  	budget2/internal/services/insights	1.052s
ok  	budget2/internal/services/majorexpenses	1.039s
ok  	budget2/internal/services/mcpsvc	1.535s
ok  	budget2/internal/services/mcpsvc/admin	13.163s
ok  	budget2/internal/services/mcpsvc/confirm	1.090s
ok  	budget2/internal/services/mcpsvc/curate	2.812s
ok  	budget2/internal/services/mcpsvc/ledger	2.290s
ok  	budget2/internal/services/mcpsvc/plan	6.442s
ok  	budget2/internal/services/mcpsvc/snapshot	1.036s
ok  	budget2/internal/services/mcpsvc/spend	9.512s
ok  	budget2/internal/services/merchants	1.049s
ok  	budget2/internal/services/metrics	1.049s
ok  	budget2/internal/services/pricecreep	1.059s
ok  	budget2/internal/services/restore	2.173s
ok  	budget2/internal/services/retirement	23.374s
ok  	budget2/internal/services/retirement/analysis	21.407s
ok  	budget2/internal/services/retirement/completeness	1.058s
ok  	budget2/internal/services/retirement/engine	1.121s
ok  	budget2/internal/services/retirement/history	1.044s
ok  	budget2/internal/services/retirement/overrides	1.071s
ok  	budget2/internal/services/retirement/prepare	1.064s
ok  	budget2/internal/services/storage	208.583s
ok  	budget2/internal/services/transfers	1.065s
ok  	budget2/internal/templates	10.671s
ok  	budget2/internal/testutil	1.053s
ok  	budget2/internal/version	1.039s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### wt-alt output
```

### 1. build

### 2. vet

### 3. API surface — accounts.Mutate must exist with the pinned signature
internal/services/accounts/accounts.go:56:func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error {

### 4. no unserialized load-modify-save left at the call sites
-- accounts.Load in handlers/MCP (each hit must be inside a Mutate fn or a read-only path):
internal/handlers/accounts/handlers.go:465:	accts, err := accounts.Load(store)
internal/services/mcpsvc/ledger/anchor_test.go:42:	accts, err := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:85:	accts, err := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:128:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:209:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:233:	accts, _ = accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/anchor_test.go:297:	accts, _ := accounts.Load(deps.Store)
internal/services/mcpsvc/ledger/accounts.go:76:		accts, err := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/accounts.go:220:		accts, err := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/register.go:137:	return accounts.Load(s.store)
internal/services/mcpsvc/ledger/transfers.go:100:			accts, aErr := deps.Accounts.LoadAccounts()
internal/services/mcpsvc/ledger/anchor.go:84:		accts, err := deps.Accounts.LoadAccounts()
-- direct Save calls outside the accounts service (expect none):
none — ok

### 5. lead-authored oracle tests under -race
ok  	budget2/internal/services/accounts	1.331s

### 6. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	38.382s
ok  	budget2/cmd/server	29.596s
ok  	budget2/cmd/validate	1.073s
ok  	budget2/internal/config	1.042s
ok  	budget2/internal/handlers/accounts	5.415s
ok  	budget2/internal/handlers/approval	1.056s
ok  	budget2/internal/handlers/backup	166.751s
ok  	budget2/internal/handlers/dashboard	10.636s
ok  	budget2/internal/handlers/duplicates	1.046s
ok  	budget2/internal/handlers/explorer	10.756s
ok  	budget2/internal/handlers/insights	7.489s
ok  	budget2/internal/handlers/majorexpenses	2.919s
ok  	budget2/internal/handlers/transfers	5.267s
ok  	budget2/internal/handlers/whatif	31.278s
ok  	budget2/internal/http	1.037s
ok  	budget2/internal/models	1.057s
ok  	budget2/internal/services/accounts	1.047s
ok  	budget2/internal/services/amazon	1.030s
ok  	budget2/internal/services/anomalies	1.103s
ok  	budget2/internal/services/backup	1.707s
ok  	budget2/internal/services/classifier	1.038s
ok  	budget2/internal/services/dataloader	2.148s
ok  	budget2/internal/services/insights	1.043s
ok  	budget2/internal/services/majorexpenses	1.042s
ok  	budget2/internal/services/mcpsvc	1.549s
ok  	budget2/internal/services/mcpsvc/admin	12.740s
ok  	budget2/internal/services/mcpsvc/confirm	1.094s
ok  	budget2/internal/services/mcpsvc/curate	2.859s
ok  	budget2/internal/services/mcpsvc/ledger	1.929s
ok  	budget2/internal/services/mcpsvc/plan	5.803s
ok  	budget2/internal/services/mcpsvc/snapshot	1.044s
ok  	budget2/internal/services/mcpsvc/spend	8.950s
ok  	budget2/internal/services/merchants	1.043s
ok  	budget2/internal/services/metrics	1.044s
ok  	budget2/internal/services/pricecreep	1.045s
ok  	budget2/internal/services/restore	2.181s
ok  	budget2/internal/services/retirement	21.201s
ok  	budget2/internal/services/retirement/analysis	19.313s
ok  	budget2/internal/services/retirement/completeness	1.040s
ok  	budget2/internal/services/retirement/engine	1.085s
ok  	budget2/internal/services/retirement/history	1.034s
ok  	budget2/internal/services/retirement/overrides	1.084s
ok  	budget2/internal/services/retirement/prepare	1.071s
ok  	budget2/internal/services/storage	198.298s
ok  	budget2/internal/services/transfers	1.035s
ok  	budget2/internal/templates	9.889s
ok  	budget2/internal/testutil	1.050s
ok  	budget2/internal/version	1.035s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### diff (primary vs alt)
```diff
7c7
< internal/services/accounts/accounts.go:80:func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error {
---
> internal/services/accounts/accounts.go:56:func Mutate(s *storage.Storage, fn func([]models.Account) ([]models.Account, error)) error {
11c11
< internal/handlers/accounts/handlers.go:483:	accts, err := accounts.Load(store)
---
> internal/handlers/accounts/handlers.go:465:	accts, err := accounts.Load(store)
22c22
< internal/services/mcpsvc/ledger/anchor.go:90:		accts, err := deps.Accounts.LoadAccounts()
---
> internal/services/mcpsvc/ledger/anchor.go:84:		accts, err := deps.Accounts.LoadAccounts()
27c27
< ok  	budget2/internal/services/accounts	1.337s
---
> ok  	budget2/internal/services/accounts	1.331s
30,45c30,45
< ok  	budget2/cmd/enrich-amazon	40.718s
< ok  	budget2/cmd/server	31.335s
< ok  	budget2/cmd/validate	1.084s
< ok  	budget2/internal/config	1.057s
< ok  	budget2/internal/handlers/accounts	5.613s
< ok  	budget2/internal/handlers/approval	1.047s
< ok  	budget2/internal/handlers/backup	177.359s
< ok  	budget2/internal/handlers/dashboard	10.934s
< ok  	budget2/internal/handlers/duplicates	1.079s
< ok  	budget2/internal/handlers/explorer	11.791s
< ok  	budget2/internal/handlers/insights	8.206s
< ok  	budget2/internal/handlers/majorexpenses	3.124s
< ok  	budget2/internal/handlers/transfers	5.760s
< ok  	budget2/internal/handlers/whatif	33.891s
< ok  	budget2/internal/http	1.057s
< ok  	budget2/internal/models	1.048s
---
> ok  	budget2/cmd/enrich-amazon	38.382s
> ok  	budget2/cmd/server	29.596s
> ok  	budget2/cmd/validate	1.073s
> ok  	budget2/internal/config	1.042s
> ok  	budget2/internal/handlers/accounts	5.415s
> ok  	budget2/internal/handlers/approval	1.056s
> ok  	budget2/internal/handlers/backup	166.751s
> ok  	budget2/internal/handlers/dashboard	10.636s
> ok  	budget2/internal/handlers/duplicates	1.046s
> ok  	budget2/internal/handlers/explorer	10.756s
> ok  	budget2/internal/handlers/insights	7.489s
> ok  	budget2/internal/handlers/majorexpenses	2.919s
> ok  	budget2/internal/handlers/transfers	5.267s
> ok  	budget2/internal/handlers/whatif	31.278s
> ok  	budget2/internal/http	1.037s
> ok  	budget2/internal/models	1.057s
47,77c47,77
< ok  	budget2/internal/services/amazon	1.037s
< ok  	budget2/internal/services/anomalies	1.090s
< ok  	budget2/internal/services/backup	1.690s
< ok  	budget2/internal/services/classifier	1.062s
< ok  	budget2/internal/services/dataloader	2.123s
< ok  	budget2/internal/services/insights	1.052s
< ok  	budget2/internal/services/majorexpenses	1.039s
< ok  	budget2/internal/services/mcpsvc	1.535s
< ok  	budget2/internal/services/mcpsvc/admin	13.163s
< ok  	budget2/internal/services/mcpsvc/confirm	1.090s
< ok  	budget2/internal/services/mcpsvc/curate	2.812s
< ok  	budget2/internal/services/mcpsvc/ledger	2.290s
< ok  	budget2/internal/services/mcpsvc/plan	6.442s
< ok  	budget2/internal/services/mcpsvc/snapshot	1.036s
< ok  	budget2/internal/services/mcpsvc/spend	9.512s
< ok  	budget2/internal/services/merchants	1.049s
< ok  	budget2/internal/services/metrics	1.049s
< ok  	budget2/internal/services/pricecreep	1.059s
< ok  	budget2/internal/services/restore	2.173s
< ok  	budget2/internal/services/retirement	23.374s
< ok  	budget2/internal/services/retirement/analysis	21.407s
< ok  	budget2/internal/services/retirement/completeness	1.058s
< ok  	budget2/internal/services/retirement/engine	1.121s
< ok  	budget2/internal/services/retirement/history	1.044s
< ok  	budget2/internal/services/retirement/overrides	1.071s
< ok  	budget2/internal/services/retirement/prepare	1.064s
< ok  	budget2/internal/services/storage	208.583s
< ok  	budget2/internal/services/transfers	1.065s
< ok  	budget2/internal/templates	10.671s
< ok  	budget2/internal/testutil	1.053s
< ok  	budget2/internal/version	1.039s
---
> ok  	budget2/internal/services/amazon	1.030s
> ok  	budget2/internal/services/anomalies	1.103s
> ok  	budget2/internal/services/backup	1.707s
> ok  	budget2/internal/services/classifier	1.038s
> ok  	budget2/internal/services/dataloader	2.148s
> ok  	budget2/internal/services/insights	1.043s
> ok  	budget2/internal/services/majorexpenses	1.042s
> ok  	budget2/internal/services/mcpsvc	1.549s
> ok  	budget2/internal/services/mcpsvc/admin	12.740s
> ok  	budget2/internal/services/mcpsvc/confirm	1.094s
> ok  	budget2/internal/services/mcpsvc/curate	2.859s
> ok  	budget2/internal/services/mcpsvc/ledger	1.929s
> ok  	budget2/internal/services/mcpsvc/plan	5.803s
> ok  	budget2/internal/services/mcpsvc/snapshot	1.044s
> ok  	budget2/internal/services/mcpsvc/spend	8.950s
> ok  	budget2/internal/services/merchants	1.043s
> ok  	budget2/internal/services/metrics	1.044s
> ok  	budget2/internal/services/pricecreep	1.045s
> ok  	budget2/internal/services/restore	2.181s
> ok  	budget2/internal/services/retirement	21.201s
> ok  	budget2/internal/services/retirement/analysis	19.313s
> ok  	budget2/internal/services/retirement/completeness	1.040s
> ok  	budget2/internal/services/retirement/engine	1.085s
> ok  	budget2/internal/services/retirement/history	1.034s
> ok  	budget2/internal/services/retirement/overrides	1.084s
> ok  	budget2/internal/services/retirement/prepare	1.071s
> ok  	budget2/internal/services/storage	198.298s
> ok  	budget2/internal/services/transfers	1.035s
> ok  	budget2/internal/templates	9.889s
> ok  	budget2/internal/testutil	1.050s
> ok  	budget2/internal/version	1.035s
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

RESOLUTION: winner = wt-primary, merged unmodified. Both arms passed the oracle
(exit 0 / exit 0) and touched an identical four files; the only diffs in oracle
output were line numbers and test timings, so the oracle could not discriminate
and the choice was made by reading both implementations.

Deciding difference: Save's observable contract. Save delegates to
SaveWithWarnings, which runs the nil-storage guard, Validate, and
OverlapWarnings(accts, existingCSVBasenames(s)), and Save logs each warning.
wt-primary's saveTx reproduces all of it (Validate -> OverlapWarnings ->
marshal -> tx.WriteFile -> log). wt-alt's Mutate inlines only Validate ->
marshal -> write, dropping OverlapWarnings and the logging entirely, and omits
the nil-storage guard. That is a silent behaviour regression on every account
save through both the UI and MCP: the pattern-overlap warning that tells a user
two accounts claim the same CSV would simply stop appearing. The oracle did not
catch it because it never asserted on warnings — a gap in the oracle, recorded
here so the next Tier-3 task's accept.sh covers observable side effects and not
only return values.

Secondary, non-deciding: wt-primary preserved the 404/not-found paths across the
load+edit+save collapse by introducing sentinel errors (errAccountNotFound,
errAnchorNotFound, errAnchorAccountVanished) rather than letting the closure
swallow the distinction. wt-alt did not need equivalents because it kept those
checks outside Mutate, which is also defensible; this did not decide the choice.

Convergence worth recording: both arms independently converted two call sites
the brief did not enumerate -- handleDelete (whole-account delete, same
load-modify-save shape) and storageAccountStore.SaveAccounts in
mcpsvc/ledger/register.go. Both are correct under the brief's blanket rule, and
accept.sh step 4 flags the register.go site directly. The lead's earlier
suspicion that this was scope creep by one arm was wrong.
