// Chart rendering utilities for Budget Dashboard

/**
 * Check if dark mode is active
 * @returns {boolean}
 */
function isDarkMode() {
    return document.documentElement.classList.contains('dark');
}

/**
 * Get theme-aware colors
 * @returns {object}
 */
function getThemeColors() {
    const dark = isDarkMode();
    return {
        text: dark ? '#e5e7eb' : '#374151',
        gridColor: dark ? '#374151' : '#e5e7eb',
        backgroundColor: 'transparent'
    };
}

/**
 * Render a Plotly chart
 * @param {string} containerId - The ID of the container element
 * @param {object} chartData - The chart data from the API
 */
function renderChart(containerId, chartData) {
    const container = document.getElementById(containerId);
    if (!container) {
        console.error('Chart container not found:', containerId);
        return;
    }

    // Clear loading content before rendering
    container.innerHTML = '';

    // Parse if string
    let data = chartData;
    if (typeof chartData === 'string') {
        try {
            data = JSON.parse(chartData);
        } catch (e) {
            console.error('Error parsing chart data:', e);
            return;
        }
    }

    const colors = getThemeColors();
    const serverLayout = data.layout || {};

    // Default layout options
    const defaultLayout = {
        margin: { t: 30, r: 20, b: 50, l: 70 },
        paper_bgcolor: 'transparent',
        plot_bgcolor: 'transparent',
        font: {
            family: 'system-ui, -apple-system, sans-serif',
            color: colors.text
        },
        showlegend: true,
        legend: {
            orientation: 'h',
            y: -0.15,
            font: { color: colors.text }
        }
    };

    // Deep merge axis properties
    const layout = {
        ...defaultLayout,
        ...serverLayout,
        xaxis: {
            gridcolor: colors.gridColor,
            tickfont: { color: colors.text },
            automargin: true,
            ...(serverLayout.xaxis || {})
        },
        yaxis: {
            gridcolor: colors.gridColor,
            tickfont: { color: colors.text },
            automargin: true,
            ...(serverLayout.yaxis || {})
        }
    };

    // Plotly config
    const config = {
        responsive: true,
        displayModeBar: false
    };

    // Render
    Plotly.newPlot(containerId, data.data, layout, config);

    // Click handler + "Other" breakdown for the major-expense donut.
    if (containerId === 'chart-major-expense') {
        container.on('plotly_click', function(eventData) {
            if (eventData.points && eventData.points.length > 0) {
                const name = eventData.points[0].label;
                if (name && typeof openMajorExpenseDrilldown === 'function') {
                    openMajorExpenseDrilldown(name);
                }
            }
        });
        renderMajorExpenseBreakdown(data.smaller);
    }
}

/**
 * Render the text breakdown of rolled-up "Other" major-expense items
 * into the breakdown sibling div. Clears the div when no items.
 * Built with DOM APIs and textContent — never innerHTML — so any HTML
 * inside a major-expense name renders as plain text.
 * @param {Array<{name: string, amount: number, percent: number}>} items
 */
function renderMajorExpenseBreakdown(items) {
    const target = document.getElementById('chart-major-expense-breakdown');
    if (!target) return;

    while (target.firstChild) {
        target.removeChild(target.firstChild);
    }

    if (!items || items.length === 0) {
        return;
    }

    const fmtMoney = new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    });

    const header = document.createElement('div');
    header.className = 'font-medium text-gray-700 dark:text-gray-200 mb-1';
    header.textContent = 'Other categories';
    target.appendChild(header);

    items.forEach(function(it) {
        const row = document.createElement('div');
        row.className = 'flex justify-between gap-4 py-0.5';

        const nameEl = document.createElement('span');
        nameEl.className = 'truncate';
        nameEl.textContent = String(it.name);
        row.appendChild(nameEl);

        const valEl = document.createElement('span');
        valEl.className = 'tabular-nums whitespace-nowrap';
        valEl.appendChild(document.createTextNode(fmtMoney.format(it.amount) + '  '));

        const pctEl = document.createElement('span');
        pctEl.className = 'text-gray-500 dark:text-gray-400';
        const pctNum = Number(it.percent);
        const pctText = (pctNum < 1 ? pctNum.toFixed(2) : pctNum.toFixed(1)) + '%';
        pctEl.textContent = pctText;
        valEl.appendChild(pctEl);

        row.appendChild(valEl);
        target.appendChild(row);
    });
}

/**
 * Render a sparkline chart with optional target overlay or balance mode.
 *
 * @param {string} containerId - The ID of the container element
 * @param {number[]} values - The data values (or cumulative balance values when mode==="balance")
 * @param {string} color - The line color (used when no target/mode customization applies)
 * @param {object} [options] - Optional rendering options
 * @param {number} [options.target] - When set, draws a dashed horizontal target line.
 *                                    Months above the line fill red, below fill green.
 * @param {string} [options.mode] - When "balance", values are cumulative balances
 *                                  (target − actual; positive = saved, negative = overspent).
 *                                  Zero is the reference; fill above zero green, below red.
 *                                  Overrides options.target.
 */
