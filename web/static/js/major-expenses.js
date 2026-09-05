// Major Expenses page: filter-state persistence, date-range quick
// buttons/step arrows, and sortable-table client behavior. Extracted from
// pages/major-expenses.html (U7).

// The data-stop-row-click guard for the exception-row cells below (pin
// checkbox, "show in Explorer" link, pin-picker) lives in interactions.js,
// loaded sitewide and early enough to win the race against the prefill
// listener further down this file.

// Wire the date-range quick buttons/step arrows that used to carry inline
// onclick= handlers (U7).
document.addEventListener('DOMContentLoaded', function () {
    document.querySelectorAll('#me-date-range-buttons [data-step]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            meStepDateRange(parseInt(btn.dataset.step, 10));
        });
    });
    document.querySelectorAll('#me-date-range-buttons .date-range-btn[data-months]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            meSetDateRange(parseInt(btn.dataset.months, 10));
        });
    });
});

// Filter-form sessionStorage restore: bare /major-expenses with empty
// query + saved key → redirect once to /major-expenses?<saved>. Mirrors
// the pattern in pages/explorer.html.
(function () {
    const KEY = 'majorExpensesFilters';
    if (window.location.search === '' || window.location.search === '?') {
        const saved = sessionStorage.getItem(KEY);
        if (saved && !sessionStorage.getItem(KEY + '_restoring')) {
            sessionStorage.setItem(KEY + '_restoring', '1');
            window.location.replace('/major-expenses?' + saved);
            return;
        }
        sessionStorage.removeItem(KEY + '_restoring');
    } else {
        sessionStorage.removeItem(KEY + '_restoring');
    }

    // Save outgoing filter params on every htmx:configRequest from the
    // filter form. We do NOT save window.location.search inside this
    // handler — at this moment it still contains the OLD URL because
    // hx-push-url runs after the request settles. Instead pull start/end
    // from the form values so the saved value reflects what's about to
    // be sent.
    document.body.addEventListener('htmx:configRequest', function (evt) {
        const form = document.getElementById('major-expenses-filter-form');
        if (!form || !(evt.detail.elt === form || form.contains(evt.detail.elt))) return;
        const start = form.querySelector('input[name="start"]').value;
        const end = form.querySelector('input[name="end"]').value;
        const params = new URLSearchParams();
        if (start) params.set('start', start);
        if (end) params.set('end', end);
        sessionStorage.setItem(KEY, params.toString());
    });
})();

// Quick-range helpers — page-local copies of Explorer's setDateRange /
// stepDateRange / detectSelectedDateRange, scoped to the major-expenses
// filter form. Naming is `me*` to avoid colliding with Explorer's
// global-scope versions on the rare chance both pages ever co-exist.
function meParseDateLocal(s) {
    if (!s) return null;
    const parts = s.split('-').map(Number);
    if (parts.length !== 3 || parts.some(isNaN)) return null;
    return new Date(parts[0], parts[1] - 1, parts[2]);
}

function meFormatDateLocal(d) {
    const year = d.getFullYear();
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return year + '-' + month + '-' + day;
}

function meSetDateRange(months) {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const minDate = startInput.getAttribute('min');
    const maxDate = endInput.getAttribute('max');
    if (months === 0) {
        startInput.value = minDate;
        endInput.value = maxDate;
    } else {
        const end = new Date(maxDate);
        const start = new Date(end);
        start.setMonth(start.getMonth() - months);
        const minDateObj = new Date(minDate);
        if (start < minDateObj) start.setTime(minDateObj.getTime());
        startInput.value = start.toISOString().split('T')[0];
        endInput.value = maxDate;
    }
    form.dataset.activeMonths = String(months);
    meUpdateDateRangeButtons(months);
    htmx.trigger(form, 'submit');
}

