# budget2 Analytics Port — SPEC

Status: AS-BUILT — port executed 2026-08-12; evidence in .swarm/
Origin: ports the *algorithms* (not the code) from the b2 analytics build
(`~/work/b2` branch `feat/analytics-v1`, see its ANALYTICS_SPEC.md Rulings).
Decision 2026-08-12: native Go, no Python sidecar — budget2 is a single
static binary with encryption-at-rest storage; data never leaves the process.

## 1. Scope

Add ONLY what budget2 lacks; do not duplicate existing analytics
(metrics, insights recurring detection, category trends, retirement
engine all stay as-is except where named below):

1. **Anomaly detection** (new) — the capability budget2 has none of.
2. **Price-creep detection** (new) — recurring charges drifting upward.
3. **Merchant-grouping hardening** (upgrade) — apply the ruled merge
   rules to the existing insights recurring detection.

Out of scope: forecasting (retirement engine + spending velocity cover
it), heatmaps/trends (exist), any storage or whatif changes.

## 2. The ruled algorithm standard (non-negotiable; from the b2 build)

These parameters survived three adversarial verification rounds; port
them exactly:

- **Merchant normalization**: uppercase, drop any standalone token that
  contains NO LETTERS (all-digit, punctuation-only, and digit/punctuation
  mixes like `#996581` — Ruling 2026-08-12b, generalizes the two earlier
  drop rules and collapses paper checks to one `CHECK` merchant group),
  collapse whitespace.
- **Merchant merge — token-subset rule with degenerate-key guard**: two
  normalized keys merge only when one token set ⊆ the other AND the
  smaller set has ≥ 2 tokens; 0/1-token keys merge on exact equality
  only (bare processor prefixes like `SQ` and empty keys from all-digit
  descriptions must not bridge groups under transitive union-find).
- **Anomalies — MAD with three hardening rules** on expenses
  (TransactionType == Outflow AND Amount < 0 — Ruling 2026-08-12; work in
  absolute values):
  - per-category (n ≥ 6) and per-merchant-group (n ≥ 4) robust z:
    |x − median| / (1.4826·MAD) > 3.5; MAD=0 fallback: x > 3×median;
  - (a) transactions in a qualifying recurring merchant group (n ≥ 4)
    are judged within that group only and EXCLUDED from the category
    baseline;
  - (b) materiality floor: |x − peer median| ≥ max(0.5×peer median, $10);
  - (c) high-side only: flag x > peer median; low-side is not an anomaly;
  - new-merchant flag: first occurrence of a merchant group in the
    window AND |amount| > p95 of window expenses;
  - dedupe multi-method hits to the highest-score method;
    severity high when score > 6, else medium.
- **Price-creep**: among recurring groups with ≥ 6 occurrences, compare
  median of first 3 vs median of last 3 absolute amounts; report
  increases > 5%. Decreases and single outliers must not report.

## 3. Architecture (follows budget2 conventions)

- New pure-function service package `internal/services/anomalies/`
  operating on `models.TransactionSet` (use `Active()`; respect
  `Suppressed`; key merchants off `DisplayName` falling back to
  `Description`; carry `Hash` through results as the join key).
- Shared merchant-grouping code factored so BOTH the anomalies service
  and the existing insights recurring detection use one implementation
  (`internal/services/merchants/` or similar) — the upgrade in scope
  item 3 is switching insights' description-merge to it.
- Handler: extend `internal/handlers/insights/` — two new sections on
  the insights page (Anomalies; Price creep), server-rendered
  `html/template` partials per the HTMX convention (full page +
  fragment routes). No new nav tab. Tables, not charts; no new JS deps.
- All data access stays in-process via the existing loader — the
  encryption boundary is never crossed.

## 4. Testing standard

- Table tests in Go, budget2 style. Port the planted-truth fixture
  approach: a `testutil` helper constructing a synthetic
  `TransactionSet` with documented planted properties (monthly NETFLIX
  with +20% stepped drift, twice-monthly payroll income, weekly
  SPOTIFY, exactly 5 high-side anomalies ≥5× category typical,
  background noise) — planted truths documented in the helper's
  comments; every detector test asserts against them.
