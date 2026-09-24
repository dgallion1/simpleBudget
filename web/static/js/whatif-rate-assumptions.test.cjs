// WS1 attempt-5 (C1, R5, R6, P5): the investment-return "Using asset
// allocation (~X% expected)" display must show the SERVER's own figure
// (data-expected-return, rendered with the same printf "%.1f" the served
// text used) and must NEVER be recomputed client-side, at load or via the
// Quick Adjust load/after-swap sync -- calculateExpectedReturnFromAllocation
// treats a genuine 0% allocation field as "not set" (`|| 10` / `|| 60`
// fallbacks) and can silently disagree with the server on the live plan's
// real allocations (WS1.4 finding O1 / this attempt's C1 regression).
//
// This is the ONLY test file that loads whatif-rate-assumptions.js AND
// whatif-quick-adjust.js together in one vm context, matching how they
// coexist on a real page (two separate <script defer> tags) -- WS1.4's
// checker-tests found that loading whatif-quick-adjust.js alone leaves
// updateInvestmentReturnDisplay undefined, so the `typeof === 'function'`
// guards in whatif-quick-adjust.js silently skip calling it, and neither
// R5 (a DOMContentLoaded recompute added to whatif-rate-assumptions.js)
// nor R6 (syncAllQuickAdjustControls re-calling it) could ever be observed.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const QUICK_ADJUST_PATH = __dirname + '/whatif-quick-adjust.js';
const RATE_ASSUMPTIONS_PATH = __dirname + '/whatif-rate-assumptions.js';

// ---- minimal fake DOM: extends the whatif-quick-adjust.test.cjs pattern
// with document.createElement/createTextNode, appendChild + computed
// textContent (updateInvestmentReturnDisplay builds its text as child
// nodes, then reads display.textContent back to derive the aria string),
// document.querySelector (singular -- calculateExpectedReturnFromAllocation
// uses it), and a REAL addEventListener/dispatch on `document` itself (to
// fire DOMContentLoaded for the R5 test). ----
function makeTextNode(text) {
    return {nodeType: 3, textContent: text};
}

// A non-null sentinel object returned by a panel-marked element's
// closest('#quick-adjust-panel') -- only its truthiness matters.
const QUICK_ADJUST_PANEL_SENTINEL = {id: 'quick-adjust-panel'};

function makeElement(spec) {
    const attrs = Object.assign({}, spec.attrs || {});
    let children = [];
    let rawText = spec.textContent || '';
    const listeners = {};
    const el = {
        tagName: (spec.tag || 'div').toUpperCase(),
        nodeType: 1,
        value: spec.value !== undefined ? spec.value : '',
        min: spec.min !== undefined ? spec.min : '',
        max: spec.max !== undefined ? spec.max : '',
        step: spec.step !== undefined ? spec.step : '',
        className: spec.className || '',
        // _writeCount: how many times updateInvestmentReturnDisplay's
        // `display.textContent = ''` clear-and-rebuild ran against THIS
        // node. Content-equality alone cannot distinguish "never called"
        // from "called again but produced an identical string" once C1's
        // fix makes the function idempotent (it always reads the same
        // server attribute) -- R5/R6 are about whether the call happened
        // AT ALL, so this is the structural signal those tests need.
        _writeCount: 0,
        hasAttribute(n) { return n in attrs; },
        getAttribute(n) { return n in attrs ? attrs[n] : null; },
        setAttribute(n, v) { attrs[n] = String(v); },
        removeAttribute(n) { delete attrs[n]; },
        // closest('#quick-adjust-panel') resolves truthy ONLY for a node
        // built with {inPanel: true} -- lets a fixture put the Quick
        // Adjust display/range genuinely "inside" the panel (inPanel=true
        // in updateInvestmentReturnDisplay) while the in-card copies stay
        // outside it, the same distinction a real nested DOM makes. WS1.5
        // checker-tests: a fake DOM whose closest() was unconditionally
        // null meant `inPanel` was NEVER true, so the Quick Adjust branch
        // of updateInvestmentReturnDisplay never ran and a QA-only client
        // recompute (C1k) was invisible to every test.
        closest(sel) { return spec.inPanel && sel === '#quick-adjust-panel' ? QUICK_ADJUST_PANEL_SENTINEL : null; },
        appendChild(node) { children.push(node); },
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
    };
    Object.defineProperty(el, 'id', {get() { return attrs.id || ''; }});
    Object.defineProperty(el, 'type', {get() { return attrs.type || ''; }});
    Object.defineProperty(el, 'textContent', {
        get() { return children.length ? children.map((c) => c.textContent).join('') : rawText; },
        set(v) { rawText = v; children = []; el._writeCount++; },
    });
    Object.defineProperty(el, 'dataset', {get() {
        const ds = {};
        for (const k of Object.keys(attrs)) {
            if (k.indexOf('data-') === 0) {
                const camel = k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
                ds[camel] = attrs[k];
            }
        }
        return ds;
    }});
    return el;
}

