// Insights page: date-range quick buttons/step arrows, recurring/trends
// table sorting, and description-cell navigation. Extracted from
// pages/insights.html (U7).

// Controls that used to carry inline onclick= (U7): the shift-window
// arrows, sortable table headers (data-sort-fn names the function, since
// this page has two independently-sortable tables), and the row/tile
// elements that navigate to the Explorer with a filter — delegated since
// these rows are re-rendered by htmx swaps.
document.addEventListener('input', function (e) {
    if (e.target.matches('#insights-date-filter input[type="date"]')) {
        e.target.form.querySelector('input[name="preset"]').value = '';
    }
});

document.addEventListener('click', function (e) {
    const step = e.target.closest('#insights-date-filter [data-step]');
    if (step) { shiftInsightWindow(parseInt(step.dataset.step, 10)); return; }
    const preset = e.target.closest('#insights-date-filter .insight-preset-btn[data-preset]');
    if (preset) { setInsightPreset(preset.dataset.preset); return; }
    var trendsToggle = e.target.closest('[data-trends-toggle]');
    if (trendsToggle) { toggleTrendsCap(trendsToggle); return; }
    var sortEl = e.target.closest('[data-sort-fn]');
    if (sortEl) {
        var fn = sortEl.getAttribute('data-sort-fn');
        var col = sortEl.getAttribute('data-sort');
        if (fn === 'sortRecurringTable') sortRecurringTable(col);
        else if (fn === 'sortTrendsTable') sortTrendsTable(col);
        return;
    }
    var navEl = e.target.closest('[data-navigate-href]');
    if (navEl) {
        window.location.href = navEl.getAttribute('data-navigate-href');
    }
});

// The recurring/income/category-trends rows above carry
// data-navigate-href + tabindex="0" role="link" (U7 attempt 2, ruling
// U-2026-09-04l): a <tr> cannot become a native <a>, so keyboard
// activation is wired here — Enter (link convention) and Space (so the
// row also behaves like the button/card idiom used elsewhere on this
// page) both navigate, matching the click behavior above exactly. Space
// is prevented from scrolling the page, same as a native control.
document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return;
    var navEl = e.target.closest('[data-navigate-href][role="link"]');
    if (!navEl) return;
    if (e.target !== navEl) return; // don't hijack Enter/Space typed into a nested control
    e.preventDefault();
    window.location.href = navEl.getAttribute('data-navigate-href');
});

// Recurring payments table sorting
let recurringSortState = { column: null, ascending: true };

function sortRecurringTable(column) {
    const table = document.getElementById('recurring-payments-table');
    if (!table) return;

    const tbody = table.querySelector('tbody');
    const rows = Array.from(tbody.querySelectorAll('tr'));

    // Toggle direction if same column clicked
    if (recurringSortState.column === column) {
        recurringSortState.ascending = !recurringSortState.ascending;
    } else {
        recurringSortState.column = column;
        recurringSortState.ascending = true;
    }

    // Sort rows
    rows.sort((a, b) => {
        let aVal = a.dataset[column];
        let bVal = b.dataset[column];

        // Handle numeric columns
        if (column === 'amount' || column === 'monthly' || column === 'annual') {
            aVal = parseFloat(aVal) || 0;
            bVal = parseFloat(bVal) || 0;
            return recurringSortState.ascending ? aVal - bVal : bVal - aVal;
        }

        // Handle frequency with custom order
        if (column === 'frequency') {
            const freqOrder = { 'weekly': 1, 'biweekly': 2, 'monthly': 3, 'yearly': 4, 'ongoing': 5 };
            aVal = freqOrder[aVal.toLowerCase()] || 99;
            bVal = freqOrder[bVal.toLowerCase()] || 99;
            return recurringSortState.ascending ? aVal - bVal : bVal - aVal;
        }

        // String comparison for description
        aVal = (aVal || '').toLowerCase();
        bVal = (bVal || '').toLowerCase();
        if (aVal < bVal) return recurringSortState.ascending ? -1 : 1;
        if (aVal > bVal) return recurringSortState.ascending ? 1 : -1;
        return 0;
    });

    // Re-append rows in sorted order
    rows.forEach(row => tbody.appendChild(row));

    // Update sort indicators
    updateSortIcons('recurring-payments-table', column, recurringSortState.ascending);
}

// Category trends table sorting
let trendsSortState = { column: null, ascending: true };

