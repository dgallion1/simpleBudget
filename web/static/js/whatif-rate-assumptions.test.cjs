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

// ── WS5: D6 (glide path reveal-only tick) / D7 (person removal saves,
//    refusal restores + refocuses) -- a richer fake DOM than the
//    investment-return fixtures above need: real parent/child structure,
//    classList, closest()/querySelector(All) over descendants, and
//    addEventListener/dispatchEvent so removePersonRow's own
//    htmx:afterRequest listener can be exercised. Reuses parseSelector/
//    elementMatches from above (same attribute-selector syntax) rather
//    than duplicating a matcher. ──────────────────────────────────────────

function rpEl(tag, attrs) {
    attrs = Object.assign({}, attrs || {});
    const listeners = {};
    const el = {
        tagName: tag.toUpperCase(),
        nodeType: 1,
        parentNode: null,
        children: [],
        checked: !!attrs.checked,
        value: attrs.value !== undefined ? attrs.value : '',
        innerHTML: '',
        form: null,
        hasAttribute(n) { return n in attrs; },
        getAttribute(n) { return n in attrs ? attrs[n] : null; },
        setAttribute(n, v) { attrs[n] = String(v); },
        removeAttribute(n) { delete attrs[n]; },
        classList: {
            add() { for (const n of arguments) el._classes().add(n); },
            remove() { for (const n of arguments) el._classes().delete(n); },
            contains(n) { return el._classes().has(n); },
        },
        _classes() {
            if (!el.__classSet) {
                el.__classSet = new Set(String(attrs.class || '').split(/\s+/).filter(Boolean));
            }
            return el.__classSet;
        },
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        removeEventListener(type, fn) {
            if (!listeners[type]) return;
            listeners[type] = listeners[type].filter((f) => f !== fn);
        },
        dispatchEvent(evt) {
            evt.target = el;
            (listeners[evt.type] || []).slice().forEach((fn) => fn(evt));
            return true;
        },
        appendChild(child) {
            child.parentNode = el;
            el.children.push(child);
            return child;
        },
        insertBefore(child, ref) {
            child.parentNode = el;
            const i = ref ? el.children.indexOf(ref) : -1;
            el.children.splice(i < 0 ? el.children.length : i, 0, child);
            return child;
        },
        remove() {
            if (el.parentNode) {
                const i = el.parentNode.children.indexOf(el);
                if (i >= 0) el.parentNode.children.splice(i, 1);
                el.parentNode = null;
            }
        },
        get nextSibling() {
            if (!el.parentNode) return null;
            const i = el.parentNode.children.indexOf(el);
            return i >= 0 ? (el.parentNode.children[i + 1] || null) : null;
        },
        closest(sel) {
            let n = el;
            while (n) {
                if (elementMatches(n, sel)) return n;
                n = n.parentNode;
            }
            return null;
        },
        querySelector(sel) { return rpQueryAll(el, sel)[0] || null; },
        querySelectorAll(sel) { return rpQueryAll(el, sel); },
        focus() { el.__focused = true; },
    };
    return el;
}

function rpQueryAll(root, sel) {
    const out = [];
    (function walk(node) {
        for (const c of node.children) {
            if (elementMatches(c, sel)) out.push(c);
            walk(c);
        }
    })(root);
    return out;
}

function rpDocument(rootChildren) {
    const root = rpEl('div');
    rootChildren.forEach((c) => root.appendChild(c));
    return {
        _root: root,
        getElementById(id) { return findById(root, id); },
        querySelector(sel) { return rpQueryAll(root, sel)[0] || null; },
        querySelectorAll(sel) { return rpQueryAll(root, sel); },
        createElement(tag) { return rpEl(tag); },
        addEventListener() {},
        body: {addEventListener() {}},
    };
}

function findById(node, id) {
    for (const c of node.children) {
        if (c.getAttribute('id') === id) return c;
        const found = findById(c, id);
        if (found) return found;
    }
    return null;
}

// A no-op Event constructor -- production code does `new Event('change',
// {bubbles: true})`; only `.type` is read by anything these tests exercise.
function FakeEvent(type, opts) {
    this.type = type;
    this.bubbles = !!(opts && opts.bubbles);
}

function loadRateAssumptions(doc, patch) {
    const context = {document: doc, window: {}, console, Event: FakeEvent};
    vm.createContext(context);
    let src = fs.readFileSync(RATE_ASSUMPTIONS_PATH, 'utf8');
    if (patch) src = patch(src);
    vm.runInContext(src, context);
    return context;
}

