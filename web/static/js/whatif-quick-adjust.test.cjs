// WS1 R-FMT/R-FMT': pins the JS whole-dollar formatter (formatWholeDollars,
// and computeQuickAdjustDisplayText's 'currency' path that calls it)
// against a table of Go formatNumber outputs, including exact .50 ties;
// pins the one-decimal-percent half-even rounding (roundHalfEvenDecimalString
// / computeQuickAdjustDisplayText's 'percent1' path) against Go %.1f; and
// pins that the Quick Adjust load/after-swap sync (syncAllQuickAdjustControls)
// never rewrites a display span or an aria-valuetext the server rendered —
// only mirror values/min/max/step. Loads the real file in a vm context (same
// pattern as busy-banner.test.cjs / insights-trends-cap.test.cjs).
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const SOURCE_PATH = __dirname + '/whatif-quick-adjust.js';

// ---- minimal fake DOM: only the query surface whatif-quick-adjust.js uses
// (attribute-presence/attribute-equals selectors, optionally tag-scoped;
// getElementById; dataset camelCasing; get/set/hasAttribute). No jsdom in
// this repo's toolchain (repo convention, see busy-banner.test.cjs) — this
// is purpose-built for exactly the selectors the file under test issues. ----
function makeElement(spec) {
    const attrs = Object.assign({}, spec.attrs || {});
    const el = {
        tagName: (spec.tag || 'div').toUpperCase(),
        value: spec.value !== undefined ? spec.value : '',
        min: spec.min !== undefined ? spec.min : '',
        max: spec.max !== undefined ? spec.max : '',
        step: spec.step !== undefined ? spec.step : '',
        textContent: spec.textContent || '',
        hasAttribute(n) { return n in attrs; },
        getAttribute(n) { return n in attrs ? attrs[n] : null; },
        setAttribute(n, v) { attrs[n] = String(v); },
        removeAttribute(n) { delete attrs[n]; },
        closest() { return null; },
    };
    Object.defineProperty(el, 'id', {get() { return attrs.id || ''; }});
    Object.defineProperty(el, 'type', {get() { return attrs.type || ''; }});
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
    return {
        _store: store,
        querySelectorAll(sel) { return store.filter((el) => elementMatches(el, sel)); },
        getElementById(id) { return store.find((el) => el.id === id) || null; },
        addEventListener() {},
        body: {addEventListener() {}},
    };
}

// load(): pure-function harness (no DOM elements needed).
// loadWithDom(specs, patchSource?): DOM-driven harness for the sync tests;
// patchSource lets a test load a deliberately-mutated copy of the real
// source to prove a given assertion actually depends on the fix (mutation
// self-check), the same way the swarm's checkers verify a test isn't
// tautological.
function loadWithDom(specs, patchSource) {
    const doc = specs ? makeFakeDom(specs) : {
        addEventListener() {}, body: {addEventListener() {}},
        querySelectorAll() { return []; }, getElementById() { return null; },
    };
    const context = {document: doc, window: {}, console};
    vm.createContext(context);
    let source = fs.readFileSync(SOURCE_PATH, 'utf8');
    if (patchSource) source = patchSource(source);
    vm.runInContext(source, context);
    context.__dom = doc;
    return context;
}

const ctx = loadWithDom(null);

test('exposes the pure formatting helpers at the top level', () => {
    assert.equal(typeof ctx.formatWholeDollars, 'function');
    assert.equal(typeof ctx.computeQuickAdjustDisplayText, 'function');
    assert.equal(typeof ctx.roundHalfEvenDecimalString, 'function');
    assert.equal(typeof ctx.syncAllQuickAdjustControls, 'function');
    assert.equal(typeof ctx.syncQuickAdjustMirrorOnly, 'function');
});

