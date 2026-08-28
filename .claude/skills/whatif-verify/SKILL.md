---
name: whatif-verify
description: Use when asked to verify, try out, screenshot, or debug the what-if page (or any simpleBudget page) in a running app — e.g. "verify the what-if page", "try it in the browser", "check it renders", or when a change needs confirmation beyond unit tests.
---

# What-If Page Verify (cold-start server)

## Overview

`scripts/whatif-verify.sh` runs the server against a throwaway **copy** of
`data/` and manages its lifecycle. Never hand-roll the launch (no `pkill`,
no `sleep`, no ad-hoc temp dirs, no `go run` against real `data/`).

## Quick reference

| Task | Command |
|------|---------|
| Launch (builds, waits on `/api/health`) | `scripts/whatif-verify.sh start` |
| Page under test | `http://localhost:8099/whatif` |
| Server log | `scripts/whatif-verify.sh log` |
| Teardown + cleanup | `scripts/whatif-verify.sh stop` |
| Second instance | append a port, e.g. `start 8124` |

`start` is idempotent — it replaces a stale instance on the same port.

## Verification checklist (past real bugs)

HTTP 200 is not verification. Load `/whatif` in the browser (Playwright) and check:

- **Est. Taxes tile is populated** — it rendered empty when `Analysis.Tax`
  wasn't set (2026-06-14 bug).
- **Cost items (taxes, IRMAA, NIIT) are styled red/rose**, not neutral gray.
- **Tab scenario survives partial refresh** — switch tab, trigger a
  recalculation, confirm the tab's scenario didn't reset.
- Numbers match the retirement engine's law-accurate values — spot-check
  against `internal/services/retirement/analysis` expectations, not just
  "renders something".

## Common mistakes

- Verifying against the repo's real `data/` — the script's copy exists so
  the app can't mutate real data; don't bypass it.
- Killing the server with `pkill` — use `stop` (clean `/killme` shutdown).
- Sleeping a fixed 1.5s then curling — `start` already waits on readiness.
