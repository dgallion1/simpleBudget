// Dashboard-specific JavaScript functionality

// KPI cards/buttons carry data-kpi-detail/-month-detail/-export instead of
// an inline onclick= (U7); delegated so it also covers the KPI detail /
// month-detail modal content the HTMX swap injects. closest() finds the
// nearest match (e.g. a "view" button inside a row that also carries the
// attribute), so a click never fires the handler twice.
document.addEventListener('click', function (e) {
    var el;
    if ((el = e.target.closest('[data-kpi-detail]'))) {
        openKPIDetail(el.getAttribute('data-kpi-detail'));
        return;
    }
    if ((el = e.target.closest('[data-kpi-month-detail]'))) {
        var parts = el.getAttribute('data-kpi-month-detail').split('|');
        openKPIMonthDetail(parts[0], parts[1]);
        return;
    }
    if ((el = e.target.closest('[data-export-kpi]'))) {
        exportKPIToCSV(el.getAttribute('data-export-kpi'));
        return;
    }
});

// The KPI-tile cards (shared/kpi-tile.html) are tabindex="0" role="button"
// divs, not native <button>s — Enter/Space don't activate those on their
// own, so this fires the same openKPIDetail() the click handler above
// uses (U7 attempt 2, ruling U-2026-09-04l). Scoped to [role="button"] so
// it does not double-fire on the real <button data-kpi-detail="savings">
// in the verdict bar, which already gets Enter/Space for free.
document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return;
    var el = e.target.closest('[data-kpi-detail][role="button"]');
    if (!el || e.target !== el) return;
    e.preventDefault();
    openKPIDetail(el.getAttribute('data-kpi-detail'));
});

document.addEventListener('DOMContentLoaded', function () {
    var dropZone = document.getElementById('drop-zone');
    var fileInput = document.getElementById('file-input');
    var importBtn = document.getElementById('import-csv-btn');
    if (dropZone && fileInput) {
        dropZone.addEventListener('click', function () { fileInput.click(); });
        dropZone.addEventListener('keydown', function (event) {
            if (event.target === dropZone && (event.key === 'Enter' || event.key === ' ')) {
                event.preventDefault();
                fileInput.click();
            }
        });
        fileInput.addEventListener('change', function () { handleFileSelect(this.files); });
    }
    // Page-header "Import CSV" button (U10): same file picker/import path
    // the drop-zone click always used.
    if (importBtn && fileInput) {
        importBtn.addEventListener('click', function () { fileInput.click(); });
    }

    document.querySelectorAll('#date-filter-form [data-step]').forEach(function (btn) {
        btn.addEventListener('click', function () { shiftWindow(parseInt(btn.dataset.step, 10)); });
    });
    document.querySelectorAll('#date-filter-form .preset-btn[data-preset]').forEach(function (btn) {
        btn.addEventListener('click', function () { setPreset(btn.dataset.preset); });
    });
});