// Builds the #glide-path-fields fixture: a checkbox plus a fields <div>
// containing the three number inputs, matching the server-rendered HIDDEN
// state (disabled, not required) toggleGlidePathFields starts from.
function glideFixture() {
    const fields = rpEl('div', {id: 'glide-path-fields', class: 'space-y-2 hidden'});
    const startInput = rpEl('input', {type: 'number', name: 'start_stock_pct', disabled: true});
    const endInput = rpEl('input', {type: 'number', name: 'end_stock_pct', disabled: true});
    const yearsInput = rpEl('input', {type: 'number', name: 'transition_years', disabled: true});
    [startInput, endInput, yearsInput].forEach((el) => { el.disabled = true; el.required = false; });
    fields.appendChild(startInput);
    fields.appendChild(endInput);
    fields.appendChild(yearsInput);

    const form = rpEl('form');
    let submitCount = 0;
    form.requestSubmit = () => { submitCount++; };
    const checkbox = rpEl('input', {type: 'checkbox', name: 'enabled'});
    checkbox.form = form;
    form.appendChild(checkbox);
    form.appendChild(fields);

    return {fields, form, checkbox, startInput, endInput, yearsInput, getSubmitCount: () => submitCount};
}

// D6 mutation (a): re-adding onchange="this.form.requestSubmit()" on the
// glide checkbox tick. Also covers the a11y fix: required/disabled must
// track the reveal state, so an untick with blank, disabled fields still
// sends the save (a disabled control is excluded from constraint
// validation AND submitted form data).
test('toggleGlidePathFields: ticking reveals the fields (now required, enabled) without submitting; unticking hides them (disabled, not required) and saves immediately', () => {
    const {fields, form, checkbox, startInput, endInput, yearsInput, getSubmitCount} = glideFixture();
    const doc = rpDocument([form]);
    const ctx = loadRateAssumptions(doc);

    checkbox.checked = true;
    ctx.toggleGlidePathFields(checkbox);
    assert.equal(fields.classList.contains('hidden'), false, 'a tick reveals the fields');
    assert.equal(getSubmitCount(), 0, 'a tick must NEVER submit the form');
    for (const input of [startInput, endInput, yearsInput]) {
        assert.equal(input.disabled, false, 'revealed fields must not be disabled (or Apply could never submit them)');
        assert.equal(input.required, true, 'revealed fields must be required');
    }

    checkbox.checked = false;
    ctx.toggleGlidePathFields(checkbox);
    assert.equal(fields.classList.contains('hidden'), true, 'unticking hides the fields again');
    assert.equal(getSubmitCount(), 1, 'unticking still saves immediately (disabling is safe) even with the fields blank');
    for (const input of [startInput, endInput, yearsInput]) {
        assert.equal(input.disabled, true, 'hidden fields must be disabled (excluded from validation AND submission)');
        assert.equal(input.required, false, 'hidden fields must not be required (a required-but-disabled field is moot, but keep the two attributes consistent)');
    }
});