// Table of (value -> Go formatNumber output), taken from the WS1 oracle's
// own .50-tie fixture values plus the two examples in the task brief. Every
// floor here is EVEN, so a half-up formatter gives a DIFFERENT (wrong)
// answer for every row -- this table cannot pass against Math.round or
// int64(v+0.5).
const goFormatNumberTies = [
    [1800.5, '$1,800'],       // task brief's own even-floor example
    [612.5, '$612'],
    [1800.5, '$1,800'],
    [2150.5, '$2,150'],
    [3450.5, '$3,450'],
    [10736.5, '$10,736'],
    [2437512.5, '$2,437,512'],
];

// An odd-floor .50 tie: half-even and half-up AGREE here (both round up),
// so this alone would never catch a half-up regression -- included only to
// pin the brief's second worked example and the "ties round to the NEAREST
// even, not always down" behaviour.
const oddFloorTie = [1801.5, '$1,802'];

// Ordinary (non-tie) values: normal nearest-integer rounding must still
// work either way.
const ordinary = [
    [1654.3, '$1,654'],
    [1655.6, '$1,656'],
    [0, '$0'],
    [999.5, '$1,000'],  // floor 999 is odd -> rounds up to 1000 either rule
];

test('formatWholeDollars: even-floor .50 ties round to EVEN (kills a half-up formatter)', () => {
    for (const [v, want] of goFormatNumberTies) {
        assert.equal(ctx.formatWholeDollars(v), want, `formatWholeDollars(${v})`);
    }
});

test('formatWholeDollars: odd-floor .50 tie and ordinary values', () => {
    const [v, want] = oddFloorTie;
    assert.equal(ctx.formatWholeDollars(v), want);
    for (const [ov, owant] of ordinary) {
        assert.equal(ctx.formatWholeDollars(ov), owant, `formatWholeDollars(${ov})`);
    }
});

// Sign placement ("$-612", not "-$612") is pre-existing, unchanged
// behaviour (Math.round(-612.5).toLocaleString() already gave "-612"
// before this fix) -- no WS1 WHOLE-DOLLAR field is ever negative (every
// money field's min is 0), so only the rounding RULE is this test's
// concern, not the sign convention. This does NOT generalize to the
// percent1 fields below -- inflation_rate/spending_decline_rate/
// pre_medicare_inflation/post_medicare_inflation carry NO server-side
// range validation (form_spec.go HasBounds:false / handlers_healthcare.go
// checks only monthlyCost and age) and so CAN legitimately hold a
// negative value; WS1.4's checker-second found exactly that gap in an
// earlier version of this file's percent1 table (WS1 C2).
test('formatWholeDollars: negative values round the same way as positive', () => {
    assert.equal(ctx.formatWholeDollars(-612.5), '$-612');
    assert.equal(ctx.formatWholeDollars(-1801.5), '$-1,802');
});

test("computeQuickAdjustDisplayText('currency', ...) delegates to formatWholeDollars (one formatting rule, one call site)", () => {
    for (const [v, want] of goFormatNumberTies) {
        assert.equal(ctx.computeQuickAdjustDisplayText('currency', v), want);
    }
});

// ---- WS1 R-FMT': one-decimal percents, half-even on the exact binary
// value, matching Go %.1f -- verified against real `go run` output for
// every row (see the WS1.4 report). Every "even" row's floor digit is
// even (so half-even keeps it) and its neighbour is odd (so a half-up/
// toFixed formatter gives a DIFFERENT string) -- this table cannot pass
// against a plain .toFixed(1) or a x*10/10 shortcut. ----
const percentHalfEvenTies = [
    [7.25, '7.2'], [4.35, '4.3'], [3.85, '3.9'], [0.35, '0.3'],
    [2.25, '2.2'], [82.25, '82.2'], [6.25, '6.2'], [10.25, '10.2'],
    [0.05, '0.1'], [0.15, '0.1'], [0.25, '0.2'], [1.05, '1.1'],
    [1.15, '1.1'], [1.25, '1.2'], [1.35, '1.4'], [1.45, '1.4'],
    [1.55, '1.6'], [1.65, '1.6'], [1.75, '1.8'], [1.85, '1.9'],
    [1.95, '1.9'], [0, '0.0'], [100, '100.0'],
];