// Chart alternatives follow the actual Plotly series, including HTMX/date
// refreshes and theme redraws. Values are transcribed as plotted: no client
// arithmetic, independent cent formatter, or claim that a capped chart sums
// to a longer selected period. The observer also covers initial async plots.
document.addEventListener('DOMContentLoaded', function () {
    const plots = document.querySelectorAll('.chart-container[data-chart-url]');
    plots.forEach(function (plot) {
        let signature = '';
        let listening = false;
        function updateTable() {
            if (!Array.isArray(plot.data)) return;
            const rows = [];
            plot.data.forEach(function (trace) {
                const labels = trace.labels || (trace.orientation === 'h' ? trace.y : trace.x) || [];
                const values = trace.values || (trace.orientation === 'h' ? trace.x : trace.y) || [];
                labels.forEach(function (label, index) {
                    if (values[index] != null) rows.push([trace.name || 'Spending', String(label), String(values[index])]);
                });
            });
            // Plotly layout shapes are data too: the budget target is a
            // horizontal reference line rather than one of its traces.
            ((plot.layout && plot.layout.shapes) || []).forEach(function (shape) {
                if (shape.type === 'line' && shape.y0 === shape.y1 && shape.yref === 'y') {
                    rows.push(['Target', 'Across plotted months', String(shape.y0)]);
                }
            });
            const next = JSON.stringify(rows);
            if (signature === next) return;
            signature = next;
            let details = document.getElementById(plot.id + '-data-table');
            if (!details) {
                details = document.createElement('details');
                details.id = plot.id + '-data-table';
                details.dataset.dashboardChartTable = '';
                details.className = 'mt-3 text-sm text-gray-700 dark:text-gray-300';
                const summary = document.createElement('summary');
                summary.textContent = 'View chart data';
                summary.className = 'cursor-pointer text-accent';
                details.appendChild(summary);
                plot.after(details);
            }
            let wrapper = details.querySelector('div');
            if (!wrapper) { wrapper = document.createElement('div'); wrapper.className = 'overflow-auto'; details.appendChild(wrapper); }
            wrapper.tabIndex = 0;
            wrapper.setAttribute('role', 'region');
            const title = plot.closest('.rounded-lg')?.querySelector('h2')?.textContent.trim() || 'Chart';
            wrapper.setAttribute('aria-label', title + ' chart data');
            wrapper.replaceChildren();
            const table = document.createElement('table');
            table.className = 'w-full text-left';
            const caption = table.createCaption();
            caption.textContent = 'Values as plotted; the rows show this chart’s category and time scope. Negative values retain their sign.';
            const head = table.createTHead().insertRow();
            ['Series', 'Period or category', 'Value'].forEach(function (name) {
                const th = document.createElement('th'); th.scope = 'col'; th.textContent = name; th.className = 'p-2'; head.appendChild(th);
            });
            const body = table.createTBody();
            rows.forEach(function (row) { const tr = body.insertRow(); row.forEach(function (value) { const td = tr.insertCell(); td.textContent = value; td.className = 'p-2'; }); });
            wrapper.appendChild(table);
        }
        const observer = new MutationObserver(function () {
            if (!listening && typeof plot.on === 'function') { plot.on('plotly_afterplot', updateTable); listening = true; }
            updateTable();
        });
        observer.observe(plot, {childList: true, subtree: true});
        updateTable();
    });
});

// Major Expense drilldown functions
function openMajorExpenseDrilldown(name) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/major-expense?name=${encodeURIComponent(name)}&start=${start}&end=${end}`, {
        target: '#major-expense-drilldown-container',
        swap: 'innerHTML'
    }).then(function () {
        // U15: move focus in, trap Tab, and remember what had focus (the
        // donut wedge click doesn't focus anything itself, but the
        // keyboard-operable "Other"/credits row buttons do) so it can be
        // restored on close.
        var backdrop = document.querySelector('#major-expense-drilldown-container [data-modal-backdrop]');
        if (backdrop && window.ModalA11y) window.ModalA11y.open(backdrop);
    });
}

function closeMajorExpenseModal(event) {
    if (event && event.target !== event.currentTarget) return;
    const container = document.getElementById('major-expense-drilldown-container');
    if (container) {
        var backdrop = container.querySelector('[data-modal-backdrop]');
        if (backdrop && window.ModalA11y) window.ModalA11y.close(backdrop);
        container.replaceChildren();
    }
}

// KPI detail functions
function openKPIDetail(kpiType) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/kpi/${encodeURIComponent(kpiType)}?start=${start}&end=${end}`, {
        target: '#kpi-detail-container',
        swap: 'innerHTML'
    }).then(function () {
        var backdrop = document.querySelector('#kpi-detail-container [data-modal-backdrop]');
        if (backdrop && window.ModalA11y) window.ModalA11y.open(backdrop);
    });
}

