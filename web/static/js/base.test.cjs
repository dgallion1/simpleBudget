const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Minimal DOM: enough for base.js's retarget/error-display listeners and
// its reportValidityOfForms config line. Not a full DOM -- innerHTML only
// parses ONE top-level element (renderError's own shape: a single <div>
// wrapping everything), which is all these listeners ever assign.
function element(tag) {
    const attrs = {};
    const el = {
        tag,
        tagName: tag.toUpperCase(),
        nodeType: 1,
        children: [],
        parentNode: null,
        textContent: '',
        get firstChild() { return el.children[0] || null; },
        setAttribute(n, v) { attrs[n] = String(v); },
        getAttribute(n) { return n in attrs ? attrs[n] : null; },
        hasAttribute(n) { return n in attrs; },
        removeAttribute(n) { delete attrs[n]; },
        appendChild(c) { el.children.push(c); c.parentNode = el; return c; },
        insertBefore(c, ref) {
            const i = ref ? el.children.indexOf(ref) : -1;
            el.children.splice(i < 0 ? el.children.length : i, 0, c);
            c.parentNode = el;
            return c;
        },
        remove() {
            if (el.parentNode) {
                el.parentNode.children = el.parentNode.children.filter(c => c !== el);
                el.parentNode = null;
            }
        },
        set innerHTML(html) {
            el._html = html;
            el.children = [];
            const str = String(html);
            const m = /^<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>([\s\S]*)<\/\1>\s*$/.exec(str.trim());
            if (m) {
                const child = element(m[1]);
                const attrRe = /([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*"([^"]*)"/g;
                let am;
                while ((am = attrRe.exec(m[2]))) child.setAttribute(am[1], am[2]);
                child._html = m[3];
                child.textContent = m[3].replace(/<[^>]*>/g, '');
                el.appendChild(child);
            } else if (str.length) {
                // Mirrors a real DOM: `.innerHTML = "plain text"` creates a
                // Text node (nodeType 3, no setAttribute) as the child, not
                // an Element -- used to prove the non-HTML-body fallback (v).
                el.children.push({nodeType: 3, textContent: str, parentNode: el});
            }
        },
        get innerHTML() { return el._html || ''; },
    };
    return el;
}

function page() {
    const listeners = {};
    const body = element('body');
    body.addEventListener = (n, f) => { (listeners[n] ||= []).push(f); };
    const document = {
        body,
        createElement: element,
        getElementById: () => null,
        documentElement: element('html'),
    };
    const htmxStub = {config: {}};
    const window = {htmx: htmxStub, scrollY: 0, scrollTo() {}, addEventListener() {}, dispatchEvent() {}};
    const context = {
        document, window, htmx: htmxStub, CustomEvent: function () {}, Set, Map,
        localStorage: {setItem() {}},
    };
    vm.runInNewContext(fs.readFileSync(__dirname + '/base.js', 'utf8'), context);
    return {
        htmx: htmxStub,
        el: element,
        fire(name, detail) { (listeners[name] || []).forEach(f => f({detail})); },
    };
}

// (a) A rejected retargeted Add: isError must survive base.js's beforeSwap
// listener (mutation this kills: restoring `evt.detail.isError = false`),
// and that keeps htmx 2's own e.successful = !isError false, so the Add
// form's hx-on::after-request="if(event.detail.successful) this.reset()"
// must not fire -- the typed values survive.
test('a rejected retargeted 4xx still swaps but reports failure, so the Add form keeps what was typed; an accepted add still resets it', () => {
    const p = page();
    const xhrErr = {
        status: 400,
        getResponseHeader: (h) => (h === 'HX-Retarget' ? '#whatif-add-income-error' : null),
    };
    // isError: true mirrors htmx 2's own Bn()-computed default for a bare
    // 4xx, set BEFORE beforeSwap listeners run (see htmx.min.js).
    const detail = {xhr: xhrErr, target: p.el('div'), isError: true};
    p.fire('htmx:beforeSwap', detail);
    assert.equal(detail.shouldSwap, true, 'still swaps into its HX-Retarget slot');
    assert.notEqual(detail.isError, false, 'isError must NOT be forced false on a rejected retarget');

    let reset = false;
    // The Add form's own hx-on::after-request="if(event.detail.successful)
    // this.reset()" -- lives in the template, not base.js, so it is
    // simulated here rather than dispatched: base.js's job is only to make
    // sure `successful` carries the right value by the time it runs.
    function templateAfterRequest(successful) { if (successful) reset = true; }

    // Mirrors htmx 2's own e.successful = !isError (Vn() in the vendored
    // htmx.min.js) -- the real Add form's after-request handler reads
    // exactly this flag.
    templateAfterRequest(!detail.isError);
    assert.equal(reset, false, 'a rejected add must not wipe what the user typed');

    const xhrOk = {status: 200, getResponseHeader: () => null};
    const detailOk = {xhr: xhrOk, target: p.el('div'), isError: false};
    p.fire('htmx:beforeSwap', detailOk);
    templateAfterRequest(!detailOk.isError);
    assert.equal(reset, true, 'an accepted add still resets the form');
});