function renderSparkline(containerId, values, color, options) {
    const container = document.getElementById(containerId);
    if (!container || !values || values.length === 0) {
        return;
    }

    options = options || {};
    const isBalance = options.mode === 'balance';
    const hasTarget = !isBalance && typeof options.target === 'number' && isFinite(options.target);

    const data = [];
    const layout = {
        margin: { t: 0, r: 0, b: 0, l: 0 },
        paper_bgcolor: 'transparent',
        plot_bgcolor: 'transparent',
        xaxis: { visible: false },
        yaxis: { visible: false },
        showlegend: false
    };

    if (isBalance) {
        // Split the line into above-zero (green = saved) and below-zero
        // (red = overspent) segments by clamping each direction. Two filled
        // traces against the zero baseline give the divergent fill.
        const above = values.map(v => v > 0 ? v : 0);
        const below = values.map(v => v < 0 ? v : 0);

        data.push({
            type: 'scatter',
            mode: 'lines',
            y: below,
            line: { color: '#ef4444', width: 1 },
            fill: 'tozeroy',
            fillcolor: 'rgba(239, 68, 68, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: above,
            line: { color: '#22c55e', width: 1 },
            fill: 'tozeroy',
            fillcolor: 'rgba(34, 197, 94, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 }
        });

        layout.shapes = [{
            type: 'line',
            xref: 'paper',
            x0: 0,
            x1: 1,
            yref: 'y',
            y0: 0,
            y1: 0,
            line: { color: '#6b7280', width: 1, dash: 'dash' }
        }];
    } else if (hasTarget) {
        // Above-target fill (red) and below-target fill (green) achieved by
        // plotting two clamped series with fill: 'tonexty' relative to a flat
        // target baseline.
        const target = options.target;
        const targetSeries = values.map(() => target);
        const above = values.map(v => v > target ? v : target);
        const below = values.map(v => v < target ? v : target);

        data.push({
            type: 'scatter',
            mode: 'lines',
            y: targetSeries,
            line: { color: 'transparent', width: 0 }
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: below,
            line: { color: 'transparent', width: 0 },
            fill: 'tonexty',
            fillcolor: 'rgba(34, 197, 94, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: targetSeries,
            line: { color: 'transparent', width: 0 }
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: above,
            line: { color: 'transparent', width: 0 },
            fill: 'tonexty',
            fillcolor: 'rgba(239, 68, 68, 0.3)'
        });
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 }
        });

        layout.shapes = [{
            type: 'line',
            xref: 'paper',
            x0: 0,
            x1: 1,
            yref: 'y',
            y0: target,
            y1: target,
            line: { color: '#6b7280', width: 1, dash: 'dash' }
        }];
    } else {
        // Original behavior — single filled line, no target reference.
        data.push({
            type: 'scatter',
            mode: 'lines',
            y: values,
            line: { color: color || '#6366f1', width: 2 },
            fill: 'tozeroy',
            fillcolor: (color || '#6366f1') + '20'
        });
    }

    const config = {
        responsive: true,
        displayModeBar: false,
        staticPlot: true
    };

    Plotly.newPlot(containerId, data, layout, config);
}

/**
 * Update chart with new data
 * @param {string} containerId - The ID of the container element
 * @param {object} newData - The new chart data
 */
function updateChart(containerId, newData) {
    const container = document.getElementById(containerId);
    if (!container) {
        return;
    }

    // Parse if string
    let data = newData;
    if (typeof newData === 'string') {
        try {
            data = JSON.parse(newData);
        } catch (e) {
            console.error('Error parsing chart data:', e);
            return;
        }
    }

    Plotly.react(containerId, data.data, data.layout || {});
}

// Load a chart by fetching its data from the URL in data-chart-url attribute
function loadChart(chartElement) {
    var url = chartElement.getAttribute('data-chart-url');
    if (!url) return;

    // Include date filter form params if available (dashboard page)
    var form = document.getElementById('date-filter-form');
    if (form) {
        var urlObj = new URL(url, window.location.origin);
        new FormData(form).forEach(function(v, k) { urlObj.searchParams.set(k, v); });
        url = urlObj.toString();
    }

    fetch(url)
        .then(function(response) {
            if (!response.ok) throw new Error('HTTP ' + response.status);
            return response.json();
        })
        .then(function(data) { renderChart(chartElement.id, data); })
        .catch(function(e) { console.error('Error loading chart:', e); });
}

// Load all non-whatif-projection charts in a scope
function loadAllCharts(scope) {
    const root = scope || document;
    root.querySelectorAll('[id^="chart-"][data-chart-url]').forEach(function(chart) {
        if (chart.closest('[data-whatif-projection-card]')) {
            return;
        }
        loadChart(chart);
    });
}

