// Dashboard-specific JavaScript functionality

// Major Expense drilldown functions
function openMajorExpenseDrilldown(name) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/major-expense?name=${encodeURIComponent(name)}&start=${start}&end=${end}`, {
        target: '#major-expense-drilldown-container',
        swap: 'innerHTML'
    });
}

function closeMajorExpenseModal(event) {
    if (event && event.target !== event.currentTarget) return;
    const container = document.getElementById('major-expense-drilldown-container');
    if (container) container.replaceChildren();
}

// KPI detail functions
function openKPIDetail(kpiType) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/kpi/${encodeURIComponent(kpiType)}?start=${start}&end=${end}`, {
        target: '#kpi-detail-container',
        swap: 'innerHTML'
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
    });
}

function closeKPIModal(event) {
    if (event && event.target !== event.currentTarget) return;
    document.getElementById('kpi-detail-container').innerHTML = '';
}

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

// Shift the date window forward (+1) or backward (-1) by the current window size.
// If a month-based preset is active, shift by N months (preserves day-of-month).
// Otherwise, shift by the current span in days. Clamps to the input's min/max.
function shiftWindow(direction) {
    const form = document.getElementById('date-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const currentStart = parseDateLocal(startInput.value);
    const currentEnd = parseDateLocal(endInput.value);
    if (!currentStart || !currentEnd) return;

    const monthMap = { '1m': 1, '2m': 2, '3m': 3, '6m': 6, '12m': 12 };
    const activePreset = form.dataset.activePreset || '';
    let newStart, newEnd;

    if (monthMap[activePreset]) {
        const months = monthMap[activePreset] * direction;
        newStart = new Date(currentStart);
        newStart.setMonth(newStart.getMonth() + months);
        newEnd = new Date(currentEnd);
        newEnd.setMonth(newEnd.getMonth() + months);
    } else {
        const dayMs = 24 * 60 * 60 * 1000;
        const spanDays = Math.round((currentEnd - currentStart) / dayMs) + 1;
        const shiftMs = spanDays * direction * dayMs;
        newStart = new Date(currentStart.getTime() + shiftMs);
        newEnd = new Date(currentEnd.getTime() + shiftMs);
    }

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

// Drag and drop file upload
document.addEventListener('DOMContentLoaded', function () {
    const dropZone = document.getElementById('drop-zone');
    if (!dropZone) return;

    // Prevent default drag behaviors on the whole document
    // Only use preventDefault - do NOT use stopPropagation as it blocks the dropZone handlers
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        document.addEventListener(eventName, function (e) {
            e.preventDefault();
        }, false);
    });

    // Highlight drop zone on drag over
    dropZone.addEventListener('dragenter', function (e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('border-gray-300', 'bg-gray-50');
        dropZone.classList.add('border-indigo-500', 'bg-indigo-100');
    }, false);

    dropZone.addEventListener('dragover', function (e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('border-gray-300', 'bg-gray-50');
        dropZone.classList.add('border-indigo-500', 'bg-indigo-100');
    }, false);

    dropZone.addEventListener('dragleave', function () {
        dropZone.classList.remove('border-indigo-500', 'bg-indigo-100');
        dropZone.classList.add('border-gray-300', 'bg-gray-50');
    }, false);

    dropZone.addEventListener('drop', function (e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('border-indigo-500', 'bg-indigo-100');
        dropZone.classList.add('border-gray-300', 'bg-gray-50');
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
