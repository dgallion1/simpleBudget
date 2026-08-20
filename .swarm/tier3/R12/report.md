# Tier 3 divergence report — R12

| worktree | oracle exit |
|----------|-------------|
| wt-primary | 0 |
| wt-alt     | 0 |

## Divergence
### wt-primary output
```

### 1. build

### 2. vet

### 3. byDay must not be keyed by a raw time.Time
no raw time.Time map key — ok

### 4. blast-radius guard: dayOf, BalanceAt, Freshness must keep their contracts
balance.go unchanged — ok

### 5. lead-authored oracle tests under -race, in five timezones
-- TZ=UTC
ok  	budget2/internal/services/accounts	1.011s
-- TZ=America/New_York
ok  	budget2/internal/services/accounts	1.012s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/accounts	1.011s
-- TZ=Pacific/Midway
ok  	budget2/internal/services/accounts	1.012s
-- TZ=Pacific/Kiritimati
ok  	budget2/internal/services/accounts	1.012s

### 6. the accounts package's own suite under -race, in three timezones
-- TZ=UTC
ok  	budget2/internal/services/accounts	1.017s
-- TZ=America/New_York
ok  	budget2/internal/services/accounts	1.021s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/accounts	1.017s

### 7. accepted downstream behaviour must survive (R3, R5, dashboard/MCP parity)
-- TZ=UTC
ok  	budget2/internal/services/mcpsvc/ledger	1.787s
ok  	budget2/internal/handlers/dashboard	4.406s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/mcpsvc/ledger	1.805s
ok  	budget2/internal/handlers/dashboard	4.461s

### 8. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	41.495s
ok  	budget2/cmd/server	32.041s
ok  	budget2/cmd/validate	1.070s
ok  	budget2/internal/config	1.035s
ok  	budget2/internal/handlers/accounts	6.636s
ok  	budget2/internal/handlers/approval	1.030s
ok  	budget2/internal/handlers/backup	174.034s
ok  	budget2/internal/handlers/dashboard	11.962s
ok  	budget2/internal/handlers/duplicates	1.048s
ok  	budget2/internal/handlers/explorer	12.651s
ok  	budget2/internal/handlers/insights	8.061s
ok  	budget2/internal/handlers/majorexpenses	3.297s
ok  	budget2/internal/handlers/transfers	5.939s
ok  	budget2/internal/handlers/whatif	34.081s
ok  	budget2/internal/http	1.041s
ok  	budget2/internal/models	1.064s
ok  	budget2/internal/services/accounts	1.055s
ok  	budget2/internal/services/amazon	1.042s
ok  	budget2/internal/services/anomalies	1.115s
ok  	budget2/internal/services/backup	1.713s
ok  	budget2/internal/services/classifier	1.042s
ok  	budget2/internal/services/dataloader	2.207s
ok  	budget2/internal/services/insights	1.053s
ok  	budget2/internal/services/majorexpenses	1.038s
ok  	budget2/internal/services/mcpsvc	1.616s
ok  	budget2/internal/services/mcpsvc/admin	13.294s
ok  	budget2/internal/services/mcpsvc/confirm	1.102s
ok  	budget2/internal/services/mcpsvc/curate	2.956s
ok  	budget2/internal/services/mcpsvc/ledger	3.367s
ok  	budget2/internal/services/mcpsvc/plan	6.479s
ok  	budget2/internal/services/mcpsvc/snapshot	1.056s
ok  	budget2/internal/services/mcpsvc/spend	9.356s
ok  	budget2/internal/services/merchants	1.036s
ok  	budget2/internal/services/metrics	1.062s
ok  	budget2/internal/services/pricecreep	1.037s
ok  	budget2/internal/services/restore	2.209s
ok  	budget2/internal/services/retirement	23.903s
ok  	budget2/internal/services/retirement/analysis	21.086s
ok  	budget2/internal/services/retirement/completeness	1.037s
ok  	budget2/internal/services/retirement/engine	1.076s
ok  	budget2/internal/services/retirement/history	1.042s
ok  	budget2/internal/services/retirement/overrides	1.053s
ok  	budget2/internal/services/retirement/prepare	1.049s
ok  	budget2/internal/services/storage	207.909s
ok  	budget2/internal/services/transfers	1.062s
ok  	budget2/internal/templates	11.264s
ok  	budget2/internal/testutil	1.043s
ok  	budget2/internal/version	1.040s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### wt-alt output
```

### 1. build

### 2. vet

### 3. byDay must not be keyed by a raw time.Time
no raw time.Time map key — ok

### 4. blast-radius guard: dayOf, BalanceAt, Freshness must keep their contracts
balance.go unchanged — ok

