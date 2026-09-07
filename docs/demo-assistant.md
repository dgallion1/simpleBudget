# Demonstrating household assistance

User intent (2026-09-06): demonstrate the assistant helping with the synthetic
retired couple while showcasing Dashboard and Insights.

## Data boundary

- Demo UI: http://localhost:8081. Demo MCP: http://localhost:8081/mcp.
- Before answering, call get_status on THAT endpoint and verify data_dir is
  /home/darrell/bin/ai/budget2-demo/data (or the explicitly declared isolated
  test copy). Abort demo tool use if it points elsewhere.
- The installed budget2 connector currently targets the REAL household. Opening
  a demo browser page does not retarget it. Do not use that connector for demo
  financial questions. Use a distinct demo connection/HTTP MCP session.
- Scope financial questions in this demo conversation to synthetic data unless
  the user explicitly changes the target. Do not pull real figures into examples.

## A short walkthrough

1. “Summarize how this household is doing for the displayed period.”
   Read spending totals and plan separately. Distinguish recorded cash flow from
   portfolio withdrawals and explain the data-through date.
2. “What explains the largest change in spending?”
   Use the same comparison periods as Insights and show supporting transactions.
   July 1–31, 2026 versus June 1–30 provides a complete-month example. The
   full March–August demo range does not have enough prior history to compare;
   explain that limitation instead of inventing a trend.
3. “Which recurring charges should I review?”
   Read detected subscriptions, bills and other recurring spending. State that
   estimated costs are not selected-period totals; do not promise savings.
4. “Is that appliance purchase unusual?”
   Explain its matched category and configured per-transaction expected range.
5. “What if we spent $300 more each month?”
   Read whatif://assumptions, get baseline analysis, then use run_scenario without
   saving. Describe the projection assumptions and difference from recorded data.
6. “Explain the spending guardrails.”
   Read saved settings/events and explain the specific cuts, raises and bounds.

## Changes during the demonstration

The application includes the server-scoped refresh_pages tool. Verify the
running instance with get_status and its available tools rather than assuming
an older running build includes it. Call it on the demo MCP endpoint after
authorized saved changes to request a reload of that server's open pages.
Dirty forms require user action; report “refresh requested” rather than
claiming all tabs refreshed. A demo request never signals the real server.
What-If also retains its saved-plan revision polling.

After deployment, reload each already-open demo tab once to load the refresh
listener. Later refresh_pages calls require no manual reload for clean visible
tabs. Hidden tabs check on return; tabs with edits offer an in-page notice near
the heading with Refresh page / Keep editing. Refreshing requires confirmation
to discard those edits. The notice must not cover keyboard focus. The tool
does not control browser tabs that have not loaded the listener.

Reads and unsaved scenario exploration are the default. Save plan settings,
change categories or resolve a duplicate only following a specific user request.
Honor guarded tools' preview and human-confirmation protocol. Never treat a
scenario run as a saved change or a pending duplicate as a confirmed error.

## Integration acceptance

Exercise demo MCP get_status, get_recurring and get_trends alongside the UI.
Confirm matching reference periods and classifications and verify synthetic
data paths. Demo tool actions must not change the 8080 real household or its
connector configuration. Separately authorized application releases may update
both servers' code, while preserving their distinct data and settings.