function parseSelector(sel) {
    const tagMatch = sel.match(/^[a-zA-Z]+/);
    const tag = tagMatch ? tagMatch[0].toUpperCase() : null;
    const rest = tag ? sel.slice(tagMatch[0].length) : sel;
    const clauses = [...rest.matchAll(/\[([a-zA-Z0-9-]+)(?:="([^"]*)")?\]/g)];
    return {tag, clauses};
}

function elementMatches(el, sel) {
    const {tag, clauses} = parseSelector(sel);
    if (tag && el.tagName !== tag) return false;
    for (const clause of clauses) {
        const attr = clause[1], val = clause[2];
        if (!el.hasAttribute(attr)) return false;
        if (val !== undefined && el.getAttribute(attr) !== val) return false;
    }
    return true;
}

function makeFakeDom(specs) {
    const store = specs.map(makeElement);
    const listeners = {};
    return {
        _store: store,
        querySelectorAll(sel) { return store.filter((el) => elementMatches(el, sel)); },
        querySelector(sel) { return store.find((el) => elementMatches(el, sel)) || null; },
        getElementById(id) { return store.find((el) => el.id === id) || null; },
        createElement(tag) { return makeElement({tag}); },
        createTextNode(text) { return makeTextNode(text); },
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        _fire(type) { (listeners[type] || []).forEach((fn) => fn({})); },
        body: {addEventListener() {}},
    };
}

// loadCombined loads whatif-quick-adjust.js THEN whatif-rate-assumptions.js
// into ONE vm context against `dom`, matching real script-tag order on
// /whatif -- both files' top-level DOMContentLoaded registrations run
// against the SAME fake document, and functions defined in either file are
// visible to the other (as they are as sibling globals on a real page).
function loadCombined(dom, patchQuickAdjust, patchRateAssumptions) {
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    let qa = fs.readFileSync(QUICK_ADJUST_PATH, 'utf8');
    if (patchQuickAdjust) qa = patchQuickAdjust(qa);
    vm.runInContext(qa, context);
    let ra = fs.readFileSync(RATE_ASSUMPTIONS_PATH, 'utf8');
    if (patchRateAssumptions) ra = patchRateAssumptions(ra);
    vm.runInContext(ra, context);
    return context;
}

// A served investment-return card at investment_return=0: server rendered
// "Using asset allocation (~6.2% expected)" from
// .Settings.GetExpectedReturnFromAllocation, baked into BOTH the visible
// text and data-expected-return (WS1 C1), and into the range's
// aria-valuetext. The canonical hidden field and a Quick Adjust mirror
// range are also present, matching a real page.
const SERVED_TEXT = 'Using asset allocation (~6.2% expected)';
function servedInvestmentReturnSpecs() {
    return [
        {tag: 'input', attrs: {id: 'investment-return-exact', type: 'hidden', 'data-quick-adjust-key': 'investment_return'}, value: '0'},
        {
            tag: 'input',
            attrs: {
                id: 'investment-return-slider', type: 'range', 'data-quick-adjust-key': 'investment_return',
                'data-quick-adjust-mirror': 'true', 'aria-valuetext': SERVED_TEXT,
            },
            value: '0',
        },
        {
            tag: 'div',
            attrs: {
                id: 'investment-return-display', 'data-quick-adjust-display': 'investment_return',
                'data-quick-adjust-display-format': 'investment-return', 'data-expected-return': '6.2',
            },
            textContent: SERVED_TEXT,
        },
        {
            tag: 'input',
            attrs: {
                id: 'qa-investment-return', type: 'range', 'data-quick-adjust-key': 'investment_return',
                'data-quick-adjust-mirror': 'true', 'aria-valuetext': SERVED_TEXT,
            },
            value: '0',
        },
        {
            tag: 'div',
            attrs: {'data-quick-adjust-display': 'investment_return', 'data-quick-adjust-display-format': 'investment-return', 'data-expected-return': '6.2'},
            textContent: SERVED_TEXT,
        },
    ];
}

// servedInvestmentReturnSpecsInPanel is servedInvestmentReturnSpecs() with
// the Quick Adjust range (index 3) and display (index 4) marked
// {inPanel: true} -- i.e. genuinely nested under #quick-adjust-panel, the
// same distinction updateInvestmentReturnDisplay's own
// `display.closest('#quick-adjust-panel') !== null` makes on a real page.
// The in-card range/display (indices 1-2) stay outside it. WS1.6: without
// this, `inPanel` is never true for ANY display, so the Quick Adjust
// branch of updateInvestmentReturnDisplay never executes and a
// Quick-Adjust-only client recompute (C1k) is invisible.
function servedInvestmentReturnSpecsInPanel() {
    const specs = servedInvestmentReturnSpecs();
    specs[3] = Object.assign({}, specs[3], {inPanel: true});
    specs[4] = Object.assign({}, specs[4], {inPanel: true});
    return specs;
}

// Same, but with a distinct sentinel data-expected-return per display
// (in-card "6.2", Quick Adjust "9.9") -- distinguishes "which display's
// text is on screen" unambiguously, unlike a shared sentinel which cannot
// tell a QA-only mutant from an in-card-only one when both start correct.
function sentinelServedSpecsInPanel() {
    const specs = servedInvestmentReturnSpecsInPanel();
    specs[2] = Object.assign({}, specs[2], {attrs: Object.assign({}, specs[2].attrs, {'data-expected-return': '6.2'})});
    specs[2].textContent = 'Using asset allocation (~6.2% expected)';
    specs[4] = Object.assign({}, specs[4], {attrs: Object.assign({}, specs[4].attrs, {'data-expected-return': '9.9'})});
    specs[4].textContent = 'Using asset allocation (~9.9% expected)';
    return specs;
}

// sentinelSpecs gives the served card a data-expected-return of "9.9" --
// distinct from anything calculateExpectedReturnFromAllocation could
// plausibly compute from its 3-7% asset-class means against a DOM with NO
// allocation inputs at all (every default-fallback blend lands well under
// 9.9). This is what makes "did a client recompute run" observable even
// though the FIXED code (reading the attribute) is a no-op re-render when
// called twice against unchanged markup -- self-checks for R5/R6 need a
// recompute to actually disagree with the served figure, not merely
// re-render it.
function sentinelServedSpecs() {
    const specs = servedInvestmentReturnSpecs();
    for (const s of specs) {
        if (s.attrs && 'data-expected-return' in s.attrs) s.attrs['data-expected-return'] = '9.9';
    }
    return specs;
}

test('exposes updateInvestmentReturnDisplay and calculateExpectedReturnFromAllocation when both files load together', () => {
    const ctx = loadCombined(makeFakeDom(servedInvestmentReturnSpecs()));
    assert.equal(typeof ctx.updateInvestmentReturnDisplay, 'function');
    assert.equal(typeof ctx.calculateExpectedReturnFromAllocation, 'function');
    assert.equal(typeof ctx.syncAllQuickAdjustControls, 'function');
});

// R5: nothing in whatif-rate-assumptions.js may recompute this display at
// DOMContentLoaded. Checked TWO ways: (a) the served text/aria stay
// byte-identical, and (b) _writeCount stays 0 -- (b) is the one that
// actually distinguishes "never called" from "called again but produced
// an identical string" (C1's fix makes updateInvestmentReturnDisplay
// idempotent against unchanged markup, so content equality alone is not
// enough once that fix is in place; see the self-check below).
test('R5: DOMContentLoaded never touches the investment-return display/aria (server text stays exactly as served)', () => {
    const dom = makeFakeDom(servedInvestmentReturnSpecs());
    loadCombined(dom);
    dom._fire('DOMContentLoaded');
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    const ranges = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-mirror'));
    for (const d of displays) {
        assert.equal(d.textContent, SERVED_TEXT);
        assert.equal(d._writeCount, 0, 'display must never be rebuilt at load');
    }
    for (const r of ranges) assert.equal(r.getAttribute('aria-valuetext'), SERVED_TEXT);
});

test('self-check: R5 is real -- a DOMContentLoaded recompute added to whatif-rate-assumptions.js is caught by the test above', () => {
    const dom = makeFakeDom(servedInvestmentReturnSpecs());
    const patch = (src) => src + "\ndocument.addEventListener('DOMContentLoaded', function() { updateInvestmentReturnDisplay(0); });\n";
    loadCombined(dom, undefined, patch);
    dom._fire('DOMContentLoaded');
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    for (const d of displays) {
        assert.ok(d._writeCount > 0, 'expected the DOMContentLoaded mutant to rebuild the display at least once');
    }
});

// R6: the Quick Adjust load/after-swap sync (syncAllQuickAdjustControls,
// mirror-only) must not call updateInvestmentReturnDisplay either -- this
// exercises the SAME code path as the WS1.4 regression, now with
// updateInvestmentReturnDisplay actually defined (both files loaded
// together), so a reintroduced call is observable.
test('R6: syncAllQuickAdjustControls (load/after-swap sync) never touches the investment-return display/aria', () => {
    const dom = makeFakeDom(servedInvestmentReturnSpecs());
    const ctx = loadCombined(dom);
    ctx.syncAllQuickAdjustControls();
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    const ranges = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-mirror'));
    for (const d of displays) {
        assert.equal(d.textContent, SERVED_TEXT);
        assert.equal(d._writeCount, 0, 'display must never be rebuilt by the load/after-swap sync');
    }
    for (const r of ranges) assert.equal(r.getAttribute('aria-valuetext'), SERVED_TEXT);
});

test('self-check: R6 is real -- syncQuickAdjustMirrorOnly re-calling updateInvestmentReturnDisplay is caught by the test above', () => {
    const dom = makeFakeDom(servedInvestmentReturnSpecs());
    const target = "function syncQuickAdjustMirrorOnly(key) {\n    const canonicalControl = getQuickAdjustCanonicalControl(key);\n    if (!canonicalControl) return null;\n    syncQuickAdjustMirrorControls(key, canonicalControl);";
    const patch = (src) => {
        const patched = src.replace(target, () => target + "\n    if (key === 'investment_return' && typeof updateInvestmentReturnDisplay === 'function') { updateInvestmentReturnDisplay(canonicalControl.value); }");
        assert.notEqual(patched, src, 'sanity: the patch target must exist in whatif-quick-adjust.js');
        return patched;
    };
    const ctx = loadCombined(dom, patch);
    ctx.syncAllQuickAdjustControls();
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    for (const d of displays) {
        assert.ok(d._writeCount > 0, 'expected the R6 mutant to rebuild the display at least once');
    }
});

// C1 / P5: a genuine user drag to 0 must show the SERVER's own figure
// (data-expected-return), never a client recompute -- even one that uses
// the correct half-even rounding (P5 is specifically "toFixed on the
// expected-return figure", but the deeper point C1 makes is that NO
// client computation belongs here at all, correct rounding or not).
test('C1/P5: dragging investment return to 0 shows the server data-expected-return figure verbatim, not a client recompute', () => {
    const dom = makeFakeDom(sentinelServedSpecs());
    const ctx = loadCombined(dom);
    ctx.updateInvestmentReturnDisplay('0');
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    for (const d of displays) {
        assert.ok(d.textContent.includes('9.9'), `expected the server's data-expected-return (9.9) verbatim, got: ${d.textContent}`);
    }
});

test('self-check: P5 is real -- a toFixed(1) client recompute of expected return is caught by the test above', () => {
    const dom = makeFakeDom(sentinelServedSpecs());
    const target = 'const expectedReturnText = display.dataset.expectedReturn;';
    const patch = (src) => {
        const patched = src.replace(target, () => 'const expectedReturnText = calculateExpectedReturnFromAllocation().toFixed(1);');
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadCombined(dom, undefined, patch);
    ctx.updateInvestmentReturnDisplay('0');
    const displays = dom._store.filter((el) => el.getAttribute('data-quick-adjust-display') === 'investment_return');
    const stillSentinel = displays.every((d) => d.textContent.includes('9.9'));
    assert.ok(!stillSentinel, 'expected the toFixed(1) recompute mutant to disagree with the server sentinel (9.9)');
});

// WS1.6 / C1k: the Quick Adjust display genuinely resolves INSIDE
// #quick-adjust-panel (closest() truthy for it, null for the in-card
// copy) -- the one distinction WS1.5's fake DOM could not make, which let
// a client recompute localized to the `if (inPanel)` branch alone survive
// every existing test. Each display carries its OWN sentinel
// data-expected-return (in-card "6.2", Quick Adjust "9.9") so a mutant
// touching only one of them is unambiguously observable on that one.
test('C1k: dragging investment return to 0 shows EACH display\'s own server data-expected-return -- in-card AND Quick Adjust (which genuinely resolves inside #quick-adjust-panel)', () => {
    const dom = makeFakeDom(sentinelServedSpecsInPanel());
    const ctx = loadCombined(dom);
    ctx.updateInvestmentReturnDisplay('0');
    const inCard = dom._store[2], qa = dom._store[4];
    assert.ok(inCard.textContent.includes('6.2'), `in-card: expected server figure 6.2, got: ${inCard.textContent}`);
    assert.ok(qa.textContent.includes('9.9'), `Quick Adjust: expected server figure 9.9, got: ${qa.textContent}`);
});

test('self-check: C1k is real -- a Quick-Adjust-only client recompute is caught by the test above', () => {
    const dom = makeFakeDom(sentinelServedSpecsInPanel());
    const target = "detail.textContent = '(~' + expectedReturnText + '% expected)';";
    const patch = (src) => {
        // The exact mutation WS1.5 checker-tests named: a recompute
        // spliced in ONLY for the panel (Quick Adjust) branch, after the
        // correct (server-attribute) text was already set.
        const patched = src.replace(
            target,
            () => target + "\n            if (inPanel) detail.textContent = '(~' + roundHalfEvenDecimalString(calculateExpectedReturnFromAllocation(), 1) + '% expected)';"
        );
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadCombined(dom, undefined, patch);
    ctx.updateInvestmentReturnDisplay('0');
    const inCard = dom._store[2], qa = dom._store[4];
    // The in-card display is untouched by this mutant.
    assert.ok(inCard.textContent.includes('6.2'), `in-card should still show 6.2, got: ${inCard.textContent}`);
    // The Quick Adjust display is the one this mutant breaks.
    assert.ok(!qa.textContent.includes('9.9'), `expected the Quick-Adjust-only recompute mutant to disagree with the QA server sentinel (9.9); got: ${qa.textContent}`);
});

test('self-check: an in-card-only client recompute is ALSO caught by the C1k test above', () => {
    const dom = makeFakeDom(sentinelServedSpecsInPanel());
    const target = "detail.textContent = '(~' + expectedReturnText + '% expected)';";
    const patch = (src) => {
        const patched = src.replace(
            target,
            () => target + "\n            if (!inPanel) detail.textContent = '(~' + roundHalfEvenDecimalString(calculateExpectedReturnFromAllocation(), 1) + '% expected)';"
        );
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadCombined(dom, undefined, patch);
    ctx.updateInvestmentReturnDisplay('0');
    const inCard = dom._store[2], qa = dom._store[4];
    assert.ok(!inCard.textContent.includes('6.2'), `expected the in-card-only recompute mutant to disagree with the in-card server sentinel (6.2); got: ${inCard.textContent}`);
    // The Quick Adjust display is untouched by this mutant.
    assert.ok(qa.textContent.includes('9.9'), `Quick Adjust should still show 9.9, got: ${qa.textContent}`);
});
