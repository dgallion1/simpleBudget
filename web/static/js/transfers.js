// Transfers page: after an HTMX swap of #transfers-queue, move focus to
// the region's heading (ACCESSIBILITY.md point 10) and render the flow
// chart. Extracted from pages/transfers.html (U7).

    (function () {
        document.body.addEventListener('htmx:afterSwap', function (evt) {
            var target = evt.detail && evt.detail.target;
            if (!target || target.id !== 'transfers-queue') return;
            var heading = target.querySelector('#transfers-queue-heading')
                || target.querySelector('[data-focus-target]');
            if (heading) {
                try { heading.focus(); } catch (e) {}
            }
        });

        /* Render the transfers flow chart from the inline JSON payload.
             Uses the existing renderChart from charts.js, which themes for
             both light and dark mode (ACCESSIBILITY.md point 12). */
        var flowEl = document.getElementById('chart-transfers-flow');
        if (flowEl && typeof renderChart === 'function') {
            var payload = flowEl.getAttribute('data-chart-payload');
            if (payload) {
                renderChart('chart-transfers-flow', payload);
            }
        }
    })();