test('self-check: D6/(a) is real -- restoring the old auto-submit-on-tick behavior is caught by the test above', () => {
    const {form, checkbox, getSubmitCount} = glideFixture();
    const doc = rpDocument([form]);
    const target = 'function toggleGlidePathFields(checkbox) {';
    const patch = (src) => {
        const patched = src.replace(target, () => target + '\n    checkbox.form.requestSubmit(); return;');
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadRateAssumptions(doc, patch);
    checkbox.checked = true;
    ctx.toggleGlidePathFields(checkbox);
    assert.ok(getSubmitCount() > 0, 'expected the reintroduced auto-submit-on-tick mutant to submit on a bare tick');
});

test('self-check: the required/disabled toggle is real -- dropping it from toggleGlidePathFields\'s reveal branch is caught by the reveal test above', () => {
    const {fields, form, checkbox, startInput} = glideFixture();
    const doc = rpDocument([form]);
    // Strips ONLY the reveal branch's disabled/required toggle (leaving the
    // `const inputs = ...` declaration and the hide branch's own forEach
    // intact, so the mutant is a clean, narrow regression rather than a
    // ReferenceError from a dangling reference).
    const target = "        inputs.forEach((input) => {\n            input.disabled = false;\n            input.required = true;\n        });\n        return;";
    const patch = (src) => {
        const patched = src.replace(target, () => '        return;');
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadRateAssumptions(doc, patch);
    checkbox.checked = true;
    ctx.toggleGlidePathFields(checkbox);
    assert.equal(fields.classList.contains('hidden'), false);
    assert.notEqual(startInput.required, true, 'expected the mutant (reveal without marking required) to leave required false, disagreeing with the fixed behavior');
});

// D7 mutation (e): the new-row birth-month `required` dropped. Pins the
// JS-generated row (addPersonRow); the server-rendered row for an existing
// person is pinned separately by a Go template test.
test('addPersonRow: the new row\'s birth-month input is required (a name-only row must never reach the server)', () => {
    const container = rpEl('div', {id: 'person-rows'});
    const doc = rpDocument([container]);
    const ctx = loadRateAssumptions(doc);
    ctx.addPersonRow();
    assert.equal(container.children.length, 1, 'a new row was appended');
    const row = container.children[0];
    const birthMonthTag = /<input type="month" name="person_birth_month\[\]"[^>]*>/.exec(row.innerHTML);
    assert.ok(birthMonthTag, 'birth month input markup not found in the new row');
    assert.match(birthMonthTag[0], /\brequired\b/, 'the new row\'s birth month input must be required');
});

// Build a minimal person-rows fixture: a primary row and a removable
// spouse row inside <form><div id="person-rows">.
function personRowsFixture() {
    const primaryRow = rpEl('div', {'data-person-row': ''});
    primaryRow.appendChild(rpEl('input', {type: 'hidden', name: 'person_role[]', value: 'primary', 'data-person-role-input': ''}));

    const removeBtn = rpEl('button', {'data-remove-person-row': ''});
    const spouseRow = rpEl('div', {'data-person-row': ''});
    spouseRow.appendChild(rpEl('input', {type: 'hidden', name: 'person_role[]', value: 'spouse', 'data-person-role-input': ''}));
    spouseRow.appendChild(removeBtn);

    const container = rpEl('div', {id: 'person-rows'});
    container.appendChild(primaryRow);
    container.appendChild(spouseRow);

    const form = rpEl('form');
    form.appendChild(container);

    return {form, container, primaryRow, spouseRow, removeBtn};
}

// D7 mutation (c): removePersonRow without a save.
test('removePersonRow detaches the row and saves immediately (dispatches a change event on its form)', () => {
    const {form, container, spouseRow, removeBtn} = personRowsFixture();
    const doc = rpDocument([form]);
    const ctx = loadRateAssumptions(doc);

    const dispatched = [];
    const origDispatch = form.dispatchEvent;
    form.dispatchEvent = function (evt) { dispatched.push(evt); return origDispatch.call(form, evt); };

    ctx.removePersonRow(removeBtn);

    assert.equal(container.children.length, 1, 'the row is removed from the DOM immediately');
    assert.ok(!container.children.includes(spouseRow), 'the removed row is gone');
    assert.equal(dispatched.length, 1, 'exactly one event was dispatched on the form (the save)');
    assert.equal(dispatched[0].type, 'change', 'removal saves via the form\'s own change trigger');
});

test('self-check: D7/(c) is real -- reverting removePersonRow to the old DOM-only version is caught by the test above', () => {
    const {form, container, removeBtn} = personRowsFixture();
    const doc = rpDocument([form]);
    const target = /function removePersonRow\(button\) \{[\s\S]*?\n\}\n/;
    const patch = (src) => {
        const oldVersion = "function removePersonRow(button) {\n"
            + "    const row = button.closest('[data-person-row]');\n"
            + "    if (row) {\n"
            + "        row.remove();\n"
            + "    }\n"
            + "    togglePhaseReferenceDropdown();\n"
            + "    updatePersonAgePreviews();\n"
            + "}\n";
        const patched = src.replace(target, () => oldVersion);
        assert.notEqual(patched, src, 'sanity: the patch target must exist');
        return patched;
    };
    const ctx = loadRateAssumptions(doc, patch);
    const dispatched = [];
    form.dispatchEvent = (evt) => { dispatched.push(evt); };
    ctx.removePersonRow(removeBtn);
    assert.equal(container.children.length, 1, 'old version still removes the row from the DOM');
    assert.equal(dispatched.length, 0, 'expected the DOM-only mutant to dispatch no save event');
});

// D7 acceptance: a refused removal (e.g. a linked healthcare entry) restores
// the row to its original position and returns focus to the Remove button
// -- checker-a11y's "focus sane after a refusal".
test('removePersonRow: a refused removal restores the row in place and refocuses its Remove button', () => {
    const {form, container, spouseRow, removeBtn} = personRowsFixture();
    const doc = rpDocument([form]);
    const ctx = loadRateAssumptions(doc);

    ctx.removePersonRow(removeBtn);
    assert.equal(container.children.length, 1, 'optimistically removed');

    // Simulate htmx completing the (debounced) save with a refusal --
    // removePersonRow's own addEventListener('htmx:afterRequest', ...)
    // (registered before the dispatch above) picks this up exactly the way
    // a real htmx response would fire it on `form`.
    form.dispatchEvent({type: 'htmx:afterRequest', detail: {elt: form, successful: false}});

    assert.equal(container.children.length, 2, 'the row is restored after a refusal');
    assert.equal(container.children[1], spouseRow, 'restored to its original position');
    assert.ok(removeBtn.__focused, 'focus returns to the Remove button that was clicked');
});

// A SUCCESSFUL removal must NOT be restored (it stays gone -- AC2 "gone
// after reload").
test('removePersonRow: a successful removal is not restored', () => {
    const {form, container, spouseRow, removeBtn} = personRowsFixture();
    const doc = rpDocument([form]);
    const ctx = loadRateAssumptions(doc);

    ctx.removePersonRow(removeBtn);
    form.dispatchEvent({type: 'htmx:afterRequest', detail: {elt: form, successful: true}});

    assert.equal(container.children.length, 1, 'the row stays removed on success');
    assert.ok(!container.children.includes(spouseRow));
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
