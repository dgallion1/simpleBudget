# Guardrail optimizer accessibility standard

The application standard at `/home/darrell/bin/ai/budget2/ACCESSIBILITY.md`
points 1–17 applies in full (WCAG 2.2 AA). This run adds:

18. Floor and success-target controls have visible labels, units, help text, and linked
validation errors. Today's dollars and whole-horizon probability are explicit.
19. Presets and custom success target are keyboard operable with an announced selected
state. Invalid custom input cannot silently select a different success target.
20. Search progress, cancellation, errors, no-qualifying-result states, and
Apply success are announced without repeated noisy live-region updates.
21. The comparison is a semantic table with headers. Risk status uses text,
not color alone. All dollar amounts identify real versus nominal units.
22. Applying a result and HTMX refresh preserve sensible keyboard focus.
Stale or disabled results expose both the disabled state and its reason.
23. The result table remains usable at 200% zoom and narrow viewport widths;
horizontal scrolling is confined to the table, with a keyboard-accessible
container when necessary. Light and dark themes meet the app contrast rules.