### 5. lead-authored oracle tests under -race, in five timezones
-- TZ=UTC
ok  	budget2/internal/services/accounts	1.012s
-- TZ=America/New_York
ok  	budget2/internal/services/accounts	1.013s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/accounts	1.011s
-- TZ=Pacific/Midway
ok  	budget2/internal/services/accounts	1.012s
-- TZ=Pacific/Kiritimati
ok  	budget2/internal/services/accounts	1.012s

### 6. the accounts package's own suite under -race, in three timezones
-- TZ=UTC
ok  	budget2/internal/services/accounts	1.018s
-- TZ=America/New_York
ok  	budget2/internal/services/accounts	1.018s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/accounts	1.016s

### 7. accepted downstream behaviour must survive (R3, R5, dashboard/MCP parity)
-- TZ=UTC
ok  	budget2/internal/services/mcpsvc/ledger	1.779s
ok  	budget2/internal/handlers/dashboard	4.400s
-- TZ=Asia/Tokyo
ok  	budget2/internal/services/mcpsvc/ledger	1.777s
ok  	budget2/internal/handlers/dashboard	4.426s

### 8. full suite under -race (no regressions)
ok  	budget2/cmd/enrich-amazon	42.522s
ok  	budget2/cmd/server	32.244s
ok  	budget2/cmd/validate	1.062s
ok  	budget2/internal/config	1.041s
ok  	budget2/internal/handlers/accounts	6.476s
ok  	budget2/internal/handlers/approval	1.031s
ok  	budget2/internal/handlers/backup	172.353s
ok  	budget2/internal/handlers/dashboard	12.006s
ok  	budget2/internal/handlers/duplicates	1.037s
ok  	budget2/internal/handlers/explorer	12.430s
ok  	budget2/internal/handlers/insights	8.615s
ok  	budget2/internal/handlers/majorexpenses	3.390s
ok  	budget2/internal/handlers/transfers	6.138s
ok  	budget2/internal/handlers/whatif	34.169s
ok  	budget2/internal/http	1.054s
ok  	budget2/internal/models	1.077s
ok  	budget2/internal/services/accounts	1.086s
ok  	budget2/internal/services/amazon	1.046s
ok  	budget2/internal/services/anomalies	1.154s
ok  	budget2/internal/services/backup	1.721s
ok  	budget2/internal/services/classifier	1.066s
ok  	budget2/internal/services/dataloader	2.239s
ok  	budget2/internal/services/insights	1.072s
ok  	budget2/internal/services/majorexpenses	1.051s
ok  	budget2/internal/services/mcpsvc	1.621s
ok  	budget2/internal/services/mcpsvc/admin	13.608s
ok  	budget2/internal/services/mcpsvc/confirm	1.117s
ok  	budget2/internal/services/mcpsvc/curate	3.300s
ok  	budget2/internal/services/mcpsvc/ledger	3.317s
ok  	budget2/internal/services/mcpsvc/plan	6.295s
ok  	budget2/internal/services/mcpsvc/snapshot	1.055s
ok  	budget2/internal/services/mcpsvc/spend	9.571s
ok  	budget2/internal/services/merchants	1.045s
ok  	budget2/internal/services/metrics	1.045s
ok  	budget2/internal/services/pricecreep	1.034s
ok  	budget2/internal/services/restore	2.173s
ok  	budget2/internal/services/retirement	23.386s
ok  	budget2/internal/services/retirement/analysis	21.130s
ok  	budget2/internal/services/retirement/completeness	1.053s
ok  	budget2/internal/services/retirement/engine	1.076s
ok  	budget2/internal/services/retirement/history	1.048s
ok  	budget2/internal/services/retirement/overrides	1.102s
ok  	budget2/internal/services/retirement/prepare	1.084s
ok  	budget2/internal/services/storage	207.457s
ok  	budget2/internal/services/transfers	1.040s
ok  	budget2/internal/templates	11.031s
ok  	budget2/internal/testutil	1.040s
ok  	budget2/internal/version	1.036s
?   	budget2/web	[no test files]

