// What-If Portfolio Settings: range slider sync, living-expenses phase
// note live update. Extracted from
// components/whatif/portfolio-settings.html (U7).

function updatePortfolioRange(rangeStr, options) {
    const shouldTriggerChange = !options || options.triggerChange !== false;
    if (options && options.sourceEvent && typeof options.sourceEvent.stopPropagation === 'function') {
        options.sourceEvent.stopPropagation();
    }
    const [min, max, step] = rangeStr.split(',').map(Number);
    const slider = document.getElementById('portfolio-slider');
    if (!slider) return;
    const currentVal = Number(slider.value);
    slider.min = min;
    slider.max = max;
    slider.step = step;
    // Clamp current value to new range
    if (currentVal < min) slider.value = min;
    else if (currentVal > max) slider.value = max;
    // Update display
    slider.nextElementSibling.textContent = formatWholeDollars(Number(slider.value));
    const select = document.getElementById('portfolio-range');
    if (select && select.value !== rangeStr) select.value = rangeStr;
    const mirrorSelect = document.getElementById('quick-adjust-portfolio-range');
    if (mirrorSelect && mirrorSelect.value !== rangeStr) mirrorSelect.value = rangeStr;
    if (typeof quickAdjustSyncFromCanonical === 'function') {
        quickAdjustSyncFromCanonical(slider);
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
    updatePortfolioRange(select.value, { triggerChange: false });
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