// #90 (merged to master, preserved through the U7 script extraction at
// reconcile time): shift by exactly ONE calendar month regardless of window
// size, preserving day-of-month (clamped to month end); the preset highlight
// is kept. "All" (activeMonths === 0) is a no-op.
function meAddMonthsClamped(d, months) {
    const day = d.getDate();
    const r = new Date(d.getFullYear(), d.getMonth() + months, 1);
    const last = new Date(r.getFullYear(), r.getMonth() + 1, 0).getDate();
    r.setDate(Math.min(day, last));
    return r;
}
function meStepDateRange(direction) {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const currentStart = meParseDateLocal(startInput.value);
    const currentEnd = meParseDateLocal(endInput.value);
    if (!currentStart || !currentEnd) return;

    const activeMonths = parseInt(form.dataset.activeMonths || '-1', 10);
    if (activeMonths === 0) return;
    let newStart = meAddMonthsClamped(currentStart, direction);
    let newEnd = meAddMonthsClamped(currentEnd, direction);

    const minDate = meParseDateLocal(startInput.getAttribute('min'));
    const maxDate = meParseDateLocal(endInput.getAttribute('max'));
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

    const newStartStr = meFormatDateLocal(newStart);
    const newEndStr = meFormatDateLocal(newEnd);
    if (newStartStr === startInput.value && newEndStr === endInput.value) return;

    startInput.value = newStartStr;
    endInput.value = newEndStr;
    // Preset highlight is intentionally preserved across shifts —
    // form.dataset.activeMonths stays set so post-swap detection
    // re-applies the same highlight.
    htmx.trigger(form, 'submit');
}