test('roundHalfEvenDecimalString(v, 1): half-even at every exact .X5 tie, matches Go %.1f', () => {
    for (const [v, want] of percentHalfEvenTies) {
        assert.equal(ctx.roundHalfEvenDecimalString(v, 1), want, `roundHalfEvenDecimalString(${v}, 1)`);
    }
});

test("computeQuickAdjustDisplayText('percent1', ...) delegates to roundHalfEvenDecimalString (kills toFixed/x*10 in the JS percent path)", () => {
    for (const [v, want] of percentHalfEvenTies) {
        assert.equal(ctx.computeQuickAdjustDisplayText('percent1', v), want + '%');
    }
});

// WS1 C2: negative values against REAL Go `%.1f` output (`go run`,
// verified by hand for every row below), incl. genuine negative-near-zero
// values that round to zero -- Go's bare %.1f (no CB9 zero-belt) keeps the
// sign unconditionally, e.g. -0.03 -> "-0.0". checker-second's 32,465-value
// sweep found the previous implementation dropped the sign whenever the
// ROUNDED result was zero (1,014 mismatches, all this one case); the tie
// logic itself was already exact. inflation_rate, spending_decline_rate,
// pre_medicare_inflation and post_medicare_inflation carry no server-side
// range check, so a genuine negative value is a real, reachable state.
const percentHalfEvenNegatives = [
    [-0.03, '-0.0'], [-0.01, '-0.0'], [-0.001, '-0.0'], [-0.049, '-0.0'],
    [-0.05, '-0.1'], [-0.0499, '-0.0'], [-0.25, '-0.2'], [-1.25, '-1.2'],
    [-7.25, '-7.2'], [-2.5, '-2.5'], [-3.5, '-3.5'], [-0.15, '-0.1'],
    [-0.45, '-0.5'],
];

test('roundHalfEvenDecimalString: negative values keep the sign exactly as Go %.1f does, even when the rounded result is zero', () => {
    for (const [v, want] of percentHalfEvenNegatives) {
        assert.equal(ctx.roundHalfEvenDecimalString(v, 1), want, `roundHalfEvenDecimalString(${v}, 1)`);
    }
    // Literal IEEE -0 (Go's %.1f also prints "-0.0" for this, unbelted) and
    // positive zero (must stay "0.0", no spurious sign).
    assert.equal(ctx.roundHalfEvenDecimalString(-0, 1), '-0.0');
    assert.equal(ctx.roundHalfEvenDecimalString(0, 1), '0.0');
});

test('self-check: the pre-fix sign-dropping logic fails the negative table above', () => {
    // Reproduces the exact regression by hand: dropping the sign whenever
    // Number(result) === 0, the previous implementation's own rule.
    function preFixSign(v, digits) {
        const s = ctx.roundHalfEvenDecimalString(v, digits);
        const negative = v < 0;
        const unsigned = s.replace(/^-/, '');
        return negative && Number(unsigned) !== 0 ? '-' + unsigned : unsigned;
    }
    let mismatches = 0;
    for (const [v, want] of percentHalfEvenNegatives) {
        if (preFixSign(v, 1) !== want) mismatches++;
    }
    assert.ok(mismatches > 0, 'expected the pre-fix sign rule to disagree with Go on at least one row');
});

// Self-check: applies the two mutations R-TEST' names by hand against this
// same table, proving it is not tautological -- each must disagree with
// the half-even table on at least one row.
test('a toFixed(1) mutant, and an x*10/Math.round/10 mutant, both fail this table (self-check)', () => {
    let toFixedMismatches = 0, x10Mismatches = 0;
    for (const [v, want] of percentHalfEvenTies) {
        if (v.toFixed(1) !== want) toFixedMismatches++;
        if ((Math.round(v * 10) / 10).toFixed(1) !== want) x10Mismatches++;
    }
    assert.ok(toFixedMismatches > 0, 'expected at least one row where toFixed(1) disagrees with the half-even table');
    assert.ok(x10Mismatches > 0, 'expected at least one row where x*10/Math.round/10 disagrees with the half-even table');
});