function sortTrendsTable(column) {
    const table = document.getElementById('category-trends-table');
    if (!table) return;

    const tbody = table.querySelector('tbody');
    const rows = Array.from(tbody.querySelectorAll('tr'));

    if (trendsSortState.column === column) {
        trendsSortState.ascending = !trendsSortState.ascending;
    } else {
        trendsSortState.column = column;
        // Numeric columns default to descending (largest first) — matches the
        // chart, which sorts by absolute change. Category defaults to A→Z.
        trendsSortState.ascending = column === 'category';
    }

    rows.sort((a, b) => {
        let aVal = a.dataset[column];
        let bVal = b.dataset[column];

        if (column === 'current' || column === 'previous' || column === 'change') {
            aVal = parseFloat(aVal) || 0;
            bVal = parseFloat(bVal) || 0;
            return trendsSortState.ascending ? aVal - bVal : bVal - aVal;
        }

        aVal = (aVal || '').toLowerCase();
        bVal = (bVal || '').toLowerCase();
        if (aVal < bVal) return trendsSortState.ascending ? -1 : 1;
        if (aVal > bVal) return trendsSortState.ascending ? 1 : -1;
        return 0;
    });

    rows.forEach(row => tbody.appendChild(row));
    updateSortIcons('category-trends-table', column, trendsSortState.ascending);
    applyTrendsCap();
}

// RF3: cap the rendered category-trends table to the largest 12 rows (in
// current DOM order -- sorting re-appends rows before this runs, so
// "largest 12" always means the top 12 of whatever the table is currently
// sorted by). Server markup carries no `hidden` (point 16: JS off shows
// every row); this only ever narrows what JS itself already broadened by
// making the whole table interactive, so JS is what may also collapse it.
function applyTrendsCap() {
    const table = document.getElementById('category-trends-table');
    if (!table) return;
    const tbody = table.querySelector('tbody');
    if (!tbody) return;
    const expanded = table.dataset.expanded === 'true';
    Array.from(tbody.querySelectorAll('tr')).forEach(function (row, i) {
        row.hidden = !expanded && i >= 12;
    });
}

function toggleTrendsCap(button) {
    const table = document.getElementById('category-trends-table');
    if (!table) return;
    const tbody = table.querySelector('tbody');
    const total = tbody ? tbody.querySelectorAll('tr').length : 0;
    const expanded = table.dataset.expanded === 'true';
    const next = !expanded;
    table.dataset.expanded = next ? 'true' : 'false';
    button.setAttribute('aria-expanded', next ? 'true' : 'false');
    button.textContent = next ? 'Show the largest 12' : ('Show all ' + total + ' categories');
    applyTrendsCap();
}

function updateSortIcons(tableId, column, ascending) {
    const table = document.getElementById(tableId);
    if (!table) return;

    // Clear all sort icons and aria-sort (U7 attempt 2: the th now wraps a
    // <button data-sort>; expose the tracked direction on the th itself).
    table.querySelectorAll('.sort-icon').forEach(icon => {
        icon.innerHTML = '';
    });
    table.querySelectorAll('th[scope="col"]').forEach(th => {
        th.removeAttribute('aria-sort');
    });

    // Set the active sort icon
    const activeIcon = table.querySelector(`.sort-icon[data-col="${column}"]`);
    if (activeIcon) {
        activeIcon.innerHTML = ascending
            ? '<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7"></path></svg>'
            : '<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path></svg>';
        const th = activeIcon.closest('th');
        if (th) th.setAttribute('aria-sort', ascending ? 'ascending' : 'descending');
    }
}

function setInsightPreset(preset) {
    const form = document.getElementById('insights-date-filter');
    const startInput = form.querySelector('input[name="start"]');

    const end = new Date();
    let start = new Date();

    switch(preset) {
        case '1m':
            start.setMonth(start.getMonth() - 1);
            break;
        case '2m':
            start.setMonth(start.getMonth() - 2);
            break;
        case '3m':
            start.setMonth(start.getMonth() - 3);
            break;
        case '6m':
            start.setMonth(start.getMonth() - 6);
            break;
        case '12m':
            start.setMonth(start.getMonth() - 12);
            break;
        case 'all':
            start = new Date(startInput.min);
            break;
    }

    const startStr = start.toISOString().split('T')[0];
    const endStr = end.toISOString().split('T')[0];

    // Navigate directly with all params - use partial update to prevent page jump
    htmx.ajax('GET', '/insights?start=' + startStr + '&end=' + endStr + '&preset=' + preset, {
        target: '#insights-wrapper',
        select: '#insights-wrapper',
        swap: 'outerHTML',
        pushUrl: true
    });
}