=== accept.sh exit: 0 ===
```
### diff (primary vs alt)
```diff
14,15d13
< ok  	budget2/internal/services/accounts	1.011s
< -- TZ=America/New_York
16a15,16
> -- TZ=America/New_York
> ok  	budget2/internal/services/accounts	1.013s
26c26
< ok  	budget2/internal/services/accounts	1.017s
---
> ok  	budget2/internal/services/accounts	1.018s
28c28
< ok  	budget2/internal/services/accounts	1.021s
---
> ok  	budget2/internal/services/accounts	1.018s
30c30
< ok  	budget2/internal/services/accounts	1.017s
---
> ok  	budget2/internal/services/accounts	1.016s
34,35c34,35
< ok  	budget2/internal/services/mcpsvc/ledger	1.787s
< ok  	budget2/internal/handlers/dashboard	4.406s
---
> ok  	budget2/internal/services/mcpsvc/ledger	1.779s
> ok  	budget2/internal/handlers/dashboard	4.400s
37,38c37,38
< ok  	budget2/internal/services/mcpsvc/ledger	1.805s
< ok  	budget2/internal/handlers/dashboard	4.461s
---
> ok  	budget2/internal/services/mcpsvc/ledger	1.777s
> ok  	budget2/internal/handlers/dashboard	4.426s
41,79c41,79
< ok  	budget2/cmd/enrich-amazon	41.495s
< ok  	budget2/cmd/server	32.041s
< ok  	budget2/cmd/validate	1.070s
< ok  	budget2/internal/config	1.035s
< ok  	budget2/internal/handlers/accounts	6.636s
< ok  	budget2/internal/handlers/approval	1.030s
< ok  	budget2/internal/handlers/backup	174.034s
< ok  	budget2/internal/handlers/dashboard	11.962s
< ok  	budget2/internal/handlers/duplicates	1.048s
< ok  	budget2/internal/handlers/explorer	12.651s
< ok  	budget2/internal/handlers/insights	8.061s
< ok  	budget2/internal/handlers/majorexpenses	3.297s
< ok  	budget2/internal/handlers/transfers	5.939s
< ok  	budget2/internal/handlers/whatif	34.081s
< ok  	budget2/internal/http	1.041s
< ok  	budget2/internal/models	1.064s
< ok  	budget2/internal/services/accounts	1.055s
< ok  	budget2/internal/services/amazon	1.042s
< ok  	budget2/internal/services/anomalies	1.115s
< ok  	budget2/internal/services/backup	1.713s
< ok  	budget2/internal/services/classifier	1.042s
< ok  	budget2/internal/services/dataloader	2.207s
< ok  	budget2/internal/services/insights	1.053s
< ok  	budget2/internal/services/majorexpenses	1.038s
< ok  	budget2/internal/services/mcpsvc	1.616s
< ok  	budget2/internal/services/mcpsvc/admin	13.294s
< ok  	budget2/internal/services/mcpsvc/confirm	1.102s
< ok  	budget2/internal/services/mcpsvc/curate	2.956s
< ok  	budget2/internal/services/mcpsvc/ledger	3.367s
< ok  	budget2/internal/services/mcpsvc/plan	6.479s
< ok  	budget2/internal/services/mcpsvc/snapshot	1.056s
< ok  	budget2/internal/services/mcpsvc/spend	9.356s
< ok  	budget2/internal/services/merchants	1.036s
< ok  	budget2/internal/services/metrics	1.062s
< ok  	budget2/internal/services/pricecreep	1.037s
< ok  	budget2/internal/services/restore	2.209s
< ok  	budget2/internal/services/retirement	23.903s
< ok  	budget2/internal/services/retirement/analysis	21.086s
< ok  	budget2/internal/services/retirement/completeness	1.037s
---
> ok  	budget2/cmd/enrich-amazon	42.522s
> ok  	budget2/cmd/server	32.244s
> ok  	budget2/cmd/validate	1.062s
> ok  	budget2/internal/config	1.041s
> ok  	budget2/internal/handlers/accounts	6.476s
> ok  	budget2/internal/handlers/approval	1.031s
> ok  	budget2/internal/handlers/backup	172.353s
> ok  	budget2/internal/handlers/dashboard	12.006s
> ok  	budget2/internal/handlers/duplicates	1.037s
> ok  	budget2/internal/handlers/explorer	12.430s
> ok  	budget2/internal/handlers/insights	8.615s
> ok  	budget2/internal/handlers/majorexpenses	3.390s
> ok  	budget2/internal/handlers/transfers	6.138s
> ok  	budget2/internal/handlers/whatif	34.169s
> ok  	budget2/internal/http	1.054s
> ok  	budget2/internal/models	1.077s
> ok  	budget2/internal/services/accounts	1.086s
> ok  	budget2/internal/services/amazon	1.046s
> ok  	budget2/internal/services/anomalies	1.154s
> ok  	budget2/internal/services/backup	1.721s
> ok  	budget2/internal/services/classifier	1.066s
> ok  	budget2/internal/services/dataloader	2.239s
> ok  	budget2/internal/services/insights	1.072s
> ok  	budget2/internal/services/majorexpenses	1.051s
> ok  	budget2/internal/services/mcpsvc	1.621s
> ok  	budget2/internal/services/mcpsvc/admin	13.608s
> ok  	budget2/internal/services/mcpsvc/confirm	1.117s
> ok  	budget2/internal/services/mcpsvc/curate	3.300s
> ok  	budget2/internal/services/mcpsvc/ledger	3.317s
> ok  	budget2/internal/services/mcpsvc/plan	6.295s
> ok  	budget2/internal/services/mcpsvc/snapshot	1.055s
> ok  	budget2/internal/services/mcpsvc/spend	9.571s
> ok  	budget2/internal/services/merchants	1.045s
> ok  	budget2/internal/services/metrics	1.045s
> ok  	budget2/internal/services/pricecreep	1.034s
> ok  	budget2/internal/services/restore	2.173s
> ok  	budget2/internal/services/retirement	23.386s
> ok  	budget2/internal/services/retirement/analysis	21.130s
> ok  	budget2/internal/services/retirement/completeness	1.053s
81,88c81,88
< ok  	budget2/internal/services/retirement/history	1.042s
< ok  	budget2/internal/services/retirement/overrides	1.053s
< ok  	budget2/internal/services/retirement/prepare	1.049s
< ok  	budget2/internal/services/storage	207.909s
< ok  	budget2/internal/services/transfers	1.062s
< ok  	budget2/internal/templates	11.264s
< ok  	budget2/internal/testutil	1.043s
< ok  	budget2/internal/version	1.040s
---
> ok  	budget2/internal/services/retirement/history	1.048s
> ok  	budget2/internal/services/retirement/overrides	1.102s
> ok  	budget2/internal/services/retirement/prepare	1.084s
> ok  	budget2/internal/services/storage	207.457s
> ok  	budget2/internal/services/transfers	1.040s
> ok  	budget2/internal/templates	11.031s
> ok  	budget2/internal/testutil	1.040s
> ok  	budget2/internal/version	1.036s
```

<!-- Boss: after adjudicating, append a line starting 'RESOLUTION:' -->

RESOLUTION: winner = wt-primary, merged by patch. Both arms passed the corrected
oracle (exit 0 / exit 0) and touched the same two files, but unlike R4 and R11
they diverged on substance -- the first meaningful divergence since R1.

Both fixed all three defects and both normalised to UTC, as the user's decision
required. They differ in how the grid is keyed:

  wt-primary  a named calendarDayKey{year, month, day} type with a single
              utcCalendarDay(t) helper, used at both the write and the read.
  wt-alt      map[string]float64 with `.UTC().Format("2006-01-02")` written
              INLINE at each of the two call sites.

wt-primary wins on a ground this run has already paid to learn. wt-alt expresses
the key convention as a duplicated string literal at two sites, with nothing
forcing them to agree. That is precisely the defect class R11 cost three attempts
to fix: backup.SkipPredicate matched a ".tmp" literal that nothing tied to the
staging name atomicWrite actually produced, and it broke silently when the
producer changed. A named type and one helper make that drift impossible by
construction rather than by discipline.

Secondary, also favouring wt-primary: 184 lines of added tests against 65,
including a deterministic analogue of the oracle's real-clock check (fixed base
instant, day-10 offset chosen clear of the horizon boundary), an explicit
Pacific/Midway negative-offset case, and a pin that an occurrence on asOf's own
UTC day is skipped for that date while the series' next occurrence still applies.
Its comments also explain the time.Time-as-map-key Location trap, so the next
reader has to work to reintroduce it. A struct key additionally avoids per-day
string formatting inside a 35-iteration loop, which is free but not decisive.

ORACLE CORRECTION, recorded because it is the lead's error and both arms caught
it. Check 4's original fixture scheduled a monthly occurrence at nowUTCDay+5.
"monthly" resolves to a fixed 30-day interval and the horizon walk is
`for d := 1; d <= 35`, so the SECOND occurrence landed on exactly day 35 -- inside
the inclusive window -- making the true minimum -200.00, not the 400.00 asserted.
BOTH arms hit it, BOTH diagnosed it identically, and NEITHER took the one-line
shortcut of making day 35 exclusive, which would have gone green while breaking
the pre-existing accepted TestProject_HorizonIs35Days. The primary arm hand-traced
the arithmetic and wrote a standalone script confirming occ+5+30 == horizonEnd
exactly; the alt arm reported STATUS: INCOMPLETE rather than claiming success.
The fixture now uses day 10, so the second occurrence falls at day 40, outside the
window. The corrected oracle was copied into BOTH worktrees before this comparison
so the two arms were judged against identical contracts, and it was re-verified to
fail all four checks against the unfixed baseline.

That two blind arms independently rejected a lead-authored oracle, agreed on why,
and both declined the shortcut is the strongest evidence this run produced for
N-version having value -- more so than the implementation divergence itself.
