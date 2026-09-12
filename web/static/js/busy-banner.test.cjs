const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Minimal DOM: enough for busy-banner.js to create the banner, flip
// aria-busy on targets, and schedule its two timers. Timers are captured so
// tests fire them explicitly.
function page() {
    const listeners = {};
    const byId = {};
    function element(tag) {
        const attrs = {}, classes = new Set();
        const el = {
            tag, hidden: false, textContent: '', children: [], parentNode: null,
            get id() { return attrs.id || ''; },
            set id(v) { attrs.id = v; byId[v] = el; },
            set className(v) { classes.clear(); v.split(/\s+/).filter(Boolean).forEach(c => classes.add(c)); },
            classList: {add: c => classes.add(c), remove: c => classes.delete(c), contains: c => classes.has(c)},
            setAttribute(n, v) { attrs[n] = String(v); },
            getAttribute(n) { return n in attrs ? attrs[n] : null; },
            hasAttribute(n) { return n in attrs; },
            removeAttribute(n) { delete attrs[n]; },
            appendChild(c) { el.children.push(c); c.parentNode = el; },
        };
        return el;
    }
    const body = element('body');
    body.addEventListener = (n, f) => { (listeners[n] ||= []).push(f); };
    const document = {
        body,
        createElement: element,
        getElementById: id => byId[id] || null,
    };
    const timers = [];
    const context = {
        document, Set, Map,
        setTimeout(f, ms) { const t = {f, ms, cleared: false}; timers.push(t); return t; },
        clearTimeout(t) { if (t) t.cleared = true; },
    };
    vm.runInNewContext(fs.readFileSync(__dirname + '/busy-banner.js', 'utf8'), context);
    return {
        document, body, timers,
        event(n, detail) { (listeners[n] || []).forEach(f => f({detail})); },
        fire(ms) { timers.filter(t => t.ms === ms && !t.cleared && !t.fired).forEach(t => { t.fired = true; t.f(); }); },
        pendingTimers() { return timers.filter(t => !t.cleared && !t.fired).map(t => t.ms); },
        banner() { return byId['app-busy'] || null; },
        text() { return byId['app-busy-text'] ? byId['app-busy-text'].textContent : null; },
        el: element,
    };
}

function request(p, {elt, target, xhr} = {}) {
    elt = elt || p.el('form');
    target = target || p.el('div');
    xhr = xhr || {};
    return {detail: {elt, target, xhr}};
}

test('banner appears after the show delay, escalates to "still calculating", hides when the last request ends', () => {
    const p = page();
    const a = request(p), b = request(p);
    p.event('htmx:beforeRequest', a.detail);
    assert.equal(p.banner(), null, 'no banner before the show delay');
    assert.deepEqual(p.pendingTimers().sort(), [400, 8000]);
    p.fire(400);
    assert.equal(p.banner().hidden, false);
    assert.equal(p.text(), 'Calculating…');
    assert.equal(p.banner().getAttribute('role'), 'status');
    assert.equal(p.banner().getAttribute('aria-live'), 'polite');
    p.event('htmx:beforeRequest', b.detail);
    assert.equal(p.pendingTimers().length, 1, 'a second overlapping request must not schedule new timers');
    p.fire(8000);
    assert.equal(p.text(), 'Still calculating. This can take a minute.');
    p.event('htmx:afterRequest', a.detail);
    assert.equal(p.banner().hidden, false, 'one request still in flight keeps the banner up');
    p.event('htmx:afterRequest', b.detail);
    assert.equal(p.banner().hidden, true);
});

test('a request that finishes before the show delay never flashes the banner', () => {
    const p = page();
    const a = request(p);
    p.event('htmx:beforeRequest', a.detail);
    p.event('htmx:afterRequest', a.detail);
    assert.deepEqual(p.pendingTimers(), [], 'both timers cleared');
    p.fire(400); p.fire(8000);
    assert.equal(p.banner(), null);
});

test('afterRequest hides the banner regardless of outcome (error/abort/timeout paths all raise it)', () => {
    const p = page();
    const a = request(p);
    p.event('htmx:beforeRequest', a.detail);
    p.fire(400);
    assert.equal(p.banner().hidden, false);
    // htmx raises htmx:afterRequest with the same detail on every XHR
    // termination path; nothing in the detail says "success".
    p.event('htmx:afterRequest', a.detail);
    assert.equal(p.banner().hidden, true);
    // A later request reuses the existing banner element.
    const b = request(p);
    p.event('htmx:beforeRequest', b.detail);
    p.fire(400);
    assert.equal(p.banner().hidden, false);
    assert.equal(p.text(), 'Calculating…', 'text resets from "still calculating"');
});

test('the 2s what-if revision poll is ignored', () => {
    const p = page();
    const poll = p.el('div'); poll.id = 'whatif-poll';
    const results = p.el('div'); results.id = 'whatif-results';
    p.event('htmx:beforeRequest', {elt: poll, target: results, xhr: {}});
    assert.deepEqual(p.pendingTimers(), []);
    assert.equal(results.getAttribute('aria-busy'), null);
});

test('target gets aria-busy and the dim class while any request against it is pending', () => {
    const p = page();
    const results = p.el('div'); results.id = 'whatif-results';
    const a = request(p, {target: results}), b = request(p, {target: results});
    p.event('htmx:beforeRequest', a.detail);
    p.event('htmx:beforeRequest', b.detail);
    assert.equal(results.getAttribute('aria-busy'), 'true');
    assert.ok(results.classList.contains('app-busy-target'));
    p.event('htmx:afterRequest', a.detail);
    assert.equal(results.getAttribute('aria-busy'), 'true', 'second request still pending');
    p.event('htmx:afterRequest', b.detail);
    assert.equal(results.getAttribute('aria-busy'), null);
    assert.ok(!results.classList.contains('app-busy-target'));
});

test('data-busy-quiet requests count toward the banner but do not dim their target', () => {
    const p = page();
    const loader = p.el('div'); loader.setAttribute('data-busy-quiet', '');
    const results = p.el('div');
    const detail = {elt: loader, target: results, xhr: {}}; // htmx reuses one xhr across both events
    p.event('htmx:beforeRequest', detail);
    assert.equal(results.getAttribute('aria-busy'), 'true');
    assert.ok(!results.classList.contains('app-busy-target'));
    p.fire(400);
    assert.equal(p.banner().hidden, false);
    p.event('htmx:afterRequest', detail);
    assert.equal(p.banner().hidden, true);
});

test('an afterRequest with no matching beforeRequest is harmless', () => {
    const p = page();
    const a = request(p);
    p.event('htmx:afterRequest', a.detail);
    assert.equal(p.banner(), null);
    p.event('htmx:beforeRequest', a.detail);
    p.fire(400);
    assert.equal(p.banner().hidden, false, 'stray afterRequest must not poison later tracking');
});