// Shift the insights date window forward (+1) or backward (-1).
// If a month-based preset is active (1m/2m/3m/6m/12m), shift by N months
// preserving day-of-month and the preset highlight (server reads the
// preset hidden input). Otherwise shift by the current span in days.
// "all" is a no-op.
function shiftInsightWindow(direction) {
    const form = document.getElementById('insights-date-filter');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const presetInput = form.querySelector('input[name="preset"]');

    const parseLocal = function (s) {
        if (!s) return null;
        const parts = s.split('-').map(Number);
        if (parts.length !== 3 || parts.some(isNaN)) return null;
        return new Date(parts[0], parts[1] - 1, parts[2]);
    };
    const formatLocal = function (d) {
        const y = d.getFullYear();
        const m = String(d.getMonth() + 1).padStart(2, '0');
        const dd = String(d.getDate()).padStart(2, '0');
        return y + '-' + m + '-' + dd;
    };

    const currentStart = parseLocal(startInput.value);
    const currentEnd = parseLocal(endInput.value);
    if (!currentStart || !currentEnd) return;

    const presetVal = presetInput ? presetInput.value : '';
    const monthMap = { '1m': 1, '2m': 2, '3m': 3, '6m': 6, '12m': 12 };
    let newStart, newEnd;

    if (monthMap[presetVal]) {
        const months = monthMap[presetVal] * direction;
        newStart = new Date(currentStart);
        newStart.setMonth(newStart.getMonth() + months);
        newEnd = new Date(currentEnd);
        newEnd.setMonth(newEnd.getMonth() + months);
    } else if (presetVal === 'all') {
        return;
    } else {
        const dayMs = 86400000;
        const spanDays = Math.round((currentEnd - currentStart) / dayMs) + 1;
        const shiftMs = spanDays * direction * dayMs;
        newStart = new Date(currentStart.getTime() + shiftMs);
        newEnd = new Date(currentEnd.getTime() + shiftMs);
    }

    const minDate = parseLocal(startInput.min);
    const maxDate = parseLocal(endInput.max);
    if (minDate && newStart < minDate) {
        const diffMs = minDate - newStart;
        newStart = new Date(newStart.getTime() + diffMs);
        newEnd = new Date(newEnd.getTime() + diffMs);
    }
    if (maxDate && newEnd > maxDate) {
        const diffMs = newEnd - maxDate;
        newStart = new Date(newStart.getTime() - diffMs);
        newEnd = new Date(newEnd.getTime() - diffMs);
    }
    if (minDate && newStart < minDate) newStart = new Date(minDate);
    if (maxDate && newEnd > maxDate) newEnd = new Date(maxDate);

    const startStr = formatLocal(newStart);
    const endStr = formatLocal(newEnd);
    if (startStr === startInput.value && endStr === endInput.value) return;

    const url = '/insights?start=' + startStr + '&end=' + endStr +
        (presetVal ? '&preset=' + encodeURIComponent(presetVal) : '');
    htmx.ajax('GET', url, {
        target: '#insights-wrapper',
        select: '#insights-wrapper',
        swap: 'outerHTML',
        pushUrl: true
    });
}

// Preset clearing is delegated above so it survives replacement.

// Date controls are replaced with the entire investigation, including findings.
// Preserve keyboard position for both native inputs and delegated preset buttons.
let insightFocusSelector = null;
document.body.addEventListener('htmx:beforeRequest', function (evt) {
    if (!evt.detail.target || evt.detail.target.id !== 'insights-wrapper') return;
    const active = document.activeElement;
    insightFocusSelector = null;
    if (!active || !active.closest('#insights-date-filter')) return;
    if (active.id) insightFocusSelector = '#' + active.id;
    else if (active.dataset.preset) insightFocusSelector = '#insights-date-filter [data-preset="' + active.dataset.preset + '"]';
    else if (active.dataset.step) insightFocusSelector = '#insights-date-filter [data-step="' + active.dataset.step + '"]';
});
document.body.addEventListener('htmx:afterSwap', function (evt) {
    if (!evt.detail.target || evt.detail.target.id !== 'insights-wrapper') return;
    const next = insightFocusSelector && document.querySelector(insightFocusSelector);
    if (next) next.focus({preventScroll: true});
    insightFocusSelector = null;
    applyTrendsCap();
});

// Init: the trends table is present in the server-rendered document by the
// time this deferred script runs.
applyTrendsCap();

// Handle chart data responses
function insightChartMarkers() {
    const css = getComputedStyle(document.documentElement);
    const accent = 'rgb(' + css.getPropertyValue('--accent').trim().split(/\s+/).join(',') + ')';
    const prior = document.documentElement.classList.contains('dark') ? '#9ca3af' : '#6b7280';
    return [{color: accent}, {color: prior, pattern: {shape: '/'}}];
}

