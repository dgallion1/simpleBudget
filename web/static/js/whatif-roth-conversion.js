// Keep the saved schedule intact until a fixed value is explicitly edited.
function toggleRothConversionFields() {
    const enabled = document.getElementById('roth-conversion-enabled').checked;
    document.getElementById('roth-conversion-fields').classList.toggle('hidden', !enabled);
}

function editFixedRothConversion() {
    const fields = document.getElementById('roth-fixed-fields');
    fields.disabled = false;
    fields.classList.remove('hidden');
    document.getElementById('roth-annual-amount').focus();
}

(function () {
    if (window.rothRecommendationEventsInstalled) return;
    window.rothRecommendationEventsInstalled = true;

    function isRecommendation(event) {
        return event.detail.elt?.closest('#roth-recommendations-controls');
    }
    function progress(text) {
        const node = document.getElementById('roth-recommendations-progress');
        if (node) node.textContent = text;
    }
    document.body.addEventListener('htmx:beforeRequest', function (event) {
        if (!isRecommendation(event)) return;
        const applying = event.detail.elt.tagName === 'FORM';
        progress(applying ? 'Applying Roth plan and Social Security ages…' : 'Finding recommendations. This may take a few seconds…');
        document.getElementById('roth-recommendations-results')?.setAttribute('aria-busy', 'true');
    });
    document.body.addEventListener('htmx:beforeSwap', function (event) {
        if (event.detail.target?.id !== 'roth-recommendations-results') return;
        // HTMX normally discards non-2xx fragments. Our server sends scoped,
        // escaped recovery messages with meaningful HTTP error statuses.
        if (event.detail.xhr.status >= 400) {
            event.detail.shouldSwap = true;
            event.detail.isError = false;
        }
    });
    document.body.addEventListener('htmx:afterSwap', function (event) {
        if (event.detail.target?.id !== 'roth-recommendations-results') return;
        const target = event.detail.target;
        target.removeAttribute('aria-busy');
        progress('');
        target.querySelector('h3, [role="alert"]')?.focus();
    });
    document.body.addEventListener('htmx:afterRequest', function (event) {
        if (!isRecommendation(event)) return;
        document.getElementById('roth-recommendations-results')?.removeAttribute('aria-busy');
        if (event.detail.xhr.status === 0) {
            progress('The connection was interrupted. Find recommendations again and retry.');
        }
    });
    document.body.addEventListener('whatif:revision', function (event) {
        const target = document.getElementById('roth-recommendations-results');
        if (!target || !target.querySelector('form')) return;
        const marker = target.querySelector('[data-roth-revision]');
        const shown = Number(marker?.dataset.rothRevision);
        const detail = event.detail;
        const value = detail && typeof detail === 'object' ? detail.value : detail;
        const next = typeof value === 'number' ? value
            : typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : NaN;
        // The initial poll reports the current revision without changing the
        // plan. Compare with this displayed set, not global polling state.
        if (!Number.isSafeInteger(shown) || !Number.isSafeInteger(next) || next < 0 || next <= shown) return;
        const restoreFocus = target.contains(document.activeElement);
        target.replaceChildren();
        progress('The plan changed. Find recommendations again to use the latest settings.');
        if (restoreFocus) document.querySelector('#roth-recommendations-controls > button')?.focus();
    });
    function announceApplied() {
        const recommendation = window.location.hash === '#roth-recommendation-applied';
        const fixed = window.location.hash === '#roth-fixed-applied';
        if (!recommendation && !fixed) return;
        const status = document.getElementById('roth-conversion-status');
        if (!status) return;
        status.textContent = recommendation
            ? 'Roth plan and Social Security claim ages applied. The planner has been refreshed.'
            : 'Fixed annual Roth conversions applied. The planner has been refreshed.';
        document.getElementById('roth-conversion-heading')?.focus();
        const cleanURL = new URL(window.location.href);
        cleanURL.searchParams.delete('roth_applied');
        cleanURL.hash = '';
        history.replaceState(null, '', cleanURL.pathname + cleanURL.search);
    }
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', announceApplied);
    else announceApplied();
})();
