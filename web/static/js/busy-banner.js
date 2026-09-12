// Site-wide "the app is working" signal for HTMX requests.
//
// Most of the what-if page recalculates on `change delay:500ms` with no
// hx-indicator, so a slider drag or field edit looked like nothing happened
// until the results column repainted -- and the expensive full analysis then
// arrived from a hx-trigger="load" fetch nobody could see. Rather than add a spinner
// to every one of the ~80 request elements, this hooks htmx's request
// lifecycle once:
//
//  1. A fixed banner (#app-busy, role="status") appears once any request has
//     been in flight for SHOW_AFTER_MS, and switches to "Still calculating.
//     This can take a minute." after STILL_AFTER_MS. It hides when the LAST
//     in-flight request finishes, on success or on any failure path --
//     htmx:afterRequest fires on load, error, abort and timeout alike.
//  2. Each request's hx-target gets aria-busy="true" while a request against
//     it is pending, plus the .app-busy-target dim/progress-cursor class
//     unless the triggering element carries data-busy-quiet (the async
//     results-full loader: dimming the whole results column for a minute
//     while the user reads the fast results would defeat fast-first).
//
// The what-if revision poll (#whatif-poll, every 2s, usually a 204) is
// ignored entirely; counting it would flash the banner every two seconds.
(function () {
    'use strict';
    var SHOW_AFTER_MS = 400;
    var STILL_AFTER_MS = 8000;
    var TEXT_BUSY = 'Calculating…';
    var TEXT_STILL = 'Still calculating. This can take a minute.';

    var inflight = new Set();       // xhr (or elt) per pending request
    var targets = new Map();        // target element -> pending count
    var showTimer = null, stillTimer = null;

    function tracked(evt) {
        var d = evt.detail;
        if (!d || !d.elt) return false;
        return d.elt.id !== 'whatif-poll';
    }
    function key(evt) {
        return evt.detail.xhr || evt.detail.elt;
    }

    function banner() {
        var el = document.getElementById('app-busy');
        if (!el) {
            el = document.createElement('div');
            el.id = 'app-busy';
            el.className = 'app-busy';
            el.setAttribute('role', 'status');
            el.setAttribute('aria-live', 'polite');
            el.hidden = true;
            var spin = document.createElement('span');
            spin.className = 'app-busy-spinner';
            spin.setAttribute('aria-hidden', 'true');
            var text = document.createElement('span');
            text.id = 'app-busy-text';
            el.appendChild(spin);
            el.appendChild(text);
            document.body.appendChild(el);
        }
        return el;
    }
    function setText(s) {
        var t = document.getElementById('app-busy-text');
        if (t) t.textContent = s;
    }
    function show(text) {
        var el = banner();
        setText(text);
        el.hidden = false;
    }
    function hide() {
        if (showTimer) { clearTimeout(showTimer); showTimer = null; }
        if (stillTimer) { clearTimeout(stillTimer); stillTimer = null; }
        var el = document.getElementById('app-busy');
        if (el) el.hidden = true;
    }

    function markTarget(evt) {
        var target = evt.detail.target;
        if (!target || !target.setAttribute) return;
        targets.set(target, (targets.get(target) || 0) + 1);
        target.setAttribute('aria-busy', 'true');
        var elt = evt.detail.elt;
        var quiet = elt.hasAttribute && elt.hasAttribute('data-busy-quiet');
        if (!quiet && target.classList) target.classList.add('app-busy-target');
    }
    function unmarkTarget(evt) {
        var target = evt.detail.target;
        if (!target || !targets.has(target)) return;
        var n = targets.get(target) - 1;
        if (n > 0) { targets.set(target, n); return; }
        targets.delete(target);
        if (target.removeAttribute) target.removeAttribute('aria-busy');
        if (target.classList) target.classList.remove('app-busy-target');
    }

    document.body.addEventListener('htmx:beforeRequest', function (evt) {
        if (!tracked(evt)) return;
        inflight.add(key(evt));
        markTarget(evt);
        if (inflight.size === 1 && !showTimer) {
            showTimer = setTimeout(function () {
                showTimer = null;
                show(TEXT_BUSY);
            }, SHOW_AFTER_MS);
            stillTimer = setTimeout(function () {
                stillTimer = null;
                show(TEXT_STILL);
            }, STILL_AFTER_MS);
        }
    });

    // Unconditional on outcome: a banner stuck visible after a failed
    // request would be worse than the silence it replaces.
    document.body.addEventListener('htmx:afterRequest', function (evt) {
        if (!tracked(evt)) return;
        inflight.delete(key(evt));
        unmarkTarget(evt);
        if (inflight.size === 0) hide();
    });
})();