// (b) A NON-retargeted 4xx from a /whatif request (mutation this kills:
// dropping the new htmx:responseError listener, or making it a no-op)
// must show its message next to the triggering form, with role="alert",
// and a later success from that same form clears it.
test('a non-retargeted 4xx from a /whatif form shows its message next to that form with role="alert", and clears on the next success', () => {
    const p = page();
    const form = p.el('form');
    const responseText = '<div class="p-4 bg-negative-soft border border-negative rounded-lg" role="alert">'
        + '<p class="mt-2 text-body-sm text-negative">Through month cannot be before the start month</p></div>';
    const xhr = {status: 400, getResponseHeader: () => null, responseText};
    p.fire('htmx:responseError', {elt: form, xhr, pathInfo: {requestPath: '/whatif/income/abc123'}});

    assert.equal(form.children.length, 1, 'exactly one message lands next to the form');
    const node = form.children[0];
    assert.equal(node.getAttribute('role'), 'alert');
    assert.ok(node.textContent.includes('Through month cannot be before the start month'));

    p.fire('htmx:afterRequest', {elt: form, successful: true, pathInfo: {requestPath: '/whatif/income/abc123'}});
    assert.equal(form.children.length, 0, 'a later success from the same form clears the message');
});

test('a retargeted 4xx is left to its HX-Retarget slot -- not shown a second time by the inline handler', () => {
    const p = page();
    const form = p.el('form');
    const xhr = {
        status: 400,
        getResponseHeader: (h) => (h === 'HX-Retarget' ? '#whatif-add-income-error' : null),
        responseText: '<div role="alert">dup</div>',
    };
    p.fire('htmx:responseError', {elt: form, xhr, pathInfo: {requestPath: '/whatif/income'}});
    assert.equal(form.children.length, 0);
});

test('a non-retargeted error from an interactive control with no enclosing form lands on the triggering element\'s own parent', () => {
    const p = page();
    const wrapper = p.el('div');
    const button = p.el('button');
    button.setAttribute('hx-delete', '/whatif/income/abc'); // its own hx-verb -- a real user click
    wrapper.appendChild(button);
    const xhr = {status: 400, getResponseHeader: () => null, responseText: '<div role="alert">Nope</div>'};
    p.fire('htmx:responseError', {elt: button, xhr, pathInfo: {requestPath: '/whatif/income/abc/purge'}});
    assert.equal(wrapper.children.length, 2);
    assert.equal(wrapper.children[0].getAttribute('role'), 'alert');
});

