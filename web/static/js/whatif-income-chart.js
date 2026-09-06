// What-If Yearly Income chart: nominal/today's-dollar toggle. Extracted
// from components/whatif/income-chart.html (U7).

(function() {
    const card = document.querySelector('[data-whatif-income-card]');
    if (!card) return;
    const baseUrl = card.dataset.chartBaseUrl || '/whatif/chart/income';
    const container = card.querySelector('#chart-income');
    const toggles = card.querySelectorAll('.income-display-toggle');

    function refresh(mode) {
        const url = baseUrl + '?display_dollars=' + encodeURIComponent(mode);
        container.dataset.chartUrl = url;
        if (typeof loadChart === 'function') {
            loadChart(container);
        }
    }

    toggles.forEach(function(btn) {
        btn.addEventListener('click', function() {
            const mode = btn.dataset.incomeDisplayDollars;
            toggles.forEach(function(other) {
                const isActive = other === btn;
                other.setAttribute('aria-pressed', isActive ? 'true' : 'false');
                other.classList.toggle('bg-accent-strong', isActive);
                other.classList.toggle('text-white', isActive);
                other.classList.toggle('bg-white', !isActive);
                other.classList.toggle('dark:bg-gray-700', !isActive);
                other.classList.toggle('text-gray-600', !isActive);
                other.classList.toggle('dark:text-gray-200', !isActive);
            });
            refresh(mode);
        });
    });
})();
