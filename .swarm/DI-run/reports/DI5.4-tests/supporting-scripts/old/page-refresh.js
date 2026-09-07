// Explicit MCP refresh requests, independent of the What-If revision poll.
(function () {
    'use strict';
    var baseline = null, pending = false, polling = false, reloading = false;
    var dirty = false, submitting = false, notice = null, dismissed = false;
    var returnFocus = null, detachedFocus = null;
    var requests = new Set();

    // Deliberately sticky: an automatic save or DOM swap is not proof that
    // every edit on this page was saved successfully.
    function edited(evt) {
        if (evt.target.matches('input, select, textarea, [contenteditable]')) dirty = true;
    }
    document.addEventListener('input', edited, true);
    document.addEventListener('change', edited, true);
    document.addEventListener('submit', function (evt) {
        // HTMX and other handled submissions cancel the native navigation.
        Promise.resolve().then(function () { if (!evt.defaultPrevented) submitting = true; });
    }, true);
    document.addEventListener('htmx:beforeRequest', function (evt) {
        if (evt.detail && evt.detail.xhr) requests.add(evt.detail.xhr);
    });
    document.addEventListener('htmx:afterRequest', function (evt) {
        if (evt.detail) requests.delete(evt.detail.xhr);
    });
    window.addEventListener('pageshow', function () { submitting = false; });

    function reload() {
        if (reloading) return;
        reloading = true;
        window.location.reload(); // Retains this tab's path, query and hash.
    }
    function keepVisible(focus) {
        if (focus && focus !== document.body && focus.isConnected) {
            var rect = focus.getBoundingClientRect();
            if (rect.top < 0 || rect.bottom > window.innerHeight ||
                rect.left < 0 || rect.right > window.innerWidth) {
                focus.scrollIntoView({block: 'nearest', inline: 'nearest', behavior: 'instant'});
            }
        }
    }
    function hasMeaningfulFocus() {
        var active = document.activeElement;
        return active && active !== document.body &&
            active !== document.documentElement && active.isConnected;
    }
    // Capture at actual removal, not while a request or delayed/cancelled swap
    // is pending. A partial outside this region has no focus ownership.
    document.addEventListener('htmx:beforeCleanupElement', function (evt) {
        var removed = evt.detail && evt.detail.elt;
        if (notice && removed && removed.contains(notice)) {
            detachedFocus = notice.contains(document.activeElement) ? document.activeElement : null;
        }
    });
    function mountNotice() {
        var restore = detachedFocus;
        detachedFocus = null;
        if (!notice || notice.isConnected) return;
        var focus = document.activeElement;
        var parent = document.querySelector('main') || document.body;
        parent.insertBefore(notice, parent.firstChild);
        // Restore only the surviving action whose detachment lost focus. An
        // autofocus target or another handler/user's new focus takes priority.
        if (restore && notice.contains(restore) && !hasMeaningfulFocus()) {
            restore.focus({preventScroll: true});
            focus = restore;
        }
        keepVisible(focus);
    }
    // A main replacement can detach the pending region. Reuse its existing
    // status and handlers; unrelated partial swaps must not repeat it.
    document.addEventListener('htmx:afterSwap', mountNotice);
    function removeNotice() {
        if (!notice) { detachedFocus = null; return; }
        var owned = notice.contains(document.activeElement) ||
            (detachedFocus && !hasMeaningfulFocus());
        var target = returnFocus;
        // Invalidate before removal so later swap events cannot resurrect it.
        var removed = notice;
        notice = null; detachedFocus = null; returnFocus = null;
        removed.remove();
        if (!owned) return;
        if (target && target !== document.body && target.isConnected) {
            target.focus({preventScroll: true});
            if (document.activeElement === target) { keepVisible(target); return; }
        }
        target = document.querySelector('main');
        if (target) {
            var tabindex = target.getAttribute('tabindex');
            target.setAttribute('tabindex', '-1');
            target.focus({preventScroll: true});
            if (tabindex === null) target.removeAttribute('tabindex');
            else target.setAttribute('tabindex', tabindex);
            keepVisible(target);
        }
    }
    function consider() {
        if (!pending || reloading || document.visibilityState !== 'visible' || submitting || requests.size) return;
        if (!dirty) { reload(); return; }
        if (notice || dismissed) return;
        notice = document.createElement('section');
        notice.setAttribute('aria-label', 'Page refresh');
        returnFocus = document.activeElement;
        notice.className = 'mb-4 p-4 rounded-lg shadow bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100';
        var message = document.createElement('p');
        message.setAttribute('role', 'status');
        message.textContent = 'A page refresh was requested. Your edits have been kept. Refreshing will discard unsaved edits.';
        notice.appendChild(message);
        var refresh = document.createElement('button');
        refresh.type = 'button';
        refresh.className = 'px-3 py-2 rounded-md border';
        refresh.style.borderColor = 'currentColor';
        refresh.textContent = 'Refresh page';
        refresh.addEventListener('click', function () {
            if (submitting || requests.size) return;
            if (window.confirm('Discard unsaved edits and refresh this page?')) reload();
        });
        notice.appendChild(refresh);
        var dismiss = document.createElement('button');
        dismiss.type = 'button';
        dismiss.className = 'px-3 py-2 rounded-md border';
        dismiss.style.borderColor = 'currentColor';
        dismiss.textContent = 'Keep editing';
        dismiss.addEventListener('click', function () {
            // Remove the announcement as well as the visible notice.
            dismissed = true;
            removeNotice();
        });
        notice.appendChild(dismiss);
        [refresh, dismiss].forEach(function (button) {
            button.addEventListener('focus', function () {
                button.style.outline = '2px solid currentColor';
                button.style.outlineOffset = '2px';
            });
            button.addEventListener('blur', function () { button.style.outline = ''; });
        });
        mountNotice();
    }
    async function poll() {
        if (polling || reloading || document.visibilityState !== 'visible') return;
        polling = true;
        try {
            var options = {cache: 'no-store', mode: 'same-origin', credentials: 'same-origin'};
            if (typeof AbortSignal !== 'undefined' && AbortSignal.timeout) options.signal = AbortSignal.timeout(5000);
            var response = await fetch('/api/ui-refresh', options);
            if (!response.ok) return;
            var state = await response.json();
            if (!state || typeof state.epoch !== 'string' || !state.epoch || !Number.isSafeInteger(state.revision) || state.revision < 0) return;
            if (!baseline || state.epoch !== baseline.epoch) {
                // A new tab/server starts here, never by interpreting an old
                // process's counter as a new refresh request.
                baseline = state;
                pending = false;
                removeNotice();
                dismissed = false;
                return;
            }
            if (state.revision > baseline.revision) {
                baseline = state;
                pending = true;
                dismissed = false;
            }
            consider();
        } catch (_) {
            // Offline, unsupported, or malformed responses leave the page intact.
        } finally { polling = false; }
    }
    document.addEventListener('visibilitychange', poll);
    window.addEventListener('focus', poll);
    setInterval(poll, 2000);
    poll();
}());
