// Sitewide interaction helpers extracted from inline onclick= attributes
// (U7). Loaded on every page (layouts/base.html) so it covers overlay
// content injected by any page's HTMX swaps. Registered here (a
// synchronous, non-deferred script near the end of body) so it runs before
// any page-specific deferred script's OWN document click listeners —
// important for the stop-row-click guard below, which must win the race
// against a page's row-level click handler.

// Elements inside a clickable row/card that must NOT trigger that row's
// own click handler (a checkbox, a nested link, a select) carry
// data-stop-row-click instead of onclick="event.stopPropagation()".
// stopImmediatePropagation so it wins over every OTHER document-level
// click listener too, not just ancestors.
document.addEventListener('click', function (e) {
    if (e.target.closest('[data-stop-row-click]')) {
        e.stopImmediatePropagation();
    }
});

// Modal backdrop-click-to-close, shared by every full-page overlay
// container in the app: a backdrop element carries data-modal-backdrop
// and data-modal-close="<globalFunctionName>"; its panel carries
// data-modal-panel. A click that lands on the backdrop but NOT inside the
// panel calls the named close function (with no event argument — every
// close function here already treats a missing/undefined event as "close
// unconditionally", the same effective behavior as the removed
// onclick="closeXModal(event)" + onclick="event.stopPropagation()" pair).
document.addEventListener('click', function (e) {
    var backdrop = e.target.closest('[data-modal-backdrop]');
    if (!backdrop) return;
    if (e.target.closest('[data-modal-panel]')) return;
    var fnName = backdrop.getAttribute('data-modal-close');
    if (fnName && typeof window[fnName] === 'function') window[fnName]();
});

// Modal close buttons (the "x" in the header) carry data-modal-close-btn
// naming the same global close function, rather than onclick="closeX()".
document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-modal-close-btn]');
    if (!btn) return;
    var fnName = btn.getAttribute('data-modal-close-btn');
    if (fnName && typeof window[fnName] === 'function') window[fnName]();
});

// Unassigned-files banner (A8, on both dashboard and explorer): dismissal
// is remembered per count in sessionStorage so it is not re-announced on
// every page load within the session. The dismiss key used to be built
// from a Go-templated count baked into an inline <script>; it now reads
// the count from the rendered data-count attribute instead.
document.addEventListener('DOMContentLoaded', function () {
    var banner = document.getElementById('unassigned-banner');
    if (!banner) return;
    var key = 'unassigned-banner-dismissed-' + banner.getAttribute('data-count');
    try {
        if (sessionStorage.getItem(key) === '1') {
            banner.remove();
            return;
        }
    } catch (e) { /* sessionStorage may be unavailable; banner stays */ }
    var btn = document.getElementById('unassigned-banner-dismiss');
    if (btn) {
        btn.addEventListener('click', function () {
            try { sessionStorage.setItem(key, '1'); } catch (e) {}
            banner.remove();
        });
    }
});
