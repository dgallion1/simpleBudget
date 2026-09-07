# DI6 — requested refresh_pages MCP tool

User request 2026-09-06: “Can we add a force refresh tool” during demo assistant
showcase. Accepted scope: server-scoped refresh request for its own open pages,
protect unsaved edits. Tier2 tests,a11y (shared UI behavior, no financial writes).

Implement in new internal/services/uirefresh package, minimal wiring in
internal/services/mcpsvc/server.go and cmd/server/main.go, new shared
web/static/js/page-refresh.js and script inclusion in layouts/base.html.
Do not edit DI1/DI2 models, spending service, insights/dashboard handlers/pages,
CSS build or existing whatif polling. Focused new tests only in owned files;
MCP registered-tools inventory test update is allowed.

- Tool name refresh_pages, no arguments (all connected pages of THIS server).
  Return requested:true and revision, with honest wording: a refresh was
  requested, not proof every browser has reloaded. No persisted data writes.
- Shared in-memory coordinator per server, no filesystem counters or global
  cross-instance signal. Expose revision via same-origin read-only API; 2s
  polling with no-store, document visibility/focus recheck and overlap guard.
  An SSE implementation is acceptable if equally small and connection lifecycle
  is tested; report the chosen interface. No arbitrary URL navigation/remote JS.
- New tabs establish baseline without looping. Each later refresh event reloads
  a clean visible tab once, preserves query/hash via location.reload. Hidden
  tabs may defer until visible. Clean form submissions in flight defer refresh.
- Dirty form input (including unsaved What-If edits) must not silently disappear.
  Present an accessible refresh notice/button and confirm discarding edits on
  explicit click, or use a native confirmation dialog. A refusal preserves the
  form. Do not bypass existing What-If automatic revision polling safeguards.
- Repeated signals coalesce; failed/unsupported API does not reload-loop or
  spam dialogs. Server restart establishes a new epoch safely.
- Tool/route tests prove two coordinators isolate signals, monotonically changing
  revisions, no cached state, registered schema. Browser/JS test proves clean
  reload once, hidden deferral, dirty protection, preserved URL and no loop.
- Update MCP description that pages can now refresh on explicit refresh_pages.
  No misleading promise of immediate/successful delivery. Retain clear real/demo
  instance boundary. Add usage to docs/demo-assistant.md only through lead.

Write .swarm/DI-run/reports/DI6.1.md and manifests/DI6.1.files. No commits,
no subagents, no ledger/spec modifications. Before copy-based checks, coordinate
freeze with lead (DI1 worker has separate territory). Tests package-scoped;
lead runs full integration at final freeze. User gate rules override skill loops.
