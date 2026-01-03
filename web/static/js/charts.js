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

    // Add click handler for category pie chart
    if (containerId === 'chart-category') {
        const container = document.getElementById(containerId);
        container.on('plotly_click', function(eventData) {
            if (eventData.points && eventData.points.length > 0) {
                const category = eventData.points[0].label;
                if (category && typeof openCategoryDrilldown === 'function') {
                    openCategoryDrilldown(category);
                }
            }
        });
    }
}

/**
 * Render a sparkline chart
 * @param {string} containerId - The ID of the container element
 * @param {number[]} values - The data values
 * @param {string} color - The line color
 */
function renderSparkline(containerId, values, color) {
    const container = document.getElementById(containerId);
    if (!container || !values || values.length === 0) {
        return;
    }

    const data = [{
        type: 'scatter',
        mode: 'lines',
        y: values,
        line: {
            color: color || '#6366f1',
            width: 2
        },
        fill: 'tozeroy',
        fillcolor: (color || '#6366f1') + '20'
    }];

    const layout = {
        margin: { t: 0, r: 0, b: 0, l: 0 },
        paper_bgcolor: 'transparent',
        plot_bgcolor: 'transparent',
        xaxis: {
            visible: false
        },
        yaxis: {
            visible: false
        },
        showlegend: false
    };

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

// Handle chart data responses from HTMX
document.addEventListener('htmx:afterRequest', function(evt) {
    const target = evt.detail.target;
    if (target && target.id && target.id.startsWith('chart-')) {
        try {
            const data = JSON.parse(evt.detail.xhr.responseText);
            renderChart(target.id, data);
        } catch (e) {
            console.error('Error rendering chart:', e);
        }
    }
});

// Initialize sparklines from data attributes
function initSparklines() {
    document.querySelectorAll('[id^="sparkline-"]').forEach(function(el) {
        const valuesAttr = el.getAttribute('data-values');
        const color = el.getAttribute('data-color') || '#6366f1';

        if (valuesAttr && valuesAttr !== 'null' && valuesAttr !== '[]') {
            try {
                const values = JSON.parse(valuesAttr);
                if (values && values.length > 0) {
                    renderSparkline(el.id, values, color);
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