// ---- WS1 R-TEST': per-call-site coverage. Each of these mirrors a single
// surface the checker found could regress to a half-up formatter WITHOUT
// any existing test noticing, because formatWholeDollars itself stayed
// correct -- these exercise the ACTUAL call site (formatQuickAdjustDisplay /
// syncQuickAdjustKey), not just the shared helper in isolation. ----

test("formatQuickAdjustDisplay 'phase-dollar' call site: even-floor .50 tie renders half-even (kills a half-up formatter localized to this call site)", () => {
    // base (monthly_living_expenses_value) x multiplier = 7202 x 0.25 =
    // 1800.5 -- even floor, so half-even "$1,800" vs half-up "$1,801".
    const dom = makeFakeDom([
        {tag: 'input', attrs: {id: 'monthly_living_expenses_value', type: 'hidden'}, value: '7202'},
    ]);
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    vm.runInContext(fs.readFileSync(SOURCE_PATH, 'utf8'), context);
    const node = makeElement({tag: 'span', attrs: {'data-quick-adjust-display-format': 'phase-dollar'}});
    context.formatQuickAdjustDisplay(node, '0.25');
    assert.equal(node.textContent, '$1,800/mo');
});

test("syncQuickAdjustKey('monthly_living_expenses') aria path: even-floor .50 tie sets half-even aria on the primary range AND every QA mirror (kills a half-up formatter localized to this call site)", () => {
    // 10736.50 -- even floor, half-even "$10,736" vs half-up "$10,737"
    // (WS1.3's own living-expenses tie value).
    const specs = [
        {tag: 'input', attrs: {type: 'hidden', 'data-quick-adjust-key': 'monthly_living_expenses'}, value: '10736.5'},
        {tag: 'input', attrs: {id: 'monthly_living_expenses_input', type: 'range', 'aria-valuetext': '$0'}, value: '10700'},
        {
            tag: 'input',
            attrs: {type: 'range', 'data-quick-adjust-key': 'monthly_living_expenses', 'data-quick-adjust-mirror': 'true', 'aria-valuetext': '$0'},
            value: '10700',
        },
    ];
    const dom = makeFakeDom(specs);
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    vm.runInContext(fs.readFileSync(SOURCE_PATH, 'utf8'), context);
    context.syncQuickAdjustKey('monthly_living_expenses');
    const primary = dom.getElementById('monthly_living_expenses_input');
    assert.equal(primary.getAttribute('aria-valuetext'), '$10,736');
    const mirror = specs.map((_, i) => dom._store[i]).find((el) => el.hasAttribute('data-quick-adjust-mirror'));
    assert.equal(mirror.getAttribute('aria-valuetext'), '$10,736');
});