function formatCurrency(value) {
    const amount = Number(value || 0);
    return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        maximumFractionDigits: 0
    }).format(amount);
}

function updateProjectionDisplayMode(card, mode) {
    if (!card) return;
    const normalized = mode === 'real' ? 'real' : 'nominal';
    const chart = card.querySelector('#chart-projection');
    if (!chart) return;

    const baseUrl = card.getAttribute('data-chart-base-url') || '/whatif/chart/projection';
    chart.setAttribute('data-chart-url', `${baseUrl}?display_dollars=${normalized}`);

    card.querySelectorAll('.projection-display-toggle').forEach(function(btn) {
        const active = btn.getAttribute('data-display-dollars') === normalized;
        btn.classList.toggle('bg-indigo-600', active);
        btn.classList.toggle('text-white', active);
        btn.classList.toggle('bg-white', !active);
        btn.classList.toggle('dark:bg-gray-700', !active);
        btn.classList.toggle('text-gray-600', !active);
        btn.classList.toggle('dark:text-gray-200', !active);
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    });

    const balanceLabel = card.querySelector('#projection-final-balance-label');
    if (balanceLabel) {
        balanceLabel.textContent = normalized === 'real' ? 'Final Balance (Today\'s Dollars)' : 'Final Balance (Nominal)';
    }

    const balanceValue = card.querySelector('#projection-final-balance-value');
    if (balanceValue) {
        const rawValue = normalized === 'real'
            ? card.getAttribute('data-final-balance-real')
            : card.getAttribute('data-final-balance-nominal');
        balanceValue.textContent = formatCurrency(rawValue);
    }

    const caption = card.querySelector('#projection-display-caption');
    if (caption) {
        caption.textContent = normalized === 'real'
            ? (caption.getAttribute('data-caption-real') || '')
            : (caption.getAttribute('data-caption-nominal') || '');
    }

    if (window.localStorage) {
        window.localStorage.setItem('whatifProjectionDisplayDollars', normalized);
    }

    loadChart(chart);
}

function initWhatIfProjectionCards(root) {
    const scope = root || document;
    scope.querySelectorAll('[data-whatif-projection-card]').forEach(function(card) {
        card.querySelectorAll('.projection-display-toggle').forEach(function(btn) {
            btn.onclick = function() {
                updateProjectionDisplayMode(card, btn.getAttribute('data-display-dollars'));
            };
        });

        let savedMode = 'nominal';
        if (window.localStorage) {
            savedMode = window.localStorage.getItem('whatifProjectionDisplayDollars') || 'nominal';
        }
        updateProjectionDisplayMode(card, savedMode);
    });
}

// Reload charts after whatif-results is swapped
document.addEventListener('htmx:afterSettle', function(evt) {
    const target = evt.detail.target;
    if (target && target.id === 'whatif-results') {
        initWhatIfProjectionCards(target);
        loadAllCharts(target);
    }
});

// Initialize sparklines from data attributes
function initSparklines() {
    document.querySelectorAll('[id^="sparkline-"]').forEach(function(el) {
        const valuesAttr = el.getAttribute('data-values');
        const color = el.getAttribute('data-color') || '#6366f1';
        const targetAttr = el.getAttribute('data-target');
        const mode = el.getAttribute('data-mode') || '';

        if (valuesAttr && valuesAttr !== 'null' && valuesAttr !== '[]') {
            try {
                const values = JSON.parse(valuesAttr);
                if (values && values.length > 0) {
                    const options = {};
                    if (mode) {
                        options.mode = mode;
                    }
                    if (targetAttr !== null && targetAttr !== '') {
                        const t = parseFloat(targetAttr);
                        if (isFinite(t) && t > 0) {
                            options.target = t;
                        }
                    }
                    renderSparkline(el.id, values, color, options);
                }
            } catch (e) {
                console.error('Error parsing sparkline data:', e);
            }
        }
    });
}

// Initialize charts when page loads
document.addEventListener('DOMContentLoaded', function() {
    console.log('Charts.js initialized');
    initSparklines();
    initWhatIfProjectionCards(document);
    loadAllCharts();
});

// Reinitialize sparklines after HTMX swaps (for KPI updates)
document.body.addEventListener('htmx:afterSwap', function(evt) {
    if (evt.detail.target && evt.detail.target.id === 'kpis-container') {
        initSparklines();
    }
});

// Re-render all charts when theme changes
window.addEventListener('themechange', function() {
    // Re-render all chart containers
    document.querySelectorAll('[id^="chart-"]').forEach(function(el) {
        if (el._plotlyData) {
            // Plotly stores data on the element, re-render with new colors
            const colors = getThemeColors();
            Plotly.relayout(el.id, {
                'font.color': colors.text,
                'legend.font.color': colors.text,
                'xaxis.gridcolor': colors.gridColor,
                'xaxis.tickfont.color': colors.text,
                'yaxis.gridcolor': colors.gridColor,
                'yaxis.tickfont.color': colors.text
            });
        }
    });
});
