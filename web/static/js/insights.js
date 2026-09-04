// Insights page: date-range quick buttons/step arrows, recurring/trends
// table sorting, and description-cell navigation. Extracted from
// pages/insights.html (U7).

// Controls that used to carry inline onclick= (U7): the shift-window
// arrows, sortable table headers (data-sort-fn names the function, since
// this page has two independently-sortable tables), and the row/tile
// elements that navigate to the Explorer with a filter — delegated since
// these rows are re-rendered by htmx swaps.
document.addEventListener('DOMContentLoaded', function () {
    document.querySelectorAll('#insights-date-filter [data-step]').forEach(function (btn) {
        btn.addEventListener('click', function () { shiftInsightWindow(parseInt(btn.dataset.step, 10)); });
    });
    document.querySelectorAll('#insights-date-filter .insight-preset-btn[data-preset]').forEach(function (btn) {
        btn.addEventListener('click', function () { setInsightPreset(btn.dataset.preset); });
    });
});

document.addEventListener('click', function (e) {
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

// Preset clearing is now handled via inline oninput handlers on date inputs

// Handle chart data responses
document.body.addEventListener('htmx:afterRequest', function(evt) {
    const target = evt.detail.target;
    if (target && target.id === 'chart-trends') {
        try {
            const data = JSON.parse(evt.detail.xhr.responseText);
            renderChart('chart-trends', data);
        } catch (e) {
            console.error('Error parsing chart data:', e);
        }
    }
});