// (ii) Lead ruling WS4.2 #2: only a form, something inside one, or an
// interactive control with its own hx-verb counts as user-initiated.
// hx-trigger="every ..." sentinels like the hidden #whatif-poll change
// detector are form-less, non-interactive (a <div>), and must never get an
// inline error -- inserting into its parent turned that parent's grid
// layout into an extra unstyled cell (checker-tests WS4.1 FAIL), and a
// fresh node every 2s would also re-announce identical text forever.
// Mutation this kills: errorHost() falling back to `elt.parentNode` for ANY
// form-less elt (the pre-fix behaviour).
test('an hx-trigger="every" sentinel (the poll) is never given an inline error, even repeatedly', () => {
    const p = page();
    const grid = p.el('div');
    grid.setAttribute('data-wf-layout-grid', '');
    const poll = p.el('div');
    poll.setAttribute('hx-trigger', 'every 2s');
    poll.setAttribute('hx-get', '/whatif/poll');
    grid.appendChild(poll);
    const xhr = {status: 500, getResponseHeader: () => null, responseText: 'Analysis failed: please retry'};
    p.fire('htmx:responseError', {elt: poll, xhr, pathInfo: {requestPath: '/whatif/poll'}});
    p.fire('htmx:responseError', {elt: poll, xhr, pathInfo: {requestPath: '/whatif/poll'}});
    assert.equal(grid.children.length, 1, 'the grid gains no inline-error child -- ever, not even once');
});

// (iii) A programmatic htmx.ajax() call with no source element defaults its
// `elt` to document.body (see the vendored htmx.min.js's he(): `if (r ==
// null) r = te().body`) -- e.g. whatif-spending-phases.js's trajectory
// refresh. That is not a click; nothing must be inserted into <body> (or,
// via the old parentNode fallback, <html> before <head> -- checker-tests
// WS4.1 FAIL). Mutation this kills: same as (ii).
test('a body-sourced htmx.ajax() call is never given an inline error', () => {
    const p = page();
    const html = p.el('html');
    const body = p.el('body');
    html.appendChild(body);
    const xhr = {status: 500, getResponseHeader: () => null, responseText: 'trajectory failed'};
    p.fire('htmx:responseError', {elt: body, xhr, pathInfo: {requestPath: '/whatif/spending-trajectory'}});
    assert.equal(html.children.length, 1, 'nothing inserted into <html>, before <head> or anywhere else');
    assert.equal(body.children.length, 0, 'nothing inserted into <body> either');
});

// WS4.3 (checker-tests WS4.2 FAIL): (ii)/(iii) above placed their form-less
// fixtures where BOTH errorHost() guards -- user-initiated AND
// forbidden-host -- agreed (the poll's parent already carried
// data-wf-layout-grid; the body's parent was <html>), so a mutant that
// drops either guard alone left both tests passing. These two isolate each
// guard: the fixture is left eligible/non-forbidden on every OTHER axis so
// only the guard under test can be doing the work.

// M1 target: `INTERACTIVE_TAGS[elt.tagName] && hasOwnHxVerb(elt)`. A
// non-interactive, load-triggered sentinel -- like the real
// #whatif-async-loader (hx-trigger="load") fetching /whatif/results-full --
// is not user-initiated, even though its parent here is an ORDINARY div (no
// data-wf-layout-grid, not html/body/main), so isForbiddenHost() alone
// would happily accept it: only the user-initiated guard can be stopping
// this insertion. Mutation this kills: M1 (errorHost() unconditionally
// falling back to `elt.parentNode` instead of requiring an interactive tag
// with its own hx-verb).
test('a load-triggered sentinel under an ordinary, non-forbidden parent still gets no inline error (isolates the user-initiated guard)', () => {
    const p = page();
    const results = p.el('div');
    results.setAttribute('id', 'whatif-results'); // ordinary: not forbidden
    const loader = p.el('div');
    loader.setAttribute('id', 'whatif-async-loader');
    loader.setAttribute('hx-get', '/whatif/results-full');
    loader.setAttribute('hx-trigger', 'load');
    results.appendChild(loader);
    const xhr = {status: 500, getResponseHeader: () => null, responseText: 'Analysis failed'};
    p.fire('htmx:responseError', {elt: loader, xhr, pathInfo: {requestPath: '/whatif/results-full'}});
    assert.equal(results.children.length, 1, 'no inline error inserted into the ordinary, non-forbidden #whatif-results');
});

