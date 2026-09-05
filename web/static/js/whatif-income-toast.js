// What-If income Undo toast (U13). The toast markup itself -- including its
// Undo button's hx-post to the existing /whatif/income/{id}/restore endpoint
// -- is rendered server-side by "whatif-removed-income-sources"
// (income-sources-list.html) and swapped in via hx-swap-oob after a remove
// or restore. This file only owns client-side dismissal: HTMX has no
// built-in "hide this element" action, and dismissal must not require a
// round trip (the toast is purely a session-local UI affordance; the
// underlying data already lives in RemovedIncomeSources either way).

function dismissIncomeRemovedToast() {
    var toast = document.getElementById('income-removed-toast');
    if (!toast || toast.classList.contains('hidden')) return;
    // classList.add('hidden') sets display:none, which removes the toast
    // from the accessibility tree along with the visual channel in one step
    // (ACCESSIBILITY.md #16: client-side suppression must be parity-complete
    // between the visual and assistive-technology channels).
    toast.classList.add('hidden');
}

document.addEventListener('click', function (event) {
    var dismissBtn = event.target.closest && event.target.closest('[data-income-toast-dismiss]');
    if (dismissBtn) dismissIncomeRemovedToast();
});

document.addEventListener('keydown', function (event) {
    if (event.key !== 'Escape') return;
    var toast = document.getElementById('income-removed-toast');
    if (toast && !toast.classList.contains('hidden')) dismissIncomeRemovedToast();
});

// The Undo button lives inside the toast, and Undo's own response OOB-swaps
// the toast's outerHTML (hiding it again, since the restored source drops
// out of RemovedIncomeSources). That swap replaces the element holding
// focus, so move focus to the income sources list first -- a sensible,
// still-visible landing spot (ACCESSIBILITY.md #10: after a swap that
// replaces the focused element, focus is restored to a sensible element
// inside the swapped region).
document.body.addEventListener('htmx:oobBeforeSwap', function (event) {
    var target = event.target;
    if (!target || target.id !== 'income-removed-toast') return;
    if (!target.contains(document.activeElement)) return;
    var list = document.getElementById('income-sources-list');
    if (list) list.focus();
});
