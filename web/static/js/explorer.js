// Explorer page: filter-state persistence, date-range quick buttons/step
// arrows, sort-by-header clicks, infinite scroll, and inline description
// rename. Extracted from pages/explorer.html (U7).

    // Persist explorer filter state across tab changes
    (function() {
        const STORAGE_KEY = 'explorerFilters';

        // On bare /explorer load (no query params), restore saved filters
        if (window.location.search === '' || window.location.search === '?') {
            const saved = sessionStorage.getItem(STORAGE_KEY);
            if (saved && !sessionStorage.getItem(STORAGE_KEY + '_restoring')) {
                sessionStorage.setItem(STORAGE_KEY + '_restoring', '1');
                window.location.replace('/explorer?' + saved);
                return; // stop further script execution during redirect
            }
            sessionStorage.removeItem(STORAGE_KEY + '_restoring');
        } else {
            // We have query params - save them (strip leading ?)
            sessionStorage.removeItem(STORAGE_KEY + '_restoring');
            sessionStorage.setItem(STORAGE_KEY, window.location.search.substring(1));
        }

        // Save filter state before every HTMX request from the form
        document.body.addEventListener('htmx:configRequest', function(evt) {
            const form = document.getElementById('explorer-filter-form');
            if (form && (evt.detail.elt === form || form.contains(evt.detail.elt))) {
                const params = new URLSearchParams(evt.detail.parameters);
                // Don't persist page number - always start at page 1
                params.set('page', '1');
                sessionStorage.setItem(STORAGE_KEY, params.toString());
            }
        });
    })();

    // Clear search input
    function clearSearch() {
        const form = document.getElementById('explorer-filter-form');
        const searchInput = document.getElementById('search-input');
        const clearBtn = document.getElementById('clear-search-btn');
        const pageInput = form.querySelector('input[name="page"]');

        searchInput.value = '';
        clearBtn.classList.add('hidden');
        pageInput.value = '1';

        htmx.trigger(form, 'submit');
    }

    // Show/hide clear button based on search input
    document.addEventListener('DOMContentLoaded', function() {
        const searchInput = document.getElementById('search-input');
        const clearBtn = document.getElementById('clear-search-btn');

        if (searchInput && clearBtn) {
            searchInput.addEventListener('input', function() {
                if (this.value) {
                    clearBtn.classList.remove('hidden');
                } else {
                    clearBtn.classList.add('hidden');
                }
            });
        }

        // Highlight the correct date range button on page load
        detectSelectedDateRange();

        // Clear stored activeMonths when the user manually edits a date
        // input, so post-swap detection runs from the date values.
        const filterForm = document.getElementById('explorer-filter-form');
        if (filterForm) {
            filterForm.querySelectorAll('input[type="date"]').forEach(function (input) {
                input.addEventListener('change', function () {
                    filterForm.dataset.activeMonths = '';
                });
            });
        }
    });

    // Re-detect after every HTMX swap (the inputs may have new values).
    document.body.addEventListener('htmx:afterSwap', detectSelectedDateRange);

    // Parse "YYYY-MM-DD" as a local-timezone date (avoids UTC off-by-one).
    function parseDateLocal(s) {
        if (!s) return null;
        const parts = s.split('-').map(Number);
        if (parts.length !== 3 || parts.some(isNaN)) return null;
        return new Date(parts[0], parts[1] - 1, parts[2]);
    }

    function formatDateLocal(d) {
        const year = d.getFullYear();
        const month = String(d.getMonth() + 1).padStart(2, '0');
        const day = String(d.getDate()).padStart(2, '0');
        return year + '-' + month + '-' + day;
    }

    // Step the date range forward or backward.
    // If a month-based preset is active (1/2/3/6/12), shift by N months
    // preserving day-of-month and keeping the preset highlight. Otherwise,
    // shift by the current span in days. The "All" preset is a no-op.
    // #90 (merged to master, preserved through the U7 script extraction at
    // reconcile time): arrows step by exactly ONE calendar month regardless
    // of window size, preserving day-of-month (clamped to month end). "All"
    // (activeMonths === 0) is a no-op.
    function addMonthsClampedExplorer(d, months) {
        const day = d.getDate();
        const r = new Date(d.getFullYear(), d.getMonth() + months, 1);
        const last = new Date(r.getFullYear(), r.getMonth() + 1, 0).getDate();
        r.setDate(Math.min(day, last));
        return r;
    }
    function stepDateRange(direction) {
        const form = document.getElementById('explorer-filter-form');
        const startInput = form.querySelector('input[name="start"]');
        const endInput = form.querySelector('input[name="end"]');
        const pageInput = form.querySelector('input[name="page"]');

        const currentStart = parseDateLocal(startInput.value);
        const currentEnd = parseDateLocal(endInput.value);
        if (!currentStart || !currentEnd) return;

        const activeMonths = parseInt(form.dataset.activeMonths || '-1', 10);
        if (activeMonths === 0) return;
        let newStart = addMonthsClampedExplorer(currentStart, direction);
        let newEnd = addMonthsClampedExplorer(currentEnd, direction);

        const minDate = parseDateLocal(startInput.getAttribute('min'));
        const maxDate = parseDateLocal(endInput.getAttribute('max'));
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

        const newStartStr = formatDateLocal(newStart);
        const newEndStr = formatDateLocal(newEnd);
        if (newStartStr === startInput.value && newEndStr === endInput.value) return;

        startInput.value = newStartStr;
        endInput.value = newEndStr;

        // Preset highlight is intentionally preserved across shifts —
        // form.dataset.activeMonths stays set so post-swap detection
        // re-applies the same highlight.
        pageInput.value = '1';
        htmx.trigger(form, 'submit');
    }

    // Set date range (months back from max date, or 0 for all)
    function setDateRange(months) {
        const form = document.getElementById('explorer-filter-form');
        const startInput = form.querySelector('input[name="start"]');
        const endInput = form.querySelector('input[name="end"]');
        const pageInput = form.querySelector('input[name="page"]');

        const minDate = startInput.getAttribute('min');
        const maxDate = endInput.getAttribute('max');

        if (months === 0) {
            // All - use full range
            startInput.value = minDate;
            endInput.value = maxDate;
        } else {
            // Calculate start date as X months before max date
            const end = new Date(maxDate);
            const start = new Date(end);
            start.setMonth(start.getMonth() - months);

            // Ensure start doesn't go before min date
            const minDateObj = new Date(minDate);
            if (start < minDateObj) {
                start.setTime(minDateObj.getTime());
            }

            startInput.value = start.toISOString().split('T')[0];
            endInput.value = maxDate;
        }

        // Update button selection state
        form.dataset.activeMonths = String(months);
        updateDateRangeButtons(months);

        pageInput.value = '1';
        htmx.trigger(form, 'submit');
    }

    // Update which date range button appears selected
    function updateDateRangeButtons(selectedMonths) {
        const buttons = document.querySelectorAll('.date-range-btn');
        buttons.forEach(btn => {
            const btnMonths = parseInt(btn.getAttribute('data-months'));
            if (btnMonths === selectedMonths) {
                // Selected state - indigo highlight
                btn.classList.remove('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
                btn.classList.add('bg-accent-strong', 'bg-accent-strong', 'text-white', 'dark:text-white');
            } else {
                // Default state
                btn.classList.remove('bg-accent-strong', 'bg-accent-strong', 'text-white', 'dark:text-white');
                btn.classList.add('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
            }
        });
    }

    // Detect which date range button should be selected based on current dates
    function detectSelectedDateRange() {
        const form = document.getElementById('explorer-filter-form');
        // Honor an explicit activeMonths set by setDateRange / stepDateRange
        // so a scrolled window keeps its preset highlight across swaps.
        const explicit = parseInt(form.dataset.activeMonths || '', 10);
        if (!isNaN(explicit) && [0, 1, 2, 3, 6, 12].includes(explicit)) {
            updateDateRangeButtons(explicit);
            return;
        }
        const startInput = form.querySelector('input[name="start"]');
        const endInput = form.querySelector('input[name="end"]');

        const minDate = startInput.getAttribute('min');
        const maxDate = endInput.getAttribute('max');
        const startDate = startInput.value;
        const endDate = endInput.value;

        // If end date isn't max, no button matches
        if (endDate !== maxDate) {
            updateDateRangeButtons(-1);
            return;
        }

        // Check if "All" is selected
        if (startDate === minDate) {
            updateDateRangeButtons(0);
            return;
        }

        // Check for 1, 2, 3, 6, 12 month ranges
        const end = new Date(maxDate);
        for (const months of [1, 2, 3, 6, 12]) {
            const expectedStart = new Date(end);
            expectedStart.setMonth(expectedStart.getMonth() - months);
            if (startDate === expectedStart.toISOString().split('T')[0]) {
                updateDateRangeButtons(months);
                return;
            }
        }

        updateDateRangeButtons(-1);
    }

    // Filter by clicking on a description
    function filterByDescription(description) {
        const form = document.getElementById('explorer-filter-form');
        const searchInput = form.querySelector('input[name="search"]');
        const clearBtn = document.getElementById('clear-search-btn');
        const pageInput = form.querySelector('input[name="page"]');

        searchInput.value = description;
        if (clearBtn) clearBtn.classList.remove('hidden');
        pageInput.value = '1';

        htmx.trigger(form, 'submit');
    }

    // Update sort parameters and trigger refresh
    function sortBy(column) {
        const form = document.getElementById('explorer-filter-form');
        const sortInput = form.querySelector('input[name="sort"]');
        const orderInput = form.querySelector('input[name="order"]');
        const pageInput = form.querySelector('input[name="page"]');

        if (sortInput.value === column) {
            // Toggle order
            orderInput.value = orderInput.value === 'asc' ? 'desc' : 'asc';
        } else {
            sortInput.value = column;
            orderInput.value = column === 'date' ? 'desc' : 'asc';
        }

        // Reset to page 1 when sorting changes
        pageInput.value = '1';

        // Trigger HTMX request
        htmx.trigger(form, 'submit');
    }

    // Go to specific page
    function goToPage(page) {
        const form = document.getElementById('explorer-filter-form');
        const pageInput = form.querySelector('input[name="page"]');
        pageInput.value = page;
        htmx.trigger(form, 'submit');
    }


    // Infinite scroll handler for nested scrollable container
    // HTMX's revealed trigger doesn't work with nested scroll containers
    function setupInfiniteScroll() {
        const scrollContainer = document.querySelector('#transactions-container .overflow-y-auto');
        if (!scrollContainer) return;

        let loading = false;

        scrollContainer.addEventListener('scroll', function() {
            if (loading) return;

            // Check if we're near the bottom (within 100px)
            const scrollBottom = this.scrollTop + this.clientHeight;
            const threshold = this.scrollHeight - 100;

            if (scrollBottom >= threshold) {
                // Find the LAST row with hx-get attribute (the infinite scroll trigger)
                // querySelector returns first match, but we need the last one after appends
                const triggerRows = document.querySelectorAll('#transaction-rows tr[hx-get]');
                const lastTriggerRow = triggerRows.length > 0 ? triggerRows[triggerRows.length - 1] : null;
                if (lastTriggerRow) {
                    loading = true;
                    htmx.trigger(lastTriggerRow, 'revealed');
                    // Reset loading flag after request completes
                    setTimeout(() => { loading = false; }, 500);
                }
            }
        });
    }

    // Setup on initial load
    setupInfiniteScroll();

    // Re-setup after HTMX swaps content
    document.body.addEventListener('htmx:afterSwap', function(e) {
        if (e.detail.target.id === 'transactions-container') {
            setupInfiniteScroll();
        }
    });

    // Build the description cell content using safe DOM methods
    function buildDescriptionContent(displayName, description) {
        const wrapper = document.createElement('span');
        wrapper.className = 'alias-display cursor-pointer hover:text-accent';
        wrapper.title = 'Click to filter, double-click to rename';

        if (displayName) {
            const nameSpan = document.createElement('span');
            nameSpan.className = 'font-medium';
            nameSpan.textContent = displayName;
            wrapper.appendChild(nameSpan);

            wrapper.appendChild(document.createTextNode(' '));

            const origSpan = document.createElement('span');
            origSpan.className = 'text-body-sm text-gray-500 dark:text-gray-300';
            origSpan.textContent = '(' + description + ')';
            wrapper.appendChild(origSpan);
        } else {
            wrapper.textContent = description;
        }

        wrapper.addEventListener('click', function() {
            filterByDescription(displayName || description);
        });

        return wrapper;
    }

    // Double-click on description to rename
    document.addEventListener('dblclick', function(e) {
        const td = e.target.closest('td[data-hash]');
        if (!td || td.querySelector('input')) return;

        const hash = td.dataset.hash;
        const description = td.dataset.description;
        const displayName = td.dataset.displayName;

        const input = document.createElement('input');
        input.type = 'text';
        input.value = displayName || '';
        input.placeholder = description;
        input.className = 'w-full border border-accent dark:bg-gray-700 dark:text-gray-100 rounded px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-accent';

        td.textContent = '';
        td.appendChild(input);
        input.focus();
        input.select();

        function save() {
            const newName = input.value.trim();
            const form = new FormData();
            form.append('hash', hash);
            form.append('display_name', newName);

            fetch('/explorer/alias', { method: 'POST', body: form })
                .then(resp => {
                    if (!resp.ok) throw new Error('Save failed');
                    td.dataset.displayName = newName;
                    td.textContent = '';
                    td.appendChild(buildDescriptionContent(newName, description));
                })
                .catch(() => {
                    td.textContent = '';
                    td.appendChild(buildDescriptionContent(displayName, description));
                });
        }

        input.addEventListener('blur', save);
        input.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
            if (e.key === 'Escape') {
                input.removeEventListener('blur', save);
                td.textContent = '';
                td.appendChild(buildDescriptionContent(displayName, description));
            }
        });
    });

