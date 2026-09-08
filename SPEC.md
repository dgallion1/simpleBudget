# Roth recommendations with Apply

User approved showing optimizer recommendations inside the Roth Conversion Strategy card with an Apply action (2026-09-08). This is the bounded design selected in conversation; existing ACCESSIBILITY.md remains the numbered acceptance standard.

## Architecture and scope

Base: 344cb303. Work in /tmp/budget2-roth-apply; original source and running main/demo remain untouched during development. No data copies or settings edits on either running instance. No engine formula changes. Extend the existing Go optimizer/handler and HTMX card; use its ranked candidates, exact conversion schedules, and combined SS claim ages. Preserve incumbent typography, neutral surfaces, accent buttons, dark theme. Voice: plain, explicit about what Apply saves. Page inventory: /whatif Roth card and existing Tax Optimizer results.

## RA1 — Recommendations and Apply

Tier 2 (reversible application code; money outcomes require tests + second; UI requires a11y).

1. The Roth card offers an explicit Find recommendations action even with conversions disabled. No optimizer on page load or normal recalc. Render ranked recommendations in a compact responsive list inside this card, with strategy/window, SS claim ages, key metrics with existing formatters, and Apply actions. Reuse the existing optimizer; keep its detailed results available.
2. Apply must save the exact selected candidate's Roth configuration AND its associated SS claim ages; visible copy/button makes this scope explicit. Bracket-fill's converged per-year amounts must persist and survive reload/preparation; never substitute an average annual amount. No-conversion and fixed ladders work too. Do not change unrelated plan settings.
3. Keep server-issued recommendation identities bound to the exact saved settings used to generate them, local to the running instance. Reject missing, forged, expired or stale selection without saving. Recommendation generation is read-only. Do not trust posted amounts/ages as a recommendation. Bound cache size/lifetime if caching.
4. Saved per-year schedules must be clearly shown as schedules, with an explicit path back to fixed annual editing. Manual fixed-amount changes and conversion-sweep Apply must clear overrides so they take effect. Disabling/re-enabling and reload must be unambiguous. Enumerate JSON copying, preparation, canonical projection, MC/backtest and sweep consumers affected by persistence, and verify parity through existing seams.
5. Apply refreshes the Roth card, SS controls, and projection results consistently; announces success, preserves sensible focus, handles loading/error/stale states. No nested forms or duplicate IDs. Loading actions prevent duplicate submissions. Labels, keyboard operation, narrow layout and both themes follow ACCESSIBILITY.md.
6. Tests first: prove recommendation render/action, exact schedule persistence and deterministic projection parity, no-conversion/fixed behavior, stale/invalid rejection without mutation, unrelated settings preserved, manual/sweep override clearing. Run build, vet, tests, staticcheck and independent tests/second/a11y checks. No writes to live data; use synthetic test fixtures only.

Territory: internal/handlers/whatif/**, internal/models/whatif.go and model tests, internal/services/retirement/analysis/** and thin retirement facade if needed, retirement/chain.go, retirement/overrides/**, retirement/prepare/** for persisted schedule consumers, web/templates/components/whatif/{roth-conversion,tax-optimizer}.html and recommendation partial, web/static/js/whatif-roth-conversion.js, web/templates/pages/whatif.html, matching regression tests, generated CSS only if needed. No engine/storage/deployment code changes without escalating scope.

## Rulings

- Pre-verdict lead review caught a bracket-fill test fixture using 22 instead of the model’s 0.22 rate. Worker was asked to correct the units and assert nonzero varying conversions so parity cannot pass vacuously. Mechanism: lead review; not an accepted verifier result.
- Second checker identified the inclusive schedule end-year / chain-transition boundary as a probe before verdict; implementation must test it explicitly.

- Pre-verdict lead UI review found that a detail anchor would target a hidden results tab, and that MC-only labels must be conditional when no MC runs exist. Sent to worker before final verification. Mechanism: lead review.

- Lead staticcheck caught an unused revision binding in a new test (SA4006); worker correcting before verifier pass. Mechanism: staticcheck, lead verification.

- Attempt 1 CONCEDE: a11y checker proved Apply changes only a fragment, so no reload, success announcement or focus restoration occurs. Primary checker independently reproduced it. Mechanisms: a11y checker, primary checker. Attempt 2 must assert refreshed controls, explicit success text and focused heading in a real browser, not merely a navigation event.
- Attempt 1 CONCEDE: second checker proved conversion-sweep Apply saves a fixed amount but leaves the previous saved schedule rendered. Mechanism: second checker. Attempt 2 must refresh the card after guarded sweep save, retain sweep conflict guards, and promote the executable probe into a durable regression. Include historical backtest parity probe as durable test.

## RA2 — Durable checker probes

Tier 2, tests + second (test-only financial assertions). Promote independent TestRA1AdversarialSweepRefresh and TestRA1AdversarialBacktest into repository tests, preserving factual assertions and only adapting harness/names as needed. Ownership: internal/handlers/whatif/ra1_sweep_ui_test.go and internal/services/retirement/analysis/ra1_adversarial_test.go. Sweep probe must catch unchanged schedule UI after guarded fixed-amount Apply; historical probe must compare all nonempty historical sequence results for scored versus persisted/reloaded candidate schedules. No production changes. Both tests must pass on corrected RA1, and the sweep probe must fail against attempt1.

## Attempt 3 revised invalidation contract

RA1 escalated to Tier3 after two failed attempts. Last implementation attempt; halt if it fails. Lead/spec defect: treated every revision event as a plan change, despite the initial poll emitting the current unchanged revision. Mechanisms: a11y real-browser trace and primary Node execution. CONCEDE attempt2.

Every rendered recommendation set must carry its generating revision as data-roth-revision. Ignore malformed, equal and older revision notifications; only a strictly newer valid revision invalidates the displayed set. Preserve recommendations and focus on equal revision regardless of poll ordering. A genuine newer revision removes choices, announces the need to recompute, and returns focus to Find recommendations only if focus was inside removed content. Bind the revision from the server snapshot, not global polling state or listener order. Keep server-side hash/scenario/revision stale-save guards authoritative.

Executable oracle must cover unchanged/newer/malformed event behavior and rendered revision, plus all existing saved schedule consumers via exact config/JSON/preparation/canonical/MC/backtest/chain/manual/sweep/reload tests. Calibrate missing feature red and a throwaway correct prototype green before dispatch. Final real-browser immediate-Find race and Apply/schedule/fixed/stale checks remain mandatory.

Oracle calibration complete: accept.sh against unchanged attempt2 fails unchanged revision7 removal (factual symptom); throwaway corrected copy passes rendered revision and all selected consumer tests, final line ORACLE PASS. Logs: .swarm/roth-apply/tier3/RA1/calibration-red.log and calibration-green.log. Prototype discarded before dispatch.

Final browser fixture retained at .swarm/roth-apply/browser-fixture.json (synthetic DefaultWhatIfSettings-derived data, no household data). Actual optimizer ranks Fill12% bracket age60→73 for it. Preview uses only that fixture and repository test transactions on127.0.0.1:8082, separate data/backups/imports under /tmp/budget2-ra1-preview. Impeccable detect on all modified HTML/JS returned [] (no findings).
