// Dashboard-specific JavaScript functionality

// Category drilldown functions
function openCategoryDrilldown(category) {
    const form = document.getElementById('date-filter-form');
    const start = form.querySelector('input[name="start"]').value;
    const end = form.querySelector('input[name="end"]').value;

    htmx.ajax('GET', `/dashboard/category/${encodeURIComponent(category)}?start=${start}&end=${end}`, {
        target: '#category-drilldown-container',
        swap: 'innerHTML'
    });
}

function closeCategoryModal(event) {
    if (event && event.target !== event.currentTarget) return;
    document.getElementById('category-drilldown-container').innerHTML = '';
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
        closeCategoryModal();
        closeKPIModal();
    }
});

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

    // Update button selection state with inline styles (more reliable than CSS classes)
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

    // Build query params
    const params = new URLSearchParams(new FormData(form)).toString();

    // Update KPIs
    htmx.ajax('GET', '/dashboard/kpis?' + params, {
        target: '#kpis-container',
        swap: 'innerHTML'
    });

    // Update each chart
    const charts = ['monthly', 'category', 'cashflow', 'merchants', 'weekly', 'cumulative'];
    charts.forEach(function(chart) {
        htmx.ajax('GET', '/dashboard/charts/data/' + chart + '?' + params, {
            target: '#chart-' + chart,
            swap: 'none'
        });
    });
}

// Clear preset selection when date inputs are manually changed
document.addEventListener('DOMContentLoaded', function() {
    document.querySelectorAll('#date-filter-form input[type="date"]').forEach(function(input) {
        input.addEventListener('input', function() {
            document.querySelectorAll('.preset-btn').forEach(function(btn) {
                btn.style.backgroundColor = '';
                btn.style.color = '';
            });
        });
    });
});

// Handle chart data responses
document.body.addEventListener('htmx:afterRequest', function (evt) {
    const target = evt.detail.target;
    if (target && target.id && target.id.startsWith('chart-')) {
        try {
            const data = JSON.parse(evt.detail.xhr.responseText);
            renderChart(target.id, data);
        } catch (e) {
            console.error('Error parsing chart data:', e);
        }
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
    if (!file.name.endsWith('.csv')) {
        showDropZoneState('error', 'Only CSV files are accepted');
        return;
    }

    uploadFile(file);
}

function showDropZoneState(state, errorMsg) {
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
            success.classList.remove('hidden');
            break;
        case 'error':
            errorMsgEl.textContent = errorMsg || 'Upload failed';
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