function meUpdateDateRangeButtons(selectedMonths) {
    document.querySelectorAll('#me-date-range-buttons .date-range-btn').forEach(function (btn) {
        const btnMonths = parseInt(btn.getAttribute('data-months'));
        if (btnMonths === selectedMonths) {
            btn.classList.remove('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
            btn.classList.add('bg-accent-strong', 'bg-accent-strong', 'text-white', 'dark:text-white');
        } else {
            btn.classList.remove('bg-accent-strong', 'bg-accent-strong', 'text-white', 'dark:text-white');
            btn.classList.add('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
        }
    });
}

function meDetectSelectedDateRange() {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    // Honor an explicit activeMonths set by setDateRange / stepDateRange
    // so a scrolled window keeps its preset highlight across swaps.
    const explicit = parseInt(form.dataset.activeMonths || '', 10);
    if (!isNaN(explicit) && [0, 1, 2, 3, 6, 12].includes(explicit)) {
        meUpdateDateRangeButtons(explicit);
        return;
    }
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const minDate = startInput.getAttribute('min');
    const maxDate = endInput.getAttribute('max');
    const startDate = startInput.value;
    const endDate = endInput.value;
    if (endDate !== maxDate) { meUpdateDateRangeButtons(-1); return; }
    if (startDate === minDate) { meUpdateDateRangeButtons(0); return; }
    const end = new Date(maxDate);
    for (const months of [1, 2, 3, 6, 12]) {
        const expectedStart = new Date(end);
        expectedStart.setMonth(expectedStart.getMonth() - months);
        if (startDate === expectedStart.toISOString().split('T')[0]) {
            meUpdateDateRangeButtons(months);
            return;
        }
    }
    meUpdateDateRangeButtons(-1);
}

// Clear the stored preset when the user manually edits a date input,
// so post-swap detection runs from the date values instead of stale state.
document.addEventListener('DOMContentLoaded', function () {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    form.querySelectorAll('input[type="date"]').forEach(function (input) {
        input.addEventListener('change', function () {
            form.dataset.activeMonths = '';
        });
    });
});

// Highlight the correct quick-range button on first paint and after
// every HTMX swap of the wrapper (the inputs may have new values).
document.addEventListener('DOMContentLoaded', meDetectSelectedDateRange);
document.body.addEventListener('htmx:afterSwap', meDetectSelectedDateRange);

(function () {
    function visibleExceptionHashes() {
        return Array.from(
            document.querySelectorAll('tr.major-expenses-exception-row:not([style*="display: none"])')
        ).map(function (tr) {
            return tr.getAttribute('data-hash') || '';
        }).filter(Boolean);
    }

    // ---- Bulk-selection helpers (DOM is the source of truth) ----

    // Anchor for shift-click range select, keyed by bucket id. Reset
    // on every htmx:afterSwap so post-mutation state is clean.
    let lastCheckedByBucket = Object.create(null);

    function rowCheckboxes(bucketID) {
        const sel = bucketID
            ? 'input.major-expenses-pin-check[data-bucket="' + bucketID + '"]'
            : 'input.major-expenses-pin-check';
        return Array.from(document.querySelectorAll(sel));
    }

    function visibleRowCheckboxesInBucket(bucketID) {
        return rowCheckboxes(bucketID).filter(function (cb) {
            const tr = cb.closest('tr.major-expenses-exception-row');
            return tr && tr.style.display !== 'none';
        });
    }

    function checkedRowCheckboxes() {
        return rowCheckboxes().filter(function (cb) { return cb.checked; });
    }

    function countChecked() { return checkedRowCheckboxes().length; }

    function collectCheckedHashes() {
        return checkedRowCheckboxes().map(function (cb) {
            return cb.getAttribute('data-hash') || '';
        }).filter(Boolean);
    }

    function refreshBucketHeader(bucketID) {
        const header = document.getElementById('major-expenses-pin-check-header-' + bucketID);
        if (!header) return;
        const visible = visibleRowCheckboxesInBucket(bucketID);
        if (!visible.length) {
            header.checked = false;
            header.indeterminate = false;
            return;
        }
        const checkedCount = visible.filter(function (cb) { return cb.checked; }).length;
        if (checkedCount === 0) {
            header.checked = false;
            header.indeterminate = false;
        } else if (checkedCount === visible.length) {
            header.checked = true;
            header.indeterminate = false;
        } else {
            header.checked = false;
            header.indeterminate = true;
        }
    }

    function refreshAllBucketHeaders() {
        ['unmatched', 'anomalous', 'new-merchants'].forEach(refreshBucketHeader);
    }

    function refreshCountChip() {
        const chip = document.getElementById('major-expenses-pin-count-chip');
        const num = document.getElementById('major-expenses-pin-count-chip-num');
        if (!chip || !num) return;
        const n = countChecked();
        num.textContent = String(n);
        chip.classList.toggle('hidden', n === 0);
    }

    // Show/hide the bulk-pin toolbar and update its row count based on
    // the current visible-exception set.
    function syncBulkPinToolbar(visibleCount, query) {
        const bar = document.getElementById('major-expenses-bulk-pin');
        if (!bar) return;
        const counter = document.getElementById('major-expenses-bulk-pin-count');
        const apply = document.getElementById('major-expenses-bulk-pin-apply');
        const target = document.getElementById('major-expenses-bulk-pin-target');
        const clear = document.getElementById('major-expenses-bulk-pin-clear');
        const labelLead = bar.querySelector('span.major-expenses-bulk-pin-label-lead');

        const checkedCount = countChecked();
        const filterActive = (query || '').trim() !== '' && visibleCount > 0;

        // Mode priority: checked > filter > hidden.
        let mode = 'hidden';
        let displayCount = 0;
        if (checkedCount > 0) {
            mode = 'checked';
            displayCount = checkedCount;
        } else if (filterActive) {
            mode = 'filter';
            displayCount = visibleCount;
        }

        const visibleBar = mode !== 'hidden';
        bar.classList.toggle('hidden', !visibleBar);
        bar.classList.toggle('flex', visibleBar);

        if (counter) counter.textContent = String(displayCount);

        // Update the lead and trail labels between "Pin all"/"matching"
        // (filter) and "Pin"/"selected" (checked) by swapping each
        // span's textContent.
        if (labelLead) {
            labelLead.textContent = mode === 'checked' ? 'Pin ' : 'Pin all ';
        }
        const labelTrail = bar.querySelector('span.major-expenses-bulk-pin-label-trail');
        if (labelTrail) {
            labelTrail.textContent = mode === 'checked' ? ' selected →' : ' matching →';
        }

        if (apply) apply.disabled = !visibleBar || !target || !target.value;
        if (clear) clear.classList.toggle('hidden', mode !== 'checked');
    }

    function setRowOpen(tbody, open) {
        if (!tbody) return;
        tbody.setAttribute('data-open', open ? 'true' : 'false');
        const btn = tbody.querySelector('button.major-expense-row-toggle');
        if (btn) btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    }

    function applyUnifiedFilter(query) {
        const q = (query || '').toLowerCase().trim();

        // ---- LEFT CARD: iterate by tbody group, not by row. ----
        const groups = document.querySelectorAll('#major-expenses-list tbody[data-expense-id]');
        let visibleExpenses = 0;
        groups.forEach(function (group) {
            const summary = group.querySelector('tr.major-expense-item-row');
            const itemHay = (summary && summary.getAttribute('data-search') || '').toLowerCase();
            const itemMatch = q === '' || itemHay.includes(q);

            const txnRows = group.querySelectorAll('tr.major-expense-matched-row');
            let txnHadMatch = false;
            txnRows.forEach(function (tr) {
                const hay = (tr.getAttribute('data-search') || '').toLowerCase();
                const match = q === '' || hay.includes(q);
                tr.style.display = match ? '' : 'none';
                if (match && q !== '') txnHadMatch = true;
            });

            const show = q === '' || itemMatch || txnHadMatch;
            group.style.display = show ? '' : 'none';
            if (show) visibleExpenses++;

            // Force-open the row when the user matched a contained txn.
            // Empty queries leave open-state untouched so collapse
            // defaults survive.
            if (q !== '' && txnHadMatch) setRowOpen(group, true);
        });

        // ---- RIGHT CARD: Exceptions ----
        const exRows = document.querySelectorAll('tr.major-expenses-exception-row');
        let visibleExceptions = 0;
        exRows.forEach(function (tr) {
            const text = (tr.getAttribute('data-search') || '').toLowerCase();
            const match = q === '' || text.includes(q);
            tr.style.display = match ? '' : 'none';
            if (match) visibleExceptions++;
        });
        document.querySelectorAll('#major-expenses-results details').forEach(function (d) {
            if (q === '') return;
            const hasMatch = d.querySelector('tr.major-expenses-exception-row:not([style*="display: none"])');
            if (hasMatch) d.open = true;
        });

        // ---- Status badge ----
        const status = document.getElementById('major-expenses-search-status');
        if (status) {
            if (q === '') {
                status.classList.add('hidden');
                status.textContent = '';
            } else {
                status.classList.remove('hidden');
                status.textContent = visibleExpenses + ' expenses · ' + visibleExceptions + ' exceptions';
            }
        }
        const clear = document.getElementById('major-expenses-search-clear');
        if (clear) clear.classList.toggle('hidden', q === '');

        refreshAllBucketHeaders();
        syncBulkPinToolbar(visibleExceptions, q);
    }

    // Persist <details> open state AND tbody[data-open] across HTMX
    // swaps. Each swap replaces DOM nodes wholesale, so we snapshot
    // before, restore after.
    let savedOpenDetails = null;
    let savedOpenRows = null;
    let savedCheckedHashes = null;
    function persistedDetailsSelector() {
        return '#major-expenses-results details[id], #major-expenses-list-card details[id]';
    }
    document.body.addEventListener('htmx:beforeSwap', function () {
        const open = new Set();
        document.querySelectorAll(persistedDetailsSelector()).forEach(function (d) {
            if (d.open) open.add(d.id);
        });
        savedOpenDetails = open;

        const rows = new Set();
        document.querySelectorAll('#major-expenses-list tbody[data-expense-id][data-open="true"]').forEach(function (tb) {
            rows.add(tb.getAttribute('data-expense-id'));
        });
        savedOpenRows = rows;

        const checked = new Set();
        document.querySelectorAll('input.major-expenses-pin-check:checked').forEach(function (cb) {
            const h = cb.getAttribute('data-hash');
            if (h) checked.add(h);
        });
        savedCheckedHashes = checked;
    });
    document.body.addEventListener('htmx:afterSwap', function () {
        // Incoming markup has fresh, unchecked checkboxes. Anchors are
        // bucket-positional and can't survive a re-render, so wipe
        // those; checkbox selections are restored by hash below.
        lastCheckedByBucket = Object.create(null);

        if (savedOpenDetails) {
            document.querySelectorAll(persistedDetailsSelector()).forEach(function (d) {
                if (savedOpenDetails.has(d.id)) d.open = true;
            });
            savedOpenDetails = null;
        }
        if (savedOpenRows) {
            savedOpenRows.forEach(function (id) {
                const tb = document.querySelector('#major-expenses-list tbody[data-expense-id="' + CSS.escape(id) + '"]');
                if (tb) setRowOpen(tb, true);
            });
            savedOpenRows = null;
        }
        if (savedCheckedHashes && savedCheckedHashes.size > 0) {
            document.querySelectorAll('input.major-expenses-pin-check').forEach(function (cb) {
                const h = cb.getAttribute('data-hash');
                if (h && savedCheckedHashes.has(h)) cb.checked = true;
            });
        }
        savedCheckedHashes = null;

        const input = document.getElementById('major-expenses-search');
        if (input && input.value) applyUnifiedFilter(input.value);
        refreshAllBucketHeaders();
        refreshCountChip();
        // syncBulkPinToolbar isn't called by applyUnifiedFilter when
        // the search box is empty, so call it directly to surface the
        // bulk-pin bar for restored selections.
        const visibleExceptions = document.querySelectorAll(
            'tr.major-expenses-exception-row:not([style*="display: none"])'
        ).length;
        syncBulkPinToolbar(visibleExceptions, input ? input.value : '');
    });

    // Bulk-pin Apply.
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-apply') return;
        e.preventDefault();
        const target = document.getElementById('major-expenses-bulk-pin-target');
        if (!target || !target.value) return;
        const hashes = countChecked() > 0 ? collectCheckedHashes() : visibleExceptionHashes();
        if (!hashes.length) return;
        const fd = new FormData();
        fd.append('expense_id', target.value);
        hashes.forEach(function (h) { fd.append('hashes', h); });
        const filterForm = document.getElementById('major-expenses-filter-form');
        if (filterForm) {
            // Carry the active date window so the post-mutation render
            // preserves the user's filter. Guard each input — an OOB
            // swap mid-request could leave the form without one.
            const startInput = filterForm.querySelector('input[name="start"]');
            const endInput = filterForm.querySelector('input[name="end"]');
            if (startInput && startInput.value) fd.append('start', startInput.value);
            if (endInput && endInput.value) fd.append('end', endInput.value);
        }
        if (window.htmx && typeof window.htmx.ajax === 'function') {
            window.htmx.ajax('POST', '/major-expenses/pins/bulk', {
                target: '#major-expenses-results',
                swap: 'innerHTML',
                values: fd,
            });
        }
    });

    document.addEventListener('change', function (e) {
        if (e.target && e.target.id === 'major-expenses-bulk-pin-target') {
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }
    });

    // Row + header checkbox change. Delegated so HTMX swaps don't
    // need rebinding. Note: shift-click range is handled in the
    // 'click' phase (a separate listener) because by the time
    // 'change' fires the browser has already toggled the box.
    document.addEventListener('change', function (e) {
        const t = e.target;
        if (!t) return;

        // Header checkbox: toggle every visible row in this bucket
        // to the header's new state.
        if (t.classList && t.classList.contains('major-expenses-pin-check-header')) {
            const bucket = t.getAttribute('data-bucket') || '';
            const want = !!t.checked;
            visibleRowCheckboxesInBucket(bucket).forEach(function (cb) {
                cb.checked = want;
            });
            // Header was just clicked; keep its state authoritative
            // (refreshBucketHeader could reset it to indeterminate
            // if any visible row diverges, but right now they all
            // match `want`).
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
            return;
        }

        // Row checkbox: update bucket header and count chip. Anchor
        // is recorded on click (next task) — change does not touch it.
        if (t.classList && t.classList.contains('major-expenses-pin-check')) {
            const bucket = t.getAttribute('data-bucket') || '';
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
            return;
        }
    }); // end change listener

    // Click on a row checkbox. Two responsibilities:
    //   1. Record the bucket anchor (always, on every click).
    //   2. If shift is held AND there is a previous anchor in this
    //      bucket AND it is still visible, toggle the visible-range
    //      between anchor and target to the target's NEW value.
    //
    // Click fires before change; the target's `checked` reflects the
    // pre-click state during the listener. We compute the new state
    // as !target.checked.
    document.addEventListener('click', function (e) {
        const t = e.target;
        if (!t || !t.classList || !t.classList.contains('major-expenses-pin-check')) return;
        const bucket = t.getAttribute('data-bucket') || '';
        const targetHash = t.getAttribute('data-hash') || '';

        const anchorHash = lastCheckedByBucket[bucket] || '';
        const wantApplyRange = e.shiftKey && anchorHash && anchorHash !== targetHash;

        if (wantApplyRange) {
            const visible = visibleRowCheckboxesInBucket(bucket);
            const idxAnchor = visible.findIndex(function (cb) { return cb.getAttribute('data-hash') === anchorHash; });
            const idxTarget = visible.findIndex(function (cb) { return cb.getAttribute('data-hash') === targetHash; });
            if (idxAnchor === -1) {
                // Anchor no longer visible; treat as a fresh click.
                lastCheckedByBucket[bucket] = targetHash;
                return;
            }
            if (idxTarget === -1) {
                // Target not in visible set (rare race with a concurrent
                // filter); treat as a plain click.
                lastCheckedByBucket[bucket] = targetHash;
                return;
            }
            // Browser will toggle target after this listener returns;
            // its NEW state is !current. Apply that to every visible
            // checkbox in [min, max].
            const newState = !t.checked;
            const lo = Math.min(idxAnchor, idxTarget);
            const hi = Math.max(idxAnchor, idxTarget);
            for (let i = lo; i <= hi; i++) {
                visible[i].checked = newState;
            }
            // Refresh state synchronously; the change event for `t`
            // will still fire and trigger another refresh, which is
            // a harmless no-op.
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }

        // Always record the anchor on click (shift or not).
        lastCheckedByBucket[bucket] = targetHash;
    });

    // Clear button: uncheck every row + every header, reset the
    // anchor map, and re-sync everything visible.
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-clear') return;
        e.preventDefault();
        rowCheckboxes().forEach(function (cb) { cb.checked = false; });
        document.querySelectorAll('input.major-expenses-pin-check-header').forEach(function (h) {
            h.checked = false;
            h.indeterminate = false;
        });
        lastCheckedByBucket = Object.create(null);
        refreshCountChip();
        const input = document.getElementById('major-expenses-search');
        applyUnifiedFilter(input ? input.value : '');
    });

    // Search input (delegated; survives HTMX swaps).
    document.addEventListener('input', function (e) {
        if (e.target && e.target.id === 'major-expenses-search') {
            applyUnifiedFilter(e.target.value);
        }
    });

    document.addEventListener('click', function (e) {
        const clear = e.target && e.target.closest && e.target.closest('#major-expenses-search-clear');
        if (!clear) return;
        e.preventDefault();
        const input = document.getElementById('major-expenses-search');
        if (!input) return;
        input.value = '';
        applyUnifiedFilter('');
        input.focus();
    });

    // Add-panel toggle: [+] icon flips <details> open state and aria.
    // Opening from this button is a "blank-slate" create — clear any
    // pin_hash that an earlier "+ Create new from this" might have set.
    document.addEventListener('click', function (e) {
        const toggle = e.target && e.target.closest && e.target.closest('#major-expenses-add-toggle');
        if (!toggle) return;
        e.preventDefault();
        const panel = document.getElementById('major-expenses-add-panel');
        if (!panel) return;
        const willOpen = !panel.open;
        panel.open = willOpen;
        toggle.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
        if (willOpen) {
            const ph = document.getElementById('major-expenses-add-pin-hash');
            if (ph) ph.value = '';
            const hint = document.getElementById('major-expenses-add-pin-hash-hint');
            if (hint) hint.classList.add('hidden');
            const name = panel.querySelector('input[name="name"]');
            if (name) name.focus();
        }
    });

    // Prefill the add form from an exception row's data-fill-* attrs.
    // If pinHash is non-empty, also stage that hash so submit will pin
    // the originating transaction to the newly created expense.
    function prefillAddFormFromRow(fillRow, pinHash) {
        const form = document.getElementById('major-expenses-add-form');
        if (!form) return;
        const panel = document.getElementById('major-expenses-add-panel');
        const toggle = document.getElementById('major-expenses-add-toggle');
        if (panel && !panel.open) {
            panel.open = true;
            if (toggle) toggle.setAttribute('aria-expanded', 'true');
        }
        const desc = fillRow.getAttribute('data-fill-name') || '';
        const amount = parseFloat(fillRow.getAttribute('data-fill-amount') || '0');
        const isCheckLike = /\bcheck\b|^\s*#?\d{3,}\s*$/i.test(desc);

        const nameInput = form.querySelector('input[name="name"]');
        const kwInput = form.querySelector('input[name="keywords"]');
        const minInput = form.querySelector('input[name="expected_min"]');
        const maxInput = form.querySelector('input[name="expected_max"]');
        if (!nameInput || !kwInput || !minInput || !maxInput) return;

        nameInput.value = '';
        if (isCheckLike) {
            kwInput.value = '';
            if (amount > 0) {
                const exact = amount.toFixed(2);
                minInput.value = exact;
                maxInput.value = exact;
            }
        } else {
            kwInput.value = desc;
            minInput.value = '';
            maxInput.value = '';
        }

        const ph = document.getElementById('major-expenses-add-pin-hash');
        const hint = document.getElementById('major-expenses-add-pin-hash-hint');
        if (ph) ph.value = pinHash || '';
        if (hint) hint.classList.toggle('hidden', !pinHash);

        nameInput.focus();
        form.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }

    // "+ Create new from this…" sentinel in pin pickers. We intercept
    // before HTMX fires (the form's hx-trigger excludes value=='__new__'
    // already, but we also reset the select so it doesn't stay stuck on
    // the sentinel after the user backs out).
    document.addEventListener('change', function (e) {
        const select = e.target && e.target.closest && e.target.closest('select.major-expenses-pin-select');
        if (!select) return;
        if (select.value !== '__new__') return;
        const hash = select.getAttribute('data-hash') || '';
        const row = select.closest('tr[data-fill-name]');
        // Reset the select back to its previous selection (or placeholder)
        // so re-selecting "+ Create new" later still fires `change`.
        select.value = '';
        for (let i = 0; i < select.options.length; i++) {
            if (select.options[i].defaultSelected) {
                select.value = select.options[i].value;
                break;
            }
        }
        if (row) prefillAddFormFromRow(row, hash);
    });

    // Chevron toggle: flips data-open on the parent tbody.
    document.addEventListener('click', function (e) {
        const btn = e.target && e.target.closest && e.target.closest('button.major-expense-row-toggle');
        if (!btn) return;
        e.preventDefault();
        const tbody = btn.closest('tbody[data-expense-id]');
        if (!tbody) return;
        const open = tbody.getAttribute('data-open') !== 'true';
        setRowOpen(tbody, open);
    });

    // Click delegation for jump-to-existing AND exception-row prefill.
    document.addEventListener('click', function (e) {
        const tag = (e.target.tagName || '').toLowerCase();
        if (tag === 'input' || tag === 'button' || tag === 'svg' || tag === 'path' ||
            tag === 'select' || tag === 'option') return;

        // Jump-to-existing: open the target row first.
        const jumpEl = e.target.closest('[data-jump-expense]');
        if (jumpEl) {
            e.preventDefault();
            const id = jumpEl.getAttribute('data-jump-expense');
            const summary = document.getElementById('major-expense-item-' + id);
            const tbody = summary && summary.closest('tbody[data-expense-id]');
            if (!summary) return;
            setRowOpen(tbody, true);
            summary.scrollIntoView({ behavior: 'smooth', block: 'center' });
            summary.classList.add('ring-2', 'ring-warning');
            setTimeout(function () { summary.classList.remove('ring-2', 'ring-warning'); }, 1500);
            return;
        }

        // Exception-row prefill: open the add panel before filling.
        // Plain row click does NOT pin — only the "+ Create new from
        // this…" path does. Keyword-only matching is the historical
        // behavior and stays the default for clicking the row body.
        const fillRow = e.target.closest('tr[data-fill-name]');
        if (fillRow) {
            prefillAddFormFromRow(fillRow, '');
            return;
        }
    });

    // Sortable tables: any <table class="major-expenses-sortable"> with
    // <th data-sort-key data-sort-type="text|number"> headers will sort
    // its <tbody> rows on header click. Rows declare values via
    // data-sort-{key} attrs. State is per-table and resets on HTMX swap
    // (which is fine — the server's default sort takes over).
    const sortStates = new WeakMap();
    function applyTableSort(table, key, type, dir) {
        const cmp = function (av, bv) {
            let v;
            if (type === 'number') {
                v = (parseFloat(av) || 0) - (parseFloat(bv) || 0);
            } else {
                v = (av || '').localeCompare(bv || '', undefined, { sensitivity: 'base' });
            }
            return dir === 'asc' ? v : -v;
        };
        // Two layouts: a single tbody of <tr> rows (default), OR multiple
        // <tbody data-expense-id> groups under one table (the expenses
        // list, where each row is a group of summary+detail+matched-txn
        // sub-rows). data-sort-groups="tbody" picks the latter.
        if (table.getAttribute('data-sort-groups') === 'tbody') {
            const groups = Array.from(table.querySelectorAll(':scope > tbody[data-expense-id]'));
            groups.sort(function (a, b) {
                return cmp(a.getAttribute('data-sort-' + key), b.getAttribute('data-sort-' + key));
            });
            groups.forEach(function (g) { table.appendChild(g); });
        } else {
            const tbody = table.querySelector('tbody');
            if (!tbody) return;
            const rows = Array.from(tbody.querySelectorAll('tr'));
            rows.sort(function (a, b) {
                return cmp(a.getAttribute('data-sort-' + key), b.getAttribute('data-sort-' + key));
            });
            rows.forEach(function (r) { tbody.appendChild(r); });
        }
        table.querySelectorAll('thead th[data-sort-key]').forEach(function (th) {
            const ind = th.querySelector('.major-expenses-sort-indicator');
            if (th.getAttribute('data-sort-key') === key) {
                if (ind) ind.textContent = dir === 'asc' ? '▲' : '▼';
                th.setAttribute('aria-sort', dir === 'asc' ? 'ascending' : 'descending');
            } else {
                if (ind) ind.textContent = '';
                th.setAttribute('aria-sort', 'none');
            }
        });
    }
    document.addEventListener('click', function (e) {
        const th = e.target && e.target.closest && e.target.closest('th[data-sort-key]');
        if (!th) return;
        const table = th.closest('table.major-expenses-sortable');
        if (!table) return;
        const key = th.getAttribute('data-sort-key');
        const type = th.getAttribute('data-sort-type') || 'text';
        let state = sortStates.get(table);
        if (!state || state.key !== key) {
            // First click on a column: numeric → desc (biggest first), text → asc.
            state = { key: key, dir: type === 'number' ? 'desc' : 'asc' };
        } else {
            state.dir = state.dir === 'asc' ? 'desc' : 'asc';
        }
        sortStates.set(table, state);
        applyTableSort(table, key, type, state.dir);
    });
})();