window.addEventListener('themechange', function () {
    const chart = document.getElementById('chart-trends');
    if (chart && chart.data && window.Plotly) {
        const markers = insightChartMarkers();
        Plotly.restyle(chart, {marker: markers});
    }
});

document.body.addEventListener('htmx:afterRequest', function(evt) {
    const target = evt.detail.target;
    if (target && target.id === 'chart-trends') {
        try {
            const data = JSON.parse(evt.detail.xhr.responseText);
            const markers = insightChartMarkers();
            data.data.forEach((trace, i) => { trace.marker = markers[i]; });
            data.layout.height = Math.max(360, data.data[0].x.length * 38 + 120);
            renderChart('chart-trends', data);
        } catch (e) {
            console.error('Error parsing chart data:', e);
        }
    }
});

// Supporting section tabs (Spending trends / Income sources / Anomalies /
// Price creep / Spending pace). Mirrors whatif-tabs.js's activateTab /
// resizeChartsIn pattern: hidden panels use the `hidden` property (not a
// class) so progressive enhancement is a plain attribute toggle, and the
// active key persists per browser (not per scenario, there is only one
// insights page).
(function () {
    var INSIGHTS_TAB_KEY = 'insightsActiveTab';
    var INSIGHTS_TAB_KEYS = ['trends', 'income', 'anomalies', 'pricecreep', 'pace'];

    function activateInsightsTab(key, persist) {
        var section = document.getElementById('insights-supporting');
        if (!section) return;
        var panels = section.querySelectorAll('[data-ins-panel]');
        var tabs = section.querySelectorAll('[data-ins-tab]');
        panels.forEach(function (p) {
            p.hidden = p.getAttribute('data-ins-panel') !== key;
        });
        tabs.forEach(function (t) {
            var on = t.getAttribute('data-ins-tab') === key;
            t.setAttribute('aria-selected', on ? 'true' : 'false');
            t.setAttribute('tabindex', on ? '0' : '-1');
            t.classList.toggle('wf-tab-active', on);
        });
        if (persist) {
            try { window.localStorage.setItem(INSIGHTS_TAB_KEY, key); } catch (e) {}
        }
        var chart = document.getElementById('chart-trends');
        if (chart && chart.data && window.Plotly) {
            try { window.Plotly.Plots.resize(chart); } catch (e) { /* not yet rendered */ }
        }
    }

    function restoreInsightsTab() {
        var key = 'trends';
        try {
            var stored = window.localStorage.getItem(INSIGHTS_TAB_KEY);
            if (stored && INSIGHTS_TAB_KEYS.indexOf(stored) !== -1) key = stored;
        } catch (e) {}
        activateInsightsTab(key, false);
    }

    function initInsightsTabs() {
        if (document.getElementById('insights-supporting')) restoreInsightsTab();
    }

    document.addEventListener('click', function (e) {
        var tab = e.target.closest('[data-ins-tab]');
        if (!tab) return;
        activateInsightsTab(tab.getAttribute('data-ins-tab'), true);
    });

    document.addEventListener('keydown', function (e) {
        var tab = e.target.closest('[data-ins-tab]');
        if (!tab) return;
        var section = document.getElementById('insights-supporting');
        if (!section) return;
        if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
            e.preventDefault();
            activateInsightsTab(tab.getAttribute('data-ins-tab'), true);
            return;
        }
        var tabs = Array.prototype.slice.call(section.querySelectorAll('[data-ins-tab]'));
        var current = tabs.indexOf(tab);
        var next = current;
        if (e.key === 'ArrowRight') next = (current + 1) % tabs.length;
        else if (e.key === 'ArrowLeft') next = (current - 1 + tabs.length) % tabs.length;
        else if (e.key === 'Home') next = 0;
        else if (e.key === 'End') next = tabs.length - 1;
        else return;
        e.preventDefault();
        tabs[next].focus();
        activateInsightsTab(tabs[next].getAttribute('data-ins-tab'), true);
    });

    if (document.readyState !== 'loading') {
        initInsightsTabs();
    } else {
        document.addEventListener('DOMContentLoaded', initInsightsTabs);
    }

    document.body.addEventListener('htmx:afterSwap', function (evt) {
        var t = evt.detail && evt.detail.target;
        if (!t) return;
        if (t.id === 'insights-supporting' || (t.querySelector && t.querySelector('#insights-supporting'))) {
            initInsightsTabs();
        }
    });
})();
