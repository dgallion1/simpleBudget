// What-If Spending Phases: phase slider labels, spending-trajectory
// preview toggle, nominal/today's-dollar view switch. Extracted from
// components/whatif/spending-phases.html (U7).

function updatePhaseSliderLabel(slider) {
    var key = slider.dataset.quickAdjustKey;
    if (key) {
        var pctLabels = document.querySelectorAll('[data-quick-adjust-display="' + key + '"][data-quick-adjust-display-format="phase-percent"]');
        pctLabels.forEach(function(label) {
            label.textContent = Math.round(slider.value * 100) + '%';
        });

        var dollarLabels = document.querySelectorAll('[data-quick-adjust-display="' + key + '"][data-quick-adjust-display-format="phase-dollar"]');
        if (dollarLabels.length > 0) {
            // Canonical exact value (hidden field), not the step=100-snapped
            // visible range.
            var baseExpenses = parseFloat(document.getElementById('monthly_living_expenses_value')?.value || '0');
            var formatted = formatWholeDollars(slider.value * baseExpenses) + '/mo';
            dollarLabels.forEach(function(label) {
                label.textContent = formatted;
            });
        }
        return;
    }

    // Fallback for any legacy slider markup without quick-adjust hooks.
    var pctLabel = slider.nextElementSibling;
    if (!pctLabel) return;
    pctLabel.textContent = Math.round(slider.value * 100) + '%';

    var dollarLabel = pctLabel.nextElementSibling;
    if (dollarLabel && dollarLabel.classList.contains('phase-dollar-label')) {
        var baseExpenses = parseFloat(document.getElementById('monthly_living_expenses_value')?.value || '0');
        dollarLabel.textContent = formatWholeDollars(slider.value * baseExpenses) + '/mo';
    }
}

function togglePhaseInputs(enabled) {
    const config = document.getElementById('spending-phases-config');
    if (enabled) {
        config.classList.remove('opacity-50', 'pointer-events-none');
    } else {
        config.classList.add('opacity-50', 'pointer-events-none');
    }
}

function togglePhasePreview() {
    const panel = document.getElementById('phase-preview-panel');
    const toggleText = document.getElementById('phase-preview-toggle-text');
    if (panel.classList.contains('hidden')) {
        panel.classList.remove('hidden');
        toggleText.textContent = 'Hide';
        updatePhasePreview();
    } else {
        panel.classList.add('hidden');
        toggleText.textContent = 'Show';
    }
}

function setDollarView(mode) {
    const panel = document.getElementById('phase-preview-panel');
    const btnNominal = document.getElementById('btn-nominal');
    const btnToday = document.getElementById('btn-today');
    const note = document.getElementById('dollar-view-note');

    panel.classList.toggle('traj-view-real', mode === 'today');
    if (mode === 'nominal') {
        btnNominal.className = 'px-2 py-0.5 rounded text-xs bg-accent-strong text-white';
        btnToday.className = 'px-2 py-0.5 rounded text-xs bg-gray-200 dark:bg-gray-600 text-gray-700 dark:text-gray-300';
        note.classList.add('hidden');
    } else {
        btnNominal.className = 'px-2 py-0.5 rounded text-xs bg-gray-200 dark:bg-gray-600 text-gray-700 dark:text-gray-300';
        btnToday.className = 'px-2 py-0.5 rounded text-xs bg-accent-strong text-white';
        note.classList.remove('hidden');
    }
}

// Fetch the Spending Trajectory rows from the engine projection. The table
// is server-rendered from the same projection as the results column and
// re-fetched on every recalc (the OOB whatif-trajectory-refresh script).
function updatePhasePreview() {
    const panel = document.getElementById('phase-preview-panel');
    if (!panel || panel.classList.contains('hidden')) return;
    if (window.htmx) {
        htmx.ajax('GET', '/whatif/spending-trajectory', {target: '#phase-preview-body', swap: 'innerHTML'});
    }
}

// The preview-toggle and dollar-view buttons used to carry inline onclick=
// (U7).
document.addEventListener('DOMContentLoaded', function () {
    var toggleBtn = document.getElementById('phase-preview-toggle-btn');
    if (toggleBtn) toggleBtn.addEventListener('click', togglePhasePreview);
    document.querySelectorAll('[data-dollar-view]').forEach(function (btn) {
        btn.addEventListener('click', function () { setDollarView(btn.dataset.dollarView); });
    });
});