// ---- WS1 R-FMT'/R-TEST': the load/after-swap sync must not rewrite any
// display span or aria-valuetext -- only mirror value/min/max/step. This
// constructs a fake "served" page (a percent1 field + its Quick Adjust
// mirror, matching what the server would have rendered: display/aria show
// the exact-decimal half-even figure "7.2%" for a stored 7.25, NOT the
// toFixed-would-give "7.3%") and asserts syncAllQuickAdjustControls() (the
// DOMContentLoaded / htmx:afterSwap / panel-open path) leaves every one of
// them untouched. ----
// The canonical's value (3.85) is DELIBERATELY different from what the
// served display/aria show (7.2%, i.e. as if the canonical were ~7.25) --
// on a real page these always agree (both come from the same server
// render), but making them disagree here turns "did a recompute run" into
// a directly observable fact, decoupled from whether the recompute's
// FORMATTER is itself correct (that's the separate percent1/roundHalfEven
// tests' job). If the load sync is structurally mirror-only, the display
// stays "7.2%" no matter what the canonical holds; if it calls the full,
// display-writing sync, the display changes to reflect 3.85 (correctly,
// "3.9%", since the formatter itself is also fixed) -- a change either way
// proves a rewrite happened.
function servedPercentFieldSpecs() {
    return [
        // Canonical: hidden exact-value input (W2 Part B / WS1 pattern).
        {tag: 'input', attrs: {id: 'pmi-exact', type: 'hidden', 'data-quick-adjust-key': 'pmi'}, value: '3.85'},
        // In-card mirror range: server rendered value snapped to its
        // step grid, but aria-valuetext/display carry the EXACT figure.
        {
            tag: 'input',
            attrs: {
                id: 'pmi-range', type: 'range', 'data-quick-adjust-key': 'pmi',
                'data-quick-adjust-mirror': 'true', 'data-quick-adjust-display-format': 'percent1',
                'aria-valuetext': '7.2%',
            },
            value: '7.5',
        },
        {tag: 'span', attrs: {'data-quick-adjust-display': 'pmi', 'data-quick-adjust-display-format': 'percent1'}, textContent: '7.2%'},
        // Quick Adjust drawer mirror + its own display span.
        {
            tag: 'input',
            attrs: {
                id: 'qa-pmi-range', type: 'range', 'data-quick-adjust-key': 'pmi',
                'data-quick-adjust-mirror': 'true', 'data-quick-adjust-display-format': 'percent1',
                'aria-valuetext': '7.2%',
            },
            value: '7.5',
        },
        {tag: 'span', attrs: {'data-quick-adjust-display': 'pmi', 'data-quick-adjust-display-format': 'percent1'}, textContent: '7.2%'},
    ];
}

test('syncAllQuickAdjustControls (load/after-swap sync) never rewrites a display span or aria-valuetext', () => {
    const dom = makeFakeDom(servedPercentFieldSpecs());
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    vm.runInContext(fs.readFileSync(SOURCE_PATH, 'utf8'), context);

    const before = dom._store.map((el) => ({text: el.textContent, aria: el.getAttribute('aria-valuetext')}));
    context.syncAllQuickAdjustControls();
    const after = dom._store.map((el) => ({text: el.textContent, aria: el.getAttribute('aria-valuetext')}));

    assert.deepEqual(after, before, 'no display textContent or aria-valuetext changed during the load sync');
    // Still reading the ORIGINAL served figure, not a recompute of the
    // (deliberately different) canonical value 3.85 ("3.9%" either
    // formatter would give, or "3.8%"/"3.9%" for a half-even/half-up one).
    for (const el of dom._store) {
        assert.notEqual(el.textContent, '3.9%');
        assert.notEqual(el.getAttribute('aria-valuetext'), '3.9%');
    }
    // The structural exception still holds: mirror VALUES do sync from
    // canonical (only display/aria are frozen).
    const canonical = dom.getElementById('pmi-exact');
    const mirrors = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-mirror'));
    for (const m of mirrors) {
        assert.equal(m.value, canonical.value, 'mirror VALUE still syncs from canonical at load');
    }
});

test('self-check: the load-sync test above actually catches a load-time rewrite (mutation: load path calls the FULL sync)', () => {
    const dom = makeFakeDom(servedPercentFieldSpecs());
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    // Patch: restore the pre-fix behaviour where the load sweep called the
    // full (display+aria-writing) syncQuickAdjustKey instead of the
    // mirror-only sync.
    const mutated = fs.readFileSync(SOURCE_PATH, 'utf8')
        .replace('syncQuickAdjustMirrorOnly(control.dataset.quickAdjustKey);', 'syncQuickAdjustKey(control.dataset.quickAdjustKey);');
    assert.notEqual(mutated.indexOf('syncQuickAdjustKey(control.dataset.quickAdjustKey);'), -1, 'the patch target string must exist in the source');
    vm.runInContext(mutated, context);

    context.syncAllQuickAdjustControls();
    const displays = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-display'));
    const rewritten = displays.some((el) => el.textContent === '3.9%');
    assert.ok(rewritten, 'the mutated (pre-fix) load path is expected to recompute the percent1 display from the canonical value (3.85 -> "3.9%"), proving a rewrite happened');
});