// M2 target: isForbiddenHost(). A real interactive control with its own
// hx-verb and no enclosing form satisfies the OTHER guard (it IS
// user-initiated), so only the forbidden-host check can be stopping
// insertion into a data-wf-layout-grid / <main> / <body> parent. Mutation
// this kills: M2 (isForbiddenHost() always returning false).
test('an eligible form-less control whose only host is forbidden gets no inline error (isolates the forbidden-host guard)', () => {
    const p = page();
    const xhr = {status: 400, getResponseHeader: () => null, responseText: '<div role="alert">Nope</div>'};
    const forbiddenParents = [
        () => { const d = p.el('div'); d.setAttribute('data-wf-layout-grid', ''); return d; },
        () => p.el('main'),
        () => p.el('body'),
    ];
    forbiddenParents.forEach((make) => {
        const host = make();
        const button = p.el('button');
        button.setAttribute('hx-post', '/whatif/montecarlo'); // its own hx-verb, no form
        host.appendChild(button);
        p.fire('htmx:responseError', {elt: button, xhr, pathInfo: {requestPath: '/whatif/montecarlo'}});
        assert.equal(host.children.length, 1, `no inline error inserted into forbidden host <${host.tagName}>`);
    });
});

// (iv) The same message must not be re-inserted (and so re-announced) while
// it is already displayed on the same host. Mutation this kills: dropping
// the `existing.__wfText === text` short-circuit (always clear + reinsert).
test('the exact same message on the same host is not re-inserted while already displayed; a genuinely different one does replace it', () => {
    const p = page();
    const form = p.el('form');
    const xhrSame = {status: 400, getResponseHeader: () => null, responseText: '<div role="alert">Same message</div>'};
    p.fire('htmx:responseError', {elt: form, xhr: xhrSame, pathInfo: {requestPath: '/whatif/income/x'}});
    assert.equal(form.children.length, 1);
    const first = form.children[0];
    p.fire('htmx:responseError', {elt: form, xhr: xhrSame, pathInfo: {requestPath: '/whatif/income/x'}});
    assert.equal(form.children.length, 1, 'not re-inserted');
    assert.equal(form.children[0], first, 'the same node instance stays -- no destroy+recreate to re-announce');

    const xhrDiff = {status: 400, getResponseHeader: () => null, responseText: '<div role="alert">Different message</div>'};
    p.fire('htmx:responseError', {elt: form, xhr: xhrDiff, pathInfo: {requestPath: '/whatif/income/x'}});
    assert.equal(form.children.length, 1);
    assert.notEqual(form.children[0], first, 'a genuinely different message DOES replace the old node');
    assert.ok(form.children[0].textContent.includes('Different message'));
});

// (v) checker-tests O2: a non-HTML error body (e.g. plain-text http.Error
// from chi's panic Recoverer -- renderError's own output never triggers
// this) must not throw trying to setAttribute() on a Text node, and must
// still show the message, safely, via textContent. Mutation this kills:
// removing the `node.nodeType === 1 && typeof node.setAttribute ===
// 'function'` guard (calling setAttribute unconditionally on wrap.firstChild).
test('a non-HTML (plain-text) error body is shown safely via a fallback role="alert" node instead of throwing', () => {
    const p = page();
    const form = p.el('form');
    const xhr = {status: 500, getResponseHeader: () => null, responseText: 'Internal Server Error'};
    assert.doesNotThrow(() => {
        p.fire('htmx:responseError', {elt: form, xhr, pathInfo: {requestPath: '/whatif/settings'}});
    });
    assert.equal(form.children.length, 1);
    const node = form.children[0];
    assert.equal(node.getAttribute('role'), 'alert');
    assert.ok(node.textContent.includes('Internal Server Error'));
});

test('errors outside /whatif are left alone', () => {
    const p = page();
    const form = p.el('form');
    const xhr = {status: 400, getResponseHeader: () => null, responseText: '<div role="alert">x</div>'};
    p.fire('htmx:responseError', {elt: form, xhr, pathInfo: {requestPath: '/accounts/import'}});
    assert.equal(form.children.length, 0);
});

// (c) A browser-invalid value must surface the browser's own validity
// message. htmx 2 only calls elt.reportValidity() for the first invalid
// field when htmx.config.reportValidityOfForms is true (see the vendored
// htmx.min.js's validation pass, an()) -- it defaults to false. Mutation
// this kills: dropping the `htmx.config.reportValidityOfForms = true;`
// line in base.js.
test('enables htmx\'s native reportValidity() on browser-invalid values', () => {
    const p = page();
    assert.equal(p.htmx.config.reportValidityOfForms, true);
});
