# SimpleBudget Accessibility Standard

Numbered standard for all UI work. Baseline: WCAG 2.2 AA. Checkers cite
point numbers in verdicts; "vibes" verdicts are invalid. Applies to every
page a task touches; the final pass runs it site-wide.

## Structure & semantics

1. Every page has exactly one `<h1>`, a logical heading hierarchy (no skipped
   levels), and landmark regions (`<main>`, `<nav>`, `<header>`).
2. Data tables use `<th>` with `scope`; no layout tables. Sortable or
   interactive headers are buttons, not bare clickable text.
3. Links navigate, buttons act. No `<div onclick>`; anything clickable is
   focusable and has a role.

## Forms

4. Every input has a programmatically associated `<label>` (or
   `aria-label` where a visible label is genuinely absent by design).
5. Validation errors are announced: error text is linked via
   `aria-describedby` and focus moves to (or an `aria-live` region announces)
   the first error on failed submit.
6. Required fields and input formats (dates, currency) are stated in text,
   not implied by placeholder alone.

## Color & contrast

7. Text contrast ≥ 4.5:1 (≥ 3:1 for large text); UI component boundaries and
   chart strokes ≥ 3:1 against background.
8. Meaning is never conveyed by color alone — status flags (low balance,
   stale data, FAIL/PASS) pair color with an icon or text.

## Keyboard & focus

9. All functionality is keyboard-operable; focus order follows visual order;
   focus indicator is visible (never `outline: none` without replacement).
10. After an HTMX swap that replaces the element with focus, focus is
    restored to a sensible element inside the swapped region; destructive or
    state-changing actions (confirm/reject, delete) announce their result via
    an `aria-live="polite"` region.

## Charts & dynamic content

11. Every Plotly chart has a text alternative: an adjacent data table (may be
    collapsed behind a disclosure) carrying the same values, and a text
    summary of the takeaway where the chart drives a decision.
12. Dark mode has full parity: all contrast requirements (7) hold in both
    themes; charts re-theme rather than staying light-on-dark.

## Project-specific

13. Currency amounts include the sign semantics in accessible text where
    sign carries meaning (e.g. `aria-label="withdrawal, $1,200"` when the UI
    shows bare `-1,200`).
14. Banners (unassigned files, staleness warnings) are dismissible via
    keyboard and re-announced only on state change, not every page load
    within a session.
15. No motion-based-only feedback; respects `prefers-reduced-motion` for any
    animated transition.

16. Client-side suppression is parity-complete. When a control hides,
    dismisses, or removes a visible affordance without a server round-trip,
    the SAME action must clear or update that affordance's assistive-
    technology counterpart — live-region text, `aria-*` state, and any hidden
    data node the announcement is derived from — so the accessibility tree
    never asserts a condition the page no longer shows, and a re-announcement
    never fires for an affordance the user can no longer see or act on. A
    dismissal that suppresses only the visual channel has not been granted to
    assistive-technology users at all.

    Added 2026-08-20 (user decision) on `judge-standards`' recommendation in
    the S4 dispute: findings 1 and 2 of `.swarm/verdicts/S4.3.checker-second.
    verdict` were real defects that no numbered point covered, so the FAIL
    rested on an "implicit companion requirement" and was overruled. This
    point makes that defect class citable.

17. Modal overlays are dialogs. Any layer that blocks the page carries
    `role="dialog"` (or `alertdialog`), `aria-modal="true"`, an accessible
    name (`aria-labelledby` or `aria-label`), moves focus into itself on
    open and back to the invoking control on close, traps Tab inside, and
    closes on Esc.

    Added 2026-09-03 (user decision, U-run Phase 0 sign-off): the 2026-09-03
    UI audit found four overlays with none of this markup, and no numbered
    point made the defect citable (the point-16 precedent).