// ---- WS1 attempt-5, J6: syncQuickAdjustAriaValueTexts' currency path must
// go through the SAME formatWholeDollars formatNumber-half-even rule as
// the display, on a REAL user-driven sync (not the load path, which never
// touches aria at all -- this is a different call site). WS1.4
// checker-second/checker-tests: a mutation that gives
// syncQuickAdjustAriaValueTexts its OWN inline half-up formatter for
// 'currency' (instead of calling computeQuickAdjustDisplayText, the shared
// helper) survived every existing test -- portfolio 1234568.5, aria showed
// "$1,234,569" while the display correctly showed "$1,234,568". ----
function currencyKeySpecs() {
    return [
        {tag: 'input', attrs: {id: 'pv-exact', type: 'hidden', 'data-quick-adjust-key': 'pv'}, value: '2437512.5'},
        {
            tag: 'input',
            attrs: {
                id: 'pv-range', type: 'range', 'data-quick-adjust-key': 'pv',
                'data-quick-adjust-mirror': 'true', 'data-quick-adjust-display-format': 'currency',
                'aria-valuetext': '$0',
            },
            value: '0',
        },
        {tag: 'span', attrs: {'data-quick-adjust-display': 'pv', 'data-quick-adjust-display-format': 'currency'}, textContent: '$0'},
        {
            tag: 'input',
            attrs: {
                id: 'qa-pv-range', type: 'range', 'data-quick-adjust-key': 'pv',
                'data-quick-adjust-mirror': 'true', 'data-quick-adjust-display-format': 'currency',
                'aria-valuetext': '$0',
            },
            value: '0',
        },
        {tag: 'span', attrs: {'data-quick-adjust-display': 'pv', 'data-quick-adjust-display-format': 'currency'}, textContent: '$0'},
    ];
}

test("syncQuickAdjustKey('pv') on a genuine user-driven sync: aria-valuetext (via syncQuickAdjustAriaValueTexts) is half-even, matching the display, on an even-floor .50 tie", () => {
    const dom = makeFakeDom(currencyKeySpecs());
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    vm.runInContext(fs.readFileSync(SOURCE_PATH, 'utf8'), context);

    context.syncQuickAdjustKey('pv');

    const displays = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-display'));
    const ranges = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-mirror'));
    for (const d of displays) assert.equal(d.textContent, '$2,437,512');
    for (const r of ranges) assert.equal(r.getAttribute('aria-valuetext'), '$2,437,512');
});

test('self-check: an inline half-up formatter localized to syncQuickAdjustAriaValueTexts\' currency path is caught by the test above', () => {
    const dom = makeFakeDom(currencyKeySpecs());
    const context = {document: dom, window: {}, console};
    vm.createContext(context);
    // Patch: give the aria-setting call its OWN inline half-up formatter
    // instead of computeQuickAdjustDisplayText (the shared, correct one) --
    // exactly the WS1.4 checker-second/checker-tests J6 mutation.
    const target = "control.setAttribute('aria-valuetext', computeQuickAdjustDisplayText(format, value));";
    // A replacer FUNCTION (not a string) avoids String.prototype.replace's
    // special "$'"/"$&" substitution patterns, which the literal '$' in
    // this replacement would otherwise trigger.
    const mutated = fs.readFileSync(SOURCE_PATH, 'utf8').replace(
        target,
        () => "control.setAttribute('aria-valuetext', format === 'currency' ? ('$' + Math.round(Number(value)).toLocaleString('en-US')) : computeQuickAdjustDisplayText(format, value));"
    );
    assert.equal(mutated.indexOf(target), -1, 'sanity: the target string must have been replaced');
    vm.runInContext(mutated, context);

    context.syncQuickAdjustKey('pv');
    const ranges = dom._store.filter((el) => el.hasAttribute('data-quick-adjust-mirror'));
    const wrong = ranges.some((r) => r.getAttribute('aria-valuetext') === '$2,437,513');
    assert.ok(wrong, 'the mutated aria path is expected to disagree with the (correct) half-even display');
});
