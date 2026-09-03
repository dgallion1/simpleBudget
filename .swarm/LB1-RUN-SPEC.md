# LB1 — low-balance threshold gated by account kind

Authorized 2026-09-02 ("Run it end to end") — finishes the abandoned
sleepy-kilby worktree's work (untouched since 2026-08-29, ported clean
onto master d3309d9; none of the six files had drifted). Closes the
NEXT.md backlog item "low_balance flags credit-kind accounts
permanently" (chip task_3f52c4ef).

## Contract
One shared gate, internal/services/accounts LowBalanceApplies(kind):
true only for checking and savings. Every surface reporting a
low-balance flag or funding projection consumes it:
- dashboard accounts card (internal/handlers/dashboard/accounts_card.go)
- MCP get_accounts (mcpsvc/ledger/accounts.go — docstring updated to
  state the kind rule and that non-applicable kinds report
  low_balance=false, threshold=0)
- MCP get_balance_projection (same package)
Credit-kind (negative-by-nature) and brokerage (non-cash) accounts must
never be flagged, regardless of threshold configured.

## Tier 2, checks: tests,second (split-classification history — a
threshold/classification on a money figure across three surfaces; the
second lane enumerates surfaces and hunts stale docstrings, the ND3
class).

## Provenance
Implementation authored by the abandoned sleepy-kilby session
(worker-coder era, no manifest/verdicts left); lead ported the diff
verbatim (16 hunks, 3-way, clean) and verified build/vet/full-suite
green before dispatching verification. Ledger worker=worker-coder,
reason records the port.

## Attempt 1 verdicts + attempt 2 remediation (2026-09-02)
- checker-tests PASS: every surface's credit-kind case individually
  pinned; the three new behavioral tests FAIL on pristine master (real
  change proven); single classifier confirmed (4 call sites, no bypass).
  Findings F1-F4 (README staleness — folded into attempt 2; duplicated
  default-threshold constant + /accounts page offering the input for
  inert kinds + no port manifest — backlog).
- checker-second FAIL (CONCEDED, ND3 stale-doc class): models/account.go
  + GLOSSARY.md still claimed AccountKind has "one behavioral
  consequence, and only one" — now false. REAL-DATA proof of the fix's
  value: usaa-credit-card was live-flagged low_balance=true with a
  $15,500 suggested top-up for a -$11,371 card; the fix errors by kind.
- Attempt 2 (lead-authored doc remediation, no code changes):
  account.go kind doc → two consequences; GLOSSARY.md "Account kind"
  updated; README get_accounts/get_balance_projection sections updated
  (folding checker-tests F1); budget2-mcp skill reference
  accounts-transfers.md gains the kind rule (third "rule that prevents
  wrong answers" + cash-kinds-only note on the projection).

## Ruling LB1-2026-09-02a — T18 contract rewrite before attempt 3
Attempts 1 AND 2 failed to the SAME defect class (ND3 stale docs), each
time on a surface the spec enumerated piecemeal. Per the T18 precedent
that is a LEAD/SPEC defect: the contract lacked a completeness
procedure. REWRITTEN doc contract for attempt 3 (X8 lesson — sweep
completeness is grep-the-token, never spot-check):
- The doc surface set IS the output of
  `grep -rl "low.balance|low_balance|LowBalance|balance_projection|balance projection"`
  over *.go and *.md (tests and .swarm excluded), classified once:
  LIVING surfaces (code comments, docstrings, serverInstructions,
  GLOSSARY, README, skill references, SKILL.md) must state the kind
  gate wherever they describe threshold/flag/projection behavior;
  HISTORICAL surfaces (dated build specs: ACCOUNTS_TRANSFERS_SPEC,
  REVIEW_*_SPEC, docs/superpowers/*) are records and stay; NON-CLAIMS
  (ACCESSIBILITY.md's color example, form-validation strings) stay.
- Attempt-3 remediation: serverInstructions (mcpsvc/server.go — the
  text returned to every MCP client on initialize) updated with the
  kind gate and the projection's cash-kinds-only refusal. Full sweep
  re-run; no other LIVING surface found stale.
- Acceptance criterion for the second lane at attempt 3: re-run the
  SAME grep, classify every hit, zero stale LIVING surfaces remain.

## Attempt 3: dual-lane PASS, accepted (2026-09-02)
- checker-second: ruling grep re-run and fully classified (8 accurate
  living-with-claim, 2 living-no-claim, 4 historical, 1 non-claim), zero
  stale living surfaces. checker-tests: byte-identity for all standing
  criteria; the serverInstructions change proven comment/const-only via
  go/parser declaration diff; mutation re-killed on the final tree.
- Lane disagreement adjudicated (both PASS, no panel): handlers.go:84-88
  default-threshold comment — second lane classified non-claim, primary
  flagged the word "everywhere" as stale. LEAD: left as-is (the
  default-substitution mechanic it describes still operates; the value
  is inert downstream for non-cash kinds). With F6 (no wants-list pin
  for the new serverInstructions claim) → scoped follow-up candidate
  LB2: two one-line changes, V3 pattern.
- Lead process notes: (1) the lead edited server.go mid-attempt-2
  verification without a freeze handshake — checker-tests caught the
  moving tree, re-baselined, and disclosed it; future lead remediations
  wait for in-flight lanes. (2) escalate-scan regenerated Z5.flag: CB6's
  sweep of it was WRONG (it is a standing artifact of the Z-run tier-2
  row vs the current critical.globs, tolerated by every historical
  gate.sh done); ND3.flag stayed gone, that half of the sweep was right.
  Z5.flag is restored by this commit and stays.