// Drill down from one month row of the KPI detail modal into that month's
// transactions. Renders into the same container, so Back/Escape still work.
function openKPIMonthDetail(kpiType, month) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/kpi/${encodeURIComponent(kpiType)}/month/${encodeURIComponent(month)}?start=${start}&end=${end}`, {
        target: '#kpi-detail-container',
        swap: 'innerHTML'
    }).then(function () {
        var backdrop = document.querySelector('#kpi-detail-container [data-modal-backdrop]');
        if (backdrop && window.ModalA11y) window.ModalA11y.open(backdrop);
    });
}

function closeKPIModal(event) {
    if (event && event.target !== event.currentTarget) return;
    var container = document.getElementById('kpi-detail-container');
    var backdrop = container.querySelector('[data-modal-backdrop]');
    if (backdrop && window.ModalA11y) window.ModalA11y.close(backdrop);
    container.innerHTML = '';
}

// Column sorting for the KPI month-detail transaction table. Client-side and
// typed: every row carries data-date (ISO), data-description, data-category
// and data-amount (signed, 2dp), so the comparators never parse the rendered
// strings. The modal markup is swapped in by htmx, so the click handler is
// delegated off document and the sort state resets whenever a different table
// node appears (re-open, or a drill into another month).
(function () {
    var state = { table: null, column: null, direction: 'asc' };

    function cmp(a, b, column) {
        var av = a.getAttribute('data-' + column) || '';
        var bv = b.getAttribute('data-' + column) || '';
        var dir = state.direction === 'asc' ? 1 : -1;
        var r;
        if (column === 'amount') {
            // Signed: ascending puts the largest outflow first, which is what
            // an expense list wants; descending puts the largest inflow first.
            r = (parseFloat(av) || 0) - (parseFloat(bv) || 0);
            if (r !== 0) r = r < 0 ? -1 : 1;
        } else if (column === 'date') {
            // ISO YYYY-MM-DD compares correctly as a string.
            r = av === bv ? 0 : (av < bv ? -1 : 1);
        } else {
            r = av.localeCompare(bv, undefined, { sensitivity: 'base', numeric: true });
        }
        return r * dir;
    }

    function renderIndicators(table) {
        table.querySelectorAll('th[data-sort]').forEach(function (th) {
            var col = th.getAttribute('data-sort');
            var arrow = th.querySelector('[data-sort-arrow]');
            var btn = th.querySelector('[data-sort-btn]');
            // Clear the arrow first so the label read below is the column
            // name alone, not the name plus a stale glyph.
            if (arrow) {
                arrow.classList.add('hidden');
                arrow.textContent = '';
            }
            var label = th.textContent.trim();
            if (col === state.column) {
                th.setAttribute('aria-sort', state.direction === 'asc' ? 'ascending' : 'descending');
                if (btn) btn.setAttribute('aria-label', label + ', sorted ' +
                    (state.direction === 'asc' ? 'ascending' : 'descending'));
                if (arrow) {
                    arrow.classList.remove('hidden');
                    arrow.textContent = state.direction === 'asc' ? '\u25B2' : '\u25BC';
                }
            } else {
                th.removeAttribute('aria-sort');
                if (btn) btn.removeAttribute('aria-label');
            }
        });
    }

    function applySort(table) {
        var tbody = table.querySelector('tbody');
        if (!tbody) return;
        var rows = Array.prototype.slice.call(tbody.querySelectorAll('tr[data-date]'));
        if (rows.length === 0) return;
        // Array.prototype.sort is stable in every evergreen browser, so the
        // server's |amount|-descending order survives as the tiebreaker.
        rows.sort(function (a, b) { return cmp(a, b, state.column); });
        var frag = document.createDocumentFragment();
        rows.forEach(function (r) { frag.appendChild(r); });
        tbody.appendChild(frag);
        renderIndicators(table);
    }

    // Capture phase: the modal's content div stops click propagation (so a
    // click inside it never closes the overlay), which would starve a normal
    // bubbling listener on document.
    document.addEventListener('click', function (e) {
        var btn = e.target.closest ? e.target.closest('[data-sort-btn]') : null;
        if (!btn) return;
        var table = btn.closest('#kpi-month-txn-table');
        if (!table) return;
        if (state.table !== table) {
            state.table = table;
            state.column = null;
            state.direction = 'asc';
        }
        var column = btn.getAttribute('data-sort-btn');
        if (state.column === column) {
            state.direction = state.direction === 'asc' ? 'desc' : 'asc';
        } else {
            state.column = column;
            state.direction = 'asc';
        }
        applySort(table);
    }, true);
})();

function exportKPIToCSV(kpiType) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;
    window.location.href = `/dashboard/kpi/${encodeURIComponent(kpiType)}/export?start=${start}&end=${end}`;
}

// Close modal on escape key
document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') {
        closeMajorExpenseModal();
        closeKPIModal();
    }
});

// Refresh all charts using current form values
function refreshCharts() {
    const form = document.getElementById('date-filter-form');
    if (!form) return;
    const params = new URLSearchParams(new FormData(form)).toString();

    document.querySelectorAll('.chart-container[data-chart-url]').forEach(function(el) {
        var urlObj = new URL(el.getAttribute('data-chart-url'), window.location.origin);
        new URLSearchParams(params).forEach(function(v, k) { urlObj.searchParams.set(k, v); });
        fetch(urlObj.toString())
            .then(function(resp) {
                if (!resp.ok) throw new Error('HTTP ' + resp.status);
                return resp.json();
            })
            .then(function(data) { renderChart(el.id, data); })
            .catch(function(e) { console.error('Error loading chart ' + el.id + ':', e); });
    });
}

// Date preset functions
function setPreset(preset) {
    const form = document.getElementById('date-filter-form');
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');

    const end = new Date();
    let start = new Date();

    switch (preset) {
        case 'ytd':
            start = new Date(end.getFullYear(), 0, 1);
            break;
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

    startInput.value = start.toISOString().split('T')[0];
    endInput.value = end.toISOString().split('T')[0];

    // Update button selection state
    document.querySelectorAll('.preset-btn').forEach(function(btn) {
        const isSelected = btn.dataset.preset === preset;
        if (isSelected) {
            btn.style.backgroundColor = 'rgb(79, 70, 229)';
            btn.style.color = 'white';
        } else {
            btn.style.backgroundColor = '';
            btn.style.color = '';
        }
    });
    form.dataset.activePreset = preset;

    // Update KPIs via HTMX (uses innerHTML swap, works reliably)
    const params = new URLSearchParams(new FormData(form)).toString();
    htmx.ajax('GET', '/dashboard/kpis?' + params, {
        target: '#kpis-container',
        swap: 'innerHTML'
    });

    // Update all charts via fetch
    refreshCharts();
}

// Parse "YYYY-MM-DD" as a local-timezone date (avoids UTC off-by-one).
function parseDateLocal(s) {
    if (!s) return null;
    const parts = s.split('-').map(Number);
    if (parts.length !== 3 || parts.some(isNaN)) return null;
    return new Date(parts[0], parts[1] - 1, parts[2]);
}

// Format a Date as "YYYY-MM-DD" using local components.
function formatDateLocal(d) {
    const year = d.getFullYear();
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return year + '-' + month + '-' + day;
}

// Shift the date window forward (+1) or backward (-1) by exactly one calendar
// month, regardless of window size. Day-of-month is preserved (clamped to month
// end). Clamps to the input's min/max.
function addMonthsClamped(d, months) {
    const day = d.getDate();
    const r = new Date(d.getFullYear(), d.getMonth() + months, 1);
    const last = new Date(r.getFullYear(), r.getMonth() + 1, 0).getDate();
    r.setDate(Math.min(day, last));
    return r;
}
function shiftWindow(direction) {
    const form = document.getElementById('date-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const currentStart = parseDateLocal(startInput.value);
    const currentEnd = parseDateLocal(endInput.value);
    if (!currentStart || !currentEnd) return;

    // Arrows always step by exactly one calendar month, regardless of window size.
    let newStart = addMonthsClamped(currentStart, direction);
    let newEnd = addMonthsClamped(currentEnd, direction);

    // Clamp to min/max bounds, preserving window size when possible.
    const minDate = parseDateLocal(startInput.min);
    const maxDate = parseDateLocal(endInput.max);
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

    // No-op if clamping produced the same range we already had.
    const newStartStr = formatDateLocal(newStart);
    const newEndStr = formatDateLocal(newEnd);
    if (newStartStr === startInput.value && newEndStr === endInput.value) return;

    startInput.value = newStartStr;
    endInput.value = newEndStr;

    const params = new URLSearchParams(new FormData(form)).toString();
    htmx.ajax('GET', '/dashboard/kpis?' + params, {
        target: '#kpis-container',
        swap: 'innerHTML'
    });
    refreshCharts();
}

// Refresh charts when date inputs change manually
document.addEventListener('DOMContentLoaded', function() {
    const filterForm = document.getElementById('date-filter-form');
    document.querySelectorAll('#date-filter-form input[type="date"]').forEach(function(input) {
        input.addEventListener('change', function() {
            // Clear preset selection
            document.querySelectorAll('.preset-btn').forEach(function(btn) {
                btn.style.backgroundColor = '';
                btn.style.color = '';
            });
            if (filterForm) filterForm.dataset.activePreset = '';
            refreshCharts();
        });
    });

    // Also refresh when comparison dropdown changes
    var comparison = document.querySelector('#date-filter-form select[name="comparison"]');
    if (comparison) {
        comparison.addEventListener('change', refreshCharts);
    }
});

// Drag and drop file upload. Whole-page (U10): the drop-zone box is now a
// secondary hint under the header "Import CSV" button, but dropping a file
// ANYWHERE on the page must still import it, so the actual 'drop' listener
// is on document (mirroring filemanager.js's document-level restore-drop
// pattern) rather than scoped to #drop-zone alone; #drop-zone only gets the
// drag-over highlight when it exists.
document.addEventListener('DOMContentLoaded', function () {
    const dropZone = document.getElementById('drop-zone');

    // Prevent default drag behaviors on the whole document (so the browser
    // never navigates to/opens the dropped file) without stopPropagation,
    // which would block the listeners below.
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        document.addEventListener(eventName, function (e) {
            e.preventDefault();
        }, false);
    });

    if (dropZone) {
        // Highlight the drop-zone hint on drag-over.
        dropZone.addEventListener('dragenter', function (e) {
            e.preventDefault();
            dropZone.classList.remove('border-gray-300', 'bg-gray-50');
            dropZone.classList.add('border-indigo-500', 'bg-indigo-100');
        }, false);

        dropZone.addEventListener('dragover', function (e) {
            e.preventDefault();
            dropZone.classList.remove('border-gray-300', 'bg-gray-50');
            dropZone.classList.add('border-indigo-500', 'bg-indigo-100');
        }, false);

        dropZone.addEventListener('dragleave', function () {
            dropZone.classList.remove('border-indigo-500', 'bg-indigo-100');
            dropZone.classList.add('border-gray-300', 'bg-gray-50');
        }, false);
    }

    // The actual import trigger: dropping anywhere on the page.
    document.addEventListener('drop', function (e) {
        e.preventDefault();
        if (dropZone) {
            dropZone.classList.remove('border-indigo-500', 'bg-indigo-100');
            dropZone.classList.add('border-gray-300', 'bg-gray-50');
        }
        const files = e.dataTransfer.files;
        handleFileSelect(files);
    }, false);
});

function handleFileSelect(files) {
    if (files.length === 0) return;

    const file = files[0];
    const isCSV = file.name.toLowerCase().endsWith('.csv');
    const isZIP = file.name.toLowerCase().endsWith('.zip');

    if (!isCSV && !isZIP) {
        showDropZoneState('error', 'Only CSV or ZIP backup files are accepted');
        return;
    }

    if (isZIP) {
        restoreBackup(file);
    } else {
        uploadFile(file);
    }
}

function showDropZoneState(state, msg) {
    const content = document.getElementById('drop-zone-content');
    const uploading = document.getElementById('drop-zone-uploading');
    const success = document.getElementById('drop-zone-success');
    const error = document.getElementById('drop-zone-error');
    const errorMsgEl = document.getElementById('drop-zone-error-msg');

    content.classList.add('hidden');
    uploading.classList.add('hidden');
    success.classList.add('hidden');
    error.classList.add('hidden');

    switch (state) {
        case 'uploading':
            uploading.classList.remove('hidden');
            break;
        case 'success':
            if (msg) success.querySelector('span').textContent = msg;
            success.classList.remove('hidden');
            break;
        case 'error':
            errorMsgEl.textContent = msg || 'Upload failed';
            error.classList.remove('hidden');
            setTimeout(() => showDropZoneState('default'), 3000);
            break;
        default:
            content.classList.remove('hidden');
    }
}

function uploadFile(file) {
    showDropZoneState('uploading');

    const formData = new FormData();
    formData.append('file', file);

    fetch('/explorer/upload', {
        method: 'POST',
        body: formData
    })
        .then(response => {
            if (!response.ok) throw new Error('Upload failed');
            return response.text();
        })
        .then(() => {
            showDropZoneState('success');
            // Reset file input
            document.getElementById('file-input').value = '';
            // Reload the page after a short delay to show new data
            setTimeout(() => window.location.reload(), 1000);
        })
        .catch(err => {
            showDropZoneState('error', err.message);
        });
}

function restoreBackup(file) {
    if (!confirm("Restore replaces ALL current data with this backup's contents — any file not in the backup will be deleted. A safety snapshot is taken first. Continue?")) {
        document.getElementById('file-input').value = '';
        return;
    }

    showDropZoneState('uploading');

    const formData = new FormData();
    formData.append('file', file);

    fetch('/restore', {
        method: 'POST',
        body: formData
    })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => { throw new Error(text || 'Restore failed'); });
            }
            return response.text();
        })
        .then(message => {
            showDropZoneState('success', message || 'Restore complete! Refreshing...');
            // Reset file input
            document.getElementById('file-input').value = '';
            // Give the user time to read the server's summary (skipped /
            // failed counts) before the reload wipes the message.
            setTimeout(() => window.location.reload(), 2500);
        })
        .catch(err => {
            showDropZoneState('error', err.message);
        });
}