// Wire the controls that used to carry inline onclick= handlers (U7).
// Delegated where the element can be re-rendered by an HTMX swap
// (filter-by-description spans, in particular); direct listeners where
// the element is part of the static page shell.
document.addEventListener('DOMContentLoaded', function () {
    document.getElementById('clear-search-btn').addEventListener('click', clearSearch);

    document.querySelectorAll('[data-step]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            stepDateRange(parseInt(btn.dataset.step, 10));
        });
    });

    document.querySelectorAll('.date-range-btn[data-months]').forEach(function (btn) {
        btn.addEventListener('click', function () {
            setDateRange(parseInt(btn.dataset.months, 10));
        });
    });

    document.querySelectorAll('[data-sort-key]').forEach(function (th) {
        th.addEventListener('click', function () {
            sortBy(th.dataset.sortKey);
        });
    });

    var clearFilters = document.getElementById('explorer-clear-filters');
    if (clearFilters) {
        clearFilters.addEventListener('click', function () {
            sessionStorage.removeItem('explorerFilters');
        });
    }
});

// Filter-by-description spans are re-rendered on every row swap (infinite
// scroll, sort, filter), so this listener is delegated off document rather
// than bound per-element.
document.addEventListener('click', function (e) {
    var span = e.target.closest('[data-filter-by]');
    if (span) filterByDescription(span.getAttribute('data-filter-by'));
});