- Regression tests carried from the b2 rulings: dominant-constant-charge
  category (MAD=0 collapse), immaterial small-group wobble, low-side
  outlier, bare `SQ` bridging triple, all-digit-description stray row,
  price-creep single-outlier fake, price-creep stepped genuine.
- Acceptance gate per task: `make check` (vet + staticcheck + govulncheck
  + full test suite) exits 0 — budget2's own pre-commit standard.

## 5. Task breakdown

| # | Task | Deliverable | Acceptance | Tier |
|---|------|-------------|-----------|------|
| P1 | Merchant grouping pkg | shared normalize/merge (subset rule + guard) + tests incl. bridging regressions | `make check` green; SQ-triple and all-digit regressions pass | 1 (dual checkers) |
| P2 | Anomalies service | `internal/services/anomalies/` MAD + rules a/b/c + new-merchant + tests on planted fixture | 5/5 planted flagged, ≤1 FP; rule regressions pass; `make check` green | 1 (dual checkers) |
| P3 | Price-creep + insights merge upgrade | creep detector; insights recurring switched to shared grouping; tests | creep catches planted drift, rejects fakes; existing insights tests stay green; `make check` | 1 (dual checkers) |
| P4 | Insights UI sections | templates + handler routes + fragment wiring | sections render with live data; empty states; a11y pass on new markup; `make check` | 1 |

Dependencies: P1 → P2 and P3 (parallel); P4 after P2+P3.
Verification: same swarm process (`.swarm/` ledger + agents2 gate.sh);
dual independent Anthropic checkers on P1–P3, plus the GLM
gateway-driver verdict on P2 (the algorithm-heaviest task).

## Rulings
(amendments during the build get logged here)
- 2026-08-12: P3 findings ruled: (1) insights hybrid ACCEPTED — legacy mergeSimilarGroups URL-suffix heuristic stays layered atop the ruled subset merge (orthogonal, preserves existing behavior incl. Lucid/Lucidmotors.com). (2) spaced-asterisk {SQ,*} 2-token bridge = VALID guard gap; fix ruled into normalization: standalone punctuation-only tokens are dropped like all-digit tokens (spec 2 amended); scheduled as task P5 (edits accepted P1 package + factors groupDisplayLabel into merchants + aligns pricecreep expense filter). (3) expense filter ruled: TransactionType==Outflow AND Amount<0 (app-native), replacing the b2-ported literal Amount<0 — applies to anomalies (P2) and pricecreep (P5 fix).
- 2026-08-12: P2 accepted (3/3: tests, second, glm). HIGH finding carried to P4 design: new_merchant must compute first-occurrence against FULL history (not the display window) so narrow windows do not chronically re-flag large recurring bills; display window only scopes which flags are shown.
- 2026-08-12: P5 accepted (dual PASS). Residual accepted risk logged by checker-second: ampersand-joined two-letter brands ("H & M" -> {H,M}) form generic 2-token keys that can subset-merge into unrelated token sets containing both letters — low probability (two coincidences required, no fixture triggers), inherent to the 2-token subset design; punch-listed for a future guard rather than fixed now.
- 2026-08-12: P6 attempt-1 FAIL adjudicated (checker-second, demonstrated): Transactions() derives the data dir from settingsDir without the basename=="settings" validation its sibling live.go spawnArgs enforces — a misconfigured -data flag silently yields count:0 instead of an error. UPHELD; returned to worker as attempt 2 (validate + clear error + regression test).
- 2026-08-12: P9 ruled from live-data observation: check-number tokens ("#996581" — digits+punctuation, no letters) survived normalization, making every paper check its own new_merchant flag (~12 of 65 live anomalies). Normalization rule generalized: drop any letterless standalone token. Checks now merge into one CHECK group (exact-equality, degenerate key) and are judged by mad_merchant like any recurring group.
