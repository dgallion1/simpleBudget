// What-If Portfolio Settings: range slider sync, living-expenses phase
// note live update. Extracted from
// components/whatif/portfolio-settings.html (U7).

function updatePortfolioRange(rangeStr, options) {
    const shouldTriggerChange = !options || options.triggerChange !== false;
    // WS1 R-FMT': initialization (load/after-swap) must not rewrite any
    // display/aria text the server just rendered — only the structural
    // min/max/step/select-bucket mirror sync. A genuine user bucket-select
    // change (the template's onchange=) leaves this true (default) so its
    // display/aria stay resynced, same as before.
    const shouldSyncDisplay = !options || options.syncDisplay !== false;
    if (options && options.sourceEvent && typeof options.sourceEvent.stopPropagation === 'function') {
        options.sourceEvent.stopPropagation();
    }
    const [min, max, step] = rangeStr.split(',').map(Number);
    const slider = document.getElementById('portfolio-slider');
    if (!slider) return;
    slider.min = min;
    slider.max = max;
    slider.step = step;
    // WS1 C5 R1: propagate the chosen range to every OTHER portfolio_value
    // mirror too (Quick Adjust's #qa-portfolio-value included). The
    // canonical control is now a hidden field with no min/max/step of its
    // own, so syncQuickAdjustMirrorControls (called below via
    // syncQuickAdjustKey) has nothing to copy from — without this loop the
    // Quick Adjust slider silently keeps stale $0-20M bounds while the
    // in-card slider and its <select> show the chosen preset.
    if (typeof getQuickAdjustMirrorControls === 'function') {
        getQuickAdjustMirrorControls('portfolio_value').forEach(function(mirror) {
            if (mirror === slider) return;
            mirror.min = min;
            mirror.max = max;
            mirror.step = step;
        });
    }
    const select = document.getElementById('portfolio-range');
    if (select && select.value !== rangeStr) select.value = rangeStr;
    const mirrorSelect = document.getElementById('quick-adjust-portfolio-range');
    if (mirrorSelect && mirrorSelect.value !== rangeStr) mirrorSelect.value = rangeStr;
    // Re-sync the slider (now a display-only mirror, WS1), its Quick Adjust
    // mirror, and both display spans from the canonical hidden field's
    // exact value — never by clamping/assigning the slider's OWN value,
    // which would silently rewrite the saved figure onto the new min/max/
    // step grid even though nothing here is a genuine user drag.
    if (shouldSyncDisplay) {
        if (typeof syncQuickAdjustKey === 'function') {
            syncQuickAdjustKey('portfolio_value');
        }
    } else if (typeof syncQuickAdjustMirrorOnly === 'function') {
        syncQuickAdjustMirrorOnly('portfolio_value');
    }
    // Trigger change event for HTMX
    if (shouldTriggerChange) {
        slider.dispatchEvent(new Event('change', { bubbles: true }));
    }
}

function initializePortfolioRange() {
    const slider = document.getElementById('portfolio-slider');
    if (!slider) return;
    const select = document.getElementById('portfolio-range');
    if (!select) return;
    updatePortfolioRange(select.value, { triggerChange: false, syncDisplay: false });
}

// updateLivingExpensesPhaseNote recomputes the phase note's dollar figure
// (the "Engine spends $X/mo now" text) as value × the current-phase
// multiplier stashed on the note's data-phase-multiplier attribute. The
// phase name and next-transition clause are static text set server-side —
// they don't change while dragging a single slider — only the dollar
// amount needs a live update.
function updateLivingExpensesPhaseNote(value) {
    const note = document.getElementById('living-expenses-phase-note');
    const amount = document.getElementById('living-expenses-phase-amount');
    if (!note || !amount) return;
    const multiplier = parseFloat(note.dataset.phaseMultiplier);
    if (!isFinite(multiplier)) return;
    const base = parseFloat(value) || 0;
    amount.textContent = '$' + (base * multiplier).toLocaleString('en-US', {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2
    });
}

// onMonthlyLivingExpensesInput handles the visible living-expenses range's
// oninput event. The range itself is not submitted (no name attribute) and
// snaps to $100 increments while dragging, which is fine for drag feel; the
// exact value only needs to be exact once dragging actually changes it. The
// hidden #monthly_living_expenses_value input is the one the form submits,
// and it is otherwise left untouched at its saved value (never
// browser-snapped, since hidden inputs aren't subject to range step
// sanitization) — this is what fixes the snap trap: an unrelated form
// submit with no drag on this slider round-trips the saved value exactly.
//
// Dispatching 'input' on the hidden field re-uses the existing quick-adjust
// sync machinery (quick-adjust-scripts.html) to update the display span,
// the quick-adjust mirror slider, and the phase note in one path — whether
// the change originated here or from a drag on the mirror slider.
function onMonthlyLivingExpensesInput(rawValue) {
    const exact = document.getElementById('monthly_living_expenses_value');
    if (exact) {
        exact.value = rawValue;
        exact.dispatchEvent(new Event('input', { bubbles: true }));
    }
    if (typeof updateSpendingPreview === 'function') updateSpendingPreview();
}

document.addEventListener('DOMContentLoaded', function() {
    initializePortfolioRange();
});

document.addEventListener('htmx:afterSettle', function(evt) {
    const target = evt.detail && evt.detail.target;
    if (target && (target.id === 'whatif-results' || target.id === 'whatif-portfolio-settings-card')) {
        initializePortfolioRange();
    }
});
